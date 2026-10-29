"""Check that every native emitter agrees on the language-neutral part of the binding manifest.

The manifest's declarations, imported marker contracts, and root bindings come from the
binding model, not from the target language. Each positive case under
model/conformance/expected has a manifest-neutral.yaml fixture holding that part; an
emitter conforms when its generated manifest projects to exactly the fixture.
"""

from __future__ import annotations

import argparse
import json
import os
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

import yaml

TOOLING = Path(__file__).resolve().parents[1]
EXPECTED = TOOLING / "model/conformance/expected"
REPOSITORY = "https://github.com/runtimeconditions/extensions"
ROOT_BINDING_KEYS = (
    "role",
    "modelRef",
    "declarationCoordinate",
    "scope",
    "sourceName",
    "path",
    "schemaCoordinate",
    "fixedInterfaceType",
    "vocabularyCoordinate",
)
CONTRACT_KEYS = ("modelRef", "owner", "sourceName")


def ordered(items: list[dict]) -> list[dict]:
    return sorted(items, key=lambda item: json.dumps(item, sort_keys=True))


def project(manifest: dict) -> dict:
    return {
        "declarations": ordered(
            [{k: item[k] for k in CONTRACT_KEYS} for item in manifest.get("declarations", [])]
        ),
        "importedMarkerContracts": ordered(
            [
                {k: item[k] for k in CONTRACT_KEYS}
                for item in manifest.get("importedMarkerContracts", [])
            ]
        ),
        "rootBindings": ordered(
            [
                {k: item[k] for k in ROOT_BINDING_KEYS if k in item}
                for item in manifest.get("rootBindings", [])
            ]
        ),
    }


def direct_dependencies(model: dict) -> list[str]:
    root = model["rootExtension"]["id"]
    entry = next(item for item in model["extensions"] if item["id"] == root)
    return sorted(entry.get("dependencies", []))


def target(api: str, kind: str, key: str, model: dict, extra: dict, dependency: dict) -> dict:
    root = model["rootExtension"]["id"]
    return {
        "apiVersion": f"runtimeconditions.io/{api}-package-target/v1alpha1",
        "kind": kind,
        "packageKey": key,
        "rootExtension": root,
        "publicationMode": "github-tag",
        "dependencies": [
            {"extension": item, **{k: v.format(i=i) for k, v in dependency.items()}}
            for i, item in enumerate(direct_dependencies(model))
        ],
        **extra,
    }


class Emitter:
    def __init__(self, name: str) -> None:
        self.name = name
        self.environment = dict(os.environ)

    def prepare(self, workspace: Path) -> None:
        pass


class Python(Emitter):
    def prepare(self, workspace: Path) -> None:
        source = TOOLING / "emitters/python/src"
        self.environment["PYTHONPATH"] = str(source)

    def target(self, key: str, model: dict) -> dict:
        return target(
            "python",
            "RuntimeConditionsPythonPackageTarget",
            key,
            model,
            {
                "distributionName": f"rc-{key}",
                "importPackage": f"rc_{key}",
                "version": "1.0.0",
                "sourceDirectory": f"bindings/{key}/python",
                "minimumPythonVersion": "3.11",
                "repositoryUrl": REPOSITORY,
                "emitterSha256": "0" * 64,
            },
            {"distributionName": "rc-dep-{i}", "importPackage": "rc_dep_{i}", "version": "1.0.0"},
        )

    def command(self, model: Path, config: Path, output: Path) -> list[str]:
        return [sys.executable, "-m", "runtimeconditions_binding_emitter", "emit", "--model", str(model), "--package-config", str(config), "--output", str(output)]

    def manifest(self, output: Path, key: str) -> Path:
        return output / "src" / f"rc_{key}" / "runtimeconditions.bindings.yaml"


class TypeScript(Emitter):
    def target(self, key: str, model: dict) -> dict:
        return target(
            "typescript",
            "RuntimeConditionsTypeScriptPackageTarget",
            key,
            model,
            {
                "packageName": f"@rc/{key}",
                "version": "1.0.0",
                "sourceDirectory": f"bindings/{key}/typescript",
                "minimumNodeVersion": "20.19",
                "repositoryUrl": REPOSITORY,
                "emitterSha256": "0" * 64,
            },
            {"packageName": "@rc/dep-{i}", "version": "1.0.0"},
        )

    def command(self, model: Path, config: Path, output: Path) -> list[str]:
        main = TOOLING / "emitters/typescript/dist/src/main.js"
        if not main.is_file():
            raise SystemExit(f"build the TypeScript emitter first: npm ci && npm run build in {main.parents[2]}")
        return ["node", str(main), "emit", "--model", str(model), "--package-config", str(config), "--output", str(output)]

    def manifest(self, output: Path, key: str) -> Path:
        return output / "runtimeconditions" / "runtimeconditions.bindings.yaml"


