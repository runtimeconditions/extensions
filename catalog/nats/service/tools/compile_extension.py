#!/usr/bin/env python3
"""Compile the NATS extension and service mapping from a neutral inventory and semantic bridge."""

from __future__ import annotations

import argparse
import copy
import hashlib
import json
import re
from pathlib import Path
from typing import Any

from jsonschema import Draft202012Validator

from serialization import read_document, write_yaml


SEMVER = re.compile(r"^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$")
OPERATION_NAME = re.compile(r"^[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*$")
INVENTORY_API_VERSION = "runtimeconditions.io/service-operations/v1alpha1"
INVENTORY_KIND = "RuntimeConditionsServiceOperationsInventory"
BRIDGE_API_VERSION = "runtimeconditions.io/service-operations-semantic-bridge/v1alpha1"
BRIDGE_KIND = "RuntimeConditionsServiceOperationsSemanticBridge"


def semantic_sha256(value: Any) -> str:
    encoded = json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=False).encode("utf-8")
    return hashlib.sha256(encoded).hexdigest()


def require_string(value: Any, description: str) -> str:
    if not isinstance(value, str) or not value:
        raise ValueError(f"{description} must be a non-empty string")
    return value


def inventory_contract(inventory: dict[str, Any]) -> tuple[dict[str, str], dict[str, dict[str, Any]], dict[str, dict[str, Any]]]:
    if inventory.get("apiVersion") != INVENTORY_API_VERSION or inventory.get("kind") != INVENTORY_KIND:
        raise ValueError("inventory does not use the standard Service Operations Inventory contract")
    metadata = inventory.get("metadata")
    if not isinstance(metadata, dict):
        raise ValueError("inventory metadata must be an object")
    identity = {"name": require_string(metadata.get("name"), "inventory metadata.name"), "service": require_string(metadata.get("service"), "inventory metadata.service")}
    shapes = inventory.get("shapes")
    if not isinstance(shapes, dict) or not shapes:
        raise ValueError("inventory shapes must be a non-empty object")
    for name, definition in shapes.items():
        if not isinstance(name, str) or not isinstance(definition, dict) or set(definition) != {"schema"} or not isinstance(definition["schema"], dict):
            raise ValueError(f"inventory shape {name!r} must contain exactly one schema object")
        Draft202012Validator.check_schema(definition["schema"])
    configured = inventory.get("operations")
    if not isinstance(configured, list) or not configured:
        raise ValueError("inventory operations must be a non-empty list")
    operations: dict[str, dict[str, Any]] = {}
    for index, operation in enumerate(configured):
        if not isinstance(operation, dict):
            raise ValueError(f"inventory operations[{index}] must be an object")
        name = require_string(operation.get("name"), f"inventory operations[{index}].name")
        resource = require_string(operation.get("resource"), f"inventory operation {name}.resource")
        action = require_string(operation.get("action"), f"inventory operation {name}.action")
        if not OPERATION_NAME.fullmatch(name) or name != f"{resource}.{action}" or name in operations:
            raise ValueError(f"inventory operation {name!r} must be one unique resource.action identifier")
        inputs = operation.get("inputs", {})
        if not isinstance(inputs, dict):
            raise ValueError(f"inventory operation {name} inputs must be an object")
        for input_name, definition in inputs.items():
            if not isinstance(input_name, str) or not isinstance(definition, dict) or set(definition) != {"shape", "required"}:
                raise ValueError(f"inventory operation {name} input {input_name!r} is invalid")
            if definition.get("shape") not in shapes or not isinstance(definition.get("required"), bool):
                raise ValueError(f"inventory operation {name} input {input_name!r} has an invalid shape or requiredness")
        operations[name] = operation
    semantic_body = {"shapes": shapes, "operations": configured}
    if metadata.get("operationCount") != len(configured) or metadata.get("semanticSha256") != semantic_sha256(semantic_body):
        raise ValueError("inventory operation count or semantic digest does not match its service-owned content")
    return identity, shapes, operations


