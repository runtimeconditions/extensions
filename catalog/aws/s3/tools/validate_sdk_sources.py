#!/usr/bin/env python3
"""Check generated mappings and reviewed annotations against pinned SDK source."""

from __future__ import annotations

import argparse
import ast
import re
from pathlib import Path
from typing import Any, Iterable

from serialization import read_document


VERSION_PATTERN = re.compile(r'''__version__\s*=\s*['"]([^'"]+)['"]''')


def version(root: Path, package: str) -> str:
    path = root / package / "__init__.py"
    match = VERSION_PATTERN.search(path.read_text(encoding="utf-8"))
    if not match:
        raise ValueError(f"{path}: could not find __version__")
    return match.group(1)


def python_name(name: str) -> str:
    first = re.sub(r"(.)([A-Z][a-z]+)", r"\1_\2", name)
    return re.sub(r"([a-z0-9])([A-Z])", r"\1_\2", first).lower()


def function_node(path: Path, name: str, class_name: str | None = None) -> ast.FunctionDef | ast.AsyncFunctionDef:
    tree = ast.parse(path.read_text(encoding="utf-8"), filename=str(path))
    scope: Iterable[ast.AST] = tree.body
    if class_name:
        classes = [item for item in scope if isinstance(item, ast.ClassDef) and item.name == class_name]
        if len(classes) != 1:
            raise ValueError(f"{path}: expected class {class_name}")
        scope = classes[0].body
    functions = [
        item
        for item in scope
        if isinstance(item, (ast.FunctionDef, ast.AsyncFunctionDef)) and item.name == name
    ]
    if len(functions) != 1:
        owner = f"{class_name}." if class_name else ""
        raise ValueError(f"{path}: expected function {owner}{name}")
    return functions[0]


def parameters(node: ast.FunctionDef | ast.AsyncFunctionDef) -> list[str]:
    names = [item.arg for item in (*node.args.posonlyargs, *node.args.args)]
    if names and names[0] in {"self", "cls"}:
        names = names[1:]
    return names


def validate_arguments(description: str, params: list[str], bindings: dict[str, Any]) -> None:
    for logical_name, binding in bindings.items():
        position = binding.get("position")
        keyword = binding.get("keyword")
        if not isinstance(position, int) or position < 0 or position >= len(params):
            raise ValueError(f"{description}: {logical_name} has invalid position {position!r}")
        if params[position] != keyword:
            raise ValueError(
                f"{description}: {logical_name} binding says {keyword!r} at {position}, "
                f"source has {params[position]!r}"
            )


def validate_boto3_wrappers(source: Path, annotations: dict[str, Any]) -> int:
    inject = source / "boto3/s3/inject.py"
    count = 0
    for wrapper in annotations.get("clientWrappers", []):
        node = function_node(inject, wrapper["method"])
        validate_arguments(f"boto3 client {wrapper['method']}", parameters(node), wrapper["arguments"])
        count += 1

    resource_prefix = {"Bucket": "bucket", "Object": "object"}
    for resource, wrappers in annotations.get("resourceWrappers", {}).items():
        if resource not in resource_prefix:
            raise ValueError(f"no source validation rule for boto3 resource {resource}")
        for wrapper in wrappers:
            function = f"{resource_prefix[resource]}_{wrapper['method']}"
            node = function_node(inject, function)
            validate_arguments(
                f"boto3 resource {resource}.{wrapper['method']}",
                parameters(node),
                wrapper["arguments"],
            )
            count += 1

    transfer = source / "boto3/s3/transfer.py"
    for wrapper in annotations.get("transferClassWrappers", []):
        method = wrapper["symbol"].rsplit(".", 1)[1]
        node = function_node(transfer, method, "S3Transfer")
        validate_arguments(wrapper["symbol"], parameters(node), wrapper["arguments"])
        context = wrapper.get("receiverContext")
        if context:
            if context.get("constructor") != "boto3.s3.transfer.S3Transfer":
                raise ValueError(f"unsupported boto3 receiver constructor: {context}")
            constructor = function_node(transfer, "__init__", "S3Transfer")
            validate_arguments(
                context["constructor"], parameters(constructor), context.get("arguments", {})
            )
        count += 1

    session = source / "boto3/session.py"
    for factories_key in ("clientFactories", "resourceFactories"):
        for factory in annotations.get(factories_key, []):
            for symbol in factory["symbols"]:
                if ".Session." not in symbol:
                    continue
                method = symbol.rsplit(".", 1)[1]
                node = function_node(session, method, "Session")
                selector = factory["serviceSelector"]
                validate_arguments(symbol, parameters(node), {"service": selector})

    handwritten_sources = {
        "Bucket": ("bucket_load", "list_buckets"),
        "ObjectSummary": ("object_summary_load", "head_object"),
    }
    for resource, actions in annotations.get("handwrittenResourceActions", {}).items():
        function, expected_call = handwritten_sources[resource]
        node = function_node(inject, function)
        calls = {
            item.func.attr
            for item in ast.walk(node)
            if isinstance(item, ast.Call) and isinstance(item.func, ast.Attribute)
        }
        for action in actions:
            if python_name(action["operation"]) != expected_call or expected_call not in calls:
                raise ValueError(f"{resource} handwritten action does not match {function}")
    return count


