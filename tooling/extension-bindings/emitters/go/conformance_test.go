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
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/runtimeconditions/extensions/tooling/extension-bindings/normalizer"
	"github.com/santhosh-tekuri/jsonschema/v6"
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
	"11-tokenization-collisions",
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

func generateConformanceModules(t *testing.T, name, workspace string) []generatedModule {
	t.Helper()
	fixtureRoot := filepath.Join("..", "..", "model", "conformance", "cases", name)
	var specs []conformanceModule
	switch name {
	case "02-additive-field":
		specs = append(specs, conformanceModule{
			targetFile: "02-additive-field-base.yaml",
			rootID:     "urn:runtimeconditions:conformance:additive-field:base",
		})
	case "03-transitive-closure":
		specs = append(specs,
			conformanceModule{
				targetFile: "03-transitive-leaf.yaml",
				rootID:     "urn:runtimeconditions:conformance:transitive:leaf",
			},
			conformanceModule{
				targetFile: "03-transitive-middle.yaml",
				rootID:     "urn:runtimeconditions:conformance:transitive:middle",
			},
		)
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
	const schemaURL = "urn:runtimeconditions:test:binding-manifest"
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
