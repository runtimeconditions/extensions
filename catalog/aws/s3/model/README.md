# S3 semantic inputs and generated owner mappings

This directory separates authoritative AWS service semantics from language-specific SDK behavior and generated artifacts.

## Reviewed inputs

[`service-operations-semantic-bridge.yaml`](service-operations-semantic-bridge.yaml) references AWS's authoritative `com.amazonaws.s3#AmazonS3` Smithy service and defines only the operations requiring Runtime Conditions classification, identity, role, or secondary-resource exceptions. It does not duplicate the authoritative service model.

[`botocore-sdk-annotations.yaml`](botocore-sdk-annotations.yaml) maps four deprecated botocore compatibility operations to canonical Smithy operations. These aliases are Python SDK surface, not extension vocabulary.

[`boto3-wrapper-annotations.yaml`](boto3-wrapper-annotations.yaml) describes only public boto3 factories, aliases, handwritten managed-transfer wrappers, and handwritten resource loads absent from generated SDK models.

[`s3transfer-semantic-annotations.yaml`](s3transfer-semantic-annotations.yaml) describes public transfer entrypoints, argument bindings, classic and CRT implementations, mutually exclusive execution paths, and conditional service operations.

The semantic bridge and SDK annotations do not contain profiler coverage percentages, unresolved application observations, fixed environment variables, credentials, Regions, endpoints, or adapter policy.

## Generated outputs

[`generated/s3-service-mapping.yaml`](generated/s3-service-mapping.yaml) is generated from AWS's authoritative Smithy model plus the reviewed semantic bridge. It contains 112 canonical operations and 119 S3 Condition templates.

[`generated/smithy-review.md`](generated/smithy-review.md) is the focused generation summary. It records exact upstream provenance, semantic digests, interface counts, and operation-set changes without requiring review of generated YAML.

The owner generator then produces three independently versioned Python artifacts under `../mappings`: botocore owns low-level client surfaces, s3transfer owns managed-transfer behavior, and boto3 owns factories, resources, and handwritten wrappers.

## Regeneration

Obtain a full or history-capable checkout of `https://github.com/aws/api-models-aws.git`, then run:

```sh
python3 extensions/tooling/smithy-runtime-conditions/tools/run_maintenance.py \
  --manifest extensions/catalog/aws/s3/maintenance/smithy.yaml \
  --models-root /absolute/path/to/api-models-aws \
  --extensions-root extensions \
  --output /tmp/aws-s3-extension-maintenance
```

The accepted semantic digests must match the candidate semantic digests. Candidate bytes may differ only in upstream provenance when a newer authoritative model remains semantically compatible; provenance-only drift does not mutate the immutable accepted release or require a new version. An operation or shape change not covered by the reviewed semantic bridge produces `extension-review-required`, and the runner never edits or approves the bridge.

Generate Python owner mappings from pinned SDK sources with [`../tools/generate_owner_mappings.py`](../tools/generate_owner_mappings.py). The generator requires the authoritative service mapping, the botocore service/paginator/waiter models, the boto3 resource model, all three reviewed SDK annotation files, and exact distribution versions.

Both compilers read models as static data and never import or execute the SDK packages whose metadata they generate.
