package goemitter

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/format"
	"sort"
	"strconv"
	"strings"

	"github.com/runtimeconditions/extensions/tooling/extension-bindings/normalizer"
)

const generatedGoFile = "bindings.go"

type declarationIR struct {
	model          normalizer.DeclarationModel
	function       *symbolRequest
	fieldInterface *symbolRequest
	markerMethod   string
}

type typeReference struct {
	builtin string
	key     string
	pointer bool
	sample  string // Conformance literal from normalized values; not API metadata.
}

type fieldIR struct {
	provenance normalizer.Provenance
	sourceName string
	name       string
	typeRef    typeReference
	required   bool
}

type typeIR struct {
	key           string
	coordinate    string
	provenance    normalizer.Provenance
	sourceName    string
	request       *symbolRequest
	kind          string
	underlying    string
	sample        string
	fields        []fieldIR
	element       typeReference
	elementSource normalizer.Provenance
	methods       map[string]bool
	members       []normalizer.NormalizedValue
	domain        string
	unionVariants []string
	variantSource []normalizer.Provenance
	unionParents  []string
	building      bool
	built         bool
}

type rootBindingIR struct {
	role                  string
	provenance            normalizer.Provenance
	declarationCoordinate string
	scope                 normalizer.ScopeModel
	sourceName            string
	path                  []normalizer.PathSegment
	value                 typeReference
	fixedInterfaceType    string
}

type constantIR struct {
	coordinate string
	sourceName string
	name       string
	typeKey    string
	value      string
}

type pathPart struct {
	name   string
	tokens []string
	array  bool
}

type shapeContext struct {
	scope        normalizer.ScopeModel
	path         []pathPart
	semanticPath []pathPart
}

type packageIR struct {
	model              normalizer.BindingModel
	target             PackageTarget
	requests           []*symbolRequest
	requestByKey       map[string]*symbolRequest
	declarations       []*declarationIR
	declarationsByKind map[string]normalizer.DeclarationModel
	types              map[string]*typeIR
	constants          []constantIR
	schemas            map[string]normalizer.NormalizedSchema
	domains            map[string]normalizer.ValueDomainModel
	rootBindings       []rootBindingIR
	sampleError        error
}

func Emit(model normalizer.BindingModel, target PackageTarget, output string) error {
	if err := validateInputs(model, target); err != nil {
		return err
	}
	ir, err := buildPackageIR(model, target)
	if err != nil {
		return err
	}
	files, err := ir.files()
	if err != nil {
		return err
	}
	if err := prepareOutput(output); err != nil {
		return err
	}
	return writeFiles(output, files)
}

func buildPackageIR(model normalizer.BindingModel, target PackageTarget) (*packageIR, error) {
	ir := &packageIR{
		model: model, target: target,
		requestByKey:       map[string]*symbolRequest{},
		declarationsByKind: map[string]normalizer.DeclarationModel{},
		types:              map[string]*typeIR{},
		schemas:            map[string]normalizer.NormalizedSchema{},
		domains:            map[string]normalizer.ValueDomainModel{},
	}
	ir.addRequest(&symbolRequest{
		key: "fixed:type:Declaration", coordinate: "runtimeconditions:declaration",
		category: "declaration result", base: []string{"declaration"}, fixed: true,
	})
	for _, declaration := range model.Vocabulary.OwnedDeclarations {
		ir.declarationsByKind[declaration.Kind] = declaration
		declarationTokens := goTokens(declaration.Kind)
		function := ir.addRequest(&symbolRequest{
			key: "fixed:function:" + declaration.Coordinate, coordinate: declaration.Coordinate,
			category: "owned declaration function", base: declarationTokens, fixed: true,
		})
		fieldTokens := append(append([]string(nil), declarationTokens...), "field")
		fieldInterface := ir.addRequest(&symbolRequest{
			key: "fixed:field-interface:" + declaration.Coordinate, coordinate: declaration.Coordinate,
			category: "owned declaration marker interface", base: fieldTokens, fixed: true,
		})
		ir.declarations = append(ir.declarations, &declarationIR{
			model: declaration, function: function, fieldInterface: fieldInterface,
			markerMethod: markerMethod(declaration),
		})
	}
	for _, declaration := range model.Vocabulary.ImportedDeclarations {
		ir.declarationsByKind[declaration.Kind] = declaration
	}
	for _, schema := range model.Schemas {
		ir.schemas[schema.Coordinate] = schema
	}
	for _, domain := range model.Vocabulary.ValueDomains {
		ir.domains[scopePathKey(domain.Kind, domain.InterfaceType, domain.Path)] = domain
	}

	for _, interfaceModel := range model.Vocabulary.Interfaces {
		if interfaceModel.Owner != model.RootExtension.ID {
			continue
		}
		scope, ok := findScope(model, interfaceModel.Kind, interfaceModel.Type)
		if !ok {
			return nil, diagnostic("model", "RCG2002", interfaceModel.Coordinate, "interface has no normalized scope")
		}
		shape := normalizer.Shape{Kind: "object", Provenance: interfaceModel.Provenance}
		if scope.Projection != nil {
			if property, found := findProperty(*scope.Projection, "interface"); found {
				shape = property.Shape
			}
		}
		declaration, ok := ir.declarationsByKind[interfaceModel.Kind]
		if !ok {
			return nil, diagnostic("model", "RCG2003", interfaceModel.Coordinate, fmt.Sprintf("kind %q has no declaration contract", interfaceModel.Kind))
		}
		context := shapeContext{
			scope: scope,
			path:  []pathPart{{name: interfaceModel.Type, tokens: goTokens(interfaceModel.Type)}},
			semanticPath: []pathPart{{
				name: "interface", tokens: []string{"interface"},
			}},
		}
		interfaceTokens := goTokens(interfaceModel.Type)
		declarationTokens := goTokens(declaration.Kind)
		typeValue, err := ir.ensureNamedShape(
			"interface:"+interfaceModel.Coordinate, interfaceModel.Coordinate, interfaceModel.Type,
			interfaceTokens, [][]string{declarationTokens}, context, shape,
		)
		if err != nil {
			return nil, err
		}
		ir.addMethod(typeValue, markerMethod(declaration))
		ir.rootBindings = append(ir.rootBindings, rootBindingIR{
			role: "interface", provenance: interfaceModel.Provenance,
			declarationCoordinate: declaration.Coordinate, scope: scope,
			sourceName: interfaceModel.Type,
			path:       []normalizer.PathSegment{{Name: "interface"}},
			value:      typeReference{key: typeValue.key}, fixedInterfaceType: interfaceModel.Type,
		})
	}

	for _, field := range model.Vocabulary.ConditionFields {
		if field.Owner != model.RootExtension.ID {
			continue
		}
		scope, ok := findScope(model, field.Kind, field.InterfaceType)
		if !ok || scope.Projection == nil {
			return nil, diagnostic("model", "RCG2004", field.Coordinate, "condition field has no normalized scope projection")
		}
		shape, ok := shapeAtSegments(*scope.Projection, field.Segments)
		if !ok {
			return nil, diagnostic("model", "RCG2005", field.Coordinate, fmt.Sprintf("condition field path %q is absent from its scope projection", field.Path))
		}
		declaration, ok := ir.declarationsByKind[field.Kind]
		if !ok {
			return nil, diagnostic("model", "RCG2003", field.Coordinate, fmt.Sprintf("kind %q has no declaration contract", field.Kind))
		}
		path := pathParts(field.Segments)
		prefixes := pathPrefixes(path)
		if interfaceModel, found := findInterface(model, field.Kind, field.InterfaceType); found {
			prefixes = append(prefixes, goTokens(interfaceModel.Type))
		}
		declarationTokens := goTokens(declaration.Kind)
		prefixes = append(prefixes, declarationTokens)
		context := shapeContext{scope: scope, path: path, semanticPath: clonePath(path)}
		sourceName := field.Segments[len(field.Segments)-1].Name
		typeValue, err := ir.ensureNamedShape(
			"condition-field:"+field.Coordinate, field.Coordinate, sourceName,
			goTokens(field.Path), prefixes, context, shape,
		)
		if err != nil {
			return nil, err
		}
		ir.addMethod(typeValue, markerMethod(declaration))
		ir.rootBindings = append(ir.rootBindings, rootBindingIR{
			role: "condition-field", provenance: field.Provenance,
			declarationCoordinate: declaration.Coordinate, scope: scope,
			sourceName: sourceName, path: append([]normalizer.PathSegment(nil), field.Segments...),
			value: typeReference{key: typeValue.key},
		})
	}
	if err := ir.discoverStructuralTypes(); err != nil {
		return nil, err
	}

	if err := ir.allocatePackageSymbols(); err != nil {
		return nil, err
	}
	return ir, nil
}

