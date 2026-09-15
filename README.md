# Runtime Conditions Extensions

Runtime Conditions is currently seeking adoption by an established parent project. The repositories in this organization are split for hands-on usability, review, demos, and implementation feedback. They are not intended to present Runtime Conditions as a standalone foundation or competing project.

Start here: https://runtimeconditions.github.io/

## Purpose

This repository contains Runtime Conditions extension definitions, declaration packages, semantic overlays, and extension-maintenance automation. Extension definitions own vocabulary and validation. Language declaration packages provide no-op source declarations that generators can read.

Extension release identity, immutability, provenance, and SDK compatibility are defined in [`EXTENSION_RELEASES.md`](EXTENSION_RELEASES.md).

Operation-oriented extensions use the authoring contract in [`SERVICE_OPERATIONS_SEMANTIC_BRIDGES.md`](SERVICE_OPERATIONS_SEMANTIC_BRIDGES.md). A semantic bridge references an authoritative Smithy, OpenAPI, protobuf, or comparable source when available and uses a separate Service Operations Inventory only as a fallback.

## Extensions

Per the [spec](https://github.com/runtimeconditions/spec/blob/main/docs/sixth-draft.md#5-extensions), first-party extensions are extensions like any other - there's no separate "core vocabulary" tier, and no notion of an extension being owned by this project versus anyone else's. Every extension lives together under `catalog/`, declared and resolved the same way regardless of who authored it:

| Extension | Identifier | Path |
| --- | --- | --- |
| Common Integrations | `https://runtimeconditions.io/extensions/common-integrations/v1alpha1/runtimeconditions.extension.yaml` | [`catalog/rc/common-integrations/`](catalog/rc/common-integrations/) |
| Env Configuration | `https://runtimeconditions.io/extensions/env-configuration/v1alpha1/runtimeconditions.extension.yaml` | [`catalog/rc/env-configuration/`](catalog/rc/env-configuration/) |
| Source Control | `https://runtimeconditions.io/extensions/source-control/0.1.0/runtimeconditions.extension.yaml` | [`catalog/rc/source-control/`](catalog/rc/source-control/) |
| Amazon S3 | `https://runtimeconditions.io/extensions/aws-s3/0.1.0/runtimeconditions.extension.yaml` | [`catalog/aws/s3/`](catalog/aws/s3/) |
| Kubernetes API | `https://runtimeconditions.io/extensions/kubernetes-api/0.1.0/runtimeconditions.extension.yaml` | [`catalog/kubernetes/api/`](catalog/kubernetes/api/) |
| NATS | `https://runtimeconditions.io/extensions/nats-service/0.1.0/runtimeconditions.extension.yaml` | [`catalog/nats/service/`](catalog/nats/service/) |
| Google Analytics | `https://runtimeconditions.io/extensions/google-analytics/0.1.0/runtimeconditions.extension.yaml` | [`catalog/google/analytics/`](catalog/google/analytics/) |

## Folder layout

- `catalog/` - every extension, first-party or community, lives here as `<namespace>/<name>/`. First-party extensions that aren't tied to an external vendor or project sit under `rc/` (`rc/common-integrations/`, `rc/env-configuration/`, `rc/source-control/`) - it's a namespace like any other, not a special tier. Everything else is namespaced by vendor or project (`aws/s3/`, `google/analytics/`, `kubernetes/api/`, `nats/service/`). The nesting is consistent for every entry, whether a namespace will only ever have one extension (`kubernetes/`, `nats/`) or many (`aws/`, `google/`, `rc/`) - we're not picking a different shape per case.
- `tooling/` - automation shared across extensions, not extensions themselves. Still figuring out the right shape for this one.
  - `common/` the shared Python helpers (e.g. `serialization.py`) every extension's `tools/` imports from, so a fix only has to happen in one place.
  - `smithy-runtime-conditions/` the external compiler used to prove semantic-bridge generation against AWS's public Smithy models before proposing an internal AWS generator integration.

## Local validation

From a sibling `go-rc-profiler` checkout:

```sh
cd ../go-rc-profiler
go run . validate-extensions -root ../extensions
```

Published extension identifiers are immutable semantic releases. A new semantic definition receives a new exact identifier; Runtime Conditions document `apiVersion` changes are independent.
