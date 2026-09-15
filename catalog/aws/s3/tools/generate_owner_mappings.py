#!/usr/bin/env python3
"""Generate owner-aligned botocore, boto3, and s3transfer S3 mappings.

The script reads SDK models and reviewed wrapper annotations as data. It does
not import or execute any of the SDK packages whose metadata it generates.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import re
from pathlib import Path
from typing import Any

from serialization import read_document, write_yaml


API_VERSION = "runtimeconditions.io/sdk-mapping/v1alpha1"
MAPPING_KIND = "RuntimeConditionsSDKMapping"


def semantic_sha256(value: Any) -> str:
    encoded = json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=False).encode("utf-8")
    return hashlib.sha256(encoded).hexdigest()


def add_mapping_semantic_digest(mapping: dict[str, Any]) -> None:
    mapping["metadata"]["semanticSha256"] = semantic_sha256({"operations": mapping.get("operations", []), "python": mapping.get("python", {})})


def python_name(name: str) -> str:
    first = re.sub(r"(.)([A-Z][a-z]+)", r"\1_\2", name)
    return re.sub(r"([a-z0-9])([A-Z])", r"\1_\2", first).lower()


def operation_fingerprint(names: list[str]) -> str:
    return hashlib.sha256(("\n".join(names) + "\n").encode()).hexdigest()


def sdk_dependency(distribution: str, mapping: str) -> dict[str, str]:
    return {
        "kind": "sdkMapping",
        "distribution": distribution,
        "mapping": mapping,
    }


def operation_ref(operation: str) -> dict[str, str]:
    return {
        "distribution": "botocore",
        "mapping": "botocore.aws.s3",
        "operation": operation,
    }


def wrapper_binding(value: dict[str, Any]) -> dict[str, Any]:
    result = dict(value)
    call = result.pop("call")
    result["callRef"] = {
        "distribution": "s3transfer",
        "mapping": "s3transfer.aws.s3",
        "call": call,
    }
    return result


def referenced_operation(value: Any, operations: set[str], context: str) -> Any:
    """Replace every semantic operation leaf with an owner-qualified ref."""
    if isinstance(value, list):
        return [referenced_operation(item, operations, context) for item in value]
    if not isinstance(value, dict):
        return value
    generated = {
        key: referenced_operation(item, operations, context)
        for key, item in value.items()
        if key != "operation"
    }
    if "operation" in value:
        operation = value["operation"]
        if operation not in operations:
            raise ValueError(f"{context} references unknown operation: {operation}")
        generated["operationRef"] = operation_ref(operation)
    return generated


def canonicalize_operation_refs(value: Any, aliases: dict[str, str]) -> Any:
    if isinstance(value, list):
        return [canonicalize_operation_refs(item, aliases) for item in value]
    if not isinstance(value, dict):
        return value
    result = {
        key: canonicalize_operation_refs(item, aliases)
        for key, item in value.items()
    }
    operation_ref_value = result.get("operationRef")
    if isinstance(operation_ref_value, dict):
        operation = operation_ref_value.get("operation")
        if operation in aliases:
            operation_ref_value["sdkOperation"] = operation
            operation_ref_value["operation"] = aliases[operation]
    return result


def request_binding(request: dict[str, Any] | None) -> dict[str, Any] | None:
    if not request or not request.get("operation"):
        return None
    return {
        "operationRef": operation_ref(request["operation"]),
        "parameterBindings": request.get("params", []),
    }


def response_resource(value: dict[str, Any] | None) -> dict[str, Any] | None:
    if not value:
        return None
    return {
        "resource": value.get("type"),
        "path": value.get("path"),
        "identifierBindings": value.get("identifiers", []),
    }


def action_binding(name: str, value: dict[str, Any], batch: bool = False) -> dict[str, Any]:
    result: dict[str, Any] = {
        "method": python_name(name),
        "modelName": name,
        "batch": batch,
    }
    request = request_binding(value.get("request"))
    if request:
        result.update(request)
    returned = response_resource(value.get("resource"))
    if returned:
        result["returns"] = returned
    if value.get("path"):
        result["responsePath"] = value["path"]
    return result


def relation_binding(name: str, value: dict[str, Any]) -> dict[str, Any]:
    resource = value.get("resource", {})
    identifiers = resource.get("identifiers", [])
    needs_data = any(item.get("source") == "data" for item in identifiers)
    relation_kind = "reference" if needs_data else "subresource"
    member = python_name(name) if needs_data else name
    result: dict[str, Any] = {
        "member": member,
        "modelName": name,
        "kind": relation_kind,
        "resource": resource.get("type"),
        "identifierBindings": identifiers,
    }
    if resource.get("path"):
        result["responsePath"] = resource["path"]
    return result


def collection_binding(name: str, value: dict[str, Any], resources: dict[str, Any]) -> dict[str, Any]:
    resource = value.get("resource", {})
    target = resource.get("type")
    result: dict[str, Any] = {
        "member": python_name(name),
        "modelName": name,
        "resource": target,
        "identifierBindings": resource.get("identifiers", []),
        "batchActions": [],
    }
    request = request_binding(value.get("request"))
    if request:
        result.update(request)
    if resource.get("path"):
        result["responsePath"] = resource["path"]
    for action_name, action in sorted(resources.get(target, {}).get("batchActions", {}).items()):
        result["batchActions"].append(action_binding(action_name, action, batch=True))
    return result


def service_relations(service: dict[str, Any], resources: dict[str, Any]) -> dict[str, Any]:
    relations = dict(service.get("has", {}))
    represented = {
        value.get("resource", {}).get("type")
        for value in relations.values()
    }
    for name, definition in resources.items():
        if name in represented:
            continue
        relations[name] = {
            "resource": {
                "type": name,
                "identifiers": [
                    {"target": item["name"], "source": "input"}
                    for item in definition.get("identifiers", [])
                ],
            }
        }
    return relations


def resource_binding(
    name: str,
    definition: dict[str, Any],
    resources: dict[str, Any],
    wrapper_annotations: dict[str, Any],
) -> dict[str, Any]:
    identifiers = [
        {
            "name": item["name"],
            "pythonName": python_name(item["name"]),
            **({"type": item["type"]} if item.get("type") else {}),
            **({"memberName": item["memberName"]} if item.get("memberName") else {}),
        }
        for item in definition.get("identifiers", [])
    ]
    actions = [
        action_binding(action_name, action)
        for action_name, action in sorted(definition.get("actions", {}).items())
    ]
    load = definition.get("load")
    if load and load.get("request", {}).get("operation"):
        binding = request_binding(load["request"])
        actions.extend(
            {
                "method": method,
                "modelName": "load",
                "batch": False,
                **binding,
            }
            for method in ("load", "reload")
        )
    for extra in wrapper_annotations.get("handwrittenResourceActions", {}).get(name, []):
        for method in extra["methods"]:
            actions.append(
                {
                    "method": method,
                    "modelName": "handwritten",
                    "batch": False,
                    "operationRef": operation_ref(extra["operation"]),
                    "parameterBindings": extra.get("parameterBindings", []),
                }
            )

    waiters = [
        {
            "method": f"wait_until_{python_name(waiter_name)}",
            "waiterRef": {
                "distribution": "botocore",
                "mapping": "botocore.aws.s3",
                "waiter": python_name(waiter.get("waiterName", waiter_name)),
            },
            "parameterBindings": waiter.get("params", []),
        }
        for waiter_name, waiter in sorted(definition.get("waiters", {}).items())
    ]
    relations = [
        relation_binding(relation_name, relation)
        for relation_name, relation in sorted(definition.get("has", {}).items())
    ]
    collections = [
        collection_binding(collection_name, collection, resources)
        for collection_name, collection in sorted(definition.get("hasMany", {}).items())
    ]
    return {
        "name": name,
        "identifiers": identifiers,
        "actions": sorted(actions, key=lambda item: (item["method"], item["modelName"])),
        "waiters": waiters,
        "relations": relations,
        "collections": collections,
        "wrappers": [
            wrapper_binding(item)
            for item in wrapper_annotations.get("resourceWrappers", {}).get(name, [])
        ],
    }


def botocore_mapping(
    service_mapping: dict[str, Any],
    sdk_service_model: dict[str, Any],
    paginators: dict[str, Any],
    waiters: dict[str, Any],
    annotations: dict[str, Any],
    version: str,
) -> dict[str, Any]:
    service_metadata = service_mapping["metadata"]
    authoritative_operations = {
        item["name"]: item for item in service_mapping["operations"]
    }
    authoritative_operation_names = set(authoritative_operations)
    aliases = annotations.get("canonicalOperationAliases", {})
    sdk_operation_names = set(sdk_service_model.get("operations", {}))
    applicable_aliases = {
        alias: canonical
        for alias, canonical in aliases.items()
        if alias in sdk_operation_names
    }
    canonical_sdk_operations = {
        aliases.get(operation, operation) for operation in sdk_operation_names
    }
    unknown_alias_targets = sorted(set(applicable_aliases.values()) - authoritative_operation_names)
    if unknown_alias_targets:
        raise ValueError(
            "botocore aliases target unknown canonical operations: "
            + ", ".join(unknown_alias_targets)
        )
    conflicting_aliases = sorted(set(applicable_aliases) & authoritative_operation_names)
    if conflicting_aliases:
        raise ValueError(
            "botocore aliases conflict with canonical operations: "
            + ", ".join(conflicting_aliases)
        )
    unsupported_operations = sorted(canonical_sdk_operations - authoritative_operation_names)
    if unsupported_operations:
        raise ValueError(
            "SDK operations are absent from the version-aligned extension: "
            + ", ".join(unsupported_operations)
        )
    operations = [
        authoritative_operations[name] for name in sorted(canonical_sdk_operations)
    ]
    operation_names = set(canonical_sdk_operations)

    paginator_items = []
    for name in sorted(paginators.get("pagination", {})):
        if name not in operation_names:
            raise ValueError(f"paginator references unknown operation: {name}")
        paginator_items.append(
            {
                "name": python_name(name),
                "terminalMethod": "paginate",
                "operation": name,
            }
        )

    waiter_items = []
    for name, value in sorted(waiters.get("waiters", {}).items()):
        operation = value.get("operation")
        if operation not in operation_names:
            raise ValueError(f"waiter {name} references unknown operation: {operation}")
        waiter_items.append(
            {
                "name": python_name(name),
                "terminalMethod": "wait",
                "operation": operation,
            }
        )

    return {
        "apiVersion": API_VERSION,
        "kind": MAPPING_KIND,
        "metadata": {
            "name": "botocore.aws.s3",
            "distribution": "botocore",
            "distributionVersion": version,
            "language": "python",
            "service": "s3",
            "serviceId": service_metadata["serviceId"],
            "serviceShape": service_metadata["serviceShape"],
            "serviceVersion": service_metadata["serviceVersion"],
            "operationNamesSha256": service_metadata["operationNamesSha256"],
            "serviceMappingSemanticSha256": service_metadata["semanticSha256"],
            "sdkOperationCount": len(sdk_operation_names),
            "sdkCanonicalOperationNamesSha256": operation_fingerprint(
                sorted(canonical_sdk_operations)
            ),
        },
        "dependencies": [
            {"kind": "extension", **service_mapping["extension"]}
        ],
        "extension": service_mapping["extension"],
        "operations": operations,
        "python": {
            "client": {
                "surface": "botocore.client.s3",
                "methods": sorted([
                    {"method": python_name(item["name"]), "operation": item["name"]}
                    for item in operations
                ] + [
                    {
                        "method": python_name(alias),
                        "sdkOperation": alias,
                        "operation": canonical,
                    }
                    for alias, canonical in applicable_aliases.items()
                ], key=lambda item: item["method"]),
                "paginatorFactory": {
                    "method": "get_paginator",
                    "selector": {"position": 0, "keyword": "operation_name"},
                    "items": paginator_items,
                },
                "waiterFactory": {
                    "method": "get_waiter",
                    "selector": {"position": 0, "keyword": "waiter_name"},
                    "items": waiter_items,
                },
            }
        },
    }


def s3transfer_mapping(annotations: dict[str, Any], version: str, operations: set[str]) -> dict[str, Any]:
    calls = []
    for call in annotations.get("calls", []):
        declared_arguments = set(call.get("arguments", []))
        for entrypoint in call.get("entrypoints", []):
            supplied_arguments = set(entrypoint.get("arguments", {}))
            supplied_arguments.update(
                entrypoint.get("receiverContext", {}).get("arguments", {})
            )
            unknown_arguments = sorted(supplied_arguments - declared_arguments)
            if unknown_arguments:
                raise ValueError(
                    f"s3transfer call {call['name']} entrypoint {entrypoint.get('symbol')} "
                    f"binds undeclared arguments: {', '.join(unknown_arguments)}"
                )
        generated_call = dict(call)
        generated_call["implementations"] = referenced_operation(
            call.get("implementations", []),
            operations,
            f"s3transfer call {call['name']}",
        )
        calls.append(generated_call)
    return {
        "apiVersion": API_VERSION,
        "kind": MAPPING_KIND,
        "metadata": {
            "name": "s3transfer.aws.s3",
            "distribution": "s3transfer",
            "distributionVersion": version,
            "language": "python",
            "service": "s3",
        },
        "dependencies": [sdk_dependency("botocore", "botocore.aws.s3")],
        "python": {"calls": calls},
    }


def boto3_mapping(
    resource_model: dict[str, Any],
    annotations: dict[str, Any],
    version: str,
    known_calls: dict[str, set[str]],
) -> dict[str, Any]:
    referenced_calls = {
        item["call"] for item in annotations.get("clientWrappers", [])
    }
    referenced_calls.update(
        item["call"] for item in annotations.get("transferClassWrappers", [])
    )
    for wrappers in annotations.get("resourceWrappers", {}).values():
        referenced_calls.update(item["call"] for item in wrappers)
    unknown_calls = sorted(referenced_calls - set(known_calls))
    if unknown_calls:
        raise ValueError(f"boto3 wrappers reference unknown s3transfer calls: {', '.join(unknown_calls)}")

    all_wrappers = list(annotations.get("clientWrappers", []))
    all_wrappers.extend(annotations.get("transferClassWrappers", []))
    for wrappers in annotations.get("resourceWrappers", {}).values():
        all_wrappers.extend(wrappers)
    for wrapper in all_wrappers:
        supplied_arguments = set(wrapper.get("arguments", {}))
        supplied_arguments.update(wrapper.get("receiverArguments", {}))
        supplied_arguments.update(
            wrapper.get("receiverContext", {}).get("arguments", {})
        )
        unknown_arguments = sorted(supplied_arguments - known_calls[wrapper["call"]])
        if unknown_arguments:
            label = wrapper.get("symbol", wrapper.get("method"))
            raise ValueError(
                f"boto3 wrapper {label} binds arguments absent from "
                f"s3transfer call {wrapper['call']}: {', '.join(unknown_arguments)}"
            )

    resources = resource_model.get("resources", {})
    service = resource_model.get("service", {})
    service_definition = dict(service)
    service_definition["has"] = service_relations(service, resources)
    service_resource = resource_binding("ServiceResource", service_definition, resources, annotations)
    service_resource["actions"] = [
        action_binding(name, value)
        for name, value in sorted(service.get("actions", {}).items())
    ]
    service_resource["collections"] = [
        collection_binding(name, value, resources)
        for name, value in sorted(service.get("hasMany", {}).items())
    ]

    return {
        "apiVersion": API_VERSION,
        "kind": MAPPING_KIND,
        "metadata": {
            "name": "boto3.aws.s3",
            "distribution": "boto3",
            "distributionVersion": version,
            "language": "python",
            "service": "s3",
        },
        "dependencies": [
            sdk_dependency("botocore", "botocore.aws.s3"),
            sdk_dependency("s3transfer", "s3transfer.aws.s3"),
        ],
        "python": {
            "aliases": annotations.get("aliases", []),
            "clientFactories": [
                {
                    **factory,
                    "service": "s3",
                    "produces": {
                        "distribution": "botocore",
                        "mapping": "botocore.aws.s3",
                        "surface": "client",
                    },
                }
                for factory in annotations.get("clientFactories", [])
            ],
            "resourceFactories": [
                {
                    **factory,
                    "service": "s3",
                    "produces": {"mapping": "boto3.aws.s3", "resource": "ServiceResource"},
                }
                for factory in annotations.get("resourceFactories", [])
            ],
            "clientWrappers": [
                wrapper_binding(item) for item in annotations.get("clientWrappers", [])
            ],
            "transferClassWrappers": [
                wrapper_binding(item)
                for item in annotations.get("transferClassWrappers", [])
            ],
            "serviceResource": service_resource,
            "resources": [
                resource_binding(name, definition, resources, annotations)
                for name, definition in sorted(resources.items())
            ],
        },
    }


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--service-mapping", type=Path, required=True)
    parser.add_argument("--botocore-service-model", type=Path, required=True)
    parser.add_argument("--paginator-model", type=Path, required=True)
    parser.add_argument("--waiter-model", type=Path, required=True)
    parser.add_argument("--resource-model", type=Path, required=True)
    parser.add_argument("--botocore-annotations", type=Path, required=True)
    parser.add_argument("--boto3-wrappers", type=Path, required=True)
    parser.add_argument("--s3transfer-annotations", type=Path, required=True)
    parser.add_argument("--botocore-output", type=Path, required=True)
    parser.add_argument("--boto3-output", type=Path, required=True)
    parser.add_argument("--s3transfer-output", type=Path, required=True)
    parser.add_argument("--botocore-version", required=True)
    parser.add_argument("--boto3-version", required=True)
    parser.add_argument("--s3transfer-version", required=True)
    args = parser.parse_args()

    service = read_document(args.service_mapping)
    botocore_service_model = read_document(args.botocore_service_model)
    paginator = read_document(args.paginator_model)
    waiter = read_document(args.waiter_model)
    resource = read_document(args.resource_model)
    boto3_annotations = read_document(args.boto3_wrappers)
    botocore_annotations = read_document(args.botocore_annotations)
    transfer_annotations = read_document(args.s3transfer_annotations)

    botocore = botocore_mapping(
        service,
        botocore_service_model,
        paginator,
        waiter,
        botocore_annotations,
        args.botocore_version,
    )
    operation_names = {item["name"] for item in botocore["operations"]}
    aliases = botocore_annotations.get("canonicalOperationAliases", {})
    transfer = s3transfer_mapping(transfer_annotations, args.s3transfer_version, operation_names)
    transfer = canonicalize_operation_refs(transfer, aliases)
    call_arguments = {
        item["name"]: set(item.get("arguments", []))
        for item in transfer["python"]["calls"]
    }
    boto3 = boto3_mapping(resource, boto3_annotations, args.boto3_version, call_arguments)
    boto3 = canonicalize_operation_refs(boto3, aliases)

    for mapping in (botocore, transfer, boto3):
        add_mapping_semantic_digest(mapping)

    write_yaml(args.botocore_output, botocore)
    write_yaml(args.s3transfer_output, transfer)
    write_yaml(args.boto3_output, boto3)

    resource_count = len(boto3["python"]["resources"])
    resource_actions = sum(len(item["actions"]) for item in boto3["python"]["resources"])
    relations = sum(len(item["relations"]) for item in boto3["python"]["resources"])
    collections = sum(len(item["collections"]) for item in boto3["python"]["resources"])
    resource_waiters = sum(len(item["waiters"]) for item in boto3["python"]["resources"])
    wrappers = (
        len(boto3["python"]["clientWrappers"])
        + len(boto3["python"]["transferClassWrappers"])
        + sum(len(item["wrappers"]) for item in boto3["python"]["resources"])
    )
    def count_operation_refs(value: Any) -> int:
        if isinstance(value, list):
            return sum(count_operation_refs(item) for item in value)
        if not isinstance(value, dict):
            return 0
        return int("operationRef" in value) + sum(
            count_operation_refs(item) for item in value.values()
        )

    transfer_refs = count_operation_refs(transfer["python"]["calls"])
    print(f"botocore operations: {len(botocore['operations'])}")
    print(f"botocore paginators: {len(botocore['python']['client']['paginatorFactory']['items'])}")
    print(f"botocore waiters: {len(botocore['python']['client']['waiterFactory']['items'])}")
    print(f"boto3 resources: {resource_count}")
    print(f"boto3 resource actions: {resource_actions}")
    print(f"boto3 relations: {relations}")
    print(f"boto3 collections: {collections}")
    print(f"boto3 resource waiters: {resource_waiters}")
    print(f"boto3 managed-transfer wrappers: {wrappers}")
    print(f"s3transfer calls: {len(transfer['python']['calls'])}")
    print(f"s3transfer operation references: {transfer_refs}")


if __name__ == "__main__":
    main()
