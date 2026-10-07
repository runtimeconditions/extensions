# Extension binding tooling

`IMPLEMENTATION.md` is the protected baseline; later owner-directed scope and
verification decisions are recorded below.
The shared Go resolver and normalizer feed native Go and Python emitters.
The Go orchestrator exposes eight commands through Go's standard `flag` package.
GitHub Actions generates and commits Go bindings to `main`. Python automation,
package distribution, GitHub promotion, and registry publication remain later work.

## Automatic Go binding generation

`.github/workflows/binding-generate.yml` runs on relevant `main` changes to the
extension catalog, delivery configuration, schemas, Go generation tooling, and
the workflow or its helper. The initial automatic targets are the verified Phase 5
pair, `common-integrations:go,env-configuration:go`. The repository variable
`BINDING_GENERATION_TARGETS` replaces that default with a comma-separated list of
`package-key:go` targets, or `all` to select every configured Go target. Manual
dispatch on `main` accepts the same values; an empty `targets` input uses the
automatic selection. Python targets are deliberately disabled until the separate
Python profiler build is fixed.

The initial pair completes generation and verification. An all-catalog rehearsal
stops at `aws-s3:go`: the catalog requests a URI/version identity, while its local
definition still supplies legacy `metadata.id`. Remaining targets must resolve
and verify before expanding automatic selection. The workflow does not silently
skip an unresolved selected target or commit a partial failed generation.

The workflow checks out current `main`, selects its exact catalog Go compiler,
installs the checksummed Go profiler release and core schema artifact, tests the
shared Go tooling, and builds workspace `rc`. It runs `rc bindings update`, which
generates and verifies candidates before synchronizing their source directories.
Only changes beneath the selected `bindings/<package-key>/go` directories are
staged and committed by `github-actions[bot]`. Unchanged output creates no commit.

Runs are serialized. The commit helper checks that `main` still points to the
generation input commit and uses an ordinary fast-forward push. If another
commit wins the race, this run fails and must be rerun against current `main`;
it never rebases or forces generated output over newer inputs. Workflow summaries
record selected targets and the generated commit, and verification YAML is in
the step log. Temporary tools and intermediate archives stay outside the checkout.

