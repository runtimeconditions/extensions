# Phase 4: structural manifests and installed profiler acceptance

Review date: 2026-10-04. Status: accepted within the approved local verification scope.

This review covers the approved structural and release manifests, test-only
package assembly, Go and Python binding generation, and the separately installed
language profilers. It includes the added dependency-only schema case. It makes
no claim about production orchestration, publication, or distribution.

## Scope and evidence

- Approved criteria: [IMPLEMENTATION.md](IMPLEMENTATION.md), Sections 11–13
  and Phase 4. The document and its guard remain unchanged.
- Shared fixtures: [fixtures/README.md](fixtures/README.md),
  [fixtures/conformance.yaml](fixtures/conformance.yaml), and
  [model/conformance/cases/13-dependency-schema-only/](model/conformance/cases/13-dependency-schema-only/).
- The root of case 13 owns all vocabulary used by its declaration. Its
  dependency owns no vocabulary and supplies the sole `maxLength: 6` constraint.
  The positive profile must omit that dependency; the negative must fail its
  schema without writing a profile.
- `spec/` is read-only throughout this work.

## Applicable gate review

Phase 4 requires gates 1–14 and 16. Gate 15 belongs to production file-manifest
orchestration in Phase 5. The shared checks cover all 14 packages per language,
including the schema-only dependency package with no declaration calls.

| Gate | Current result | Evidence |
| --- | --- | --- |
| 1: model schema | Pass | All 28 assembled model resources validate; preparation also checks semantic digests and identity agreement. |
| 2: strict manifest schema | Pass | All 28 manifests validate against v1alpha2; Go regression checks reject provisional inventories and stale identities. |
| 3: native package metadata | Pass | Every Go module parses through Go tooling; every Python project and wheel metadata parses and matches its package identity. |
| 4: native formatting | Pass | `gofmt -d` and pinned `ruff format --check` report no differences for all packages. |
| 5: minimum-version compilation | Pass | All Go binding and conformance sources compile with Go 1.22.12; every Python source compiles without execution under Python 3.11.16. |
| 6: static analysis | Pass | Go 1.22.12 `go vet` and strict mypy pass for all generated packages. |
| 7: native regression suites | Pass | The normalizer suite includes all 13 cases and passes, including repeated normalization and permutation checks. The Go emitter suite includes case 13 and its dependency; all 14 model AST checks and 98 Python tests pass. |
| 8: complete conformance coverage | Pass | Native AST checks cover all public surfaces in all 14 packages per language. The prepared consumer inventory covers all 11 Go emitted calls plus one deferred call, and 13 Python emitted calls plus two deferred calls. |
| 9: exact generated profiles | Pass | Both installed CLIs pass all 19 exact positive profiles. The matching v4 Go run records the tested binary's hash in every package release; the Python inputs are byte-identical to its passing run. |
| 10: complete profile validation | Pass | Both installed runs validate the complete closure and omit the schema-only dependency from direct contributors. That dependency still supplies the constraint rejecting the invalid declaration. |
| 11: semantic negatives | Pass | Both installed runs reject all six reviewed negatives, including the dependency-only constraint. The three normalizer negatives also match their exact diagnostics. |
| 12: deterministic source generation | Pass | Three source emissions per model match byte for byte for both languages. |
| 13: required archive resources | Pass | All four fixed resources are present and byte-identical to the assembled resources in every archive. |
| 14: no undeclared archive files | Pass | Exact archive file sets and Python RECORD hashes are checked for all 28 archives. |
| 16: API comparison | Pass | Go and Python native AST surfaces match their normalized-model emission plans for all packages. |

## Prepared evidence locations

- Complete, validated assembly recording the repaired Go profiler:
  `/Users/colacy/code/github.com/runtimeconditions/phase4-dependency-schema-fixtures-v4-20261004`.
  The v3 assembly remains preserved for earlier evidence. Preparation attempts
  before v3 are incomplete and are not acceptance inputs.
- Reviewed workloads, expected profiles and diagnostics, installation commands,
  and hashes:
  `/Users/colacy/code/github.com/runtimeconditions/phase4-dependency-schema-results-v4-20261004/inventory.yaml`.