func (ir *packageIR) addRequest(request *symbolRequest) *symbolRequest {
	if existing, ok := ir.requestByKey[request.key]; ok {
		return existing
	}
	ir.requestByKey[request.key] = request
	ir.requests = append(ir.requests, request)
	return request
}

func (ir *packageIR) ensureNamedShape(key, coordinate, sourceName string, base []string, prefixes [][]string, context shapeContext, shape normalizer.Shape) (*typeIR, error) {
	if existing, ok := ir.types[key]; ok {
		return existing, nil
	}
	typeValue := &typeIR{
		key: key, coordinate: coordinate, sourceName: sourceName,
		request: ir.addRequest(&symbolRequest{
			key: "type:" + key, coordinate: coordinate, category: "generated type",
			base: append([]string(nil), base...), prefixGroups: cloneTokenGroups(prefixes),
		}),
		methods: map[string]bool{}, building: true,
	}
	ir.types[key] = typeValue
	if shape.Kind == "ref" {
		resolved, err := ir.resolveReference(shape)
		if err != nil {
			return nil, err
		}
		shape = resolved.Shape
	}
	typeValue.provenance = shape.Provenance
	switch shape.Kind {
	case "scalar":
		typeValue.kind = "scalar"
		typeValue.underlying = goScalar(shape.Scalar)
		typeValue.sample = normalizedSample(shape)
		typeValue.members = ir.stringMembers(context, shape)
		if domain, ok := ir.domains[scopePathKey(context.scope.Kind, context.scope.InterfaceType, pathString(context.semanticPath))]; ok && shape.Scalar == "string" {
			for _, value := range domain.Values {
				if _, isString := value.Value.(string); isString {
					typeValue.domain = domain.Coordinate
					break
				}
			}
		}
	case "object":
		typeValue.kind = "struct"
		fieldNames := map[string]string{}
		for _, property := range shape.Properties {
			propertyTokens := goTokens(property.Name)
			fieldName := pascal(propertyTokens)
			if err := validateGoIdentifier(fieldName, &symbolRequest{
				coordinate: property.Provenance.Coordinate + property.Provenance.JSONPointer,
				category:   "field",
			}); err != nil {
				return nil, err
			}
			if previous, exists := fieldNames[fieldName]; exists {
				return nil, diagnostic("symbol", "RCG2006", coordinate, fmt.Sprintf("Go field %q collides between properties %q and %q", fieldName, previous, property.Name))
			}
			fieldNames[fieldName] = property.Name
			propertyContext := context
			propertyContext.path = appendPath(context.path, pathPart{name: property.Name, tokens: propertyTokens})
			propertyContext.semanticPath = appendPath(context.semanticPath, pathPart{name: property.Name, tokens: propertyTokens})
			reference, err := ir.referenceForShape(propertyContext, property.Shape, property.Required)
			if err != nil {
				return nil, err
			}
			typeValue.fields = append(typeValue.fields, fieldIR{
				provenance: property.Provenance,
				sourceName: property.Name, name: fieldName, typeRef: reference, required: property.Required,
			})
		}
	case "array":
		typeValue.kind = "slice"
		if shape.Items == nil {
			return nil, diagnostic("model", "RCG1017", coordinate, "array shape has no item shape")
		}
		itemContext := context
		if len(itemContext.path) != 0 {
			itemContext.path = clonePath(itemContext.path)
			itemContext.path[len(itemContext.path)-1].array = true
		}
		if len(itemContext.semanticPath) != 0 {
			itemContext.semanticPath = clonePath(itemContext.semanticPath)
			itemContext.semanticPath[len(itemContext.semanticPath)-1].array = true
		}
		reference, err := ir.referenceForArrayItem(itemContext, *shape.Items)
		if err != nil {
			return nil, err
		}
		typeValue.element = reference
		typeValue.elementSource = shape.Items.Provenance
	case "map":
		typeValue.kind = "map"
		if shape.MapValues == nil {
			return nil, diagnostic("model", "RCG1018", coordinate, "map shape has no value shape")
		}
		reference, err := ir.referenceForMapValue(context, *shape.MapValues)
		if err != nil {
			return nil, err
		}
		typeValue.element = reference
		typeValue.elementSource = shape.MapValues.Provenance
	case "union":
		typeValue.kind = "union"
		for index, variant := range shape.Variants {
			variantKey, err := ir.ensureUnionVariant(typeValue, context, index, variant)
			if err != nil {
				return nil, err
			}
			typeValue.unionVariants = append(typeValue.unionVariants, variantKey)
			typeValue.variantSource = append(typeValue.variantSource, variant.Provenance)
		}
	case "any":
		return nil, diagnostic("model", "RCG1024", coordinate, "unconstrained shape has no supported named Go declaration representation")
	default:
		return nil, diagnostic("model", "RCG1020", coordinate, fmt.Sprintf("unknown structural node %q", shape.Kind))
	}
	typeValue.building = false
	typeValue.built = true
	return typeValue, nil
}

