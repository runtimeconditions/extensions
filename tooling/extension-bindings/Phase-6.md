# Phase 6: automatic generation into main

## Scope approved on 2026-10-07

The repository owner directed GitHub Actions to generate extension binding files
and commit them directly to `main`. This supersedes manual generation and the
generation review PR described in the earlier handoff. Package distribution is
deferred to the next phase. Go is enabled first; Python remains disabled until
the Python profiler build is fixed in its own repository. Work is confined to
`extensions/`, and profiler source is not inspected, embedded, or modified here.

## Implementation

- `binding-generate.yml` runs on relevant pushes to `main` and manual dispatch.
- Automatic selection initially covers `common-integrations:go` and
  `env-configuration:go`, the verified Phase 5 pair. The optional repository
  variable `BINDING_GENERATION_TARGETS` and manual input accept comma-separated
  `package-key:go` keys or `all` for the full configured Go catalog.
- The runner installs the catalog's exact Go compiler, a checksummed released
  Go profiler binary, and a checksummed core schema snapshot. It builds generation
  tools from this repository's workspace source and records their real identities.
- The existing `rc bindings update` pipeline verifies generation before replacing
  selected generated source trees. Shared tooling tests retain conformance coverage.
- The helper commits only selected generated Go source directories, includes stale
  file removals, and leaves unchanged output without a commit.
- Runs are serialized, and the helper rejects a changed `main` head before staging.
  The final ordinary Git push also rejects concurrent divergent updates.
- The workflow uses the repository token and reports its result in the Actions
  summary. It creates no tags, release assets, or registry publications.

The workflow requires branch rules that allow the Actions bot to push to `main`.
The full catalog currently stops at `aws-s3:go`, whose local definition still
uses legacy `metadata.id` rather than the root requested by the package catalog.
Automatic selection can expand after the selected definitions resolve and verify.
Enabling Python later requires a working independently installed profiler artifact
and corresponding runner provisioning and tests inside this repository.

The protected `IMPLEMENTATION.md`, repository checksum variable, and implementation
lock workflow are unchanged. Production release toolchain pins and distribution
remain pending.

## Local verification

The seven helper tests, actionlint, and the Go normalizer, emitter, and orchestrator
suites passed. The initial pair passed generation and native package verification;
a second run produced identical bytes across all 14 generated Go files. Rehearsal
outputs stayed in the ignored `phase5-work/` checkout. No workflow has been
dispatched and no generated commit has been pushed to GitHub during this work.
