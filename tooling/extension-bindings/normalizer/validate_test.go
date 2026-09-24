package normalizer

import "testing"

func TestLanguageSpecificEmittersMayHandleHyphens(t *testing.T) {
	data := []byte("apiVersion: runtimeconditions.io/v1alpha1\nkind: RuntimeConditionsExtensionDefinition\nmetadata:\n  id: urn:runtimeconditions:test:hyphen-id\nspec:\n  kinds:\n    - name: service\n  fieldValues:\n    - field: mode\n      targetKind: service\n      values: [direct-mode]\n")
	definition, mapping, err := DecodeExtension(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := testSchemas(t).ValidateExtension(mapping, definition); err != nil {
		t.Fatal(err)
	}
}