func (ir *packageIR) referenceForShape(context shapeContext, shape normalizer.Shape, required bool) (typeReference, error) {
	if shape.Kind == "ref" {
		definition, err := ir.ensureDefinition(context, shape)
		if err != nil {
			return typeReference{}, err
		}
		return typeReference{key: definition.key, pointer: optionalPointer(definition, required)}, nil
	}
	if shape.Kind == "scalar" && len(ir.stringMembers(context, shape)) == 0 {
		return typeReference{builtin: goScalar(shape.Scalar), pointer: !required, sample: normalizedSample(shape)}, nil
	}
	if shape.Kind == "any" {
		return typeReference{builtin: "any"}, nil
	}
	if len(context.path) == 0 {
		return typeReference{}, diagnostic("model", "RCG2007", shape.Provenance.Coordinate, "public shape has no canonical path")
	}
	last := context.path[len(context.path)-1]
	prefixes := pathPrefixes(context.path)
	prefixes = append(prefixes, scopePrefixes(ir.model, context.scope)...)
	key := "shape:" + context.scope.Coordinate + ":" + pathString(context.path) + ":" + shape.Kind
	typeValue, err := ir.ensureNamedShape(key, shape.Provenance.Coordinate+shape.Provenance.JSONPointer, last.name, last.tokens, prefixes, context, shape)
	if err != nil {
		return typeReference{}, err
	}
	return typeReference{key: typeValue.key, pointer: optionalPointer(typeValue, required)}, nil
}

func optionalPointer(typeValue *typeIR, required bool) bool {
	if required {
		return false
	}
	if typeValue.kind == "struct" {
		return true
	}
	return typeValue.kind == "scalar" && len(typeValue.members) != 0
}

func (ir *packageIR) referenceForArrayItem(context shapeContext, shape normalizer.Shape) (typeReference, error) {
	if shape.Kind == "object" || shape.Kind == "array" || shape.Kind == "map" || shape.Kind == "union" || (shape.Kind == "scalar" && len(ir.stringMembers(context, shape)) != 0) {
		last := context.path[len(context.path)-1]
		base := append(append([]string(nil), last.tokens...), "item")
		prefixes := pathPrefixes(context.path)
		prefixes = append(prefixes, scopePrefixes(ir.model, context.scope)...)
		key := "array-item:" + context.scope.Coordinate + ":" + pathString(context.path) + ":" + shape.Kind
		typeValue, err := ir.ensureNamedShape(key, shape.Provenance.Coordinate+shape.Provenance.JSONPointer, last.name, base, prefixes, context, shape)
		if err != nil {
			return typeReference{}, err
		}
		return typeReference{key: typeValue.key}, nil
	}
	return ir.referenceForShape(context, shape, true)
}

func (ir *packageIR) referenceForMapValue(context shapeContext, shape normalizer.Shape) (typeReference, error) {
	if shape.Kind == "object" || shape.Kind == "array" || shape.Kind == "map" || shape.Kind == "union" || (shape.Kind == "scalar" && len(ir.stringMembers(context, shape)) != 0) {
		last := context.path[len(context.path)-1]
		base := append(append([]string(nil), last.tokens...), "value")
		prefixes := pathPrefixes(context.path)
		prefixes = append(prefixes, scopePrefixes(ir.model, context.scope)...)
		key := "map-value:" + context.scope.Coordinate + ":" + pathString(context.path) + ":" + shape.Kind
		typeValue, err := ir.ensureNamedShape(key, shape.Provenance.Coordinate+shape.Provenance.JSONPointer, last.name, base, prefixes, context, shape)
		if err != nil {
			return typeReference{}, err
		}
		return typeReference{key: typeValue.key}, nil
	}
	return ir.referenceForShape(context, shape, true)
}

func (ir *packageIR) ensureDefinition(context shapeContext, shape normalizer.Shape) (*typeIR, error) {
	schema, ok := ir.schemas[shape.Provenance.Coordinate]
	if !ok {
		return nil, diagnostic("model", "RCG2008", shape.Provenance.Coordinate, fmt.Sprintf("reference %q has no normalized schema", shape.Ref))
	}
	name := definitionName(shape.Ref)
	for _, definition := range schema.Definitions {
		if definition.Name != name {
			continue
		}
		prefixes := scopePrefixes(ir.model, context.scope)
		key := "definition:" + schema.Coordinate + ":" + definition.JSONPointer
		definitionContext := context
		definitionTokens := goTokens(definition.Name)
		definitionContext.path = []pathPart{{name: definition.Name, tokens: definitionTokens}}
		definitionContext.semanticPath = clonePath(definitionContext.path)
		return ir.ensureNamedShape(key, definition.Provenance.Coordinate+definition.JSONPointer, definition.Name, definitionTokens, prefixes, definitionContext, definition.Shape)
	}
	return nil, diagnostic("model", "RCG2009", shape.Provenance.Coordinate, fmt.Sprintf("reference %q has no normalized definition", shape.Ref))
}

func (ir *packageIR) resolveReference(shape normalizer.Shape) (normalizer.NamedShape, error) {
	schema, ok := ir.schemas[shape.Provenance.Coordinate]
	if !ok {
		return normalizer.NamedShape{}, diagnostic("model", "RCG2008", shape.Provenance.Coordinate, fmt.Sprintf("reference %q has no normalized schema", shape.Ref))
	}
	name := definitionName(shape.Ref)
	for _, definition := range schema.Definitions {
		if definition.Name == name {
			return definition, nil
		}
	}
	return normalizer.NamedShape{}, diagnostic("model", "RCG2009", shape.Provenance.Coordinate, fmt.Sprintf("reference %q has no normalized definition", shape.Ref))
}

