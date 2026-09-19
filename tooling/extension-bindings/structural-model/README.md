# Structural extension binding model

This directory records the Go and Python binding shapes produced by a direct,
deterministic projection of Runtime Conditions extension vocabulary and JSON
Schema. It is an executable design fixture, not a published binding package.

The fixtures cover:

- `catalog/rc/common-integrations/common-integrations-v1alpha1.yaml`
- `catalog/rc/env-configuration/env-configuration-v1alpha1.yaml`

They intentionally do not reuse the existing handwritten declaration APIs.

## Projection rules

1. An owned kind becomes a declaration function.
2. Root Condition fields are flat declaration arguments. Interface values and
   additive fields such as `configuration` are siblings.
3. An interface type becomes an object type named directly from
   `interfaceTypes[].name`. Its `interface.type` value is fixed metadata and is
   not a caller-supplied field.
4. An object property becomes a field with the same serialized name.
5. An object schema becomes a Go struct or frozen, keyword-only Python
   dataclass.
6. An array property becomes a Go slice or Python sequence. If its item is an
   object, the item type is named `<Property>Item`; no singularization is used.
7. A scalar enum becomes a native named enum.
8. A `const` is fixed metadata and is not a caller-supplied field.
9. A payload-free schema variant becomes a typed constant. Object and array
   variants use native maps and collections without wrapper constructors.
10. An object with `oneOf` required-field branches remains one object type.
    The `oneOf` remains a validation constraint; it does not create interpreted
    public names.
11. A `$defs` entry becomes a named type using the definition key.
12. Optional scalars use the native representation needed to distinguish an
    omitted value from a valid zero value.
13. Additive extension types implement the declaration-field contracts for
    every kind listed by their resolved scopes. Their serialized root path is
    the owned field name and does not depend on source nesting.
14. Public names are derived from vocabulary values and canonical property
    paths. Collisions are resolved by prepending parent path components.

The generated source is inert. JSON Schema constraints, including `oneOf`,
patterns, minimum lengths, and collection cardinality, remain release-time and
profile-generation validation inputs.

## Native construction

Go uses struct and collection literals:

```go
envconfiguration.Configuration{
	Env: []envconfiguration.Env{
		{Property: envconfiguration.BaseURL, Name: "TODOS_API_URL"},
	},
}
```

Python uses keyword-only dataclass construction:

```python
env_configuration.Configuration(
    env=(
        env_configuration.Env(
            property=env_configuration.Property.BASE_URL,
            name="TODOS_API_URL",
        ),
    ),
)
```

## Verification

```sh
cd tooling/extension-bindings/structural-model/go
go test ./...

cd ../python
python -m unittest discover -s tests -v
```
