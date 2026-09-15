# Generated boto3-owned S3 mapping

[`runtimeconditions.sdk-mapping.yaml`](runtimeconditions.sdk-mapping.yaml) is generated static metadata for the public S3 behavior owned by boto3 1.43.70.

It contains top-level and Session client/resource factories, the complete 18-resource S3 construction graph, 71 modeled resource actions, 37 relations, four collections, six resource waiters, five injected client transfer helpers, ten Bucket/Object helpers, and two `boto3.s3.transfer.S3Transfer` methods.

It deliberately does not copy botocore's canonical operation templates or s3transfer's implementation behavior. Those are independently owned and versioned, and this mapping uses owner-qualified references to the sibling [`botocore`](../botocore/) and [`s3transfer`](../s3transfer/) artifacts.

Maintainers review the boto3 resource-model diff, [`../../model/boto3-wrapper-annotations.yaml`](../../model/boto3-wrapper-annotations.yaml), source-validation output, and representative resolutions. The generated file is not a line-by-line review surface.

The local wheel proof, recursive discovery, release-maintenance tools, and seven-application profiler integration live under [`../../../../sdk/authorship/aws-python`](../../../../sdk/authorship/aws-python/).
