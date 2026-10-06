"""Strict data-only loading of the normalized model and one Python target."""

from __future__ import annotations

import keyword
import math
import re
from collections.abc import Hashable
from dataclasses import dataclass
from pathlib import Path
from typing import Any, NoReturn, cast

import yaml

MAX_BYTES = 64 * 1024 * 1024
MAX_NODES = 1_000_000
MAX_DEPTH = 256
MAX_ALIASES = 100
MODEL_API = "runtimeconditions.io/binding-model/v1alpha1"
MODEL_KIND = "RuntimeConditionsBindingModel"
TARGET_API = "runtimeconditions.io/python-package-target/v1alpha1"
TARGET_KIND = "RuntimeConditionsPythonPackageTarget"
VERSION_PATTERN = re.compile(
    r"(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)"
    r"(?:-(?:alpha|beta|rc)\.(0|[1-9][0-9]*))?\Z"
)
KEY_PATTERN = re.compile(r"[a-z0-9]+(?:-[a-z0-9]+)*\Z")
IMPORT_PATTERN = re.compile(r"[a-z][a-z0-9_]*\Z")
SHA_PATTERN = re.compile(r"[0-9a-f]{64}\Z")


@dataclass(frozen=True)
class Diagnostic:
    category: str
    code: str
    coordinate: str
    message: str
    json_pointer: str = ""


class DiagnosticError(ValueError):
    def __init__(self, diagnostic: Diagnostic):
        self.diagnostic = diagnostic
        location = diagnostic.coordinate
        if diagnostic.json_pointer:
            location += f" {diagnostic.json_pointer}"
        super().__init__(f"{diagnostic.code} {location}: {diagnostic.message}")


def fail(
    category: str, code: str, coordinate: str, message: str, pointer: str = ""
) -> NoReturn:
    raise DiagnosticError(Diagnostic(category, code, coordinate, message, pointer))


@dataclass(frozen=True)
class PackageDependency:
    extension: str
    distribution_name: str
    import_package: str
    version: str


@dataclass(frozen=True)
class PackageTarget:
    package_key: str
    root_extension: str
    distribution_name: str
    import_package: str
    version: str
    source_directory: str
    minimum_python_version: str
    publication_mode: str
    repository_url: str
    registry_id: str | None
    dependencies: tuple[PackageDependency, ...]
    emitter_sha256: str = ""


class _LimitedLoader(yaml.SafeLoader):
    def __init__(self, stream: str):
        super().__init__(stream)
        self.nodes = 0
        self.depth = 0
        self.aliases = 0

    def compose_node(self, parent: yaml.Node | None, index: int) -> yaml.Node | None:
        self.nodes += 1
        if self.nodes > MAX_NODES:
            raise ValueError("YAML node limit exceeded")
        if self.check_event(yaml.AliasEvent):
            self.aliases += 1
            if self.aliases > MAX_ALIASES:
                raise ValueError("YAML alias limit exceeded")
        self.depth += 1
        if self.depth > MAX_DEPTH:
            raise ValueError("YAML nesting limit exceeded")
        try:
            return super().compose_node(parent, index)
        finally:
            self.depth -= 1

    def construct_mapping(
        self, node: yaml.MappingNode, deep: bool = False
    ) -> dict[Hashable, Any]:
        result: dict[Hashable, Any] = {}
        for key_node, value_node in node.value:
            key = self.construct_object(key_node, deep=deep)
            if not isinstance(key, str):
                raise ValueError("YAML mapping keys must be strings")
            if key in result:
                raise ValueError(f"duplicate YAML mapping key {key!r}")
            result[key] = self.construct_object(value_node, deep=deep)
        return result


