# Phase 4 installable binding fixtures

`assemble.py` builds **test-only** Go modules and Python wheels for all ten
positive conformance cases, including owned declarations and direct and
transitive additive dependencies. It is a limited fixture assembler, not the
Phase 5 production orchestrator or a package
publication tool. The profiler runs later, from its installed CLI, against a
separate workload. Neither profiler is embedded in the binding package.

The fourteen package roots are `01-owned-kind-interface`, the base and root of
`02-additive-field`, and the leaf, middle, and root of
`03-transitive-closure`, plus `06-recursive-reference`, `07-object-alternatives`,
`08-heterogeneous-union`, `09-collections-and-maps`,
`10-scoped-domains-collisions`, `11-source-name-preservation`, and the dependency
and root of `13-dependency-schema-only`.
Both languages use this same dependency-first package inventory. Cases 04 and
05 are negative normalizer cases and do not produce installable packages.
Each package contains the exact emitter-produced structural manifest plus the
normalized model, resolved root extension, and
schema-validated fixture release manifest at its final package-local resource
location. The assembler produces both languages in dependency order and records
SHA-256 of the actual normalizer executable, Go emitter executable, Python
emitter source snapshot, assembler script, profiler artifact, extension source,
and direct dependency archives. It rejects missing
or contradictory identities instead of inventing release provenance.

The assembler writes derived package targets under `targets/`, supplying
`emitterSha256` without changing committed target fixtures. Go uses the built
emitter executable's exact bytes. Python development uses the SHA-256 of a
canonical JSON mapping from source-relative paths to exact file digests: the
emitter's `pyproject.toml` and every `src/runtimeconditions_binding_emitter/*.py`
file, with sorted keys, compact separators, and UTF-8 encoding. The emitted
binding manifest records `<emitter-name>@sha256:<digest>` in `generated.emitter`.
This identifies the actual development source snapshot; production generation
must use its locked release artifact digest.

## Assemble

Use Python 3.11 or newer with `PyYAML==6.0.3`, `jsonschema==4.26.0`,
`rfc8785==0.1.4`, `setuptools==84.0.0`, `wheel==0.48.0`, and `ruff==0.16.9`
installed. The Python interpreter must have `pip` and the pinned Ruff executable
available. Go 1.22 or newer must be on `PATH`. Supply an actual Go profiler
executable built with the approved core schema and an actual Python profiler
wheel; the assembler hashes these artifacts for test provenance. The core
schema is read from the adjacent `spec` repository **only at assembly time**.

```sh
python3 tooling/extension-bindings/fixtures/assemble.py \
  --python /absolute/path/to/python \
  --go-profiler /absolute/path/to/go-rc-profiler \
  --python-profiler-wheel /absolute/path/to/runtimeconditions_profiler-0.1.0-py3-none-any.whl \
  --output /absolute/path/to/new-fixture-output
```

The default command produces fourteen Go module archives and fourteen Python
wheels. Run this command from the `extensions` checkout. The output directory
must be new or empty. Generated packages are written under `go/packages/` and
`python/packages/`; the installable artifacts are under `go/proxy/` and
`python/wheels/`. `models/` contains the exact normalized models consumed by
the emitters, and `tools/` contains the executables whose bytes were hashed.
No package is published, and the fixture output must not be treated as
production release evidence.

For the Python profiler acceptance suite, use `--python-only`. This assembles
the same fourteen Python wheels for all positive conformance cases. It requires
no other profiler artifact and does not produce Go packages. The Python
interpreter supplied with `--python`
must have the pinned fixture build tools installed.

```sh
python3 tooling/extension-bindings/fixtures/assemble.py \
  --python-only \
  --python /absolute/path/to/fixture-build-venv/bin/python \
  --python-profiler-wheel /absolute/path/to/runtimeconditions_profiler-0.1.0-py3-none-any.whl \
  --output /absolute/path/to/new-python-fixture-output
```

The checked-in normalizer conformance checkpoints still use a synthetic
normalizer hash to keep those golden files independent of a platform-specific
binary. `assemble.py` normalizes afresh with its built executable and places
that executable's actual SHA-256 in every installable fixture model.

