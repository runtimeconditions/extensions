"""Validated, fully named Python emission plan."""

from __future__ import annotations

from dataclasses import dataclass
from hashlib import sha256
import json
from typing import Any

from .naming import Symbol, allocate_symbols
from .package import PackageTarget, fail

JSON_VALUE_ALIAS_RHS = (
    '"str | bool | int | float | None | Sequence[JSONValue] | dict[str, JSONValue]"'
)


@dataclass(frozen=True)
class TypeExpr:
    kind: str
    arguments: tuple[TypeExpr, ...] = ()
    symbol_key: str = ""

    def annotation(self, names: dict[str, str]) -> str:
        if self.kind in {"string", "boolean", "integer", "number", "null"}:
            return {
                "string": "str",
                "boolean": "bool",
                "integer": "int",
                "number": "float",
                "null": "None",
            }[self.kind]
        if self.kind == "any":
            return "JSONValue"
        if self.kind in {"object", "enum", "ref", "alias"}:
            return names[self.symbol_key]
        if self.kind == "array":
            return f"Sequence[{self.arguments[0].annotation(names)}]"
        if self.kind == "map":
            return f"dict[str, {self.arguments[0].annotation(names)}]"
        if self.kind == "union":
            return " | ".join(argument.annotation(names) for argument in self.arguments)
        raise AssertionError(f"unplanned type kind {self.kind}")


@dataclass(frozen=True)
class FieldPlan:
    coordinate: str
    source_name: str
    name: str
    type_expr: TypeExpr
    annotation: str
    required: bool
    default_none: bool


@dataclass(frozen=True)
class TypePlan:
    coordinate: str
    name: str
    kind: str  # object, alias, enum
    fields: tuple[FieldPlan, ...] = ()
    expression: TypeExpr | None = None
    annotation: str = ""
    alias_rhs: str = ""
    members: tuple[tuple[str, str], ...] = ()
    marker: MarkerReference | None = None
    is_declaration_field: bool = False
    schema_coordinate: str = ""
    source_name: str = ""


@dataclass(frozen=True)
class MarkerReference:
    owner: str
    protocol: str
    method: str
    provider_import: str | None


@dataclass(frozen=True)
class DeclarationPlan:
    coordinate: str
    kind: str
    function: str
    protocol: str
    method: str


@dataclass(frozen=True)
class ImportedDeclarationPlan:
    coordinate: str
    owner: str
    kind: str
    function: str
    protocol: str
    method: str
    provider_import: str | None


@dataclass(frozen=True)
class EmissionPlan:
    target: PackageTarget
    names: dict[str, str]
    declarations: tuple[DeclarationPlan, ...]
    imported_declarations: tuple[ImportedDeclarationPlan, ...]
    types: tuple[TypePlan, ...]
    uses_json_value: bool
    model_digest: str
    json_value_alias_rhs: str = JSON_VALUE_ALIAS_RHS
    any_coordinates: tuple[tuple[str, str], ...] = ()


def _direct_dependencies(model: dict[str, Any]) -> set[str]:
    root = model["rootExtension"]["id"]
    for extension in model["extensions"]:
        if extension["id"] == root:
            return set(extension.get("dependencies", []))
    fail(
        "model",
        "RCP1011",
        root,
        "root extension is absent from resolved extension closure",
    )


def _marker_method(owner: str, coordinate: str) -> str:
    identity = owner.encode("utf-8") + b"\x00" + coordinate.encode("utf-8")
    return "rc_marker_" + sha256(identity).hexdigest()[:16]


def _provider_import(model: dict[str, Any], target: PackageTarget, owner: str) -> str:
    graph = {
        item["id"]: tuple(item.get("dependencies", [])) for item in model["extensions"]
    }

    def reaches(current: str, visited: set[str]) -> bool:
        if current == owner:
            return True
        if current in visited:
            return False
        visited.add(current)
        return any(
            reaches(dependency, visited) for dependency in graph.get(current, ())
        )

    for dependency in sorted(target.dependencies, key=lambda item: item.extension):
        if reaches(dependency.extension, set()):
            return dependency.import_package
    fail(
        "model",
        "RCP1011",
        owner,
        "imported declaration has no direct dependency provider",
    )