func (ir *packageIR) ensureUnionVariant(union *typeIR, context shapeContext, index int, shape normalizer.Shape) (string, error) {
	if shape.Kind == "ref" {
		definition, err := ir.ensureDefinition(context, shape)
		if err != nil {
			return "", err
		}
		definition.unionParents = appendUnique(definition.unionParents, union.key)
		return definition.key, nil
	}
	label := shape.Kind
	if shape.Kind == "scalar" {
		label = shape.Scalar
	}
	base := append(append([]string(nil), union.request.base...), label)
	key := fmt.Sprintf("%s:variant:%d", union.key, index)
	variantContext := context
	typeValue, err := ir.ensureNamedShape(key, shape.Provenance.Coordinate+shape.Provenance.JSONPointer, union.sourceName, base, union.request.prefixGroups, variantContext, shape)
	if err != nil {
		return "", err
	}
	typeValue.unionParents = appendUnique(typeValue.unionParents, union.key)
	return typeValue.key, nil
}

func (ir *packageIR) addMethod(typeValue *typeIR, method string) {
	if typeValue.methods[method] {
		return
	}
	typeValue.methods[method] = true
	if typeValue.kind == "union" {
		for _, key := range typeValue.unionVariants {
			ir.addMethod(ir.types[key], method)
		}
	}
}

func (ir *packageIR) stringMembers(context shapeContext, shape normalizer.Shape) []normalizer.NormalizedValue {
	if shape.Kind != "scalar" || shape.Scalar != "string" {
		return nil
	}
	if domain, ok := ir.domains[scopePathKey(context.scope.Kind, context.scope.InterfaceType, pathString(context.semanticPath))]; ok {
		var result []normalizer.NormalizedValue
		for _, value := range domain.Values {
			if _, ok := value.Value.(string); ok {
				result = append(result, value)
			}
		}
		if len(result) != 0 {
			return result
		}
	}
	var result []normalizer.NormalizedValue
	for _, value := range shape.Values {
		if _, ok := value.Value.(string); ok {
			result = append(result, value)
		}
	}
	return result
}

func (ir *packageIR) discoverStructuralTypes() error {
	ownedFields := map[string]bool{}
	for _, field := range ir.model.Vocabulary.ConditionFields {
		if field.Owner == ir.model.RootExtension.ID {
			ownedFields[scopePathKey(field.Kind, field.InterfaceType, field.Path)] = true
		}
	}
	for _, scope := range ir.model.Scopes {
		if scope.Projection == nil || scope.Projection.Kind != "object" {
			continue
		}
		for _, property := range scope.Projection.Properties {
			if property.Name == "interface" || property.Provenance.Owner != ir.model.RootExtension.ID {
				continue
			}
			path := []pathPart{{name: property.Name, tokens: goTokens(property.Name)}}
			if ownedFields[scopePathKey(scope.Kind, scope.InterfaceType, pathString(path))] {
				continue
			}
			context := shapeContext{scope: scope, path: path, semanticPath: clonePath(path)}
			reference, err := ir.referenceForShape(context, property.Shape, property.Required)
			if err != nil {
				return err
			}
			declaration, ok := ir.declarationsByKind[scope.Kind]
			if !ok {
				return diagnostic("model", "RCG2003", scope.Coordinate, fmt.Sprintf("kind %q has no declaration contract", scope.Kind))
			}
			if reference.key == "" {
				last := path[len(path)-1]
				prefixes := append(pathPrefixes(path), scopePrefixes(ir.model, scope)...)
				key := "shape:" + scope.Coordinate + ":" + pathString(path) + ":" + property.Shape.Kind
				typeValue, err := ir.ensureNamedShape(key, property.Shape.Provenance.Coordinate+property.Shape.Provenance.JSONPointer,
					property.Name, last.tokens, prefixes, context, property.Shape)
				if err != nil {
					return err
				}
				reference.key = typeValue.key
				reference.builtin = ""
			}
			ir.addMethod(ir.types[reference.key], markerMethod(declaration))
			ir.rootBindings = append(ir.rootBindings, rootBindingIR{
				role: "schema-field", provenance: property.Provenance,
				declarationCoordinate: declaration.Coordinate, scope: scope,
				sourceName: property.Name, path: []normalizer.PathSegment{{Name: property.Name}},
				value: reference,
			})
		}
	}
	return nil
}

type constantCandidate struct {
	typeKey    string
	coordinate string
	value      string
	suffix     string
}

type packageSymbol struct {
	request  *symbolRequest
	constant *constantCandidate
	info     *symbolRequest
}

func (ir *packageIR) allocatePackageSymbols() error {
	for _, request := range ir.requests {
		request.level = 0
		request.name = pascal(request.base)
		if err := validateGoIdentifier(request.name, request); err != nil {
			return err
		}
	}
	var candidates []constantCandidate
	for _, key := range sortedTypeKeys(ir.types) {
		typeValue := ir.types[key]
		seen := map[string]string{}
		for _, member := range typeValue.members {
			value, ok := member.Value.(string)
			if !ok {
				continue
			}
			suffix := pascal(goTokens(value))
			if err := validateGoIdentifier(suffix, &symbolRequest{
				coordinate: typeValue.coordinate + ":" + strconv.Quote(value),
				category:   "value-domain member",
			}); err != nil {
				return err
			}
			if previous, exists := seen[suffix]; exists {
				name := typeValue.request.name + suffix
				return diagnostic("symbol", "RCG2010", typeValue.coordinate, fmt.Sprintf("Go value member %q collides between exact values %q and %q", name, previous, value))
			}
			seen[suffix] = value
			candidates = append(candidates, constantCandidate{
				typeKey: key, coordinate: typeValue.coordinate + ":" + strconv.Quote(value),
				value: value, suffix: suffix,
			})
		}
	}

	for {
		groups := map[string][]packageSymbol{}
		for _, request := range ir.requests {
			groups[request.name] = append(groups[request.name], packageSymbol{request: request, info: request})
		}
		for index := range candidates {
			candidate := &candidates[index]
			name := ir.types[candidate.typeKey].request.name + candidate.suffix
			info := &symbolRequest{coordinate: candidate.coordinate, category: "value-domain member", name: name}
			groups[name] = append(groups[name], packageSymbol{constant: candidate, info: info})
		}
		var names []string
		for name, group := range groups {
			if len(group) > 1 {
				names = append(names, name)
			}
		}
		if len(names) == 0 {
			break
		}
		sort.Strings(names)
		advance := map[*symbolRequest]bool{}
		for _, name := range names {
			group := groups[name]
			for _, symbol := range group {
				if symbol.request != nil && symbol.request.fixed {
					return symbolCollision(ir.target.ModulePath, name, group[0].info, group[1].info)
				}
			}
			for _, symbol := range group {
				request := symbol.request
				if symbol.constant != nil {
					request = ir.types[symbol.constant.typeKey].request
				}
				if request.fixed || request.level >= len(request.prefixGroups) {
					return symbolCollision(ir.target.ModulePath, name, group[0].info, group[1].info)
				}
				advance[request] = true
			}
		}
		for request := range advance {
			request.level++
			request.name = allocatedName(request)
			if err := validateGoIdentifier(request.name, request); err != nil {
				return err
			}
		}
	}

	for _, candidate := range candidates {
		name := ir.types[candidate.typeKey].request.name + candidate.suffix
		ir.constants = append(ir.constants, constantIR{
			coordinate: candidate.coordinate, sourceName: candidate.value,
			name: name, typeKey: candidate.typeKey, value: candidate.value,
		})
	}
	sort.Slice(ir.constants, func(i, j int) bool { return ir.constants[i].name < ir.constants[j].name })
	return nil
}

