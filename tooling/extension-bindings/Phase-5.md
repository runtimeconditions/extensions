# Phase 5: binding generation and delivery foundation

Report date: 2026-10-07.

The shared binding-generation mechanism is implemented for Go and Python. It
successfully generated and verified bindings for `env-configuration` and its
dependency, `common-integrations`. Future extensions enter through their
definitions and package delivery configuration; extension identifiers and
vocabulary never select implementation code paths.

## 1. Deliberate limits on complexity

Early decisions deliberately avoided over-engineering: one shared pipeline,
Go's standard `flag` package, existing native package builders, and independently
installed profilers. Cobra and a broader CLI redesign were deferred. Work stayed
inside `extensions/`; the profiler repositories remained independent.

We deliberately avoided writing **1,448 redundant conformance files** merely to
repeat the same proofs across existing extensions. No such corpus was added to
the repository. We retained 13 synthetic cases, six authored profiler consumers
across both languages, and focused regression tests. Bugs exposed by new
real-world cases become small, shared tests.

Production package output contains native source, package metadata, four YAML
resources, and a file manifest. Generated conformance trees, expected-profile
files, and Python `_conformance.py` are excluded. Scratch builds and local
previews live in the ignored `phase5-work/` directory. The abandoned bulk file
plan and stray test output under `bindings/` were removed.

## 2. Configuration and provenance contracts

- Added generic schemas for the package catalog, toolchain lock, and file
  manifest. `packages.yaml` contains delivery metadata only; definitions and
  normalized models supply extension meaning and dependencies.
- Added the emitter SHA-256 identity to both binding emitters' manifests.
  Verification rejects missing, malformed, or mismatched emitter digests and
  checks agreement among source, model, package, and tool identities.
- Kept fixture provenance separate from the release assembly contract.
  Development uses workspace tools and records their actual identities.
- Removed normalizer calls to deleted profiler identity helpers, restoring
  ordinary Go builds without profiler source coupling or temporary overlays.

## 3. Eight commands over one pipeline

The Go orchestrator implements all eight `rc bindings` commands:

| Command | Purpose |
| --- | --- |
| `resolve` | Resolve and validate the extension closure and dependency lock. |
| `normalize` | Produce the canonical binding model. |
| `generate` | Generate selected package trees into a separate output directory. |
| `verify` | Check package structure, mappings, identities, native tooling, and reproducibility. |
| `package` | Compare regeneration with committed source, then verify and package that source. |
| `plan-release` | Compare prior releases, schemas, native APIs, behavior, and dependencies for version planning. |
| `check` | Regenerate, verify, and detect committed-source drift. |
| `update` | Verify regeneration and atomically synchronize selected source trees, removing stale files. |

Dependency planning derives edges from resolved extension closures, including
schema-only dependencies. Providers build first. Assembly adds the extension
definition, binding model, binding manifest, release metadata, and file manifest;
existing native builders produce deterministic archives. Temporary intermediates
are cleaned unless explicitly retained.

## 4. Shared fixes discovered during implementation

- Combined alternative string constants at the same field path into one value
  domain, eliminating normalizer union collisions.
- Allowed configured Go versions from 1.22 and Python versions from 3.11;
  verification still requires the catalog's exact compiler or interpreter.
- Fixed Python's unconstrained array-item lookup so it preserves the parent
  array entry.
- Fixed duplicate Python class emission for already-expanded definitions while
  preserving recursive references and schema-only packages.
- Made Python condition fields use combined scope projections. Fields supplied
  by shared schemas, including `name`, `required`, and `sensitive`, now appear
  alongside scoped property domains. Manifest mappings use those complete
  projections too.

The last two fixes gained synthetic regression tests. Existing mutated test
models were updated to keep schema and scope projections consistent.

## 5. Successful generation for two extensions

The first reviewed case uses `env-configuration:v1alpha1`, whose declared
dependency is `common-integrations:v1alpha1`. Both received Go and Python catalog
entries. Dependency requirements were derived automatically in both languages.

| Extension | Go files | Python files |
| --- | ---: | ---: |
| `common-integrations` | 7 | 10 |
| `env-configuration` | 7 | 10 |
| **Total** | **14** | **20** |

The **34 package files** are local development previews under
`phase5-work/previews/env-configuration-review/bindings/`. This case exercises
dependency ownership, recursive schemas, configuration alternatives, and scoped
value domains through the same generic pipeline.

## 6. Verification recorded

- Go normalizer, emitter, CLI, and orchestrator suites passed during this work.
- The latest Python suite passed **118 tests**; strict type checking passed for
  all nine emitter source modules.
- All four preview targets passed package verification: dependency installation,
  native formatting/build/static analysis, exact model mappings, provenance,
  file digests, archive inventory, and deterministic regeneration.
- Synthetic consumers cover profile extraction and rejection of constraints
  supplied by schema-only dependencies. Release/network cases use mocked inputs
  or local test servers.

Per-package reports identify `scope: package-structure`. Gates 7–11 are marked
`not-applicable`; their mechanism coverage belongs to the shared tooling suite.

## 7. Agreed delivery path

Local previews provide the output review. The next automation work is:

1. A manually triggered GitHub Actions workflow generates bindings, commits them
   to a bot branch, and opens a review PR.
2. Read-only PR checks compare regeneration with committed source and verify the
   reviewed packages.
3. After merge, Actions builds packages from that commit. Protected approval
   promotes the exact built artifacts without rebuilding them after approval.

These workflows are planned, not implemented here. The current toolchain lock
remains a development lock; production pinning requires actual released tools
and artifact digests. PyPI organization approval remains pending, and the Python
profiler has not been published to PyPI. Registry publication remains separate
from this local generation result.