def build_plan(model: dict[str, Any], target: PackageTarget) -> EmissionPlan:
    """Validate model/target agreement and allocate every source symbol in memory."""
    root = model["rootExtension"]["id"]
    package = target.package_key
    if root != target.root_extension:
        fail(
            "package-config",
            "RCP1009",
            package,
            f"target root {target.root_extension!r} differs from model root {root!r}",
            "/rootExtension",
        )
    direct = _direct_dependencies(model)
    configured = {dependency.extension for dependency in target.dependencies}
    if direct != configured:
        fail(
            "package-config",
            "RCP1009",
            package,
            f"direct dependency set differs: model={sorted(direct)!r}, target={sorted(configured)!r}",
            "/dependencies",
        )
    extension_ids = {item["id"] for item in model["extensions"]}
    if not configured <= extension_ids:
        fail(
            "model",
            "RCP1011",
            root,
            "target dependency is absent from resolved closure",
        )

    vocabulary = model["vocabulary"]
    symbols: list[Symbol] = []
    declarations: list[tuple[str, str, str]] = []
    imported: list[ImportedDeclarationPlan] = []
    imported_kinds: dict[str, ImportedDeclarationPlan] = {}
    owned_kinds: dict[str, str] = {}
    for item in vocabulary.get("ownedDeclarations", []):
        coordinate = item["coordinate"]
        kind = item["kind"]
        if item["owner"] != root:
            fail(
                "model", "RCP1011", coordinate, "owned declaration has a foreign owner"
            )
        if kind in owned_kinds:
            fail("model", "RCP1011", coordinate, f"duplicate owned kind {kind!r}")
        owned_kinds[kind] = coordinate
        function_key = f"function:{coordinate}"
        protocol_key = f"protocol:{coordinate}"
        symbols.extend(
            (
                Symbol(function_key, coordinate, kind, "snake", fixed=True),
                Symbol(protocol_key, coordinate, kind + " Field", "pascal", fixed=True),
            )
        )
        declarations.append((coordinate, function_key, protocol_key))
    if declarations:
        symbols.append(
            Symbol(
                "builtin:Declaration",
                "python:Declaration",
                "Declaration",
                "pascal",
                fixed=True,
            )
        )
    for item in vocabulary.get("importedDeclarations", []):
        if item["owner"] == root or item["owner"] not in extension_ids:
            fail(
                "model",
                "RCP1011",
                item["coordinate"],
                "invalid imported declaration owner",
            )
        coordinate = item["coordinate"]
        function_key = f"import:function:{coordinate}"
        protocol_key = f"import:protocol:{coordinate}"
        symbols.extend(
            (
                Symbol(function_key, coordinate, item["kind"], "snake", fixed=True),
                Symbol(
                    protocol_key,
                    coordinate,
                    item["kind"] + " Field",
                    "pascal",
                    fixed=True,
                ),
            )
        )
        imported_plan = ImportedDeclarationPlan(
            coordinate,
            item["owner"],
            item["kind"],
            function_key,
            protocol_key,
            _marker_method(item["owner"], coordinate),
            _provider_import(model, target, item["owner"]),
        )
        imported.append(imported_plan)
        if item["kind"] in imported_kinds:
            fail("model", "RCP1011", item["coordinate"], "duplicate imported kind")
        imported_kinds[item["kind"]] = imported_plan

    raw_types: dict[
        str,
        tuple[
            str,
            Any,
            TypeExpr | None,
            list[tuple[str, str, str, TypeExpr, bool]],
            list[tuple[str, str]],
            MarkerReference | None,
        ],
    ] = {}
    # key -> (kind, source shape, expression, fields, members, marker key)
    type_sources: dict[str, str] = {}
    any_coordinates: set[tuple[str, str]] = set()
    declaration_field_schemas: dict[str, str] = {}
    any_used = False
    definition_keys: dict[tuple[str, str], str] = {}
    referenced_definitions: set[str] = set()
    pending_definitions: dict[str, tuple[dict[str, Any], tuple[str, ...], str]] = {}
    owned_field_roots = {
        (item["kind"], item.get("interfaceType", ""), item["segments"][0]["name"])
        for item in vocabulary.get("conditionFields", [])
        if item["owner"] == root
    }
    scope_projections = {
        (item["kind"], item.get("interfaceType", "")): item.get("projection", {})
        for item in model.get("scopes", [])
    }
    for schema in model.get("schemas", []):
        for definition in schema.get("definitions", []):
            definition_keys[(schema["coordinate"], definition["jsonPointer"])] = (
                f"type:{schema['coordinate']}:{definition['jsonPointer']}"
            )

    def register_shape(
        shape: dict[str, Any],
        key: str,
        source: str,
        parents: tuple[str, ...],
        schema_coordinate: str,
        marker: MarkerReference | None = None,
        exact_source: str | None = None,
    ) -> TypeExpr:
        nonlocal any_used
        coordinate = shape["provenance"]["coordinate"] + shape["provenance"].get(
            "jsonPointer", ""
        )
        kind = shape["kind"]
        source_name = exact_source if exact_source is not None else source
        if kind == "any":
            any_used = True
            any_coordinates.add((coordinate, source_name))
            return TypeExpr("any")
        if kind == "scalar":
            values = shape.get("values", [])
            if values and shape["scalar"] == "string":
                if any(not isinstance(entry["value"], str) for entry in values):
                    fail(
                        "model",
                        "RCP1012",
                        coordinate,
                        "string domain contains non-string value",
                    )
                symbols.append(
                    Symbol(key, coordinate, source, "pascal", parents=parents)
                )
                members: list[tuple[str, str]] = []
                for index, entry in enumerate(values):
                    member_key = f"member:{key}:{index}"
                    symbols.append(
                        Symbol(
                            member_key,
                            f"{coordinate}#value:{entry['value']}",
                            entry["value"],
                            "upper",
                            f"enum:{key}",
                        )
                    )
                    members.append((member_key, entry["value"]))
                raw_types[key] = ("enum", shape, None, [], members, marker)
                type_sources[key] = source_name
                return TypeExpr("enum", symbol_key=key)
            return TypeExpr(shape["scalar"])
        if kind == "ref":
            ref = shape["ref"]
            reference_key = definition_keys.get(
                (shape["provenance"]["coordinate"], ref.removeprefix("#"))
            )
            if reference_key is None:
                fail("model", "RCP1012", coordinate, f"unresolved reference {ref!r}")
            referenced_definitions.add(reference_key)
            return TypeExpr("ref", symbol_key=reference_key)
        if kind == "object":
            symbols.append(Symbol(key, coordinate, source, "pascal", parents=parents))
            if marker:
                symbols.append(
                    Symbol(
                        f"method:{key}",
                        marker.owner,
                        marker.method,
                        "snake",
                        f"fields:{key}",
                        fixed=True,
                    )
                )
            raw_fields: list[tuple[str, str, str, TypeExpr, bool]] = []
            required = set(shape.get("required", []))
            properties = shape.get("properties", [])
            property_names = {property_["name"] for property_ in properties}
            if not required <= property_names:
                fail(
                    "model",
                    "RCP1012",
                    coordinate,
                    f"required properties are missing: {sorted(required - property_names)!r}",
                )
            if len(property_names) != len(properties):
                fail("model", "RCP1012", coordinate, "duplicate object property")
            for property_ in properties:
                name = property_["name"]
                property_coordinate = property_["provenance"]["coordinate"] + property_[
                    "provenance"
                ].get("jsonPointer", "")
                field_key = f"field:{key}:{name}"
                symbols.append(
                    Symbol(
                        field_key, property_coordinate, name, "snake", f"fields:{key}"
                    )
                )
                child = property_["shape"]
                child_key = f"type:{key}:{name}"
                expression = register_shape(
                    child, child_key, name, (source,) + parents, schema_coordinate
                )
                raw_fields.append(
                    (
                        field_key,
                        property_coordinate,
                        name,
                        expression,
                        name in required or property_.get("required", False),
                    )
                )
            raw_types[key] = ("object", shape, None, raw_fields, [], marker)
            type_sources[key] = source_name
            return TypeExpr("object", symbol_key=key)
        if kind in {"array", "map", "union"}:
            if kind == "union":
                children = shape["variants"]
                if not children:
                    fail("model", "RCP1012", coordinate, "union has no variants")
            else:
                children = [shape["items"] if kind == "array" else shape["mapValues"]]
            expressions: list[TypeExpr] = []
            for index, child in enumerate(children):
                suffix = (
                    "Item"
                    if kind == "array"
                    else "Value"
                    if kind == "map"
                    else f"Variant{index + 1}"
                )
                child_source = (
                    (source.removesuffix(" Value") if kind == "array" else source)
                    + " "
                    + suffix
                )
                child_key = f"type:{key}:{suffix}"
                expressions.append(
                    register_shape(
                        child,
                        child_key,
                        child_source,
                        parents,
                        schema_coordinate,
                        exact_source=source_name,
                    )
                )
            expression = TypeExpr(kind, tuple(expressions))
            symbols.append(Symbol(key, coordinate, source, "pascal", parents=parents))
            raw_types[key] = ("alias", shape, expression, [], [], marker)
            type_sources[key] = source_name
            return TypeExpr("alias", symbol_key=key)
        fail("model", "RCP1010", coordinate, f"unknown structural node {kind!r}")

    for schema in model.get("schemas", []):
        if schema["owner"] != root:
            continue
        schema_coordinate = schema["coordinate"]
        kind = schema.get("kind", schema["id"])
        interface = schema.get("interfaceType")
        scope = tuple(part for part in (interface, kind) if part)
        if kind in owned_kinds:
            declaration_coordinate = owned_kinds[kind]
            marker = MarkerReference(
                root,
                f"protocol:{declaration_coordinate}",
                _marker_method(root, declaration_coordinate),
                None,
            )
        elif kind in imported_kinds:
            imported_marker = imported_kinds[kind]
            marker = MarkerReference(
                imported_marker.owner,
                imported_marker.protocol,
                imported_marker.method,
                imported_marker.provider_import,
            )
        else:
            marker = None
        projection = schema["projection"]
        if projection["kind"] != "object":
            fail(
                "model",
                "RCP1012",
                schema_coordinate,
                "root condition projection must be an object",
            )
        for property_ in projection.get("properties", []):
            property_name = property_["name"]
            field_source = (
                interface
                if property_name == "interface" and interface
                else property_name
            )
            field_parents = (
                (property_name,) + scope
                if property_name == "interface" and interface
                else scope
            )
            key = f"type:field:{schema_coordinate}:{property_name}"
            declaration_field_schemas[key] = schema_coordinate
            shape = property_["shape"]
            if (kind, interface or "", property_name) in owned_field_roots:
                scope_properties = scope_projections.get(
                    (kind, interface or ""), {}
                ).get("properties", [])
                projected_field = next(
                    (
                        item
                        for item in scope_properties
                        if item["name"] == property_name
                    ),
                    None,
                )
                if projected_field is None:
                    fail(
                        "model",
                        "RCP1012",
                        schema_coordinate,
                        f"condition field {property_name!r} is absent from its scope projection",
                    )
                shape = projected_field["shape"]
            if shape["kind"] == "object":
                register_shape(
                    shape, key, field_source, field_parents, schema_coordinate, marker
                )
                continue
            if marker is None:
                fail(
                    "model",
                    "RCP1011",
                    schema_coordinate,
                    f"field {property_name!r} has no declaration contract",
                )
            coordinate = property_["provenance"]["coordinate"] + property_[
                "provenance"
            ].get("jsonPointer", "")
            symbols.append(
                Symbol(key, coordinate, field_source, "pascal", parents=field_parents)
            )
            symbols.append(
                Symbol(
                    f"method:{key}",
                    marker.owner,
                    marker.method,
                    "snake",
                    f"fields:{key}",
                    fixed=True,
                )
            )
            value_key = f"field:{key}:value"
            symbols.append(
                Symbol(
                    value_key, coordinate, "value", "snake", f"fields:{key}", fixed=True
                )
            )
            field_expression = register_shape(
                shape,
                f"type:{key}:value",
                field_source + " Value",
                field_parents,
                schema_coordinate,
            )
            raw_types[key] = (
                "object",
                shape,
                None,
                [(value_key, coordinate, "value", field_expression, True)],
                [],
                marker,
            )
            type_sources[key] = field_source
        # Non-recursive references are expanded into the projection by the
        # normalizer. Emit standalone definitions only for remaining references;
        # otherwise expanded fields and their definitions can request one name.
        for definition in schema.get("definitions", []):
            definition_key = definition_keys[
                (schema_coordinate, definition["jsonPointer"])
            ]
            pending_definitions[definition_key] = (definition, scope, schema_coordinate)
            # A schema without projected fields exposes its named definitions
            # as its native structural API, including schema-only dependencies.
            if not projection.get("properties"):
                referenced_definitions.add(definition_key)

    while required_definitions := sorted(
        referenced_definitions.intersection(pending_definitions)
    ):
        for definition_key in required_definitions:
            definition, scope, schema_coordinate = pending_definitions.pop(
                definition_key
            )
            register_shape(
                definition["shape"],
                definition_key,
                definition["name"],
                scope,
                schema_coordinate,
            )

    names = allocate_symbols(package, symbols)
    types: list[TypePlan] = []
    for key in sorted(raw_types):
        kind, shape, expression, raw_fields, raw_members, marker = raw_types[key]
        fields: list[FieldPlan] = []
        for field_key, coordinate, source_name, field_type, required in raw_fields:
            annotation = field_type.annotation(names)
            if not required and "None" not in annotation.split(" | "):
                annotation += " | None"
            fields.append(
                FieldPlan(
                    coordinate,
                    source_name,
                    names[field_key],
                    field_type,
                    annotation,
                    required,
                    not required,
                )
            )
        fields.sort(key=lambda item: (not item.required, item.name, item.coordinate))
        annotation = expression.annotation(names) if expression else ""
        coordinate = shape["provenance"]["coordinate"] + shape["provenance"].get(
            "jsonPointer", ""
        )
        types.append(
            TypePlan(
                coordinate,
                names[key],
                kind,
                tuple(fields),
                expression,
                annotation,
                json.dumps(annotation, ensure_ascii=False) if kind == "alias" else "",
                tuple((names[member_key], value) for member_key, value in raw_members),
                (
                    MarkerReference(
                        marker.owner,
                        names[marker.protocol]
                        if marker.owner == root
                        else marker.protocol,
                        marker.method,
                        marker.provider_import,
                    )
                    if marker
                    else None
                ),
                marker is not None and kind == "object",
                declaration_field_schemas.get(key, ""),
                type_sources[key],
            )
        )
    result_declarations = tuple(
        DeclarationPlan(
            coordinate,
            next(
                item["kind"]
                for item in vocabulary.get("ownedDeclarations", [])
                if item["coordinate"] == coordinate
            ),
            names[function_key],
            names[protocol_key],
            _marker_method(root, coordinate),
        )
        for coordinate, function_key, protocol_key in sorted(declarations)
    )
    result_imported = tuple(
        ImportedDeclarationPlan(
            item.coordinate,
            item.owner,
            item.kind,
            names[item.function],
            names[item.protocol],
            item.method,
            item.provider_import,
        )
        for item in sorted(imported, key=lambda entry: entry.coordinate)
    )
    return EmissionPlan(
        target,
        names,
        result_declarations,
        result_imported,
        tuple(types),
        any_used,
        model["metadata"]["semanticSha256"],
        any_coordinates=tuple(sorted(any_coordinates)),
    )
