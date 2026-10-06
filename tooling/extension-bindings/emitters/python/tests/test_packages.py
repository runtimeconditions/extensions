"""Step 5 wheel, sdist, and installed-package verification."""

from __future__ import annotations

import io
import json
import os
import shutil
import stat
import struct
import subprocess
import sys
import tarfile
import zipfile
from copy import deepcopy
from dataclasses import replace
from hashlib import sha256
from pathlib import Path

import pytest
import yaml
from packaging.requirements import Requirement

from runtimeconditions_binding_emitter import (
    build_archives,
    build_plan,
    emit_package,
    load_model,
    load_target,
    verify_sdist,
    verify_wheel,
)
from runtimeconditions_binding_emitter.archive import ARCHIVE_EPOCH, ArchiveArtifacts
from runtimeconditions_binding_emitter.package import DiagnosticError, PackageDependency

TOOLING = Path(__file__).resolve().parents[3]
MODELS = TOOLING / "model/conformance/expected"
TARGET = TOOLING / "emitters/python/testdata/package-target.yaml"
LOCK = yaml.safe_load(
    Path(
        os.environ.get(
            "RC_BINDINGS_TEST_TOOLCHAIN_LOCK", str(TOOLING / "toolchain.lock.yaml")
        )
    ).read_text(encoding="utf-8")
)
BUILD_PYTHON = Path(
    os.environ.get("RC_BINDINGS_BUILD_PYTHON", sys.executable)
).absolute()


def _run(*arguments: str, cwd: Path | None = None) -> str:
    environment = os.environ.copy()
    environment.pop("PYTHONPATH", None)
    result = subprocess.run(
        arguments, cwd=cwd, env=environment, check=True, capture_output=True, text=True
    )
    return result.stdout


def _model(case: str) -> dict:
    return load_model(MODELS / case / "runtimeconditions.binding-model.yaml")


def _development_target():
    # Development fixtures identify the exact source snapshot executed here.
    source = TOOLING / "emitters/python"
    paths = [
        source / "pyproject.toml",
        *sorted((source / "src/runtimeconditions_binding_emitter").glob("*.py")),
    ]
    inventory = {
        path.relative_to(source).as_posix(): sha256(path.read_bytes()).hexdigest()
        for path in paths
    }
    digest = sha256(
        json.dumps(inventory, sort_keys=True, separators=(",", ":")).encode("utf-8")
    ).hexdigest()
    return replace(load_target(TARGET), emitter_sha256=digest)


def _plan(case: str):
    model = _model(case)
    root = model["rootExtension"]["id"]
    dependencies = next(
        item.get("dependencies", [])
        for item in model["extensions"]
        if item["id"] == root
    )
    target = replace(
        _development_target(),
        root_extension=root,
        dependencies=tuple(
            PackageDependency(item, f"test-{index}", f"test_{index}", "1.0.0")
            for index, item in enumerate(dependencies)
        ),
    )
    return model, build_plan(model, target)


def _build(model: dict, plan, root: Path, label: str) -> ArchiveArtifacts:
    source = root / f"{label}-source"
    emit_package(plan, model, source)
    artifacts = build_archives(
        plan, model, source, root / f"{label}-dist", BUILD_PYTHON, LOCK
    )
    assert sorted(
        path.relative_to(source).as_posix()
        for path in source.rglob("*")
        if path.is_file()
    ) == sorted(
        [
            "pyproject.toml",
            *(
                f"src/{plan.target.import_package}/{name}"
                for name in (
                    "__init__.py",
                    "bindings.py",
                    "py.typed",
                    "runtimeconditions.bindings.yaml",
                )
            ),
        ]
    )
    return artifacts


