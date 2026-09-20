# Extension Binding Generation Implementation Standard

## 1. Authority and purpose

This document is the implementation authority for generated Runtime Conditions
extension bindings. Implementations MUST conform to this document. A change to
the architecture, artifact contracts, derivation rules, validation behavior,
versioning policy, or promotion lifecycle MUST update this document before the
implementation change is merged.

The system converts immutable extension definitions into deterministic,
language-native declarative binding packages. It consists of one shared semantic
front end and one emitter per output language. The shared front end owns all
interpretation of extension vocabulary and JSON Schema. Emitters own only
language syntax, language naming, package layout, and language-native tooling.

The implementation MUST satisfy the following top-level invariants:

1. Implementation behavior, including branches, symbol rules, schema rules,
   fixtures, and workflows, MUST NOT be selected by a particular extension
   identifier, package name, extension-defined kind, interface type, field name,
   field value, or repository path.
2. Extension YAML is an immutable input. Binding generation MUST NOT modify an
   extension definition or require an extension definition to be changed to fit
   a generator limitation.
3. Unsupported input MUST stop generation with a deterministic diagnostic. It
   MUST NOT be widened to `any`, silently omitted, or replaced by a guessed API.
4. The same input closure, configuration, and toolchain versions MUST produce
   byte-identical persisted outputs on every supported host.
5. Persisted Runtime Conditions interchange artifacts, checkpoints, manifests,
   indexes, and evidence MUST be YAML. Language-native source and package-manager
   metadata retain their required native formats. JSON serialization is
   permitted only as an internal canonicalization step for equality and SHA-256
   digests; it MUST NOT become a parallel Runtime Conditions artifact contract.
6. Generated declarations enforce the structural type model but do not enforce
   every cross-field schema constraint. Every generated profile MUST undergo
   complete semantic validation before it is accepted or published.
7. Package generation, verification, GitHub promotion, and later registry
   publication MUST use the same locally executable commands.
8. Registry publication MUST promote previously verified GitHub Release bytes.
   It MUST NOT rebuild packages.
9. The normalized binding model MUST contain semantic identity only. Exact
   source-byte identity and resolution transport evidence MUST be confined to
   the dependency lock and release provenance and MUST NOT affect normalized
   model bytes or the model digest.

Normative requirements from the Runtime Conditions specification, especially
extension identity, dependency resolution, vocabulary ownership, conflict
validation, YAML serialization, and JSON Schema validation, remain authoritative
over this implementation document.

The resolver and normalizer MUST index declarations by the complete semantic
coordinate of owner identifier, kind, interface type, and property path. This is
data-driven scoping, not behavior dispatch: every coordinate MUST pass through
the same algorithms in Sections 6 through 10.

## 2. Repository ownership and tool versioning

All source for the shared model, normalizer, resolver, orchestrator, conformance
suite, and first-party emitters MUST live under:

```text
tooling/extension-bindings/
```

in this repository. The tooling MUST be versioned independently with Git tags of
the form:

```text
tooling/extension-bindings/v<major>.<minor>.<patch>
```

Each tooling release MUST publish checksummed command-line artifacts on GitHub.
Production binding promotion MUST use a released toolchain pinned by version and
SHA-256 in:

```text
tooling/extension-bindings/toolchain.lock.yaml
```

Development and pull-request tests of tooling changes MUST use workspace source.
A binding release MUST NOT use unreleased workspace tooling. A tooling change
therefore follows this order:

1. Pass the complete tooling conformance suite.
2. Publish a tooling release.
3. Update `toolchain.lock.yaml` to the released versions and digests.
4. Regenerate affected binding packages with the locked release.

Every normalized checkpoint MUST record the binding-model API version and
normalizer version and digest. Every generated binding manifest and release
manifest MUST additionally record the emitter, orchestrator, and applicable
profiler versions and digests.

## 3. Fixed system architecture

The pipeline is:

```text
root extension YAML
    + exact transitive extension dependency closure
    + core profile schema
    + package catalog
    + locked toolchain
        |
        v
shared Go resolver and validator
        |
        +--------------------------+
        |                          |
        v                          v
exact dependency lock       validated semantic closure
                                   |
                                   v
                          shared Go normalizer
                                   |
                                   v
runtimeconditions.binding-model.yaml
        |
        +--------------------------+
        |                          |
        v                          v
target-language emitter      target-language emitter
        |                          |
        v                          v
language source package      language source package
        |                          |
        +-------------+------------+
                      |
                      v
format, compile, profile generation, semantic validation, package inspection
                      |
                      v
checksummed release set -> target GitHub Releases -> external registries
```

The dependency lock is resolution and supply-chain evidence. The normalized
binding model is the semantic checkpoint consumed by emitters. The orchestrator
MUST verify that the lock and model describe the same identifiers, versions,
direct dependency edges, and semantic SHA-256 values before emission. Emitters
MUST NOT receive source-byte SHA-256 values, source backends, or source locators.

The shared resolver, validator, normalizer, and orchestrator MUST be written in
Go. Every language emitter MUST be written in the language it emits. The Go
emitter MUST be written in Go. The Python emitter MUST be written in Python.
Every future emitter MUST follow the same rule. Each static source profiler MUST
be written in the language it parses so that it uses that language's native
parser and package metadata APIs.

An emitter MUST consume only the normalized binding model and its target package
configuration. It MUST NOT read an extension YAML file, resolve dependencies,
reinterpret JSON Schema, add semantic distinctions, or remove semantic
distinctions.

## 4. Planned source and artifact layout

The implementation MUST use this layout:

