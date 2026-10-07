"""Exercise the native command boundary without profiler source dependencies."""

from __future__ import annotations

import json
import os
import subprocess
import sys
from pathlib import Path

import pytest
import yaml

from runtimeconditions_binding_emitter.__main__ import main
from runtimeconditions_binding_emitter.verification import api_surface


def test_native_command_help() -> None:
    environment = os.environ.copy()
    environment["PYTHONDONTWRITEBYTECODE"] = "1"
    result = subprocess.run(
        [sys.executable, "-m", "runtimeconditions_binding_emitter", "--help"],
        env=environment,
        check=False,
        capture_output=True,
        text=True,
    )
    assert result.returncode == 0
    assert "emit,verify,archive,api,verify-installation" in result.stdout


def test_api_uses_ast_without_executing_source(tmp_path: Path) -> None:
    (tmp_path / "bindings.py").write_text(
        'raise RuntimeError("must not execute")\n'
        "class FutureShape:\n    address: str\n"
        '__all__ = ["FutureShape"]\n',
        encoding="utf-8",
    )
    assert api_surface(tmp_path) == [
        "class|FutureShape|",
        "field|FutureShape|address|str|required",
    ]


def test_cli_rejects_missing_installed_artifact(tmp_path: Path) -> None:
    assert (
        main(
            [
                "verify-installation",
                "--artifact",
                str(tmp_path / "absent.whl"),
                "--distribution",
                "missing-distribution",
            ]
        )
        == 1
    )


def test_cli_requires_a_command() -> None:
    with pytest.raises(SystemExit) as error:
        main([])
    assert error.value.code == 2


def test_production_cli_emits_no_conformance(tmp_path: Path, capsys) -> None:
    tooling = Path(__file__).resolve().parents[3]
    model = (
        tooling
        / "model/conformance/expected/01-owned-kind-interface/runtimeconditions.binding-model.yaml"
    )
    config = yaml.safe_load(
        (tooling / "emitters/python/testdata/package-target.yaml").read_text()
    )
    config["emitterSha256"] = "a" * 64
    target = tmp_path / "target.yaml"
    target.write_text(yaml.safe_dump(config), encoding="utf-8")
    output = tmp_path / "package"
    assert (
        main(
            [
                "emit",
                "--model",
                str(model),
                "--package-config",
                str(target),
                "--output",
                str(output),
                "--assembled",
            ]
        )
        == 0
    )
    capsys.readouterr()
    assert not (output / "conformance").exists()
    assert not list(output.rglob("_conformance.py"))
    package = output / "src" / config["importPackage"]
    assert (
        main(
            [
                "verify",
                "--model",
                str(model),
                "--package-config",
                str(target),
                "--input",
                str(package),
            ]
        )
        == 0
    )
    assert json.loads(capsys.readouterr().out)["modelMapping"] is True
