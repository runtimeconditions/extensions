# Kubernetes API and Python client provenance

## Why two source models are necessary

The Runtime Conditions extension and the Python SDK mapping have different semantic owners. The extension follows the authoritative Kubernetes API release through its Service Operations Semantic Bridge. The SDK mapping follows the exact transformed model and source emitted by the Kubernetes Python client repository and references the resulting service mapping.

For the first proof, Python client 36.0.3 identifies Kubernetes API v1.36.2 in its changelog. Kubernetes v1.36.2 resolves to commit `24e2b02af5543d7910c2bb074c7264df5a8f0467`. Python client v36.0.3 resolves to commit `67e7d9abfc6fe6629fa650d9b0abf4c99ef8c39c`.

## Authoritative equivalence

The Kubernetes source file and the Python repository's retained `kubernetes/swagger.json.unprocessed` file have different byte SHA-256 values because their JSON serialization differs. Parsed as JSON, they have the same semantic SHA-256, `ca58855c8fe1774f8859e957ec94ebb41b016ea726e986f166460eefce488cfd`. Their paths, definitions, parameters, and operations are semantically equal.

This means an SDK release can prove which authoritative API semantics it consumed without requiring identical whitespace, key order, or source-file bytes. Both raw and semantic digests remain recorded so a semantic comparison never hides provenance.

## Python generator transformation

The authoritative model contains 1,123 operations over 562 paths. The Python repository's retained generator input at `scripts/swagger.json` contains 936 operations over 572 paths. The generator pipeline removes the 215 dedicated watch and watch-list endpoints, normalizes two non-resource paths, adds 28 parameterized custom-object endpoints, and renames 903 of the 906 otherwise shared endpoint operation IDs before OpenAPI Generator converts them to Python classes and snake_case methods. The transformed document also reuses some operation IDs across API tags and versions, so endpoint plus generated owning class—not transformed operation ID alone—identifies a Python public surface.

For example, the authoritative operation `readCoreV1NamespacedConfigMap` and the transformed Python operation `readNamespacedConfigMap` share `GET /api/v1/namespaces/{namespace}/configmaps/{name}`. The published method is `CoreV1Api.read_namespaced_config_map`. The safe join is therefore authoritative endpoint semantics to the transformed generator endpoint and then to the generator's language symbol, not an assumption that operation IDs remain identical.

The 28 generator-injected dynamic endpoints consist of 27 distinct custom-resource methods and one API-discovery method. Each method remains a separate mapping record with a fixed base verb, scope, and optional subresource derived from its public method and route; the three list methods have one explicit source-proven `watch=true` override. Group, version, and plural resource are argument bindings rather than options on one combinatorial operation. A shared generator rule may emit these separate records, but application source must still prove the required resource coordinates before a record emits a concrete Runtime Condition.

## Maintenance ownership

Extension automation watches Kubernetes API releases and uses the semantic bridge to classify changes in authoritative resource, subresource, access-scope, non-resource, watch, and connect semantics. Python SDK automation watches Python client releases, verifies that the retained unprocessed model still joins semantically to the accepted service mapping, and projects generator transformations plus handwritten wrappers into version-owned mappings. A new API operation can leave the extension unchanged when it uses existing Condition vocabulary, but every Python release that exposes it still regenerates its SDK mapping.

Kubernetes Python maintainers should not maintain 936 method mappings. A generator integration can emit the generated surface mechanically from the retained processed model. Human-authored SDK metadata should be limited to custom-object argument bindings, handwritten watch behavior, aliases, and other public semantics absent from the generator input.
