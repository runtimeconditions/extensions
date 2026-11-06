# Tagging the Go modules

The orchestrator is imported by `go-rc-cli`, so its in-repo dependencies must resolve from tags.
Push the tags in this order, each from the commit that should be released:

1. `tooling/extension-bindings/normalizer/v0.1.0`
2. `tooling/extension-bindings/emitters/go/v0.1.0`
3. `tooling/extension-bindings/orchestrator/v0.1.0`

`emitters/go` requires `normalizer`, and `orchestrator` requires both, so a later tag fails to resolve until the earlier ones exist.
The `replace` directives stay for local development; Go ignores them in imported modules.
`structural-model/go` has no in-repo dependencies and is not imported by these modules.
