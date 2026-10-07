"""Step 2 loading, naming, and in-memory type-plan tests."""

from __future__ import annotations

import ast
import json
import subprocess
import sys
import tomllib
from copy import deepcopy
from dataclasses import FrozenInstanceError, replace
from hashlib import sha256
from importlib import import_module, resources
from pathlib import Path

import pytest
import yaml
from jsonschema import Draft202012Validator, ValidationError
from packaging.requirements import Requirement
from packaging.version import Version

from runtimeconditions_binding_emitter import (
    build_plan,
    emit_package,
    emit_sources,
    load_model,
    load_target,
    render_package,
    render_resources,
    render_sources,
)
from runtimeconditions_binding_emitter.source import _conformance_source
from runtimeconditions_binding_emitter.metadata import (
    EMITTER_NAME,
    EMITTER_VERSION,
    SETUPTOOLS_VERSION,
)
from runtimeconditions_binding_emitter.naming import (
    Symbol,
    allocate_symbols,
    python_name,
)
from runtimeconditions_binding_emitter.package import DiagnosticError, PackageDependency

TOOLING = Path(__file__).resolve().parents[3]
MODELS = TOOLING / "model/conformance/expected"
TARGET = TOOLING / "emitters/python/testdata/package-target.yaml"


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


def _plan(case: str):
    model = load_model(MODELS / case / "runtimeconditions.binding-model.yaml")
    target = _development_target()
    root = model["rootExtension"]["id"]
    dependencies = next(
        item.get("dependencies", [])
        for item in model["extensions"]
        if item["id"] == root
    )
    resolved = tuple(
        PackageDependency(item, f"test-{index}", f"test_{index}", "1.0.0")
        for index, item in enumerate(dependencies)
    )
    return build_plan(
        model, replace(target, root_extension=root, dependencies=resolved)
    )


def _model_ref(provenance: dict) -> dict:
    return {
        key: value
        for key, value in provenance.items()
        if key in {"coordinate", "jsonPointer"}
    }


@pytest.mark.parametrize(
    "case",
    sorted(
        path.parent.name
        for path in MODELS.glob("*/runtimeconditions.binding-model.yaml")
    ),
)
def test_positive_models_have_allocated_types(case: str) -> None:
    plan = _plan(case)
    assert plan.types
    assert len({item.name for item in plan.types}) == len(plan.types)
    assert all("Any" not in item.annotation for item in plan.types)
    assert all(
        "Any" not in field.annotation for item in plan.types for field in item.fields
    )


@pytest.mark.parametrize(
    ("case", "interface_type", "other_pointer"),
    [
        ("06-recursive-reference", "node", "/$defs/node"),
        ("08-heterogeneous-union", "target", "/properties/target"),
    ],
)
def test_interface_collision_uses_canonical_parent(
    case: str, interface_type: str, other_pointer: str
) -> None:
    plan = _plan(case)
    interface = next(
        item
        for item in plan.types
        if item.is_declaration_field
        and item.coordinate == item.schema_coordinate + "/properties/interface"
    )
    other = next(
        item
        for item in plan.types
        if item.coordinate == item.schema_coordinate + other_pointer
        and (other_pointer.startswith("/$defs/") or item.is_declaration_field)
    )
    assert interface.source_name == interface_type
    assert interface.name == "Interface" + interface_type.title()
    assert interface.name != other.name


@pytest.mark.parametrize(
    "case",
    sorted(
        path.parent.name
        for path in MODELS.glob("*/runtimeconditions.binding-model.yaml")
    ),
)
def test_root_bindings_match_exact_model_locations(case: str) -> None:
    model = load_model(MODELS / case / "runtimeconditions.binding-model.yaml")
    plan = _plan(case)
    manifest_path = f"src/{plan.target.import_package}/runtimeconditions.bindings.yaml"
    manifest = yaml.safe_load(render_resources(plan, model)[manifest_path])
    native_types = {item["nativeName"]: item for item in manifest["types"]}
    properties = {
        (schema["coordinate"], property_["name"]): property_
        for schema in model["schemas"]
        for property_ in schema["projection"].get("properties", [])
        if schema["owner"] == model["rootExtension"]["id"]
    }
    for binding in manifest["rootBindings"]:
        property_ = properties[
            (binding["schemaCoordinate"], binding["path"][0]["name"])
        ]
        native = native_types[binding["value"]["type"]]
        assert binding["modelRef"] == _model_ref(property_["provenance"])
        assert native["modelRef"] == _model_ref(property_["shape"]["provenance"])
        assert native["sourceName"] == binding["sourceName"]


def test_distinct_root_properties_can_share_a_shape_location() -> None:
    model = deepcopy(
        load_model(
            MODELS / "06-recursive-reference/runtimeconditions.binding-model.yaml"
        )
    )
    schema = model["schemas"][0]
    original = next(
        item for item in schema["projection"]["properties"] if item["name"] == "root"
    )
    duplicate = deepcopy(original)
    duplicate["name"] = "branch"
    duplicate["provenance"]["jsonPointer"] = "/properties/branch"
    schema["projection"]["properties"].append(duplicate)
    target = replace(_development_target(), root_extension=model["rootExtension"]["id"])
    plan = build_plan(model, target)
    manifest_path = f"src/{plan.target.import_package}/runtimeconditions.bindings.yaml"
    manifest = yaml.safe_load(render_resources(plan, model)[manifest_path])
    bindings = {item["path"][0]["name"]: item for item in manifest["rootBindings"]}
    native_types = {item["nativeName"]: item for item in manifest["types"]}
    assert bindings["root"]["value"] != bindings["branch"]["value"]
    for name in ("root", "branch"):
        binding = bindings[name]
        native = native_types[binding["value"]["type"]]
        assert binding["modelRef"]["jsonPointer"] == f"/properties/{name}"
        assert native["modelRef"] == _model_ref(original["shape"]["provenance"])
        assert native["sourceName"] == name