@pytest.mark.parametrize(
    "case",
    sorted(
        path.parent.name
        for path in MODELS.glob("*/runtimeconditions.binding-model.yaml")
    ),
)
def test_archives_are_reproducible_and_contain_only_declared_files(
    case: str, tmp_path: Path
) -> None:
    model, plan = _plan(case)
    first = _build(model, plan, tmp_path, "first")
    second = _build(model, plan, tmp_path, "second")
    assert first.wheel.read_bytes() == second.wheel.read_bytes()
    assert first.sdist.read_bytes() == second.sdist.read_bytes()
    with zipfile.ZipFile(first.wheel) as wheel:
        assert wheel.namelist() == sorted(wheel.namelist())
        assert all(
            entry.date_time == (1980, 1, 1, 0, 0, 0) for entry in wheel.infolist()
        )
        assert all(
            entry.compress_type == zipfile.ZIP_DEFLATED
            and entry.external_attr >> 16 == stat.S_IFREG | 0o644
            for entry in wheel.infolist()
        )
    assert struct.unpack("<I", first.sdist.read_bytes()[4:8])[0] == ARCHIVE_EPOCH
    with tarfile.open(first.sdist, "r:gz") as sdist:
        assert [member.name for member in sdist.getmembers()] == sorted(
            member.name for member in sdist.getmembers()
        )
        assert all(
            member.mtime == ARCHIVE_EPOCH and member.uid == 0 and member.gid == 0
            for member in sdist.getmembers()
        )
        assert all(
            member.mode == (0o755 if member.isdir() else 0o644)
            for member in sdist.getmembers()
        )


def test_build_toolchain_must_match_lock(tmp_path: Path) -> None:
    model, plan = _plan("01-owned-kind-interface")
    source = tmp_path / "source"
    emit_package(plan, model, source)
    wrong_lock = deepcopy(LOCK)
    wrong_lock["python"]["tools"]["wheel"] = "0.0.0"
    with pytest.raises(DiagnosticError) as error:
        build_archives(plan, model, source, tmp_path / "dist", BUILD_PYTHON, wrong_lock)
    assert error.value.diagnostic.code == "RCP4002"
    wrong_lock = deepcopy(LOCK)
    wrong_lock["python"]["interpreterVersion"] = "0.0.0"
    with pytest.raises(DiagnosticError) as error:
        build_archives(plan, model, source, tmp_path / "dist", BUILD_PYTHON, wrong_lock)
    assert error.value.diagnostic.code == "RCP4002"


def test_prerelease_package_archive_names(tmp_path: Path) -> None:
    model, plan = _plan("01-owned-kind-interface")
    plan = replace(plan, target=replace(plan.target, version="1.2.3-rc.4"))
    artifacts = _build(model, plan, tmp_path, "prerelease")
    assert artifacts.wheel.name.endswith("-1.2.3rc4-py3-none-any.whl")
    assert artifacts.sdist.name.endswith("-1.2.3rc4.tar.gz")


