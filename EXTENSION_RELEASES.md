# Extension release and compatibility contract

## Purpose

An extension release is an immutable semantic vocabulary artifact. Its Runtime Conditions `apiVersion` identifies the document schema understood by validators; it does not identify the extension's semantic release.

## Identity and versioning

- A published extension identifier MUST resolve permanently to the same semantic definition.
- A semantic change MUST produce a new extension identifier. Existing identifiers are never repointed or overwritten.
- Provider extensions SHOULD encode a Semantic Versioning 2.0.0 release in the identifier and repeat it as `metadata.version` so humans and automation can distinguish document schema version from extension semantic version.
- An additive vocabulary or validation change increments the minor version, a meaning-changing removal or incompatible reclassification increments the major version, and a correction that does not change accepted vocabulary or profile meaning increments the patch version.
- During pre-adoption development, `0.x.y` releases may replace earlier experimental conventions without compatibility promises. Before public adoption, the same immutability and compatibility rules apply to every published identifier.

The AWS S3 candidate therefore uses `https://runtimeconditions.io/aws/aws-s3:0.1.0` while retaining `apiVersion: runtimeconditions.io/v1alpha1` as the Runtime Conditions document schema version.

## Authoritative provenance

Generated provider extensions record the authoritative model repository, the exact commit that last changed the selected service model, the model path, the model SHA-256 digest, the Smithy service shape, and the service version. Unrelated commits elsewhere in an upstream model repository do not create extension releases.

The upstream model revision is provenance, not the extension version. Many model revisions may produce no Runtime Conditions semantic change, while one extension release may support many SDK languages and versions.

## One extension to many SDK mappings

The dependency is directional: an SDK mapping names the exact extension release it targets, while the extension does not enumerate every language or SDK version that consumes it. A mapping records its owning distribution version, mapping-contract version, target extension identifier, target extension semantic digest, and authoritative service-mapping digest.

An SDK mapping can use a subset of operations defined by its target extension. This allows an immutable additive extension release to support SDKs that have not yet exposed every authoritative API operation. An SDK operation absent from the selected extension, after applying reviewed SDK compatibility aliases, is an extension-alignment review event.

Ordinary package dependency resolution selects compatible SDK packages. Each installed package supplies metadata for the behavior it owns, and recursive mapping validation verifies the selected graph. No handwritten Cartesian compatibility matrix is maintained.

## Maintenance classifications

- `automatic`: the authoritative model or fallback inventory, reviewed Service Operations Semantic Bridge, generated extension, service mapping, SDK surfaces, and package proof remain aligned.
- `extension-review-required`: authoritative API semantics cannot be represented safely by the selected extension release.
- `sdk-review-required`: the extension remains sufficient, but a language-specific SDK surface, wrapper, dependency, or package integration changed.
- `invalid`: the automation failed or received unsupported input before semantic ownership could be classified safely.

Extension and SDK review events are measured separately so extension-author burden is not attributed to SDK maintainers and automation defects are not attributed to either group.

## Human review surface

Humans review semantic bridge changes, added or removed operations, resource classifications, identity paths, roles, cross-service dependencies, representative profile changes, and adapter-facing impact. Generated extension YAML, language-neutral mappings, and language-specific SDK mappings are machine output and are not line-by-line review surfaces.

## HTTPS catalog lookup

A definition uses `metadata.uri` and `metadata.version`; its identifier is
`<uri>:<version>`. The canonical URI has `/<provider>/<service>` or `/<service>`.
The versioned artifact is retrieved from
`https://<domain>/extensions/<provider>/<service>/<version>/runtimeconditions.extension.yaml`.
Providerless identities use `rc` for lookup without changing the declared URI.
A missing definition is not found; no alternate path or provider is tried.
Only HTTPS retrieval is currently supported. `file:` and `oci:` are deferred.
