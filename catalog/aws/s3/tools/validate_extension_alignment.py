#!/usr/bin/env python3
"""Validate that a botocore S3 model can be projected through an exact AWS S3 extension release."""

from __future__ import annotations

import argparse
from pathlib import Path
from typing import Any

from serialization import read_document


def validate_input_path(model: dict[str, Any], operation_name: str, path: str) -> None:
    operation = model.get("operations", {}).get(operation_name)
    if not operation:
        raise ValueError(f"unknown botocore operation {operation_name}")
    shape_name = operation.get("input", {}).get("shape")
    if not shape_name:
        raise ValueError(f"{operation_name}: Condition identity path {path!r} requires an input")
    for segment in path.split("."):
        is_list = segment.endswith("[]")
        member_name = segment[:-2] if is_list else segment
        shape = model.get("shapes", {}).get(shape_name, {})
        member = shape.get("members", {}).get(member_name)
        if not member:
            raise ValueError(f"{operation_name}: Condition identity path {path!r} does not resolve at {member_name!r}")
        shape_name = member.get("shape")
        if is_list:
            list_shape = model.get("shapes", {}).get(shape_name, {})
            if list_shape.get("type") != "list":
                raise ValueError(f"{operation_name}: Condition identity path {path!r} marks non-list {member_name!r} as a list")
            shape_name = list_shape.get("member", {}).get("shape")


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--service-mapping", type=Path, required=True)
    parser.add_argument("--botocore-service-model", type=Path, required=True)
    parser.add_argument("--botocore-annotations", type=Path, required=True)
    args = parser.parse_args()

    mapping = read_document(args.service_mapping)
    model = read_document(args.botocore_service_model)
    annotations = read_document(args.botocore_annotations)
    aliases = annotations.get("canonicalOperationAliases", {})
    sdk_operations = set(model.get("operations", {}))
    mapping_operations = {item["name"]: item for item in mapping.get("operations", [])}
    applicable_aliases = {alias: target for alias, target in aliases.items() if alias in sdk_operations}
    canonical_sdk_operations = {aliases.get(name, name) for name in sdk_operations}
    unsupported = sorted(canonical_sdk_operations - set(mapping_operations))
    if unsupported:
        raise ValueError("SDK operations are absent from the version-aligned extension: " + ", ".join(unsupported))
    invalid_alias_targets = sorted(set(applicable_aliases.values()) - set(mapping_operations))
    if invalid_alias_targets:
        raise ValueError("SDK compatibility aliases target operations absent from the extension: " + ", ".join(invalid_alias_targets))

    sdk_names_by_canonical: dict[str, list[str]] = {}
    for sdk_operation in sdk_operations:
        sdk_names_by_canonical.setdefault(aliases.get(sdk_operation, sdk_operation), []).append(sdk_operation)
    for canonical in sorted(canonical_sdk_operations):
        for condition in mapping_operations[canonical].get("conditions", []):
            identity_path = condition.get("identity", {}).get("path")
            if not identity_path:
                continue
            for sdk_operation in sdk_names_by_canonical[canonical]:
                validate_input_path(model, sdk_operation, identity_path)

    extension = mapping.get("extension", {})
    if not extension.get("id") or not extension.get("version") or not extension.get("semanticSha256"):
        raise ValueError("service mapping does not identify an exact extension semantic release")
    print("SDK-to-extension alignment passed")
    print(f"  extension: {extension['id']}")
    print(f"  SDK operations: {len(sdk_operations)}")
    print(f"  canonical extension operations used: {len(canonical_sdk_operations)}")
    print(f"  SDK compatibility aliases: {len(applicable_aliases)}")


if __name__ == "__main__":
    main()