def test_undeclared_source_wheel_and_sdist_files_are_rejected(tmp_path: Path) -> None:
    model, plan = _plan("01-owned-kind-interface")
    source = tmp_path / "source"
    emit_package(plan, model, source)
    rogue = source / "src" / plan.target.import_package / "rogue.py"
    rogue.write_text("pass\n", encoding="utf-8")
    with pytest.raises(DiagnosticError) as error:
        build_archives(plan, model, source, tmp_path / "rejected", BUILD_PYTHON, LOCK)
    assert error.value.diagnostic.code == "RCP4004"
    rogue.unlink()
    artifacts = build_archives(
        plan, model, source, tmp_path / "accepted", BUILD_PYTHON, LOCK
    )
    with zipfile.ZipFile(artifacts.wheel, "a") as wheel:
        wheel.writestr("rogue.py", "pass\n")
    with pytest.raises(DiagnosticError) as error:
        verify_wheel(artifacts.wheel, plan, model)
    assert error.value.diagnostic.code == "RCP4004"

    changed = tmp_path / "changed"
    changed.mkdir()
    record_mismatch = changed / artifacts.wheel.name
    with zipfile.ZipFile(artifacts.wheel) as original:
        with zipfile.ZipFile(record_mismatch, "w") as output:
            for name in original.namelist():
                if name == "rogue.py":
                    continue
                data = original.read(name)
                if name.endswith(".dist-info/METADATA"):
                    data += b"\n"
                output.writestr(name, data)
    with pytest.raises(DiagnosticError) as error:
        verify_wheel(record_mismatch, plan, model)
    assert error.value.diagnostic.code == "RCP4004"

    altered_sdist = changed / artifacts.sdist.name
    with tarfile.open(artifacts.sdist, "r:gz") as original:
        with tarfile.open(altered_sdist, "w:gz") as output:
            for member in original.getmembers():
                output.addfile(
                    member, original.extractfile(member) if member.isfile() else None
                )
            member = tarfile.TarInfo(
                artifacts.sdist.name.removesuffix(".tar.gz") + "/rogue.py"
            )
            data = b"pass\n"
            member.size = len(data)
            output.addfile(member, io.BytesIO(data))
    with pytest.raises(DiagnosticError) as error:
        verify_sdist(altered_sdist, plan, model)
    assert error.value.diagnostic.code == "RCP4004"

    sources_mismatch = tmp_path / "sources-mismatch"
    sources_mismatch.mkdir()
    altered_sdist = sources_mismatch / artifacts.sdist.name
    with tarfile.open(artifacts.sdist, "r:gz") as original:
        with tarfile.open(altered_sdist, "w:gz") as output:
            for member in original.getmembers():
                if member.isfile() and member.name.endswith(".egg-info/SOURCES.txt"):
                    stream = original.extractfile(member)
                    assert stream is not None
                    data = stream.read() + b"rogue.py\n"
                    member.size = len(data)
                    output.addfile(member, io.BytesIO(data))
                else:
                    output.addfile(
                        member,
                        original.extractfile(member) if member.isfile() else None,
                    )
    with pytest.raises(DiagnosticError) as error:
        verify_sdist(altered_sdist, plan, model)
    assert error.value.diagnostic.code == "RCP4004"


def _new_installation(root: Path) -> Path:
    environment = root / "venv"
    _run(str(BUILD_PYTHON), "-m", "venv", "--without-pip", str(environment))
    return environment / "bin" / "python"


def _install(python: Path, wheelhouse: Path, distribution: str, version: str) -> None:
    _run(
        str(BUILD_PYTHON),
        "-m",
        "pip",
        "--python",
        str(python),
        "install",
        "--no-index",
        "--find-links",
        str(wheelhouse),
        f"{distribution}=={version}",
    )
    _run(str(BUILD_PYTHON), "-m", "pip", "--python", str(python), "check")


def _installed(python: Path, packages: list[tuple[str, str]]) -> list[dict]:
    script = """
import importlib, importlib.metadata as metadata, importlib.resources as resources
import json, sys
result = []
for distribution_name, package_name in json.loads(sys.argv[1]):
    distribution = metadata.distribution(distribution_name)
    package = resources.files(package_name)
    importlib.import_module(package_name)
    result.append({
        'name': distribution.metadata['Name'],
        'version': distribution.version,
        'requires': distribution.requires or [],
        'files': [str(item) for item in distribution.files or []],
        'manifest': package.joinpath('runtimeconditions.bindings.yaml').read_text(encoding='utf-8'),
        'py_typed': package.joinpath('py.typed').read_bytes().hex(),
        'conformance': package.joinpath('_conformance.py').is_file(),
    })
print(json.dumps(result))
"""
    return json.loads(_run(str(python), "-c", script, json.dumps(packages)))


def _assert_installed(snapshot: list[dict], specs: list[tuple[dict, object]]) -> None:
    for installed, (model, plan) in zip(snapshot, specs, strict=True):
        assert installed["name"] == plan.target.distribution_name
        assert installed["version"] == plan.target.version
        package_prefix = plan.target.import_package + "/"
        assert package_prefix + "runtimeconditions.bindings.yaml" in installed["files"]
        assert package_prefix + "py.typed" in installed["files"]
        assert package_prefix + "_conformance.py" not in installed["files"]
        assert installed["py_typed"] == ""
        assert installed["conformance"] is False
        manifest = yaml.safe_load(installed["manifest"])
        assert manifest["extension"]["id"] == model["rootExtension"]["id"]
        assert (
            manifest["extension"]["semanticSha256"]
            == model["rootExtension"]["semanticSha256"]
        )
        assert (
            manifest["model"]["semanticSha256"] == model["metadata"]["semanticSha256"]
        )
        assert {Requirement(item).name for item in installed["requires"]} == {
            item.distribution_name for item in plan.target.dependencies
        }


