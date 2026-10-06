package goemitter

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/scanner"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/runtimeconditions/extensions/tooling/extension-bindings/normalizer"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"
)

var positiveGoCases = []string{
	"01-owned-kind-interface",
	"02-additive-field",
	"03-transitive-closure",
	"06-recursive-reference",
	"07-object-alternatives",
	"08-heterogeneous-union",
	"09-collections-and-maps",
	"10-scoped-domains-collisions",
	"11-source-name-preservation",
	"13-dependency-schema-only",
}

type conformanceModule struct {
	targetFile string
	rootID     string
	expected   string
}

type generatedModule struct {
	model  normalizer.BindingModel
	target PackageTarget
	path   string
	ir     *packageIR
}

func TestGoEmitterConformance(t *testing.T) {
	for _, name := range positiveGoCases {
		name := name
		t.Run(name, func(t *testing.T) {
			workspace := t.TempDir()
			modules := generateConformanceModules(t, name, workspace)
			root := modules[len(modules)-1]
			consumer := writeDependencyConsumer(t, name, workspace, root)

			schemas, err := normalizer.LoadSchemas(
				filepath.Join("..", "..", "model", "runtimeconditions.extension-semantic.schema.yaml"),
				filepath.Join("..", "..", "model", "runtimeconditions.binding-model.schema.yaml"),
			)
			if err != nil {
				t.Fatal(err)
			}
			if err := schemas.ValidateModel(root.model); err != nil {
				t.Fatalf("gate 1 model schema: %v", err)
			}
			validateYAMLDocument(t,
				filepath.Join("..", "..", "model", "runtimeconditions.binding-manifest.schema.yaml"),
				filepath.Join(root.path, "runtimeconditions.bindings.yaml"),
			)
			for _, module := range modules {
				assertStructuralManifest(t, module)
			}

			goWork := writeGoWork(t, workspace, modules, consumer)
			for _, module := range modules {
				runCommand(t, module.path, goWork, "go", "mod", "edit", "-json")
				if output := runCommand(t, module.path, goWork, "gofmt", "-d", "bindings.go", filepath.Join("conformance", "conformance_test.go")); output != "" {
					t.Fatalf("gate 4 gofmt diff:\n%s", output)
				}
				runCommand(t, module.path, goWork, "go", "test", "./...")
				runCommand(t, module.path, goWork, "go", "vet", "./...")
			}
			if consumer != "" {
				runCommand(t, consumer, goWork, "go", "test", "./...")
			}

			assertConformanceCoverage(t, root)
			assertAPISurface(t, root)
			assertDeterministicEmission(t, root.model, root.target, root.path)
			assertArchive(t, root.path)
		})
	}
}

