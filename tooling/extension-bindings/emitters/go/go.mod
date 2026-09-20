module github.com/runtimeconditions/extensions/tooling/extension-bindings/emitters/go

go 1.22

require (
	github.com/runtimeconditions/extensions/tooling/extension-bindings/normalizer v0.0.0
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.3
	gopkg.in/yaml.v3 v3.0.1
)

require golang.org/x/text v0.14.0 // indirect

replace github.com/runtimeconditions/extensions/tooling/extension-bindings/normalizer => ../../normalizer