def test_clean_wheel_install_and_sdist_rebuild(tmp_path: Path) -> None:
    model, plan = _plan("01-owned-kind-interface")
    artifacts = _build(model, plan, tmp_path, "root")
    installed_python = _new_installation(tmp_path / "wheel-install")
    _install(
        installed_python,
        artifacts.wheel.parent,
        plan.target.distribution_name,
        plan.target.version,
    )
    _assert_installed(
        _installed(
            installed_python,
            [(plan.target.distribution_name, plan.target.import_package)],
        ),
        [(model, plan)],
    )

    extracted = tmp_path / "sdist-extracted"
    extracted.mkdir()
    verify_sdist(artifacts.sdist, plan, model)
    with tarfile.open(artifacts.sdist, "r:gz") as archive:
        for member in archive.getmembers():
            if member.isfile():
                destination = extracted / member.name
                destination.parent.mkdir(parents=True, exist_ok=True)
                stream = archive.extractfile(member)
                assert stream is not None
                destination.write_bytes(stream.read())
    rebuilt = tmp_path / "sdist-rebuilt"
    rebuilt.mkdir()
    source = next(extracted.iterdir())
    _run(
        str(BUILD_PYTHON),
        "-m",
        "build",
        "--wheel",
        "--no-isolation",
        "--outdir",
        str(rebuilt),
        str(source),
    )
    rebuilt_wheel = next(rebuilt.glob("*.whl"))
    verify_wheel(rebuilt_wheel, plan, model)
    rebuilt_python = _new_installation(tmp_path / "sdist-install")
    _install(
        rebuilt_python, rebuilt, plan.target.distribution_name, plan.target.version
    )
    _assert_installed(
        _installed(
            rebuilt_python,
            [(plan.target.distribution_name, plan.target.import_package)],
        ),
        [(model, plan)],
    )


def _target(root: str, key: str, dependencies: tuple[PackageDependency, ...] = ()):
    base = _development_target()
    return replace(
        base,
        package_key=key,
        source_directory=f"bindings/{key}/python",
        root_extension=root,
        distribution_name=key,
        import_package=key.replace("-", "_"),
        dependencies=dependencies,
    )


def _build_wheelhouse(specs: list[tuple[dict, object]], tmp_path: Path) -> Path:
    wheelhouse = tmp_path / "wheelhouse"
    wheelhouse.mkdir()
    for index, (model, plan) in enumerate(specs):
        artifacts = _build(model, plan, tmp_path, f"dependency-{index}")
        shutil.copy2(artifacts.wheel, wheelhouse / artifacts.wheel.name)
    return wheelhouse


