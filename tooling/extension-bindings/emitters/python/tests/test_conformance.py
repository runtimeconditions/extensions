"""Phase 3 schema, source, AST, and three-run conformance gates."""

from __future__ import annotations

import ast
import json
import os
import subprocess
import sys
import tomllib
from copy import deepcopy
from dataclasses import replace
from hashlib import sha256
from pathlib import Path

import pytest
import yaml
from jsonschema import Draft202012Validator
from packaging.requirements import Requirement
from packaging.specifiers import SpecifierSet

from runtimeconditions_binding_emitter import (
    build_plan,
    emit_package,
    load_model,
    load_target,
    render_sources,
)
from runtimeconditions_binding_emitter.source import _conformance_source
from runtimeconditions_binding_emitter.verification import verify_package
from runtimeconditions_binding_emitter.package import DiagnosticError, PackageDependency

TOOLING = Path(__file__).resolve().parents[3]
MODELS = TOOLING / "model/conformance/expected"
TARGET = TOOLING / "emitters/python/testdata/package-target.yaml"
CASES = sorted(
    path.parent.name for path in MODELS.glob("*/runtimeconditions.binding-model.yaml")
)


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


def _plan(model: dict):
    root = model["rootExtension"]["id"]
    direct = next(
        item.get("dependencies", [])
        for item in model["extensions"]
        if item["id"] == root
    )
    target = replace(
        _development_target(),
        root_extension=root,
        dependencies=tuple(
            PackageDependency(extension, f"test-{index}", f"test_{index}", "1.0.0")
            for index, extension in enumerate(direct)
        ),
    )
    return build_plan(model, target)


def _files(directory: Path) -> dict[str, bytes]:
    return {
        path.relative_to(directory).as_posix(): path.read_bytes()
        for path in directory.rglob("*")
        if path.is_file()
    }


def _stage_dependency_sources(model: dict, plan, source_root: Path) -> None:
    """Place owner and provider packages beside the root for strict import checking."""
    graph = {item["id"]: item for item in model["extensions"]}
    root = plan.target.root_extension
    names = {item.extension: item.import_package for item in plan.target.dependencies}
    pending = list(names)
    while pending:
        extension = pending.pop(0)
        for dependency in graph[extension].get("dependencies", []):
            if dependency not in names:
                names[dependency] = f"test_{len(names)}"
                pending.append(dependency)

    for extension in sorted(names):
        dependency_model = deepcopy(model)
        dependency_model["rootExtension"] = dict(graph[extension])
        imported = model["vocabulary"].get("importedDeclarations", [])
        dependency_model["vocabulary"]["ownedDeclarations"] = [
            item for item in imported if item["owner"] == extension
        ]
        dependency_model["vocabulary"]["importedDeclarations"] = [
            item
            for item in imported
            if item["owner"] != extension
            and item["owner"] in names
            and item["owner"] != root
        ]
        target = replace(
            _development_target(),
            root_extension=extension,
            package_key=f"dependency-{names[extension]}",
            distribution_name=names[extension].replace("_", "-"),
            import_package=names[extension],
            dependencies=tuple(
                PackageDependency(
                    item, names[item].replace("_", "-"), names[item], "1.0.0"
                )
                for item in graph[extension].get("dependencies", [])
            ),
        )
        dependency_plan = build_plan(dependency_model, target)
        for relative, content in render_sources(
            dependency_plan, dependency_model
        ).items():
            destination = source_root / relative.removeprefix("src/")
            destination.parent.mkdir(parents=True, exist_ok=True)
            destination.write_text(content, encoding="utf-8")


def _exports(tree: ast.Module) -> list[str]:
    assignments = [
        node
        for node in tree.body
        if isinstance(node, ast.Assign)
        and any(
            isinstance(target, ast.Name) and target.id == "__all__"
            for target in node.targets
        )
    ]
    assert len(assignments) == 1
    names = ast.literal_eval(assignments[0].value)
    assert names == sorted(set(names))
    return names


def _qualified(node: ast.expr) -> str:
    assert isinstance(node, ast.Attribute)
    assert isinstance(node.value, ast.Name) and node.value.id == "b"
    return node.attr