def test_expanded_definition_does_not_duplicate_field_type() -> None:
    model = load_model(
        MODELS / "06-recursive-reference/runtimeconditions.binding-model.yaml"
    )
    schema = model["schemas"][0]
    field = next(
        item for item in schema["projection"]["properties"] if item["name"] == "root"
    )
    schema["definitions"].append(
        {
            "name": "root",
            "jsonPointer": "/$defs/root",
            "shape": deepcopy(field["shape"]),
            "provenance": deepcopy(field["shape"]["provenance"]),
        }
    )
    target = replace(_development_target(), root_extension=model["rootExtension"]["id"])
    plan = build_plan(model, target)
    assert f"type:{schema['coordinate']}:/$defs/root" not in plan.names
    assert f"type:{schema['coordinate']}:/$defs/node" in plan.names
    prefix = f"src/{target.import_package}"
    assert render_sources(plan, model)[f"{prefix}/bindings.py"]
    assert render_resources(plan, model)[f"{prefix}/runtimeconditions.bindings.yaml"]


def test_condition_field_uses_combined_scope_projection() -> None:
    model = load_model(
        MODELS / "02-additive-field/runtimeconditions.binding-model.yaml"
    )
    schema = next(
        item for item in model["schemas"] if item["id"] == "additive-credential"
    )
    shared = deepcopy(schema)
    shared["id"] = "shared-shape"
    shared["coordinate"] = schema["owner"] + "#schema:shared-shape"
    shared.pop("kind")
    shared.pop("interfaceType")
    field = shared["projection"]["properties"][0]
    label = field["shape"]["properties"][0]
    label["name"] = "label"
    field["shape"]["required"] = ["label"]
    for item in (label, label["shape"]):
        item["provenance"]["coordinate"] = shared["coordinate"]
        item["provenance"]["jsonPointer"] = "/properties/credential/properties/label"
    for item in (shared, shared["projection"], field, field["shape"]):
        item["provenance"]["coordinate"] = shared["coordinate"]
    shared["exact"] = {
        "type": "object",
        "properties": {
            "credential": {
                "type": "object",
                "required": ["label"],
                "properties": {"label": {"type": "string"}},
            }
        },
    }
    model["schemas"].append(shared)
    scope = model["scopes"][0]
    scope["applicableSchemas"].append(shared["coordinate"])
    projected = next(
        item
        for item in scope["projection"]["properties"]
        if item["name"] == "credential"
    )["shape"]
    projected["properties"].append(deepcopy(label))
    projected["required"].append("label")

    plan = build_plan(model, _plan("02-additive-field").target)
    credential = next(
        item for item in plan.types if item.schema_coordinate == schema["coordinate"]
    )
    assert {item.name: item.required for item in credential.fields} == {
        "label": True,
        "token": True,
    }
    path = f"src/{plan.target.import_package}/runtimeconditions.bindings.yaml"
    manifest = yaml.safe_load(render_resources(plan, model)[path])
    native = next(
        item for item in manifest["types"] if item["nativeName"] == credential.name
    )
    mapping = next(item for item in native["fields"] if item["sourceName"] == "label")
    assert mapping["modelRef"] == {
        "coordinate": shared["coordinate"],
        "jsonPointer": "/properties/credential/properties/label",
    }


def test_collections_recursive_references_and_domains() -> None:
    collections = _plan("09-collections-and-maps")
    annotations = {
        item.annotation for item in collections.types if item.kind == "alias"
    }
    assert any(value.startswith("Sequence[") for value in annotations)
    assert any(value.startswith("dict[str, ") for value in annotations)
    recursive = _plan("06-recursive-reference")
    assert any(
        "Node" in item.annotation for item in recursive.types if item.kind == "alias"
    )
    domains = _plan("10-scoped-domains-collisions")
    enums = [item for item in domains.types if item.kind == "enum"]
    assert len(enums) == 2
    assert {value for item in enums for _, value in item.members} == {
        "direct",
        "proxy",
        "streaming",
    }
    assert all(
        item.alias_rhs == json.dumps(item.annotation, ensure_ascii=False)
        for item in recursive.types
        if item.kind == "alias"
    )


def test_additive_types_reference_the_owning_protocol() -> None:
    direct = _plan("02-additive-field")
    assert len(direct.imported_declarations) == 1
    assert all(
        item.marker and item.marker.provider_import == "test_0"
        for item in direct.types
        if item.marker
    )
    assert not any("base-service-http" in item.coordinate for item in direct.types)
    transitive = _plan("03-transitive-closure")
    assert transitive.imported_declarations[0].provider_import == "test_0"
    assert any(
        item.marker and item.marker.owner == transitive.imported_declarations[0].owner
        for item in transitive.types
    )


