# Phase 4 final report and Phase 5 handoff

Report date: 2026-10-04.

**Phase 4 is accepted within the approved local verification scope.** All
applicable verification gates, 1–14 and 16, pass. Go and Python reconstruct
complete profiles from installed generated bindings, validate their complete
extension closure, and reject invalid declarations before writing a profile.
There are no remaining blockers in that accepted scope.

This report consolidates the work across `extensions`, `go-rc-profiler`, and
`python-rc-profiler`. It supplements the concise
[Phase 4 gate record](/Users/colacy/code/github.com/runtimeconditions/extensions/tooling/extension-bindings/PHASE-4.md).
The [implementation standard](/Users/colacy/code/github.com/runtimeconditions/extensions/tooling/extension-bindings/IMPLEMENTATION.md)
remains the authority for Phase 5. Earlier phase reports describe historical
contracts and counts; their provisional manifests and incomplete fixture
inventories must not be used as the current integration contract.

## 1. What acceptance establishes

| Readiness question | Final result |
| --- | --- |
| Can the profilers work as separately installed CLIs? | Yes. Acceptance invokes installed artifacts against unrelated workloads and dependencies installed by native package managers. |
| Must an end user clone `extensions` or either profiler repository? | No. The profiling inputs are workload source, installed binding packages and their packaged resources, native dependency metadata, and the profiler's bundled schema assets. |
| Do the profilers execute workload or binding code? | No. Static extraction and execution sentinels demonstrate the accepted workflow without application or binding initialization. |
| Are profiles fully validated before output? | Yes. Core structure, extension identities and closure, vocabulary ownership and values, and every applicable closure schema are checked. |
| Are the generated packages production releases? | No. The installed corpus consists of explicitly identified test fixtures with real provenance. |
| Is the entire generation and publication system production-ready? | That claim remains governed by Section 20: all seven phases, released locked tools and profilers, reproducible production releases, and the required workflow and registry evidence. |

The profilers are functionally ready for the end-user generated-binding
workflow tested here. Production delivery must provide properly built,
released profiler artifacts and generated packages satisfying these contracts.
Phase 4 does not establish that those artifacts have been published or that a
clean `go install` of the Go repository reproduces the tested executable.

The distinction matters for Phase 5: build on the verified extraction and
validation engines, preserve their installed-package boundary, and supply the
production configuration, orchestration, release identities, and committed
generated source that are still absent.

## 2. Decisions that constrain future work

1. The structural binding manifest is the integration contract. Production
   behavior must not be reconstructed from the legacy manual SDK mapping
   approach or a flat symbol inventory.
2. Each profiler remains in its own repository and uses its language's native
   source and package metadata tools. Profiler source must never be embedded in
   `tooling/extension-bindings/` or its tooling release.
3. Go and Python are the enabled languages for this work. Other languages were
   not implemented or accepted here.
4. Profiles are complete documents, including profile identity, workload
   identity, direct extension contributors, and Conditions. Condition-shape
   checks alone do not establish acceptance.
5. An emitted profile lists extensions that directly contributed vocabulary.
   Validation resolves their full dependency closure, including dependencies
   that supply only schemas.
6. Generated native APIs enforce structural types. Full JSON Schema semantics
   remain the profiler's responsibility; constructors do not become runtime
   validators.
7. Legacy SDK mapping compatibility may be sacrificed when it conflicts with
   generated-binding interoperability. Existing compatibility code is not an
   authority for the new contract. In Python, use the `profile` command group
   for this workflow; the older top-level `generate` route remains separate.
8. The SDK binding-generation page was treated as research and intent for
   automation. Updating it was deferred. The implementation standard, current
   schemas, and accepted evidence take precedence over its technical examples.
9. Production code must stay driven by semantic coordinates and model data.
   Conformance identifiers, vocabulary values, and local repository paths must
   not select implementation behavior.
10. The final closure work and this report leave `spec/` unchanged. The protected
    implementation standard and its guard remain unchanged. A Phase 5 agent
    must follow `AGENTS.md` and obtain the exact file-list approval required by
    Section 4 before changing more than five files.

## 3. Work completed during Phase 4

### 3.1 Replaced the provisional manifest

Phase 2 supplied a provisional symbol inventory, and Phase 3 emitted against
that provisional contract. Those records could not tell a profiler how to
reconstruct nested native values at exact model locations.

The shared
[structural manifest schema](/Users/colacy/code/github.com/runtimeconditions/extensions/tooling/extension-bindings/model/runtimeconditions.binding-manifest.schema.yaml)
now fixes `apiVersion: runtimeconditions.io/bindings/v1alpha2` and rejects
unknown fields. Both emitters produce it. The required sections are
`generated`, `model`, `extension`, `package`, `declarations`,
`importedMarkerContracts`, `rootBindings`, and `types`.

