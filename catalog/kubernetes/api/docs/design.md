# Kubernetes API extension and SDK mapping design

## Purpose

The extension must describe what a workload requires from a Kubernetes API. It must not describe the Python client, prescribe kubeconfig or environment-variable conventions, assume that a cluster must be provisioned, or translate the requirement into a particular platform's identity and RBAC objects.

## Accepted condition

The initial typed-client call produces the following extension-defined condition operation:

```yaml
kind: kubernetes
interface:
  type: api
  operations:
  - verb: get
    apiGroup: ""
    apiVersion: v1
    resource: configmaps
    scope: namespaced
```

This shape is released locally as `kubernetes-api/0.1.0`. The structured operation is preferred over putting a generated Python method name or an OpenAPI `operationId` into the profile because group, version, resource, scope, and verb express Kubernetes API meaning across languages and can also represent custom resources. The SDK mapping still uses the authoritative `operationId` as its exact join between the OpenAPI model and a generated language symbol.

Concrete cluster endpoints, contexts, namespaces, object names, service accounts, tokens, certificates, and environment-variable names remain absent. Source code may contain some of those values, but their presence does not make them portable provisioning or workload-configuration requirements.

## Authoritative derivation

The Kubernetes v1.36.2 Swagger document exposes each built-in endpoint's path, HTTP method, `operationId`, `x-kubernetes-action`, and `x-kubernetes-group-version-kind`. The initial operation is `/api/v1/namespaces/{namespace}/configmaps/{name}` plus `GET`, with `operationId: readCoreV1NamespacedConfigMap`, action `get`, and the core/v1 `ConfigMap` GVK.

A deterministic neutral projection can preserve the built-in OpenAPI operation surface as follows, while the semantic bridge performs the Runtime Conditions translations in steps 4 through 6:

1. Retain the exact OpenAPI `operationId` as authoritative model identity while using path and method as the endpoint join through generator transformations that may rename it.
2. Derive API group and version from the endpoint path. Retain `x-kubernetes-group-version-kind` as request or response representation evidence because eviction, scale, and token subresources demonstrate that GVK may differ from the accessed endpoint.
3. Derive the plural resource and optional subresource from the path rather than pluralizing the kind.
4. The semantic bridge derives `namespaced`, `all_namespaces`, or `cluster` Condition access scope by comparing a preserved endpoint route with its resource family. A cluster-level list or watch of ConfigMaps is not equivalent to access within one namespace or to a cluster-scoped resource.
5. The semantic bridge maps authoritative Kubernetes actions to Condition verbs such as `get`, `list`, `watch`, `create`, `update`, `patch`, `delete`, and `deletecollection` rather than treating HTTP methods as sufficient semantics.
6. The bridge retains unusual connect, proxy, logs, status, scale, eviction, token, and other subresource operations as focused semantic decisions instead of silently forcing them into ordinary CRUD.

The projection must fail with a focused extension-review event when required model evidence is absent or a new operation category has no reviewed transformation rule.

## Non-resource APIs

The official client also exposes discovery, API-group, version, OpenID configuration, and other non-resource endpoints. A complete Kubernetes API extension cannot pretend those methods are resource operations or omit them merely because they do not translate to a namespaced object.

The likely `api` interface therefore needs two validated operation forms: the structured resource access shown above and a non-resource operation identified by canonical path and method or another maintainer-approved Kubernetes API identity. Generated OpenAPI operation IDs remain join keys, not necessarily adopter-facing profile values. The full projection must classify every generated public API operation into a reviewed resource, subresource, non-resource, or unsupported category and must stop for review when an operation is unclassified.

## Custom resources

The built-in OpenAPI document is authoritative for Kubernetes built-in APIs at the selected release, but Kubernetes API vocabulary is extensible at runtime through CustomResourceDefinitions. Closing extension validation over only the generated built-in groups, versions, and resources would make the extension incapable of expressing normal Kubernetes applications.

The extension should therefore validate the structure and syntax of operation coordinates while allowing custom group, version, resource, and subresource values. The generated built-in catalog remains authoritative input for official typed-client mappings and maintenance review; it is not the complete set of values that application declarations or dynamic-client mappings may express.