def s3transfer_symbol(source: Path, symbol: str) -> tuple[Path, str, str]:
    parts = symbol.split(".")
    if parts[:2] == ["s3transfer", "manager"]:
        return source / "s3transfer/manager.py", parts[2], parts[3]
    if parts[:2] == ["s3transfer", "crt"]:
        return source / "s3transfer/crt.py", parts[2], parts[3]
    if len(parts) == 3 and parts[0] == "s3transfer":
        return source / "s3transfer/__init__.py", parts[1], parts[2]
    raise ValueError(f"unsupported s3transfer entrypoint: {symbol}")


def operation_references(value: Any) -> set[str]:
    if isinstance(value, list):
        result: set[str] = set()
        for item in value:
            result.update(operation_references(item))
        return result
    if not isinstance(value, dict):
        return set()
    result = {value["operation"]} if isinstance(value.get("operation"), str) else set()
    for item in value.values():
        result.update(operation_references(item))
    return result


def source_operation_names(paths: Iterable[Path], known_methods: set[str]) -> set[str]:
    found: set[str] = set()
    for path in paths:
        tree = ast.parse(path.read_text(encoding="utf-8"), filename=str(path))
        for node in ast.walk(tree):
            if isinstance(node, ast.Attribute) and node.attr in known_methods:
                found.add(node.attr)
            elif isinstance(node, ast.Constant) and node.value in known_methods:
                found.add(node.value)
    return {next(name for name in OPERATION_NAMES if python_name(name) == method) for method in found}


OPERATION_NAMES: set[str] = set()