```text
tooling/extension-bindings/
  IMPLEMENTATION.md
  README.md
  packages.yaml
  toolchain.lock.yaml

  model/
    runtimeconditions.extension-semantic.schema.yaml
    runtimeconditions.binding-model.schema.yaml
    runtimeconditions.binding-manifest.schema.yaml
    runtimeconditions.binding-release.schema.yaml
    runtimeconditions.package-catalog.schema.yaml
    runtimeconditions.toolchain-lock.schema.yaml
    runtimeconditions.public-api.schema.yaml
    runtimeconditions.file-manifest.schema.yaml
    runtimeconditions.build-plan.schema.yaml
    runtimeconditions.verification-summary.schema.yaml
    conformance/
      cases/
      expected/

  normalizer/
    go.mod
    go.sum
    model.go
    normalize.go
    resolve.go
    validate.go
    canonical.go
    normalize_test.go
    resolve_test.go
    conformance_test.go
    cmd/rc-binding-model/main.go

  orchestrator/
    go.mod
    go.sum
    catalog.go
    plan.go
    release.go
    verify.go
    plan_test.go
    release_test.go
    cmd/rc-extension-bindings/main.go

  emitters/
    go/
      go.mod
      go.sum
      emitter.go
      naming.go
      package.go
      emitter_test.go
      conformance_test.go
      cmd/rc-go-bindings/main.go

    python/
      pyproject.toml
      src/runtimeconditions_binding_emitter/__init__.py
      src/runtimeconditions_binding_emitter/emitter.py
      src/runtimeconditions_binding_emitter/naming.py
      src/runtimeconditions_binding_emitter/package.py
      src/runtimeconditions_binding_emitter/__main__.py
      tests/test_emitter.py
      tests/test_conformance.py

  profilers/
    go/
      go.mod
      go.sum
      profiler.go
      resolver.go
      profiler_test.go
      conformance_test.go
      cmd/rc-go-profiler/main.go

    python/
      pyproject.toml
      src/runtimeconditions_python_profiler/__init__.py
      src/runtimeconditions_python_profiler/profiler.py
      src/runtimeconditions_python_profiler/resolver.py
      src/runtimeconditions_python_profiler/__main__.py
      tests/test_profiler.py
      tests/test_conformance.py

bindings/
  <package-key>/
    go/
    python/

.github/workflows/
  _binding-build.yml
  binding-checks.yml
  binding-promote.yml
```

`bindings/<package-key>/<language>/` contains non-editable generated source.
`package-key` is declared in `packages.yaml`; it is not inferred from extension
semantics. Generated build archives MUST NOT be committed.

The external-registry workflow MUST NOT be added until external publication is
authorized. When authorized, its fixed path is:

```text
.github/workflows/binding-publish.yml
```

Implementation MUST be divided into reviewed phases. Before any phase that adds,
edits, or deletes more than five files, the exact file list, including tests,
fixtures, packaging files, generated files, and workflow files, MUST be approved.

## 5. Inputs

One package set represents one root extension. One build target is exactly one
language within one package set and consists of exactly these inputs:

1. One root `RuntimeConditionsExtensionDefinition`.
2. The complete exact transitive closure of `spec.dependencies`.
3. The exact core profile schema version selected by the package catalog.
4. One package-set entry and one nested language-target entry from the package
   catalog.
5. One locked normalizer release.
6. One locked emitter release for each requested language.
7. The previous `runtimeconditions.binding-release.yaml` when the target has a
   previous release.

`packages.yaml` owns publication data only. Each package-set entry MUST contain:

- a stable package key;
- the root extension identifier and repository-local source path;
- the core profile schema version;
- the package-license identifier;
- the repository URL;
- one `languages` mapping keyed by language identifier.

Every nested language-target entry MUST contain:

- the package-manager coordinate;
- the requested package version;
- the generated source output directory, which MUST equal
  `bindings/<package-key>/<language>`;
- the minimum supported language version;
- the publication mode, exactly `github-tag` for ecosystems distributed from
  repository tags or `registry` for pushed artifacts;
- the external registry identifier when publication mode is `registry`, even
  while external publication is disabled.

The pair `<package-key>, <language>` is the immutable build-target key. Package
keys and language keys MUST each be unique within their containing mapping.
Package keys MUST match `^[a-z0-9]+(?:-[a-z0-9]+)*$`; language keys MUST match
`^[a-z][a-z0-9-]*$`. Therefore commas and colons are unambiguous workflow-input
separators.

`packages.yaml` MUST NOT restate kinds, interface types, fields, field values,
schemas, dependencies, requiredness, or any other extension semantics.

## 6. Extension definition resolution

### 6.1 Resolver inputs and backends

Extension identifiers are exact, case-sensitive absolute URIs. The resolver MUST
support these backends:

1. Explicit identifier-to-file overrides supplied by the command line.
2. Repository-local catalog roots supplied by the command line.
3. Package-local vendored extension definitions discovered through a resolved
   binding package.
4. A content-addressed local extension cache.
5. `file:` URIs.
6. `https:` URIs.
7. `oci:` URIs.

Plain `http:` resolution MUST be rejected. An unknown URI scheme MUST be
rejected. Network resolution MUST be disabled by default and enabled only by the
explicit `--network` flag. CI promotion MUST use a committed dependency lock and
MUST fail if resolution requires content absent from the declared sources.

Repository-local discovery MUST parse candidate extension files and index them
by `metadata.id`. It MUST NOT infer an identifier from a file path. Finding two
different byte contents for one identifier MUST fail, even when both definitions
normalize to the same vocabulary.

### 6.2 Closure algorithm

The source-byte SHA-256 MUST be computed over the exact fetched bytes before
parsing. The semantic SHA-256 MUST be computed in memory as follows:

1. Parse and validate the extension against
   `model/runtimeconditions.extension-semantic.schema.yaml`.
2. Retain every validated data field and remove YAML presentation details only.
3. Sort mapping keys by UTF-8 byte sequence.
4. For every sequence, obey its mandatory `x-runtimeconditions-ordering`
   annotation in that schema: sort `set` sequences by each item's canonical
   bytes and preserve `source` sequences exactly.
5. Serialize the result with the JSON Canonicalization Scheme defined by RFC
   8785 and hash those UTF-8 bytes with SHA-256.

Every sequence in the semantic schema MUST declare exactly one ordering. Schema
tests MUST fail if an array schema omits the annotation or declares another
value. The canonical JSON bytes are internal only and MUST NOT be persisted.

The resolver MUST perform these steps in order:

1. Resolve the root identifier to one definition.
2. Verify `kind` is `RuntimeConditionsExtensionDefinition`.
3. Verify resolved `metadata.id` exactly equals the requested identifier.
4. Compute the source-byte SHA-256 and semantic SHA-256.
5. Sort direct dependency identifiers by their exact UTF-8 byte sequence.
6. Resolve every direct dependency recursively.
7. Detect a cycle using the active traversal stack and report the complete cycle.
8. Reject any identifier that resolves to more than one source-byte SHA-256.
9. Produce one topological order with dependencies before dependents; ties are
   ordered by exact extension identifier.
10. Build the resolved vocabulary ownership index.
11. Reject every vocabulary conflict defined by the core specification.
12. Write the exact identifier, semantic version when present, semantic SHA-256,
    and dependency identifiers into the semantic closure supplied to the
    normalizer.
13. Write the exact identifier, semantic version when present, source-byte
    SHA-256, semantic SHA-256, source backend, immutable source locator, and
    dependency identifiers into the dependency lock.

