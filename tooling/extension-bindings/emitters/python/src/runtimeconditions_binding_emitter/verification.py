"""Native Python AST checks; generated modules are parsed, never imported."""

from __future__ import annotations

import ast
from pathlib import Path
from typing import Any, cast

from .emitter import DeclarationPlan, EmissionPlan, ImportedDeclarationPlan


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
    if not len(assignments) == 1:
        raise ValueError("native API or conformance mismatch: len(assignments) == 1")
    names = ast.literal_eval(assignments[0].value)
    if not names == sorted(set(names)):
        raise ValueError(
            "native API or conformance mismatch: names == sorted(set(names))"
        )
    return cast(list[str], names)


def _check_public_ast(plan: EmissionPlan, package: Path) -> None:
    """Compare declarations, signatures, fields and domains to the model plan."""
    init = ast.parse((package / "__init__.py").read_text(encoding="utf-8"))
    bindings = ast.parse((package / "bindings.py").read_text(encoding="utf-8"))
    expected = {item.name for item in plan.types}
    declarations: list[DeclarationPlan | ImportedDeclarationPlan] = [
        *plan.declarations,
        *plan.imported_declarations,
    ]
    for declaration in declarations:
        expected.update((declaration.function, declaration.protocol))
    if plan.declarations:
        expected.add("Declaration")
    if plan.uses_json_value:
        expected.add("JSONValue")
    if not set(_exports(init)) == expected:
        raise ValueError(
            "native API or conformance mismatch: set(_exports(init)) == expected"
        )
    if not _exports(bindings) == _exports(init):
        raise ValueError(
            "native API or conformance mismatch: _exports(bindings) == _exports(init)"
        )
    imported = next(
        node
        for node in init.body
        if isinstance(node, ast.ImportFrom) and node.module == "bindings"
    )
    if not {alias.name for alias in imported.names} == expected:
        raise ValueError(
            "native API or conformance mismatch: {alias.name for alias in imported.names} == expected"
        )
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
    if not set(classes) == {
        item.name for item in plan.types if item.kind != "alias"
    } | {declaration.protocol for declaration in plan.declarations} | (
        {"Declaration"} if plan.declarations else set()
    ):
        raise ValueError(
            "native API or conformance mismatch: set(classes) == {item.name for item in plan.types if item.kind != 'alias'} | {declaration.protocol for declaration in plan.declarations} | ({'Declaration'} if plan.declarations else set())"
        )
    if not set(functions) == {
        declaration.function for declaration in plan.declarations
    }:
        raise ValueError(
            "native API or conformance mismatch: set(functions) == {declaration.function for declaration in plan.declarations}"
        )
    if not set(aliases) == {
        item.name for item in plan.types if item.kind == "alias"
    } | ({"JSONValue"} if plan.uses_json_value else set()):
        raise ValueError(
            "native API or conformance mismatch: set(aliases) == {item.name for item in plan.types if item.kind == 'alias'} | ({'JSONValue'} if plan.uses_json_value else set())"
        )
    for declaration in plan.declarations:
        protocol = classes[declaration.protocol]
        if not [ast.unparse(base) for base in protocol.bases] == ["Protocol"]:
            raise ValueError(
                "native API or conformance mismatch: [ast.unparse(base) for base in protocol.bases] == ['Protocol']"
            )
        methods = [node for node in protocol.body if isinstance(node, ast.FunctionDef)]
        if not [method.name for method in methods] == [declaration.method]:
            raise ValueError(
                "native API or conformance mismatch: [method.name for method in methods] == [declaration.method]"
            )
        function = functions[declaration.function]
        if not function.args.vararg is not None:
            raise ValueError(
                "native API or conformance mismatch: function.args.vararg is not None"
            )
        if not function.args.vararg.arg == "fields":
            raise ValueError(
                "native API or conformance mismatch: function.args.vararg.arg == 'fields'"
            )
        if function.args.vararg.annotation is None or function.returns is None:
            raise ValueError("declaration annotations are missing")
        if not ast.unparse(function.args.vararg.annotation) == declaration.protocol:
            raise ValueError(
                "native API or conformance mismatch: ast.unparse(function.args.vararg.annotation) == declaration.protocol"
            )
        if not ast.unparse(function.returns) == "Declaration":
            raise ValueError(
                "native API or conformance mismatch: ast.unparse(function.returns) == 'Declaration'"
            )
    for declaration in plan.imported_declarations:
        if not declaration.function not in functions:
            raise ValueError(
                "native API or conformance mismatch: declaration.function not in functions"
            )
        if not declaration.protocol not in classes:
            raise ValueError(
                "native API or conformance mismatch: declaration.protocol not in classes"
            )
    for item in plan.types:
        if item.kind == "alias":
            if not ast.unparse(aliases[item.name].annotation) == "TypeAlias":
                raise ValueError(
                    "native API or conformance mismatch: ast.unparse(aliases[item.name].annotation) == 'TypeAlias'"
                )
            alias_value = aliases[item.name].value
            if alias_value is None:
                raise ValueError("alias value is missing")
            if not ast.literal_eval(alias_value) == item.annotation:
                raise ValueError(
                    "native API or conformance mismatch: ast.literal_eval(aliases[item.name].value) == item.annotation"
                )
            continue
        body = classes[item.name].body
        if item.kind == "enum":
            if not [ast.unparse(base) for base in classes[item.name].bases] == [
                "StrEnum"
            ]:
                raise ValueError(
                    "native API or conformance mismatch: [ast.unparse(base) for base in classes[item.name].bases] == ['StrEnum']"
                )
            if not [
                (node.targets[0].id, ast.literal_eval(node.value))
                for node in body
                if isinstance(node, ast.Assign)
                and isinstance(node.targets[0], ast.Name)
            ] == list(item.members):
                raise ValueError(
                    "native API or conformance mismatch: [(node.targets[0].id, ast.literal_eval(node.value)) for node in body if isinstance(node, ast.Assign) and isinstance(node.targets[0], ast.Name)] == list(item.members)"
                )
            continue
        if not [
            ast.unparse(decorator) for decorator in classes[item.name].decorator_list
        ] == ["dataclass(frozen=True, kw_only=True)"]:
            raise ValueError(
                "native API or conformance mismatch: [ast.unparse(decorator) for decorator in classes[item.name].decorator_list] == ['dataclass(frozen=True, kw_only=True)']"
            )
        fields = [node for node in body if isinstance(node, ast.AnnAssign)]
        if not [
            (
                ast.unparse(node.target),
                ast.unparse(node.annotation),
                node.value is not None and ast.unparse(node.value) == "None",
            )
            for node in fields
        ] == [
            (field.name, field.annotation, field.default_none) for field in item.fields
        ]:
            raise ValueError(
                "native API or conformance mismatch: [(node.target.id, ast.unparse(node.annotation), node.value is not None and ast.unparse(node.value) == 'None') for node in fields] == [(field.name, field.annotation, field.default_none) for field in item.fields]"
            )
        markers = [node.name for node in body if isinstance(node, ast.FunctionDef)]
        if not markers == ([item.marker.method] if item.marker else []):
            raise ValueError(
                "native API or conformance mismatch: markers == ([item.marker.method] if item.marker else [])"
            )


