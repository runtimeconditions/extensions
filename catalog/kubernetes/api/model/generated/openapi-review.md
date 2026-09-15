# Kubernetes OpenAPI source review

**Classification: `investigation`**

The authoritative Kubernetes OpenAPI operation surface normalized deterministically without adding Runtime Conditions semantics.

## Authoritative input

- Repository: `https://github.com/kubernetes/kubernetes.git`
- Revision: `24e2b02af5543d7910c2bb074c7264df5a8f0467`
- Release ref: `v1.36.2`
- Path: `api/openapi-spec/swagger.json`
- Source SHA-256: `dcede2063da1d7ad62ecb5af8adb6d7fabd0b52385a7fa0048afb491dac90450`
- Source semantic SHA-256: `ca58855c8fe1774f8859e957ec94ebb41b016ea726e986f166460eefce488cfd`
- Operation IDs: 1123
- Operation-ID SHA-256: `f32fcf3c6d90729c067ef7df8b7ece4e9c9cc52a7a29958d1df6519e0471e15d`
- Projection semantic SHA-256: `bf8209aaddb164672b0630658212a67030c2096adee3ecdde27ac986c4a91b46`

## Source projection

- Resource operations: 1058
- Non-resource operations: 65
- Resource families: 95 (43 namespaced, 52 cluster-scoped)
- Route scopes: `cluster` 571, `namespaced` 487
- Endpoint/GVK group-version differences requiring the endpoint coordinates to remain authoritative: 14

| Authoritative Kubernetes action | Operations |
| --- | ---: |
| `connect` | 48 |
| `delete` | 87 |
| `deletecollection` | 86 |
| `get` | 133 |
| `list` | 129 |
| `patch` | 131 |
| `post` | 97 |
| `put` | 132 |
| `watch` | 87 |
| `watchlist` | 128 |

## Representative operation

`readCoreV1NamespacedConfigMap` is `get` on the `core/v1` endpoint resource `configmaps` with a `namespaced` route.

## Findings that affect extension design

- The neutral projection preserves whether each authoritative endpoint route is namespaced or cluster-level. Runtime Conditions access-scope semantics are assigned later by the Service Operations Semantic Bridge.
- The endpoint group and version identify the accessed API resource. GVK identifies the request or response representation and differs for eviction, scale, and token subresources, so it cannot replace endpoint coordinates.
- Dedicated watch and watch-list actions remain distinct authoritative source values in the neutral projection. Their translation to the same Runtime Conditions `watch` semantic belongs to the bridge.
- Non-resource endpoints remain classified without being forced into Runtime Conditions resource coordinates.
- Connect operations preserve the authoritative Kubernetes action and HTTP method; the bridge decides their adapter-facing representation.

## Human review surface

Review authoritative operation counts, endpoint structure, Kubernetes actions, group/version/kind evidence, and source drift. Runtime Conditions verbs, access scopes, and adapter impact belong to the separately reviewed Service Operations Semantic Bridge.
