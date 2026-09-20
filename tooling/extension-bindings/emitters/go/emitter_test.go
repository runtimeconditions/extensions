package goemitter

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/runtimeconditions/extensions/tooling/extension-bindings/normalizer"
	"gopkg.in/yaml.v3"
)

func TestGoNamingRules(t *testing.T) {
	tests := map[string]struct {
		tokens []string
		want   string
	}{
		"initialisms":   {[]string{"http", "server", "2", "url"}, "HTTPServer2URL"},
		"leading digit": {[]string{"9", "patch"}, "X9Patch"},
		"encoded UTF-8": {[]string{"caf", "uC3A9"}, "CafUc3a9"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if actual := pascal(test.tokens); actual != test.want {
				t.Fatalf("pascal(%v) = %q, want %q", test.tokens, actual, test.want)
			}
		})
	}
}

func TestEmitterRejectsInvalidInputs(t *testing.T) {
	model := loadExpectedModel(t, "01-owned-kind-interface")
	target := loadTestTarget(t, "01-owned-kind-interface.yaml")
	tests := map[string]func() error{
		"missing model field": func() error {
			changed := model
			changed.RootExtension.ID = ""
			return Emit(changed, target, filepath.Join(t.TempDir(), "output"))
		},
		"unsupported model API": func() error {
			changed := model
			changed.APIVersion = "runtimeconditions.io/binding-model/v2"
			return Emit(changed, target, filepath.Join(t.TempDir(), "output"))
		},
		"unknown structural node": func() error {
			changed := model
			changed.Scopes = append([]normalizer.ScopeModel(nil), model.Scopes...)
			projection := *changed.Scopes[0].Projection
			projection.Kind = "mystery"
			changed.Scopes[0].Projection = &projection
			return Emit(changed, target, filepath.Join(t.TempDir(), "output"))
		},
		"mismatched target": func() error {
			changed := target
			changed.RootExtension = "urn:runtimeconditions:other"
			return Emit(model, changed, filepath.Join(t.TempDir(), "output"))
		},
		"unsupported Go version": func() error {
			changed := target
			changed.MinimumGoVersion = "1.21"
			return Emit(model, changed, filepath.Join(t.TempDir(), "output"))
		},
	}
	for name, run := range tests {
		t.Run(name, func(t *testing.T) {
			if err := run(); err == nil {
				t.Fatal("expected deterministic rejection")
			}
		})
	}

	output := t.TempDir()
	if err := os.WriteFile(filepath.Join(output, "existing"), []byte("occupied"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Emit(model, target, output); err == nil || !strings.Contains(err.Error(), "output directory must be empty") {
		t.Fatalf("non-empty output error = %v", err)
	}
}

func TestLoadersRejectUnknownFields(t *testing.T) {
	modelData, err := os.ReadFile(expectedModelPath("01-owned-kind-interface"))
	if err != nil {
		t.Fatal(err)
	}
	modelPath := filepath.Join(t.TempDir(), "model.yaml")
	if err := os.WriteFile(modelPath, append(modelData, []byte("unknownField: true\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadModel(modelPath); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown model field error = %v", err)
	}

	targetData, err := os.ReadFile(testTargetPath("01-owned-kind-interface.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	targetPath := filepath.Join(t.TempDir(), "target.yaml")
	if err := os.WriteFile(targetPath, append(targetData, []byte("unknownField: true\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPackageTarget(targetPath); err == nil || !strings.Contains(err.Error(), "unknown package target field") {
		t.Fatalf("unknown package-target field error = %v", err)
	}
}

func TestLoadModelRejectsMissingRequiredFields(t *testing.T) {
	modelData, err := os.ReadFile(expectedModelPath("01-owned-kind-interface"))
	if err != nil {
		t.Fatal(err)
	}
	base, err := normalizer.ParseYAMLData(modelData)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		pointer string
		remove  func(map[string]any)
	}{
		{
			name:    "top-level object",
			pointer: "/vocabulary",
			remove: func(model map[string]any) {
				delete(model, "vocabulary")
			},
		},
		{
			name:    "nested object",
			pointer: "/metadata/normalizer",
			remove: func(model map[string]any) {
				delete(model["metadata"].(map[string]any), "normalizer")
			},
		},
		{
			name:    "nested scalar",
			pointer: "/metadata/normalizer/version",
			remove: func(model map[string]any) {
				normalizerData := model["metadata"].(map[string]any)["normalizer"].(map[string]any)
				delete(normalizerData, "version")
			},
		},
		{
			name:    "array member field",
			pointer: "/extensions/0/semanticSha256",
			remove: func(model map[string]any) {
				extension := model["extensions"].([]any)[0].(map[string]any)
				delete(extension, "semanticSha256")
			},
		},
		{
			name:    "declaration provenance field",
			pointer: "/vocabulary/ownedDeclarations/0/provenance/coordinate",
			remove: func(model map[string]any) {
				vocabulary := model["vocabulary"].(map[string]any)
				declaration := vocabulary["ownedDeclarations"].([]any)[0].(map[string]any)
				provenance := declaration["provenance"].(map[string]any)
				delete(provenance, "coordinate")
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			model := cloneDocument(t, base)
			test.remove(model)
			data, err := yaml.Marshal(model)
			if err != nil {
				t.Fatal(err)
			}
			modelPath := filepath.Join(t.TempDir(), "model.yaml")
			if err := os.WriteFile(modelPath, data, 0o600); err != nil {
				t.Fatal(err)
			}
			_, err = LoadModel(modelPath)
			if err == nil || !strings.Contains(err.Error(), "RCG1021") || !strings.Contains(err.Error(), test.pointer) {
				t.Fatalf("missing model field error = %v, want RCG1021 at %s", err, test.pointer)
			}
		})
	}
}

func TestEmitRejectsMissingNestedModelField(t *testing.T) {
	model := loadExpectedModel(t, "01-owned-kind-interface")
	model.Metadata.Normalizer.Version = ""
	err := Emit(model, loadTestTarget(t, "01-owned-kind-interface.yaml"), filepath.Join(t.TempDir(), "output"))
	if err == nil || !strings.Contains(err.Error(), "RCG1021") || !strings.Contains(err.Error(), "/metadata/normalizer/version") {
		t.Fatalf("missing model field error = %v", err)
	}
}

func TestLoadModelRejectsMissingStringValueTokens(t *testing.T) {
	modelData, err := os.ReadFile(expectedModelPath("10-scoped-domains-collisions"))
	if err != nil {
		t.Fatal(err)
	}
	model, err := normalizer.ParseYAMLData(modelData)
	if err != nil {
		t.Fatal(err)
	}
	vocabulary := model["vocabulary"].(map[string]any)
	domain := vocabulary["valueDomains"].([]any)[0].(map[string]any)
	value := domain["values"].([]any)[0].(map[string]any)
	delete(value, "tokens")
	data, err := yaml.Marshal(model)
	if err != nil {
		t.Fatal(err)
	}
	modelPath := filepath.Join(t.TempDir(), "model.yaml")
	if err := os.WriteFile(modelPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = LoadModel(modelPath)
	pointer := "/vocabulary/valueDomains/0/values/0/tokens"
	if err == nil || !strings.Contains(err.Error(), "RCG1021") || !strings.Contains(err.Error(), pointer) {
		t.Fatalf("missing string value tokens error = %v, want RCG1021 at %s", err, pointer)
	}
}

func cloneDocument(t *testing.T, value map[string]any) map[string]any {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestFixedSymbolCollisionHasExactDiagnostic(t *testing.T) {
	fixture := filepath.Join("testdata", "negative", "fixed-symbol-collision")
	model := normalizeFixture(t, fixture, "urn:runtimeconditions:conformance:go-fixed-symbol-collision")
	target, err := LoadPackageTarget(filepath.Join(fixture, "package-target.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	err = Emit(model, target, filepath.Join(t.TempDir(), "output"))
	var diagnosticError *DiagnosticError
	if !errors.As(err, &diagnosticError) {
		t.Fatalf("error = %T %v, want DiagnosticError", err, err)
	}
	actual, err := yaml.Marshal(diagnosticError.Diagnostic)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := os.ReadFile(filepath.Join(fixture, "diagnostic.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(actual) != string(expected) {
		t.Fatalf("diagnostic:\n%s\nwant:\n%s", actual, expected)
	}
}

func TestSameDomainMemberCollisionFails(t *testing.T) {
	model := loadExpectedModel(t, "10-scoped-domains-collisions")
	for index := range model.Vocabulary.ValueDomains {
		if model.Vocabulary.ValueDomains[index].InterfaceType == "http" && model.Vocabulary.ValueDomains[index].Path == "mode" {
			model.Vocabulary.ValueDomains[index].Values = []normalizer.NormalizedValue{
				{Value: "apiUrl", Tokens: []string{"api", "url"}},
				{Value: "api_url", Tokens: []string{"api", "url"}},
			}
		}
	}
	err := Emit(model, loadTestTarget(t, "10-scoped-domains-collisions.yaml"), filepath.Join(t.TempDir(), "output"))
	if err == nil || !strings.Contains(err.Error(), "RCG2010") {
		t.Fatalf("same-domain collision error = %v", err)
	}
}

func normalizeFixture(t *testing.T, root, rootID string) normalizer.BindingModel {
	t.Helper()
	schemas, err := normalizer.LoadSchemas(
		filepath.Join("..", "..", "model", "runtimeconditions.extension-semantic.schema.yaml"),
		filepath.Join("..", "..", "model", "runtimeconditions.binding-model.schema.yaml"),
	)
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := normalizer.NewResolver(normalizer.ResolverConfig{Schemas: schemas, CatalogRoots: []string{root}})
	if err != nil {
		t.Fatal(err)
	}
	closure, err := resolver.Resolve(context.Background(), rootID)
	if err != nil {
		t.Fatal(err)
	}
	model, err := normalizer.Normalize(closure, normalizer.BuildDependencyLock(closure), schemas, normalizer.NormalizeConfig{
		CoreProfileSchema: normalizer.CoreProfileIdentity{
			ID: "urn:runtimeconditions:test:core-profile-schema", Version: "0.0.0-test", SemanticSHA256: strings.Repeat("c", 64),
		},
		Normalizer: normalizer.ToolIdentity{
			Name: normalizer.NormalizerName, Version: normalizer.NormalizerVersion, SHA256: strings.Repeat("d", 64),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return model
}

func expectedModelPath(name string) string {
	return filepath.Join("..", "..", "model", "conformance", "expected", name, "runtimeconditions.binding-model.yaml")
}

func testTargetPath(name string) string {
	return filepath.Join("testdata", "package-targets", name)
}

func loadExpectedModel(t *testing.T, name string) normalizer.BindingModel {
	t.Helper()
	model, err := LoadModel(expectedModelPath(name))
	if err != nil {
		t.Fatal(err)
	}
	return model
}

func loadTestTarget(t *testing.T, name string) PackageTarget {
	t.Helper()
	target, err := LoadPackageTarget(testTargetPath(name))
	if err != nil {
		t.Fatal(err)
	}
	return target
}