def bridge_contract(bridge: dict[str, Any], inventory_identity: dict[str, str], inventory: dict[str, Any], shapes: dict[str, dict[str, Any]], operations: dict[str, dict[str, Any]]) -> tuple[dict[str, Any], dict[str, dict[str, Any]], list[dict[str, Any]]]:
    if bridge.get("apiVersion") != BRIDGE_API_VERSION or bridge.get("kind") != BRIDGE_KIND:
        raise ValueError("semantic bridge does not use the standard Service Operations Semantic Bridge contract")
    metadata = bridge.get("metadata")
    if not isinstance(metadata, dict) or metadata.get("name") != inventory_identity["name"] or metadata.get("service") != inventory_identity["service"]:
        raise ValueError("semantic bridge identity does not match the inventory")
    inventory_ref = bridge.get("operationSource")
    if not isinstance(inventory_ref, dict) or inventory_ref.get("kind") != "ServiceOperationsInventory":
        raise ValueError("semantic bridge operationSource must identify a Service Operations Inventory")
    for field in ("name", "service", "semanticSha256"):
        expected = inventory["metadata"].get(field)
        if inventory_ref.get(field) != expected:
            raise ValueError(f"semantic bridge operationSource {field} does not match the exact inventory")
    extension = bridge.get("extension")
    if not isinstance(extension, dict):
        raise ValueError("semantic bridge extension must be an object")
    for field in ("id", "version", "conditionKind", "interfaceType"):
        require_string(extension.get(field), f"semantic bridge extension.{field}")
    if not SEMVER.fullmatch(extension["version"]) or f"/{extension['version']}/" not in extension["id"]:
        raise ValueError("semantic bridge extension id and version must identify the same semantic release")
    configured_fields = bridge.get("conditionFields")
    if not isinstance(configured_fields, dict) or not configured_fields:
        raise ValueError("semantic bridge conditionFields must be a non-empty object")
    condition_fields: dict[str, dict[str, Any]] = {}
    condition_field_shapes: dict[str, str] = {}
    for field, definition in configured_fields.items():
        if not isinstance(field, str) or not isinstance(definition, dict) or set(definition) != {"inventoryShape"}:
            raise ValueError(f"semantic bridge condition field {field!r} is invalid")
        shape_name = definition.get("inventoryShape")
        if shape_name not in shapes:
            raise ValueError(f"semantic bridge condition field {field!r} references unknown inventory shape {shape_name!r}")
        condition_field_shapes[field] = shape_name
        condition_fields[field] = {"schema": copy.deepcopy(shapes[shape_name]["schema"])}
    mappings = bridge.get("operationMappings")
    if not isinstance(mappings, list) or not mappings:
        raise ValueError("semantic bridge operationMappings must be a non-empty list")
    seen: set[str] = set()
    compiled: list[dict[str, Any]] = []
    for index, mapping in enumerate(mappings):
        if not isinstance(mapping, dict):
            raise ValueError(f"semantic bridge operationMappings[{index}] must be an object")
        name = require_string(mapping.get("operation"), f"semantic bridge operationMappings[{index}].operation")
        if name in seen or name not in operations:
            raise ValueError(f"semantic bridge operation {name!r} is duplicate or absent from the inventory")
        seen.add(name)
        condition = mapping.get("condition")
        if not isinstance(condition, dict) or set(condition) != {"resource", "action"}:
            raise ValueError(f"semantic bridge operation {name} condition must contain exactly resource and action")
        operation = {"resource": require_string(condition.get("resource"), f"semantic bridge operation {name} condition.resource"), "action": require_string(condition.get("action"), f"semantic bridge operation {name} condition.action")}
        configured_bindings = mapping.get("bindings", {})
        if not isinstance(configured_bindings, dict):
            raise ValueError(f"semantic bridge operation {name} bindings must be an object")
        required: list[str] = []
        optional: list[str] = []
        inventory_inputs = operations[name].get("inputs", {})
        for field, binding in configured_bindings.items():
            if field not in condition_fields or not isinstance(binding, dict) or set(binding) != {"input", "required"}:
                raise ValueError(f"semantic bridge operation {name} binding {field!r} is invalid")
            input_name = binding.get("input")
            if input_name not in inventory_inputs:
                raise ValueError(f"semantic bridge operation {name} binding {field!r} references unknown input {input_name!r}")
            if inventory_inputs[input_name]["shape"] != condition_field_shapes[field]:
                raise ValueError(f"semantic bridge operation {name} binding {field!r} changes the inventory input shape")
            if not isinstance(binding.get("required"), bool):
                raise ValueError(f"semantic bridge operation {name} binding {field!r} requiredness must be boolean")
            (required if binding["required"] else optional).append(field)
        compiled.append({"name": name, "conditions": [{"kind": extension["conditionKind"], "interfaceType": extension["interfaceType"], "operation": operation, "bindings": {"required": required, "optional": optional}}]})
    return extension, condition_fields, compiled


