# Generated Kubernetes API model artifacts

[`generated/kubernetes-v1.36-openapi-projection.yaml`](generated/kubernetes-v1.36-openapi-projection.yaml) is deterministic RC-neutral machine output from Kubernetes v1.36.2's authoritative Swagger document. It contains every API operation and preserves the OpenAPI operation ID, path, method, Kubernetes action, GVK, endpoint coordinates, subresource, and route scope. It does not contain Condition verbs, Condition access scopes, extension coordinates, or profile semantics, and it is not a maintained Service Operations Inventory.

[`generated/openapi-review.md`](generated/openapi-review.md) is the human review surface. Maintainers review counts, semantic categories, design findings, and representative meaning rather than the 1,123 generated entries.

[`service-operations-semantic-bridge.yaml`](service-operations-semantic-bridge.yaml) pins the authoritative OpenAPI source and contains the compact reviewed translation from source actions and routes to Runtime Conditions verbs and access scopes. [`generated/extension-review.md`](generated/extension-review.md), [`generated/kubernetes-service-mapping.yaml`](generated/kubernetes-service-mapping.yaml), and [`../releases/0.1.0/runtimeconditions.extension.yaml`](../releases/0.1.0/runtimeconditions.extension.yaml) are deterministic outputs.