def _load_yaml(path: str | Path, *, model: bool) -> dict[str, Any]:
    coordinate = "model" if model else "package-target"
    try:
        data = Path(path).read_bytes()
    except OSError as error:
        fail(
            "input", "RCP1001", coordinate, f"cannot read YAML input: {error.strerror}"
        )
    if len(data) > MAX_BYTES:
        fail("input", "RCP1001", coordinate, "YAML input exceeds 64 MiB")
    if model and (
        data.startswith(b"\xef\xbb\xbf")
        or b"\r" in data
        or not data.endswith(b"\n")
        or data.endswith(b"\n\n")
    ):
        fail(
            "input",
            "RCP1002",
            coordinate,
            "binding model must use UTF-8 without BOM and LF with one trailing newline",
        )
    try:
        source = data.decode("utf-8")
        if model:
            covered_through = 0
            for token in yaml.scan(source):
                if "#" in source[covered_through : token.start_mark.index]:
                    raise ValueError("binding model may not contain YAML comments")
                if isinstance(
                    token,
                    (
                        yaml.tokens.AnchorToken,
                        yaml.tokens.AliasToken,
                        yaml.tokens.TagToken,
                    ),
                ):
                    raise ValueError(
                        "binding model may not contain YAML anchors, aliases, or tags"
                    )
                covered_through = max(covered_through, token.end_mark.index)
            if "#" in source[covered_through:]:
                raise ValueError("binding model may not contain YAML comments")
        loader = _LimitedLoader(source)
        try:
            if not loader.check_data():  # type: ignore[no-untyped-call]
                raise ValueError("YAML document is empty")
            value = loader.get_data()
            if loader.check_data():  # type: ignore[no-untyped-call]
                raise ValueError("YAML input must contain exactly one document")
            if model and loader.aliases:
                raise ValueError("binding model may not contain YAML aliases")
        finally:
            loader.dispose()
    except (UnicodeError, yaml.YAMLError, ValueError, RecursionError) as error:
        fail("input", "RCP1002", coordinate, str(error))
    _check_data_tree(value, coordinate)
    if not isinstance(value, dict):
        fail("input", "RCP1002", coordinate, "YAML document must be a mapping")
    return cast(dict[str, Any], value)


def _check_data_tree(value: Any, coordinate: str) -> None:
    active: set[int] = set()
    count = 0

    def visit(node: Any, depth: int) -> None:
        nonlocal count
        count += 1
        if count > MAX_NODES:
            fail("input", "RCP1002", coordinate, "decoded YAML node limit exceeded")
        if depth > MAX_DEPTH:
            fail("input", "RCP1002", coordinate, "decoded YAML nesting limit exceeded")
        if isinstance(node, (dict, list)):
            identity = id(node)
            if identity in active:
                fail(
                    "input",
                    "RCP1002",
                    coordinate,
                    "cyclic YAML aliases are unsupported",
                )
            active.add(identity)
            if isinstance(node, dict):
                for key, child in node.items():
                    if not isinstance(key, str):
                        fail(
                            "input",
                            "RCP1002",
                            coordinate,
                            "YAML mapping keys must be strings",
                        )
                    visit(child, depth + 1)
            else:
                for child in node:
                    visit(child, depth + 1)
            active.remove(identity)
        elif isinstance(node, float) and not math.isfinite(node):
            fail("input", "RCP1002", coordinate, "YAML contains a non-finite number")
        elif not (node is None or isinstance(node, (str, int, float, bool))):
            fail("input", "RCP1002", coordinate, "YAML contains a non-data value")

    visit(value, 0)


def _pointer(base: str, key: str | int) -> str:
    return base + "/" + str(key).replace("~", "~0").replace("/", "~1")


def _object(
    value: Any, allowed: set[str], required: set[str], pointer: str
) -> dict[str, Any]:
    if not isinstance(value, dict):
        fail("model", "RCP1003", "model", "expected mapping", pointer)
    for key in sorted(required - value.keys()):
        fail(
            "model",
            "RCP1003",
            "model",
            f"missing required field {key!r}",
            _pointer(pointer, key),
        )
    for key in sorted(value.keys() - allowed):
        fail(
            "model",
            "RCP1003",
            "model",
            f"unknown field {key!r}",
            _pointer(pointer, key),
        )
    return cast(dict[str, Any], value)


def _string(value: Any, pointer: str) -> str:
    if not isinstance(value, str) or not value:
        fail("model", "RCP1003", "model", "expected nonempty string", pointer)
    return value


def _sequence(value: Any, pointer: str) -> list[Any]:
    if not isinstance(value, list):
        fail("model", "RCP1003", "model", "expected sequence", pointer)
    return value