def operation_schema(operation: dict[str, Any], fields: dict[str, dict[str, Any]]) -> dict[str, Any]:
    fixed = operation["conditions"][0]["operation"]
    bindings = operation["conditions"][0]["bindings"]
    properties = {"resource": {"const": fixed["resource"]}, "action": {"const": fixed["action"]}}
    for field in bindings["required"] + bindings["optional"]:
        properties[field] = copy.deepcopy(fields[field]["schema"])
    return {"type": "object", "required": ["resource", "action", *bindings["required"]], "properties": properties, "additionalProperties": False}


def build(inventory: dict[str, Any], bridge: dict[str, Any]) -> tuple[dict[str, Any], dict[str, Any]]:
    inventory_identity, shapes, inventory_operations = inventory_contract(inventory)
    extension_config, fields, operations = bridge_contract(bridge, inventory_identity, inventory, shapes, inventory_operations)
    kind = extension_config["conditionKind"]
    interface_type = extension_config["interfaceType"]
    resources = list(dict.fromkeys(item["conditions"][0]["operation"]["resource"] for item in operations))
    actions = sorted({item["conditions"][0]["operation"]["action"] for item in operations})
    schema = {
        "$schema": "https://json-schema.org/draft/2020-12/schema",
        "type": "object",
        "required": ["kind", "interface"],
        "properties": {
            "kind": {"const": kind},
            "interface": {
                "type": "object",
                "required": ["type", "operations"],
                "properties": {"type": {"const": interface_type}, "operations": {"type": "array", "minItems": 1, "uniqueItems": True, "items": {"oneOf": [operation_schema(operation, fields) for operation in operations]} }},
                "additionalProperties": False,
            },
        },
        "additionalProperties": True,
    }
    Draft202012Validator.check_schema(schema)
    spec = {
        "kinds": [{"name": kind}],
        "interfaceTypes": [{"name": interface_type, "targetKind": kind}],
        "interfaceFields": [{"name": "operations", "targetKind": kind, "targetType": interface_type}],
        "fieldValues": [
            {"field": "interface.operations[].resource", "targetKind": kind, "targetType": interface_type, "values": resources},
            {"field": "interface.operations[].action", "targetKind": kind, "targetType": interface_type, "values": actions},
        ],
        "schemas": [{"id": "nats-service-interface", "appliesToKind": kind, "appliesToInterfaceType": interface_type, "description": "Validates adapter-actionable NATS connection, subject authorization, and JetStream resource requirements.", "schema": schema}],
    }
    digest = semantic_sha256(spec)
    extension = {"apiVersion": "runtimeconditions.io/v1alpha1", "kind": "RuntimeConditionsExtensionDefinition", "metadata": {"id": extension_config["id"], "version": extension_config["version"], "semanticSha256": digest}, "spec": spec}
    semantic_body = {"fields": fields, "operations": operations}
    service_mapping = {
        "apiVersion": "runtimeconditions.io/service-mapping/v1alpha1",
        "kind": "RuntimeConditionsServiceMapping",
        "metadata": {
            **inventory_identity,
            "operationCount": len(operations),
            "sourceInventorySemanticSha256": inventory["metadata"]["semanticSha256"],
            "semanticBridgeSha256": semantic_sha256(bridge),
            "semanticSha256": semantic_sha256(semantic_body),
        },
        "extension": {"id": extension_config["id"], "version": extension_config["version"], "semanticSha256": digest},
        **semantic_body,
    }
    return extension, service_mapping


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--inventory", type=Path, required=True)
    parser.add_argument("--bridge", type=Path, required=True)
    parser.add_argument("--extension-output", type=Path, required=True)
    parser.add_argument("--service-mapping-output", type=Path, required=True)
    args = parser.parse_args()
    extension, service_mapping = build(read_document(args.inventory), read_document(args.bridge))
    write_yaml(args.extension_output, extension)
    write_yaml(args.service_mapping_output, service_mapping)
    print(f"extension: {extension['metadata']['id']}")
    print(f"extension semantic sha256: {extension['metadata']['semanticSha256']}")
    print(f"service operations: {service_mapping['metadata']['operationCount']}")
    print(f"service mapping semantic sha256: {service_mapping['metadata']['semanticSha256']}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
