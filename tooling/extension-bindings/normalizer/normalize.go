package normalizer

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

type NormalizeConfig struct {
	CoreProfileSchema CoreProfileIdentity
	Normalizer        ToolIdentity
}

func Normalize(closure ResolvedClosure, lock DependencyLock, schemas *Schemas, config NormalizeConfig) (BindingModel, error) {
	if len(closure.Documents) == 0 {
		return BindingModel{}, diagnostic("model", "RCB1301", closure.Root, "", "resolved extension closure is empty")
	}
	if err := ValidateDependencyLock(closure, lock); err != nil {
		return BindingModel{}, err
	}
	if config.CoreProfileSchema.ID == "" || config.CoreProfileSchema.Version == "" || config.CoreProfileSchema.SemanticSHA256 == "" {
		return BindingModel{}, diagnostic("model", "RCB1302", closure.Root, "", "core profile schema identity, version, and semantic digest are required")
	}
	if config.Normalizer.Name == "" {
		config.Normalizer.Name = NormalizerName
	}
	if config.Normalizer.Version == "" {
		config.Normalizer.Version = NormalizerVersion
	}
	if config.Normalizer.SHA256 == "" {
		return BindingModel{}, diagnostic("model", "RCB1303", closure.Root, "", "normalizer digest is required")
	}
	rootDocument, exists := closure.ByID[closure.Root]
	if !exists {
		return BindingModel{}, diagnostic("model", "RCB1304", closure.Root, "", "resolved closure does not contain its root")
	}
	model := BindingModel{
		APIVersion: ModelAPIVersion,
		Kind:       ModelKind,
		Metadata: ModelMetadata{
			Normalizer: config.Normalizer,
		},
		CoreProfileSchema: config.CoreProfileSchema,
		RootExtension: ArtifactIdentity{
			ID: closure.Root, Version: rootDocument.Definition.Metadata.Version,
			SemanticSHA256: rootDocument.SemanticSHA256,
		},
		DependencyEdges: append([]DependencyEdge(nil), closure.Edges...),
	}
	interfaces := interfacesByKind(closure.Documents)
	for _, document := range closure.Documents {
		dependencies := append([]string(nil), document.Definition.Spec.Dependencies...)
		sort.Strings(dependencies)
		model.Extensions = append(model.Extensions, ResolvedExtension{
			ID: document.Definition.Metadata.ID, Version: document.Definition.Metadata.Version,
			SemanticSHA256: document.SemanticSHA256, Dependencies: dependencies,
		})
		appendVocabulary(&model, document, interfaces, document.Definition.Metadata.ID == closure.Root)
		normalizedSchemas, err := normalizeSchemas(document)
		if err != nil {
			return BindingModel{}, err
		}
		model.Schemas = append(model.Schemas, normalizedSchemas...)
	}
	var err error
	model.Scopes, err = normalizeScopes(closure.Documents, model.Schemas)
	if err != nil {
		return BindingModel{}, err
	}
	if err := validateFieldValueDomains(model); err != nil {
		return BindingModel{}, err
	}
	sortModel(&model)
	digest, err := ModelSemanticSHA256(model)
	if err != nil {
		return BindingModel{}, err
	}
	model.Metadata.SemanticSHA256 = digest
	if err := schemas.ValidateModel(model); err != nil {
		return BindingModel{}, err
	}
	return model, nil
}

