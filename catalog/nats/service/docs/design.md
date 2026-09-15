# NATS extension design

## Dependency boundary

A NATS client call proves a dependency on a NATS service and may additionally prove subject authorization or a JetStream resource action. The extension records those environment-facing facts without recording Go package names, receiver types, callback signatures, internal `$JS.API` subjects, connection environment variables, authentication mechanisms, or server topology.

The `nats` kind identifies the provider and protocol family. The `service` interface reflects that Core NATS and JetStream capabilities are enabled and authorized through one NATS account even when the application addresses several subjects or resources.

## Service inventory and semantic bridge

NATS uses a hand-maintained [`service-operations-inventory.yaml`](https://github.com/runtimeconditions/service-operations-inventories/blob/main/nats/service-operations-inventory.yaml) because this workflow does not yet consume an authoritative machine-readable NATS operation model comparable to Smithy or OpenAPI. The inventory contains only service-owned operations, inputs, input requiredness, and value shapes. It is neither generated from one language SDK nor copied for each language SDK.

The reviewed [`service-operations-semantic-bridge.yaml`](../model/service-operations-semantic-bridge.yaml) references the exact inventory digest and translates service operations into Condition semantics. It selects the Condition kind and interface, maps service inputs to Condition fields, and decides which fields are required or optional in a valid profile. The bridge is the RC-specific authoring artifact; the inventory does not mention extensions, Conditions, adapters, SDKs, or profiles.

The inventory gives every operation one stable `resource.action` name. That name is an authoring and generation reference, not a field emitted in an application profile. Language SDK integrations cannot redefine the bridge's meaning for `stream.create` or manufacture new Condition resource/action combinations without first changing and versioning the extension semantics.

- A connection operation proves only that the workload needs a reachable NATS service.
- A subject operation distinguishes publish, subscribe, and request because they require different authorization. Request is not collapsed into publish because it also requires a reply inbox.
- Stream actions distinguish management and inspection from publishing. Stream publishing can be proven from a subject even when the application does not know the owning stream name.
- Consumer actions retain the stream and, when source proves it, the consumer name.
- Key/value and object-store actions retain bucket identity and distinguish management, inspection, reading, writing, and watching.

These are separate array items validated by separate JSON Schema branches. The extension schema and machine-consumable service mapping are generated from the exact inventory plus reviewed semantic bridge, so service facts, translation decisions, and validation rules cannot drift silently. An SDK mapping binds a concrete call to exactly one stable service operation; it does not select a resource/action combination from arbitrary values at profiling time.

## Cross-language boundary

The inventory and generated service mapping contain no language symbols. Go's `Conn.Publish`, a JavaScript client's publish function, and a future Java method may all reference `subject.publish`, but each language retains its own symbol identity, argument binding, state propagation, and wrapper behavior. Cross-language reuse applies to external-service meaning; it does not presume that SDK object models or call graphs are equivalent.

## Deliberate omissions

The extension does not declare credentials, environment-variable names, URLs, accounts, clusters, replicas, storage classes, retention policy, maximum sizes, or timeouts unless application source and future adapter review establish that they belong in a portable demand. Creating a resource through the SDK proves a management authorization requirement; it does not tell an adapter to create a duplicate resource.

SDK methods that merely configure local client behavior emit no condition. Calls whose resource or subject cannot be statically resolved emit no inferred operation; application developers can use extension-provided no-op bindings when their organization requires an explicit declaration.