The workflow uses the repository `GITHUB_TOKEN` with `contents: write` in its
generation job. Repository branch rules must permit that bot to push to `main`.
GitHub does not start another push workflow for a commit made with `GITHUB_TOKEN`,
so the generated commit does not recursively run this automation. See
[GitHub's workflow triggering documentation](https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/trigger-a-workflow).

This is development generation with actual workspace tool identities. It does
not create package tags, publish releases, or contact a registry. The pinned core
schema snapshot supplies version 0.2.0 while the default website URL is unavailable;
both it and the independently released Go profiler archive are checked before use.
The Python helper runtime is separate from generated Python bindings and does
not install or build the Python profiler.

On 2026-10-07, the repository owner explicitly directed Phase 6 to generate and
commit bindings to `main`, replacing the manual generation and review-PR path.
That direction supersedes the relevant delivery steps in `IMPLEMENTATION.md`;
the protected document and its checksum guard remain unchanged. Phase 5's shared
conformance verification decision remains in effect.

## `rc bindings`

Build from the repository root:

```sh
go -C tooling/extension-bindings/orchestrator build -o /tmp/rc ./cmd/rc
/tmp/rc bindings --help
```

Every command shares the same catalog, resolution, model, emission, verification,
and packaging pipeline. Target selection uses repeatable
`--target <package-key>:<language>` flags and defaults to all catalog targets.
Adding an extension requires delivery metadata in `packages.yaml`; vocabulary,
schema shapes, and dependency edges come from its definition. Production code
does not dispatch on extension identifiers or vocabulary values.

| Command | Operation and retained output |
| --- | --- |
| `resolve` | Validated semantic closure and dependency lock on stdout; requested `--closure-output` and `--dependency-lock-output` files only. |
| `normalize` | Normalized model on stdout; optional `--output` model and `--dependency-lock-output` files. |
| `generate` | Selected package trees beneath the required new or empty `--output` directory. |
| `verify` | Package structure, native tooling, and reproducibility checks against `--input`, or committed selected trees; YAML on stdout and an optional `--report` file. |
| `package` | Clean generation must match committed source; verifies and packages committed trees into the required `--output` directory. |
| `plan-release` | Automatically finds previous GitHub target assets and compares schemas, native AST APIs, behavior, dependencies, and language minimums; YAML on stdout and optional `--output`. |
| `check` | Clean generation, package verification, and exact comparison with committed selected trees; optional `--report`. |
| `update` | Clean generation and package verification, followed by synchronization of selected committed trees, including stale file removal. |

All commands accept `--packages`, `--toolchain-lock`, `--cache`, and `--work-dir`.
Configuration defaults to the nearest ancestor containing both configuration
files. An explicit configuration path determines the sibling default for the
other file. Supplied relative paths resolve against the current directory.
`--tooling-dir` locates the schema bundle and development tools when needed.

No persistent cache or work directory is created by default. Subprocess inputs,
native package consumers, and build artifacts use one temporary workspace that
is cleaned on return. Explicit `--work-dir` retains required intermediates;
explicit `--cache` enables verified content-addressed extension reuse. Output
paths must be new files or empty directories outside committed `bindings/`.
`update` is the sole command that synchronizes committed generated source.

Resolution accepts exact `--extension-root <id>`, development-only
`--extension-override <id>=<path>`, and optional `--catalog-root <directory>`.
The repository catalog is discovered by default. Development HTTPS resolution
records the fetched bytes in the dependency lock; released resolution requires
an existing `--dependency-lock`. HTTPS redirects and unsupported identifier
schemes fail resolution.

### Native tools and independent profilers

Development builds the normalizer and Go emitter from workspace source and uses
the workspace Python emitter. Their actual artifact or source snapshot digests
enter generated provenance. A released lock requires installed tools matching
its digests. Verification requires the catalog's exact Go compiler or Python
interpreter; use `--go` and `--python` to select them.

Install the profilers independently and provide their paths:

```sh
export RC_GO_PROFILER_BIN=/path/to/go-rc-profiler
export RC_BINDINGS_PYTHON=/path/to/tooling-venv/bin/python
export RC_PYTHON_PROFILER_BIN=/path/to/tooling-venv/bin/runtimeconditions-python-profiler
export RC_PYTHON_PROFILER_ARTIFACT=/path/to/runtimeconditions_profiler-version-py3-none-any.whl

/tmp/rc bindings generate --packages tooling/extension-bindings/packages.yaml \
  --target source-control:go --output /tmp/generated-bindings
/tmp/rc bindings verify --packages tooling/extension-bindings/packages.yaml \
  --target source-control:go --input /tmp/generated-bindings
```

The Python tooling environment needs the emitter's declared build and analysis
dependencies. The profiler wheel is checked against its independently installed
distribution, then installed with generated binding wheels into an isolated
native consumer. Go uses an isolated module proxy and module cache. The bounded synthetic
profiler tests receive these installed binding packages and source declarations.
The generator never imports a profiler source repository, and conformance
declarations are compiled or parsed without executing application code.

The current development catalog uses Go 1.25.0 and Python 3.12.10. Go 1.25
matches the source analysis supported by the independently released Go profiler.
The existing
development toolchain lock remains a development input. Producing the released
toolchain lock and synchronizing the repository's generated target inventory
remain separate Phase 5 work.

## Phase 5 verification scope

On 2026-10-06, the repository owner explicitly overruled the per-target
conformance requirement in `IMPLEMENTATION.md`. The protected baseline remains
read-only. This decision replaces Section 13 gates 8–11 for production packages
and moves generated-package unit exercises (gate 7) into the shared tooling suite.

The generation mechanism is tested against the existing 13 synthetic cases for
schema shapes, naming, dependencies, unsupported input, and determinism. Native
API exercises are temporary test consumers. Six authored profiler consumers
across Go and Python check exact profile extraction and rejection of a constraint
from a schema-only dependency. Network and previous-release responses are mocked
or served by test servers. These tests require no real extension catalog.

Delivered packages contain native source, package metadata, the four fixed YAML
resources, and the file manifest. They contain no generated conformance trees,
expected-profile files, or `_conformance.py`. New regressions belong in the
shared tooling tests.

`verify`, `check`, `package`, and `update` check the model, exact manifest-to-model
mappings, source and tool identities, dependency installation, native formatting,
compilation and static analysis, deterministic regeneration, archive inventory,
and file digests. YAML reports identify `scope: package-structure` and mark gates
7–11 `not-applicable`; they do not claim those checks ran for each package.

Real-extension package generation is an explicit use of these commands. The
verification revision does not add committed packages for the repository catalog.
Production release pins remain pending actual tooling releases and a separately
released Python profiler artifact. A checksummed GitHub wheel release is sufficient
for that artifact; PyPI publication is not required.

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
on Linux.

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

Output paths must not already exist. HTTPS retrieval requires a dependency lock
with an exact source digest and the URL derived from the requested identifier:
`https://<domain>/extensions/<provider>/<service>/<version>/runtimeconditions.extension.yaml`.
Providerless identities use provider `rc` for retrieval while retaining their
declared identity. HTTPS redirects are rejected; `file:` and `oci:` retrieval
are unsupported. A previously written lock is supplied with `--dependency-lock`.

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
uses the structural v1alpha2 schema in `model/`.

## Phase 5 configuration contracts

Step 1 is complete for Go and Python. The normalizer and Go emitter build
directly from this checkout without overlays. Fresh fixtures for all fourteen
package roots in each language have passed independent installed-profiler
verification. The command pipeline above supplies production assembly.

The shared schemas in `model/` define the inputs for the production generation
mechanism:

- `runtimeconditions.package-catalog.schema.yaml` describes arbitrary extension
  roots and language delivery targets: native coordinates and names, package
  versions, exact compiler or interpreter versions, source directories, and
  publication settings. Extension vocabulary, shapes, and dependency edges
  come from extension definitions and the normalized model.
- `runtimeconditions.toolchain-lock.schema.yaml` accepts development workspace
  tools and requires versions and artifact SHA-256 values for the normalizer,
  orchestrator, emitters, and independent profilers in a released lock. It also
  pins the native language build tools. The existing `phase3-development` lock
  remains valid; it is not a released toolchain lock.
- `runtimeconditions.file-manifest.schema.yaml` defines a complete mapping of
  generated relative POSIX file paths to SHA-256 values, excluding the manifest
  itself. Producers sort paths by UTF-8 bytes. Consumers check exact inventory
  and file bytes.

These are generic contracts. The orchestrator checks relationships
that JSON Schema cannot compare: a target directory against its containing
package/language keys, selected language coverage in the lock, and actual tool
and generated-file digests. The catalog contains delivery metadata; the model
contains extension meaning.

Both native emitters require `emitterSha256` in the package target when emitting
package resources. The caller supplies the lowercase SHA-256 of the emitter
artifact it executes. Released generation uses the exact artifact from the
toolchain lock; development can use an actual workspace tool. Loading a target
for source planning does not require this field. Missing or malformed values
fail before a package output tree is written.

The binding manifest records the digest in its existing string field:

```yaml
generated:
  nonEditable: true
  emitter: <emitter-name>@sha256:<64-lowercase-hex-digest>
  version: <emitter-version>
```

The API version remains `runtimeconditions.io/bindings/v1alpha2`, with the same
field structure. The producer schema requires the checksummed identity. Each
profiler remains an independent installed consumer; these configuration schemas
are owned by the generation tooling and require no profiler repository changes.

## Installed Go profiler fixture

The Go emitter test `TestInstalledGoProfilerBindingPackage` is a limited Phase
4 fixture assembler and installed-package check. It emits an owned-declaration
module, adds the exact normalized model and validated root extension, and
builds a schema-valid test-only release manifest from the resolver's real
dependency lock and the actual assembler and profiler binary digests.

Set `RC_GO_PROFILER_BIN` to an absolute path to an installed profiler binary,
then run from `emitters/go/`:

```text
RC_GO_PROFILER_BIN=/path/to/go-rc-profiler go test . -run TestInstalledGoProfilerBindingPackage -count=1 -v
```

The test creates a local Go module proxy outside the profiler repository and
downloads the assembled module into an isolated workload. It then disables
`GOPROXY` before invoking the profiler. The profiler receives only the workload
directory and the binding package resolved by Go. A second case changes the
installed root extension bytes and requires the release source digest check to
reject the package.

This is the first owned-declaration fixture. Direct and transitive additive
packages and the Python fixtures remain for the wider Phase 4 gate.