func appendVocabulary(model *BindingModel, document ResolvedDocument, interfaces map[string][]string, root bool) {
	owner := document.Definition.Metadata.ID
	provenance := func(coordinate, pointer string) Provenance {
		return Provenance{Owner: owner, ExtensionSHA256: document.SemanticSHA256, Coordinate: coordinate, JSONPointer: pointer}
	}
	for _, kind := range document.Definition.Spec.Kinds {
		coordinate := "kind:" + kind.Name
		model.Vocabulary.Owners = append(model.Vocabulary.Owners, VocabularyOwner{
			Coordinate: coordinate, Category: "kind", Owner: owner, Kind: kind.Name,
		})
		if root {
			model.Vocabulary.OwnedDeclarations = append(model.Vocabulary.OwnedDeclarations, DeclarationModel{
				Coordinate: coordinate, Owner: owner, Kind: kind.Name, Tokens: tokenize(kind.Name),
				Provenance: provenance(coordinate, ""),
			})
		}
	}
	for _, interfaceType := range document.Definition.Spec.InterfaceTypes {
		coordinate := "interface:" + interfaceType.TargetKind + ":" + interfaceType.Name
		model.Vocabulary.Owners = append(model.Vocabulary.Owners, VocabularyOwner{
			Coordinate: coordinate, Category: "interface", Owner: owner,
			Kind: interfaceType.TargetKind, InterfaceType: interfaceType.Name,
		})
		model.Vocabulary.Interfaces = append(model.Vocabulary.Interfaces, InterfaceModel{
			Coordinate: coordinate, Owner: owner, Kind: interfaceType.TargetKind, Type: interfaceType.Name,
			Tokens:     tokenize(interfaceType.Name),
			Provenance: provenance(coordinate, ""),
		})
	}
	for _, field := range document.Definition.Spec.ConditionFields {
		for _, kind := range sortedStrings(field.AppliesToKinds) {
			types := sortedStrings(field.AppliesToInterfaceTypes)
			if len(types) == 0 {
				types = interfaces[kind]
			}
			if len(types) == 0 {
				types = []string{""}
			}
			for _, interfaceType := range types {
				coordinate := "condition-field:" + kind + ":" + interfaceType + ":" + field.Name
				path := field.Name
				segments, _ := parsePath(path)
				model.Vocabulary.Owners = append(model.Vocabulary.Owners, VocabularyOwner{
					Coordinate: coordinate, Category: "condition-field", Owner: owner,
					Kind: kind, InterfaceType: interfaceType, Path: path,
				})
				model.Vocabulary.ConditionFields = append(model.Vocabulary.ConditionFields, FieldModel{
					Coordinate: coordinate, Owner: owner, Kind: kind, InterfaceType: interfaceType,
					Path: path, Segments: segments, Tokens: tokenize(field.Name),
					Provenance: provenance(coordinate, ""),
				})
			}
		}
	}
	for _, field := range document.Definition.Spec.InterfaceFields {
		coordinate := "interface-field:" + field.TargetKind + ":" + field.TargetType + ":" + field.Name
		path := "interface." + field.Name
		segments, _ := parsePath(path)
		model.Vocabulary.Owners = append(model.Vocabulary.Owners, VocabularyOwner{
			Coordinate: coordinate, Category: "interface-field", Owner: owner,
			Kind: field.TargetKind, InterfaceType: field.TargetType, Path: path,
		})
		model.Vocabulary.InterfaceFields = append(model.Vocabulary.InterfaceFields, FieldModel{
			Coordinate: coordinate, Owner: owner, Kind: field.TargetKind, InterfaceType: field.TargetType,
			Path: path, Segments: segments, Tokens: tokenize(field.Name),
			Provenance: provenance(coordinate, ""),
		})
	}
	for _, domain := range document.Definition.Spec.FieldValues {
		coordinate := "field-domain:" + domain.TargetKind + ":" + domain.TargetType + ":" + domain.Field
		segments, _ := parsePath(domain.Field)
		values := append([]any(nil), domain.Values...)
		_ = sortAny(values)
		model.Vocabulary.ValueDomains = append(model.Vocabulary.ValueDomains, ValueDomainModel{
			Coordinate: coordinate, Owner: owner, Kind: domain.TargetKind, InterfaceType: domain.TargetType,
			Path: domain.Field, Segments: segments, Values: values,
			Provenance: provenance(coordinate, ""),
		})
		for _, value := range values {
			encoded, _ := canonicalJSON(value)
			model.Vocabulary.Owners = append(model.Vocabulary.Owners, VocabularyOwner{
				Coordinate: coordinate + ":" + string(encoded), Category: "field-value", Owner: owner,
				Kind: domain.TargetKind, InterfaceType: domain.TargetType, Path: domain.Field, Value: value,
			})
		}
	}
}

func normalizeSchemas(document ResolvedDocument) ([]NormalizedSchema, error) {
	owner := document.Definition.Metadata.ID
	result := make([]NormalizedSchema, 0, len(document.Definition.Spec.Schemas))
	for _, schema := range document.Definition.Spec.Schemas {
		coordinate := owner + "#schema:" + schema.ID
		pointer := ""
		exact, ok := canonicalSchemaByID(document.SemanticData, schema.ID)
		if !ok {
			return nil, diagnostic("model", "RCB1313", coordinate, "", "canonical semantic closure does not contain the schema")
		}
		projection, err := projectShape(exact, exact, coordinate, document.SemanticSHA256, pointer, map[string]bool{})
		if err != nil {
			return nil, err
		}
		projection = removeFixedScopeMetadata(projection, schema.AppliesToKind, schema.AppliesToInterfaceType)
		var definitions []NamedShape
		if defs, ok := exact["$defs"].(map[string]any); ok {
			names := referencedDefinitionNames(exact)
			for _, name := range names {
				definitionSchema, ok := defs[name].(map[string]any)
				if !ok {
					continue
				}
				definitionPointer := pointer + "/$defs/" + escapeJSONPointer(name)
				shape, err := projectShape(definitionSchema, exact, coordinate, document.SemanticSHA256, definitionPointer, map[string]bool{})
				if err != nil {
					return nil, err
				}
				definitions = append(definitions, NamedShape{
					Name: name, Tokens: tokenize(name), JSONPointer: "/$defs/" + escapeJSONPointer(name), Shape: shape,
					Provenance: Provenance{Owner: owner, ExtensionSHA256: document.SemanticSHA256, Coordinate: coordinate, JSONPointer: definitionPointer},
				})
			}
		}
		result = append(result, NormalizedSchema{
			Coordinate: coordinate, Owner: owner, ID: schema.ID,
			Kind: schema.AppliesToKind, InterfaceType: schema.AppliesToInterfaceType,
			Exact: exact, Projection: projection, Definitions: definitions,
			Provenance: Provenance{Owner: owner, ExtensionSHA256: document.SemanticSHA256, Coordinate: coordinate, JSONPointer: pointer},
		})
	}
	return result, nil
}

func removeFixedScopeMetadata(shape Shape, kind, interfaceType string) Shape {
	if shape.Kind != "object" {
		return shape
	}
	if kind != "" {
		shape.Properties = removeProperty(shape.Properties, "kind")
		shape.Required = removeString(shape.Required, "kind")
	}
	if interfaceType != "" {
		for index := range shape.Properties {
			if shape.Properties[index].Name != "interface" || shape.Properties[index].Shape.Kind != "object" {
				continue
			}
			interfaceShape := shape.Properties[index].Shape
			interfaceShape.Properties = removeProperty(interfaceShape.Properties, "type")
			interfaceShape.Required = removeString(interfaceShape.Required, "type")
			shape.Properties[index].Shape = interfaceShape
		}
	}
	return shape
}