func TestManifestRejectsProvisionalInventoryAndStaleIdentity(t *testing.T) {
	model := loadExpectedModel(t, "01-owned-kind-interface")
	target := loadTestTarget(t, "01-owned-kind-interface.yaml")
	output := filepath.Join(t.TempDir(), "package")
	if err := Emit(model, target, output); err != nil {
		t.Fatal(err)
	}
	schemaData, err := os.ReadFile(filepath.Join("..", "..", "model", "runtimeconditions.binding-manifest.schema.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	schemaValue, err := normalizer.ParseYAMLData(schemaData)
	if err != nil {
		t.Fatal(err)
	}
	schemaJSON, err := json.Marshal(schemaValue)
	if err != nil {
		t.Fatal(err)
	}
	resource, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaJSON))
	if err != nil {
		t.Fatal(err)
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("https://runtimeconditions.io/test/structural-manifest:1.0.0", resource); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile("https://runtimeconditions.io/test/structural-manifest:1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	manifestData, err := os.ReadFile(filepath.Join(output, "runtimeconditions.bindings.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := normalizer.ParseYAMLData(manifestData)
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.Validate(manifest); err != nil {
		t.Fatalf("structural manifest rejected: %v", err)
	}
	for _, emitter := range []string{EmitterName, EmitterName + "@sha256:" + strings.Repeat("A", 64)} {
		invalid := make(map[string]any, len(manifest))
		for key, value := range manifest {
			invalid[key] = value
		}
		invalid["generated"] = map[string]any{"nonEditable": true, "emitter": emitter, "version": EmitterVersion}
		if err := schema.Validate(invalid); err == nil {
			t.Fatalf("invalid emitter identity accepted: %s", emitter)
		}
	}
	for _, version := range []string{"runtimeconditions.io/bindings/v1alpha1", ManifestAPIVersion} {
		legacy := make(map[string]any, len(manifest))
		for key, value := range manifest {
			legacy[key] = value
		}
		legacy["apiVersion"] = version
		legacy["symbols"] = []any{}
		delete(legacy, "declarations")
		delete(legacy, "importedMarkerContracts")
		delete(legacy, "rootBindings")
		delete(legacy, "types")
		if err := schema.Validate(legacy); err == nil {
			t.Fatalf("provisional symbol inventory accepted as %s", version)
		}
	}

	stale := model
	stale.Metadata.SemanticSHA256 = strings.Repeat("0", 64)
	if err := Emit(stale, target, filepath.Join(t.TempDir(), "stale-model")); err == nil || !strings.Contains(err.Error(), "RCG1023") {
		t.Fatalf("stale model digest: %v", err)
	}
	stale = model
	stale.RootExtension.SemanticSHA256 = strings.Repeat("0", 64)
	if err := Emit(stale, target, filepath.Join(t.TempDir(), "stale-extension")); err == nil || !strings.Contains(err.Error(), "RCG1022") {
		t.Fatalf("stale root extension digest: %v", err)
	}
}

func assertStructuralManifest(t *testing.T, module generatedModule) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(module.path, "runtimeconditions.bindings.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest Manifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Generated.Emitter != EmitterName+"@sha256:"+module.target.EmitterSHA256 ||
		manifest.Generated.Version != EmitterVersion || !manifest.Generated.NonEditable {
		t.Fatal("manifest emitter identity differs from the generating tool")
	}
	if manifest.APIVersion != ManifestAPIVersion || manifest.Model.APIVersion != module.model.APIVersion ||
		manifest.Model.SemanticSHA256 != module.model.Metadata.SemanticSHA256 ||
		manifest.Extension.ID != module.model.RootExtension.ID ||
		manifest.Extension.SemanticSHA256 != module.model.RootExtension.SemanticSHA256 ||
		manifest.Package.Coordinate != module.target.ModulePath ||
		manifest.Package.Name != module.target.PackageName ||
		manifest.Package.Version != module.target.Version {
		t.Fatal("manifest identity differs from the normalized model or package target")
	}

	refs := map[ManifestModelRef]bool{}
	shapes := map[ManifestModelRef]normalizer.Shape{}
	memberValues := map[ManifestModelRef]map[string]bool{}
	var walkShape func(normalizer.Shape)
	walkShape = func(shape normalizer.Shape) {
		ref := manifestModelRef(shape.Provenance)
		refs[ref] = true
		shapes[ref] = shape
		for _, value := range shape.Values {
			if stringValue, ok := value.Value.(string); ok {
				if memberValues[ref] == nil {
					memberValues[ref] = map[string]bool{}
				}
				memberValues[ref][stringValue] = true
			}
		}
		for _, property := range shape.Properties {
			refs[manifestModelRef(property.Provenance)] = true
			walkShape(property.Shape)
		}
		if shape.Items != nil {
			walkShape(*shape.Items)
		}
		if shape.MapValues != nil {
			walkShape(*shape.MapValues)
		}
		for _, variant := range shape.Variants {
			walkShape(variant)
		}
	}
	for _, scope := range module.model.Scopes {
		if scope.Projection != nil {
			walkShape(*scope.Projection)
		}
	}
	for _, schema := range module.model.Schemas {
		walkShape(schema.Projection)
		for _, definition := range schema.Definitions {
			refs[manifestModelRef(definition.Provenance)] = true
			walkShape(definition.Shape)
		}
	}
	declarations := map[string]normalizer.DeclarationModel{}
	for _, declaration := range module.model.Vocabulary.OwnedDeclarations {
		declarations[declaration.Coordinate] = declaration
		refs[manifestModelRef(declaration.Provenance)] = true
	}
	for _, declaration := range module.model.Vocabulary.ImportedDeclarations {
		declarations[declaration.Coordinate] = declaration
		refs[manifestModelRef(declaration.Provenance)] = true
	}
	for _, value := range module.model.Vocabulary.Interfaces {
		refs[manifestModelRef(value.Provenance)] = true
	}
	for _, value := range module.model.Vocabulary.ConditionFields {
		refs[manifestModelRef(value.Provenance)] = true
	}
	for _, value := range module.model.Vocabulary.ValueDomains {
		ref := manifestModelRef(value.Provenance)
		refs[ref] = true
		for _, member := range value.Values {
			if stringValue, ok := member.Value.(string); ok {
				if memberValues[ref] == nil {
					memberValues[ref] = map[string]bool{}
				}
				memberValues[ref][stringValue] = true
			}
		}
	}
	checkRef := func(ref ManifestModelRef) {
		t.Helper()
		if !refs[ref] {
			t.Errorf("manifest references absent model location %+v", ref)
		}
	}
	if len(manifest.Declarations) != len(module.model.Vocabulary.OwnedDeclarations) ||
		len(manifest.ImportedMarkerContracts) != len(module.model.Vocabulary.ImportedDeclarations) {
		t.Fatal("manifest declaration contracts do not cover normalized vocabulary")
	}
	for _, declaration := range manifest.Declarations {
		checkRef(declaration.ModelRef)
		model := declarations[declaration.ModelRef.Coordinate]
		if model.Owner != declaration.Owner || model.Kind != declaration.SourceName ||
			declaration.MarkerMethod != markerMethod(model) {
			t.Errorf("declaration contract disagrees with model: %+v", declaration)
		}
	}
	for _, imported := range manifest.ImportedMarkerContracts {
		checkRef(imported.ModelRef)
		model := declarations[imported.ModelRef.Coordinate]
		if model.Owner != imported.Owner || model.Kind != imported.SourceName ||
			imported.MarkerMethod != markerMethod(model) {
			t.Errorf("imported marker contract disagrees with model: %+v", imported)
		}
	}

	nativeTypes := map[string]ManifestNamedType{}
	for _, entry := range manifest.Types {
		checkRef(entry.ModelRef)
		if _, exists := nativeTypes[entry.NativeName]; exists {
			t.Errorf("duplicate native type %q", entry.NativeName)
		}
		nativeTypes[entry.NativeName] = entry
	}
	checkValue := func(value ManifestReference) {
		t.Helper()
		if value.Type != "" {
			if _, exists := nativeTypes[value.Type]; !exists {
				t.Errorf("dangling native type reference %q", value.Type)
			}
		} else if value.Builtin == "" {
			t.Error("reference has neither native type nor builtin")
		}
	}
	for _, binding := range manifest.RootBindings {
		checkRef(binding.ModelRef)
		checkValue(binding.Value)
		if declaration, exists := declarations[binding.DeclarationCoordinate]; !exists || declaration.Kind != binding.Scope.Kind {
			t.Errorf("root binding has no matching declaration: %+v", binding)
		}
		if len(binding.Path) == 0 ||
			(binding.Role == "interface" && binding.SourceName != binding.FixedInterfaceType) ||
			(binding.Role != "interface" && binding.SourceName != binding.Path[len(binding.Path)-1].Name) {
			t.Errorf("root binding loses serialized path name: %+v", binding)
		}
	}
	assertRootDeclarationContracts(t, module, manifest)
	for _, entry := range manifest.Types {
		shape, hasShape := shapes[entry.ModelRef]
		if hasShape {
			construct := map[string]string{"object": "object", "scalar": "scalar", "array": "collection", "map": "map", "union": "union", "any": "any"}[shape.Kind]
			if shape.Kind != "ref" && construct != entry.Construct {
				t.Errorf("native type %s is %s but model is %s", entry.NativeName, entry.Construct, shape.Kind)
			}
		}
		for _, contract := range entry.Implements {
			declaration, exists := declarations[contract.DeclarationCoordinate]
			if !exists || contract.MarkerMethod != markerMethod(declaration) {
				t.Errorf("type %s has unknown marker contract %+v", entry.NativeName, contract)
			}
		}
		if entry.Fields != nil {
			interfaceShape := strings.HasPrefix(entry.ModelRef.Coordinate, "interface:")
			if !interfaceShape && (!hasShape || shape.Kind != "object" || len(*entry.Fields) != len(shape.Properties)) {
				t.Errorf("object %s fields do not match model shape", entry.NativeName)
				continue
			}
			for index, field := range *entry.Fields {
				checkRef(field.ModelRef)
				checkValue(field.Value)
				if !interfaceShape {
					property := shape.Properties[index]
					if field.ModelRef != manifestModelRef(property.Provenance) ||
						field.SourceName != property.Name || field.Required != property.Required {
						t.Errorf("field %s.%s differs from model property %s", entry.NativeName, field.NativeName, property.Name)
					}
					if !field.Required {
						pointed := field.Value.Builtin != "" && field.Value.Builtin != "any"
						if referenced, exists := nativeTypes[field.Value.Type]; exists {
							pointed = referenced.Construct == "object" ||
								(referenced.Construct == "scalar" && len(referenced.Members) != 0)
						}
						if field.Value.Pointer != pointed {
							t.Errorf("optional field %s.%s has wrong native pointer shape", entry.NativeName, field.NativeName)
						}
					}
				}
			}
		}
		if entry.Element != nil {
			checkRef(entry.Element.ModelRef)
			checkValue(entry.Element.Value)
			var child *normalizer.Shape
			if hasShape && shape.Kind == "array" {
				child = shape.Items
			} else if hasShape && shape.Kind == "map" {
				child = shape.MapValues
			}
			if child == nil || entry.Element.ModelRef != manifestModelRef(child.Provenance) {
				t.Errorf("type %s element does not map to model child", entry.NativeName)
			}
		}
		if len(entry.Variants) != 0 {
			if !hasShape || shape.Kind != "union" || len(entry.Variants) != len(shape.Variants) {
				t.Errorf("union %s variants do not match model shape", entry.NativeName)
				continue
			}
			for index, variant := range entry.Variants {
				checkRef(variant.ModelRef)
				checkValue(variant.Value)
				if variant.ModelRef != manifestModelRef(shape.Variants[index].Provenance) {
					t.Errorf("union %s variant %d maps to wrong model child", entry.NativeName, index)
				}
			}
		}
		for _, member := range entry.Members {
			checkRef(member.ModelRef)
			if !memberValues[member.ModelRef][member.Value] {
				t.Errorf("constant %s = %q is absent from model value domain", member.NativeName, member.Value)
			}
		}
	}
	for _, field := range module.model.Vocabulary.ConditionFields {
		if field.Owner != module.model.RootExtension.ID {
			continue
		}
		found := false
		for _, binding := range manifest.RootBindings {
			if binding.Role == "condition-field" && binding.ModelRef == manifestModelRef(field.Provenance) {
				found = reflect.DeepEqual(binding.Path, field.Segments)
			}
		}
		if !found {
			t.Errorf("owned condition field %s has no exact root binding", field.Coordinate)
		}
	}
}

func assertRootDeclarationContracts(t *testing.T, module generatedModule, manifest Manifest) {
	t.Helper()
	for _, interfaceModel := range module.model.Vocabulary.Interfaces {
		if interfaceModel.Owner != module.model.RootExtension.ID {
			continue
		}
		found := false
		for _, binding := range manifest.RootBindings {
			if binding.Role == "interface" && binding.ModelRef == manifestModelRef(interfaceModel.Provenance) &&
				binding.Scope.Kind == interfaceModel.Kind && binding.Scope.InterfaceType == interfaceModel.Type {
				found = true
			}
		}
		if !found {
			t.Errorf("model interface %s has no declaration binding", interfaceModel.Coordinate)
		}
	}
	for _, scope := range module.model.Scopes {
		if scope.Projection == nil {
			continue
		}
		for _, property := range scope.Projection.Properties {
			if property.Name == "interface" || property.Provenance.Owner != module.model.RootExtension.ID {
				continue
			}
			found := false
			for _, binding := range manifest.RootBindings {
				if binding.Scope.Kind == scope.Kind && binding.Scope.InterfaceType == scope.InterfaceType &&
					len(binding.Path) == 1 && binding.Path[0].Name == property.Name && !binding.Path[0].Array {
					found = true
					if binding.Role == "schema-field" && binding.ModelRef != manifestModelRef(property.Provenance) {
						t.Errorf("root property %s.%s loses its model location", scope.Coordinate, property.Name)
					}
				}
			}
			if !found {
				t.Errorf("model root property %s.%s has no declaration binding", scope.Coordinate, property.Name)
			}
		}
	}

	declarations := map[string]normalizer.DeclarationModel{}
	for _, declaration := range append(append([]normalizer.DeclarationModel(nil), module.model.Vocabulary.OwnedDeclarations...), module.model.Vocabulary.ImportedDeclarations...) {
		declarations[declaration.Coordinate] = declaration
	}
	nativeTypes := map[string]ManifestNamedType{}
	for _, entry := range manifest.Types {
		nativeTypes[entry.NativeName] = entry
	}
	expected := map[string]map[string]string{}
	var requireContract func(string, normalizer.DeclarationModel)
	requireContract = func(name string, declaration normalizer.DeclarationModel) {
		entry, exists := nativeTypes[name]
		if !exists {
			t.Errorf("declaration field %q is not a generated named type", name)
			return
		}
		if expected[name] == nil {
			expected[name] = map[string]string{}
		}
		if _, exists := expected[name][declaration.Coordinate]; exists {
			return
		}
		expected[name][declaration.Coordinate] = markerMethod(declaration)
		for _, variant := range entry.Variants {
			requireContract(variant.Value.Type, declaration)
		}
	}
	for _, binding := range manifest.RootBindings {
		declaration, exists := declarations[binding.DeclarationCoordinate]
		if !exists || declaration.Kind != binding.Scope.Kind {
			t.Errorf("root binding %s has no model declaration contract", binding.SourceName)
			continue
		}
		requireContract(binding.Value.Type, declaration)
	}

	packageValue := typeCheckGeneratedPackage(t, module.path, module.target.ModulePath)
	for _, entry := range manifest.Types {
		actual := map[string]string{}
		for _, contract := range entry.Implements {
			actual[contract.DeclarationCoordinate] = contract.MarkerMethod
		}
		if len(actual) != len(expected[entry.NativeName]) {
			t.Errorf("type %s declaration contracts differ: manifest %v, expected %v", entry.NativeName, actual, expected[entry.NativeName])
		}
		native := packageType(t, packageValue, entry.NativeName)
		for coordinate, declaration := range declarations {
			method := markerMethod(declaration)
			signature := types.NewSignatureType(nil, nil, nil, types.NewTuple(), types.NewTuple(), false)
			contract := types.NewInterfaceType([]*types.Func{types.NewFunc(token.NoPos, packageValue, method, signature)}, nil).Complete()
			want := expected[entry.NativeName][coordinate] == method
			if implements := types.Implements(native, contract); implements != want {
				t.Errorf("Go type %s implements declaration %s = %t, want %t", entry.NativeName, coordinate, implements, want)
			}
			if want && actual[coordinate] != method {
				t.Errorf("type %s lacks manifest marker %s for %s", entry.NativeName, method, coordinate)
			}
		}
	}
}

func generateConformanceModules(t *testing.T, name, workspace string) []generatedModule {
	t.Helper()
	fixtureRoot := filepath.Join("..", "..", "model", "conformance", "cases", name)
	var specs []conformanceModule
	switch name {
	case "02-additive-field":
		specs = append(specs, conformanceModule{
			targetFile: "02-additive-field-base.yaml",
			rootID:     "https://runtimeconditions.io/conformance/additive-field-base:1.0.0",
		})
	case "03-transitive-closure":
		specs = append(specs,
			conformanceModule{
				targetFile: "03-transitive-leaf.yaml",
				rootID:     "https://runtimeconditions.io/conformance/transitive-leaf:1.0.0",
			},
			conformanceModule{
				targetFile: "03-transitive-middle.yaml",
				rootID:     "https://runtimeconditions.io/conformance/transitive-middle:1.0.0",
			},
		)
	case "13-dependency-schema-only":
		specs = append(specs, conformanceModule{
			targetFile: "13-dependency-schema-only-dependency.yaml",
			rootID:     "https://runtimeconditions.io/conformance/dependency-schema-only-dependency:1.0.0",
		})
	}
	specs = append(specs, conformanceModule{targetFile: name + ".yaml", expected: name})

	modules := make([]generatedModule, 0, len(specs))
	for index, spec := range specs {
		target := loadTestTarget(t, spec.targetFile)
		var model normalizer.BindingModel
		if spec.expected != "" {
			model = loadExpectedModel(t, spec.expected)
		} else {
			model = normalizeFixture(t, fixtureRoot, spec.rootID)
		}
		output := filepath.Join(workspace, fmt.Sprintf("module-%02d", index))
		if err := Emit(model, target, output); err != nil {
			t.Fatalf("emit %s: %v", target.PackageKey, err)
		}
		ir, err := buildPackageIR(model, target)
		if err != nil {
			t.Fatal(err)
		}
		source, err := ir.renderConformance()
		if err != nil {
			t.Fatal(err)
		}
		directory := filepath.Join(output, "conformance")
		if err = os.MkdirAll(directory, 0755); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(directory, "conformance_test.go"), source, 0644); err != nil {
			t.Fatal(err)
		}
		modules = append(modules, generatedModule{model: model, target: target, path: output, ir: ir})
	}
	return modules
}

func writeGoWork(t *testing.T, workspace string, modules []generatedModule, additional ...string) string {
	t.Helper()
	var buffer strings.Builder
	buffer.WriteString("go 1.22\n\nuse (\n")
	for _, module := range modules {
		absolute, err := filepath.EvalSymlinks(module.path)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&buffer, "\t%s\n", filepath.ToSlash(absolute))
	}
	for _, path := range additional {
		if path == "" {
			continue
		}
		absolute, err := filepath.EvalSymlinks(path)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&buffer, "\t%s\n", filepath.ToSlash(absolute))
	}
	buffer.WriteString(")\n")
	path := filepath.Join(workspace, "go.work")
	if err := os.WriteFile(path, []byte(buffer.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeDependencyConsumer(t *testing.T, name, workspace string, root generatedModule) string {
	t.Helper()
	var source string
	switch name {
	case "02-additive-field":
		source = `package consumer

import (
	additive "example.com/runtimeconditions/conformance/additive-field"
	base "example.com/runtimeconditions/conformance/additive-field-base"
)

var _ base.ServiceField = additive.Credential{}
var _ = base.Service(additive.Credential{})
`
	case "03-transitive-closure":
		source = `package consumer

import (
	root "example.com/runtimeconditions/conformance/transitive-root"
	leaf "example.com/runtimeconditions/conformance/transitive-leaf"
)

var _ leaf.WorkerField = root.Command("")
var _ = leaf.Worker(root.Command(""))
`
	default:
		return ""
	}
	directory := filepath.Join(workspace, "consumer")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	goMod := fmt.Sprintf("module example.com/runtimeconditions/conformance/consumer\n\ngo 1.22\n\nrequire %s %s\n", root.target.ModulePath, root.target.Version)
	if err := os.WriteFile(filepath.Join(directory, "go.mod"), []byte(goMod), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "consumer_test.go"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	return directory
}

func runCommand(t *testing.T, directory, goWork, name string, arguments ...string) string {
	t.Helper()
	command := exec.Command(name, arguments...)
	command.Dir = directory
	command.Env = append(os.Environ(),
		"GOCACHE="+filepath.Join(filepath.Dir(goWork), "go-cache"),
		"GOWORK="+goWork,
		"GOTOOLCHAIN=local",
	)
	output, err := command.CombinedOutput()
	if err != nil {
		workData, _ := os.ReadFile(goWork)
		t.Fatalf("%s %s in %s with %s:\n%s\n%v\n%s", name, strings.Join(arguments, " "), directory, goWork, workData, err, output)
	}
	return string(output)
}

func validateYAMLDocument(t *testing.T, schemaPath, documentPath string) {
	t.Helper()
	schemaData, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	schemaValue, err := normalizer.ParseYAMLData(schemaData)
	if err != nil {
		t.Fatal(err)
	}
	schemaJSON, err := json.Marshal(schemaValue)
	if err != nil {
		t.Fatal(err)
	}
	resource, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaJSON))
	if err != nil {
		t.Fatal(err)
	}
	compiler := jsonschema.NewCompiler()
	const schemaURL = "https://runtimeconditions.io/test/binding-manifest:1.0.0"
	if err := compiler.AddResource(schemaURL, resource); err != nil {
		t.Fatal(err)
	}
	compiled, err := compiler.Compile(schemaURL)
	if err != nil {
		t.Fatal(err)
	}
	documentData, err := os.ReadFile(documentPath)
	if err != nil {
		t.Fatal(err)
	}
	document, err := normalizer.ParseYAMLData(documentData)
	if err != nil {
		t.Fatal(err)
	}
	if err := compiled.Validate(document); err != nil {
		t.Fatalf("gate 2 manifest schema: %v", err)
	}
}

func assertConformanceCoverage(t *testing.T, module generatedModule) {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filepath.Join(module.path, "conformance", "conformance_test.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	selectors := map[string]bool{}
	fields := map[string]bool{}
	ast.Inspect(file, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.SelectorExpr:
			selectors[typed.Sel.Name] = true
		case *ast.KeyValueExpr:
			if identifier, ok := typed.Key.(*ast.Ident); ok {
				fields[identifier.Name] = true
			}
		}
		return true
	})
	required := map[string]bool{"Declaration": true}
	for _, declaration := range module.ir.declarations {
		required[declaration.function.name] = true
		required[declaration.fieldInterface.name] = true
	}
	for _, typeValue := range module.ir.types {
		required[typeValue.request.name] = true
		for _, field := range typeValue.fields {
			if !fields[field.name] {
				t.Errorf("gate 8 conformance does not exercise field %s.%s", typeValue.request.name, field.name)
			}
		}
	}
	for _, constant := range module.ir.constants {
		required[constant.name] = true
	}
	for name := range required {
		if !selectors[name] {
			t.Errorf("gate 8 conformance does not reference %s", name)
		}
	}
}