Resolution MUST produce the same topological order regardless of declaration
order, catalog directory traversal order, host filesystem ordering, cache state,
or network response ordering.

### 6.3 Dependency lock

Every promoted binding release MUST contain a generated dependency lock in its
release manifest. For every extension in the closure, the lock MUST record:

- exact identifier;
- source-byte SHA-256;
- semantic SHA-256;
- exact direct dependency identifiers;
- source backend;
- resolved immutable source locator.

A repeated build MUST reject content whose digest differs from the lock. Every
HTTPS redirect MUST fail. A mutable OCI tag is acceptable only when the final
content matches the lock. OCI resolution MUST record the immutable manifest
digest.

The dependency lock and normalized model MUST remain separate data structures
and separate canonicalization domains. The normalized model MUST record, for
each extension, only the exact identifier, semantic version when present,
semantic SHA-256, and exact direct dependency identifiers. It MUST NOT contain a
source-byte SHA-256, source backend, source locator, dependency-lock digest, or
other resolution-transport field.

Before normalization output is accepted, the normalizer boundary MUST verify a
one-to-one match between lock entries and semantic closure records by exact
identifier. The semantic version, semantic SHA-256, and sorted direct dependency
identifiers MUST match exactly. A missing entry, extra entry, or mismatch MUST
stop the build. The orchestrator MUST invoke this verification before emission.
Source-byte and transport fields are verified against fetched content and remain
release provenance; they are not inputs to model canonicalization.

### 6.4 Resolution acceptance checks

The resolver is complete only when all checks below pass:

1. One direct-dependency fixture resolves in exactly one order in 100 repeated
   runs.
2. One three-level transitive fixture resolves dependencies before dependents in
   100 repeated runs.
3. One two-node cycle and one three-node cycle both fail before normalization.
4. One missing direct dependency and one missing transitive dependency both
   fail before normalization.
5. One duplicate identifier with different bytes fails.
6. One duplicate vocabulary definition in the same scope fails even when both
   definitions are textually identical.
7. One local override, one catalog resolution, one `file:` resolution, one
   locked `https:` resolution, and one locked `oci:` resolution produce the same
   semantic closure record and normalized model bytes for byte-identical
   content; their lock entries retain their actual backend and immutable
   locator.
8. Network-disabled execution performs zero outbound requests.
9. Every `RuntimeConditionsExtensionDefinition` under configured repository
   catalog roots resolves and validates without relying on path-derived identity.
10. Mapping-key reorder, YAML scalar-style changes, and reordering a
    schema-marked `set` sequence change the source-byte digest in the dependency
    lock but leave the semantic digest, normalized model bytes, and model digest
    unchanged.
11. Reordering a schema-marked `source` sequence changes the source-byte digest,
    semantic digest, normalized model bytes, and model digest.
12. A missing, extra, or semantically mismatched dependency-lock entry fails
    before emission.

## 7. Language package dependency resolution

Extension dependency resolution and language package dependency resolution are
separate mandatory operations.

For each direct extension dependency, `packages.yaml` MUST contain a binding
package target for every requested language. Generation MUST fail when a direct
dependency lacks a binding package in that language. The package build graph is
the extension dependency graph projected onto one language.

Generated package metadata MUST declare direct language-package dependencies.
It MUST NOT copy dependency-owned declarations into the dependent package. An
additive package MUST import the dependency package contracts and implement the
applicable marker interfaces or protocols generated by the owning package.

Candidate builds MUST use exact dependency package versions from the build plan.
Published dependency metadata and profiler checks MUST use these exact rules:

- Go `go.mod` records the exact tested version as its minimum version. Because
  Minimal Version Selection can choose a higher version, the binding release
  manifest also records the compatible interval and the profiler rejects a
  selected version outside it.
- Python uses `>=<tested>,<next-breaking>`.
- Maven uses `[<tested>,<next-breaking>)`.
- npm uses `>=<tested> <next-breaking>`.

For a `0.y.z` dependency, `next-breaking` is `0.(y+1).0`; for a dependency at or
above `1.0.0`, it is `(major+1).0.0`. The exact tested version and artifact
SHA-256 MUST remain in `runtimeconditions.binding-release.yaml`.

Package resolution MUST use the designated native package manager rather than
recursively scanning package caches. Package-manager and helper-tool versions
MUST be pinned in `toolchain.lock.yaml`.

- Go: use `go mod download`, `go mod verify`, and `go list -deps -json`. Local
  targets use a generated `go.work`; packaged-target tests use temporary exact
  `replace` directives. Release metadata contains no `replace` directive.
- Python: create an isolated virtual environment, install with
  `python -m pip`, inspect distributions with `importlib.metadata`, and build
  with `python -m build`. Internal candidate wheels are installed from a local
  wheelhouse with `--no-index --find-links`; released external dependencies are
  installed from the configured index using a generated fully hashed
  requirements lock.
- Java: use Maven, resolve with `dependency:go-offline`, verify with
  `dependency:tree`, and inspect only resolved classpath artifacts. Internal
  candidate artifacts are installed into an isolated temporary Maven repository.
- JavaScript and TypeScript: use npm, install with `npm ci`, inspect with
  `npm ls --json`, and test internal candidates from exact local package
  tarballs referenced by the generated lock file.

Package caches are permitted only as accelerators and MUST NOT determine
identity.
Resolved package coordinates, versions, and artifact SHA-256 values MUST be
recorded in the build plan. The workspace, wheelhouse, temporary Maven
repository, and local tarball mechanisms above are the only local overrides. CI
builds MUST test packaged artifacts, not only source directories.

The package dependency implementation is complete only when:

1. A dependent package builds against a repository-local dependency package.
2. The same dependent package builds against the packaged dependency artifact.
3. The generated public API is byte-identical in both builds.
4. A missing language binding dependency fails before source emission.
5. A dependency with the correct coordinate but wrong extension identifier
   fails.
6. A dependency with the correct identifier but wrong artifact digest fails.
7. Transitive package dependencies are installed by the native package manager
   and are not duplicated in generated direct dependency metadata.

## 8. Normalized binding model

### 8.1 Artifact identity

The checkpoint filename is:

```text
runtimeconditions.binding-model.yaml
```

Its envelope is:

```yaml
apiVersion: runtimeconditions.io/binding-model/v1alpha1
kind: RuntimeConditionsBindingModel
```

The complete format MUST be defined by
`model/runtimeconditions.binding-model.schema.yaml`. The schema and Go model
types MUST describe identical required fields and cardinalities. A generated
checkpoint that fails this schema MUST never reach an emitter.