def _strings(item: dict[str, Any], pointer: str, *names: str) -> None:
    for name in names:
        if name in item:
            _string(item[name], _pointer(pointer, name))


def _sha(value: Any, pointer: str) -> None:
    if not isinstance(value, str) or not SHA_PATTERN.fullmatch(value):
        fail(
            "model",
            "RCP1003",
            "model",
            "expected lowercase SHA-256 hex digest",
            pointer,
        )


def _records(
    value: Any, pointer: str, allowed: set[str], required: set[str]
) -> list[dict[str, Any]]:
    return [
        _object(item, allowed, required, _pointer(pointer, index))
        for index, item in enumerate(_sequence(value, pointer))
    ]


def _provenance(value: Any, pointer: str) -> None:
    item = _object(
        value,
        {"owner", "extensionSha256", "coordinate", "jsonPointer"},
        {"owner", "extensionSha256", "coordinate"},
        pointer,
    )
    _strings(item, pointer, "owner", "coordinate")
    _sha(item["extensionSha256"], _pointer(pointer, "extensionSha256"))
    if "jsonPointer" in item and not isinstance(item["jsonPointer"], str):
        fail(
            "model",
            "RCP1003",
            "model",
            "expected string",
            _pointer(pointer, "jsonPointer"),
        )


def _shape(value: Any, pointer: str) -> None:
    item = _object(
        value,
        {
            "kind",
            "scalar",
            "ref",
            "required",
            "properties",
            "items",
            "mapValues",
            "variants",
            "values",
            "constraints",
            "provenance",
        },
        {"kind", "provenance"},
        pointer,
    )
    kind = _string(item["kind"], _pointer(pointer, "kind"))
    _provenance(item["provenance"], _pointer(pointer, "provenance"))
    supported = {"scalar", "object", "array", "map", "union", "ref", "any"}
    if kind not in supported:
        fail(
            "model",
            "RCP1010",
            item["provenance"]["coordinate"],
            f"unknown structural node {kind!r}",
            pointer,
        )
    needed = {
        "scalar": "scalar",
        "array": "items",
        "map": "mapValues",
        "union": "variants",
        "ref": "ref",
    }
    if kind in needed and needed[kind] not in item:
        fail(
            "model",
            "RCP1003",
            "model",
            f"missing required field {needed[kind]!r}",
            _pointer(pointer, needed[kind]),
        )
    if kind == "scalar" and (
        not isinstance(item["scalar"], str)
        or item["scalar"] not in {"string", "boolean", "integer", "number", "null"}
    ):
        fail(
            "model",
            "RCP1003",
            "model",
            "unknown scalar type",
            _pointer(pointer, "scalar"),
        )
    if kind == "ref":
        _string(item["ref"], _pointer(pointer, "ref"))
    if kind == "array":
        _shape(item["items"], _pointer(pointer, "items"))
    if kind == "map":
        _shape(item["mapValues"], _pointer(pointer, "mapValues"))
    if kind == "union":
        variants = _sequence(item["variants"], _pointer(pointer, "variants"))
        if not variants:
            fail(
                "model",
                "RCP1003",
                "model",
                "union must have variants",
                _pointer(pointer, "variants"),
            )
        for index, variant in enumerate(variants):
            _shape(variant, _pointer(_pointer(pointer, "variants"), index))
    if kind == "object":
        for index, prop in enumerate(
            _records(
                item.get("properties", []),
                _pointer(pointer, "properties"),
                {"name", "required", "shape", "provenance"},
                {"name", "shape", "provenance"},
            )
        ):
            prop_pointer = _pointer(_pointer(pointer, "properties"), index)
            _string(prop["name"], _pointer(prop_pointer, "name"))
            _shape(prop["shape"], _pointer(prop_pointer, "shape"))
            _provenance(prop["provenance"], _pointer(prop_pointer, "provenance"))
            if "required" in prop and not isinstance(prop["required"], bool):
                fail(
                    "model",
                    "RCP1003",
                    "model",
                    "expected boolean",
                    _pointer(prop_pointer, "required"),
                )
    if "required" in item:
        for index, name in enumerate(
            _sequence(item["required"], _pointer(pointer, "required"))
        ):
            _string(name, _pointer(_pointer(pointer, "required"), index))
    if "values" in item:
        for index, entry in enumerate(
            _records(item["values"], _pointer(pointer, "values"), {"value"}, {"value"})
        ):
            if isinstance(entry["value"], (list, dict)):
                fail(
                    "model",
                    "RCP1003",
                    "model",
                    "value-domain member must be scalar",
                    _pointer(_pointer(_pointer(pointer, "values"), index), "value"),
                )
    if "constraints" in item and not isinstance(item["constraints"], dict):
        fail(
            "model",
            "RCP1003",
            "model",
            "constraints must be a mapping",
            _pointer(pointer, "constraints"),
        )


