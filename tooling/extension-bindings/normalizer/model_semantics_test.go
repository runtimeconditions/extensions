package normalizer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func positiveConformanceModel(t *testing.T, name string) BindingModel {
	t.Helper()
	model, err := runConformanceCase(t, testSchemas(t), filepath.Join("..", "model", "conformance", "cases", name))
	if err != nil {
		t.Fatal(err)
	}
	return model
}

func findProperty(t *testing.T, shape Shape, name string) PropertyShape {
	t.Helper()
	for _, property := range shape.Properties {
		if property.Name == name {
			return property
		}
	}
	t.Fatalf("shape has no property %q", name)
	return PropertyShape{}
}

func TestConformanceModelsExerciseNormalizationRules(t *testing.T) {
	t.Run("fixed scope metadata is not caller supplied", func(t *testing.T) {
		model := positiveConformanceModel(t, "01-owned-kind-interface")
		projection := model.Schemas[0].Projection
		for _, property := range projection.Properties {
			if property.Name == "kind" {
				t.Fatal("fixed kind remained in the caller-supplied projection")
			}
		}
		interfaceProperty := findProperty(t, projection, "interface")
		for _, property := range interfaceProperty.Shape.Properties {
			if property.Name == "type" {
				t.Fatal("fixed interface type remained in the caller-supplied projection")
			}
		}
	})

	t.Run("dependency-owned declarations stay imported", func(t *testing.T) {
		model := positiveConformanceModel(t, "02-additive-field")
		if len(model.Vocabulary.OwnedDeclarations) != 0 {
			t.Fatal("additive root package incorrectly owns its dependency declaration")
		}
		if len(model.Vocabulary.ImportedDeclarations) != 1 || model.Vocabulary.ImportedDeclarations[0].Kind != "service" {
			t.Fatalf("imported declarations = %#v", model.Vocabulary.ImportedDeclarations)
		}
		if model.Scopes[0].Projection == nil {
			t.Fatal("scope has no effective additive projection")
		}
		findProperty(t, *model.Scopes[0].Projection, "credential")
	})

	t.Run("recursive references name only referenced definitions", func(t *testing.T) {
		model := positiveConformanceModel(t, "06-recursive-reference")
		if len(model.Schemas) != 1 || len(model.Schemas[0].Definitions) != 1 || model.Schemas[0].Definitions[0].Name != "node" {
			t.Fatalf("definitions = %#v", model.Schemas[0].Definitions)
		}
		node := model.Schemas[0].Definitions[0].Shape
		children := findProperty(t, node, "children")
		if children.Shape.Items == nil || children.Shape.Items.Kind != "ref" || children.Shape.Items.Ref != "#/$defs/node" {
			t.Fatal("recursive reference was not retained")
		}
	})

	t.Run("alternative requiredness is intersected", func(t *testing.T) {
		model := positiveConformanceModel(t, "07-object-alternatives")
		configuration := findProperty(t, model.Schemas[0].Projection, "configuration")
		if len(configuration.Shape.Required) != 0 {
			t.Fatalf("branch-specific requirements became structural: %v", configuration.Shape.Required)
		}
		findProperty(t, configuration.Shape, "command")
		findProperty(t, configuration.Shape, "image")
	})

	t.Run("heterogeneous alternatives remain a union", func(t *testing.T) {
		model := positiveConformanceModel(t, "08-heterogeneous-union")
		target := findProperty(t, model.Schemas[0].Projection, "target")
		if target.Shape.Kind != "union" || len(target.Shape.Variants) != 2 {
			t.Fatalf("target projection = %#v", target.Shape)
		}
	})

	t.Run("collections maps and array path segments stay explicit", func(t *testing.T) {
		model := positiveConformanceModel(t, "09-collections-and-maps")
		projection := model.Schemas[0].Projection
		if findProperty(t, projection, "tags").Shape.Kind != "array" ||
			findProperty(t, projection, "entries").Shape.Items.Kind != "object" ||
			findProperty(t, projection, "labels").Shape.Kind != "map" {
			t.Fatal("collection or map projection is incorrect")
		}
		value := findProperty(t, *findProperty(t, projection, "entries").Shape.Items, "value")
		if len(value.Shape.Values) != 2 {
			t.Fatalf("non-string enum values = %#v", value.Shape.Values)
		}
	})

	t.Run("value domains remain scoped", func(t *testing.T) {
		model := positiveConformanceModel(t, "10-scoped-domains-collisions")
		if len(model.Vocabulary.ValueDomains) != 2 ||
			model.Vocabulary.ValueDomains[0].InterfaceType == model.Vocabulary.ValueDomains[1].InterfaceType {
			t.Fatalf("scoped value domains = %#v", model.Vocabulary.ValueDomains)
		}
		for _, field := range model.Vocabulary.ConditionFields {
			if field.Path == "base_url" {
				return
			}
		}
		t.Fatal("base_url condition field was not retained")
	})
}

func TestProductionNormalizerHasNoFixtureOrCatalogDispatch(t *testing.T) {
	forbidden := []string{
		"aws-s3", "common-integrations", "env-configuration", "google-analytics",
		"kubernetes-api", "nats-service", "source-control", "urn:runtimeconditions:conformance:",
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		data, err := os.ReadFile(entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		for _, value := range forbidden {
			if strings.Contains(string(data), value) {
				t.Errorf("production file %s contains extension-specific dispatch token %q", entry.Name(), value)
			}
		}
	}
}

func TestExtractConstraintsSeparatesProjectionAndAnnotations(t *testing.T) {
	constraints := extractConstraints(map[string]any{
		"type":        "string",
		"required":    []any{"value"},
		"description": "human-readable annotation",
		"examples":    []any{"example"},
		"minLength":   int64(1),
		"pattern":     "^[a-z]+$",
	})
	if len(constraints) != 2 || constraints["minLength"] != int64(1) || constraints["pattern"] != "^[a-z]+$" {
		t.Fatalf("constraints = %#v", constraints)
	}
}
