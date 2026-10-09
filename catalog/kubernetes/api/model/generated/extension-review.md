# Kubernetes API extension release review

**Classification: `accepted`**

The approved Kubernetes operation forms compile into an immutable extension release and every authoritative Kubernetes v1.36.2 operation validates against that release.

## Release

- Extension: `https://runtimeconditions.io/extensions/kubernetes/api/0.1.0/runtimeconditions.extension.yaml`
- Version: `0.1.0`
- Extension semantic SHA-256: `74325613d6fad555e7ab3fc195fe373b16c1f1a7ab77cedacee06a96c4353896`
- Service-mapping semantic SHA-256: `51e251ef09d58c2f8dc8c1e36033c060bfc8ef0fa1ebe19dc045b71c04303e66`
- Authoritative operations: 1123
- Resource operations: 1058
- Non-resource operations: 65
- Distinct condition operations: 1015
- Discoverable built-in resource selectors: 95

## Preserved semantics

- Resource verbs: `connect` 48, `create` 97, `delete` 87, `deletecollection` 86, `get` 133, `list` 129, `patch` 131, `update` 132, `watch` 215
- Connect HTTP methods: `delete` 6, `get` 9, `head` 6, `options` 6, `patch` 6, `post` 9, `put` 6
- Resource operations retain API group, API version, plural resource, access scope, and optional subresource.
- Connect operations additionally retain HTTP method rather than collapsing distinct connect endpoints.
- Non-resource operations retain canonical path and HTTP method.
- The resource-coordinate schema remains open to valid CRD group, version, resource, and subresource values; the built-in inventory is not a closed vocabulary enum.
- The service mapping includes a generated, language-neutral GVK-to-resource discovery catalog for built-in resources. It does not claim that unmodeled CRDs can be resolved without live discovery evidence.

## Maintainer review surface

Maintainers review the compact `model/service-operations-semantic-bridge.yaml` contract and this summary. The extension release and complete service mapping are deterministic machine outputs and are not line-by-line review surfaces.