func assertAPISurface(t *testing.T, module generatedModule) {
	t.Helper()
	actual := sourceAPISurface(t, filepath.Join(module.path, generatedGoFile))
	expected := expectedAPISurface(t, module.ir)
	if strings.Join(actual, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("gate 16 AST surface mismatch\nactual:\n%s\nexpected:\n%s", strings.Join(actual, "\n"), strings.Join(expected, "\n"))
	}
}

func sourceAPISurface(t *testing.T, path string) []string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var result []string
	for _, declaration := range file.Decls {
		switch typed := declaration.(type) {
		case *ast.GenDecl:
			for _, spec := range typed.Specs {
				switch value := spec.(type) {
				case *ast.TypeSpec:
					if ast.IsExported(value.Name.Name) {
						result = append(result, "type|"+value.Name.Name+"|"+printNode(fset, value.Type))
					}
				case *ast.ValueSpec:
					for index, name := range value.Names {
						if !ast.IsExported(name.Name) {
							continue
						}
						typeText := ""
						if value.Type != nil {
							typeText = printNode(fset, value.Type)
						}
						valueText := ""
						if index < len(value.Values) {
							valueText = printNode(fset, value.Values[index])
						}
						result = append(result, "const|"+name.Name+"|"+typeText+"|"+valueText)
					}
				}
			}
		case *ast.FuncDecl:
			if typed.Recv == nil {
				if ast.IsExported(typed.Name.Name) {
					result = append(result, "func|"+typed.Name.Name+"|"+printNode(fset, typed.Type))
				}
				continue
			}
			receiver := printNode(fset, typed.Recv.List[0].Type)
			result = append(result, "method|"+receiver+"|"+typed.Name.Name+"|"+printNode(fset, typed.Type))
		}
	}
	sort.Strings(result)
	return result
}