def validate_s3transfer(
    source: Path, annotations: dict[str, Any], known_operations: set[str]
) -> tuple[int, int]:
    global OPERATION_NAMES
    OPERATION_NAMES = known_operations
    entrypoint_count = 0
    for call in annotations.get("calls", []):
        for entrypoint in call.get("entrypoints", []):
            path, class_name, method = s3transfer_symbol(source, entrypoint["symbol"])
            node = function_node(path, method, class_name)
            validate_arguments(entrypoint["symbol"], parameters(node), entrypoint["arguments"])
            context = entrypoint.get("receiverContext")
            if context:
                constructor_path, constructor_class, constructor_method = s3transfer_symbol(
                    source, f"{context['constructor']}.__init__"
                )
                constructor = function_node(
                    constructor_path, constructor_method, constructor_class
                )
                validate_arguments(
                    context["constructor"],
                    parameters(constructor),
                    context.get("arguments", {}),
                )
            entrypoint_count += 1

    expected_by_call = {
        call["name"]: operation_references(call.get("implementations", []))
        for call in annotations.get("calls", [])
    }
    known_methods = {python_name(name) for name in known_operations}
    modules_by_call = {
        "managed-upload": ["tasks.py", "upload.py"],
        "managed-download": ["download.py"],
        "managed-copy": ["tasks.py", "copies.py"],
        "managed-delete": ["delete.py"],
    }
    for call_name, modules in modules_by_call.items():
        found = source_operation_names(
            (source / "s3transfer" / module for module in modules), known_methods
        )
        if found != expected_by_call[call_name]:
            raise ValueError(
                f"s3transfer {call_name}: annotations {sorted(expected_by_call[call_name])}, "
                f"source modules {sorted(found)}"
            )
    crt_found = source_operation_names([source / "s3transfer/crt.py"], known_methods)
    if crt_found != {"PutObject", "GetObject", "DeleteObject"}:
        raise ValueError(f"unexpected CRT operation surface: {sorted(crt_found)}")
    return entrypoint_count, sum(len(value) for value in expected_by_call.values())


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--boto3-source", type=Path, required=True)
    parser.add_argument("--botocore-source", type=Path, required=True)
    parser.add_argument("--s3transfer-source", type=Path, required=True)
    parser.add_argument("--boto3-annotations", type=Path, required=True)
    parser.add_argument("--botocore-annotations", type=Path, required=True)
    parser.add_argument("--s3transfer-annotations", type=Path, required=True)
    parser.add_argument("--botocore-mapping", type=Path, required=True)
    parser.add_argument("--boto3-mapping", type=Path, required=True)
    parser.add_argument("--s3transfer-mapping", type=Path, required=True)
    args = parser.parse_args()

    botocore_mapping = read_document(args.botocore_mapping)
    boto3_mapping = read_document(args.boto3_mapping)
    transfer_mapping = read_document(args.s3transfer_mapping)
    boto3_annotations = read_document(args.boto3_annotations)
    botocore_annotations = read_document(args.botocore_annotations)
    transfer_annotations = read_document(args.s3transfer_annotations)

    versions = {
        "botocore": version(args.botocore_source, "botocore"),
        "boto3": version(args.boto3_source, "boto3"),
        "s3transfer": version(args.s3transfer_source, "s3transfer"),
    }
    mappings = {
        "botocore": botocore_mapping,
        "boto3": boto3_mapping,
        "s3transfer": transfer_mapping,
    }
    for distribution, installed_version in versions.items():
        if mappings[distribution].get("metadata", {}).get("distributionVersion") != installed_version:
            raise ValueError(f"{distribution}: mapping version does not match source")

    service_model = read_document(
        args.botocore_source / "botocore/data/s3/2006-03-01/service-2.json"
    )
    service_operations = set(service_model.get("operations", {}))
    aliases = botocore_annotations.get("canonicalOperationAliases", {})
    unknown_aliases = sorted(set(aliases) - service_operations)
    if unknown_aliases:
        raise ValueError(
            "botocore alias annotations reference absent SDK operations: "
            + ", ".join(unknown_aliases)
        )
    canonical_service_operations = {aliases.get(name, name) for name in service_operations}
    mapped_operations = {item["name"] for item in botocore_mapping.get("operations", [])}
    if mapped_operations != canonical_service_operations:
        raise ValueError("botocore mapping canonical operation set does not match the SDK service model and reviewed aliases")
    methods = botocore_mapping.get("python", {}).get("client", {}).get("methods", [])
    expected_methods = {(python_name(name), aliases.get(name, name)) for name in service_operations}
    if {(item["method"], item["operation"]) for item in methods} != expected_methods:
        raise ValueError("botocore Python operation names do not match the SDK service model and canonical aliases")

    wrapper_count = validate_boto3_wrappers(args.boto3_source, boto3_annotations)
    entrypoint_count, transfer_operation_count = validate_s3transfer(
        args.s3transfer_source, transfer_annotations, canonical_service_operations
    )

    resource_model = read_document(
        args.boto3_source / "boto3/data/s3/2006-03-01/resources-1.json"
    )
    mapped_resources = {item["name"] for item in boto3_mapping["python"]["resources"]}
    if mapped_resources != set(resource_model.get("resources", {})):
        raise ValueError("boto3 mapping resource set does not match resource model")

    print("pinned SDK source validation passed")
    print(f"  botocore SDK methods: {len(service_operations)}")
    print(f"  canonical Smithy operations: {len(canonical_service_operations)}")
    print(f"  boto3 handwritten wrapper surfaces: {wrapper_count}")
    print(f"  boto3 modeled resources: {len(mapped_resources)}")
    print(f"  s3transfer public entrypoints: {entrypoint_count}")
    print(f"  s3transfer distinct canonical operations: {transfer_operation_count}")


if __name__ == "__main__":
    main()