- Assembly comparison and profiler provenance:
  `/Users/colacy/code/github.com/runtimeconditions/phase4-dependency-schema-review-20261004/v4-provenance.yaml`.
  Every Go release records profiler SHA-256
  `b2e2aee5e10af47a0e75998d3ac591ee17f2887aaaddeae46405296763bc1444`.
  All other generated Go files, all 14 Python wheels, and both languages'
  workload sources and expected results are byte-identical to v3. The Go
  archive dependency locks change with the release metadata.
- Shared gate evidence:
  `/Users/colacy/code/github.com/runtimeconditions/phase4-dependency-schema-review-20261004/evidence-final/gates.yaml`.
  Its status is `passed`. Earlier pending reviews remain preserved separately.
- Repeatable shared verification runner:
  `/Users/colacy/code/github.com/runtimeconditions/phase4-dependency-schema-review-20261004/run_gates.py`.
  It writes evidence in YAML, uses a separate source snapshot for regression/AST
  checks, compiles copies of Go package sources, and executes no profiler or
  application source. Minimum-version Go test binaries are compiled without
  execution; native unit suites run separately with the current Go toolchain.
- Consolidated machine-readable acceptance review and evidence hashes:
  `/Users/colacy/code/github.com/runtimeconditions/phase4-dependency-schema-review-20261004/consolidated-review.yaml`.

## Installed CLI evidence

- Python: 54/54 checks pass: 19 exact positive profiles, six reviewed negatives,
  and 29 supplemental checks. The schema-only dependency contributes no
  vocabulary, is omitted from the profile, and still rejects the invalid
  declaration. The unchanged Python artifacts permit reuse of this passing run:
  `/Users/colacy/code/github.com/runtimeconditions/python-profiler-dependency-schema-acceptance-final-20261004/summary.yaml`.
  The Python agent independently verified all 14 wheels, 56 fixed resources,
  50 workload files, 25 expectation files, and the inventory after accounting
  for the changed assembly/result paths:
  `/Users/colacy/code/github.com/runtimeconditions/phase4-dependency-schema-review-20261004/python-v4-equivalence.yaml`.
- Go v4: all 25 workloads pass: 19 exact positive profiles and six reviewed
  negatives. All 14 package releases record the executed profiler's full
  SHA-256. The run checks resolved module-cache resources, denies source
  checkout reads, and confirms binding source is not executed using an
  injected panic canary in a separate derived artifact:
  `/Users/colacy/code/github.com/runtimeconditions/go-rc-profiler/conformance/phase4-go-matched-provenance-2026-10-04/result.yaml`.
  Schema-linked profiler regression tests and `go vet` also pass:
  `/Users/colacy/code/github.com/runtimeconditions/go-rc-profiler/conformance/phase4-go-matched-provenance-2026-10-04/checks.yaml`.
  The earlier v3 result remains preserved as
  `behavior-passed-provenance-pending`; it is superseded by this passing v4 run.

## Case 13 registration and acceptance conclusion

The user approved the three additional files required to register case 13.
The completed changes are:

- `normalizer/conformance_test.go`: register the thirteenth case so the complete
  suite performs its existing repeated-normalization and permutation checks.
- `emitters/go/conformance_test.go`: include the new positive model and assemble
  its dependency first in the existing package checks.
- `model/conformance/expected/13-dependency-schema-only/runtimeconditions.binding-model.yaml`:
  retain the reviewed normalizer unit checkpoint. Its synthetic unit-test
  normalizer digest is separate from the real executable provenance in the
  installable fixtures.

The checkpoint matches the installable model's structure, coordinates,
constraints, and extension/core identities. Only the unit-test normalizer
provenance and resulting model digest differ. Python's existing parametrized
suite discovers the checkpoint automatically and now covers case 13.

All applicable Phase 4 gates (1–14 and 16) pass. The installed Go and Python
acceptance evidence, shared package checks, and native regression suites close
the approved Phase 4 scope. The registration changes no production code or
assembled package bytes, so the passing installed CLI runs remain applicable.

This conclusion covers the tested local environment. Go 1.22.12 compiles every
generated binding and conformance source and passes static analysis; its test
binaries were compiled without execution. Native Go suites execute with the
current Go toolchain, while Python checks use Python 3.11.16. Production
orchestration, publication, distribution, and broader platform qualification
remain outside this acceptance. `spec/`, the implementation standard, and its
guard remain unchanged.