| Structural record | Information available to a profiler |
| --- | --- |
| Declaration | Exact model coordinate, owning extension, original source name, native function, and marker contract. |
| Root binding | Declaration coordinate, kind/interface scope, serialized path, native value reference, and interface, vocabulary-field, or schema-field role. |
| Named type | Exact model reference, source/native names, construction form, and declaration contracts implemented by that type. |
| Object field | Its containing type, serialized `sourceName`, native field name, requiredness, and child value reference. |
| Collection or map | Its containing type and exact item/value model reference. |
| Union | Ordered alternatives and their exact native representations and model references. |
| Constant or enum member | Native member name, model reference, and exact literal value. |
| Optional reference | Explicit Go pointer or Python nullable representation, distinct from requiredness. |
| Imported marker | Owning declaration contract and, where applicable, the Python provider package needed to resolve it. |

`modelRef.coordinate` and `modelRef.jsonPointer` are separate lookup fields.
Fields, elements, members, and variants are children of their containing type;
they do not rely on ambiguous parent-name strings. Native references must
resolve to one type or a permitted builtin. Serialized names survive native
naming transformations, including separator changes, case transitions,
digits, and Unicode.

Identity checks cover native package identity, model API version and digest,
and root extension ID and digest. JSON Schema validation is followed by
cross-resource checks and reference resolution. The regression suite explicitly
rejects the old symbol inventory, including one relabeled with the new API
version, and rejects stale model or extension identities. There is no accidental
compatibility path for provisional manifests.

### 3.2 Added the missing shared release contract

The absence of `runtimeconditions.binding-release.schema.yaml` was a real
integration blocker. Running the emitters against the catalog would not create
that schema: it is a shared contract, not an extension-specific output.

The
[release schema](/Users/colacy/code/github.com/runtimeconditions/extensions/tooling/extension-bindings/model/runtimeconditions.binding-release.schema.yaml)
now fixes `apiVersion: runtimeconditions.io/binding-release/v1alpha1` for Go
and Python. It records:

- package key, language coordinate, native name, version, minimum language
  version, and publication mode;
- model API version and semantic digest;
- root extension identity and semantic digest;
- the complete extension dependency lock, including exact source digests,
  semantic digests, versions, source backends/locators, and dependency edges;
- direct language-package dependencies, tested versions, compatible intervals,
  and artifact kinds/digests;
- either actual `test-fixture` assembly provenance or `production` provenance.

Fixture provenance requires the real fixture assembler and profiler identities.
Production provenance requires the orchestrator and profiler identities,
`bindings/<package-key>/<language>` source directory, and the matching target
release tag. The latter branch is defined for Phase 5; Phase 4 did not fabricate
production provenance or implement the production orchestrator.

Schema validity alone cannot prove these equalities. Assembly and both
profilers check agreement with the native dependency graph, normalized model,
packaged extension definitions, and dependency metadata. Source-byte identity
and transport evidence remain outside the semantic model.

### 3.3 Established the real core-schema prerequisite

Early conformance models contained a placeholder core-schema digest, and some
structural examples omitted the interface required by the current core draft.
Those inputs could neither establish core validation nor produce accepted
complete profiles.

The approved
[core schema](/Users/colacy/code/github.com/runtimeconditions/spec/schema/runtimeconditions.profile.v0.2.0.schema.yaml)
is now the pinned contract used by fixture assembly and both profilers:

| Identity | Value |
| --- | --- |
| Schema ID | `https://runtimeconditions.io/schemas/profile/0.2.0/runtimeconditions.profile.schema.yaml` |
| Version | `0.1.0` |
| Source-byte SHA-256 | `342bf20bce479f5012b9fc2c6238dc1fb0935e327ecb0fbca6e647362563c73c` |
| Semantic SHA-256 | `a090a8016d045f9c3fa872a67f8df293b77ca2809a1bea5ae9fa31a27a06109a` |

The shared test configuration and expected normalized models reference this
real identity. Go carries approved schema bytes in the installed binary through
its build process; Python carries them as profiler-wheel package data. Neither
installed profiler reads the `spec` checkout.

Normalizer unit checkpoints still use a deliberately synthetic normalizer
executable digest to remain independent of platform-specific binaries. That
unit-test convention is separate from the core-schema identity. Installable
fixtures are normalized afresh and record the actual executed normalizer's
digest. They contain no placeholder core identity or invented release provenance.

### 3.4 Corrected conformance inputs and general emitter defects

- Cases 08 and 09 gained the required interface representation. Cases 06, 07,
  10, and 11 were then corrected so their closed extension schemas admit the
  required core `kind` and `interface.type` fields. Their original recursive,
  alternative, collection, value-domain, and naming constraints remain.
- Python naming collisions exposed missing canonical parent context. The
  allocator now includes the `interface` parent where needed.
- Case 08 also exposed an ambiguous Python manifest lookup: a field named
  `target` could replace the interface type associated with the same source
  token. Root bindings now use the property's already allocated symbol and
  exact structural identity. The correction was reviewed with the complete
  Python suite before approval. A negative test now selects its enum by name
  instead of relying on the old property position.
- Go schema-derived root field types in cases 06–09 initially lacked the marker
  methods and manifest `implements` entries needed by declaration functions.
  The general emitter now derives those declaration contracts from the model
  and root bindings. Native method-set checks and manifest assertions agree.