### 8.2 Required contents

The model MUST contain:

- root extension identity, semantic version when present, and semantic digest;
- normalizer identity and digest;
- core profile schema identity, version, and semantic digest;
- complete topologically ordered extension closure containing each extension's
  exact identifier, semantic version when present, semantic SHA-256, and exact
  direct dependency identifiers;
- dependency edges;
- vocabulary owners;
- owned kind declarations;
- kind-scoped interface types;
- condition fields expanded to exact scopes;
- interface fields expanded to exact scopes;
- parsed field-path segments with array traversal explicit;
- scoped portable value domains;
- exact JSON Schema documents represented as YAML data;
- language-neutral structural projections;
- validation constraints not expressible by structural projections;
- semantic provenance for every normalized declaration, field, value domain,
  structural node, and constraint;
- deterministic diagnostics for unsupported constructs;
- canonical semantic SHA-256 for the complete model excluding the digest field
  itself.

The model MUST contain no source-byte SHA-256 values, source backends, source
locators, dependency-lock digests, language symbol names, package-manager
coordinates, source filenames for generated packages, or language-specific
types.

### 8.3 Restricted YAML profile

Generated model YAML MUST obey all of these rules:

1. UTF-8 without a byte-order mark.
2. LF line endings.
3. Exactly one trailing newline.
4. String mapping keys only.
5. No duplicate mapping keys.
6. No custom tags.
7. No anchors, aliases, or merge keys.
8. No comments.
9. Optional fields omitted when unused.
10. Ambiguous strings quoted.
11. Schema-defined mapping field order.
12. Set-like sequences sorted by their documented canonical keys.
13. Semantically ordered sequences retain their source order.

The model digest MUST be computed by converting this YAML data model to
JSON-compatible values, serializing them with RFC 8785, and applying SHA-256 to
the resulting UTF-8 bytes. The canonical JSON bytes MUST not be persisted as an
artifact. Because resolution evidence is excluded from the model, changing only
YAML presentation, a schema-marked `set` sequence order, source backend, source
locator, or source-byte digest MUST NOT change model bytes or the model digest.

### 8.4 Provenance

Every normalized semantic element MUST point to:

- the owning extension identifier;
- the extension semantic SHA-256;
- the extension vocabulary coordinate or schema identifier;
- the JSON Pointer within the schema when the element originates in JSON Schema.

Provenance MUST use semantic coordinates, not YAML sequence indexes. Reordering
set-like source declarations MUST therefore leave provenance and model bytes
unchanged.

## 9. Universal structural normalization rules

Every `schemas[].schema` document MUST be processed as JSON Schema Draft
2020-12. An omitted `$schema` uses that dialect; a present `$schema` MUST name
the Draft 2020-12 dialect. Validation MUST use a complete Draft 2020-12
implementation even when structural projection cannot express a keyword.

These rules apply to every extension without identifier-specific handling:

1. An owned kind becomes a declaration coordinate.
2. A dependency-owned kind remains an imported declaration contract and MUST
   NOT become a declaration owned by the root package.
3. An interface type becomes an object shape in its exact kind and interface
   scope. Fixed `kind` and `interface.type` values are metadata and are not
   caller-supplied fields.
4. A condition field becomes a root Condition property in each declared scope.
5. An interface field becomes an `interface` property in its exact scope.
6. An object schema becomes an object shape. Property names are preserved
   exactly in the model.
7. A scalar schema becomes the corresponding language-neutral string, boolean,
   integer, number, or null shape. It MUST NOT become a schema-named wrapper
   shape unless `enum`, `const`, `fieldValues`, or a union requires a named
   domain.
8. An array schema becomes a collection shape. Array traversal is represented by
   an explicit path-segment flag, never by singularizing a property name.
9. An object-valued array item becomes an item object associated with the full
   property path.
10. `additionalProperties` with a schema becomes a map-value shape.
11. Scalar `enum` and scoped `fieldValues` become value domains.
12. A scalar `const` becomes a one-value domain. Const values occupying the same
    scoped path in alternative branches are unioned into that path's value
    domain.
13. A property is structurally required across `oneOf` or `anyOf` only when it is
    required in every branch. Branch-specific requiredness remains an exact
    validation constraint.
14. Object branches under `oneOf` or `anyOf` are projected to one object whose
    property set is the union of branch properties. The original alternatives
    remain in the exact schema and validation constraints.
15. Heterogeneous `oneOf` or `anyOf` branches remain an explicit union shape.
16. `allOf` is a conjunction. Object properties and required properties are
    unioned; incompatible structural types stop normalization.
17. Every referenced `$defs` entry becomes a named shape derived mechanically
    from its definition key. Unreferenced `$defs` entries remain validation data
    and do not create public API.
18. A `$ref` MUST be either `#` or a fragment beginning with `#/` that contains
    a syntactically valid JSON Pointer. It resolves only within the containing
    schema document. `$anchor`, `$dynamicAnchor`, `$recursiveAnchor`, and anchor
    references are forbidden. Recursive JSON Pointer references remain named
    recursive references and MUST NOT be infinitely expanded.
19. Binding-model API `v1alpha1` does not support references outside the
    containing schema document; encountering one stops normalization.
20. Validation-only keywords, including cardinality, uniqueness, range, length,
    pattern, format, and conditional branch relationships, are preserved as
    constraints even when native types cannot enforce them.
21. A structure-changing JSON Schema keyword not defined by the binding-model
    schema stops normalization with the extension identifier, schema identifier,
    JSON Pointer, and keyword.
22. Applicable schemas combine additively. A required property from any
    conjunctive applicable schema is required. Conflicting structural types stop
    normalization. Validation domains are intersected; an empty intersection
    stops normalization.
23. A scoped `fieldValues` domain MUST resolve to a defined field path in the
    complete closure. Every declared value MUST be accepted by all applicable
    schemas. Failure of either check stops normalization.
24. A normalized symbol or shape MUST NOT depend on descriptions, repository
    paths, filenames, example documents, or operation-specific conventions.

## 10. Language emitter contract

Every emitter MUST implement this command contract:

```text
<emitter> \
  --model runtimeconditions.binding-model.yaml \
  --package-config <one target from packages.yaml> \
  --output <new empty directory>
```

The emitter MUST reject a missing model field, unsupported model API version,
unknown structural node, non-empty output directory, mismatched package target,
or unsupported language version.

Every emitter MUST generate:

1. Language-native source.
2. Native package-manager metadata.
3. `runtimeconditions.bindings.yaml`.
4. A language public-API descriptor named
   `runtimeconditions.public-api.yaml`.
