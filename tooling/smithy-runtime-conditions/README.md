# Runtime Conditions Smithy pipeline

This project proves an externally maintainable workflow that AWS can later integrate into its internal Smithy and SDK generators. It consumes AWS's public Smithy JSON AST models, applies small Service Operations Semantic Bridges, validates reviewed semantics against authoritative shapes, and emits deterministic extension and language-neutral service-mapping artifacts.

The implementation is service-independent. Amazon S3 is the first acceptance case; adding a service supplies a manifest and semantic bridge rather than another service-specific operation-table generator.

## Inputs and outputs

```text
authoritative Smithy model
  + reviewed Service Operations Semantic Bridge
  -> immutable extension definition
  -> language-neutral service mapping
  -> focused semantic review report
```

[`tools/compile_extension.py`](tools/compile_extension.py) is the external compiler. It requires PyYAML but no Runtime Conditions runtime or SDK import. The bridge references the authoritative Smithy repository, model path, and service shape directly, and records only the Runtime Conditions translation decisions that Smithy does not own. A future AWS-owned integration can consume equivalent annotations through a native `SmithyBuildPlugin` and join the normalized output to each language generator's authoritative symbol provider.

Install the authoring-only dependency with `python3 -m pip install -r extensions/tooling/smithy-runtime-conditions/requirements.txt`; generated extensions and consuming applications gain no Python dependency.

Runtime Conditions-owned manifests, semantic bridges, mappings, indexes, state, and evidence use YAML. The compiler accepts JSON only at the boundary where AWS publishes an authoritative Smithy JSON AST; consuming that upstream representation does not create a second first-party serialization contract.

[`tools/run_maintenance.py`](tools/run_maintenance.py) resolves the commit that last changed a selected service model, runs the compiler, compares deterministic output with the accepted release, and classifies the observation as `automatic`, `extension-review-required`, or `invalid`.

[`tools/replay_history.py`](tools/replay_history.py) inventories every public model-changing commit for a selected service. It reports operation-set transitions as mandatory semantic review points without claiming that model-only changes are automatically safe.

## Review boundary

Mechanically derived operation inventories, shape paths, provenance, field-value enums, validation schemas, mappings, and digests are generated. Humans review only interface classification, resource identity, roles, secondary resources, cross-service dependencies, stable higher-level intents, and representative profile meaning.

The operation-set fingerprint is an adoption gate, not a profile field. A changed authoritative inventory produces a focused review event and never silently approves new extension semantics.