def _validate_model(value: dict[str, Any]) -> None:
    model = _object(
        value,
        {
            "apiVersion",
            "kind",
            "metadata",
            "coreProfileSchema",
            "rootExtension",
            "extensions",
            "dependencyEdges",
            "vocabulary",
            "scopes",
            "schemas",
        },
        {
            "apiVersion",
            "kind",
            "metadata",
            "coreProfileSchema",
            "rootExtension",
            "extensions",
            "vocabulary",
        },
        "",
    )
    if model["apiVersion"] != MODEL_API:
        fail(
            "model",
            "RCP1004",
            "model",
            f"unsupported model apiVersion {model['apiVersion']!r}",
            "/apiVersion",
        )
    if model["kind"] != MODEL_KIND:
        fail(
            "model",
            "RCP1005",
            "model",
            f"unsupported model kind {model['kind']!r}",
            "/kind",
        )
    metadata = _object(
        model["metadata"],
        {"semanticSha256", "normalizer"},
        {"semanticSha256", "normalizer"},
        "/metadata",
    )
    _sha(metadata["semanticSha256"], "/metadata/semanticSha256")
    tool = _object(
        metadata["normalizer"],
        {"name", "version", "sha256"},
        {"name", "version", "sha256"},
        "/metadata/normalizer",
    )
    _strings(tool, "/metadata/normalizer", "name", "version")
    _sha(tool["sha256"], "/metadata/normalizer/sha256")
    for name, required in (
        ("coreProfileSchema", {"id", "version", "semanticSha256"}),
        ("rootExtension", {"id", "semanticSha256"}),
    ):
        item = _object(
            model[name],
            {"id", "version", "semanticSha256"},
            required,
            _pointer("", name),
        )
        _strings(item, _pointer("", name), "id", "version")
        _sha(item["semanticSha256"], _pointer(_pointer("", name), "semanticSha256"))
    extensions = _records(
        model["extensions"],
        "/extensions",
        {"id", "version", "semanticSha256", "dependencies"},
        {"id", "semanticSha256"},
    )
    if not extensions:
        fail("model", "RCP1003", "model", "extensions must not be empty", "/extensions")
    extension_ids: set[str] = set()
    for index, item in enumerate(extensions):
        for name in ("id", "semanticSha256"):
            if name == "id":
                _string(item[name], _pointer(_pointer("/extensions", index), name))
            else:
                _sha(item[name], _pointer(_pointer("/extensions", index), name))
        _strings(item, _pointer("/extensions", index), "version")
        if item["id"] in extension_ids:
            fail(
                "model",
                "RCP1003",
                "model",
                f"duplicate extension {item['id']!r}",
                _pointer(_pointer("/extensions", index), "id"),
            )
        extension_ids.add(item["id"])
        dependencies_seen: set[str] = set()
        for dep_index, dep in enumerate(
            _sequence(
                item.get("dependencies", []),
                _pointer(_pointer("/extensions", index), "dependencies"),
            )
        ):
            _string(
                dep,
                _pointer(
                    _pointer(_pointer("/extensions", index), "dependencies"), dep_index
                ),
            )
            if dep in dependencies_seen:
                fail(
                    "model",
                    "RCP1003",
                    "model",
                    f"duplicate dependency {dep!r}",
                    _pointer(
                        _pointer(_pointer("/extensions", index), "dependencies"),
                        dep_index,
                    ),
                )
            dependencies_seen.add(dep)
    if model["rootExtension"]["id"] not in extension_ids:
        fail(
            "model",
            "RCP1003",
            "model",
            "root extension is missing from closure",
            "/rootExtension/id",
        )
    for index, item in enumerate(extensions):
        for dep_index, dep in enumerate(item.get("dependencies", [])):
            if dep not in extension_ids:
                fail(
                    "model",
                    "RCP1003",
                    "model",
                    f"dependency {dep!r} is missing from closure",
                    _pointer(
                        _pointer(_pointer("/extensions", index), "dependencies"),
                        dep_index,
                    ),
                )
    vocab = _object(
        model["vocabulary"],
        {
            "owners",
            "ownedDeclarations",
            "importedDeclarations",
            "interfaces",
            "conditionFields",
            "interfaceFields",
            "valueDomains",
        },
        set(),
        "/vocabulary",
    )
    for index, edge in enumerate(
        _records(
            model.get("dependencyEdges", []),
            "/dependencyEdges",
            {"from", "to"},
            {"from", "to"},
        )
    ):
        _strings(edge, _pointer("/dependencyEdges", index), "from", "to")
    categories = {
        "owners": (
            {
                "coordinate",
                "category",
                "owner",
                "kind",
                "interfaceType",
                "path",
                "value",
            },
            {"coordinate", "category", "owner"},
        ),
        "ownedDeclarations": (
            {"coordinate", "owner", "kind", "provenance"},
            {"coordinate", "owner", "kind", "provenance"},
        ),
        "importedDeclarations": (
            {"coordinate", "owner", "kind", "provenance"},
            {"coordinate", "owner", "kind", "provenance"},
        ),
        "interfaces": (
            {"coordinate", "owner", "kind", "type", "provenance"},
            {"coordinate", "owner", "kind", "type", "provenance"},
        ),
        "conditionFields": (
            {
                "coordinate",
                "owner",
                "kind",
                "interfaceType",
                "path",
                "segments",
                "provenance",
            },
            {"coordinate", "owner", "kind", "path", "segments", "provenance"},
        ),
        "interfaceFields": (
            {
                "coordinate",
                "owner",
                "kind",
                "interfaceType",
                "path",
                "segments",
                "provenance",
            },
            {"coordinate", "owner", "kind", "path", "segments", "provenance"},
        ),
        "valueDomains": (
            {
                "coordinate",
                "owner",
                "kind",
                "interfaceType",
                "path",
                "segments",
                "values",
                "provenance",
            },
            {"coordinate", "owner", "kind", "path", "segments", "values", "provenance"},
        ),
    }
    for name, (allowed, required) in categories.items():
        for index, item in enumerate(
            _records(
                vocab.get(name, []), _pointer("/vocabulary", name), allowed, required
            )
        ):
            pointer = _pointer(_pointer("/vocabulary", name), index)
            _strings(
                item,
                pointer,
                "coordinate",
                "category",
                "owner",
                "kind",
                "interfaceType",
                "path",
                "type",
            )
            if "provenance" in item:
                _provenance(item["provenance"], _pointer(pointer, "provenance"))
            if "segments" in item:
                segments = _records(
                    item["segments"],
                    _pointer(pointer, "segments"),
                    {"name", "array"},
                    {"name"},
                )
                if not segments:
                    fail(
                        "model",
                        "RCP1003",
                        "model",
                        "field path has no segments",
                        _pointer(pointer, "segments"),
                    )
                for segment_index, segment in enumerate(segments):
                    segment_pointer = _pointer(
                        _pointer(pointer, "segments"), segment_index
                    )
                    _string(segment["name"], _pointer(segment_pointer, "name"))
                    if "array" in segment and not isinstance(segment["array"], bool):
                        fail(
                            "model",
                            "RCP1003",
                            "model",
                            "expected boolean",
                            _pointer(segment_pointer, "array"),
                        )
            if "values" in item:
                values = _records(
                    item["values"], _pointer(pointer, "values"), {"value"}, {"value"}
                )
                if not values:
                    fail(
                        "model",
                        "RCP1003",
                        "model",
                        "value domain must not be empty",
                        _pointer(pointer, "values"),
                    )
                for value_index, entry in enumerate(values):
                    _string(
                        entry["value"],
                        _pointer(
                            _pointer(_pointer(pointer, "values"), value_index), "value"
                        ),
                    )
    for index, scope in enumerate(
        _records(
            model.get("scopes", []),
            "/scopes",
            {"coordinate", "kind", "interfaceType", "applicableSchemas", "projection"},
            {"coordinate", "kind"},
        )
    ):
        pointer = _pointer("/scopes", index)
        _strings(scope, pointer, "coordinate", "kind", "interfaceType")
        for schema_index, identifier in enumerate(
            _sequence(
                scope.get("applicableSchemas", []),
                _pointer(pointer, "applicableSchemas"),
            )
        ):
            _string(
                identifier,
                _pointer(_pointer(pointer, "applicableSchemas"), schema_index),
            )
        if "projection" in scope:
            _shape(scope["projection"], _pointer(pointer, "projection"))
    for index, schema in enumerate(
        _records(
            model.get("schemas", []),
            "/schemas",
            {
                "coordinate",
                "owner",
                "id",
                "kind",
                "interfaceType",
                "exact",
                "projection",
                "definitions",
                "provenance",
            },
            {"coordinate", "owner", "id", "exact", "projection", "provenance"},
        )
    ):
        pointer = _pointer("/schemas", index)
        _strings(schema, pointer, "coordinate", "owner", "id", "kind", "interfaceType")
        if not isinstance(schema["exact"], dict):
            fail(
                "model",
                "RCP1003",
                "model",
                "exact schema must be a mapping",
                _pointer(pointer, "exact"),
            )
        _shape(schema["projection"], _pointer(pointer, "projection"))
        _provenance(schema["provenance"], _pointer(pointer, "provenance"))
        for def_index, definition in enumerate(
            _records(
                schema.get("definitions", []),
                _pointer(pointer, "definitions"),
                {"name", "jsonPointer", "shape", "provenance"},
                {"name", "jsonPointer", "shape", "provenance"},
            )
        ):
            def_pointer = _pointer(_pointer(pointer, "definitions"), def_index)
            _strings(definition, def_pointer, "name", "jsonPointer")
            _shape(definition["shape"], _pointer(def_pointer, "shape"))
            _provenance(definition["provenance"], _pointer(def_pointer, "provenance"))


