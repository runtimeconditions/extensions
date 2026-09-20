# Extension binding tooling

This directory contains the implementation governed by `IMPLEMENTATION.md`.
Phase 1 provides the shared Go resolver, validator, structural normalizer,
binding-model schema, CLI, and language-neutral conformance suite. Language
emitters, package assembly, promotion, and registry publication are not part of
Phase 1.

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