func (ir *packageIR) files() (map[string][]byte, error) {
	bindings, err := ir.renderBindings()
	if err != nil {
		return nil, err
	}
	conformance, err := ir.renderConformance()
	if err != nil {
		return nil, err
	}
	manifest, err := marshalManifest(ir.manifest())
	if err != nil {
		return nil, err
	}
	return map[string][]byte{
		generatedGoFile:                   bindings,
		"conformance/conformance_test.go": conformance,
		"go.mod":                          ir.renderGoMod(),
		"runtimeconditions.bindings.yaml": manifest,
	}, nil
}

func (ir *packageIR) renderBindings() ([]byte, error) {
	var buffer bytes.Buffer
	writeGoHeader(&buffer, ir.model.Metadata.SemanticSHA256)
	fmt.Fprintf(&buffer, "package %s\n\n", ir.target.PackageName)
	buffer.WriteString("// Declaration is the inert result of a Runtime Conditions declaration.\n")
	buffer.WriteString("type Declaration struct{}\n\n")
	sort.Slice(ir.declarations, func(i, j int) bool { return ir.declarations[i].function.name < ir.declarations[j].function.name })
	for _, declaration := range ir.declarations {
		fmt.Fprintf(&buffer, "// %sField is the field contract for %s declarations.\n", declaration.function.name, declaration.function.name)
		fmt.Fprintf(&buffer, "type %s interface {\n\t%s()\n}\n\n", declaration.fieldInterface.name, declaration.markerMethod)
		fmt.Fprintf(&buffer, "// %s declares a %s Condition.\n", declaration.function.name, declaration.model.Kind)
		fmt.Fprintf(&buffer, "func %s(fields ...%s) Declaration { return Declaration{} }\n\n", declaration.function.name, declaration.fieldInterface.name)
	}
	keys := sortedTypeKeys(ir.types)
	for _, key := range keys {
		typeValue := ir.types[key]
		name := typeValue.request.name
		fmt.Fprintf(&buffer, "// %s is generated from %s.\n", name, typeValue.coordinate)
		switch typeValue.kind {
		case "scalar":
			fmt.Fprintf(&buffer, "type %s %s\n", name, typeValue.underlying)
		case "struct":
			fmt.Fprintf(&buffer, "type %s struct {\n", name)
			for _, field := range typeValue.fields {
				fmt.Fprintf(&buffer, "\t%s %s\n", field.name, ir.renderTypeReference(field.typeRef))
			}
			buffer.WriteString("}\n")
		case "slice":
			fmt.Fprintf(&buffer, "type %s []%s\n", name, ir.renderTypeReference(typeValue.element))
		case "map":
			fmt.Fprintf(&buffer, "type %s map[string]%s\n", name, ir.renderTypeReference(typeValue.element))
		case "union":
			fmt.Fprintf(&buffer, "type %s interface {\n\t%s()\n", name, unionMethod(name))
			for _, method := range sortedMethods(typeValue.methods) {
				fmt.Fprintf(&buffer, "\t%s()\n", method)
			}
			buffer.WriteString("}\n")
		case "any":
			fmt.Fprintf(&buffer, "type %s interface{}\n", name)
		}
		if typeValue.kind != "union" {
			for _, parent := range typeValue.unionParents {
				fmt.Fprintf(&buffer, "\nfunc (%s) %s() {}\n", name, unionMethod(ir.types[parent].request.name))
			}
			for _, method := range sortedMethods(typeValue.methods) {
				fmt.Fprintf(&buffer, "\nfunc (%s) %s() {}\n", name, method)
			}
		}
		buffer.WriteByte('\n')
	}
	if len(ir.constants) != 0 {
		buffer.WriteString("const (\n")
		for _, constant := range ir.constants {
			fmt.Fprintf(&buffer, "\t%s %s = %s\n", constant.name, ir.types[constant.typeKey].request.name, strconv.Quote(constant.value))
		}
		buffer.WriteString(")\n")
	}
	return format.Source(buffer.Bytes())
}