func expectedAPISurface(t *testing.T, ir *packageIR) []string {
	t.Helper()
	var result []string
	result = append(result, "type|Declaration|"+normalizeExpression(t, "struct{}"))
	for _, declaration := range ir.declarations {
		result = append(result,
			"type|"+declaration.fieldInterface.name+"|"+normalizeExpression(t, "interface{"+declaration.markerMethod+"()}"),
			"func|"+declaration.function.name+"|"+normalizeExpression(t, "func(fields ..."+declaration.fieldInterface.name+") Declaration"),
		)
	}
	for _, key := range sortedTypeKeys(ir.types) {
		typeValue := ir.types[key]
		name := typeValue.request.name
		var expression string
		switch typeValue.kind {
		case "scalar":
			expression = typeValue.underlying
		case "struct":
			var fields []string
			for _, field := range typeValue.fields {
				fields = append(fields, field.name+" "+ir.renderTypeReference(field.typeRef))
			}
			expression = "struct{" + strings.Join(fields, ";") + "}"
		case "slice":
			expression = "[]" + ir.renderTypeReference(typeValue.element)
		case "map":
			expression = "map[string]" + ir.renderTypeReference(typeValue.element)
		case "union":
			methods := []string{unionMethod(name) + "()"}
			for _, method := range sortedMethods(typeValue.methods) {
				methods = append(methods, method+"()")
			}
			expression = "interface{" + strings.Join(methods, ";") + "}"
		case "any":
			expression = "interface{}"
		}
		result = append(result, "type|"+name+"|"+normalizeExpression(t, expression))
		if typeValue.kind != "union" {
			for _, parent := range typeValue.unionParents {
				result = append(result, "method|"+name+"|"+unionMethod(ir.types[parent].request.name)+"|"+normalizeExpression(t, "func()"))
			}
			for _, method := range sortedMethods(typeValue.methods) {
				result = append(result, "method|"+name+"|"+method+"|"+normalizeExpression(t, "func()"))
			}
		}
	}
	for _, constant := range ir.constants {
		result = append(result, "const|"+constant.name+"|"+ir.types[constant.typeKey].request.name+"|"+strconv.Quote(constant.value))
	}
	sort.Strings(result)
	return result
}