func removeProperty(properties []PropertyShape, name string) []PropertyShape {
	result := properties[:0]
	for _, property := range properties {
		if property.Name != name {
			result = append(result, property)
		}
	}
	return result
}

func removeString(values []string, value string) []string {
	result := values[:0]
	for _, candidate := range values {
		if candidate != value {
			result = append(result, candidate)
		}
	}
	return result
}

func canonicalSchemaByID(extension map[string]any, id string) (map[string]any, bool) {
	spec, _ := extension["spec"].(map[string]any)
	schemas, _ := spec["schemas"].([]any)
	for _, value := range schemas {
		definition, _ := value.(map[string]any)
		if definition["id"] != id {
			continue
		}
		schema, ok := definition["schema"].(map[string]any)
		return schema, ok
	}
	return nil, false
}

func referencedDefinitionNames(schema map[string]any) []string {
	definitions, _ := schema["$defs"].(map[string]any)
	seen := map[string]bool{}
	var pending []string
	var collect func(any, bool)
	collect = func(value any, skipDefinitions bool) {
		switch typed := value.(type) {
		case map[string]any:
			if reference, ok := typed["$ref"].(string); ok {
				if name, ok := definitionNameFromReference(reference); ok && definitions[name] != nil && !seen[name] {
					seen[name] = true
					pending = append(pending, name)
				}
			}
			keys := make([]string, 0, len(typed))
			for key := range typed {
				if skipDefinitions && key == "$defs" {
					continue
				}
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				collect(typed[key], skipDefinitions)
			}
		case []any:
			for _, item := range typed {
				collect(item, skipDefinitions)
			}
		}
	}
	collect(schema, true)
	for len(pending) > 0 {
		name := pending[0]
		pending = pending[1:]
		collect(definitions[name], true)
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func definitionNameFromReference(reference string) (string, bool) {
	const prefix = "#/$defs/"
	if !strings.HasPrefix(reference, prefix) {
		return "", false
	}
	raw := strings.TrimPrefix(reference, prefix)
	if index := strings.IndexByte(raw, '/'); index >= 0 {
		raw = raw[:index]
	}
	if raw == "" {
		return "", false
	}
	return strings.ReplaceAll(strings.ReplaceAll(raw, "~1", "/"), "~0", "~"), true
}

func projectShape(schema, root map[string]any, coordinate, extensionDigest, pointer string, active map[string]bool) (Shape, error) {
	provenance := Provenance{Owner: strings.Split(coordinate, "#schema:")[0], ExtensionSHA256: extensionDigest, Coordinate: coordinate, JSONPointer: pointer}
	if err := rejectUnsupportedStructuralKeywords(schema, coordinate, pointer); err != nil {
		return Shape{}, err
	}
	if reference, ok := schema["$ref"].(string); ok {
		for _, keyword := range []string{"type", "properties", "items", "additionalProperties", "allOf", "anyOf", "oneOf"} {
			if _, exists := schema[keyword]; exists {
				return Shape{}, diagnostic("schema", "RCB1314", coordinate, pointer+"/"+keyword, fmt.Sprintf("structure-changing sibling %q beside $ref is unsupported by binding-model v1alpha1", keyword))
			}
		}
		return Shape{Kind: "ref", Ref: reference, Provenance: provenance, Constraints: extractConstraints(schema)}, nil
	}
	base, err := projectDirectShape(schema, root, coordinate, extensionDigest, pointer, active)
	if err != nil {
		return Shape{}, err
	}
	for _, keyword := range []string{"allOf", "oneOf", "anyOf"} {
		branches, ok := schema[keyword].([]any)
		if !ok {
			continue
		}
		projected := make([]Shape, 0, len(branches))
		for i, branchValue := range branches {
			branch, ok := branchValue.(map[string]any)
			if !ok {
				return Shape{}, diagnostic("schema", "RCB1305", coordinate, fmt.Sprintf("%s/%s/%d", pointer, keyword, i), "schema branch must be an object")
			}
			shape, err := projectShape(branch, root, coordinate, extensionDigest, fmt.Sprintf("%s/%s/%d", pointer, keyword, i), active)
			if err != nil {
				return Shape{}, err
			}
			projected = append(projected, shape)
		}
		var combined Shape
		if keyword == "allOf" {
			combined, err = mergeConjunctiveShapes(projected, provenance)
		} else {
			combined, err = mergeAlternativeShapes(projected, provenance)
		}
		if err != nil {
			return Shape{}, diagnostic("schema", "RCB1306", coordinate, pointer+"/"+keyword, err.Error())
		}
		base, err = mergeBaseAndCombinator(base, combined, provenance)
		if err != nil {
			return Shape{}, diagnostic("schema", "RCB1307", coordinate, pointer+"/"+keyword, err.Error())
		}
	}
	base.Constraints = extractConstraints(schema)
	base.Provenance = provenance
	return base, nil
}

func projectDirectShape(schema, root map[string]any, coordinate, extensionDigest, pointer string, active map[string]bool) (Shape, error) {
	provenance := Provenance{Owner: strings.Split(coordinate, "#schema:")[0], ExtensionSHA256: extensionDigest, Coordinate: coordinate, JSONPointer: pointer}
	types := schemaTypes(schema)
	if len(types) > 1 {
		variants := make([]Shape, 0, len(types))
		for _, schemaType := range types {
			variantSchema := cloneMap(schema)
			variantSchema["type"] = schemaType
			variant, err := projectDirectShape(variantSchema, root, coordinate, extensionDigest, pointer, active)
			if err != nil {
				return Shape{}, err
			}
			variants = append(variants, variant)
		}
		return Shape{Kind: "union", Variants: variants, Provenance: provenance}, nil
	}
	schemaType := ""
	if len(types) == 1 {
		schemaType = types[0]
	} else {
		switch {
		case schema["properties"] != nil || schema["required"] != nil:
			schemaType = "object"
		case schema["items"] != nil || schema["contains"] != nil:
			schemaType = "array"
		case schema["enum"] != nil:
			schemaType = scalarTypeForValues(schema["enum"].([]any))
		case schema["const"] != nil:
			schemaType = scalarType(schema["const"])
		}
	}
	switch schemaType {
	case "object":
		properties, _ := schema["properties"].(map[string]any)
		additional, additionalIsSchema := schema["additionalProperties"].(map[string]any)
		if len(properties) == 0 && additionalIsSchema {
			valueShape, err := projectShape(additional, root, coordinate, extensionDigest, pointer+"/additionalProperties", active)
			if err != nil {
				return Shape{}, err
			}
			return Shape{Kind: "map", MapValues: &valueShape, Provenance: provenance}, nil
		}
		requiredSet := stringSet(schema["required"])
		names := make([]string, 0, len(properties))
		for name := range properties {
			names = append(names, name)
		}
		sort.Strings(names)
		shape := Shape{Kind: "object", Required: sortedSet(requiredSet), Provenance: provenance}
		for _, name := range names {
			propertySchema, ok := properties[name].(map[string]any)
			if !ok {
				return Shape{}, diagnostic("schema", "RCB1308", coordinate, pointer+"/properties/"+escapeJSONPointer(name), "property schema must be an object")
			}
			propertyPointer := pointer + "/properties/" + escapeJSONPointer(name)
			propertyShape, err := projectShape(propertySchema, root, coordinate, extensionDigest, propertyPointer, active)
			if err != nil {
				return Shape{}, err
			}
			shape.Properties = append(shape.Properties, PropertyShape{
				Name: name, Tokens: tokenize(name), Required: requiredSet[name], Shape: propertyShape,
				Provenance: Provenance{Owner: provenance.Owner, ExtensionSHA256: extensionDigest, Coordinate: coordinate, JSONPointer: propertyPointer},
			})
		}
		if additionalIsSchema {
			valueShape, err := projectShape(additional, root, coordinate, extensionDigest, pointer+"/additionalProperties", active)
			if err != nil {
				return Shape{}, err
			}
			shape.MapValues = &valueShape
		}
		return shape, nil
	case "array":
		itemSchema, ok := schema["items"].(map[string]any)
		itemPointer := pointer + "/items"
		if !ok {
			item := Shape{Kind: "any", Provenance: provenance}
			return Shape{Kind: "array", Items: &item, Provenance: provenance}, nil
		}
		item, err := projectShape(itemSchema, root, coordinate, extensionDigest, itemPointer, active)
		if err != nil {
			return Shape{}, err
		}
		return Shape{Kind: "array", Items: &item, Provenance: provenance}, nil
	case "string", "boolean", "integer", "number", "null":
		shape := Shape{Kind: "scalar", Scalar: schemaType, Provenance: provenance}
		if values, ok := schema["enum"].([]any); ok {
			shape.Values = append([]any(nil), values...)
			_ = sortAny(shape.Values)
		} else if value, exists := schema["const"]; exists {
			shape.Values = []any{value}
		}
		return shape, nil
	default:
		return Shape{Kind: "any", Provenance: provenance}, nil
	}
}

func rejectUnsupportedStructuralKeywords(schema map[string]any, coordinate, pointer string) error {
	for _, keyword := range []string{
		"$dynamicRef",
		"dependentSchemas",
		"patternProperties",
		"prefixItems",
		"unevaluatedProperties",
	} {
		if _, exists := schema[keyword]; exists {
			return diagnostic("schema", "RCB1309", coordinate, pointer+"/"+keyword, fmt.Sprintf("structure-changing keyword %q is unsupported by binding-model v1alpha1", keyword))
		}
	}
	return nil
}

func mergeAlternativeShapes(shapes []Shape, provenance Provenance) (Shape, error) {
	if len(shapes) == 0 {
		return Shape{Kind: "any", Provenance: provenance}, nil
	}
	allObjects := true
	for _, shape := range shapes {
		if shape.Kind != "object" {
			allObjects = false
			break
		}
	}
	if !allObjects {
		return Shape{Kind: "union", Variants: deduplicateShapes(shapes), Provenance: provenance}, nil
	}
	properties := map[string][]PropertyShape{}
	required := stringSetFromSlice(shapes[0].Required)
	for _, shape := range shapes {
		required = intersectSets(required, stringSetFromSlice(shape.Required))
		for _, property := range shape.Properties {
			properties[property.Name] = append(properties[property.Name], property)
		}
	}
	result := Shape{Kind: "object", Required: sortedSet(required), Provenance: provenance}
	for _, name := range sortedPropertyNames(properties) {
		candidates := properties[name]
		property := candidates[0]
		if len(candidates) > 1 {
			variants := make([]Shape, 0, len(candidates))
			for _, candidate := range candidates {
				variants = append(variants, candidate.Shape)
			}
			property.Shape = collapseEquivalentShapes(variants, provenance)
		}
		property.Required = required[name]
		result.Properties = append(result.Properties, property)
	}
	return result, nil
}

func mergeConjunctiveShapes(shapes []Shape, provenance Provenance) (Shape, error) {
	if len(shapes) == 0 {
		return Shape{Kind: "any", Provenance: provenance}, nil
	}
	result := shapes[0]
	for _, next := range shapes[1:] {
		var err error
		result, err = mergeBaseAndCombinator(result, next, provenance)
		if err != nil {
			return Shape{}, err
		}
	}
	return result, nil
}

func mergeBaseAndCombinator(base, combined Shape, provenance Provenance) (Shape, error) {
	if base.Kind == "any" {
		combined.Provenance = provenance
		return combined, nil
	}
	if combined.Kind == "any" {
		return base, nil
	}
	if base.Kind == "object" && combined.Kind == "object" {
		properties := map[string][]PropertyShape{}
		for _, property := range base.Properties {
			properties[property.Name] = append(properties[property.Name], property)
		}
		for _, property := range combined.Properties {
			properties[property.Name] = append(properties[property.Name], property)
		}
		required := stringSetFromSlice(base.Required)
		for name := range stringSetFromSlice(combined.Required) {
			required[name] = true
		}
		result := Shape{Kind: "object", Required: sortedSet(required), Provenance: provenance}
		for _, name := range sortedPropertyNames(properties) {
			candidates := properties[name]
			property := candidates[0]
			if len(candidates) > 1 {
				merged := property.Shape
				for _, candidate := range candidates[1:] {
					var err error
					merged, err = mergeBaseAndCombinator(merged, candidate.Shape, merged.Provenance)
					if err != nil {
						return Shape{}, fmt.Errorf("property %q has %w", name, err)
					}
				}
				property.Shape = merged
			}
			property.Required = required[name]
			result.Properties = append(result.Properties, property)
		}
		if base.MapValues != nil && combined.MapValues != nil {
			mapValues, err := mergeBaseAndCombinator(*base.MapValues, *combined.MapValues, base.MapValues.Provenance)
			if err != nil {
				return Shape{}, fmt.Errorf("additional properties have %w", err)
			}
			result.MapValues = &mapValues
		} else if base.MapValues != nil {
			result.MapValues = base.MapValues
		} else if combined.MapValues != nil {
			result.MapValues = combined.MapValues
		}
		return result, nil
	}
	if shapesEqual(base, combined) {
		return base, nil
	}
	if base.Kind == "scalar" && combined.Kind == "scalar" && base.Scalar == combined.Scalar {
		result := base
		switch {
		case len(base.Values) == 0:
			result.Values = append([]any(nil), combined.Values...)
		case len(combined.Values) == 0:
			result.Values = append([]any(nil), base.Values...)
		default:
			result.Values = intersectValues(base.Values, combined.Values)
			if len(result.Values) == 0 {
				return Shape{}, fmt.Errorf("scalar value domains have an empty intersection")
			}
		}
		return result, nil
	}
	if base.Kind == "array" && combined.Kind == "array" && base.Items != nil && combined.Items != nil {
		items, err := mergeBaseAndCombinator(*base.Items, *combined.Items, base.Items.Provenance)
		if err != nil {
			return Shape{}, fmt.Errorf("array items have %w", err)
		}
		base.Items = &items
		return base, nil
	}
	if base.Kind == "map" && combined.Kind == "map" && base.MapValues != nil && combined.MapValues != nil {
		values, err := mergeBaseAndCombinator(*base.MapValues, *combined.MapValues, base.MapValues.Provenance)
		if err != nil {
			return Shape{}, fmt.Errorf("map values have %w", err)
		}
		base.MapValues = &values
		return base, nil
	}
	return Shape{}, fmt.Errorf("incompatible structural types %q and %q", base.Kind, combined.Kind)
}

func intersectValues(left, right []any) []any {
	rightValues := map[string]bool{}
	for _, value := range right {
		encoded, _ := canonicalJSON(value)
		rightValues[string(encoded)] = true
	}
	var result []any
	for _, value := range left {
		encoded, _ := canonicalJSON(value)
		if rightValues[string(encoded)] {
			result = append(result, value)
		}
	}
	_ = sortAny(result)
	return result
}

func collapseEquivalentShapes(shapes []Shape, provenance Provenance) Shape {
	deduplicated := deduplicateShapes(shapes)
	if len(deduplicated) == 1 {
		return deduplicated[0]
	}
	return Shape{Kind: "union", Variants: deduplicated, Provenance: provenance}
}

func deduplicateShapes(shapes []Shape) []Shape {
	byKey := map[string]Shape{}
	for _, shape := range shapes {
		key := shapeStructuralKey(shape)
		byKey[key] = shape
	}
	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]Shape, 0, len(keys))
	for _, key := range keys {
		result = append(result, byKey[key])
	}
	return result
}