def test_same_kind_from_different_owners_has_distinct_marker() -> None:
    first = _plan("01-owned-kind-interface")
    second = _plan("10-scoped-domains-collisions")
    assert first.declarations[0].kind == second.declarations[0].kind == "service"
    assert first.declarations[0].method != second.declarations[0].method


def test_any_uses_recursive_json_value_alias() -> None:
    model = deepcopy(
        load_model(
            MODELS / "01-owned-kind-interface/runtimeconditions.binding-model.yaml"
        )
    )
    shape = model["schemas"][0]["projection"]["properties"][1]["shape"]
    shape.pop("scalar")
    shape["kind"] = "any"
    model["scopes"][0]["projection"]["properties"][1]["shape"] = deepcopy(shape)
    plan = build_plan(model, _development_target())
    assert plan.uses_json_value
    assert "Sequence[JSONValue]" in plan.json_value_alias_rhs
    assert any(
        field.annotation == "JSONValue" for item in plan.types for field in item.fields
    )
    sources = render_sources(plan, model)
    bindings_ast = ast.parse(sources[f"src/{plan.target.import_package}/bindings.py"])
    json_alias = next(
        node
        for node in bindings_ast.body
        if isinstance(node, ast.AnnAssign)
        and isinstance(node.target, ast.Name)
        and node.target.id == "JSONValue"
    )
    assert ast.literal_eval(json_alias.value) == ast.literal_eval(
        plan.json_value_alias_rhs
    )
    manifest_path = f"src/{plan.target.import_package}/runtimeconditions.bindings.yaml"
    manifest = yaml.safe_load(render_resources(plan, model)[manifest_path])
    assert any(
        field["value"] == {"builtin": "JSONValue"}
        for item in manifest["types"]
        for field in item.get("fields", [])
    )


def test_required_nullable_value_has_no_initializer_default() -> None:
    model = deepcopy(
        load_model(
            MODELS / "01-owned-kind-interface/runtimeconditions.binding-model.yaml"
        )
    )
    shape = model["schemas"][0]["projection"]["properties"][1]["shape"]
    original = deepcopy(shape)
    nullable = deepcopy(shape)
    nullable["scalar"] = "null"
    shape.clear()
    shape.update(
        {
            "kind": "union",
            "variants": [original, nullable],
            "provenance": original["provenance"],
        }
    )
    model["scopes"][0]["projection"]["properties"][1]["shape"] = deepcopy(shape)
    plan = build_plan(model, _development_target())
    region = next(item for item in plan.types if item.name == "Region")
    assert [
        (field.name, field.required, field.default_none) for field in region.fields
    ] == [("value", True, False)]
    source = render_sources(plan, model)[
        f"src/{plan.target.import_package}/bindings.py"
    ]
    assert "value: RegionValue\n" in source
    assert 'RegionValue: TypeAlias = "str | None"' in source


def test_renderer_rejects_a_different_model() -> None:
    model = load_model(
        MODELS / "01-owned-kind-interface/runtimeconditions.binding-model.yaml"
    )
    plan = build_plan(model, _development_target())
    changed = deepcopy(model)
    changed["metadata"]["semanticSha256"] = "0" * 64
    with pytest.raises(DiagnosticError) as error:
        render_sources(plan, changed)
    assert str(error.value) == (
        "RCP1003 model: emission plan does not match supplied model"
    )


def test_source_emission_writes_only_the_two_api_files(tmp_path: Path) -> None:
    model = load_model(
        MODELS / "01-owned-kind-interface/runtimeconditions.binding-model.yaml"
    )
    plan = build_plan(model, _development_target())
    output = tmp_path / "binding-source"
    expected = render_sources(plan, model)
    assert emit_sources(plan, model, output) == tuple(sorted(expected))
    assert {
        path.relative_to(output).as_posix()
        for path in output.rglob("*")
        if path.is_file()
    } == set(expected)
    assert {
        path.relative_to(output).as_posix(): path.read_text(encoding="utf-8")
        for path in output.rglob("*")
        if path.is_file()
    } == expected
    with pytest.raises(DiagnosticError) as error:
        emit_sources(plan, model, output)
    assert str(error.value) == (
        "RCP3001 conformance-owned-kind-interface: "
        "output directory must be new or empty and not a symbolic link"
    )


def test_requiredness_is_separate_from_nullability() -> None:
    plan = _plan("01-owned-kind-interface")
    interface = next(item for item in plan.types if item.name == "Http")
    region = next(item for item in plan.types if item.name == "Region")
    assert [
        (field.name, field.annotation, field.required, field.default_none)
        for field in interface.fields
    ] == [("endpoint", "str", True, False)]
    assert [
        (field.name, field.annotation, field.required, field.default_none)
        for field in region.fields
    ] == [("value", "str", True, False)]
    assert all(item.is_declaration_field for item in (interface, region))
    optional = _plan("07-object-alternatives")
    assert any(
        field.default_none and field.annotation.endswith(" | None")
        for item in optional.types
        for field in item.fields
    )


def test_target_model_mismatch_has_exact_diagnostic() -> None:
    model = load_model(
        MODELS / "01-owned-kind-interface/runtimeconditions.binding-model.yaml"
    )
    target = replace(_development_target(), root_extension="other")
    with pytest.raises(DiagnosticError) as error:
        build_plan(model, target)
    assert str(error.value) == (
        "RCP1009 conformance-owned-kind-interface /rootExtension: target root 'other' differs "
        "from model root 'https://runtimeconditions.io/conformance/owned-kind-interface:1.0.0'"
    )