def test_installed_direct_additive_dependency(tmp_path: Path) -> None:
    model = _model("02-additive-field")
    base_id = model["vocabulary"]["importedDeclarations"][0]["owner"]
    base_model = deepcopy(model)
    base_extension = next(item for item in model["extensions"] if item["id"] == base_id)
    base_model["rootExtension"] = dict(base_extension)
    base_model["extensions"] = [base_extension]
    base_model["vocabulary"]["ownedDeclarations"] = base_model["vocabulary"][
        "importedDeclarations"
    ]
    base_model["vocabulary"]["importedDeclarations"] = []
    base_target = _target(base_id, "owner-binding")
    base_plan = build_plan(base_model, base_target)
    addon_target = _target(
        model["rootExtension"]["id"],
        "direct-addon-binding",
        (
            PackageDependency(
                base_id,
                base_target.distribution_name,
                base_target.import_package,
                base_target.version,
            ),
        ),
    )
    addon_plan = build_plan(model, addon_target)
    specs = [(base_model, base_plan), (model, addon_plan)]
    wheelhouse = _build_wheelhouse(specs, tmp_path)
    incomplete = tmp_path / "incomplete-wheelhouse"
    incomplete.mkdir()
    shutil.copy2(next(wheelhouse.glob("direct_addon_binding-*.whl")), incomplete)
    missing_python = _new_installation(tmp_path / "missing-direct-install")
    with pytest.raises(subprocess.CalledProcessError):
        _install(
            missing_python,
            incomplete,
            addon_target.distribution_name,
            addon_target.version,
        )
    python = _new_installation(tmp_path / "direct-install")
    _install(python, wheelhouse, addon_target.distribution_name, addon_target.version)
    _assert_installed(
        _installed(
            python,
            [
                (plan.target.distribution_name, plan.target.import_package)
                for _, plan in specs
            ],
        ),
        specs,
    )
    _run(
        str(python),
        "-c",
        "import owner_binding as owner, direct_addon_binding as addon; "
        "assert addon.ServiceField is owner.ServiceField; "
        "assert addon.service is owner.service; "
        "assert isinstance(owner.service(addon.Credential(token='sample')), owner.Declaration)",
    )


def test_installed_transitive_additive_dependency(tmp_path: Path) -> None:
    model = _model("03-transitive-closure")
    leaf_id = model["vocabulary"]["importedDeclarations"][0]["owner"]
    middle_extension = next(
        item for item in model["extensions"] if leaf_id in item.get("dependencies", [])
    )
    middle_id = middle_extension["id"]
    leaf_extension = next(item for item in model["extensions"] if item["id"] == leaf_id)

    leaf_model = deepcopy(model)
    leaf_model["rootExtension"] = dict(leaf_extension)
    leaf_model["extensions"] = [leaf_extension]
    leaf_model["vocabulary"]["ownedDeclarations"] = leaf_model["vocabulary"][
        "importedDeclarations"
    ]
    leaf_model["vocabulary"]["importedDeclarations"] = []
    leaf_target = _target(leaf_id, "leaf-binding")
    leaf_plan = build_plan(leaf_model, leaf_target)

    middle_model = deepcopy(model)
    middle_model["rootExtension"] = dict(middle_extension)
    middle_model["extensions"] = [leaf_extension, middle_extension]
    middle_target = _target(
        middle_id,
        "middle-binding",
        (
            PackageDependency(
                leaf_id,
                leaf_target.distribution_name,
                leaf_target.import_package,
                leaf_target.version,
            ),
        ),
    )
    middle_plan = build_plan(middle_model, middle_target)

    root_target = _target(
        model["rootExtension"]["id"],
        "root-binding",
        (
            PackageDependency(
                middle_id,
                middle_target.distribution_name,
                middle_target.import_package,
                middle_target.version,
            ),
        ),
    )
    root_plan = build_plan(model, root_target)
    specs = [(leaf_model, leaf_plan), (middle_model, middle_plan), (model, root_plan)]
    wheelhouse = _build_wheelhouse(specs, tmp_path)
    incomplete = tmp_path / "incomplete-wheelhouse"
    incomplete.mkdir()
    for name in ("middle_binding-*.whl", "root_binding-*.whl"):
        shutil.copy2(next(wheelhouse.glob(name)), incomplete)
    missing_python = _new_installation(tmp_path / "missing-transitive-install")
    with pytest.raises(subprocess.CalledProcessError):
        _install(
            missing_python,
            incomplete,
            root_target.distribution_name,
            root_target.version,
        )
    python = _new_installation(tmp_path / "transitive-install")
    _install(python, wheelhouse, root_target.distribution_name, root_target.version)
    _assert_installed(
        _installed(
            python,
            [
                (plan.target.distribution_name, plan.target.import_package)
                for _, plan in specs
            ],
        ),
        specs,
    )
    _run(
        str(python),
        "-c",
        "import leaf_binding as leaf, middle_binding as middle, "
        "root_binding as root; assert root.WorkerField is middle.WorkerField "
        "is leaf.WorkerField; assert root.worker is leaf.worker",
    )