func (ir *packageIR) renderConformance() ([]byte, error) {
	var buffer bytes.Buffer
	writeGoHeader(&buffer, ir.model.Metadata.SemanticSHA256)
	buffer.WriteString("package conformance_test\n\n")
	alias := lowerIdentifier([]string{ir.target.PackageName})
	fmt.Fprintf(&buffer, "import %s %q\n\n", alias, ir.target.ModulePath)
	fmt.Fprintf(&buffer, "var _ %s.Declaration\n", alias)
	keys := sortedTypeKeys(ir.types)
	for _, key := range keys {
		expression := ir.conformanceExpression(key, alias, map[string]bool{})
		fmt.Fprintf(&buffer, "var _ %s.%s = %s\n", alias, ir.types[key].request.name, expression)
		for _, alternative := range ir.optionalSamples(key, alias) {
			fmt.Fprintf(&buffer, "var _ %s.%s = %s\n", alias, ir.types[key].request.name, alternative)
		}
	}
	if len(ir.constants) != 0 {
		buffer.WriteByte('\n')
		for _, constant := range ir.constants {
			fmt.Fprintf(&buffer, "var _ = %s.%s\n", alias, constant.name)
		}
	}
	for _, declaration := range ir.declarations {
		argumentsByScope := map[string][]string{}
		rootTypes := map[string]bool{}
		fmt.Fprintf(&buffer, "\nvar _ %s.%s\n", alias, declaration.fieldInterface.name)
		for _, binding := range ir.rootBindings {
			if binding.declarationCoordinate == declaration.model.Coordinate {
				expression := ir.referenceExpression(binding.value, alias, map[string]bool{})
				argumentsByScope[binding.scope.Coordinate] = append(argumentsByScope[binding.scope.Coordinate], expression)
				rootTypes[binding.value.key] = true
				fmt.Fprintf(&buffer, "\nvar _ %s.%s = %s\n", alias, declaration.fieldInterface.name, expression)
			}
		}
		for _, key := range keys {
			if !rootTypes[key] && ir.types[key].methods[declaration.markerMethod] {
				expression := ir.conformanceExpression(key, alias, map[string]bool{})
				fmt.Fprintf(&buffer, "\nvar _ %s.%s = %s\n", alias, declaration.fieldInterface.name, expression)
			}
		}
		if len(argumentsByScope) == 0 {
			// A declaration-only package cannot invent an interface owned by a
			// consumer's other dependency. Its complete calls live in consumer
			// fixtures; this reference retains native API coverage here.
			fmt.Fprintf(&buffer, "var _ = %s.%s\n", alias, declaration.function.name)
			continue
		}
		var scopes []string
		for scope := range argumentsByScope {
			scopes = append(scopes, scope)
		}
		sort.Strings(scopes)
		for _, scope := range scopes {
			fmt.Fprintf(&buffer, "\nvar _ %s.Declaration = %s.%s(%s)\n", alias, alias, declaration.function.name, strings.Join(argumentsByScope[scope], ", "))
			for _, binding := range ir.rootBindings {
				if binding.declarationCoordinate != declaration.model.Coordinate || binding.scope.Coordinate != scope {
					continue
				}
				for _, alternative := range ir.optionalSamples(binding.value.key, alias) {
					arguments := append([]string(nil), argumentsByScope[scope]...)
					original := ir.referenceExpression(binding.value, alias, map[string]bool{})
					if binding.value.pointer {
						alternative = "&" + alternative
					}
					for index, argument := range arguments {
						if argument == original {
							arguments[index] = alternative
							break
						}
					}
					fmt.Fprintf(&buffer, "var _ %s.Declaration = %s.%s(%s)\n", alias, alias, declaration.function.name, strings.Join(arguments, ", "))
				}
			}
		}
	}
	for _, declaration := range ir.model.Vocabulary.ImportedDeclarations {
		declarationTokens := goTokens(declaration.Kind)
		method := markerMethod(declaration)
		localName := "imported" + pascal(declarationTokens) + "Field"
		fmt.Fprintf(&buffer, "\ntype %s interface { %s() }\n", localName, method)
		for _, binding := range ir.rootBindings {
			if binding.declarationCoordinate == declaration.Coordinate {
				fmt.Fprintf(&buffer, "var _ %s = %s\n", localName, ir.referenceExpression(binding.value, alias, map[string]bool{}))
			}
		}
		for _, key := range keys {
			if ir.types[key].methods[method] {
				fmt.Fprintf(&buffer, "var _ %s = %s\n", localName, ir.conformanceExpression(key, alias, map[string]bool{}))
			}
		}
	}
	if ir.sampleError != nil {
		return nil, ir.sampleError
	}
	return format.Source(buffer.Bytes())
}

// optionalSamples exercises each optional field separately. Native optionality
// comes from the normalized projection; schema branch validation stays external.
func (ir *packageIR) optionalSamples(key, alias string) []string {
	typeValue := ir.types[key]
	if typeValue == nil || typeValue.kind != "struct" {
		return nil
	}
	var optional []fieldIR
	for _, field := range typeValue.fields {
		if !field.required {
			optional = append(optional, field)
		}
	}
	if len(optional) < 2 {
		return nil
	}
	var result []string
	for _, selected := range optional[1:] {
		var fields []string
		for _, field := range typeValue.fields {
			if field.required || field.name == selected.name {
				fields = append(fields, field.name+": "+ir.referenceExpression(field.typeRef, alias, map[string]bool{key: true}))
			}
		}
		result = append(result, alias+"."+typeValue.request.name+"{"+strings.Join(fields, ", ")+"}")
	}
	return result
}

func normalizedSample(shape normalizer.Shape) string {
	if len(shape.Values) == 0 {
		return ""
	}
	if value, ok := shape.Values[0].Value.(string); ok {
		return strconv.Quote(value)
	}
	if shape.Values[0].Value == nil {
		return "struct{}{}"
	}
	return fmt.Sprint(shape.Values[0].Value)
}

func (ir *packageIR) conformanceExpression(key, alias string, active map[string]bool) string {
	typeValue := ir.types[key]
	qualified := alias + "." + typeValue.request.name
	if active[key] {
		switch typeValue.kind {
		case "struct":
			var fields []string
			for _, field := range typeValue.fields {
				if !field.required {
					continue
				}
				if field.typeRef.key != "" && active[field.typeRef.key] {
					ir.sampleError = diagnostic("model", "RCG1025", field.provenance.Coordinate+field.provenance.JSONPointer, "required recursive field has no finite conformance sample")
					return qualified + "{}"
				}
				fields = append(fields, field.name+": "+ir.referenceExpression(field.typeRef, alias, active))
			}
			return qualified + "{" + strings.Join(fields, ", ") + "}"
		case "slice":
			return qualified + "{}"
		case "map":
			return qualified + "{}"
		case "scalar":
			return qualified + "(" + zeroScalar(typeValue.underlying) + ")"
		}
	}
	copyActive := cloneBoolMap(active)
	copyActive[key] = true
	switch typeValue.kind {
	case "scalar":
		for _, constant := range ir.constants {
			if constant.typeKey == key {
				return alias + "." + constant.name
			}
		}
		if typeValue.sample != "" {
			return qualified + "(" + typeValue.sample + ")"
		}
		return qualified + "(" + zeroScalar(typeValue.underlying) + ")"
	case "struct":
		var fields []string
		selectedOptional := false
		for _, field := range typeValue.fields {
			if !field.required {
				if selectedOptional {
					continue
				}
				selectedOptional = true
			}
			fields = append(fields, field.name+": "+ir.referenceExpression(field.typeRef, alias, copyActive))
		}
		return qualified + "{" + strings.Join(fields, ", ") + "}"
	case "slice":
		return qualified + "{" + ir.referenceExpression(typeValue.element, alias, copyActive) + "}"
	case "map":
		return qualified + "{\"key\": " + ir.referenceExpression(typeValue.element, alias, copyActive) + "}"
	case "union":
		if len(typeValue.unionVariants) == 0 {
			return "nil"
		}
		return ir.conformanceExpression(typeValue.unionVariants[0], alias, copyActive)
	default:
		return "nil"
	}
}