def test_duplicate_yaml_key_is_rejected(tmp_path: Path) -> None:
    path = tmp_path / "target.yaml"
    path.write_text("apiVersion: a\napiVersion: b\n", encoding="utf-8")
    with pytest.raises(DiagnosticError) as error:
        load_target(path)
    assert (
        str(error.value)
        == "RCP1002 package-target: duplicate YAML mapping key 'apiVersion'"
    )


def test_unsupported_shape_has_exact_diagnostic(tmp_path: Path) -> None:
    source = (
        MODELS / "01-owned-kind-interface/runtimeconditions.binding-model.yaml"
    ).read_text()
    path = tmp_path / "model.yaml"
    path.write_text(source.replace("kind: scalar", "kind: mystery", 1))
    with pytest.raises(DiagnosticError) as error:
        load_model(path)
    assert error.value.diagnostic.code == "RCP1010"
    assert "unknown structural node 'mystery'" in str(error.value)


def test_invalid_scalar_shape_has_a_diagnostic(tmp_path: Path) -> None:
    source = (
        MODELS / "01-owned-kind-interface/runtimeconditions.binding-model.yaml"
    ).read_text()
    path = tmp_path / "model.yaml"
    path.write_text(source.replace("scalar: string", "scalar: [string]", 1))
    with pytest.raises(DiagnosticError) as error:
        load_model(path)
    assert error.value.diagnostic.code == "RCP1003"
    assert error.value.diagnostic.json_pointer.endswith("/scalar")


def test_python_name_boundaries_and_fixed_collision() -> None:
    assert python_name("HTTPServer2URL", "pascal", "test") == "HttpServer2Url"
    assert python_name("2-way", "snake", "test") == "x_2_way"
    assert python_name("2-way", "pascal", "test") == "X2Way"
    assert python_name("class", "snake", "test") == "class_"
    assert python_name("__init__", "snake", "test") == "rc___init__"
    symbols = [
        Symbol("fixed", "kind:service", "ServiceField", "pascal", fixed=True),
        Symbol("other", "field:service", "ServiceField", "pascal", parents=("scope",)),
    ]
    with pytest.raises(DiagnosticError) as error:
        allocate_symbols("package", symbols)
    assert str(error.value) == (
        "RCP2001 package: fixed symbol 'ServiceField' conflicts: field:service and kind:service"
    )


def test_nonfixed_collision_uses_parent_path() -> None:
    symbols = [
        Symbol("left", "scope:left:a", "a", "pascal", parents=("left",)),
        Symbol("right", "scope:right:a", "a", "pascal", parents=("right",)),
    ]
    assert allocate_symbols("package", symbols) == {"left": "LeftA", "right": "RightA"}


def _write_sources(root: Path, sources: dict[str, str]) -> None:
    for relative_path, source in sources.items():
        destination = root / relative_path
        destination.parent.mkdir(parents=True, exist_ok=True)
        destination.write_text(source, encoding="utf-8")


@pytest.mark.parametrize(
    "case",
    [
        "01-owned-kind-interface",
        "06-recursive-reference",
        "07-object-alternatives",
        "08-heterogeneous-union",
        "09-collections-and-maps",
        "10-scoped-domains-collisions",
        "11-source-name-preservation",
    ],
)
def test_rendered_api_imports_and_conformance_runs(
    case: str, tmp_path: Path, monkeypatch
) -> None:
    model = load_model(MODELS / case / "runtimeconditions.binding-model.yaml")
    plan = _plan(case)
    package_name = "generated_" + case.replace("-", "_")
    plan = replace(plan, target=replace(plan.target, import_package=package_name))
    sources = _synthetic_sources(plan, model)
    assert sources == _synthetic_sources(plan, model)
    assert sorted(path.rsplit("/", 1)[-1] for path in sources) == [
        "__init__.py",
        "_conformance.py",
        "bindings.py",
    ]
    for source in sources.values():
        ast.parse(source, feature_version=(3, 11))
        assert model["metadata"]["semanticSha256"] in source.splitlines()[0]
        assert "DO NOT EDIT" in source.splitlines()[0]
        assert "Any" not in source
    _write_sources(tmp_path, sources)
    monkeypatch.syspath_prepend(str(tmp_path / "src"))
    package = import_module(package_name)
    assert package.__all__ == sorted(package.__all__)
    assert set(package.__all__) == set(
        import_module(package_name + ".bindings").__all__
    )
    assert import_module(package_name + "._conformance").exercise() is None


def test_declaration_uses_flat_frozen_keyword_only_fields(
    tmp_path: Path, monkeypatch
) -> None:
    case = "01-owned-kind-interface"
    model = load_model(MODELS / case / "runtimeconditions.binding-model.yaml")
    plan = replace(
        _plan(case), target=replace(_plan(case).target, import_package="flat_binding")
    )
    _write_sources(tmp_path, render_sources(plan, model))
    monkeypatch.syspath_prepend(str(tmp_path / "src"))
    binding = import_module("flat_binding")
    interface = binding.Http(endpoint="https://example.test")
    region = binding.Region(value="west")
    assert isinstance(binding.service(interface, region), binding.Declaration)
    with pytest.raises(TypeError):
        binding.Http("https://example.test")
    with pytest.raises(FrozenInstanceError):
        region.value = "east"


