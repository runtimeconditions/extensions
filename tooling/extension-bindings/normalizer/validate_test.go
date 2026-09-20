package normalizer

import "testing"

func TestBindingExposedNamesRejectASCIIHyphen(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		pointer string
	}{
		{
			name:    "kind",
			yaml:    "spec:\n  kinds:\n    - name: data-store\n",
			pointer: "/spec/kinds/0/name",
		},
		{
			name:    "schema property",
			yaml:    "spec:\n  kinds:\n    - name: service\n  schemas:\n    - id: service\n      description: service\n      appliesToKind: service\n      schema:\n        type: object\n        properties:\n          base-url: {type: string}\n",
			pointer: "/spec/schemas/0/schema/properties/base-url",
		},
		{
			name:    "referenced definition",
			yaml:    "spec:\n  kinds:\n    - name: tree\n  schemas:\n    - id: tree\n      description: tree\n      appliesToKind: tree\n      schema:\n        type: object\n        properties:\n          root: {$ref: '#/$defs/tree-node'}\n        $defs:\n          tree-node: {type: string}\n",
			pointer: "/spec/schemas/0/schema/$defs/tree-node",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := []byte("apiVersion: runtimeconditions.io/v1alpha1\nkind: RuntimeConditionsExtensionDefinition\nmetadata:\n  id: urn:runtimeconditions:test:hyphen\n" + test.yaml)
			definition, mapping, err := DecodeExtension(data)
			if err != nil {
				t.Fatal(err)
			}
			err = testSchemas(t).ValidateExtension(mapping, definition)
			if err == nil || diagnosticCode(t, err) != "RCB1116" {
				t.Fatalf("expected RCB1116, got %v", err)
			}
			diagnosticError := err.(*DiagnosticError)
			if diagnosticError.Diagnostic.JSONPointer != test.pointer {
				t.Fatalf("pointer = %q, want %q", diagnosticError.Diagnostic.JSONPointer, test.pointer)
			}
		})
	}
}

func TestBindingNameRestrictionDoesNotApplyToIdentifiersOrValues(t *testing.T) {
	data := []byte("apiVersion: runtimeconditions.io/v1alpha1\nkind: RuntimeConditionsExtensionDefinition\nmetadata:\n  id: urn:runtimeconditions:test:hyphen-id\nspec:\n  kinds:\n    - name: service\n  fieldValues:\n    - field: mode\n      targetKind: service\n      values: [direct-mode]\n")
	definition, mapping, err := DecodeExtension(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := testSchemas(t).ValidateExtension(mapping, definition); err != nil {
		t.Fatal(err)
	}
}
