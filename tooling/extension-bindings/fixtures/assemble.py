#!/usr/bin/env python3
"""Assemble installable Phase 4 Go and Python binding fixtures.

This is a test-package assembler, not the production release orchestrator.
It never runs either profiler or application source.
"""

from __future__ import annotations

import argparse
from email.parser import Parser
from hashlib import sha256
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile
from typing import Any
import zipfile

sys.dont_write_bytecode = True
import jsonschema  # noqa: E402
import rfc8785  # noqa: E402
import yaml  # noqa: E402


TOOLING = Path(__file__).resolve().parents[1]
EXTENSIONS = TOOLING.parents[1]
WORKSPACE = EXTENSIONS.parent
MODEL = TOOLING / "model"
CASES = MODEL / "conformance/cases"
GO_TARGETS = TOOLING / "emitters/go/testdata/package-targets"
PYTHON_EMITTER = TOOLING / "emitters/python/src"
CORE_SCHEMA = WORKSPACE / "spec/schema/runtimeconditions.profile.v0.2.0.schema.yaml"
RESOURCE_NAMES = (
    "runtimeconditions.bindings.yaml",
    "runtimeconditions.binding-model.yaml",
    "runtimeconditions.extension.yaml",
    "runtimeconditions.binding-release.yaml",
)
CORE_ID = "https://runtimeconditions.io/schemas/profile/0.2.0/runtimeconditions.profile.schema.yaml"
CORE_VERSION = "0.2.0"
CORE_DIGEST = "a090a8016d045f9c3fa872a67f8df293b77ca2809a1bea5ae9fa31a27a06109a"
ZIP_TIME = (1980, 1, 1, 0, 0, 0)

# Dependency-first order. Each package is assembled from the same conformance
# source as its normalized model, including dependency-owned declarations.
PACKAGES = (
    ("01-owned-kind-interface", "01-owned-kind-interface"),
    ("02-additive-field", "02-additive-field-base"),
    ("02-additive-field", "02-additive-field"),
    ("03-transitive-closure", "03-transitive-leaf"),
    ("03-transitive-closure", "03-transitive-middle"),
    ("03-transitive-closure", "03-transitive-closure"),
    ("06-recursive-reference", "06-recursive-reference"),
    ("07-object-alternatives", "07-object-alternatives"),
    ("08-heterogeneous-union", "08-heterogeneous-union"),
    ("09-collections-and-maps", "09-collections-and-maps"),
    ("10-scoped-domains-collisions", "10-scoped-domains-collisions"),
    ("11-source-name-preservation", "11-source-name-preservation"),
    ("13-dependency-schema-only", "13-dependency-schema-only-dependency"),
    ("13-dependency-schema-only", "13-dependency-schema-only"),
)


def fail(message: str) -> None:
    raise SystemExit(message)


def digest(data: bytes) -> str:
    return sha256(data).hexdigest()


def read_yaml(path: Path) -> dict[str, Any]:
    result = yaml.safe_load(path.read_bytes())
    if not isinstance(result, dict):
        fail(f"{path}: expected a YAML mapping")
    return result