## Consume through package managers

For a Go workload, add the desired generated module at `v1.0.0` to its
`go.mod`, then resolve it with `GOPROXY=file:///absolute/path/to/new-fixture-output/go/proxy`
and `GOSUMDB=off`. `go list -json <module-import-path>` must report a source
directory in the Go module cache. The four `runtimeconditions.*.yaml` files
are next to `bindings.go` in that directory. To check the dependency chain,
use `example.com/runtimeconditions/conformance/additive-field` or
`example.com/runtimeconditions/conformance/transitive-root` as the imported
module. The additional cases use the same proxy; for example, resolve
`example.com/runtimeconditions/conformance/recursive-reference@v1.0.0` for
case 06. The profiler CLI receives the workload directory and resolves that
module through Go; it receives no `extensions` checkout path.

For a Python workload, install the generated root wheel with
`python -m pip install --no-index --find-links /absolute/path/to/new-fixture-output/python/wheels runtimeconditions-conformance-transitive-root`.
Pip installs the direct and transitive dependency wheels. The four resources
are package data in the installed import package and can be located through
`importlib.resources` and distribution metadata. The installed Python
profiler resolves them from that environment without an `extensions` checkout.

The positive cases 06, 07, 10, and 11 now explicitly include the required core
`kind` and `interface.type` properties in their closed extension schemas.
Their recursive shapes, union branches, scoped domains, and exact source names
retain their original constraints.

## Prepare consumer workloads and expected results

`conformance.yaml` is the reviewed, versioned inventory for both languages.
It records complete Condition values, exact direct contributing extension IDs,
emitted-call indices, complete consumer calls, and negative constraints. It is
the semantic oracle; expected values are never captured from a profiler run.

`prepare_results.py` materializes that inventory against a fresh official
assembly. Run it with the same Python environment as the assembler, with Go
available for the native AST, formatter, YAML serializer, and schema validator:

```sh
python3 tooling/extension-bindings/fixtures/prepare_results.py \
  --fixtures /absolute/path/to/new-fixture-output \
  --output /absolute/path/to/new-consumer-results
```

Use `--language python` with a Python-only assembly, or `--language go` to
prepare only Go workloads. The consumer output must be new or empty and outside
the `extensions` repository and assembled package tree. Neither fixture tool
imports binding packages, executes workload source, or invokes a profiler.
The native Go helper prepares fixtures; it performs no Condition extraction or
installed-package discovery. This remains **test-only** fixture tooling.

Preparation produces, per language:

- **19 positive workloads** with exact expected profile YAML: all ten positive
  cases; owned, direct, and transitive consumers; both object alternatives and
  heterogeneous union variants; both numeric collection values; every scoped
  enum member; omitted optional fields; recursion; exact source names; and a
  schema-only dependency omitted from direct contributors.
- **Six negative workloads**: missing/both object alternatives, an incomplete
  heterogeneous object variant, invalid collection enum value, and invalid
  scoped value, and violation of a dependency-only string-length constraint.
  Each `.error.yaml` identifies the model coordinate, condition
  and schema pointers, constraint, expected failure stage, exact diagnostic,
  nonzero exit, and absent profile output. Replace the absolute workload path
  in CLI stderr with `<workload>` before the exact comparison.
- `inventory.yaml`, with all installation/profile commands, source/expected
  hashes, package archive/resource hashes, model digests, direct contributors,
  full extension closure, and emitted/deferred declaration coverage.

The preparer validates package resources and artifact agreement, validates every
positive oracle against the approved core and all applicable closure schemas,
and confirms each negative oracle violates its recorded constraint. It fails
if an emitted call lacks an oracle or a deferred declaration lacks a complete
consumer call. The current inventory covers **11 emitted Go calls plus one
deferred declaration**, and **13 emitted Python calls plus two deferred
declarations**. The remaining positives are explicit consumer and constraint
coverage workloads.