5. Conformance source exercising every declaration, object type, field, enum
   value, collection shape, map shape, and union shape at least once.
6. Expected profile YAML for each conformance declaration.

After an emitter succeeds, the orchestrator MUST assemble the final generated
package tree by adding:

1. `runtimeconditions.extension.yaml` for the root extension from the validated
   resolver input;
2. the exact `runtimeconditions.binding-model.yaml` consumed by the emitter;
3. `runtimeconditions.binding-release.yaml`, including the dependency lock and
   complete source-resolution provenance; and
4. a file manifest containing the relative path and SHA-256 of every generated
   file except the manifest itself.

The orchestrator MUST NOT modify emitter-produced source, package metadata,
binding metadata, public-API metadata, conformance source, or expected profiles
during assembly. Source-byte digests, source backends, and source locators MUST
appear only in `runtimeconditions.binding-release.yaml`; they MUST NOT appear in
emitter-produced files or generated source headers.

Generated source and metadata MUST contain a standard non-editable header. The
header MUST name the model digest and emitter version, but MUST NOT contain a
timestamp or host-specific path.

### 10.1 Shared API rules

1. Root Condition fields are flat declaration arguments or fields; emitters MUST
   NOT introduce semantic placement wrappers.
2. Native object construction MUST be used for object shapes.
3. Native collection construction MUST be used for arrays.
4. Native maps MUST be used for map shapes.
5. Public type and field names derive only from canonical vocabulary paths and
   language naming rules.
6. Array-item type names derive from the property name followed by `Item`; they
   MUST NOT use grammatical singularization.
7. Name collisions are resolved by prepending parent path components from
   nearest to farthest, followed by kind and interface scope when required.
8. A collision that remains after the complete canonical path and scope are used
   stops emission.
9. Reserved words are escaped by one documented language rule.
10. Value-domain members are emitted as language-native enums or typed constants.
11. Alternative branches MUST NOT become operation-specific constructors,
    factories, or differently shaped APIs.
12. Invalid cross-field combinations are rejected during profile-generation-time
    semantic validation, not by custom semantic constructors.
13. Declaration functions retain the language-specific `Declaration` return
    convention. The return value is an inert source-declaration anchor, not an
    in-memory Condition representation.

### 10.2 Canonical symbol derivation

Symbol derivation MUST use the following tokenizer before applying a language's
case convention:

1. Process the exact UTF-8 vocabulary or property-path segment; do not translate,
   singularize, pluralize, stem, or interpret it.
2. Split ASCII runs at non-alphanumeric bytes, lower-or-digit to upper-case
   transitions, letter-to-digit transitions, digit-to-letter transitions, and
   before the last capital in a capital run followed by a lower-case letter.
3. Encode each maximal non-ASCII byte run as one token consisting of `u` followed
   by the upper-case hexadecimal bytes. This keeps every generated identifier
   within the portable ASCII identifier subset.
4. Drop empty separator runs. If no token remains, use the token `x`.
5. Preserve the complete ordered token list in the normalized model. Emitters
   MUST NOT retokenize source strings.

The fixed initialism set is `ACL`, `API`, `ASCII`, `CPU`, `CSS`, `DNS`, `EOF`,
`GUID`, `HTML`, `HTTP`, `HTTPS`, `ID`, `IP`, `JSON`, `QPS`, `RAM`, `RPC`, `SDK`,
`SLA`, `SMTP`, `SQL`, `SSH`, `TCP`, `TLS`, `TTL`, `UDP`, `UI`, `UID`, `URI`,
`URL`, `UTF8`, `UUID`, `VM`, `XML`, `XMPP`, `XSRF`, and `XSS`. Initialism
matching is ASCII case-insensitive. Changing this set is a naming-rule change
and MUST be processed as a public-API compatibility change.

Names MUST be allocated as complete sets, never first-come-first-served. If two
semantic coordinates initially produce the same name, every member of that
collision group MUST prepend the nearest unused parent-path token group. This is
repeated from nearest to farthest, then with interface-type tokens, then kind
tokens. Emission MUST fail if the complete canonical path and scope still
collide. The binding manifest MUST retain the serialized source name for every
native symbol.

### 10.3 Go rules

1. Every owned kind becomes one exported declaration function named from the
   kind, with signature `func <Kind>(fields ...<Kind>Field) Declaration`.
2. `<Kind>Field` is an exported marker interface owned by the package that owns
   the kind. Every applicable interface object and additive field object
   implements that interface.
3. Objects become structs. Object construction uses struct literals; the emitter
   MUST NOT generate constructors.
4. Arrays become named slice types when referenced by a public field.
5. Maps become named map types when referenced by a public field.
6. Optional scalar and enum fields use pointers. Required scalar and enum fields
   use values.
7. Optional arrays, maps, and objects use pointers only when omission and an
   empty native value have different schema meaning; this decision is derived
   mechanically from requiredness.
8. Marker-interface methods required for cross-package additive extensions are
   exported and derived from the owning declaration coordinate.
9. The declaration result is a zero-sized `Declaration` value.
10. Package-scope usage is `var _ = package.Declaration(...)`.
11. Exported types, fields, functions, and constants use Pascal case. Each token
   in the fixed initialism set is upper case; every other alphabetic token is
   lower case with its first byte upper case. A leading numeric token is prefixed
   with `X`.
12. Package identifiers use lower-case concatenated tokens. A Go keyword gains
    the suffix `binding`.
13. Enum and typed-constant member names start with the Pascal-cased value. The
    collision algorithm prepends their value-domain type before path and scope
    tokens.
14. Source MUST pass `gofmt`, `go vet`, and `go test` using the declared minimum
    Go version.

### 10.4 Python rules

1. Every owned kind becomes one function named from the kind, with signature
   `def <kind>(*fields: <Kind>Field) -> Declaration`.
2. `<Kind>Field` is a structural `Protocol` owned by the package that owns the
   kind. Every applicable interface object and additive field object satisfies
   that protocol through its generated class marker.
3. Objects become frozen, keyword-only dataclasses. Their standard dataclass
   initializers provide named-argument construction; the emitter MUST NOT add
   separate constructor functions.
4. Arrays become `Sequence[T]` aliases and conformance declarations use tuples.
5. Maps become `dict[K, V]` aliases.
6. Optional fields use `T | None` and default to `None`.
7. Value domains become `StrEnum` when all values are strings.
8. Declaration functions return an inert `Declaration` object.
9. Classes and type aliases use Pascal case with the same initialism handling as
   Go. Functions, parameters, fields, and modules use lower-case tokens joined
   by `_`. Enum members and typed constants use upper-case tokens joined by `_`.