func (ir *packageIR) referenceExpression(reference typeReference, alias string, active map[string]bool) string {
	var expression string
	if reference.key != "" {
		expression = ir.conformanceExpression(reference.key, alias, active)
	} else {
		expression = zeroScalar(reference.builtin)
		if reference.sample != "" {
			expression = reference.sample
		}
	}
	if reference.pointer {
		if reference.key != "" && ir.types[reference.key].kind == "struct" {
			return "&" + expression
		}
		nativeType := reference.builtin
		if reference.key != "" {
			nativeType = alias + "." + ir.types[reference.key].request.name
		}
		return "&[]" + nativeType + "{" + expression + "}[0]"
	}
	return expression
}

func (ir *packageIR) renderTypeReference(reference typeReference) string {
	value := reference.builtin
	if reference.key != "" {
		value = ir.types[reference.key].request.name
	}
	if reference.pointer {
		return "*" + value
	}
	return value
}

func (ir *packageIR) renderGoMod() []byte {
	var buffer bytes.Buffer
	writeGeneratedHeader(&buffer, "//", ir.model.Metadata.SemanticSHA256)
	fmt.Fprintf(&buffer, "module %s\n\ngo %s\n", ir.target.ModulePath, ir.target.MinimumGoVersion)
	if len(ir.target.Dependencies) != 0 {
		dependencies := append([]PackageDependency(nil), ir.target.Dependencies...)
		sort.Slice(dependencies, func(i, j int) bool { return dependencies[i].ModulePath < dependencies[j].ModulePath })
		buffer.WriteString("\nrequire (\n")
		for _, dependency := range dependencies {
			fmt.Fprintf(&buffer, "\t%s %s\n", dependency.ModulePath, dependency.Version)
		}
		buffer.WriteString(")\n")
	}
	return buffer.Bytes()
}

func (ir *packageIR) manifest() Manifest {
	manifest := Manifest{
		APIVersion: ManifestAPIVersion,
		Kind:       ManifestKind,
		Generated:  ManifestGenerated{NonEditable: true, Emitter: EmitterName, Version: EmitterVersion},
		Model:      ManifestModel{APIVersion: ir.model.APIVersion, SemanticSHA256: ir.model.Metadata.SemanticSHA256},
		Extension:  ManifestExtension{ID: ir.model.RootExtension.ID, SemanticSHA256: ir.model.RootExtension.SemanticSHA256},
		Package: ManifestPackage{
			Language: "go", Coordinate: ir.target.ModulePath, Name: ir.target.PackageName,
			Version: ir.target.Version, MinimumGoVersion: ir.target.MinimumGoVersion,
		},
		Declarations:            []ManifestDeclaration{},
		ImportedMarkerContracts: []ManifestImportedMarker{},
		RootBindings:            []ManifestRootBinding{},
		Types:                   []ManifestNamedType{},
	}
	for _, declaration := range ir.declarations {
		manifest.Declarations = append(manifest.Declarations, ManifestDeclaration{
			ModelRef: manifestModelRef(declaration.model.Provenance), Owner: declaration.model.Owner,
			SourceName: declaration.model.Kind, Function: declaration.function.name,
			MarkerInterface: declaration.fieldInterface.name, MarkerMethod: declaration.markerMethod,
			File: generatedGoFile,
		})
	}
	for _, declaration := range ir.model.Vocabulary.ImportedDeclarations {
		manifest.ImportedMarkerContracts = append(manifest.ImportedMarkerContracts, ManifestImportedMarker{
			ModelRef: manifestModelRef(declaration.Provenance), Owner: declaration.Owner,
			SourceName:      declaration.Kind,
			MarkerInterface: pascal(append(goTokens(declaration.Kind), "field")),
			MarkerMethod:    markerMethod(declaration),
		})
	}
	for _, root := range ir.rootBindings {
		manifest.RootBindings = append(manifest.RootBindings, ManifestRootBinding{
			Role: root.role, ModelRef: manifestModelRef(root.provenance),
			DeclarationCoordinate: root.declarationCoordinate,
			Scope:                 ManifestScope{Kind: root.scope.Kind, InterfaceType: root.scope.InterfaceType},
			SourceName:            root.sourceName, Path: root.path,
			Value: ir.manifestReference(root.value), FixedInterfaceType: root.fixedInterfaceType,
		})
	}
	for _, key := range sortedTypeKeys(ir.types) {
		typeValue := ir.types[key]
		entry := ManifestNamedType{
			ModelRef: manifestModelRef(typeValue.provenance), SourceName: typeValue.sourceName,
			NativeName: typeValue.request.name,
			Construct:  map[string]string{"struct": "object", "slice": "collection", "map": "map", "union": "union", "scalar": "scalar", "any": "any"}[typeValue.kind],
			File:       generatedGoFile,
		}
		for _, declaration := range ir.model.Vocabulary.OwnedDeclarations {
			method := markerMethod(declaration)
			if typeValue.methods[method] {
				entry.Implements = append(entry.Implements, ManifestImplements{DeclarationCoordinate: declaration.Coordinate, MarkerMethod: method})
			}
		}
		for _, declaration := range ir.model.Vocabulary.ImportedDeclarations {
			method := markerMethod(declaration)
			if typeValue.methods[method] {
				entry.Implements = append(entry.Implements, ManifestImplements{DeclarationCoordinate: declaration.Coordinate, MarkerMethod: method})
			}
		}
		sort.Slice(entry.Implements, func(i, j int) bool {
			return entry.Implements[i].DeclarationCoordinate < entry.Implements[j].DeclarationCoordinate
		})
		if typeValue.kind == "scalar" {
			entry.Underlying = typeValue.underlying
		}
		if typeValue.kind == "struct" {
			fields := make([]ManifestField, 0, len(typeValue.fields))
			for _, field := range typeValue.fields {
				fields = append(fields, ManifestField{
					ModelRef: manifestModelRef(field.provenance), SourceName: field.sourceName,
					NativeName: field.name, Required: field.required,
					Value: ir.manifestReference(field.typeRef),
				})
			}
			entry.Fields = &fields
		}
		if typeValue.kind == "slice" || typeValue.kind == "map" {
			entry.Element = &ManifestElement{ModelRef: manifestModelRef(typeValue.elementSource), Value: ir.manifestReference(typeValue.element)}
		}
		for index, variant := range typeValue.unionVariants {
			entry.Variants = append(entry.Variants, ManifestVariant{
				ModelRef: manifestModelRef(typeValue.variantSource[index]),
				Value:    ir.manifestReference(typeReference{key: variant}),
			})
		}
		memberRef := typeValue.provenance
		if typeValue.domain != "" {
			for _, domain := range ir.model.Vocabulary.ValueDomains {
				if domain.Coordinate == typeValue.domain {
					memberRef = domain.Provenance
					break
				}
			}
		}
		for _, constant := range ir.constants {
			if constant.typeKey == key {
				entry.Members = append(entry.Members, ManifestMember{
					ModelRef: manifestModelRef(memberRef), NativeName: constant.name, Value: constant.value,
				})
			}
		}
		manifest.Types = append(manifest.Types, entry)
	}
	return manifest
}