- Go conformance samples now satisfy valid numeric domains, construct required
  recursive values, and supply one optional object alternative at a time.
- Declaration-only packages retain native function-reference coverage. Complete
  calls are supplied by consumer workloads with installed interface/field
  providers. A positive case is never counted as accepted merely because its
  incomplete declaration was correctly rejected.

These were corrections to shared conformance fixtures and general algorithms.
They were not changes to real catalog extension YAML or case-specific production
branches.

## 4. Installed resource contract

Every generated package must install these four resources together:

| Resource | Purpose |
| --- | --- |
| `runtimeconditions.bindings.yaml` | Native-to-model structural mapping. |
| `runtimeconditions.binding-model.yaml` | Exact semantic model consumed by the emitter, with core and normalizer identities. |
| `runtimeconditions.extension.yaml` | The validated root extension definition. Dependencies supply their own packaged definitions. |
| `runtimeconditions.binding-release.yaml` | Source lock, dependency artifact identities, and assembly/verification provenance. |

For Go they are beside the declaration source in the package directory resolved
through Go tooling. For Python they are package data inside the installed
generated import package, owned by its installed distribution.

Python's profiler locates third-party resources through distribution file
metadata without importing the binding package. Calling
`importlib.resources.files()` on an arbitrary binding-package name can execute
its initialization; the static discovery implementation deliberately avoids
that. Reading the profiler's own trusted bundled resources is a separate use of
`importlib.resources`.

Resource discovery must remain tied to the imported package and native package
manager. Recursive searches of module caches, site-packages, sibling repositories,
or catalogs are not part of the installed profiling workflow.

## 5. Go profiler production-oriented implementation

The [Go profiler interoperability plan](/Users/colacy/code/github.com/runtimeconditions/go-rc-profiler/GENERATED-BINDINGS-INTEROP-PLAN.md)
and [conformance record](/Users/colacy/code/github.com/runtimeconditions/go-rc-profiler/CONFORMANCE.md)
document the separate repository's work. The implemented path includes:

1. Native Go source loading and type information, with package-qualified
   declaration recognition rather than guessed names or SDK mappings.
2. Go module download/verification and package resolution, followed by reads of
   only the fixed package-local resources.
3. Strict resource-schema validation; package, model, extension, lock, source,
   dependency-version, marker, and native-reference consistency checks.
4. Structural value extraction for the generated API: objects, scalars,
   constants, arrays, maps, union representations, recursive structures,
   pointers, zero values, and optional omission.
5. Direct vocabulary contributor selection and complete installed extension
   closure validation against the bundled core and preserved extension schemas.
6. Deterministic YAML output and an atomic file write after validation succeeds.
   Unsupported expressions, unresolved values, or invalid declarations fail
   instead of becoming guessed accepted Conditions.

Useful implementation entry points are
[the CLI](/Users/colacy/code/github.com/runtimeconditions/go-rc-profiler/main.go),
[generated extraction](/Users/colacy/code/github.com/runtimeconditions/go-rc-profiler/extractor/generated.go),
[native value reading](/Users/colacy/code/github.com/runtimeconditions/go-rc-profiler/extractor/generated_values.go),
[installed package verification](/Users/colacy/code/github.com/runtimeconditions/go-rc-profiler/extensioncheck/imported.go),
[release checks](/Users/colacy/code/github.com/runtimeconditions/go-rc-profiler/extensioncheck/release.go),
and [profile finalization](/Users/colacy/code/github.com/runtimeconditions/go-rc-profiler/extensioncheck/generated_profile.go).
Structural extraction is implemented; the earlier stub is not the current state.

The final schema-only dependency fixture exposed one additional resolver defect.
Its dependency was selected by the Go module graph but not imported by source.
The original resolver tried to list it from the workload in a way that required
a `go.mod` edit. The repair uses Go's selected module build list and inspects
the resolved dependency artifact. `TestInstalledValidationOnlyDependency`
preserves that regression. A dependency does not need a source import to
participate in semantic validation.

The Go release build must include the approved core bytes. The existing
[build script](/Users/colacy/code/github.com/runtimeconditions/go-rc-profiler/scripts/build-with-core-schema.sh)
checks their source digest and supplies them through linker configuration.
That is producer-side preparation. End users receive the completed executable
and need no schema checkout. A plain build/install that omits this preparation
must not be advertised as the accepted production artifact.

The profiler repository declares Go 1.25.0; its acceptance ran with Go 1.26.5
on macOS arm64. Generated binding source was separately checked at Go 1.22.12.
Those are different support statements. The installed profiler uses Go tooling
to resolve and inspect the workload; an appropriate `go` toolchain must remain
available in the end-user or CI environment.

## 6. Python profiler production-oriented implementation

The [Python interoperability plan](/Users/colacy/code/github.com/runtimeconditions/python-rc-profiler/GENERATED-BINDINGS-INTEROP-PLAN.md)
and [conformance record](/Users/colacy/code/github.com/runtimeconditions/python-rc-profiler/CONFORMANCE.md)
document the separate repository's work. The implemented path includes:

1. AST discovery of imported binding symbols and installed distribution ownership.
2. Exact package-data lookup through `importlib.metadata`, including missing or
   misplaced resources, ambiguous ownership, and workload-shadowing rejection.
3. Strict YAML parsing, resource limits, bundled schema validation, canonical
   digests, installed file-record hashes, package identities, exact closure
   edges, compatible dependency intervals, and imported marker verification.
4. Static extraction of generated declaration functions, dataclass-style object
   construction, wrapper objects, scoped enum members, sequences, maps, unions,
   recursive shapes, and omitted/nullable fields. Application and binding
   imports are not evaluated.
5. Core validation, direct vocabulary contributors, complete closure validation,
   and all applicable exact extension schemas. Unsupported dynamic authoring,
   invalid enum representations, and values outside the binding structure fail.
6. Complete deterministic profile serialization and atomic output after all
   validation succeeds.

Implementation entry points are
[the installed CLI](/Users/colacy/code/github.com/runtimeconditions/python-rc-profiler/runtimeconditions_profiler/cli.py),
[static installed discovery](/Users/colacy/code/github.com/runtimeconditions/python-rc-profiler/runtimeconditions_profiler/project/installed.py),
[artifact verification](/Users/colacy/code/github.com/runtimeconditions/python-rc-profiler/runtimeconditions_profiler/project/verify.py),
[generated extraction](/Users/colacy/code/github.com/runtimeconditions/python-rc-profiler/runtimeconditions_profiler/profile/generated.py),
and [semantic validation](/Users/colacy/code/github.com/runtimeconditions/python-rc-profiler/runtimeconditions_profiler/profile/semantic.py).

The wheel supplies the console script, the approved core YAML, and the binding
contract-schema bundle. The generated-binding commands are
`runtimeconditions-python-profiler profile verify-bindings` and
`runtimeconditions-python-profiler profile generate`.

The CLI must run in the Python environment containing the workload's binding
distributions. An isolated CLI environment without those dependencies does not
meet that discovery contract. Python 3.11 or newer is declared; the final installed
CLI acceptance used Python 3.12.10. Binding generation and its minimum-version
checks used Python 3.11.16. The profiler regression run recorded 86 passing tests;
the final Python **emitter** suite recorded 98. These are separate suites.

Python installation normally does not retain the original wheel archive.
Acceptance hashes the wheel before installation, while the profiler checks
installed RECORD entries and resource/model/extension digests. Phase 5 must
retain this distinction when verifying archives and installed packages; an
installed RECORD check is not a recomputed original wheel-archive digest.

## 7. What the end-user workflow now supports

An end user installs a released profiler and the desired generated binding
dependencies through normal language dependency management, writes static
declarations in their workload, and invokes the installed profiler. The
generated dependencies carry the four fixed resources and their closure.

For a Go workload, the implemented command shape is:

```sh
go-rc-profiler generate \
  -dir /path/to/workload \
  -name checkout-service \
  -workload-uri https://example.test/checkout-service \
  -workload-version 1.0.0 \
  -out /path/to/output/runtimeconditions.profile.yaml
```

For Python, invoke these in the environment containing the binding dependencies:

```sh
runtimeconditions-python-profiler profile verify-bindings \
  --project /path/to/workload

runtimeconditions-python-profiler profile generate \
  --project /path/to/workload \
  --name checkout-service \
  --workload-uri https://example.test/checkout-service \
  --workload-version 1.0.0 \
  --out /path/to/output/runtimeconditions.profile.yaml
```

These commands require workload paths, not paths into the extension catalog.
Omitting the output option writes the accepted profile to stdout. A binding-only
verification command does not replace generation and complete profile validation.
The examples describe implemented CLI behavior; they do not assert that the
tested local artifacts are available from a public registry.

### URI resolution and remote delivery

Extension URI identity and language-package delivery are related but distinct.
The current generated-binding profiler paths resolve extension identities and
definitions from the already verified installed binding graph. They do not
provide a general command to fetch arbitrary extension YAML from a supplied URI.
Their code and acceptance evidence must not be described as proving such a
network resolver.

The shared resolver/normalizer already owns source resolution. Phase 5 must
expose its supported network-backed mechanisms through production orchestration,
resolve the root and complete closure by their globally resolvable IDs, lock the
exact inputs, and package those definitions for end-user profiling. A valid
installed closure therefore permits profiling without another extension fetch;
the upstream package producer may have obtained its YAML remotely or from a
validated cache. Native package managers retrieve the binding dependencies.

The current identity authority is the sixth draft and implementation standard:
absolute extension URI IDs are opaque identities, and their schemes determine
resolution. Do not revive the fifth draft's last-colon version splitting or an
HTTP-only restriction in these contracts. The conformance corpus's URNs identify
test inputs resolved through its catalog; they are not evidence of public
HTTPS/OCI extension hosting. No end-user checkout is implied by the producer's
catalog fixtures or builder-side repository access.

## 8. Fixtures, semantic oracles, and complete coverage