def _check_public_ast(plan, package: Path) -> None:
    """Compare declarations, signatures, fields and domains to the model plan."""
    init = ast.parse((package / "__init__.py").read_text(encoding="utf-8"))
    bindings = ast.parse((package / "bindings.py").read_text(encoding="utf-8"))
    expected = {item.name for item in plan.types}
    for declaration in (*plan.declarations, *plan.imported_declarations):
        expected.update((declaration.function, declaration.protocol))
    if plan.declarations:
        expected.add("Declaration")
    if plan.uses_json_value:
        expected.add("JSONValue")
    assert set(_exports(init)) == expected
    assert _exports(bindings) == _exports(init)
    imported = next(
        node
        for node in init.body
        if isinstance(node, ast.ImportFrom) and node.module == "bindings"
    )
    assert {alias.name for alias in imported.names} == expected

    classes = {
        node.name: node for node in bindings.body if isinstance(node, ast.ClassDef)
    }
    functions = {
        node.name: node for node in bindings.body if isinstance(node, ast.FunctionDef)
    }
    aliases = {
        node.target.id: node
        for node in bindings.body
        if isinstance(node, ast.AnnAssign) and isinstance(node.target, ast.Name)
    }
    assert set(classes) == {
        item.name for item in plan.types if item.kind != "alias"
    } | {declaration.protocol for declaration in plan.declarations} | (
        {"Declaration"} if plan.declarations else set()
    )
    assert set(functions) == {declaration.function for declaration in plan.declarations}
    assert set(aliases) == {
        item.name for item in plan.types if item.kind == "alias"
    } | ({"JSONValue"} if plan.uses_json_value else set())

    for declaration in plan.declarations:
        protocol = classes[declaration.protocol]
        assert [ast.unparse(base) for base in protocol.bases] == ["Protocol"]
        methods = [node for node in protocol.body if isinstance(node, ast.FunctionDef)]
        assert [method.name for method in methods] == [declaration.method]
        function = functions[declaration.function]
        assert function.args.vararg is not None
        assert function.args.vararg.arg == "fields"
        assert ast.unparse(function.args.vararg.annotation) == declaration.protocol
        assert ast.unparse(function.returns) == "Declaration"
    for declaration in plan.imported_declarations:
        assert declaration.function not in functions
        assert declaration.protocol not in classes

    for item in plan.types:
        if item.kind == "alias":
            assert ast.unparse(aliases[item.name].annotation) == "TypeAlias"
            assert ast.literal_eval(aliases[item.name].value) == item.annotation
            continue
        body = classes[item.name].body
        if item.kind == "enum":
            assert [ast.unparse(base) for base in classes[item.name].bases] == [
                "StrEnum"
            ]
            assert [
                (node.targets[0].id, ast.literal_eval(node.value))
                for node in body
                if isinstance(node, ast.Assign)
                and isinstance(node.targets[0], ast.Name)
            ] == list(item.members)
            continue
        assert [
            ast.unparse(decorator) for decorator in classes[item.name].decorator_list
        ] == ["dataclass(frozen=True, kw_only=True)"]
        fields = [node for node in body if isinstance(node, ast.AnnAssign)]
        assert [
            (
                node.target.id,
                ast.unparse(node.annotation),
                node.value is not None and ast.unparse(node.value) == "None",
            )
            for node in fields
        ] == [
            (field.name, field.annotation, field.default_none) for field in item.fields
        ]
        markers = [node.name for node in body if isinstance(node, ast.FunctionDef)]
        assert markers == ([item.marker.method] if item.marker else [])