def test_conformance_covers_object_alternative_fields_separately() -> None:
    case = "07-object-alternatives"
    model = load_model(MODELS / case / "runtimeconditions.binding-model.yaml")
    plan = _plan(case)
    source = _synthetic_sources(plan, model)[
        f"src/{plan.target.import_package}/_conformance.py"
    ]
    calls = [
        node
        for node in ast.walk(ast.parse(source))
        if isinstance(node, ast.Call)
        and isinstance(node.func, ast.Attribute)
        and node.func.attr == "Configuration"
    ]
    alternatives = {
        tuple(
            (keyword.arg, ast.literal_eval(keyword.value)) for keyword in call.keywords
        )
        for call in calls
    }
    assert alternatives == {
        (("command", "sample"), ("image", None)),
        (("command", None), ("image", "sample")),
    }
    assert source.count("b.deployment(_object") == 2


def test_conformance_keeps_scoped_declarations_separate() -> None:
    case = "10-scoped-domains-collisions"
    model = load_model(MODELS / case / "runtimeconditions.binding-model.yaml")
    plan = _plan(case)
    source = _synthetic_sources(plan, model)[
        f"src/{plan.target.import_package}/_conformance.py"
    ]
    calls = [line for line in source.splitlines() if "b.service(" in line]
    assert len(calls) == 2
    indexed = {
        item.name: f"_object_{index}"
        for index, item in enumerate(sorted(plan.types, key=lambda entry: entry.name))
    }
    grpc = indexed[
        next(item.name for item in plan.types if item.name.endswith("GrpcMode"))
    ]
    http = indexed[
        next(item.name for item in plan.types if item.name.endswith("HttpMode"))
    ]
    assert all(not (grpc in line and http in line) for line in calls)


def test_direct_additive_protocol_is_owned_by_dependency(
    tmp_path: Path, monkeypatch
) -> None:
    model = load_model(
        MODELS / "02-additive-field/runtimeconditions.binding-model.yaml"
    )
    base_id = model["vocabulary"]["importedDeclarations"][0]["owner"]
    base_model = deepcopy(model)
    base_extension = next(item for item in model["extensions"] if item["id"] == base_id)
    base_model["rootExtension"] = dict(base_extension)
    base_model["extensions"] = [base_extension]
    base_model["vocabulary"]["ownedDeclarations"] = base_model["vocabulary"][
        "importedDeclarations"
    ]
    base_model["vocabulary"]["importedDeclarations"] = []
    target = _development_target()
    base_target = replace(
        target, root_extension=base_id, import_package="owner_binding"
    )
    _write_sources(
        tmp_path, _synthetic_sources(build_plan(base_model, base_target), base_model)
    )
    addon_target = replace(
        target,
        root_extension=model["rootExtension"]["id"],
        import_package="direct_addon_binding",
        dependencies=(
            PackageDependency(base_id, "owner-binding", "owner_binding", "1.0.0"),
        ),
    )
    _write_sources(tmp_path, _synthetic_sources(build_plan(model, addon_target), model))
    monkeypatch.syspath_prepend(str(tmp_path / "src"))
    owner = import_module("owner_binding")
    addon = import_module("direct_addon_binding")
    assert addon.ServiceField is owner.ServiceField
    assert addon.service is owner.service
    marker = next(
        name for name in vars(owner.ServiceField) if name.startswith("rc_marker_")
    )
    assert hasattr(addon.Credential, marker)
    assert isinstance(
        owner.service(addon.Credential(token="sample")), owner.Declaration
    )
    assert import_module("direct_addon_binding._conformance").exercise() is None


def test_transitive_additive_protocol_reexports_through_direct_dependency(
    tmp_path: Path, monkeypatch
) -> None:
    model = load_model(
        MODELS / "03-transitive-closure/runtimeconditions.binding-model.yaml"
    )
    imported = model["vocabulary"]["importedDeclarations"][0]
    leaf_id = imported["owner"]
    root_id = model["rootExtension"]["id"]
    middle_extension = next(
        item for item in model["extensions"] if leaf_id in item.get("dependencies", [])
    )
    middle_id = middle_extension["id"]
    leaf_extension = next(item for item in model["extensions"] if item["id"] == leaf_id)
    target = _development_target()

    leaf_model = deepcopy(model)
    leaf_model["rootExtension"] = dict(leaf_extension)
    leaf_model["extensions"] = [leaf_extension]
    leaf_model["vocabulary"]["ownedDeclarations"] = leaf_model["vocabulary"][
        "importedDeclarations"
    ]
    leaf_model["vocabulary"]["importedDeclarations"] = []
    leaf_target = replace(target, root_extension=leaf_id, import_package="leaf_binding")
    _write_sources(
        tmp_path, _synthetic_sources(build_plan(leaf_model, leaf_target), leaf_model)
    )

    middle_model = deepcopy(model)
    middle_model["rootExtension"] = dict(middle_extension)
    middle_model["extensions"] = [leaf_extension, middle_extension]
    middle_target = replace(
        target,
        root_extension=middle_id,
        import_package="middle_binding",
        dependencies=(
            PackageDependency(leaf_id, "leaf-binding", "leaf_binding", "1.0.0"),
        ),
    )
    _write_sources(
        tmp_path,
        _synthetic_sources(build_plan(middle_model, middle_target), middle_model),
    )

    root_target = replace(
        target,
        root_extension=root_id,
        import_package="root_binding",
        dependencies=(
            PackageDependency(middle_id, "middle-binding", "middle_binding", "1.0.0"),
        ),
    )
    _write_sources(tmp_path, _synthetic_sources(build_plan(model, root_target), model))
    monkeypatch.syspath_prepend(str(tmp_path / "src"))
    leaf = import_module("leaf_binding")
    middle = import_module("middle_binding")
    root = import_module("root_binding")
    assert root.WorkerField is middle.WorkerField is leaf.WorkerField
    assert root.worker is leaf.worker
    marker = next(
        name for name in vars(leaf.WorkerField) if name.startswith("rc_marker_")
    )
    assert hasattr(root.Command, marker)
    assert isinstance(root.worker(root.Command(value="sample")), leaf.Declaration)
    assert import_module("root_binding._conformance").exercise() is None