The limited
[fixture assembler](/Users/colacy/code/github.com/runtimeconditions/extensions/tooling/extension-bindings/fixtures/assemble.py)
was brought forward to unblock installed integration. It emits **14 Go module
archives and 14 Python wheels**, in dependency order, from ten positive root
cases and their required dependency packages. It adds the exact four resources
at their final locations, checks their schemas and identities, and records real
normalizer, assembler, profiler, extension-source, and dependency-archive hashes.

Its package order, target conversion, output paths, and local distribution are
test infrastructure. It does not implement a production catalog, generic build
planning, SemVer classification, committed output synchronization, promotion,
or publication. Phase 5 replaces it for production targets while retaining the
corpus for regression tests.

The reviewed
[conformance inventory](/Users/colacy/code/github.com/runtimeconditions/extensions/tooling/extension-bindings/fixtures/conformance.yaml)
is the independent semantic oracle. The
[consumer preparer](/Users/colacy/code/github.com/runtimeconditions/extensions/tooling/extension-bindings/fixtures/prepare_results.py)
materializes complete workloads, expected profile YAML, exact expected errors,
installation commands, direct contributors, dependency closures, and input hashes.
It validates those expectations against the core and applicable closure schemas;
it never derives expected values from profiler output or invokes a profiler.

The preparer is a Python orchestration script containing a generated Go helper.
That helper uses native Go AST handling/formatting, YAML serialization, and schema
validation to prepare fixture source and expectations. It is not a profiler and
does not perform installed-package discovery or Condition extraction. Its
language mix is confined to test preparation.

| Case | Coverage and final disposition |
| --- | --- |
| 01 | Owned kind, interface, field, and complete declaration. |
| 02 | Owned declaration plus direct additive package. |
| 03 | Leaf declaration, middle interface, root field, transitive ownership, and complete consumer calls. |
| 04 | Exact normalizer rejection of a dependency cycle; no package/model output. |
| 05 | Exact normalizer rejection of vocabulary ownership conflict; no package/model output. |
| 06 | Recursive local reference and required nested values. |
| 07 | Both valid object alternatives; missing and simultaneous alternatives rejected. |
| 08 | Object and scalar union branches; incomplete object branch rejected. |
| 09 | Arrays, object items, maps, both allowed numeric values; invalid value rejected. |
| 10 | Scoped domains, normalized-name collisions, all reviewed enum members, optional omission; invalid scoped value rejected. |
| 11 | Exact serialized source names, including separators, digits, and Unicode. |
| 12 | Exact normalizer rejection of an unsupported structure-changing keyword; no package/model output. |
| 13 | Schema-only dependency validated without appearing among direct contributors. |

Each language has **19 positive and six negative workload declarations**.
The inventory accounts for all **11 emitted Go calls plus one completed deferred
declaration**, and **13 emitted Python calls plus two completed deferred
declarations**. Additional positive workloads cover consumers, branch choices,
and values; emitted-call counts and workload counts are intentionally different.

Failure stages are recorded per language. For example, Go rejects the incomplete
union object structurally, while Python's reviewed failure is at the exact
`oneOf` schema. Both reject an invalid scoped value at vocabulary validation.
Diagnostics name the recorded coordinate/pointers and match after only the
documented workload-path normalization. Native type rejection is not substituted
for proof of a semantic schema failure.

### The final missing case: a schema-only dependency

The original 18-positive/five-negative corpus had direct contributors equal to
the full closure in every workload. It could not demonstrate omission of a
dependency that still constrained the Condition.

[Case 13](/Users/colacy/code/github.com/runtimeconditions/extensions/tooling/extension-bindings/model/conformance/cases/13-dependency-schema-only/root.yaml)
owns the `job` kind, `process` interface, and `command` field in its root.
Its dependency owns no vocabulary and supplies the applicable `command-limit`
schema with `maxLength: 6`. The root does not duplicate that limit.

- `dependency-schema-only` emits the root extension ID alone while validating
  both installed extensions.
- `dependency-schema-invalid` uses `too-long`. Core and root schemas accept
  that Condition; only the dependency's schema rejects it, at
  `urn:runtimeconditions:conformance:dependency-schema-only:dependency#schema:command-limit`.
  There is no accepted output.

Case 13 is registered in the normalizer and Go emitter suites. Go assembles its
dependency before the root. Its
[expected normalized checkpoint](/Users/colacy/code/github.com/runtimeconditions/extensions/tooling/extension-bindings/model/conformance/expected/13-dependency-schema-only/runtimeconditions.binding-model.yaml)
matches the installed model except for unit-test normalizer provenance and the
resulting model digest. Python discovers the checkpoint automatically. Existing
100-run normalization and 100-permutation checks now include it.

## 9. Acceptance runs and provenance repair

The full installed tests are distinct from fixture preparation and emitter
tests. Go downloads modules into isolated native caches, then runs the installed
profiler offline with source-checkout reads denied. Workloads contain panic
sentinels, and a separate derived binding artifact contains an initialization
panic; successful profiling proves those code paths were not executed.