func shapeStructuralKey(shape Shape) string {
	copyShape := shape
	copyShape.Provenance = Provenance{}
	copyShape.Constraints = nil
	encoded, _ := canonicalJSON(copyShape)
	return string(encoded)
}

func shapesEqual(left, right Shape) bool {
	return shapeStructuralKey(left) == shapeStructuralKey(right)
}

func extractConstraints(schema map[string]any) map[string]any {
	projectedOrAnnotative := map[string]bool{
		"$schema": true, "$id": true, "$anchor": true, "$ref": true, "$defs": true,
		"type": true, "properties": true, "items": true, "additionalProperties": true,
		"allOf": true, "anyOf": true, "oneOf": true, "enum": true, "const": true,
		"required": true,
		"$comment": true, "title": true, "description": true, "default": true,
		"deprecated": true, "readOnly": true, "writeOnly": true, "examples": true,
	}
	constraints := map[string]any{}
	for key, value := range schema {
		if !projectedOrAnnotative[key] {
			constraints[key] = value
		}
	}
	if len(constraints) == 0 {
		return nil
	}
	return constraints
}

func normalizeScopes(documents []ResolvedDocument, schemas []NormalizedSchema) ([]ScopeModel, error) {
	scopeMap := map[string]ScopeModel{}
	for _, document := range documents {
		for _, interfaceType := range document.Definition.Spec.InterfaceTypes {
			coordinate := interfaceType.TargetKind + ":" + interfaceType.Name
			scopeMap[coordinate] = ScopeModel{Coordinate: coordinate, Kind: interfaceType.TargetKind, InterfaceType: interfaceType.Name}
		}
		for _, kind := range document.Definition.Spec.Kinds {
			found := false
			for _, scope := range scopeMap {
				if scope.Kind == kind.Name {
					found = true
				}
			}
			if !found {
				scopeMap[kind.Name+":"] = ScopeModel{Coordinate: kind.Name + ":", Kind: kind.Name}
			}
		}
	}
	sortedSchemas := append([]NormalizedSchema(nil), schemas...)
	sort.Slice(sortedSchemas, func(i, j int) bool { return sortedSchemas[i].Coordinate < sortedSchemas[j].Coordinate })
	for coordinate, scope := range scopeMap {
		var projections []Shape
		for _, schema := range sortedSchemas {
			if schema.Kind != "" && schema.Kind != scope.Kind {
				continue
			}
			if schema.InterfaceType != "" && schema.InterfaceType != scope.InterfaceType {
				continue
			}
			scope.ApplicableSchemas = append(scope.ApplicableSchemas, schema.Coordinate)
			expanded := expandShapeForMerge(schema.Projection, schema, map[string]bool{})
			projections = append(projections, removeFixedScopeMetadata(expanded, scope.Kind, scope.InterfaceType))
		}
		sort.Strings(scope.ApplicableSchemas)
		if len(projections) > 0 {
			projection, err := mergeConjunctiveShapes(projections, projections[0].Provenance)
			if err != nil {
				return nil, diagnostic("schema", "RCB1312", "scope:"+coordinate, "", "applicable schemas have "+err.Error())
			}
			scope.Projection = &projection
		}
		scopeMap[coordinate] = scope
	}
	coordinates := make([]string, 0, len(scopeMap))
	for coordinate := range scopeMap {
		coordinates = append(coordinates, coordinate)
	}
	sort.Strings(coordinates)
	result := make([]ScopeModel, 0, len(coordinates))
	for _, coordinate := range coordinates {
		result = append(result, scopeMap[coordinate])
	}
	return result, nil
}

