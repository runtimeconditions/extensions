# NATS service extension

## Status

**Initial semantic candidate for SDK-authorship and adapter review.**

This extension describes the smallest NATS distinctions that can change an adapter decision: connecting to a NATS service, publishing, subscribing or requesting on a subject, and managing or using JetStream streams, consumers, key/value buckets, and object stores. It deliberately does not copy the `nats.go` method inventory into extension vocabulary.

The language-neutral source is [`runtimeconditions/service-operations-inventories/nats/service-operations-inventory.yaml`](https://github.com/runtimeconditions/service-operations-inventories/blob/main/nats/service-operations-inventory.yaml). A Service Operations Inventory is the fallback when a service does not provide an adequate authoritative machine-readable operation model such as Smithy or OpenAPI. It defines one cohesive inventory of stable service operations, inputs, input requiredness, and service-domain shapes without referencing Runtime Conditions.

[`model/service-operations-semantic-bridge.yaml`](model/service-operations-semantic-bridge.yaml) is the reviewed Runtime Conditions authoring surface. It references the exact inventory semantic digest and maps inventory operations and inputs to adapter-actionable NATS Condition semantics. [`tools/compile_extension.py`](tools/compile_extension.py) deterministically emits both the immutable extension release and [`model/generated/nats-service-mapping.yaml`](model/generated/nats-service-mapping.yaml) from the inventory plus bridge. Every NATS SDK mapping should reference the same generated service mapping rather than reproduce its operation definitions.

## Adapter-actionable minimum

An operation is retained only when it can change service enablement, NATS subject authorization, JetStream API authorization, or resource provisioning and policy. Retry options, callback shapes, asynchronous return types, message encoding, client buffering, and other SDK behavior are not extension semantics unless a downstream adapter would act differently because of them.

The semantic bridge selects one `kind: nats` condition and `interface.type: service`. Each stable inventory name such as `subject.publish`, `stream.create`, or `object_store.watch` maps to one fixed Condition operation form. It is not a runtime-selected resource/action combination. “Service operation” is used instead of “endpoint” because integrations such as NATS are not expressed solely as HTTP endpoints.

The inventory, semantic bridge, and generated service mapping are language-neutral; none describes public SDK methods. They contain no Go packages, JavaScript modules, classes, methods, arguments, or return types. A Go, Python, JavaScript, Java, or other NATS SDK supplies only the language-specific symbols, field bindings, state flow, and delegation needed to reach these shared operations.

## Build

From the extensions repository root:

```sh
python3 catalog/nats/service/tools/compile_extension.py --inventory ../service-operations-inventories/nats/service-operations-inventory.yaml --bridge catalog/nats/service/model/service-operations-semantic-bridge.yaml --extension-output catalog/nats/service/releases/0.1.0/runtimeconditions.extension.yaml --service-mapping-output catalog/nats/service/model/generated/nats-service-mapping.yaml
```

## Current review boundary

The current neutral inventory contains the 26 operations used by this first cross-language experiment. It must be reviewed and expanded from authoritative NATS protocol, server API, and schema sources before it is presented as a complete NATS service inventory. That inventory work is independent of deciding which operations produce adapter-actionable Runtime Conditions.

The semantic bridge action groupings are intentionally smaller than any language SDK surface and larger than individual server protocol subjects. They must be reviewed with NATS maintainers and adapter authors before public release. The existing Go and Python applications demonstrate that each currently aligned Condition semantic can produce a valid profile, but they do not establish that every grouping is sufficiently precise for production authorization and provisioning.
