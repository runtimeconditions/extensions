# Extension binding tooling

This directory contains the implementation governed by `IMPLEMENTATION.md`.
Phase 1 provides the shared Go resolver, validator, structural normalizer,
binding-model schema, CLI, and language-neutral conformance suite. Phase 2 adds
the Go emitter, provisional binding manifest, native package metadata,
conformance source, and deterministic source-archive verification. Package
assembly, promotion, and registry publication remain later phases.

## Local verification

From `normalizer/`, run:

```text
go test -count=1 ./...
go vet ./...
```

The tests normalize every extension definition discovered under the repository
catalog, verify all resolver backends, compare the 12 committed conformance
cases with their exact expected models or diagnostics, and run the required
100-iteration determinism checks. The repository workflow runs the same suite
on Linux and macOS.

From `emitters/go/`, run:

```text
go test -count=1 ./...
go vet ./...
```

The Go suite emits every positive conformance model, validates the model and
binding manifest schemas, parses `go.mod`, runs `gofmt`, compiles and tests the
generated modules, runs `go vet`, checks conformance coverage, verifies
deterministic archives, and compares the exported source API with the API
derived from the normalized model through Go ASTs. It also verifies local
additive and transitive package composition through the generated exported
marker contracts.

## Model generation

`rc-binding-model` writes a semantic binding model and, when requested, a
separate dependency lock:

```text
go run ./cmd/rc-binding-model \
  --root <extension-id> \
  --extension-root <catalog-directory> \
  --semantic-schema ../model/runtimeconditions.extension-semantic.schema.yaml \
  --model-schema ../model/runtimeconditions.binding-model.schema.yaml \
  --core-profile-id <core-schema-id> \
  --core-profile-version <core-schema-version> \
  --core-profile-semantic-sha256 <64-lowercase-hex-digest> \
  --normalizer-sha256 <locked-normalizer-release-digest> \
  --output <new-model-path> \
  --dependency-lock-output <new-lock-path>
```

Output paths must not already exist. Network resolution is disabled unless
`--network` is supplied. HTTPS and OCI resolution require a dependency lock with
an exact source digest and source locator. HTTPS redirects are rejected. An OCI
input locator may use a mutable tag; after its locked extension content is
verified, the resolved dependency lock records the manifest's immutable
`sha256` locator. A previously written lock is supplied with
`--dependency-lock`.

The extension cache is content-addressed. Each extension entry must be named
`<lowercase-sha256>`, `<lowercase-sha256>.yaml`, or
`<lowercase-sha256>.yml`. The resolver hashes the exact bytes and rejects an
entry whose digest differs from its filename before the entry can be resolved.

The model intentionally excludes source-byte digests, source backends, and
source locators. Those values remain in the dependency lock and do not affect
model bytes or the model digest.

## Go package generation

`rc-go-bindings` consumes only a normalized model and one Go package target:

```text
go run ./cmd/rc-go-bindings \
  --model <runtimeconditions.binding-model.yaml> \
  --package-config <go-package-target.yaml> \
  --output <new-or-empty-directory>
```

Phase 2 keeps its conformance-only package targets under
`emitters/go/testdata/package-targets/`. They are temporary test configuration,
not the production package catalog; Phase 5 introduces the permanent catalog
and generated package locations. The generated `runtimeconditions.bindings.yaml`
uses the provisional Phase 2 schema in `model/` and remains an input to the
structural manifest work in Phase 4.