Python installs the profiler and binding distributions by name through a mock
HTTP package index into isolated virtual environments. Workload and binding
execution sentinels, installed-resource checks, and adversarial packages exercise
the actual console entry points. The mock index/proxy substitutes dependency
distribution; the extraction and validation engines are the real profilers.
This establishes native installed-package behavior without production publication.

| Verification | Final result |
| --- | --- |
| Go installed official corpus | 19 exact repeatable profiles; six exact rejections; 14 packages; complete emitted/deferred coverage; three normalizer negatives. |
| Python installed official corpus | 19 exact repeatable profiles and six exact rejections across 14 distributions. |
| Python supplemental corpus | 29 checks: 16 expected profiles and 13 expected rejections for missing/altered resources, wrong identities, missing dependencies, invalid static authoring, and execution boundaries. |
| Python installed total | 54/54, zero failed and zero pending. |
| Native regression suites | Normalizer passes all 13 cases; Go emitter/profiler suites and static checks pass; final Python emitter suite passes 98 tests. |
| Shared package checks | All 28 packages pass model/manifest schemas, native metadata, formatting, minimum-version compilation, static analysis, structural/API coverage, three-run source determinism, and exact archive resource/file-set checks. |

Go's resolver repair changed its binary. The v3 fixtures still recorded the
original `46af7aa5…` profiler hash while the passing binary was `b2e2aee5…`.
That behavior-only run was correctly kept at
`behavior-passed-provenance-pending`.

The official v4 assembly was rebuilt with the repaired binary, then the entire
25-workload installed run was repeated and saved. **All 14 Go releases now
record the exact hash of the executed profiler.** Only Go release metadata and
the dependent Go archive locks changed; the other generated Go files were
unchanged.

All 14 Python wheels were byte-identical to the previously passing assembly.
The Python agent independently verified those wheels, all 56 fixed resources,
all 50 workload files, all 25 expectation files, and the inventory after replacing
only assembly/result paths. Its 54/54 evidence therefore applies to v4 without
re-running identical artifacts. Earlier runs remain preserved.

The final registration changes touched tests and the new checkpoint. They
changed no production source or assembled package bytes. A fresh shared gate
run passed, closing the remaining normalizer inventory failure and gate 7.

### Recorded profiler artifact identities

| Artifact | Accepted identity |
| --- | --- |
| Go profiler executable | SHA-256 `b2e2aee5e10af47a0e75998d3ac591ee17f2887aaaddeae46405296763bc1444` |
| Python profiler wheel, version 0.1.0 | SHA-256 `1a845ba0edd27eb02d877b7acd001878cf5b587b3fba36143eb7b6dfd19abbf6` |

These identify local acceptance artifacts. The Go build information records a
modified working tree. Do not treat these hashes as evidence that released
toolchain artifacts already exist, or copy them into a production release lock
while claiming they identify released tools.

### Runtime and platform qualification

| Check | Environment |
| --- | --- |
| Go installed CLI and native Go suites | Go 1.26.5, macOS arm64. |
| Minimum Go generated-source checks | Go 1.22.12 compilation, conformance-test compilation, and `go vet`. The produced test binaries were not executed. |
| Python binding minimum checks and final emitter suite | Python 3.11.16 with pinned build/format/type/test tools. |
| Python installed profiler acceptance | Python 3.12.10. |

Cross-platform installed acceptance, Go minimum-toolchain test execution, and
Python installed-CLI acceptance on every declared supported interpreter are not
established by this local run. Release qualification must retain that distinction
instead of inheriting broader support claims from the declared minimum versions.

## 10. Evidence and how to continue verification

| Evidence | Location |
| --- | --- |
| Human gate review | [PHASE-4.md](/Users/colacy/code/github.com/runtimeconditions/extensions/tooling/extension-bindings/PHASE-4.md) |
| Final consolidated YAML, identities, hashes, and accepted scope | [consolidated-review.yaml](/Users/colacy/code/github.com/runtimeconditions/phase4-dependency-schema-review-20261004/consolidated-review.yaml) |
| Final shared commands, outputs, package/archive hashes | [evidence-final/gates.yaml](/Users/colacy/code/github.com/runtimeconditions/phase4-dependency-schema-review-20261004/evidence-final/gates.yaml) |
| Current complete assembly | [v4 fixture tree](/Users/colacy/code/github.com/runtimeconditions/phase4-dependency-schema-fixtures-v4-20261004) |
| Current workloads, oracles, commands, and inventory | [v4 inventory](/Users/colacy/code/github.com/runtimeconditions/phase4-dependency-schema-results-v4-20261004/inventory.yaml) |
| Go matching-provenance installed run | [result.yaml](/Users/colacy/code/github.com/runtimeconditions/go-rc-profiler/conformance/phase4-go-matched-provenance-2026-10-04/result.yaml) and [regression checks](/Users/colacy/code/github.com/runtimeconditions/go-rc-profiler/conformance/phase4-go-matched-provenance-2026-10-04/checks.yaml) |
| Python complete installed run | [summary.yaml](/Users/colacy/code/github.com/runtimeconditions/python-profiler-dependency-schema-acceptance-final-20261004/summary.yaml) |
| Python durable inputs/results archive | [acceptance bundle](/Users/colacy/code/github.com/runtimeconditions/python-profiler-dependency-schema-acceptance-bundle-20261004.tar.gz) |
| Independent Python v4 comparison | [python-v4-equivalence.yaml](/Users/colacy/code/github.com/runtimeconditions/phase4-dependency-schema-review-20261004/python-v4-equivalence.yaml) |
| Assembly comparison | [v4-provenance.yaml](/Users/colacy/code/github.com/runtimeconditions/phase4-dependency-schema-review-20261004/v4-provenance.yaml) |