func expandShapeForMerge(shape Shape, schema NormalizedSchema, active map[string]bool) Shape {
	if shape.Kind == "ref" {
		name, ok := definitionNameFromReference(shape.Ref)
		if !ok || active[shape.Ref] {
			return shape
		}
		for _, definition := range schema.Definitions {
			if definition.Name != name {
				continue
			}
			activeCopy := cloneBoolMap(active)
			activeCopy[shape.Ref] = true
			return expandShapeForMerge(definition.Shape, schema, activeCopy)
		}
		return shape
	}
	result := shape
	for index := range result.Properties {
		result.Properties[index].Shape = expandShapeForMerge(result.Properties[index].Shape, schema, cloneBoolMap(active))
	}
	if result.Items != nil {
		items := expandShapeForMerge(*result.Items, schema, cloneBoolMap(active))
		result.Items = &items
	}
	if result.MapValues != nil {
		values := expandShapeForMerge(*result.MapValues, schema, cloneBoolMap(active))
		result.MapValues = &values
	}
	for index := range result.Variants {
		result.Variants[index] = expandShapeForMerge(result.Variants[index], schema, cloneBoolMap(active))
	}
	return result
}

func validateFieldValueDomains(model BindingModel) error {
	schemaByCoordinate := map[string]NormalizedSchema{}
	for _, schema := range model.Schemas {
		schemaByCoordinate[schema.Coordinate] = schema
	}
	for _, domain := range model.Vocabulary.ValueDomains {
		found := false
		for _, scope := range model.Scopes {
			if scope.Kind != domain.Kind || (domain.InterfaceType != "" && scope.InterfaceType != domain.InterfaceType) {
				continue
			}
			for _, schemaCoordinate := range scope.ApplicableSchemas {
				schema := schemaByCoordinate[schemaCoordinate]
				for _, value := range domain.Values {
					pathFound, accepted := fieldValueAcceptedAtPath(schema.Exact, domain.Segments, schema.Exact, "", value, map[string]bool{})
					if !pathFound {
						continue
					}
					found = true
					if !accepted {
						return diagnostic("schema", "RCB1310", domain.Coordinate, "", fmt.Sprintf("value %v is rejected by applicable schema %s", value, schemaCoordinate))
					}
				}
			}
		}
		if !found {
			return diagnostic("schema", "RCB1311", domain.Coordinate, "", "fieldValues path does not resolve in any applicable schema")
		}
	}
	return nil
}

