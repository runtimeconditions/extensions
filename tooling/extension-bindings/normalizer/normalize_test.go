package normalizer

import (
	"bytes"
	"context"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func testSchemas(t *testing.T) *Schemas {
	t.Helper()
	schemas, err := LoadSchemas(
		filepath.Join("..", "model", "runtimeconditions.extension-semantic.schema.yaml"),
		filepath.Join("..", "model", "runtimeconditions.binding-model.schema.yaml"),
	)
	if err != nil {
		t.Fatalf("load schemas: %v", err)
	}
	return schemas
}

func testNormalizeConfig() NormalizeConfig {
	return NormalizeConfig{
		CoreProfileSchema: CoreProfileIdentity{
			ID:             "urn:runtimeconditions:test:core-profile-schema",
			Version:        "0.0.0-test",
			SemanticSHA256: strings.Repeat("c", 64),
		},
		Normalizer: ToolIdentity{
			Name: NormalizerName, Version: NormalizerVersion, SHA256: strings.Repeat("d", 64),
		},
	}
}

func TestNormalizeEveryCatalogExtension(t *testing.T) {
	schemas := testSchemas(t)
	resolver, err := NewResolver(ResolverConfig{
		Schemas: schemas,
		CatalogRoots: []string{
			filepath.Join("..", "..", "..", "catalog"),
		},
	})
	if err != nil {
		t.Fatalf("index catalog: %v", err)
	}
	ids := make([]string, 0, len(resolver.candidates))
	for id := range resolver.candidates {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		t.Fatal("catalog contains no extension definitions")
	}
	for _, id := range ids {
		t.Run(id, func(t *testing.T) {
			closure, err := resolver.Resolve(context.Background(), id)
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}
			model, err := Normalize(closure, BuildDependencyLock(closure), schemas, testNormalizeConfig())
			if err != nil {
				t.Fatalf("normalize: %v", err)
			}
			first, err := CanonicalModelYAML(model)
			if err != nil {
				t.Fatalf("serialize: %v", err)
			}
			second, err := CanonicalModelYAML(model)
			if err != nil {
				t.Fatalf("serialize again: %v", err)
			}
			if string(first) != string(second) {
				t.Fatal("canonical model serialization is not deterministic")
			}
			for _, forbidden := range [][]byte{[]byte("sourceSha256"), []byte("sourceBackend"), []byte("sourceLocator")} {
				if bytes.Contains(first, forbidden) {
					t.Fatalf("semantic model contains resolution field %q", forbidden)
				}
			}
			positions := map[string]int{}
			for index, extension := range model.Extensions {
				positions[extension.ID] = index
			}
			for _, edge := range model.DependencyEdges {
				if positions[edge.To] >= positions[edge.From] {
					t.Fatalf("dependency %s does not precede dependent %s", edge.To, edge.From)
				}
			}
		})
	}
}

func TestTokenizerIsMechanical(t *testing.T) {
	tests := map[string][]string{
		"baseUrl":        {"base", "url"},
		"HTTPServer2URL": {"http", "server", "2", "url"},
		"key_value":      {"key", "value"},
		"9patch":         {"9", "patch"},
		"café":           {"caf", "uC3A9"},
	}
	for input, expected := range tests {
		actual := tokenize(input)
		if strings.Join(actual, ",") != strings.Join(expected, ",") {
			t.Errorf("tokenize(%q) = %v, want %v", input, actual, expected)
		}
	}
}

func TestFieldValuesUseCompleteJSONSchemaValidation(t *testing.T) {
	id := "urn:runtimeconditions:test:field-values-full-schema"
	document := fixtureExtension(id, nil, `  kinds:
    - name: service
  fieldValues:
    - field: code
      targetKind: service
      values: [UPPERCASE]
  schemas:
    - id: service
      description: Field value validation fixture.
      appliesToKind: service
      schema:
        type: object
        properties:
          code:
            type: string
            pattern: '^[a-z]+$'
`)
	directory := t.TempDir()
	writeFixture(t, directory, "extension.yaml", document)
	schemas := testSchemas(t)
	resolver, err := NewResolver(ResolverConfig{Schemas: schemas, CatalogRoots: []string{directory}})
	if err != nil {
		t.Fatal(err)
	}
	closure, err := resolver.Resolve(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Normalize(closure, BuildDependencyLock(closure), schemas, testNormalizeConfig())
	if err == nil || diagnosticCode(t, err) != "RCB1310" {
		t.Fatalf("expected RCB1310, got %v", err)
	}
}