10. A leading numeric token is prefixed with `X` for classes and `x_` for every
   other symbol. A Python keyword gains one trailing underscore. A generated
   name beginning and ending with two underscores gains the prefix `rc_`.
11. Source MUST pass `python -m compileall`, `ruff format --check`,
   `mypy --strict`, and `pytest` using the declared minimum Python version and
   versions pinned in `toolchain.lock.yaml`.

## 11. Binding manifest and profile generation

`runtimeconditions.bindings.yaml` MUST map source constructs to normalized
coordinates. It MUST support:

- declaration calls;
- native object construction;
- named object fields;
- collection elements;
- map entries;
- enum and typed-constant values;
- omitted optional values;
- imported marker contracts from dependency packages;
- exact model and extension digests.

Generated packages MUST place `runtimeconditions.bindings.yaml`,
`runtimeconditions.extension.yaml`, `runtimeconditions.binding-model.yaml`, and
`runtimeconditions.binding-release.yaml` together at exactly these locations:

- Go: the source directory of the package that defines the declaration
  functions, as returned by `go list -json`.
- Python: the generated import-package directory included as package data, as
  located through `importlib.resources` from distribution metadata.
- Java: `META-INF/runtimeconditions/` in the JAR.
- JavaScript and TypeScript: `runtimeconditions/` at the resolved npm package
  root.

Profilers MUST begin from the imported package symbol, ask the native package
manager for its resolved artifact location, and read only the fixed location
above. Recursive cache or filesystem searches are forbidden.

Profilers MUST parse source and static metadata. They MUST NOT execute application
or package code. They MUST resolve imported packages through the native package
manager, inspect only resolved package locations, and load binding artifacts from
the language-standard package locations.

For every conformance declaration, profile generation MUST:

1. Reconstruct a Condition from source.
2. List every extension that directly contributed vocabulary.
3. Resolve the complete transitive extension closure.
4. Validate core structure.
5. Validate extension resolution and vocabulary ownership.
6. Apply every matching JSON Schema from the closure.
7. Reject every semantic error before writing an accepted profile.

The phrase "runtime validation" in this project means validation performed by
the profile-generation workflow against the exact extension closure. It does not
mean application runtime validation and does not require declaration values to
remain in application memory.

## 12. Determinism and conformance suite

The conformance suite MUST contain at least these 12 language-neutral cases:

1. Owned kind and interface.
2. Additive field targeting dependency-owned vocabulary.
3. Three-level transitive dependency closure.
4. Dependency cycle.
5. Vocabulary ownership conflict.
6. Recursive local `$ref`.
7. Object-only `oneOf` with branch-dependent required fields.
8. Heterogeneous `oneOf`.
9. Arrays, object items, scalar items, and schema-valued maps.
10. Scoped value domains and normalized-name collisions.
11. Reserved language words, acronym boundaries, leading digits, non-ASCII
    encoding, and normalized-name collisions.
12. Unsupported structure-changing schema keyword.

A conformance case MUST NOT cause production code to be keyed to the case's
identifier or vocabulary values.

Each positive case MUST have one committed expected normalized YAML file. Each
negative case MUST have one committed diagnostic file containing the exact error
category, semantic coordinate, and JSON Pointer when applicable.

Determinism is accepted only when:

1. Normalizing each positive case 100 times yields one SHA-256 value.
2. Randomly permuting every set-like source sequence 100 times yields the same
   normalized bytes.
3. Running on Linux and macOS yields byte-identical checkpoints.
4. Each emitter run three times from a new empty directory yields identical file
   manifests and identical file bytes.
5. A second full repository generation produces a zero-byte Git diff.
6. Every extension definition found under configured catalog roots normalizes
   successfully or fails with a documented unsupported keyword; an extension
   identifier or vocabulary value special case MUST NOT cause a failure.

For this section, normalized bytes means the complete serialized
`runtimeconditions.binding-model.yaml`. Dependency-lock and release-provenance
bytes are deliberately outside that comparison.

## 13. Generated package verification

A generated target is valid only when all of these gates pass:

1. The normalized model validates against its model schema.
2. The binding manifest validates against its manifest schema.
3. Package metadata parses with the native package manager.
4. Every generated source file passes the native formatter.
5. Every generated source file compiles with the minimum supported language
   version.
6. The native static analyzer/type checker reports zero errors.
7. The native unit test suite reports zero failures.
8. Every public declaration, type, field, value-domain member, collection shape,
   map shape, and union shape appears in conformance source at least once.
9. Every conformance declaration generates the exact expected profile.
10. Every expected profile passes core, extension-resolution, vocabulary, and
    JSON Schema validation.
11. At least one generated negative fixture for each branch-dependent constraint
    fails semantic validation.
12. Rebuilding produces byte-identical generated source.
13. The package archive contains every required Runtime Conditions resource.
14. The package archive contains no undeclared file.
15. The generated file-manifest digests match every file.
16. The public-API descriptor is derivable from generated source and matches the
    committed descriptor byte-for-byte.

Any failed gate stops packaging and promotion.

## 14. Semantic versioning

Each language package has an independent SemVer version. Every package remains
pinned to one exact root extension identifier, exact extension closure, and exact
model digest. Extension and package versions are not permanently lockstep.

For a package with no prior release, a valid extension SemVer is the suggested
initial package version. When the extension does not provide a SemVer release,
the package starts at the explicit version in `packages.yaml`.

For packages at or above 1.0.0:

- major: any source-breaking or behavior-breaking change;
- minor: any backward-compatible public declaration, field, type, or value-domain
  addition, or any validation relaxation;
- patch: implementation, packaging, manifest, or metadata correction that does
  not change accepted declarations or public API;
- no release: byte-only regeneration with no package-content change.

For packages below 1.0.0:

- breaking change increments the minor component and resets patch to zero;
- backward-compatible addition increments patch;
- compatible correction increments patch;
- no semantic or package-content change produces no release.

The compatibility classifier MUST compare:

1. Old and new normalized extension closures.
2. Old and new language public-API descriptors.
3. Old and new `runtimeconditions.bindings.yaml` behavior manifests.
4. Old and new direct package dependencies.
5. Old and new minimum language versions.

Removal, rename, type change, optional-to-required change, value removal,
validation tightening, package-coordinate change, or an exposed dependency
breaking change is breaking. A schema change whose compatibility cannot be
proved mechanically is breaking. The requested version is accepted when it is
equal to or greater than the mechanically required bump and is rejected when it
is smaller.

