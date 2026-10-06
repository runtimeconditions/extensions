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
			ID:             "https://runtimeconditions.io/schemas/profile/0.2.0/runtimeconditions.profile.schema.yaml",
			Version:        "0.2.0",
			SemanticSHA256: "a090a8016d045f9c3fa872a67f8df293b77ca2809a1bea5ae9fa31a27a06109a",
		},
		Normalizer: ToolIdentity{
			Name: NormalizerName, Version: NormalizerVersion, SHA256: strings.Repeat("d", 64),
		},
	}
}

func TestAlternativeStringConstantsShareOneDomain(t *testing.T) {
	provenance := Provenance{Coordinate: "https://new.example.test/future:1.0.0#schema:channel", JSONPointer: "/properties/interface"}
	shapes := []Shape{
		{Kind: "scalar", Scalar: "string", Values: normalizeValues([]any{"publish"}), Provenance: provenance},
		{Kind: "scalar", Scalar: "string", Values: normalizeValues([]any{"subscribe", "publish"}), Provenance: provenance},
	}
	merged := collapseEquivalentShapes(shapes, provenance)
	if merged.Kind != "scalar" || merged.Scalar != "string" || len(merged.Values) != 2 || len(merged.Variants) != 0 {
		t.Fatalf("alternative constants were not one domain: %+v", merged)
	}
	object := func(value string) Shape {
		return Shape{Kind: "object", Required: []string{"action"}, Properties: []PropertyShape{{Name: "action", Required: true, Shape: Shape{Kind: "scalar", Scalar: "string", Values: normalizeValues([]any{value}), Provenance: provenance}, Provenance: provenance}}, Provenance: provenance}
	}
	projection, err := mergeAlternativeShapes([]Shape{object("publish"), object("subscribe")}, provenance)
	if err != nil || len(projection.Properties) != 1 || projection.Properties[0].Shape.Kind != "scalar" || len(projection.Properties[0].Shape.Values) != 2 {
		t.Fatalf("object alternatives did not share the field domain: %+v %v", projection, err)
	}
	first, second := shapes[0], shapes[1]
	collections := []Shape{{Kind: "array", Items: &first, Provenance: provenance}, {Kind: "array", Items: &second, Provenance: provenance}}
	collection := collapseEquivalentShapes(collections, provenance)
	if collection.Kind != "array" || collection.Items == nil || collection.Items.Kind != "scalar" || len(collection.Items.Values) != 2 {
		t.Fatalf("nested alternative domains were not merged: %+v", collection)
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

func TestFieldValuesUseCompleteJSONSchemaValidation(t *testing.T) {
	id := "https://runtimeconditions.io/test/field-values-full-schema:1.0.0"
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