func manifestModelRef(provenance normalizer.Provenance) ManifestModelRef {
	return ManifestModelRef{Coordinate: provenance.Coordinate, JSONPointer: provenance.JSONPointer}
}

func (ir *packageIR) manifestReference(reference typeReference) ManifestReference {
	result := ManifestReference{Builtin: reference.builtin, Pointer: reference.pointer}
	if reference.key != "" {
		result.Type = ir.types[reference.key].request.name
	}
	return result
}

func findScope(model normalizer.BindingModel, kind, interfaceType string) (normalizer.ScopeModel, bool) {
	for _, scope := range model.Scopes {
		if scope.Kind == kind && scope.InterfaceType == interfaceType {
			return scope, true
		}
	}
	return normalizer.ScopeModel{}, false
}

func findInterface(model normalizer.BindingModel, kind, interfaceType string) (normalizer.InterfaceModel, bool) {
	for _, value := range model.Vocabulary.Interfaces {
		if value.Kind == kind && value.Type == interfaceType {
			return value, true
		}
	}
	return normalizer.InterfaceModel{}, false
}

func findProperty(shape normalizer.Shape, name string) (normalizer.PropertyShape, bool) {
	for _, property := range shape.Properties {
		if property.Name == name {
			return property, true
		}
	}
	return normalizer.PropertyShape{}, false
}

func shapeAtSegments(shape normalizer.Shape, segments []normalizer.PathSegment) (normalizer.Shape, bool) {
	current := shape
	for _, segment := range segments {
		property, ok := findProperty(current, segment.Name)
		if !ok {
			return normalizer.Shape{}, false
		}
		current = property.Shape
		if segment.Array {
			if current.Items == nil {
				return normalizer.Shape{}, false
			}
			current = *current.Items
		}
	}
	return current, true
}

func pathParts(segments []normalizer.PathSegment) []pathPart {
	result := make([]pathPart, 0, len(segments))
	for _, segment := range segments {
		result = append(result, pathPart{name: segment.Name, tokens: goTokens(segment.Name), array: segment.Array})
	}
	return result
}

func pathPrefixes(path []pathPart) [][]string {
	var result [][]string
	for index := len(path) - 2; index >= 0; index-- {
		result = append(result, append([]string(nil), path[index].tokens...))
	}
	return result
}

func scopePrefixes(model normalizer.BindingModel, scope normalizer.ScopeModel) [][]string {
	var result [][]string
	if value, ok := findInterface(model, scope.Kind, scope.InterfaceType); ok {
		result = append(result, goTokens(value.Type))
	}
	declarations := append(append([]normalizer.DeclarationModel(nil), model.Vocabulary.OwnedDeclarations...), model.Vocabulary.ImportedDeclarations...)
	for _, declaration := range declarations {
		if declaration.Kind == scope.Kind {
			result = append(result, goTokens(declaration.Kind))
			break
		}
	}
	return result
}

func scopePathKey(kind, interfaceType, path string) string {
	return kind + "\x00" + interfaceType + "\x00" + path
}

func pathString(path []pathPart) string {
	var parts []string
	for _, part := range path {
		value := part.name
		if part.array {
			value += "[]"
		}
		parts = append(parts, value)
	}
	return strings.Join(parts, ".")
}

func appendPath(path []pathPart, value pathPart) []pathPart {
	result := append([]pathPart(nil), path...)
	return append(result, value)
}

func clonePath(path []pathPart) []pathPart {
	result := make([]pathPart, len(path))
	for index, part := range path {
		result[index] = part
		result[index].tokens = append([]string(nil), part.tokens...)
	}
	return result
}

func cloneTokenGroups(groups [][]string) [][]string {
	result := make([][]string, len(groups))
	for index := range groups {
		result[index] = append([]string(nil), groups[index]...)
	}
	return result
}

func markerMethod(declaration normalizer.DeclarationModel) string {
	identity := declaration.Owner + "\x00" + declaration.Coordinate
	digest := sha256.Sum256([]byte(identity))
	return "RuntimeConditions" + pascal(goTokens(declaration.Kind)) + "Field" + hex.EncodeToString(digest[:])
}

func unionMethod(name string) string {
	return "runtimeConditions" + name
}

func definitionName(reference string) string {
	const prefix = "#/$defs/"
	value := strings.TrimPrefix(reference, prefix)
	if index := strings.IndexByte(value, '/'); index >= 0 {
		value = value[:index]
	}
	value = strings.ReplaceAll(strings.ReplaceAll(value, "~1", "/"), "~0", "~")
	return value
}

func goScalar(value string) string {
	switch value {
	case "string":
		return "string"
	case "boolean":
		return "bool"
	case "integer":
		return "int64"
	case "number":
		return "float64"
	case "null":
		return "struct{}"
	default:
		return "any"
	}
}

func zeroScalar(value string) string {
	switch value {
	case "string":
		return `""`
	case "bool":
		return "false"
	case "int64", "float64":
		return "0"
	case "struct{}":
		return "struct{}{}"
	default:
		return "nil"
	}
}

func sortedTypeKeys(values map[string]*typeIR) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		left, right := values[keys[i]], values[keys[j]]
		if left.request.name != right.request.name {
			return left.request.name < right.request.name
		}
		return keys[i] < keys[j]
	})
	return keys
}

func sortedMethods(values map[string]bool) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func cloneBoolMap(values map[string]bool) map[string]bool {
	result := make(map[string]bool, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

func writeGoHeader(buffer *bytes.Buffer, digest string) {
	writeGeneratedHeader(buffer, "//", digest)
}

func writeGeneratedHeader(buffer *bytes.Buffer, prefix, digest string) {
	fmt.Fprintf(buffer, "%s Code generated by %s %s. DO NOT EDIT.\n", prefix, EmitterName, EmitterVersion)
	fmt.Fprintf(buffer, "%s Binding model SHA-256: %s\n\n", prefix, digest)
}