def api_surface(package: Path) -> list[str]:
    """Return structural native signatures, excluding executable bodies."""
    tree = ast.parse((package / "bindings.py").read_text(encoding="utf-8"))
    exports = set(_exports(tree))
    result: list[str] = []
    for node in tree.body:
        if isinstance(node, ast.ClassDef) and node.name in exports:
            result.append(
                "class|"
                + node.name
                + "|"
                + ",".join(ast.unparse(base) for base in node.bases)
            )
            for child in node.body:
                if isinstance(child, ast.AnnAssign):
                    result.append(
                        "field|"
                        + node.name
                        + "|"
                        + ast.unparse(child.target)
                        + "|"
                        + ast.unparse(child.annotation)
                        + "|"
                        + (ast.unparse(child.value) if child.value else "required")
                    )
                elif isinstance(child, ast.Assign):
                    result.append("member|" + node.name + "|" + ast.unparse(child))
                elif isinstance(child, ast.FunctionDef):
                    result.append(
                        "method|"
                        + node.name
                        + "|"
                        + child.name
                        + "|"
                        + ast.unparse(child.args)
                        + "|"
                        + (ast.unparse(child.returns) if child.returns else "")
                    )
        elif isinstance(node, ast.FunctionDef) and node.name in exports:
            result.append(
                "function|"
                + node.name
                + "|"
                + ast.unparse(node.args)
                + "|"
                + (ast.unparse(node.returns) if node.returns else "")
            )
        elif (
            isinstance(node, ast.AnnAssign)
            and isinstance(node.target, ast.Name)
            and (node.target.id in exports)
        ):
            if node.value is None:
                raise ValueError("alias value is missing")
            result.append("alias|" + node.target.id + "|" + ast.unparse(node.value))
        elif isinstance(node, ast.ImportFrom):
            for name in node.names:
                if name.name in exports:
                    result.append("import|" + str(node.module) + "|" + name.name)
    return sorted(result)


def verify_package(
    plan: EmissionPlan, package: Path, model: dict[str, Any]
) -> dict[str, Any]:
    _check_public_ast(plan, package)
    from .metadata import render_resources

    key = f"src/{plan.target.import_package}/runtimeconditions.bindings.yaml"
    expected = render_resources(plan, model)[key]
    if (package / "runtimeconditions.bindings.yaml").read_text(
        encoding="utf-8"
    ) != expected:
        raise ValueError("binding manifest mappings differ from normalized model")
    return {"api": api_surface(package), "modelMapping": True}