The primary final review and summaries are YAML. Historical detailed Python
results and earlier Go bundles include JSON evidence; those preserved files do
not establish an alternative production interchange format. New Runtime
Conditions manifests, checkpoints, indexes, and verification evidence must
follow the standard's YAML rule.

The assembly, results, runners, and some artifacts are outside the `extensions`
repository. Local paths, especially `/private/tmp` executables and virtual
environments, must not be assumed to exist on another host. Preserve or transfer
the evidence bundles and rebuild inputs when needed. Early failed assembly
attempts and the v3 Go provenance-pending run are not replacements for the
accepted v4 inputs.

For a fresh producer-side replay, follow the pinned dependencies and commands in
[the fixture README](/Users/colacy/code/github.com/runtimeconditions/extensions/tooling/extension-bindings/fixtures/README.md):

```sh
python3 tooling/extension-bindings/fixtures/assemble.py \
  --python /path/to/pinned-fixture-python \
  --go-profiler /path/to/schema-equipped-go-profiler \
  --python-profiler-wheel /path/to/runtimeconditions_profiler-0.1.0-py3-none-any.whl \
  --output /path/to/new-fixtures

python3 tooling/extension-bindings/fixtures/prepare_results.py \
  --fixtures /path/to/new-fixtures \
  --output /path/to/new-results
```

Run these from the builder's `extensions` checkout, using new output directories.
That checkout is a build/test input and must stay outside profiler runtime inputs.
The installed Go replay runner is
[run.py](/Users/colacy/code/github.com/runtimeconditions/go-rc-profiler/conformance/phase4-go-matched-provenance-2026-10-04/run.py);
the Python runner is
[run_phase4_acceptance.py](/Users/colacy/code/github.com/runtimeconditions/python-profiler-cli-acceptance/run_phase4_acceptance.py).
Their recorded invocations and inventories give the exact commands, environment,
resource hashes, and required oracles. The
[shared gate runner](/Users/colacy/code/github.com/runtimeconditions/phase4-dependency-schema-review-20261004/run_gates.py)
uses a separate source snapshot and compiles copies of package source; it runs
no profiler or application source.

If a profiler changes, build its actual artifact first, assemble new fixtures
with that identity, prepare new consumers, and repeat the installed acceptance.
Matching behavior against packages that record another profiler hash is not a
completed provenance check. Reuse prior evidence only after recording exact
artifact and workload/expectation equivalence.

## 11. Starting state and required work for Phase 5

### 11.1 What exists and what is still absent

| Component | Starting state |
| --- | --- |
| Shared semantic resolver and normalizer | Implemented in Go; preserve their model/lock separation and resolution rules. |
| Go and Python emitters | Implemented with strict structural manifests and accepted conformance coverage. |
| Shared binding-release schema | Implemented, with distinct fixture and production branches. |
| Separately installed profilers | Implemented and accepted for the generated-binding workflow above. |
| Fixture assembly and semantic oracles | Implemented as test infrastructure; reusable as regression inputs. |
| `packages.yaml` | Not present. Production configuration must be introduced. |
| Production `orchestrator/` | Not present. Its Go implementation and `rc bindings` integration are Phase 5 work. |
| Package-catalog, toolchain-lock, and file-manifest schemas listed in Section 4 | Not present in `model/`; create them for the production contracts. |
| `toolchain.lock.yaml` | Exists with `status: phase3-development` and pinned Python build tools. It is not the required complete released-tool/profiler lock. |
| Committed `bindings/<package-key>/<language>` trees | Not delivered by Phase 4. Test package targets are not the production catalog. |
| Production release/promotion/registry workflows | Outside this phase; follow Phases 6 and 7 and their approval boundaries. |

### 11.2 Production identity and schema-distribution prerequisites

Before recording production provenance:

1. Release the required tooling and schema-equipped profilers, then pin their
   real versions and artifact SHA-256 values. Do not use workspace binaries
   while claiming released-toolchain provenance.
2. Retain the approved core schema bytes and identity in profiler releases.
   Make the Go build's core-schema preparation part of its release process.
3. Make contract-schema distribution and drift checks repeatable for the
   independently released profilers. Go currently embeds checked-in YAML copies;
   Python ships a JSON representation bundle as a profiler implementation asset.
   At this report date, all four Go copies byte-match the authoritative schemas
   and the Python representations match semantically. Synchronization is still
   manual; [FUTURE_WORK.md](/Users/colacy/code/github.com/runtimeconditions/extensions/tooling/extension-bindings/FUTURE_WORK.md)
   records the follow-up. Installed validation must remain self-contained.
