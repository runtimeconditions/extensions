package normalizer

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

var conformanceCases = []string{
	"01-owned-kind-interface",
	"02-additive-field",
	"03-transitive-closure",
	"04-dependency-cycle",
	"05-vocabulary-conflict",
	"06-recursive-reference",
	"07-object-alternatives",
	"08-heterogeneous-union",
	"09-collections-and-maps",
	"10-scoped-domains-collisions",
	"11-tokenization-collisions",
	"12-unsupported-structural-keyword",
}

var negativeConformanceCases = map[string]bool{
	"04-dependency-cycle":               true,
	"05-vocabulary-conflict":            true,
	"12-unsupported-structural-keyword": true,
}

func TestConformanceSuite(t *testing.T) {
	schemas := testSchemas(t)
	casesRoot := filepath.Join("..", "model", "conformance", "cases")
	expectedRoot := filepath.Join("..", "model", "conformance", "expected")
	entries, err := os.ReadDir(casesRoot)
	if err != nil {
		t.Fatal(err)
	}
	var actualCases []string
	for _, entry := range entries {
		if entry.IsDir() {
			actualCases = append(actualCases, entry.Name())
		}
	}
	sort.Strings(actualCases)
	if strings.Join(actualCases, "\n") != strings.Join(conformanceCases, "\n") {
		t.Fatalf("conformance cases are %v, want %v", actualCases, conformanceCases)
	}

	update := os.Getenv("UPDATE_CONFORMANCE") == "1"
	for _, name := range conformanceCases {
		name := name
		t.Run(name, func(t *testing.T) {
			caseRoot := filepath.Join(casesRoot, name)
			model, runErr := runConformanceCase(t, schemas, caseRoot)
			expectedDirectory := filepath.Join(expectedRoot, name)
			if negativeConformanceCases[name] {
				if runErr == nil {
					t.Fatal("expected deterministic diagnostic")
				}
				diagnosticBytes := conformanceDiagnosticYAML(t, runErr)
				expectedPath := filepath.Join(expectedDirectory, "diagnostic.yaml")
				compareOrUpdateConformance(t, expectedPath, diagnosticBytes, update)
				return
			}
			if runErr != nil {
				t.Fatalf("normalize positive case: %v", runErr)
			}
			modelBytes, err := CanonicalModelYAML(model)
			if err != nil {
				t.Fatal(err)
			}
			expectedPath := filepath.Join(expectedDirectory, "runtimeconditions.binding-model.yaml")
			compareOrUpdateConformance(t, expectedPath, modelBytes, update)
			assertRepeatedNormalization(t, schemas, caseRoot, modelBytes)
			assertSetPermutationDeterminism(t, schemas, caseRoot, modelBytes)
		})
	}
}

func runConformanceCase(t *testing.T, schemas *Schemas, caseRoot string) (BindingModel, error) {
	t.Helper()
	rootBytes, err := os.ReadFile(filepath.Join(caseRoot, "root.yaml"))
	if err != nil {
		return BindingModel{}, err
	}
	definition, _, err := DecodeExtension(rootBytes)
	if err != nil {
		return BindingModel{}, err
	}
	resolver, err := NewResolver(ResolverConfig{Schemas: schemas, CatalogRoots: []string{caseRoot}})
	if err != nil {
		return BindingModel{}, err
	}
	closure, err := resolver.Resolve(context.Background(), definition.Metadata.ID)
	if err != nil {
		return BindingModel{}, err
	}
	return Normalize(closure, BuildDependencyLock(closure), schemas, testNormalizeConfig())
}

func conformanceDiagnosticYAML(t *testing.T, err error) []byte {
	t.Helper()
	diagnosticError, ok := err.(*DiagnosticError)
	if !ok {
		t.Fatalf("conformance failure is not a deterministic diagnostic: %T: %v", err, err)
	}
	data, marshalErr := yaml.Marshal(diagnosticError.Diagnostic)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	return data
}

func compareOrUpdateConformance(t *testing.T, path string, actual []byte, update bool) {
	t.Helper()
	if update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, actual, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	expected, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read expected output %s: %v", path, err)
	}
	if string(actual) != string(expected) {
		t.Fatalf("output differs from %s; regenerate only after reviewing the model contract", path)
	}
}

func assertRepeatedNormalization(t *testing.T, schemas *Schemas, caseRoot string, expected []byte) {
	t.Helper()
	for iteration := 0; iteration < 100; iteration++ {
		model, err := runConformanceCase(t, schemas, caseRoot)
		if err != nil {
			t.Fatalf("repeat %d: %v", iteration, err)
		}
		actual, err := CanonicalModelYAML(model)
		if err != nil {
			t.Fatal(err)
		}
		if string(actual) != string(expected) {
			t.Fatalf("repeat %d produced different normalized bytes", iteration)
		}
	}
}

func assertSetPermutationDeterminism(t *testing.T, schemas *Schemas, caseRoot string, expected []byte) {
	t.Helper()
	entries, err := os.ReadDir(caseRoot)
	if err != nil {
		t.Fatal(err)
	}
	var fixtureNames []string
	for _, entry := range entries {
		if !entry.IsDir() && (strings.HasSuffix(entry.Name(), ".yaml") || strings.HasSuffix(entry.Name(), ".yml")) {
			fixtureNames = append(fixtureNames, entry.Name())
		}
	}
	sort.Strings(fixtureNames)
	random := rand.New(rand.NewSource(20260920))
	for iteration := 0; iteration < 100; iteration++ {
		permutedRoot := t.TempDir()
		for _, name := range fixtureNames {
			data, err := os.ReadFile(filepath.Join(caseRoot, name))
			if err != nil {
				t.Fatal(err)
			}
			mapping, err := ParseYAMLData(data)
			if err != nil {
				t.Fatal(err)
			}
			permuted, err := permuteSemanticSets(mapping, schemas.semanticData, schemas.semanticData, random)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := yaml.Marshal(permuted)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(permutedRoot, name), encoded, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		model, err := runConformanceCase(t, schemas, permutedRoot)
		if err != nil {
			t.Fatalf("set permutation %d: %v", iteration, err)
		}
		actual, err := CanonicalModelYAML(model)
		if err != nil {
			t.Fatal(err)
		}
		if string(actual) != string(expected) {
			t.Fatalf("set permutation %d changed normalized bytes", iteration)
		}
	}
}

func permuteSemanticSets(value any, schema, root map[string]any, random *rand.Rand) (any, error) {
	selected, err := selectSemanticSchema(value, schema, root)
	if err != nil {
		return nil, err
	}
	switch typed := value.(type) {
	case map[string]any:
		properties, _ := selected["properties"].(map[string]any)
		additional, _ := selected["additionalProperties"].(map[string]any)
		result := make(map[string]any, len(typed))
		for key, child := range typed {
			childSchema, _ := properties[key].(map[string]any)
			if childSchema == nil {
				childSchema = additional
			}
			if childSchema == nil {
				return nil, fmt.Errorf("semantic schema has no rule for %q", key)
			}
			result[key], err = permuteSemanticSets(child, childSchema, root, random)
			if err != nil {
				return nil, err
			}
		}
		return result, nil
	case []any:
		items, _ := selected["items"].(map[string]any)
		result := make([]any, len(typed))
		for index, item := range typed {
			result[index], err = permuteSemanticSets(item, items, root, random)
			if err != nil {
				return nil, err
			}
		}
		if selected["x-runtimeconditions-ordering"] == "set" {
			random.Shuffle(len(result), func(i, j int) { result[i], result[j] = result[j], result[i] })
		}
		return result, nil
	default:
		return value, nil
	}
}