Case 13 owns every used kind, interface, and field in its root extension. Its
dependency owns no vocabulary and supplies only the applicable `command-limit`
schema. The positive `dependency-schema-only` profile lists only the root,
although its resolved closure includes both extensions. The negative
`dependency-schema-invalid` command is too long solely under that dependency's
`maxLength: 6` constraint. The preparer checks that the dependency is omitted
from direct contributors, declares no vocabulary, and has an applicable schema;
it also checks that every other schema accepts the negative Condition. The
`dependencySchema` inventory field identifies the exact coordinate to verify
in installed CLI evidence. The original twelve packages and earlier passing
evidence remain separate, preserved inputs.

Declaration-only packages retain function-reference coverage in their native
conformance source. They cannot invent an interface supplied by a dependent
package. The consumer workloads complete those calls through installed
interface/field providers. No positive declaration is counted as a successful
rejection. Go case 07 samples supply one optional alternative at a time; case
09 samples use the normalized allowed value `1`. Go samples use native static
address expressions and include required fields when terminating recursion.

Failure stages are intentional: Go rejects the incomplete union object at the
manifest's required-field check; Python rejects it at the preserved `oneOf`
schema. Both languages reject the scoped value during vocabulary validation.
These stages are recorded explicitly rather than presenting structural
rejection as evidence that the CLI reached JSON Schema validation.

The three normalizer negatives (04, 05, 12) are also copied under `normalizer/`,
with the assembled normalizer executable, schemas, exact committed diagnostics,
and repeatable commands in `inventory.yaml`. They produce no installable binding
packages and must leave no normalized model output.

## Installed CLI acceptance handoff

The preparer writes `status: prepared-awaiting-installed-cli-acceptance`.
Schema-valid oracles and compilable workloads are preparation evidence; the
profiler agents must run the installed CLIs and compare the actual results.

The executed profiler artifact must match the SHA-256 recorded in each
package's release provenance. After a profiler repair, assemble new packages
with the repaired artifact and prepare a new result directory before repeating
the installed acceptance run. Preserve earlier assemblies and results as
separate evidence; passing behavior against different recorded profiler bytes
does not establish matching-provenance acceptance.

For each Go workload, use an isolated module/build cache, then run the recorded
`go mod download all` command with its fixture proxy. Confirm `go list -json`
returns downloaded package directories with all four resources, then invoke
the separately installed profiler with `GOPROXY=off`, for example:

```sh
cd /absolute/path/to/new-consumer-results/go/workloads/owned
export GOWORK=off GOSUMDB=off
export GOMODCACHE=/absolute/path/to/isolated-go-module-cache
export GOCACHE=/absolute/path/to/isolated-go-build-cache
export GOPROXY=file:///absolute/path/to/new-fixture-output/go/proxy
go mod download all
GOPROXY=off /absolute/path/to/installed-go-profiler generate \
  -dir . -name phase4-owned \
  -workload-uri https://example.test/phase4/owned \
  -workload-version 1.0.0 -out profile.yaml
```

For Python, create an isolated environment, install the profiler artifact and
its dependencies there, and install each workload's pinned distributions
through `pip`. Use that environment's installed console script:

```sh
cd /absolute/path/to/new-consumer-results/python/workloads/owned
/absolute/path/to/workload-venv/bin/python -m pip install \
  --no-index --find-links /absolute/path/to/new-fixture-output/python/wheels \
  -r requirements.txt
/absolute/path/to/workload-venv/bin/runtimeconditions-python-profiler profile generate \
  --project . --name phase4-owned \
  --workload-uri https://example.test/phase4/owned \
  --workload-version 1.0.0 --out profile.yaml
```

The workload imports only installed packages and passes no source checkout path
to a profiler. A local Go proxy and local wheel directory are mock retrieval
infrastructure; all four packaged resources and their provenance/digests are
real, schema-valid fixture data. Agents may also serve the same artifacts from
their existing mock package indexes.

Acceptance requires all 19 positive comparisons and six negative comparisons
for each language, repeatable profile bytes, no output after rejection, full
closure validation, and no application/package-code execution. Preserve the
inventory and exact artifact hashes with each run. The profiler repositories'
additional trust-chain, expression-boundary, and local/package-manager checks
remain part of their installed CLI acceptance work.