func fieldValueAcceptedAtPath(schema map[string]any, segments []PathSegment, root map[string]any, pointer string, value any, active map[string]bool) (bool, bool) {
	if reference, ok := schema["$ref"].(string); ok {
		if active[reference] {
			return false, false
		}
		resolved, resolvedPointer, ok := resolveLocalReferenceWithPointer(root, reference)
		if !ok {
			return false, false
		}
		activeCopy := cloneBoolMap(active)
		activeCopy[reference] = true
		return fieldValueAcceptedAtPath(resolved, segments, root, resolvedPointer, value, activeCopy)
	}
	directFound, directAccepted := false, true
	if len(segments) == 0 {
		return true, validateExtensionSchemaFragment(root, pointer, value) == nil
	} else if properties, ok := schema["properties"].(map[string]any); ok {
		if property, ok := properties[segments[0].Name].(map[string]any); ok {
			propertyPointer := pointer + "/properties/" + escapeJSONPointer(segments[0].Name)
			if segments[0].Array {
				property, propertyPointer = dereferenceSchemaWithPointer(property, root, propertyPointer, map[string]bool{})
				if item, ok := property["items"].(map[string]any); ok {
					property = item
					propertyPointer += "/items"
				} else if item, ok := property["contains"].(map[string]any); ok {
					property = item
					propertyPointer += "/contains"
				} else {
					property = nil
				}
			}
			if property != nil {
				directFound, directAccepted = fieldValueAcceptedAtPath(property, segments[1:], root, propertyPointer, value, active)
			}
		}
	}
	for _, keyword := range []string{"allOf", "oneOf", "anyOf"} {
		branches, ok := schema[keyword].([]any)
		if !ok {
			continue
		}
		branchFound := false
		branchAccepted := keyword == "allOf"
		for index, branchValue := range branches {
			branch, ok := branchValue.(map[string]any)
			if !ok {
				continue
			}
			branchPointer := fmt.Sprintf("%s/%s/%d", pointer, keyword, index)
			found, accepted := fieldValueAcceptedAtPath(branch, segments, root, branchPointer, value, active)
			if !found {
				continue
			}
			branchFound = true
			if keyword == "allOf" {
				branchAccepted = branchAccepted && accepted
			} else {
				branchAccepted = branchAccepted || accepted
			}
		}
		if branchFound {
			if directFound {
				directAccepted = directAccepted && branchAccepted
			} else {
				directFound, directAccepted = true, branchAccepted
			}
		}
	}
	return directFound, directAccepted
}