Every target tag MUST have this exact form:

```text
bindings/<package-key>/<language>/v<major>.<minor>.<patch>
```

The tag prefix exactly matches that target's generated repository directory.

Reaching Go module version 2 or greater MUST update the module path with the
required semantic import suffix and MUST regenerate every dependent Go binding.

## 15. Local orchestration

The single local entry point is `rc-extension-bindings`. It MUST expose:

```text
rc-extension-bindings resolve
rc-extension-bindings normalize
rc-extension-bindings generate
rc-extension-bindings verify
rc-extension-bindings package
rc-extension-bindings plan-release
rc-extension-bindings check
rc-extension-bindings update
```

All commands MUST accept `--packages`, `--toolchain-lock`, `--cache`, and
`--work-dir`. Target-aware commands MUST accept repeatable
`--target <package-key>:<language>`; no `--target` means all configured targets.
Resolution commands MUST accept repeatable `--extension-root` and
`--extension-override <id>=<path>`. Network access MUST require `--network`.

The command behaviors are fixed:

- `resolve` writes the validated semantic closure and separate dependency lock
  under `--work-dir`.
- `normalize` performs resolution, verifies the semantic closure against the
  dependency lock, and writes the normalized model under `--work-dir`.
- `generate` performs normalization and emission under `--work-dir`.
- `verify` accepts `--input <generated-tree>`, verifies that tree without
  modifying it, and writes reports under `--work-dir`.
- `package` performs resolve through verification and writes package and release
  archives under `--work-dir`.
- `plan-release` compares committed targets with their previous GitHub release
  manifests and writes the build plan and required SemVer impact under
  `--work-dir`.
- `check` performs a clean full generation and verification, then compares it
  byte-for-byte with committed target directories without modifying them.
- `update` performs the same clean full generation and verification, then
  atomically synchronizes only the selected `bindings/<package-key>/<language>`
  directories, including removal of stale generated files.

`generate`, `verify`, and `package` MUST operate in a newly created staging
subdirectory of `--work-dir`. `check` and `update` MUST create the same staging
structure internally. Every command MUST refuse its own existing non-empty
staging subdirectory. Temporary paths and timestamps MUST NOT enter generated
files. `update` is the only command permitted to mutate committed generated
targets; no command is permitted to mutate extension YAML.

The local full verification command MUST complete all gates in Section 13 and
return zero only when the working tree's generated binding source is current.
The command MUST print one machine-readable YAML summary and one concise human
summary. It MUST never modify extension YAML.

## 16. GitHub Actions orchestration

### 16.1 Reusable build workflow

`.github/workflows/_binding-build.yml` is the only workflow that builds binding
artifacts. It MUST be callable with `workflow_call` and receive exactly two
inputs: `targets`, a comma-separated list of `<package-key>:<language>` build
target keys, and `network`, a boolean network policy. It MUST perform:

```text
plan -> resolve -> normalize -> emit -> verify -> package -> assemble release set
```

The `plan` job MUST write `binding-build-plan.yaml`. The plan MUST include every
root target, extension closure, language package dependency, build group,
toolchain version, requested package version, and expected output path.

Build groups are connected dependency components within one language. Every
independent build group MUST be a separate matrix entry, allowing the Actions
scheduler to run independent groups concurrently. Packages inside one group
MUST build in topological order. A matrix entry MUST represent one complete build
group so that dynamic dependency ordering is handled by the orchestrator, not by
guessed GitHub `needs` relationships.

The reusable workflow MUST have `contents: read`, no registry credentials, and
no release permissions. It MUST upload one artifact per build group plus one
aggregate release-set artifact. Artifact names MUST include the workflow run ID,
language, package key, and model digest prefix.

### 16.2 Pull-request checks

`.github/workflows/binding-checks.yml` MUST run for changes to extension
definitions, package configuration, the binding-model schema, normalizer,
resolver, orchestrator, emitters, profiler contracts, or generated bindings.

It MUST call `_binding-build.yml`, compare regenerated source to committed
source, publish the API and version-classification summary to the workflow
summary, and upload candidate artifacts. It MUST use no write permission and
perform no tagging or publication.

Required quantitative results are:

- zero uncommitted generated-source differences;
- zero formatter, compiler, analyzer, type-checker, or test failures;
- zero unresolved extensions or package dependencies;
- zero schema-validation failures for positive fixtures;
- the expected non-zero failures for every negative fixture;
- one release-impact classification for every changed target.

### 16.3 GitHub promotion

`.github/workflows/binding-promote.yml` MUST use `workflow_dispatch` and run only
from the default branch. It MUST accept `targets` in the same format as the
reusable workflow and a boolean `dry_run`. Requested versions MUST come only
from the committed `packages.yaml`; arbitrary versions and source refs are
forbidden.

The workflow MUST call `_binding-build.yml` once. After the aggregate release set
is built and verified, a separate job guarded by the protected
`binding-github-release` environment MUST:

1. Download the aggregate artifact from the same workflow run.
2. Recompute and verify every file digest.
3. Verify the source commit is still the default-branch head selected by the
   workflow.
4. Verify requested versions satisfy the compatibility classifier.
5. Preflight every target and reject a tag or release collision unless the
   existing tag, commit, release manifest, and asset digests are all exact
   matches.
6. Process targets in package-dependency topological order.
7. For each target not already promoted exactly, create its exact Section 14 tag
   on that commit.
8. For each target not already promoted exactly, create one GitHub Release
   attached to that target's tag and name it
   `<package-key> <language> v<version>`.
9. Upload only that target's exact verified artifacts and release manifest to
   its GitHub Release.
10. Record every release URL, asset URL, and digest in the workflow summary.

The approval occurs after build and verification. With `dry_run: true`, the
workflow MUST perform every verification through tag and release collision
checks and then stop before its first mutation. With `dry_run: false`, it MUST
perform the mutations above. The promotion job MUST NOT rebuild, reformat,
regenerate, or alter an artifact. The aggregate release-set workflow artifact
is verification and transfer evidence only; it MUST NOT be published as a
GitHub Release and MUST NOT receive a tag. An interrupted promotion MUST be
resumable: an exact existing target is skipped, and a non-exact collision stops
the workflow before any additional target is mutated.

### 16.4 External publication

When external publication is authorized,
`.github/workflows/binding-publish.yml` MUST trigger only from a published GitHub
Release. Each run handles the single target identified by that release's tag. It
MUST download release assets, verify the release manifest and every digest, and
publish those exact bytes through one protected environment per registry. It
MUST never invoke an emitter or package builder.