def _check_conformance_ast(plan, package: Path, model: dict) -> None:
    # This synthetic consumer is temporary test input, never emitter output.
    source = subprocess.run(
        [
            sys.executable,
            "-m",
            "ruff",
            "format",
            "--stdin-filename",
            "_conformance.py",
            "-",
        ],
        input=_conformance_source(plan, model),
        text=True,
        capture_output=True,
        check=True,
    ).stdout
    (package / "_conformance.py").write_text(source, encoding="utf-8")
    tree = ast.parse((package / "_conformance.py").read_text(encoding="utf-8"))
    exercise = next(
        node
        for node in tree.body
        if isinstance(node, ast.FunctionDef) and node.name == "exercise"
    )
    calls: dict[str, list[ast.Call]] = {}
    samples: dict[str, list[ast.expr]] = {}
    members: set[tuple[str, str]] = set()
    for node in ast.walk(exercise):
        if (
            isinstance(node, ast.Call)
            and isinstance(node.func, ast.Attribute)
            and isinstance(node.func.value, ast.Name)
            and node.func.value.id == "b"
        ):
            calls.setdefault(node.func.attr, []).append(node)
        if (
            isinstance(node, ast.AnnAssign)
            and isinstance(node.annotation, ast.Attribute)
            and node.value is not None
        ):
            samples.setdefault(_qualified(node.annotation), []).append(node.value)
        if (
            isinstance(node, ast.Attribute)
            and isinstance(node.value, ast.Attribute)
            and isinstance(node.value.value, ast.Name)
            and node.value.value.id == "b"
        ):
            members.add((node.value.attr, node.attr))
    for declaration in (*plan.declarations, *plan.imported_declarations):
        if not calls.get(declaration.function):
            fields = [
                item
                for item in plan.types
                if item.is_declaration_field
                and item.marker
                and item.marker.protocol == declaration.protocol
            ]
            assert not fields, declaration.coordinate
            # Declaration-only packages retain a function reference. Their
            # complete positive calls are supplied by installed consumer
            # fixtures, which can also import the interface/field provider.
            assert any(
                isinstance(node, ast.Assign)
                and isinstance(node.value, ast.Attribute)
                and isinstance(node.value.value, ast.Name)
                and node.value.value.id == "b"
                and node.value.attr == declaration.function
                and any(
                    isinstance(target, ast.Name) and target.id.startswith("_deferred_")
                    for target in node.targets
                )
                for node in exercise.body
            ), declaration.coordinate
    for item in plan.types:
        if item.kind == "object":
            assert calls.get(item.name), item.coordinate
            for field in item.fields:
                assert any(
                    keyword.arg == field.name
                    and not (
                        isinstance(keyword.value, ast.Constant)
                        and keyword.value.value is None
                    )
                    for call in calls[item.name]
                    for keyword in call.keywords
                ), field.coordinate
        elif item.kind == "enum":
            assert {(item.name, name) for name, _ in item.members} <= members
        else:
            assert samples.get(item.name), item.coordinate
            assert item.expression is not None
            if item.expression.kind == "array":
                assert any(isinstance(value, ast.Tuple) for value in samples[item.name])
            elif item.expression.kind == "map":
                assert any(isinstance(value, ast.Dict) for value in samples[item.name])
            elif item.expression.kind == "union":
                assert len({ast.dump(value) for value in samples[item.name]}) >= len(
                    item.expression.arguments
                )
    if plan.uses_json_value:
        assert len(samples.get("JSONValue", [])) >= 3


@pytest.mark.parametrize("case", CASES)
def test_phase3_positive_gates(case: str, tmp_path: Path) -> None:
    model = load_model(MODELS / case / "runtimeconditions.binding-model.yaml")
    Draft202012Validator(
        yaml.safe_load(
            (TOOLING / "model/runtimeconditions.binding-model.schema.yaml").read_text()
        )
    ).validate(model)
    plan = _plan(model)
    snapshots: list[dict[str, bytes]] = []
    for index in range(3):
        output = tmp_path / f"run-{index}"
        emit_package(plan, model, output)
        snapshots.append(_files(output))
    assert snapshots[0] == snapshots[1] == snapshots[2]
    assert all(str(tmp_path).encode() not in data for data in snapshots[0].values())
    package = tmp_path / "run-0/src" / plan.target.import_package
    manifest = yaml.safe_load((package / "runtimeconditions.bindings.yaml").read_text())
    Draft202012Validator(
        yaml.safe_load(
            (
                TOOLING / "model/runtimeconditions.binding-manifest.schema.yaml"
            ).read_text()
        )
    ).validate(manifest)
    assert manifest["model"]["semanticSha256"] == model["metadata"]["semanticSha256"]
    metadata = tomllib.loads((tmp_path / "run-0/pyproject.toml").read_text())
    assert metadata["project"]["requires-python"] == ">=3.11"
    assert "3.11" in SpecifierSet(metadata["project"]["requires-python"])
    assert {
        Requirement(value).name for value in metadata["project"]["dependencies"]
    } == {dependency.distribution_name for dependency in plan.target.dependencies}
    assert not (package / "_conformance.py").exists()
    assert verify_package(plan, package, model)["modelMapping"] is True
    _check_public_ast(plan, package)
    _check_conformance_ast(plan, package, model)
    ruff = Path(sys.executable).with_name("ruff")
    mypy = Path(sys.executable).with_name("mypy")
    assert ruff.is_file() and mypy.is_file()
    subprocess.run(
        [str(ruff), "format", "--check", str(package)], check=True, capture_output=True
    )
    subprocess.run(
        [sys.executable, "-m", "compileall", "-q", str(package)],
        check=True,
        capture_output=True,
    )
    source_root = tmp_path / "run-0/src"
    _stage_dependency_sources(model, plan, source_root)
    environment = os.environ.copy()
    environment["MYPYPATH"] = str(source_root)
    subprocess.run(
        [str(mypy), "--strict", str(package)],
        check=True,
        capture_output=True,
        env=environment,
    )