def load_model(path: str | Path) -> dict[str, Any]:
    value = _load_yaml(path, model=True)
    _validate_model(value)
    return value


def _target_string(value: Any, name: str) -> str:
    if not isinstance(value, str) or not value:
        fail(
            "package-config", "RCP1006", "package-target", f"invalid {name}", f"/{name}"
        )
    return value


def load_target(path: str | Path) -> PackageTarget:
    value = _load_yaml(path, model=False)
    allowed = {
        "apiVersion",
        "kind",
        "packageKey",
        "rootExtension",
        "distributionName",
        "importPackage",
        "version",
        "sourceDirectory",
        "minimumPythonVersion",
        "publicationMode",
        "registryId",
        "repositoryUrl",
        "dependencies",
        "emitterSha256",
    }
    required = allowed - {"registryId", "dependencies", "emitterSha256"}
    for name in sorted(required - value.keys()):
        fail(
            "package-config",
            "RCP1006",
            "package-target",
            f"missing required field {name!r}",
            f"/{name}",
        )
    for name in sorted(value.keys() - allowed):
        fail(
            "package-config",
            "RCP1006",
            "package-target",
            f"unknown field {name!r}",
            f"/{name}",
        )
    if value["apiVersion"] != TARGET_API or value["kind"] != TARGET_KIND:
        fail(
            "package-config",
            "RCP1006",
            "package-target",
            "unsupported target apiVersion or kind",
        )
    emitter_digest = value.get("emitterSha256", "")
    if "emitterSha256" in value and (
        not isinstance(emitter_digest, str) or not SHA_PATTERN.fullmatch(emitter_digest)
    ):
        fail(
            "package-config",
            "RCP1006",
            "package-target",
            "emitterSha256 must be a 64-character lowercase SHA-256 of the emitter tool",
            "/emitterSha256",
        )
    for name in required - {"apiVersion", "kind"}:
        _target_string(value[name], name)
    for name in ("packageKey", "distributionName"):
        if not KEY_PATTERN.fullmatch(value[name]):
            fail(
                "package-config",
                "RCP1006",
                "package-target",
                f"invalid {name}",
                f"/{name}",
            )
    if not IMPORT_PATTERN.fullmatch(value["importPackage"]) or keyword.iskeyword(
        value["importPackage"]
    ):
        fail(
            "package-config",
            "RCP1006",
            "package-target",
            "invalid importPackage",
            "/importPackage",
        )
    if not VERSION_PATTERN.fullmatch(value["version"]):
        fail(
            "package-config",
            "RCP1006",
            "package-target",
            "unsupported package version",
            "/version",
        )
    if value["sourceDirectory"] != f"bindings/{value['packageKey']}/python":
        fail(
            "package-config",
            "RCP1007",
            value["packageKey"],
            "source directory does not match package key",
            "/sourceDirectory",
        )
    minimum = value["minimumPythonVersion"]
    if not re.fullmatch(r"(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)", minimum) or tuple(
        int(part) for part in minimum.split(".")
    ) < (3, 11):
        fail(
            "package-config",
            "RCP1008",
            value["packageKey"],
            "unsupported minimum Python version",
            "/minimumPythonVersion",
        )
    if value["publicationMode"] not in {"github-tag", "registry"}:
        fail(
            "package-config",
            "RCP1006",
            value["packageKey"],
            "unsupported publication mode",
            "/publicationMode",
        )
    if not value["repositoryUrl"].startswith("https://") or any(
        character.isspace() for character in value["repositoryUrl"]
    ):
        fail(
            "package-config",
            "RCP1006",
            value["packageKey"],
            "repositoryUrl must use https",
            "/repositoryUrl",
        )
    registry = value.get("registryId")
    if (value["publicationMode"] == "registry") != (registry is not None):
        fail(
            "package-config",
            "RCP1006",
            value["packageKey"],
            "registryId is required exactly for registry publication",
            "/registryId",
        )
    if registry is not None:
        _target_string(registry, "registryId")
    entries = value.get("dependencies", [])
    if not isinstance(entries, list):
        fail(
            "package-config",
            "RCP1006",
            value["packageKey"],
            "dependencies must be a sequence",
            "/dependencies",
        )
    dependencies: list[PackageDependency] = []
    seen: set[str] = set()
    for index, entry in enumerate(entries):
        pointer = f"/dependencies/{index}"
        if not isinstance(entry, dict):
            fail(
                "package-config",
                "RCP1006",
                value["packageKey"],
                "dependency must be a mapping",
                pointer,
            )
        fields = {"extension", "distributionName", "importPackage", "version"}
        for name in sorted(fields - entry.keys()):
            fail(
                "package-config",
                "RCP1006",
                value["packageKey"],
                f"missing dependency field {name!r}",
                f"{pointer}/{name}",
            )
        for name in sorted(entry.keys() - fields):
            fail(
                "package-config",
                "RCP1006",
                value["packageKey"],
                f"unknown dependency field {name!r}",
                f"{pointer}/{name}",
            )
        for name in fields:
            _target_string(entry[name], name)
        if entry["extension"] in seen:
            fail(
                "package-config",
                "RCP1006",
                value["packageKey"],
                f"duplicate dependency {entry['extension']!r}",
                pointer,
            )
        seen.add(entry["extension"])
        if (
            not KEY_PATTERN.fullmatch(entry["distributionName"])
            or not IMPORT_PATTERN.fullmatch(entry["importPackage"])
            or keyword.iskeyword(entry["importPackage"])
            or not VERSION_PATTERN.fullmatch(entry["version"])
        ):
            fail(
                "package-config",
                "RCP1006",
                value["packageKey"],
                "invalid dependency package metadata",
                pointer,
            )
        dependencies.append(
            PackageDependency(
                entry["extension"],
                entry["distributionName"],
                entry["importPackage"],
                entry["version"],
            )
        )
    return PackageTarget(
        value["packageKey"],
        value["rootExtension"],
        value["distributionName"],
        value["importPackage"],
        value["version"],
        value["sourceDirectory"],
        value["minimumPythonVersion"],
        value["publicationMode"],
        value["repositoryUrl"],
        registry,
        tuple(dependencies),
        emitter_digest,
    )