func findPathSchemas(schema map[string]any, segments []PathSegment, root map[string]any, active map[string]bool) []map[string]any {
	if reference, ok := schema["$ref"].(string); ok {
		if active[reference] {
			return nil
		}
		resolved, ok := resolveLocalReference(root, reference)
		if !ok {
			return nil
		}
		activeCopy := cloneBoolMap(active)
		activeCopy[reference] = true
		return findPathSchemas(resolved, segments, root, activeCopy)
	}
	var candidates []map[string]any
	for _, keyword := range []string{"allOf", "oneOf", "anyOf"} {
		if branches, ok := schema[keyword].([]any); ok {
			for _, branchValue := range branches {
				if branch, ok := branchValue.(map[string]any); ok {
					candidates = append(candidates, findPathSchemas(branch, segments, root, active)...)
				}
			}
		}
	}
	if len(segments) == 0 {
		return append(candidates, schema)
	}
	properties, _ := schema["properties"].(map[string]any)
	property, ok := properties[segments[0].Name].(map[string]any)
	if !ok {
		return candidates
	}
	if segments[0].Array {
		property = dereferenceSchema(property, root, map[string]bool{})
		item, ok := property["items"].(map[string]any)
		if !ok {
			item, ok = property["contains"].(map[string]any)
		}
		if !ok {
			return candidates
		}
		property = item
	}
	return append(candidates, findPathSchemas(property, segments[1:], root, active)...)
}

func dereferenceSchema(schema, root map[string]any, active map[string]bool) map[string]any {
	resolved, _ := dereferenceSchemaWithPointer(schema, root, "", active)
	return resolved
}

func dereferenceSchemaWithPointer(schema, root map[string]any, pointer string, active map[string]bool) (map[string]any, string) {
	reference, ok := schema["$ref"].(string)
	if !ok || active[reference] {
		return schema, pointer
	}
	resolved, resolvedPointer, ok := resolveLocalReferenceWithPointer(root, reference)
	if !ok {
		return schema, pointer
	}
	active[reference] = true
	return dereferenceSchemaWithPointer(resolved, root, resolvedPointer, active)
}

func resolveLocalReference(root map[string]any, reference string) (map[string]any, bool) {
	resolved, _, ok := resolveLocalReferenceWithPointer(root, reference)
	return resolved, ok
}

func resolveLocalReferenceWithPointer(root map[string]any, reference string) (map[string]any, string, bool) {
	if reference == "#" {
		return root, "", true
	}
	if !strings.HasPrefix(reference, "#/") {
		return nil, "", false
	}
	var value any = root
	for _, raw := range strings.Split(strings.TrimPrefix(reference, "#/"), "/") {
		segment := strings.ReplaceAll(strings.ReplaceAll(raw, "~1", "/"), "~0", "~")
		mapping, ok := value.(map[string]any)
		if !ok {
			return nil, "", false
		}
		value, ok = mapping[segment]
		if !ok {
			return nil, "", false
		}
	}
	result, ok := value.(map[string]any)
	return result, strings.TrimPrefix(reference, "#"), ok
}

func schemaTypes(schema map[string]any) []string {
	switch value := schema["type"].(type) {
	case string:
		return []string{value}
	case []any:
		var result []string
		for _, item := range value {
			if text, ok := item.(string); ok {
				result = append(result, text)
			}
		}
		sort.Strings(result)
		return result
	default:
		return nil
	}
}

func scalarTypeForValues(values []any) string {
	result := ""
	for _, value := range values {
		current := scalarType(value)
		if result == "" {
			result = current
		} else if result != current {
			return ""
		}
	}
	return result
}