@pytest.mark.parametrize(
    "case",
    sorted(
        path.parent.name
        for path in MODELS.glob("*/runtimeconditions.binding-model.yaml")
    ),
)
def test_package_metadata_and_manifest_are_deterministic(case: str) -> None:
    model = load_model(MODELS / case / "runtimeconditions.binding-model.yaml")
    plan = _plan(case)
    files = render_package(plan, model)
    assert files == render_package(plan, model)
    prefix = f"src/{plan.target.import_package}"
    assert set(files) == {
        "pyproject.toml",
        f"{prefix}/__init__.py",
        f"{prefix}/bindings.py",
        f"{prefix}/py.typed",
        f"{prefix}/runtimeconditions.bindings.yaml",
    }
    assert files[f"{prefix}/py.typed"] == ""
    metadata = tomllib.loads(files["pyproject.toml"])
    assert metadata["build-system"]["requires"] == [f"setuptools=={SETUPTOOLS_VERSION}"]
    assert metadata["project"]["name"] == plan.target.distribution_name
    assert metadata["project"]["requires-python"] == ">=3.11"
    assert (
        str(Version(metadata["project"]["version"])) == metadata["project"]["version"]
    )
    assert all(
        Requirement(dependency).name
        for dependency in metadata["project"]["dependencies"]
    )
    assert metadata["tool"]["setuptools"]["package-data"][
        plan.target.import_package
    ] == [
        "py.typed",
        "runtimeconditions.bindings.yaml",
        "runtimeconditions.binding-model.yaml",
        "runtimeconditions.extension.yaml",
        "runtimeconditions.binding-release.yaml",
    ]
    manifest_source = files[f"{prefix}/runtimeconditions.bindings.yaml"]
    assert manifest_source.startswith("# Code generated by ")
    manifest = yaml.safe_load(manifest_source)
    assert manifest["generated"] == {
        "nonEditable": True,
        "emitter": f"{EMITTER_NAME}@sha256:{plan.target.emitter_sha256}",
        "version": EMITTER_VERSION,
    }
    assert manifest["model"]["semanticSha256"] == model["metadata"]["semanticSha256"]
    assert manifest["extension"] == {
        "id": model["rootExtension"]["id"],
        "semanticSha256": model["rootExtension"]["semanticSha256"],
    }
    assert manifest["package"] == {
        "language": "python",
        "coordinate": plan.target.distribution_name,
        "name": plan.target.import_package,
        "version": "1.0.0",
        "minimumPythonVersion": "3.11",
    }
    assert manifest["apiVersion"] == "runtimeconditions.io/bindings/v1alpha2"
    assert "symbols" not in manifest
    types = manifest["types"]
    assert types == sorted(types, key=lambda item: item["nativeName"])
    assert {item["nativeName"] for item in types} == {item.name for item in plan.types}
    names = {item["nativeName"] for item in types}
    values = [item["value"] for item in manifest["rootBindings"]]
    for item in types:
        values.extend(field["value"] for field in item.get("fields", []))
        values.extend([item["element"]["value"]] if "element" in item else [])
        values.extend(variant["value"] for variant in item.get("variants", []))
    assert all(value["type"] in names for value in values if "type" in value)

    def coordinate(provenance: dict) -> tuple[str, str]:
        return provenance["coordinate"], provenance.get("jsonPointer", "")

    locations: set[tuple[str, str]] = set()
    shapes: dict[tuple[str, str], dict] = {}

    def walk(value: object) -> None:
        if isinstance(value, dict):
            if isinstance(value.get("provenance"), dict):
                location = coordinate(value["provenance"])
                locations.add(location)
                if value.get("kind") in {
                    "object",
                    "array",
                    "map",
                    "union",
                    "scalar",
                    "any",
                    "ref",
                }:
                    shapes[location] = value
            for child in value.values():
                walk(child)
        elif isinstance(value, list):
            for child in value:
                walk(child)

    walk(model)

    def check_ref(entry: dict) -> None:
        assert coordinate(entry["modelRef"]) in locations

    for section in ("declarations", "importedMarkerContracts", "rootBindings"):
        for entry in manifest[section]:
            check_ref(entry)
    for entry in types:
        check_ref(entry)
        shape = shapes[coordinate(entry["modelRef"])]
        for field in entry.get("fields", []):
            check_ref(field)
        if entry["construct"] == "object" and shape["kind"] == "object":
            assert {
                (coordinate(field["modelRef"]), field["sourceName"])
                for field in entry["fields"]
            } == {
                (coordinate(property_["provenance"]), property_["name"])
                for property_ in shape.get("properties", [])
            }
        if "element" in entry:
            check_ref(entry["element"])
            child = shape[
                "items" if entry["construct"] == "collection" else "mapValues"
            ]
            assert entry["element"]["modelRef"] == {
                "coordinate": child["provenance"]["coordinate"],
                **(
                    {"jsonPointer": child["provenance"]["jsonPointer"]}
                    if "jsonPointer" in child["provenance"]
                    else {}
                ),
            }
        if "variants" in entry:
            assert len(entry["variants"]) == len(shape["variants"])
            for variant, child in zip(
                entry["variants"], shape["variants"], strict=True
            ):
                check_ref(variant)
                assert coordinate(variant["modelRef"]) == coordinate(
                    child["provenance"]
                )
        for member in entry.get("members", []):
            check_ref(member)
    assert len(manifest["declarations"]) == len(plan.declarations)
    assert len(manifest["importedMarkerContracts"]) == len(plan.imported_declarations)


