# Runtime Conditions Extensions

Runtime Conditions is a portable declaration of what one workload needs from outside itself, such as APIs, datastores, caches, and configuration, without the actual values. This repository defines the vocabulary that Runtime Conditions profiles use, and generates code packages so developers can declare those needs in code.

Start here: https://runtimeconditions.github.io/
To learn more about the language we use in this project, check out the [glossary](GLOSSARY.md).

## How it fits together

The core [spec](https://github.com/runtimeconditions/spec/blob/main/docs/sixth-draft.md#5-extensions) defines the shape of a profile, but not its words. Extensions define the words (kinds, interface types, interface fields, and allowed values) and the rules for using them. A profile lists every extension it uses by its exact identifier URL. Bindings are generated packages that let developers declare a workload's needs in code. A profiler reads those declarations and writes the profile, and a platform validates the profile against the extensions it lists.

<img width="2648" height="302" alt="image" src="https://gist.github.com/user-attachments/assets/0898c0fc-2149-470f-acb5-a257975ca593" />


## Here is an example

This example follows one need, a Redis cache, from the extension to the profile.

The Common Integrations extension defines the `cache` kind, ties the `key_value` interface type to it, and allows `redis` as its engine. From [`catalog/rc/common-integrations/common-integrations-v1alpha1.yaml`](catalog/rc/common-integrations/common-integrations-v1alpha1.yaml):

```yaml
spec:
  kinds:
    - name: api
    - name: datastore
    - name: cache

  interfaceTypes:
    # ...
    - name: key_value
      targetKind: cache

  # ...

  fieldValues:
    # ...
    - field: interface.engine
      targetKind: cache
      targetType: key_value
      values:
        - redis
        - memcached
```

A Go workload declares the cache with the generated Common Integrations and Env Configuration bindings:

```go
import (
    "runtimeconditions.io/x/rc/common"
    "runtimeconditions.io/x/rc/env"
)

func cache() common.Declaration {
    engine := common.KeyValueEngineRedis
    return common.Cache(
        common.KeyValue{Engine: &engine},
        env.KeyValueConfiguration{
            Env: env.KeyValueConfigurationEnv{
                {Name: "REDIS_URL", Property: env.KeyValueConfigurationEnvPropertyUrl},
            },
        },
    )
}
```

A profiler turns that declaration into the profile's `extensions` entries and a `cache` condition:

```yaml
extensions:
  - https://runtimeconditions.io/extensions/rc/common-integrations/v1alpha1/runtimeconditions.extension.yaml
  - https://runtimeconditions.io/extensions/rc/env-configuration/v1alpha1/runtimeconditions.extension.yaml
conditions:
  - kind: cache
    interface:
      type: key_value
      engine: redis
    configuration:
      env:
        - name: REDIS_URL
          property: url
```

The generated functions do nothing at runtime: the compiler checks the declaration, and the profiler reads it.

## What's in this repository

- `catalog/` - every extension, first-party or community, lives here as `<namespace>/<name>/`. First-party extensions that aren't tied to an external vendor or project sit under `rc/` (`rc/common-integrations/`, `rc/env-configuration/`, `rc/source-control/`) - it's a namespace like any other, not a special tier. Everything else is namespaced by vendor or project (`aws/s3/`, `google/analytics/`, `kubernetes/api/`, `nats/service/`). The nesting is consistent for every entry, whether a namespace will only ever have one extension (`kubernetes/`, `nats/`) or many (`aws/`, `google/`, `rc/`) - we're not picking a different shape per case.
- `bindings/` - generated packages, one folder per extension and language (`bindings/<package-key>/<language>/`), written by the `binding-generate` workflow. Never edited by hand.
- `tooling/extension-bindings/` - the generator that turns extensions into bindings: the normalizer, the per-language emitters, and the orchestrator. See its [README](https://github.com/runtimeconditions/extensions/blob/main/tooling/extension-bindings/README.md).
- `tooling/smithy-runtime-conditions/` - the compiler that builds operation extensions, such as Amazon S3, from a provider's Smithy model. See its [README](https://github.com/runtimeconditions/extensions/blob/main/tooling/smithy-runtime-conditions/README.md).
- `tooling/common/` - shared Python helpers (e.g. `serialization.py`) that each extension's `tools/` imports, so a fix only has to happen in one place, and the extensions site build (`build_extensions_site.py`).
- `.github/` - the workflows that test, generate, and publish.
- [`EXTENSION_RELEASES.md`](https://github.com/runtimeconditions/extensions/blob/main/EXTENSION_RELEASES.md) - extension release identity, immutability, provenance, and SDK compatibility.
- [`SERVICE_OPERATIONS_SEMANTIC_BRIDGES.md`](https://github.com/runtimeconditions/extensions/blob/main/SERVICE_OPERATIONS_SEMANTIC_BRIDGES.md) - the authoring contract for operation-oriented extensions. A semantic bridge references an authoritative Smithy, OpenAPI, protobuf, or comparable source when available and uses a separate Service Operations Inventory only as a fallback.

Bindings are produced from each extension definition in stages:

<img width="2602" height="604" alt="image" src="https://gist.github.com/user-attachments/assets/bd6abcf6-c501-46bc-94ae-ad256679e1d0" />


## Extensions

Per the [spec](https://github.com/runtimeconditions/spec/blob/main/docs/sixth-draft.md#5-extensions), first-party extensions are extensions like any other - there's no separate "core vocabulary" tier, and no notion of an extension being owned by this project versus anyone else's. Every extension lives together under `catalog/`, declared and resolved the same way regardless of who authored it. The status is the one each extension's own README gives.

| Extension | Identifier | Path | Status | Bindings |
| --- | --- | --- | --- | --- |
| Common Integrations | `https://runtimeconditions.io/extensions/rc/common-integrations/v1alpha1/runtimeconditions.extension.yaml` | [`catalog/rc/common-integrations/`](catalog/rc/common-integrations/) | Draft — Request for Comments | Go, Python, TypeScript |
| Env Configuration | `https://runtimeconditions.io/extensions/rc/env-configuration/v1alpha1/runtimeconditions.extension.yaml` | [`catalog/rc/env-configuration/`](catalog/rc/env-configuration/) | Draft — Request for Comments | Go, Python, TypeScript |
| Source Control | `https://runtimeconditions.io/extensions/rc/source-control/0.1.0/runtimeconditions.extension.yaml` | [`catalog/rc/source-control/`](catalog/rc/source-control/) | Experimental | TypeScript |
| Amazon S3 | `https://runtimeconditions.io/extensions/aws/s3/0.2.0/runtimeconditions.extension.yaml` | [`catalog/aws/s3/`](catalog/aws/s3/) | Authoritative-model maintenance experiment — not yet an AWS-published extension | TypeScript |
| Kubernetes API | `https://runtimeconditions.io/extensions/kubernetes/api/0.1.0/runtimeconditions.extension.yaml` | [`catalog/kubernetes/api/`](catalog/kubernetes/api/) | Accepted local semantic release — ready for maintainer review before public publication | TypeScript |
| NATS | `https://runtimeconditions.io/extensions/nats/service/0.1.0/runtimeconditions.extension.yaml` | [`catalog/nats/service/`](catalog/nats/service/) | Initial semantic candidate for SDK-authorship and adapter review | TypeScript |
| Google Analytics | `https://runtimeconditions.io/extensions/google/analytics/0.1.0/runtimeconditions.extension.yaml` | [`catalog/google/analytics/`](catalog/google/analytics/) | Experimental | TypeScript |

## Using the bindings

`tooling/extension-bindings/packages.yaml` defines each public module path,
package name, version, and generated source directory. Future bindings use
`runtimeconditions.io/x/<provider>/<extension>`, such as
`runtimeconditions.io/x/aws/s3` with package name `s3`.

### Go

Generated Go modules are public, use short import paths, and require Go 1.25 or later.
Consumers install them without GitHub credentials or private-module configuration:

```sh
go get runtimeconditions.io/x/rc/common@v0.2.0
go get runtimeconditions.io/x/rc/env@v0.2.0
```

The package names match the public paths, so aliases are optional.

The Go source remains in `bindings/<package-key>/go`. Versions are published through
**Publish Go bindings** in GitHub Actions; see
[the binding publication workflow](https://github.com/runtimeconditions/extensions/blob/main/tooling/extension-bindings/README.md#go-binding-publication).

### TypeScript

TypeScript bindings are published to npm for all seven extensions and require Node.js 20.19 or later:

```sh
npm install @runtimeconditions/common-integrations
```

```ts
import { cache, KeyValue, KeyValueEngine } from "@runtimeconditions/common-integrations";

export const sessions = cache(new KeyValue({ engine: KeyValueEngine.Redis }));
```

### Python

The Python bindings are not published to PyPI yet. The generated packages are in
[`bindings/common-integrations/python/`](https://github.com/runtimeconditions/extensions/tree/main/bindings/common-integrations/python) and
[`bindings/env-configuration/python/`](https://github.com/runtimeconditions/extensions/tree/main/bindings/env-configuration/python), and require Python 3.12 or later.

```python
from runtimeconditions_common_integrations import KeyValue, KeyValueEngine, cache

sessions = cache(KeyValue(engine=KeyValueEngine.REDIS))
```

## Validating extensions locally

From a sibling `go-rc-profiler` checkout:

```sh
cd ../go-rc-profiler
go run . validate-extensions -root ../extensions
```

Published extension identifiers are immutable semantic releases. A new semantic definition receives a new exact identifier; Runtime Conditions document `apiVersion` changes are independent.

## Contributing

See [`CONTRIBUTING.md`](https://github.com/runtimeconditions/extensions/tree/main?tab=contributing-ov-file) for the checks to run and the pull request conventions.
Never edit files under `bindings/` by hand change the emitter, the catalog entry, or the extension instead.


## Project status

Runtime Conditions is currently seeking adoption by an established parent project. The repositories in this organization are split for hands-on usability, review, demos, and implementation feedback. They are not intended to present Runtime Conditions as a standalone foundation or competing project.
