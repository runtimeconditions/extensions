# Amazon S3 API analysis

## Purpose and authoritative source

This analysis asks one question: what Runtime Condition can an S3 SDK operation truthfully imply before any language-specific mapping is written?

The canonical service input is AWS's public Smithy model at `models/s3/service/2006-03-01/s3-2006-03-01.json` in [`aws/api-models-aws`](https://github.com/aws/api-models-aws). The accepted model revision, source digest, operation fingerprint, and generated semantic digest are recorded in [`../model/generated/smithy-review.md`](../model/generated/smithy-review.md).

The accepted Smithy service contains 112 canonical operations. It describes the S3 data-plane and bucket-control API, not the separate S3 Control service.

botocore 1.43.70 exposes 116 S3 client methods. Four are compatibility aliases for canonical operations that remain in the SDK but are absent from the authoritative service model:

| SDK operation | Canonical operation |
| --- | --- |
| `GetBucketLifecycle` | `GetBucketLifecycleConfiguration` |
| `GetBucketNotification` | `GetBucketNotificationConfiguration` |
| `PutBucketLifecycle` | `PutBucketLifecycleConfiguration` |
| `PutBucketNotification` | `PutBucketNotificationConfiguration` |

Those aliases are SDK facts and live in [`../model/botocore-sdk-annotations.yaml`](../model/botocore-sdk-annotations.yaml). They do not expand extension vocabulary.

## Semantic classification

The `Bucket` input member alone does not prove that an existing bucket is the runtime dependency. `CreateBucket` accepts a bucket name, but the bucket does not exist before the call. `ListBuckets` and `ListDirectoryBuckets` are also service-scoped.

The reviewed extension semantics classify the authoritative operations as follows:

| Runtime demand | Count | Condition |
| --- | ---: | --- |
| Existing bucket is the primary operation target | 108 | `aws.s3` / `bucket` |
| Service-level S3 access | 3 | `aws.s3` / `service` |
| Object Lambda response context | 1 | `aws.s3` / `object_lambda` |

`WriteGetObjectResponse` passes a transformed object back to an active `GetObject` request through an Object Lambda access point. Its required route and request token are invocation context, not an ordinary bucket identifier, so the extension gives it a distinct interface type.

This is extension vocabulary. It is not a mapping coverage field, profile diagnostic, or unresolved-observation mechanism.

## Why operation names are necessary but insufficient

For the basic case, `PutObject` proves two stable facts: the workload needs an S3 bucket and invokes the canonical `PutObject` operation. The extension records those facts and no more.

An API operation name is useful to an adapter, but it is not an IAM policy declaration. AWS authorization requirements can vary with request features, encryption choices, endpoint forms, and service behavior. The adapter translates extension semantics into platform-specific permissions; the SDK mapping must not manufacture request facts the application source does not prove.

Several operations demonstrate why the Smithy model still needs a reviewed Service Operations Semantic Bridge:

- `CopyObject` and `UploadPartCopy` target a destination bucket and reference a source object through another input path. The mapping emits source and destination bucket templates with explicit roles.
- Bucket configuration calls can contain references to other buckets, KMS keys, IAM roles, SNS topics, SQS queues, or Lambda functions. Those secondary resources are nested request semantics, not consequences of an operation name.
- S3 calls can use bucket names, access points, directory buckets, or other endpoint forms. The SDK method alone does not prove the selected form.

Incomplete detection remains a concern for application developers, platforms, profilers, and downstream tooling. The mapping does not publish a percentage or an unresolved-observation list.

## Consequences for the architecture

1. The extension owns resource scope, canonical operation vocabulary, schemas, and validation.
2. The service mapping projects authoritative Smithy operations and reviewed input paths into extension-owned Condition templates.
3. SDK mappings bind versioned public SDK surfaces to canonical operations and retain SDK-only compatibility aliases.
4. A new or removed authoritative operation stops extension generation until its semantics are reviewed and a new immutable extension release is accepted.
5. An SDK release can change independently without creating a new extension release when it continues to resolve to the same accepted canonical semantics.

## Secondary S3 buckets

The generated service mapping contains seven additional conditional bucket templates for S3 references nested outside the primary `Bucket` input:

| Operation | Secondary bucket purpose |
| --- | --- |
| `CopyObject` | Copy source |
| `UploadPartCopy` | Copy source |
| `PutBucketAnalyticsConfiguration` | Analytics export destination |
| `PutBucketInventoryConfiguration` | Inventory destination |
| `PutBucketLogging` | Logging destination |
| `PutBucketReplication` | Replication destination |
| `RestoreObject` | Select restore output destination |

These templates are emitted only when the nested input is present and statically identifiable. References to other AWS services require vocabulary owned by their respective extensions.

[write-response]: https://docs.aws.amazon.com/AmazonS3/latest/API/API_WriteGetObjectResponse.html