def test_manifest_covers_collection_map_enum_optional_and_imported_contracts() -> None:
    cases = (
        "09-collections-and-maps",
        "10-scoped-domains-collisions",
        "07-object-alternatives",
        "02-additive-field",
    )
    manifests = {}
    for case in cases:
        model = load_model(MODELS / case / "runtimeconditions.binding-model.yaml")
        plan = _plan(case)
        path = f"src/{plan.target.import_package}/runtimeconditions.bindings.yaml"
        manifests[case] = yaml.safe_load(render_resources(plan, model)[path])
    collections = manifests["09-collections-and-maps"]["types"]
    assert {"collection", "map"} <= {item["construct"] for item in collections}
    assert all(
        "element" in item
        for item in collections
        if item["construct"] in {"collection", "map"}
    )
    assert any(
        item["construct"] == "scalar" and item.get("members")
        for item in manifests["10-scoped-domains-collisions"]["types"]
    )
    assert any(
        field["value"].get("nullable") is True
        for item in manifests["07-object-alternatives"]["types"]
        for field in item.get("fields", [])
    )
    imported = manifests["02-additive-field"]["importedMarkerContracts"]
    assert imported and all("file" not in item for item in imported)
    assert all(item["providerPackage"] for item in imported)
    source_model = load_model(
        MODELS / "11-source-name-preservation/runtimeconditions.binding-model.yaml"
    )
    source_plan = _plan("11-source-name-preservation")
    source_path = (
        f"src/{source_plan.target.import_package}/runtimeconditions.bindings.yaml"
    )
    source_manifest = yaml.safe_load(
        render_resources(source_plan, source_model)[source_path]
    )
    assert any(
        item.get("sourceName") == "café" for item in source_manifest["rootBindings"]
    )


def test_package_versions_and_direct_dependency_intervals() -> None:
    model = load_model(
        MODELS / "02-additive-field/runtimeconditions.binding-model.yaml"
    )
    plan = _plan("02-additive-field")
    target = replace(
        plan.target,
        version="1.2.3-rc.4",
        dependencies=(replace(plan.target.dependencies[0], version="0.4.5-beta.2"),),
    )
    metadata = tomllib.loads(
        render_resources(replace(plan, target=target), model)["pyproject.toml"]
    )
    assert metadata["project"]["version"] == "1.2.3rc4"
    assert metadata["project"]["dependencies"] == ["test-0>=0.4.5b2,<0.5.0"]
    stable = replace(
        target, dependencies=(replace(target.dependencies[0], version="2.1.3"),)
    )
    metadata = tomllib.loads(
        render_resources(replace(plan, target=stable), model)["pyproject.toml"]
    )
    assert metadata["project"]["dependencies"] == ["test-0>=2.1.3,<3.0.0"]


def test_package_emission_writes_six_files_and_rejects_occupied_output(
    tmp_path: Path,
) -> None:
    model = load_model(
        MODELS / "01-owned-kind-interface/runtimeconditions.binding-model.yaml"
    )
    plan = _plan("01-owned-kind-interface")
    files = render_package(plan, model)
    output = tmp_path / "package"
    assert emit_package(plan, model, output) == tuple(sorted(files))
    assert {
        path.relative_to(output).as_posix(): path.read_text(encoding="utf-8")
        for path in output.rglob("*")
        if path.is_file()
    } == files
    with pytest.raises(DiagnosticError) as error:
        emit_package(plan, model, output)
    assert error.value.diagnostic.code == "RCP3001"


def test_manifest_is_at_fixed_import_package_resource_path(
    tmp_path: Path, monkeypatch
) -> None:
    model = load_model(
        MODELS / "01-owned-kind-interface/runtimeconditions.binding-model.yaml"
    )
    plan = _plan("01-owned-kind-interface")
    plan = replace(plan, target=replace(plan.target, import_package="resource_binding"))
    files = render_package(plan, model)
    _write_sources(tmp_path, files)
    monkeypatch.syspath_prepend(str(tmp_path / "src"))
    package = import_module("resource_binding")
    expected = files["src/resource_binding/runtimeconditions.bindings.yaml"]
    assert (
        resources.files(package)
        .joinpath("runtimeconditions.bindings.yaml")
        .read_text(encoding="utf-8")
        == expected
    )
    assert resources.files(package).joinpath("py.typed").read_bytes() == b""


