# AWS S3 extension candidate

## Status

**Authoritative-model maintenance experiment — not yet an AWS-published extension.**

This directory contains an AWS-specific Runtime Conditions extension, its externally maintained Service Operations Semantic Bridge, generated language-neutral service mapping, and owner-aligned Python SDK mappings. A downstream adapter may fulfill the requirement with a compatible implementation, but the extension vocabulary describes Amazon S3 rather than a generic object store.

## Condition shape

```yaml
kind: aws.s3
interface:
  type: bucket
  operations:
    - name: PutObject
```

This says that the workload requires an S3 bucket and invokes the canonical S3 `PutObject` operation against it. It does not name the bucket, select an AWS account or Region, prescribe credentials, or declare environment-variable names.

Service-level operations use `interface.type: service`. `WriteGetObjectResponse` uses `interface.type: object_lambda` because its request route and token context are not an ordinary bucket identity.

Operation entries identify canonical S3 API operations, not IAM actions. An adapter may translate operations and source-proven request features into permissions, provisioning behavior, and platform-specific bindings.

## Authoritative semantic source

AWS's public [`api-models-aws`](https://github.com/aws/api-models-aws) Smithy repository is the authoritative operation source. [`model/service-operations-semantic-bridge.yaml`](model/service-operations-semantic-bridge.yaml) references that source directly and defines only the Runtime Conditions decisions that the Smithy model does not own.

The compiler under [`../../../tooling/smithy-runtime-conditions`](../../../tooling/smithy-runtime-conditions/) produces both:

- [`releases/0.1.0/runtimeconditions.extension.yaml`](releases/0.1.0/runtimeconditions.extension.yaml), the immutable `0.1.0` extension semantic release; and
- [`model/generated/s3-service-mapping.yaml`](model/generated/s3-service-mapping.yaml), the language-neutral projection consumed by SDK-language mapping generators.

The accepted authoritative model contains 112 canonical S3 operations: 108 bucket operations, three service operations, and one Object Lambda response operation. Seven additional Condition templates represent secondary S3 buckets in request inputs.

The earlier botocore-derived extension listed 116 operations. The four additional names—`GetBucketLifecycle`, `GetBucketNotification`, `PutBucketLifecycle`, and `PutBucketNotification`—are deprecated botocore compatibility surfaces absent from the authoritative Smithy operation closure. They now remain in [`model/botocore-sdk-annotations.yaml`](model/botocore-sdk-annotations.yaml) and resolve to their canonical extension operations instead of expanding extension vocabulary.

## One extension, many SDK mappings

The extension does not enumerate supported languages or SDK releases. Each mapping identifies its exact owning distribution version and exact target extension release.

- [`mappings/botocore/runtimeconditions.sdk-mapping.yaml`](mappings/botocore/runtimeconditions.sdk-mapping.yaml) owns Python low-level client methods, paginators, waiters, and terminal Condition templates.
- [`mappings/s3transfer/runtimeconditions.sdk-mapping.yaml`](mappings/s3transfer/runtimeconditions.sdk-mapping.yaml) owns managed-transfer calls and execution paths.
- [`mappings/boto3/runtimeconditions.sdk-mapping.yaml`](mappings/boto3/runtimeconditions.sdk-mapping.yaml) owns factories, resources, relations, and handwritten wrappers.

The tested Python graph contains 116 botocore SDK methods aligned to 112 canonical Smithy operations, eight paginators, four waiters, 18 boto3 resources, 71 resource actions, 37 relations, four collections, six resource waiters, 17 managed-transfer wrapper surfaces, and nine public s3transfer entrypoints.

An older SDK may use a subset of operations in this extension release. A newer SDK operation that cannot be aligned after applying reviewed SDK compatibility aliases stops with `extension-review-required`.

## Maintenance automation

[`maintenance/smithy.yaml`](maintenance/smithy.yaml) declares the authoritative model, semantic bridge, generated release, and accepted service mapping. [`.github/workflows/smithy-maintenance.yml`](../.github/workflows/smithy-maintenance.yml) checks the public model daily, validates pull requests, retains evidence, and creates one deduplicated issue when extension review is required.

[`evidence/smithy-history/history.md`](evidence/smithy-history/history.md) inventories the public S3 model history. The first 24 model-changing commits produced six distinct operation inventories and five operation-set transitions, establishing concrete historical extension-review points without claiming that every model-only change is semantically irrelevant.

## Human-authored inputs

- [`model/service-operations-semantic-bridge.yaml`](model/service-operations-semantic-bridge.yaml) owns S3 interface classification, resource identity paths, source/destination roles, secondary buckets, extension identity, and the reviewed operation fingerprint while referencing AWS's Smithy model as the operation authority.
- [`model/botocore-sdk-annotations.yaml`](model/botocore-sdk-annotations.yaml) owns deprecated Python SDK compatibility aliases absent from authoritative Smithy vocabulary.
- [`model/boto3-wrapper-annotations.yaml`](model/boto3-wrapper-annotations.yaml) owns handwritten boto3 surfaces absent from its resource model.
- [`model/s3transfer-semantic-annotations.yaml`](model/s3transfer-semantic-annotations.yaml) owns public transfer entrypoints and implementation paths absent from the service model.

Generated YAML is not a line-by-line human review surface. Maintainers review the semantic bridge diff, focused model summary, representative profile changes, and adapter-facing impact.

## Authentication and configuration

Creating or calling an AWS SDK client does not prove a workload-facing environment-variable convention, credential source, Region, account, or endpoint. Those concerns remain absent until application source or a separate additive extension proves a portable requirement.

## Remaining boundary

Some S3 request shapes refer to KMS keys, IAM roles, SNS topics, SQS queues, Lambda functions, or S3 Tables. Those are potential cross-service Runtime Conditions, but the S3 extension cannot own their vocabulary. They become expressible only after the corresponding extensions and reviewed cross-service bridge mappings exist.