4. Reconcile emitter artifact-digest recording with Section 2 before production
   release. The current strict manifest's `generated` object records emitter
   name/version but has no emitter artifact-digest field. The standard requires
   that digest. This is an observed production-contract gap, not an implemented
   capability. Adding an unknown field to strict v1alpha2 is not compatible;
   review the versioned schema, emitter, profiler, and migration implications
   before changing the contract. This report makes no such change.
5. Keep emitter diagnostic-code standardization visible. The current Python
   `RCP` codes and Go `RCG` codes are tested, but their cross-language ownership
   and stability policy remain documented follow-up work. Do not silently
   reinterpret matching numbers as matching semantics.

### 11.3 Recommended implementation order

1. **Read the authority and define the edit scope.** Start with implementation
   Sections 2–7, 10–15, 17–20, this report, the current schemas, and accepted
   evidence. Obtain Section 4 approval for the exact Phase 5 file list.
2. **Define and validate production configuration.** Introduce `packages.yaml`
   and its schema, the complete toolchain lock and its schema, and the
   file-manifest schema. Package configuration owns delivery metadata only;
   it must not duplicate extension vocabulary, schemas, or dependency semantics.
3. **Implement Go orchestration behind the global `rc` CLI.** The required
   commands are `rc bindings resolve`, `normalize`, `generate`, `verify`,
   `package`, `plan-release`, `check`, and `update`. These are Phase 5 command
   contracts, not commands implemented by the fixture assembler.
4. **Plan the real dependency graph.** Resolve configured root IDs and closures,
   validate exact locks, map extension dependencies to configured language
   targets, and build each language's package dependencies before dependents.
   Include schema-only dependencies that have no source import or vocabulary
   contribution. Do not carry forward the fixture assembler's hardcoded case list.
5. **Generate and assemble production trees.** Reuse the normalizer and native
   emitters. Add the exact model, validated root definition, production release
   metadata, and deterministic file manifest. Preserve the emitter's source,
   package metadata, structural manifest, conformance source, and expected
   profile values. Do not introduce source-resolution data into the semantic
   model or source headers.
6. **Verify through installed native packages.** Preserve gates 1–14 and 16 and
   implement gate 15. Package and install artifacts into isolated consumer
   environments, invoke the released locked profilers, compare complete exact
   profiles and required failures, and check all file digests and archive entries.
   Source-based tests alone are insufficient for this boundary.
7. **Implement compatibility and build planning.** Derive native API surfaces
   through ASTs; compare prior release models, behavior manifests, direct
   dependencies, and language versions. Apply Section 14's SemVer rules and
   treat unprovable schema compatibility as breaking. Resolve previous releases
   automatically. Handle ecosystem major-version coordinate changes and
   regeneration of dependents.
8. **Implement committed-output operations.** Generate every configured target
   regardless of publication mode under `bindings/<package-key>/<language>`.
   `check` compares without changing committed source; `update` atomically
   replaces only selected target trees and removes stale generated files.
   `package` must reproduce and verify the committed tree, then package those
   exact bytes. Archives and compiled outputs are not committed source.
9. **Apply Section 15's filesystem contract.** Default cache/work behavior leaves
   no persistent intermediate tree. Explicit caches are content-addressed and
   every reused object is digest-checked. Tool-required files use invocation-local
   temporary directories outside generated targets, cleaned on return. Host
   paths and timestamps must not affect persisted output. Emit YAML machine
   summaries and concise human summaries.
10. **Prove Phase 5 exit criteria.** Perform a clean full build from an empty work
    directory, repeat it with zero generated-source diff, prove packaged-source
    equality with committed trees, verify dependency order and exact archive
    contents, and execute supported network-backed resolution locally. All 16
    gates must pass for every target.

The production tag must match
`bindings/<package-key>/<language>/v<major>.<minor>.<patch>` and the committed
source directory. Native package source metadata, production release metadata,
and later promotion must agree on that source and release identity.

Phase 5 must keep the complete positive and negative corpus, schema-only
dependency case, exact serialized names, marker contracts, no-execution checks,
and provenance equality as regressions. It must also establish the support
matrix appropriate to the released profilers rather than expanding the local
acceptance claims in this report by assumption.

## 12. Final handoff conclusion

Phase 4 supplied the missing structural and release contracts, real core-schema
integration, complete installable fixtures and independent profile oracles,
general emitter corrections, and functioning separately installed Go and Python
profilers. The final matching-provenance Go run, complete Python run and artifact
equivalence review, and passing shared regression/package gates are preserved.

Phase 5 can now implement production orchestration using this accepted
integration boundary. Its remaining work is concrete: production configuration
and locked release identities, contract/release prerequisites above, dependency
planning, deterministic assembly and file manifests, installed verification,
version classification, committed output synchronization, and all 16 gates.
It must preserve the end-user experience already demonstrated: install normal
dependencies and a self-contained profiler, profile static workload source,
receive a completely validated profile, and require no source checkout of this
repository ecosystem.
