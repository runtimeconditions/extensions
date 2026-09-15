# AWS S3 extension design

## Design priority

1. An application using a mapped SDK should generate an understandable profile without changing its source.
2. A Condition must say enough for an adapter to make a useful fulfillment decision without claiming facts the source did not prove.
3. Service semantics should be maintained once and reused by every language-specific SDK mapping.
4. SDK and extension releases must be independently maintainable while retaining an exact, mechanically verifiable relationship.
5. Manual declarations remain typed fallback mechanisms when static SDK detection cannot prove a requirement.

## Layer responsibilities

| Layer | Owns | Does not own |
| --- | --- | --- |
| AWS S3 extension | S3 kind, interface types, canonical operation vocabulary, schemas, validation, immutable semantic releases | SDK symbols, detection policy, credentials, provisioning |
| Service Operations Semantic Bridge | Reviewed translation from Smithy operations to Condition classification, resource identity paths, roles, and secondary dependencies | SDK-specific aliases and public names |
| Service mapping | Deterministic projection from authoritative Smithy operations to extension Condition templates | New Condition vocabulary, coverage reporting, adapter policy |
| Language SDK mapping | Versioned SDK construction patterns, public symbols, wrappers, and SDK compatibility aliases | Independent S3 semantics |
| Language profiler | Static discovery and profile generation | AWS semantics absent from resolved mappings |
| Adapter | S3 fulfillment, authorization translation, and platform wiring | Reinterpreting invalid extension vocabulary |

The extension must exist before an SDK mapping can conform to it. The mapping may emit only vocabulary validated by an exact immutable extension release.

## Condition shape

`kind` identifies the AWS S3 integration family. `interface.type` identifies the resource-facing surface required by the workload.

### Existing bucket demand

```yaml
kind: aws.s3
interface:
  type: bucket
  operations:
    - name: PutObject
```

One Condition represents one distinguishable S3 bucket requirement. `operations[].name` contains canonical S3 API operations invoked against that bucket. The Condition does not carry a concrete runtime bucket name.

### Service-level demand

```yaml
kind: aws.s3
interface:
  type: service
  operations:
    - name: CreateBucket
```

This avoids claiming that an existing bucket should be provisioned for code whose purpose is to create buckets. It also covers account-wide listing operations without manufacturing a bucket Condition.

### Object Lambda response demand

```yaml
kind: aws.s3
interface:
  type: object_lambda
  operations:
    - name: WriteGetObjectResponse
```

`WriteGetObjectResponse` operates against route and request-token context supplied to an Object Lambda function. The separate interface type preserves that runtime contract.

## Operation representation

Canonical S3 operation objects are the selected minimum:

```yaml
operations:
  - name: CopyObject
    role: source
```

The canonical name is the fact most directly proved by an SDK call. The `role` distinguishes source and destination bucket Conditions only for operations whose reviewed semantics define those roles; generated schemas reject a role on any other operation and require it when an operation such as `CopyObject` would otherwise be ambiguous. Object entries permit future proven request semantics without replacing a string array later.

Broad capabilities such as `read`, `write`, `list`, and `admin` collapse materially different operations and create another taxonomy for extension authors to maintain. IAM actions are fulfillment instructions and do not map one-to-one to SDK behavior. HTTP methods and routes lose meaning across S3 endpoint forms. Desired features such as versioning or encryption may be useful later, but only when source analysis proves a desired state an adapter can act upon. None is a better primary source fact than the canonical API operation.

## Authentication and configuration

Creating an AWS SDK client does not prove `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_REGION`, a bucket environment variable, or any other delivery mechanism. AWS SDKs can use explicit settings, environment variables, shared files, web identity, container or instance providers, and other credential sources with precedence rules.

Listing every supported setting as optional would still create misleading application inputs and repeat configuration that belongs to a shared SDK client or execution identity. The current decision is therefore not that AWS settings can never be represented; it is that an S3 operation mapping does not prove a settings delivery mechanism.

A later AWS SDK Configuration extension could describe source-proven SDK settings and identity requirements at the client level. An application declaration could separately describe a workload-specific input such as `UPLOAD_BUCKET`. Both can compose with S3 Conditions without making configuration mandatory for ordinary SDK mapping.

## Complete service vocabulary versus complete request semantics

The extension contains the 112 canonical operations in the authoritative AWS Smithy service:

- 108 operations primarily target an existing bucket;
- `CreateBucket`, `ListBuckets`, and `ListDirectoryBuckets` require the service surface;
- `WriteGetObjectResponse` requires the Object Lambda surface.

The generated service mapping has 119 Condition templates: one primary template per canonical operation plus seven conditional secondary-bucket templates.

Some S3 request shapes also reference KMS, IAM, SNS, SQS, Lambda, or S3 Tables resources. Those are real possible runtime dependencies, but an S3 extension cannot own their vocabulary. Complete cross-service request semantics depends on those extensions existing and on source analysis proving the nested values.

botocore currently adds four deprecated compatibility operation names. Those remain SDK aliases that resolve to canonical operations and do not become extension operations.

## Identity and aggregation

The extension validates one Condition, the service mapping identifies where resource identity originates, and the profiler decides static grouping.

- Two configured clients do not automatically collapse into one Condition.
- One client can address more than one bucket.
- Calls against the same statically identifiable bucket can share a Condition whose operation list is their union.
- A dynamic bucket expression may prevent reliable grouping.
- Concrete bucket names or ARNs remain outside the portable profile.

Generated `identity` paths are analysis instructions, not profile fields.

## Release and maintenance contract

The accepted extension is published at an immutable versioned identifier and carries a semantic digest. The authoritative Smithy revision and source digest are provenance, not the extension's compatibility identity.

An SDK mapping declares the exact extension identifier, version, and semantic digest it targets. Many SDK mappings and SDK versions can target one extension release. A new SDK release alone does not require a new extension release. A semantic extension change does require a new immutable extension release and regeneration or validation of every mapping that claims compatibility with it.

Across extensions, a new service operation can map to existing Condition vocabulary without changing the extension. Every SDK release that exposes that operation must still regenerate its language mapping so the new public method references the canonical service operation. For this S3 extension, canonical operation names are deliberately adapter-actionable authorization vocabulary, so adding an S3 operation name normally changes the extension; that S3-specific consequence is not a universal semantic-bridge rule.

The maintenance lanes are intentionally separate:

1. The extension lane watches authoritative Smithy model changes and stops for focused semantic review when the accepted semantic bridge no longer covers the model.
2. Each SDK lane watches its own releases, binds public symbols and SDK-only aliases to the accepted canonical service mapping, and stops for SDK-specific review when that binding changes.
3. The join gate rejects an SDK mapping when its canonical operations, input paths, extension identity, or semantic digest do not match the selected extension release.

## Acceptance criteria

1. Every authoritative Smithy operation has a reviewed classification and valid identity path.
2. Every selected SDK operation resolves to one canonical extension operation, including declared SDK compatibility aliases.
3. Higher-level SDK mappings reference owner-qualified lower-level behavior without duplicating extension semantics.
4. The unmodified direct-client fixture can ultimately generate one `aws.s3` / `bucket` Condition containing `{name: PutObject}`.
5. Generated profiles contain no invented credentials, environment variables, Region, endpoint, concrete bucket value, coverage field, or unresolved observation.
6. Invalid operations, roles, paths, references, extension identities, or semantic digests fail validation.
7. A Smithy semantic change stops extension maintenance until a maintainer accepts a new immutable extension release.
8. An SDK release with unchanged bindings and semantics regenerates without handwritten changes.
9. No Runtime Conditions runtime dependency is added to an SDK or application.