func scalarType(value any) string {
	switch value.(type) {
	case string:
		return "string"
	case bool:
		return "boolean"
	case int, int64, uint64:
		return "integer"
	case float64:
		return "number"
	case nil:
		return "null"
	default:
		return ""
	}
}

func parsePath(path string) ([]PathSegment, error) {
	if path == "" {
		return nil, fmt.Errorf("field path is empty")
	}
	parts := strings.Split(path, ".")
	segments := make([]PathSegment, 0, len(parts))
	for _, part := range parts {
		array := strings.HasSuffix(part, "[]")
		name := strings.TrimSuffix(part, "[]")
		if name == "" || strings.ContainsAny(name, "[]") {
			return nil, fmt.Errorf("invalid field path segment %q", part)
		}
		segments = append(segments, PathSegment{Name: name, Array: array, Tokens: tokenize(name)})
	}
	return segments, nil
}

func tokenize(value string) []string {
	var raw []string
	for i := 0; i < len(value); {
		if value[i] >= utf8.RuneSelf {
			start := i
			for i < len(value) && value[i] >= utf8.RuneSelf {
				_, size := utf8.DecodeRuneInString(value[i:])
				i += size
			}
			var builder strings.Builder
			builder.WriteByte('u')
			for _, valueByte := range []byte(value[start:i]) {
				builder.WriteString(fmt.Sprintf("%02X", valueByte))
			}
			raw = append(raw, builder.String())
			continue
		}
		if !isASCIIAlphaNumeric(value[i]) {
			i++
			continue
		}
		start := i
		i++
		for i < len(value) && value[i] < utf8.RuneSelf && isASCIIAlphaNumeric(value[i]) {
			previous := value[i-1]
			current := value[i]
			var next byte
			if i+1 < len(value) {
				next = value[i+1]
			}
			if tokenBoundary(previous, current, next) {
				raw = append(raw, strings.ToLower(value[start:i]))
				start = i
			}
			i++
		}
		raw = append(raw, strings.ToLower(value[start:i]))
	}
	if len(raw) == 0 {
		return []string{"x"}
	}
	return raw
}

func tokenBoundary(previous, current, next byte) bool {
	if isASCIIDigit(previous) != isASCIIDigit(current) {
		return true
	}
	if isASCIILower(previous) && isASCIIUpper(current) {
		return true
	}
	return isASCIIUpper(previous) && isASCIIUpper(current) && isASCIILower(next)
}

func isASCIIAlphaNumeric(value byte) bool {
	return isASCIILower(value) || isASCIIUpper(value) || isASCIIDigit(value)
}

func isASCIILower(value byte) bool { return value >= 'a' && value <= 'z' }
func isASCIIUpper(value byte) bool { return value >= 'A' && value <= 'Z' }
func isASCIIDigit(value byte) bool { return value >= '0' && value <= '9' }

func sortModel(model *BindingModel) {
	sort.Slice(model.DependencyEdges, func(i, j int) bool {
		if model.DependencyEdges[i].From != model.DependencyEdges[j].From {
			return model.DependencyEdges[i].From < model.DependencyEdges[j].From
		}
		return model.DependencyEdges[i].To < model.DependencyEdges[j].To
	})
	sort.Slice(model.Vocabulary.Owners, func(i, j int) bool {
		return model.Vocabulary.Owners[i].Coordinate < model.Vocabulary.Owners[j].Coordinate
	})
	sort.Slice(model.Vocabulary.OwnedDeclarations, func(i, j int) bool {
		return model.Vocabulary.OwnedDeclarations[i].Coordinate < model.Vocabulary.OwnedDeclarations[j].Coordinate
	})
	sort.Slice(model.Vocabulary.Interfaces, func(i, j int) bool {
		return model.Vocabulary.Interfaces[i].Coordinate < model.Vocabulary.Interfaces[j].Coordinate
	})
	sort.Slice(model.Vocabulary.ConditionFields, func(i, j int) bool {
		return model.Vocabulary.ConditionFields[i].Coordinate < model.Vocabulary.ConditionFields[j].Coordinate
	})
	sort.Slice(model.Vocabulary.InterfaceFields, func(i, j int) bool {
		return model.Vocabulary.InterfaceFields[i].Coordinate < model.Vocabulary.InterfaceFields[j].Coordinate
	})
	sort.Slice(model.Vocabulary.ValueDomains, func(i, j int) bool {
		return model.Vocabulary.ValueDomains[i].Coordinate < model.Vocabulary.ValueDomains[j].Coordinate
	})
	sort.Slice(model.Scopes, func(i, j int) bool { return model.Scopes[i].Coordinate < model.Scopes[j].Coordinate })
	sort.Slice(model.Schemas, func(i, j int) bool { return model.Schemas[i].Coordinate < model.Schemas[j].Coordinate })
}

func sortedStrings(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	return result
}

func stringSet(value any) map[string]bool {
	items, _ := value.([]any)
	result := map[string]bool{}
	for _, item := range items {
		if text, ok := item.(string); ok {
			result[text] = true
		}
	}
	return result
}

func stringSetFromSlice(values []string) map[string]bool {
	result := map[string]bool{}
	for _, value := range values {
		result[value] = true
	}
	return result
}

func intersectSets(left, right map[string]bool) map[string]bool {
	result := map[string]bool{}
	for value := range left {
		if right[value] {
			result[value] = true
		}
	}
	return result
}

func sortedSet(values map[string]bool) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func sortedPropertyNames(values map[string][]PropertyShape) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func cloneBoolMap(source map[string]bool) map[string]bool {
	result := make(map[string]bool, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