func normalizeExpression(t *testing.T, source string) string {
	t.Helper()
	expression, err := parser.ParseExpr(source)
	if err != nil {
		t.Fatalf("parse expected expression %q: %v", source, err)
	}
	return printNode(token.NewFileSet(), expression)
}

func printNode(fset *token.FileSet, node any) string {
	var buffer bytes.Buffer
	if err := printer.Fprint(&buffer, fset, node); err != nil {
		panic(err)
	}
	var lexer scanner.Scanner
	lexer.Init(token.NewFileSet().AddFile("surface.go", -1, buffer.Len()), buffer.Bytes(), nil, 0)
	var parts []string
	for {
		_, kind, literal := lexer.Scan()
		if kind == token.EOF {
			break
		}
		if kind == token.SEMICOLON {
			continue
		} else if literal == "" {
			literal = kind.String()
		}
		parts = append(parts, literal)
	}
	return strings.Join(parts, " ")
}

func assertDeterministicEmission(t *testing.T, model normalizer.BindingModel, target PackageTarget, firstPath string) {
	t.Helper()
	first := readTree(t, firstPath)
	delete(first, "conformance/conformance_test.go")
	for iteration := 0; iteration < 2; iteration++ {
		output := filepath.Join(t.TempDir(), "output")
		if err := Emit(model, target, output); err != nil {
			t.Fatal(err)
		}
		actual := readTree(t, output)
		if !equalTree(first, actual) {
			t.Fatalf("gate 12 emission %d differs", iteration+2)
		}
	}
}