class Rust(Emitter):
    def prepare(self, workspace: Path) -> None:
        crate = TOOLING / "emitters/rust"
        subprocess.run(["cargo", "build", "--release", "--locked", "--target-dir", str(workspace / "rust")], cwd=crate, check=True)
        self.binary = workspace / "rust/release/runtimeconditions-binding-emitter-rust"

    def target(self, key: str, model: dict) -> dict:
        return target(
            "rust",
            "RuntimeConditionsRustPackageTarget",
            key,
            model,
            {
                "crateName": f"rc-{key}",
                "version": "1.0.0",
                "sourceDirectory": f"bindings/{key}/rust",
                "minimumRustVersion": "1.75",
                "repositoryUrl": REPOSITORY,
                "emitterSha256": "0" * 64,
            },
            {"crateName": "rc-dep-{i}", "version": "1.0.0"},
        )

    def command(self, model: Path, config: Path, output: Path) -> list[str]:
        return [str(self.binary), "emit", "--model", str(model), "--package-config", str(config), "--output", str(output)]

    def manifest(self, output: Path, key: str) -> Path:
        return output / "runtimeconditions" / "runtimeconditions.bindings.yaml"


EMITTERS = {"python": Python, "typescript": TypeScript, "rust": Rust}


def cases(selected: list[str]) -> list[str]:
    found = sorted(path.parent.name for path in EXPECTED.glob("*/runtimeconditions.binding-model.yaml"))
    unknown = sorted(set(selected) - set(found))
    if unknown:
        raise SystemExit(f"unknown cases: {', '.join(unknown)}")
    return selected or found


def generate(emitter: Emitter, workspace: Path, case: str) -> dict:
    key = "case" + case.split("-")[0]
    model_path = EXPECTED / case / "runtimeconditions.binding-model.yaml"
    model = yaml.safe_load(model_path.read_text(encoding="utf-8"))
    config = workspace / f"{emitter.name}-{case}.yaml"
    config.write_text(yaml.safe_dump(emitter.target(key, model)), encoding="utf-8")
    output = workspace / f"{emitter.name}-{case}"
    shutil.rmtree(output, ignore_errors=True)
    result = subprocess.run(emitter.command(model_path, config, output), capture_output=True, text=True, env=emitter.environment)
    if result.returncode:
        raise SystemExit(f"{emitter.name} failed on {case}:\n{result.stderr or result.stdout}")
    return project(yaml.safe_load(emitter.manifest(output, key).read_text(encoding="utf-8")))


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--emitters", default=",".join(EMITTERS), help="comma-separated emitters to check")
    parser.add_argument("--case", action="append", default=[], help="limit to a case; repeatable")
    parser.add_argument("--update", action="store_true", help="rewrite the fixtures from the first selected emitter")
    arguments = parser.parse_args()
    names = [name.strip() for name in arguments.emitters.split(",") if name.strip()]
    unknown = sorted(set(names) - set(EMITTERS))
    if unknown or not names:
        raise SystemExit(f"choose emitters from {', '.join(EMITTERS)}")
    failures = []
    with tempfile.TemporaryDirectory(prefix="emitter-conformance-") as temporary:
        workspace = Path(temporary)
        emitters = [EMITTERS[name](name) for name in names]
        for emitter in emitters:
            emitter.prepare(workspace)
        for case in cases(arguments.case):
            fixture = EXPECTED / case / "manifest-neutral.yaml"
            for emitter in emitters:
                actual = generate(emitter, workspace, case)
                if arguments.update:
                    fixture.write_text(yaml.safe_dump(actual, sort_keys=True), encoding="utf-8")
                    print(f"wrote {fixture.relative_to(TOOLING)} from {emitter.name}")
                    break
                expected = yaml.safe_load(fixture.read_text(encoding="utf-8"))
                status = "ok" if actual == expected else "DIFFERS"
                print(f"{status:8} {emitter.name:11} {case}")
                if actual != expected:
                    failures.append(f"{emitter.name}: {case}")
    if failures:
        raise SystemExit("emitters disagree with the fixtures: " + ", ".join(failures))


if __name__ == "__main__":
    main()