def test_manifest_schema_accepts_python_and_rejects_wrong_language_fields() -> None:
    schema = yaml.safe_load(
        (TOOLING / "model/runtimeconditions.binding-manifest.schema.yaml").read_text(
            encoding="utf-8"
        )
    )
    model = load_model(
        MODELS / "01-owned-kind-interface/runtimeconditions.binding-model.yaml"
    )
    plan = _plan("01-owned-kind-interface")
    path = f"src/{plan.target.import_package}/runtimeconditions.bindings.yaml"
    manifest = yaml.safe_load(render_resources(plan, model)[path])
    Draft202012Validator(schema).validate(manifest)
    invalid = deepcopy(manifest)
    invalid["package"]["minimumGoVersion"] = "1.22"
    with pytest.raises(ValidationError):
        Draft202012Validator(schema).validate(invalid)


def test_manifest_rejects_stale_root_extension_digest() -> None:
    model = load_model(
        MODELS / "01-owned-kind-interface/runtimeconditions.binding-model.yaml"
    )
    plan = _plan("01-owned-kind-interface")
    model["rootExtension"]["semanticSha256"] = "0" * 64
    with pytest.raises(DiagnosticError) as error:
        render_resources(plan, model)
    assert error.value.diagnostic.code == "RCP1003"


def test_manifest_rejects_ambiguous_legacy_plan_coordinate() -> None:
    model = load_model(
        MODELS / "01-owned-kind-interface/runtimeconditions.binding-model.yaml"
    )
    plan = _plan("01-owned-kind-interface")
    properties = model["schemas"][0]["projection"]["properties"]
    first = properties[0]["shape"]["provenance"]
    second = properties[1]["shape"]["provenance"]
    first["coordinate"] = second["coordinate"] + second.get("jsonPointer", "")
    first.pop("jsonPointer", None)
    with pytest.raises(DiagnosticError) as error:
        render_resources(plan, model)
    assert error.value.diagnostic.code == "RCP1012"


def test_generated_backend_pin_matches_toolchain_lock() -> None:
    lock = yaml.safe_load((TOOLING / "toolchain.lock.yaml").read_text(encoding="utf-8"))
    assert lock["python"]["tools"]["setuptools"] == SETUPTOOLS_VERSION


@pytest.mark.parametrize("digest", ["", "a" * 63, "A" * 64, "g" * 64])
def test_emitter_digest_required_before_writing(digest: str, tmp_path: Path) -> None:
    model = load_model(
        MODELS / "01-owned-kind-interface/runtimeconditions.binding-model.yaml"
    )
    plan = _plan("01-owned-kind-interface")
    plan = replace(plan, target=replace(plan.target, emitter_sha256=digest))
    output = tmp_path / "package"
    with pytest.raises(DiagnosticError, match="RCP1006"):
        emit_package(plan, model, output)
    assert not output.exists()


def test_emitter_digest_changes_only_manifest() -> None:
    model = load_model(
        MODELS / "01-owned-kind-interface/runtimeconditions.binding-model.yaml"
    )
    plan = _plan("01-owned-kind-interface")
    before = render_package(plan, model)
    target = replace(
        plan.target,
        emitter_sha256=sha256(plan.target.emitter_sha256.encode("ascii")).hexdigest(),
    )
    after = render_package(replace(plan, target=target), model)
    assert {path for path in before if before[path] != after[path]} == {
        f"src/{plan.target.import_package}/runtimeconditions.bindings.yaml"
    }


@pytest.mark.parametrize("digest", ["a" * 64, "", "A" * 64, "g" * 64, 123])
def test_package_target_digest_loading(digest, tmp_path: Path) -> None:
    data = yaml.safe_load(TARGET.read_text(encoding="utf-8"))
    data["emitterSha256"] = digest
    path = tmp_path / "target.yaml"
    path.write_text(yaml.safe_dump(data), encoding="utf-8")
    if digest == "a" * 64:
        assert load_target(path).emitter_sha256 == digest
    else:
        with pytest.raises(DiagnosticError, match="RCP1006"):
            load_target(path)


@pytest.mark.parametrize(
    "identity", [EMITTER_NAME, f"{EMITTER_NAME}@sha256:" + "A" * 64]
)
def test_manifest_schema_requires_emitter_digest(identity: str) -> None:
    model = load_model(
        MODELS / "01-owned-kind-interface/runtimeconditions.binding-model.yaml"
    )
    plan = _plan("01-owned-kind-interface")
    path = f"src/{plan.target.import_package}/runtimeconditions.bindings.yaml"
    manifest = yaml.safe_load(render_resources(plan, model)[path])
    manifest["generated"]["emitter"] = identity
    schema = yaml.safe_load(
        (TOOLING / "model/runtimeconditions.binding-manifest.schema.yaml").read_text(
            encoding="utf-8"
        )
    )
    with pytest.raises(ValidationError):
        Draft202012Validator(schema).validate(manifest)


def _synthetic_sources(plan, model):
    """Create bounded synthetic API exercises only for tooling tests."""
    sources = render_sources(plan, model)
    path = f"src/{plan.target.import_package}/_conformance.py"
    result = subprocess.run(
        [sys.executable, "-m", "ruff", "format", "--stdin-filename", path, "-"],
        input=_conformance_source(plan, model),
        text=True,
        capture_output=True,
        check=True,
    )
    sources[path] = result.stdout
    return sources
