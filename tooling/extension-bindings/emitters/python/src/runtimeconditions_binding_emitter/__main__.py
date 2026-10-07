"""Native emitter commands invoked by the shared Go orchestrator."""

from __future__ import annotations

import argparse
import json
import sys
import zipfile
from hashlib import sha256
from importlib import metadata
from pathlib import Path
from typing import Any

import yaml

from .archive import build_archives
from .emitter import build_plan
from .package import DiagnosticError, load_model, load_target
from .source import emit_package, render_assembly_metadata
from .verification import api_surface, verify_package


def verify_installation(artifact: Path, distribution: str) -> dict[str, str]:
    """Bind installed distribution bytes to a separately supplied release wheel."""
    installed = metadata.distribution(distribution)
    with zipfile.ZipFile(artifact) as wheel:
        names = wheel.namelist()
        if len(names) != len(set(names)):
            raise ValueError("release wheel contains duplicate entries")
        wheel_metadata = next(
            name for name in names if name.endswith(".dist-info/METADATA")
        )
        from email.parser import BytesParser

        info = BytesParser().parsebytes(wheel.read(wheel_metadata))

        def normalize(name: str) -> str:
            return name.lower().replace("_", "-").replace(".", "-")

        if (
            normalize(info["Name"]) != normalize(distribution)
            or info["Version"] != installed.version
        ):
            raise ValueError(
                "release wheel identity differs from installed distribution"
            )
        for name in names:
            if name.endswith("/") or ".dist-info/" in name:
                continue
            if ".data/" in name or name.startswith("/") or ".." in Path(name).parts:
                raise ValueError("unsupported or unsafe release wheel layout")
            path = Path(str(installed.locate_file(name)))
            if path.is_symlink() or path.read_bytes() != wheel.read(name):
                raise ValueError(
                    f"installed distribution differs from release artifact: {name}"
                )
    return {
        "version": installed.version,
        "sha256": sha256(artifact.read_bytes()).hexdigest(),
    }


def main(arguments: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="runtimeconditions-binding-emitter")
    parser.add_argument("--version", action="version", version="0.1.0")
    commands = parser.add_subparsers(dest="command", required=True)
    for command in ("emit", "verify", "archive", "api"):
        child = commands.add_parser(command)
        if command != "api":
            child.add_argument("--model", required=True, type=Path)
            child.add_argument("--package-config", required=True, type=Path)
        child.add_argument(
            "--input" if command != "emit" else "--output", required=True, type=Path
        )
        if command == "emit":
            child.add_argument("--assembled", action="store_true")
        if command == "archive":
            child.add_argument("--output", required=True, type=Path)
            child.add_argument("--toolchain-lock", required=True, type=Path)
            child.add_argument("--build-python", default=sys.executable)
    child = commands.add_parser("verify-installation")
    child.add_argument("--artifact", required=True, type=Path)
    child.add_argument("--distribution", required=True)
    args = parser.parse_args(arguments)
    result: dict[str, Any]
    try:
        if args.command == "verify-installation":
            result = verify_installation(args.artifact, args.distribution)
        elif args.command == "api":
            result = {"api": api_surface(args.input)}
        else:
            model, target = load_model(args.model), load_target(args.package_config)
            plan = build_plan(model, target)
            if args.command == "emit":
                emit_package(plan, model, args.output)
                if args.assembled:
                    for name, content in render_assembly_metadata().items():
                        (args.output / name).write_text(content, encoding="utf-8")
                result = {"output": str(args.output)}
            elif args.command == "verify":
                result = verify_package(plan, args.input, model)
            else:
                lock = yaml.safe_load(args.toolchain_lock.read_text(encoding="utf-8"))
                artifacts = build_archives(
                    plan,
                    model,
                    args.input,
                    args.output,
                    args.build_python,
                    lock,
                    assembled=True,
                )
                result = {"wheel": str(artifacts.wheel), "sdist": str(artifacts.sdist)}
        print(json.dumps(result, sort_keys=True))
        return 0
    except (
        DiagnosticError,
        ValueError,
        OSError,
        metadata.PackageNotFoundError,
    ) as error:
        print(str(error), file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