func assertArchive(t *testing.T, root string) {
	t.Helper()
	files := readTree(t, root)
	declared := make([]string, 0, len(files))
	for path := range files {
		declared = append(declared, path)
	}
	archive, err := SourceArchive(root)
	if err != nil {
		t.Fatal(err)
	}
	secondArchive, err := SourceArchive(root)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(archive, secondArchive) {
		t.Fatal("source archive is not byte-identical across rebuilds")
	}
	if err := VerifySourceArchive(archive, declared); err != nil {
		t.Fatalf("gate 14: %v", err)
	}
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}
	if len(reader.File) != len(declared) {
		t.Fatalf("archive has %d files, want %d", len(reader.File), len(declared))
	}
}

func readTree(t *testing.T, root string) map[string][]byte {
	t.Helper()
	result := map[string][]byte{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		result[filepath.ToSlash(relative)] = data
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func equalTree(left, right map[string][]byte) bool {
	if len(left) != len(right) {
		return false
	}
	for path, data := range left {
		if !bytes.Equal(data, right[path]) {
			return false
		}
	}
	return true
}

func TestReusableNativeAPISurface(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "bindings.go")
	if err := os.WriteFile(path, []byte("package generated\ntype FutureValue string\nfunc FutureDeclaration(value FutureValue) {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	surface, err := SourceAPISurface(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(surface) != 2 {
		t.Fatalf("surface %v", surface)
	}
}