Targets with `github-tag` publication mode MUST stop after GitHub promotion.
Targets with `registry` publication mode MUST publish only to their configured
registry. A manual retry MUST accept one existing release tag, verify that
release again, and contact only its configured registry. Registry rate limits
MUST be respected by serializing publication per registry and preventing
concurrent publication of the same package coordinate. A registry response with
`Retry-After` MUST be retried at that exact delay. Without `Retry-After`, the
workflow MUST make at most five retries after 30, 60, 120, 240, and 480 seconds,
then fail without contacting another registry. External publication remains
absent until this workflow is separately approved.

## 17. Output retention and publication

The repository MUST commit:

- tooling source and tests;
- model schemas;
- conformance inputs and expected normalized outputs;
- package and toolchain configuration;
- generated package source;
- generated binding manifests;
- generated normalized checkpoints;
- generated public-API descriptors;
- generated conformance source and expected profiles;
- generated release metadata that is independent of archive digests.

The repository MUST NOT commit:

- package-manager caches;
- temporary staging directories;
- wheels, source distributions, JARs, or generic package archives;
- CI logs;
- workflow artifact downloads;
- credentials;
- host-specific lock state.

Each target-specific GitHub Release MUST contain:

1. `runtimeconditions.binding-release.yaml`.
2. `runtimeconditions.binding-model.yaml`.
3. The source archive for its target.
4. The native package archive for its target except ecosystems distributed
   directly from version-control tags.
5. `SHA256SUMS` covering every release asset except `SHA256SUMS` itself.
6. Toolchain provenance.
7. Public-API and compatibility reports.
8. Conformance expected profiles.

Generated packages MUST place Runtime Conditions resources at the exact Section
11 locations. Published packages MUST include the root extension definition,
binding manifest, normalized model, and binding release manifest.
Dependency extension definitions are supplied by their own dependency packages;
the aggregate GitHub release set MUST additionally contain the complete extension
closure for offline verification.

## 18. Security and operational requirements

1. YAML MUST be parsed with a safe data-only parser.
2. Duplicate mapping keys MUST be rejected before normalization.
3. YAML input MUST be limited to 64 MiB, 1,000,000 decoded nodes, 256 levels of
   nesting, and 100 alias references. Exceeding a limit MUST fail before
   normalization.
4. Package code MUST never be imported or executed to discover bindings.
5. Extension, package, and binding manifests MUST be treated as untrusted static
   data.
6. Each resolver download MUST require TLS certificate validation, reject every
   redirect, allow at most 64 MiB after decompression, use a 10-second
   connection timeout, and use a 60-second total timeout.
7. Every downloaded or cached artifact MUST be verified against its lock digest.
8. Logs, diagnostics, models, and profiles MUST contain no credentials, secret
   values, environment values, or customer data.
9. Release jobs alone receive `contents: write`; registry credentials are scoped
   to their protected publication environment.
10. Workflows MUST pin third-party GitHub Actions by full commit SHA.
11. Build jobs MUST use dependency lock files and declared minimum toolchain
    versions.
12. A failed verification or incomplete provenance record MUST stop promotion.

## 19. Implementation phases and exit criteria

### Phase 1: binding-model schema, resolver, and normalizer

Deliver the model schema, Go data model, canonical YAML writer, extension
resolver, structural normalizer, CLI, and 12-case conformance suite.

Exit requires:

- all Section 6.4 resolution checks;
- all Section 12 determinism checks applicable to normalization;
- schema validation of every positive expected model;
- exact diagnostics for every negative case;
- normalization of every configured repository extension without
  identifier-specific code;
- confirmation that no normalized model or model digest contains or depends on
  source-byte SHA-256 values, source backends, source locators, or dependency-lock
  digests;
- zero extension YAML modifications.

### Phase 2: Go emitter

Deliver the Go emitter, package metadata generation, public-API descriptor,
conformance package, and package archive verification.

Exit requires all 16 checks in Section 13 for every positive conformance model,
plus exact expected failures for every negative model relevant to Go.

### Phase 3: Python emitter

Deliver the Python emitter, package metadata generation, public-API descriptor,
conformance package, wheel and source-distribution verification.

Exit requires all 16 checks in Section 13 for every positive conformance model,
plus exact expected failures for every negative model relevant to Python.

### Phase 4: structural binding manifests and profiler support

Deliver the structural binding-manifest schema and Go and Python profiler support
for every construct in Section 11.

Exit requires:

- exact profile generation for 100% of positive conformance declarations;
- expected semantic failure for 100% of negative declarations;
- zero application or package-code execution;
- native package-manager resolution tests for local and packaged dependencies;
- complete extension closure validation for every generated profile.

### Phase 5: local orchestration and committed generated packages

Deliver `packages.yaml`, `toolchain.lock.yaml`, the orchestrator, build planning,
version classification, repository output comparison, and generated package
trees.

Exit requires:

- a clean full build from an empty work directory;
- a second full build with a zero-byte Git diff;
- correct topological language-package build ordering;
- exact package archive content checks;
- successful local execution with network disabled using a populated cache;
- successful local execution with network enabled from an empty cache.

### Phase 6: pull-request and GitHub promotion workflows

Deliver `_binding-build.yml`, `binding-checks.yml`, protected promotion
configuration, and `binding-promote.yml`.

Exit requires:

- one pull-request run with no write permissions and no publication;
- one manually approved dry-run promotion that performs no release mutation;
- one approved GitHub release using artifacts built before approval;
- matching local and Actions artifact SHA-256 values;
- zero rebuild steps after the protected-environment approval.

### Phase 7: external registry publication

This phase starts only after explicit approval. It delivers
`binding-publish.yml` and registry-specific credential environments.

Exit requires, for each enabled registry:

- publication of a disposable pre-release package from existing GitHub assets;
- exact downloaded-versus-published artifact digest equality where the registry
  exposes artifact bytes;
- an idempotent rerun that performs no duplicate publication;
- one isolated failed-registry retry without contacting any already successful
  target registry;
- documented rollback or deprecation procedure supported by that registry.

## 20. Definition of production readiness

The system is production-ready only when all seven phases are complete for every
enabled language and registry, every check in Sections 6.4, 7, 12, and 13 passes,
the default branch has zero generated-source drift, all production workflows use
released locked tooling, and a complete binding release can be reproduced from
its release manifest with byte-identical outputs.

Until then, generated packages MUST be labeled pre-release, external registry
publication MUST remain disabled, and GitHub Releases MUST state which production
readiness gates remain incomplete.