def test_recursive_json_value_conformance(tmp_path: Path) -> None:
    model = deepcopy(
        load_model(
            MODELS / "01-owned-kind-interface" / "runtimeconditions.binding-model.yaml"
        )
    )
    shape = model["schemas"][0]["projection"]["properties"][1]["shape"]
    shape.pop("scalar")
    shape["kind"] = "any"
    plan = _plan(model)
    assert plan.uses_json_value
    emit_package(plan, model, tmp_path / "package")
    package = tmp_path / "package/src" / plan.target.import_package
    assert not (package / "_conformance.py").exists()
    assert verify_package(plan, package, model)["modelMapping"] is True
    _check_public_ast(plan, package)
    _check_conformance_ast(plan, package, model)
    subprocess.run(
        [str(Path(sys.executable).with_name("mypy")), "--strict", str(package)],
        check=True,
        capture_output=True,
    )


def _mutate_negative(case: str, model: dict) -> None:
    if case == "reserved-builtin":
        model["vocabulary"]["ownedDeclarations"][0]["kind"] = "list"
        model["schemas"][0]["kind"] = "list"
    elif case == "fixed-function-collision":
        second = deepcopy(model["vocabulary"]["ownedDeclarations"][0])
        second["coordinate"] = "kind:Service"
        second["kind"] = "Service"
        model["vocabulary"]["ownedDeclarations"].append(second)
    elif case == "enum-member-collision":
        shape = next(
            item["shape"]
            for item in model["schemas"][0]["projection"]["properties"]
            if item["name"] == "mode"
        )
        shape["values"].append({"value": shape["values"][0]["value"].upper()})
    elif case == "unnameable-field":
        model["schemas"][0]["projection"]["properties"][1]["name"] = "💥"
    else:
        raise AssertionError(case)


@pytest.mark.parametrize(
    "fixture",
    yaml.safe_load(
        (TOOLING / "emitters/python/testdata/negative-diagnostics.yaml").read_text()
    ),
)
def test_python_negative_diagnostic(fixture: dict) -> None:
    model = deepcopy(
        load_model(
            MODELS / fixture["positiveCase"] / "runtimeconditions.binding-model.yaml"
        )
    )
    _mutate_negative(fixture["case"], model)
    Draft202012Validator(
        yaml.safe_load(
            (TOOLING / "model/runtimeconditions.binding-model.schema.yaml").read_text()
        )
    ).validate(model)
    with pytest.raises(DiagnosticError) as error:
        _plan(model)
    diagnostic = error.value.diagnostic
    assert {
        "category": diagnostic.category,
        "code": diagnostic.code,
        "coordinate": diagnostic.coordinate,
        "jsonPointer": diagnostic.json_pointer,
        "message": diagnostic.message,
    } == fixture["expected"]


def test_manifest_mapping_tampering_is_rejected(tmp_path: Path) -> None:
    model = load_model(
        MODELS / "01-owned-kind-interface/runtimeconditions.binding-model.yaml"
    )
    plan = _plan(model)
    emit_package(plan, model, tmp_path / "package")
    package = tmp_path / "package/src" / plan.target.import_package
    path = package / "runtimeconditions.bindings.yaml"
    text = path.read_text(encoding="utf-8")
    changed = text.replace("#", "#wrong", 1)
    assert changed != text
    path.write_text(changed, encoding="utf-8")
    with pytest.raises(ValueError, match="manifest mappings"):
        verify_package(plan, package, model)