def write_yaml(path: Path, value: dict[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(yaml.safe_dump(value, sort_keys=False, allow_unicode=True), encoding="utf-8")


def run(*arguments: str, cwd: Path | None = None, env: dict[str, str] | None = None) -> str:
    result = subprocess.run(
        arguments, cwd=cwd, env=env, check=True, text=True,
        stdout=subprocess.PIPE, stderr=subprocess.PIPE,
    )
    return result.stdout


def validate(document: dict[str, Any], schema_path: Path) -> None:
    schema = read_yaml(schema_path)
    jsonschema.Draft202012Validator(schema, format_checker=jsonschema.FormatChecker()).validate(document)


def check_core() -> None:
    schema = read_yaml(CORE_SCHEMA)
    if schema.get("$id") != CORE_ID or schema.get("x-runtimeconditions-version") != CORE_VERSION:
        fail("core schema ID or version differs from the fixture contract")
    if digest(rfc8785.dumps(schema)) != CORE_DIGEST:
        fail("core schema semantic digest differs from the fixture contract")


def build_go_binary(source: Path, destination: Path, env: dict[str, str]) -> None:
    destination.parent.mkdir(parents=True, exist_ok=True)
    run("go", "build", "-trimpath", "-o", str(destination), "./cmd/" +
        ("rc-binding-model" if source.name == "normalizer" else "rc-go-bindings"),
        cwd=source, env=env)


def extension_source(case: str, extension_id: str) -> Path:
    matches = [
        path for path in sorted((CASES / case).glob("*.yaml"))
        if (read_yaml(path).get("metadata", {}).get("uri", "") + ":" + read_yaml(path).get("metadata", {}).get("version", "")) == extension_id
    ]
    if len(matches) != 1:
        fail(f"{case}: expected one source for {extension_id}, found {len(matches)}")
    return matches[0]


def normalize(
    case: str, target: dict[str, Any], output: Path, normalizer: Path,
    normalizer_digest: str,
) -> tuple[Path, dict[str, Any], dict[str, Any]]:
    output.mkdir(parents=True, exist_ok=True)
    model_path = output / "runtimeconditions.binding-model.yaml"
    lock_path = output / "dependency-lock.yaml"
    run(
        str(normalizer), "--root", target["rootExtension"],
        "--extension-root", str(CASES / case),
        "--semantic-schema", str(MODEL / "runtimeconditions.extension-semantic.schema.yaml"),
        "--model-schema", str(MODEL / "runtimeconditions.binding-model.schema.yaml"),
        "--core-profile-id", CORE_ID,
        "--core-profile-version", CORE_VERSION,
        "--core-profile-semantic-sha256", CORE_DIGEST,
        "--normalizer-sha256", normalizer_digest,
        "--output", str(model_path),
        "--dependency-lock-output", str(lock_path),
    )
    normalized = read_yaml(model_path)
    lock = read_yaml(lock_path)
    validate(normalized, MODEL / "runtimeconditions.binding-model.schema.yaml")
    copy = json.loads(json.dumps(normalized))
    stated_digest = copy["metadata"].pop("semanticSha256")
    if digest(rfc8785.dumps(copy)) != stated_digest:
        fail(f"{target['packageKey']}: normalized model digest is inconsistent")
    if normalized["coreProfileSchema"] != {
        "id": CORE_ID, "version": CORE_VERSION, "semanticSha256": CORE_DIGEST,
    }:
        fail(f"{target['packageKey']}: normalized model references another core schema")
    if normalized["metadata"]["normalizer"]["sha256"] != normalizer_digest:
        fail(f"{target['packageKey']}: model does not identify the executed normalizer")
    model_extensions = {item["id"]: item for item in normalized["extensions"]}
    locked = {item["id"]: item for item in lock["extensions"]}
    if model_extensions.keys() != locked.keys():
        fail(f"{target['packageKey']}: model and lock extension sets differ")
    for extension_id, item in locked.items():
        source = extension_source(case, extension_id)
        if item["sourceSha256"] != digest(source.read_bytes()):
            fail(f"{target['packageKey']}: source digest differs for {extension_id}")
        if item["semanticSha256"] != model_extensions[extension_id]["semanticSha256"]:
            fail(f"{target['packageKey']}: semantic digest differs for {extension_id}")
        if item["version"] != model_extensions[extension_id]["version"] or (
            set(item.get("dependencies", [])) !=
            set(model_extensions[extension_id].get("dependencies", []))
        ):
            fail(f"{target['packageKey']}: version or dependency edges differ for {extension_id}")
        if item["sourceBackend"] != "catalog":
            fail(f"{target['packageKey']}: unexpected resolver backend for {extension_id}")
    if normalized["rootExtension"] != {
        "id": target["rootExtension"],
        "version": locked[target["rootExtension"]]["version"],
        "semanticSha256": locked[target["rootExtension"]]["semanticSha256"],
    }:
        fail(f"{target['packageKey']}: root identity differs from the lock")
    direct = set(model_extensions[target["rootExtension"]].get("dependencies", []))
    configured = {item["extension"] for item in target.get("dependencies", [])}
    if direct != configured:
        fail(f"{target['packageKey']}: direct package dependencies differ from the model")
    return model_path, normalized, lock


def python_target(go_target: dict[str, Any], by_id: dict[str, dict[str, Any]]) -> dict[str, Any]:
    key = go_target["packageKey"]
    target: dict[str, Any] = {
        "apiVersion": "runtimeconditions.io/python-package-target/v1alpha1",
        "kind": "RuntimeConditionsPythonPackageTarget",
        "packageKey": key,
        "rootExtension": go_target["rootExtension"],
        "distributionName": "runtimeconditions-" + key,
        "importPackage": ("runtimeconditions_" + key).replace("-", "_"),
        "version": go_target["version"].removeprefix("v"),
        "sourceDirectory": f"bindings/{key}/python",
        "minimumPythonVersion": "3.11",
        "publicationMode": go_target["publicationMode"],
        "repositoryUrl": "https://github.com/runtimeconditions/extensions",
    }
    if go_target.get("dependencies"):
        target["dependencies"] = [
            {
                "extension": item["extension"],
                "distributionName": "runtimeconditions-" + by_id[item["extension"]]["packageKey"],
                "importPackage": ("runtimeconditions_" + by_id[item["extension"]]["packageKey"]).replace("-", "_"),
                "version": item["version"].removeprefix("v"),
            }
            for item in go_target["dependencies"]
        ]
    return target


def python_profiler_identity(python_wheel: Path) -> dict[str, str]:
    if not python_wheel.is_file():
        fail("Python profiler wheel must be an existing file")
    with zipfile.ZipFile(python_wheel) as archive:
        metadata_paths = [name for name in archive.namelist() if name.endswith(".dist-info/METADATA")]
        if len(metadata_paths) != 1:
            fail("Python profiler wheel must contain one METADATA file")
        metadata = Parser().parsestr(archive.read(metadata_paths[0]).decode("utf-8"))
    if metadata["Name"].lower().replace("_", "-") != "runtimeconditions-profiler":
        fail("Python profiler wheel has the wrong distribution name")
    return {
        "name": "python-rc-profiler", "version": metadata["Version"],
        "sha256": digest(python_wheel.read_bytes()),
    }


def profiler_identities(go_path: Path, python_wheel: Path) -> dict[str, dict[str, str]]:
    if not go_path.is_file():
        fail("Go profiler artifact must be an existing file")
    build_info = run("go", "version", "-m", str(go_path))
    if "go-rc-profiler" not in build_info:
        fail("Go profiler binary lacks the expected Go build identity")
    version_lines = re.findall(r"^\s*mod\s+\S*go-rc-profiler\s+(\S+)", build_info, re.M)
    go_version = version_lines[0] if version_lines else "(devel)"
    return {
        "go": {"name": "go-rc-profiler", "version": go_version, "sha256": digest(go_path.read_bytes())},
        "python": python_profiler_identity(python_wheel),
    }


def dependency_entries(
    language: str, target: dict[str, Any], by_id: dict[str, dict[str, Any]],
    artifacts: dict[str, dict[str, Path]],
) -> list[dict[str, Any]]:
    entries = []
    for dependency in target.get("dependencies", []):
        extension_id = dependency["extension"]
        depended = by_id[extension_id]
        artifact = artifacts[language].get(extension_id)
        if artifact is None:
            fail(f"{target['packageKey']}: dependency {extension_id} was not assembled first")
        if language == "go":
            coordinate = dependency["modulePath"]
            name = dependency["packageName"]
            version = dependency["version"]
            kind = "go-module-zip"
            next_version = "v2.0.0"
        else:
            coordinate = depended["distributionName"]
            name = depended["importPackage"]
            version = dependency["version"]
            kind = "python-wheel"
            next_version = "2.0.0"
        entries.append({
            "extension": extension_id, "coordinate": coordinate, "name": name,
            "testedVersion": version,
            "compatibleVersionRange": {
                "minimumInclusive": version, "nextBreakingExclusive": next_version,
            },
            "artifact": {"kind": kind, "sha256": digest(artifact.read_bytes())},
        })
    return entries


def release(
    language: str, target: dict[str, Any], normalized: dict[str, Any],
    lock: dict[str, Any], dependencies: list[dict[str, Any]],
    profiler: dict[str, str],
) -> dict[str, Any]:
    package = {
        "packageKey": target["packageKey"], "language": language,
        "coordinate": target["modulePath"] if language == "go" else target["distributionName"],
        "name": target["packageName"] if language == "go" else target["importPackage"],
        "version": target["version"], "publicationMode": target["publicationMode"],
    }
    package["minimumGoVersion" if language == "go" else "minimumPythonVersion"] = (
        target["minimumGoVersion"] if language == "go" else target["minimumPythonVersion"]
    )
    result = {
        "apiVersion": "runtimeconditions.io/binding-release/v1alpha1",
        "kind": "RuntimeConditionsBindingRelease",
        "package": package,
        "model": {
            "apiVersion": normalized["apiVersion"],
            "semanticSha256": normalized["metadata"]["semanticSha256"],
        },
        "rootExtension": normalized["rootExtension"],
        "dependencyLock": lock,
        "packageDependencies": dependencies,
        "provenance": {
            "mode": "test-fixture",
            "fixtureAssembler": {
                "name": "rc-binding-fixture-assembler", "version": "0.1.0",
                "sha256": digest(Path(__file__).read_bytes()),
            },
            "profiler": profiler,
        },
    }
    validate(result, MODEL / "runtimeconditions.binding-release.schema.yaml")
    return result


def check_manifest(package_dir: Path, normalized: dict[str, Any], target: dict[str, Any], language: str) -> None:
    manifest = read_yaml(package_dir / "runtimeconditions.bindings.yaml")
    validate(manifest, MODEL / "runtimeconditions.binding-manifest.schema.yaml")
    if manifest["model"]["semanticSha256"] != normalized["metadata"]["semanticSha256"]:
        fail(f"{target['packageKey']}: binding manifest and model digests differ")
    if manifest["extension"]["id"] != target["rootExtension"] or (
        manifest["extension"]["semanticSha256"] != normalized["rootExtension"]["semanticSha256"]
    ):
        fail(f"{target['packageKey']}: binding manifest and root extension differ")
    coordinate = target["modulePath"] if language == "go" else target["distributionName"]
    package = manifest["package"]
    expected = {
        "language": language,
        "coordinate": coordinate,
        "name": target["packageName"] if language == "go" else target["importPackage"],
        "version": target["version"],
        "minimumGoVersion" if language == "go" else "minimumPythonVersion":
            target["minimumGoVersion"] if language == "go" else target["minimumPythonVersion"],
    }
    if package != expected:
        fail(f"{target['packageKey']}: binding manifest package identity differs")


def write_resources(
    package_dir: Path, case: str, target: dict[str, Any],
    model_path: Path, normalized: dict[str, Any], lock: dict[str, Any],
    language: str, dependencies: list[dict[str, Any]], profiler: dict[str, str],
) -> None:
    check_manifest(package_dir, normalized, target, language)
    source = extension_source(case, target["rootExtension"])
    (package_dir / "runtimeconditions.binding-model.yaml").write_bytes(model_path.read_bytes())
    (package_dir / "runtimeconditions.extension.yaml").write_bytes(source.read_bytes())
    write_yaml(
        package_dir / "runtimeconditions.binding-release.yaml",
        release(language, target, normalized, lock, dependencies, profiler),
    )
    if set(RESOURCE_NAMES) - {path.name for path in package_dir.iterdir()}:
        fail(f"{target['packageKey']}: one or more package-local resources are missing")


def canonical_zip(source: Path, destination: Path, prefix: str = "") -> None:
    destination.parent.mkdir(parents=True, exist_ok=True)
    with zipfile.ZipFile(destination, "w") as archive:
        for path in sorted(source.rglob("*")):
            if not path.is_file():
                continue
            relative = path.relative_to(source).as_posix()
            info = zipfile.ZipInfo(prefix + relative, ZIP_TIME)
            info.compress_type = zipfile.ZIP_DEFLATED
            info.external_attr = 0o100644 << 16
            archive.writestr(info, path.read_bytes())


def go_archive(package_dir: Path, target: dict[str, Any], proxy: Path) -> Path:
    module = target["modulePath"]
    version = target["version"]
    version_dir = proxy / module / "@v"
    archive_path = version_dir / f"{version}.zip"
    canonical_zip(package_dir, archive_path, module + "@" + version + "/")
    (version_dir / f"{version}.mod").write_bytes((package_dir / "go.mod").read_bytes())
    (version_dir / f"{version}.info").write_text(
        json.dumps({"Version": version, "Time": "2026-01-01T00:00:00Z"}) + "\n",
        encoding="utf-8",
    )
    return archive_path


def python_wheel(package_tree: Path, wheel_dir: Path, python: Path) -> Path:
    wheel_dir.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory() as temporary:
        build_tree = Path(temporary) / "package"
        shutil.copytree(package_tree, build_tree)
        environment = os.environ.copy()
        environment["SOURCE_DATE_EPOCH"] = "315532800"
        environment["PYTHONHASHSEED"] = "0"
        run(
            str(python), "-m", "pip", "wheel", "--no-deps", "--no-build-isolation",
            "--wheel-dir", str(wheel_dir), str(build_tree),
            env=environment,
        )
    package_manifest = read_yaml(package_tree / "src" / next(
        child.name for child in (package_tree / "src").iterdir() if child.is_dir()
    ) / "runtimeconditions.bindings.yaml")["package"]
    wheel_name = (
        package_manifest["coordinate"].replace("-", "_") + "-" +
        package_manifest["version"] + "-py3-none-any.whl"
    )
    wheel = wheel_dir / wheel_name
    if not wheel.is_file():
        fail(f"{package_tree}: wheel build did not produce {wheel_name}")
    with tempfile.TemporaryDirectory() as temporary:
        normalized_wheel = Path(temporary) / wheel.name
        with zipfile.ZipFile(wheel) as source, zipfile.ZipFile(normalized_wheel, "w") as output:
            names = source.namelist()
            if len(names) != len(set(names)):
                fail(f"{wheel}: duplicate archive entry")
            for name in sorted(names):
                if name.endswith("/"):
                    continue
                info = zipfile.ZipInfo(name, ZIP_TIME)
                info.compress_type = zipfile.ZIP_DEFLATED
                info.external_attr = 0o100644 << 16
                output.writestr(info, source.read(name))
        shutil.copyfile(normalized_wheel, wheel)
    with zipfile.ZipFile(wheel) as archive:
        names = set(archive.namelist())
        packages = [name for name in names if name.endswith("/runtimeconditions.bindings.yaml")]
        if len(packages) != 1:
            fail(f"{wheel}: expected exactly one binding import package")
        prefix = packages[0].removesuffix("runtimeconditions.bindings.yaml")
        if {prefix + name for name in RESOURCE_NAMES} - names:
            fail(f"{wheel}: package data omits a required binding resource")
        for name in RESOURCE_NAMES:
            disk = package_tree / "src" / prefix / name
            if archive.read(prefix + name) != disk.read_bytes():
                fail(f"{wheel}: packaged {name} differs from assembled bytes")
    return wheel


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True, help="new or empty fixture directory")
    parser.add_argument("--python", type=Path, default=Path(sys.executable), help="Python with pinned emitter build tools")
    parser.add_argument("--go-profiler", type=Path, help="actual Go profiler executable")
    parser.add_argument("--python-profiler-wheel", type=Path, required=True, help="actual Python profiler wheel")
    parser.add_argument("--python-only", action="store_true", help="assemble all positive Python conformance wheels without Go artifacts")
    args = parser.parse_args()
    output = args.output.resolve()
    if output.exists() and any(output.iterdir()):
        fail(f"{output}: output must be new or empty")
    output.mkdir(parents=True, exist_ok=True)
    check_core()
    if not args.python_only and args.go_profiler is None:
        fail("--go-profiler is required unless --python-only is selected")
    profiler = (
        {"python": python_profiler_identity(args.python_profiler_wheel.resolve())}
        if args.python_only else
        profiler_identities(args.go_profiler.resolve(), args.python_profiler_wheel.resolve())
    )
    sys.path.insert(0, str(PYTHON_EMITTER))
    from runtimeconditions_binding_emitter import build_plan, emit_package, load_model, load_target

    environment = os.environ.copy()
    environment["GOCACHE"] = str(output / "go-build-cache")
    normalizer = output / "tools/rc-binding-model"
    build_go_binary(TOOLING / "normalizer", normalizer, environment)
    go_emitter = output / "tools/rc-go-bindings"
    if not args.python_only:
        build_go_binary(TOOLING / "emitters/go", go_emitter, environment)
    normalizer_digest = digest(normalizer.read_bytes())
    selected = PACKAGES
    go_targets = [read_yaml(GO_TARGETS / f"{target}.yaml") for _, target in selected]
    by_id = {target["rootExtension"]: target for target in go_targets}
    if len(by_id) != len(selected):
        fail("package targets contain duplicate root extension IDs")
    python_targets = {item["rootExtension"]: python_target(item, by_id) for item in go_targets}
    artifacts: dict[str, dict[str, Path]] = {"go": {}, "python": {}}
    for case, target_file in selected:
        go_target = read_yaml(GO_TARGETS / f"{target_file}.yaml")
        extension_id = go_target["rootExtension"]
        model_path, normalized, lock = normalize(
            case, go_target, output / "models" / target_file, normalizer, normalizer_digest,
        )
        if not args.python_only:
            go_package = output / "go/packages" / go_target["packageKey"]
            run(
                str(go_emitter), "--model", str(model_path),
                "--package-config", str(GO_TARGETS / f"{target_file}.yaml"),
                "--output", str(go_package),
            )
            go_dependencies = dependency_entries("go", go_target, by_id, artifacts)
            write_resources(
                go_package, case, go_target, model_path, normalized, lock,
                "go", go_dependencies, profiler["go"],
            )
            artifacts["go"][extension_id] = go_archive(go_package, go_target, output / "go/proxy")

        py_target = python_targets[extension_id]
        py_target_path = output / "targets/python" / f"{target_file}.yaml"
        write_yaml(py_target_path, py_target)
        py_package = output / "python/packages" / py_target["packageKey"]
        model = load_model(model_path)
        target = load_target(py_target_path)
        emit_package(build_plan(model, target), model, py_package)
        resources = py_package / "src" / py_target["importPackage"]
        python_dependencies = dependency_entries("python", py_target, python_targets, artifacts)
        write_resources(
            resources, case, py_target, model_path, normalized, lock,
            "python", python_dependencies, profiler["python"],
        )
        artifacts["python"][extension_id] = python_wheel(
            py_package, output / "python/wheels", args.python.absolute(),
        )
        print(f"{target_file}: Python wheel assembled" if args.python_only else
              f"{target_file}: Go module and Python wheel assembled")
    print(f"fixtures: {output}")


if __name__ == "__main__":
    main()
