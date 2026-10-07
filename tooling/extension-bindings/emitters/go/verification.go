package goemitter

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/scanner"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/runtimeconditions/extensions/tooling/extension-bindings/normalizer"
)

// VerifyAPI compares native ASTs against the normalized model's IR.
func VerifyAPI(model normalizer.BindingModel, target PackageTarget, root string) ([]string, error) {
	if err := validateInputs(model, target); err != nil {
		return nil, err
	}
	ir, err := buildPackageIR(model, target)
	if err != nil {
		return nil, err
	}
	actual, err := SourceAPISurface(filepath.Join(root, generatedGoFile))
	if err != nil {
		return nil, err
	}
	expected := modelAPISurface(ir)
	if strings.Join(actual, "\n") != strings.Join(expected, "\n") {
		return nil, fmt.Errorf("exported Go API differs from normalized model")
	}
	expectedManifest, err := marshalManifest(ir.manifest())
	if err != nil {
		return nil, err
	}
	actualManifest, err := os.ReadFile(filepath.Join(root, "runtimeconditions.bindings.yaml"))
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(actualManifest, expectedManifest) {
		return nil, fmt.Errorf("binding manifest mappings differ from normalized model")
	}
	return actual, nil
}

func SourceAPISurface(path string) ([]string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, err
	}
	var result []string
	for _, declaration := range file.Decls {
		switch typed := declaration.(type) {
		case *ast.GenDecl:
			for _, spec := range typed.Specs {
				switch value := spec.(type) {
				case *ast.TypeSpec:
					if ast.IsExported(value.Name.Name) {
						result = append(result, "type|"+value.Name.Name+"|"+surfaceNode(fset, value.Type))
					}
				case *ast.ValueSpec:
					for index, name := range value.Names {
						if !ast.IsExported(name.Name) {
							continue
						}
						typeText := ""
						if value.Type != nil {
							typeText = surfaceNode(fset, value.Type)
						}
						valueText := ""
						if index < len(value.Values) {
							valueText = surfaceNode(fset, value.Values[index])
						}
						result = append(result, "const|"+name.Name+"|"+typeText+"|"+valueText)
					}
				}
			}
		case *ast.FuncDecl:
			if typed.Recv == nil {
				if ast.IsExported(typed.Name.Name) {
					result = append(result, "func|"+typed.Name.Name+"|"+surfaceNode(fset, typed.Type))
				}
				continue
			}
			receiver := surfaceNode(fset, typed.Recv.List[0].Type)
			result = append(result, "method|"+receiver+"|"+typed.Name.Name+"|"+surfaceNode(fset, typed.Type))
		}
	}
	sort.Strings(result)
	return result, nil
}

func modelAPISurface(ir *packageIR) []string {
	var result []string
	result = append(result, "type|Declaration|"+surfaceExpression("struct{}"))
	for _, declaration := range ir.declarations {
		result = append(result,
			"type|"+declaration.fieldInterface.name+"|"+surfaceExpression("interface{"+declaration.markerMethod+"()}"),
			"func|"+declaration.function.name+"|"+surfaceExpression("func(fields ..."+declaration.fieldInterface.name+") Declaration"),
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
		result = append(result, "type|"+name+"|"+surfaceExpression(expression))
		if typeValue.kind != "union" {
			for _, parent := range typeValue.unionParents {
				result = append(result, "method|"+name+"|"+unionMethod(ir.types[parent].request.name)+"|"+surfaceExpression("func()"))
			}
			for _, method := range sortedMethods(typeValue.methods) {
				result = append(result, "method|"+name+"|"+method+"|"+surfaceExpression("func()"))
			}
		}
	}
	for _, constant := range ir.constants {
		result = append(result, "const|"+constant.name+"|"+ir.types[constant.typeKey].request.name+"|"+strconv.Quote(constant.value))
	}
	sort.Strings(result)
	return result
}

func surfaceExpression(source string) string {
	expression, err := parser.ParseExpr(source)
	if err != nil {
		panic(fmt.Sprintf("parse model expression %q: %v", source, err))
	}
	return surfaceNode(token.NewFileSet(), expression)
}

func surfaceNode(fset *token.FileSet, node any) string {
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
