# Validation record

## Authoritative extension generation

The external compiler reads AWS's public Smithy JSON AST model for `com.amazonaws.s3#AmazonS3`, applies the reviewed Service Operations Semantic Bridge, validates every Condition identity path through authoritative shapes, verifies the reviewed operation fingerprint, and emits deterministic extension and service-mapping artifacts.

The accepted source is the S3 model last changed by `aws/api-models-aws` commit `073f307ee1fd0acea67b706ddbd4ad5437c67eb8`. The model SHA-256 is `6975caa92319bf6c1fc2fdea7b3f64f9b9aca6d9b32edeccf086796338842e5a`.

```text
Smithy service shape: com.amazonaws.s3#AmazonS3
service version: 2006-03-01
canonical operations: 112
operation-name SHA-256: 209771cb0915567090f615e18dacf594436fc6aadc2867f56535ae0db7935436
Condition templates: 119
```

The extension semantic SHA-256 is `1a505b63d55893c26f3ffe6cf3cd9f90f0b5bd7975fabe47ff444a3ed1e13c72`. The generated release records the source repository, exact S3-changing commit, model path and digest, Smithy version, service shape, and service version. Operation schemas validate name-and-role pairs rather than independent global enums: for example, `PutObject` rejects a `source` role while `CopyObject` requires either `source` or `destination`.

## Authoritative versus SDK operations

Botocore 1.43.70 exposes 116 S3 client methods. Four are deprecated compatibility operations absent from the authoritative Smithy service closure. Reviewed SDK annotations map them to canonical extension operations:

| Botocore operation | Canonical extension operation |
| --- | --- |
| `GetBucketLifecycle` | `GetBucketLifecycleConfiguration` |
| `GetBucketNotification` | `GetBucketNotificationConfiguration` |
| `PutBucketLifecycle` | `PutBucketLifecycleConfiguration` |
| `PutBucketNotification` | `PutBucketNotificationConfiguration` |

SDK-to-extension alignment validates all 116 methods, 112 canonical operations, four aliases, and every Condition identity path against the botocore source model.

## Owner-aligned mapping validation

Static recursive validation checks unique mapping identities, declared dependencies, operation/waiter/call references, extension release identity and semantic digest, canonical Condition operations, client method projection, and dependency cycles.

```text
recursive SDK mapping validation passed
dependency order:
  botocore: botocore.aws.s3
  s3transfer: s3transfer.aws.s3
  boto3: boto3.aws.s3
```

Pinned source validation checks exact distribution versions, the 116-to-112 operation projection, Python spellings, boto3 resource inventory, handwritten signatures and delegates, s3transfer public entrypoints, receiver bindings, and implementation operation sets.

```text
pinned SDK source validation passed
  botocore SDK methods: 116
  canonical Smithy operations: 112
  boto3 handwritten wrapper surfaces: 17
  boto3 modeled resources: 18
  s3transfer public entrypoints: 9
  s3transfer distinct canonical operations: 19
```

## Historical Smithy evidence

The public repository contains 24 commits that changed the S3 model between May 2025 and August 2026. They produced six distinct operation inventories and five operation-set transitions. The exact commits and additions are retained in [`../evidence/smithy-history/history.md`](../evidence/smithy-history/history.md).

Every operation-set transition is a required extension-semantic review point. A model-only change is not automatically declared safe; the compiler also checks all reviewed shape paths and classifications against the selected model.

## Installed package proof

The existing Python packaging proof stages the owner mappings into local boto3, botocore, and s3transfer source trees, builds wheels without publishing to a registry, discovers the mappings through package metadata without SDK imports, validates the installed dependency closure, resolves representative recursive paths, and runs unchanged application fixtures.

## Remaining limitations

- The semantic-bridge compiler and normalized service-mapping contract are implemented externally but not yet packaged as a JVM `SmithyBuildPlugin` for AWS-owned integration.
- Cross-service request dependencies remain blocked until their target extensions and reviewed bridge mappings exist.
- Python profiler consumption is proved for the seven accepted AWS fixtures, while paginator, collection, transfer-class, and precise runtime-path selection remain outside that proof.
- Historical model inventory measures review opportunities; maintainer interview evidence is still required to measure the human cost of each semantic interruption.
