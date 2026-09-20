package normalizer

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestCanonicalJSONRFC8785NumberFormatting(t *testing.T) {
	value := []any{333333333.33333329, 1e30, 4.50, 2e-3, 1e-27, 0.000001, 1e-7, -0.0}
	actual, err := canonicalJSON(value)
	if err != nil {
		t.Fatal(err)
	}
	want := `[333333333.3333333,1e+30,4.5,0.002,1e-27,0.000001,1e-7,0]`
	if string(actual) != want {
		t.Fatalf("canonical numbers = %s, want %s", actual, want)
	}
}

func TestCanonicalJSONStringAndUTF16KeyOrdering(t *testing.T) {
	value := map[string]any{
		"\ue000": "<>&\u2028",
		"😀":      "line\n\t\x00",
	}
	actual, err := canonicalJSON(value)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\"😀\":\"line\\n\\t\\u0000\",\"\ue000\":\"<>&\u2028\"}"
	if string(actual) != want {
		t.Fatalf("canonical strings = %q, want %q", actual, want)
	}
}

func TestYAMLParserRejectsUnsafeOrAmbiguousDocuments(t *testing.T) {
	tests := map[string]string{
		"duplicate key":  "key: first\nkey: second\n",
		"custom tag":     "key: !custom value\n",
		"multiple docs":  "key: first\n---\nkey: second\n",
		"non-string key": "1: value\n",
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseYAMLData([]byte(input)); err == nil {
				t.Fatal("expected parser rejection")
			}
		})
	}
}

func TestCanonicalModelYAMLRestrictedProfile(t *testing.T) {
	model, err := runConformanceCase(t, testSchemas(t), filepath.Join("..", "model", "conformance", "cases", "01-owned-kind-interface"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := CanonicalModelYAML(model)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "\r\n") || !strings.HasSuffix(string(data), "\n") || strings.HasSuffix(string(data), "\n\n") {
		t.Fatal("canonical YAML line-ending contract was not met")
	}
	if strings.Contains(string(data), "sourceSha256:") || strings.Contains(string(data), "sourceBackend:") || strings.Contains(string(data), "sourceLocator:") {
		t.Fatal("canonical model contains source resolution identity")
	}
}