This also means that the extension and SDK mapping have different coverage boundaries. The extension can represent a CRD requirement before an official generated client exposes it. A typed-client mapping can map only methods actually present in its SDK release, while a dynamic-client mapping must prove its resource coordinates from application code or emit nothing.

## SDK mapping boundary

The official Python client transforms the authoritative OpenAPI model before OpenAPI Generator maps transformed operation IDs to generated API classes and snake_case methods. For the initial call, the SDK-owned artifact must join `CoreV1Api.read_namespaced_config_map` to transformed operation `readNamespacedConfigMap`, then join `GET /api/v1/namespaces/{namespace}/configmaps/{name}` to authoritative operation `readCoreV1NamespacedConfigMap` and the extension's structured Kubernetes API operation.

Constructing `CoreV1Api`, loading in-cluster configuration, or loading kubeconfig does not prove a resource operation by itself. The mapping must not emit a broad Kubernetes condition from those expressions.

The Python proof handles these materially different surfaces:

- A generated list method can perform a normal list or a watch depending on arguments and wrapper usage, so the mapping may require a source-proven conditional operation rather than one unconditional conclusion.
- `watch.Watch.stream` wraps a generated list method and must preserve the underlying resource while changing the required operation to watch where the call proves it.
- `DynamicClient.resources.get` produces a Resource whose later methods derive operations from discovered state. The service mapping therefore generates an unambiguous built-in GVK selector catalog from authoritative operations, while the SDK mapping source-verifies the producer and Resource proxy. Statically resolved built-in selectors can produce state; unmodeled CRDs and unresolved selectors emit nothing because source does not prove their plural name or scope.
- Dynamic custom-resource SDK methods remain distinct mapping records. The method and route fix the base verb, scope, and optional subresource; the three list methods have the same explicit `watch=true` override as typed list methods. A shared generation rule may bind group, version, and resource arguments across those records but must never turn them into one operation with combinatorial behavior.
- Utility, configuration, discovery, serialization, and model-only calls do not necessarily prove resource access.

These are Kubernetes SDK questions, not reasons to add Python-specific vocabulary to the extension.

## Expected SDK-author burden

For the generated typed client, the intended integration is a code-generation plugin or adjacent deterministic build step that consumes the same OpenAPI model and symbol table already used to generate the client. Maintainers should not annotate every generated method or review a generated operation table.

Human-authored SDK metadata should be limited to public handwritten wrappers, aliases, conditional behavior, and surfaces whose semantics are absent from the generator model. Normal patch releases should regenerate and validate without a manual mapping edit.

The local proof ships static YAML metadata inside the ordinary distribution. It adds no Runtime Conditions runtime dependency and changes no application-facing API; a version-aligned companion remains a governance alternative if upstream maintainers prefer it.

## Initial acceptance result

The first slice demonstrates all of the following:

- The unchanged ConfigMap reader resolves `CoreV1Api.read_namespaced_config_map` to the structured core/v1 ConfigMap get operation.
- Client construction and configuration calls alone emit no condition.
- The mapping is generated from the authoritative Kubernetes OpenAPI model and the official Python generator surface rather than a handwritten method table.
- The mapping records the exact owning Python distribution version and exact target extension semantics without adding an application compatibility lock.
- A generated mapping is packaged and discovered statically without importing or executing the Kubernetes SDK.
- At least one historical or subsequent Python client release is regenerated to measure actual maintenance work.
- Generated YAML is validated mechanically and summarized for review rather than presented to maintainers for line-by-line approval.

## Expansion result

The generated typed-client expansion, list-versus-watch behavior, separate custom-resource helpers, package integration, `Watch.stream` delegation, DynamicClient base-resource state flow, real-profiler proof, and four-release historical replay are implemented. Dynamic subresources, ResourceList fan-out, constructor-only discovery traffic, and resource coordinates available only from live CRD discovery remain explicit boundaries. Those limitations do not close the extension vocabulary: extension-provided declarations and future statically proved mappings may still express valid custom-resource coordinates.
