# SDK author workflow for the AWS S3 extension

## Goal

SDK maintainers should maintain only the behavior their repository owns, reuse existing service and code-generation models, and review concise generated summaries. They should never hand-maintain complete operation tables or generated mapping YAML.

The Python proof uses three owner-aligned artifacts because boto3, botocore, and s3transfer version and publish different public behavior. An SDK repository that owns all equivalent layers may keep those references within one artifact.

## Shared service semantics

AWS's public Smithy model supplies the authoritative S3 operation and shape inventory. [`../model/service-operations-semantic-bridge.yaml`](../model/service-operations-semantic-bridge.yaml) references that model and supplies only Runtime Conditions decisions that cannot be inferred safely: extension identity, default bucket classification, service and Object Lambda exceptions, resource identity paths, source/destination roles, secondary buckets, and the reviewed operation fingerprint.

The shared compiler generates the immutable extension definition and language-neutral service mapping once. A new SDK language consumes that output and does not reproduce S3 semantics by hand.

## Python-owned semantics

[`../model/botocore-sdk-annotations.yaml`](../model/botocore-sdk-annotations.yaml) maps deprecated botocore compatibility methods absent from the authoritative Smithy closure to canonical extension operations. An SDK alias remains SDK metadata and does not expand extension vocabulary.

[`../model/boto3-wrapper-annotations.yaml`](../model/boto3-wrapper-annotations.yaml) contains only factories, aliases, injected client and resource transfer helpers, transfer-class methods, and handwritten resource loads absent from boto3's resource model.

[`../model/s3transfer-semantic-annotations.yaml`](../model/s3transfer-semantic-annotations.yaml) contains public transfer entrypoints grouped into logical calls, their argument bindings, receiver-held configuration, classic and CRT implementations, mutually exclusive paths, and conditional canonical operation references.

Runtime branches remain behavioral mapping facts. They do not become profile coverage fields or unresolved observations; profilers and their callers decide how to handle source that cannot prove a branch.

## Generated owner graph

```text
boto3 factory/resource/wrapper
  -> botocore client operation or waiter
  -> exact AWS S3 extension release

boto3 managed-transfer wrapper
  -> s3transfer logical call and selected execution path
  -> botocore client operations
  -> exact AWS S3 extension release
```

Each mapping records its owning distribution, exact version, and SDK mapping-contract version. The terminal botocore service mapping records the target extension identifier and semantic digest plus the authoritative service-mapping digest; higher-level mappings reach that contract through required owner-qualified dependencies. The complete directed graph is validated recursively.

## First repository integration

For each owning distribution, a maintainer would:

1. Add or adopt the small reviewed semantic bridge for behavior absent from generated service models and SDK annotations for behavior absent from generated SDK models.
2. Enable the Runtime Conditions projection in the repository's existing model or code-generation workflow.
3. Include `runtimeconditions/index.yaml` and `runtimeconditions/mappings/*.yaml` as static package data or in an automatically installed version-aligned companion artifact.
4. Run authoritative-extension alignment, SDK-source, recursive-reference, package, and representative application gates.
5. Review only the authored semantic bridge or SDK annotation diff, focused generated summary, representative profile change, and adapter-facing impact.
6. Publish static metadata through the SDK's normal release lifecycle without adding a Runtime Conditions runtime dependency.

Application source and runtime behavior remain unchanged.

## Normal maintenance

- A package-only release regenerates automatically; distribution versions are derived from immutable source rather than maintained in the semantic bridge or SDK annotations.
- A Smithy operation or resource-semantic change is routed as `extension-review-required` before an SDK mapping update can be accepted.
- A generated language spelling or model-surface change updates mechanically and is checked against exact source.
- A handwritten wrapper signature, delegate, or implementation-path change is routed as `sdk-review-required` with the affected SDK annotation and source diagnostic.
- An automation or unsupported-input failure is `invalid` and is not counted as extension- or SDK-maintainer work.
- Repeated observations of one semantic fingerprint are deduplicated into one maintenance item.

An older SDK may map a subset of operations defined by an additive extension release. The extension never lists every consuming language or SDK version, and maintainers never maintain a Cartesian compatibility matrix.

## Application developer experience

The application developer uses and versions the SDK normally, runs the language profiler locally or in CI, and reviews the generated profile when desired. Recognized SDK calls require no Runtime Conditions dependency, mapping file, evidence file, compatibility lock, or source annotation.

When mapping metadata is absent or static detection is incomplete, extension-provided no-op declarations and project-local overrides remain the escape hatches. Application developers are not asked to become SDK mapping authors.

## Decisions outside SDK mapping authorship

SDK mappings do not decide how incomplete detection is handled, whether CI accepts a profile, what an adapter provisions, how IAM is rendered, which credential source is used, which Region or account is selected, which environment variables exist, or which compatible provider implementation fulfills a Condition.

## Current proof boundary

Authoritative Smithy generation, immutable extension identity, Python owner alignment, local packaging, static discovery, recursive validation, and unchanged application fixtures are implemented. The Python profiler now consumes the three owner mappings across all seven applications without adding SDK annotations or application declarations; the exact supported constructs and incomplete-source decisions remain an experimental consumer contract rather than a cross-language standard.
