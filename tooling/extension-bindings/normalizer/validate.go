package normalizer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

type Schemas struct {
	semantic     *jsonschema.Schema
	semanticData map[string]any
	model        *jsonschema.Schema
}

func LoadSchemas(semanticPath, modelPath string) (*Schemas, error) {
	semantic, semanticData, err := compileYAMLSchema(semanticPath)
	if err != nil {
		return nil, fmt.Errorf("compile extension semantic schema: %w", err)
	}
	if err := validateOrderingAnnotations(semanticData, "#"); err != nil {
		return nil, err
	}
	model, modelData, err := compileYAMLSchema(modelPath)
	if err != nil {
		return nil, fmt.Errorf("compile binding model schema: %w", err)
	}
	if err := validateOrderingAnnotations(modelData, "#"); err != nil {
		return nil, err
	}
	return &Schemas{semantic: semantic, semanticData: semanticData, model: model}, nil
}

func compileYAMLSchema(path string) (*jsonschema.Schema, map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	mapping, err := ParseYAMLData(data)
	if err != nil {
		return nil, nil, err
	}
	encoded, err := json.Marshal(mapping)
	if err != nil {
		return nil, nil, err
	}
	resource, err := jsonschema.UnmarshalJSON(bytes.NewReader(encoded))
	if err != nil {
		return nil, nil, err
	}
	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()
	resourceURL := "urn:runtimeconditions:schema:" + SHA256Hex(encoded)
	if err := compiler.AddResource(resourceURL, resource); err != nil {
		return nil, nil, err
	}
	compiled, err := compiler.Compile(resourceURL)
	if err != nil {
		return nil, nil, err
	}
	return compiled, mapping, nil
}

func validateOrderingAnnotations(value any, pointer string) error {
	mapping, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	if schemaType, ok := mapping["type"].(string); ok && schemaType == "array" {
		ordering, ok := mapping["x-runtimeconditions-ordering"].(string)
		if !ok || (ordering != "set" && ordering != "source") {
			return diagnostic("model", "RCB1101", "semantic-schema", pointer, "array schema must declare x-runtimeconditions-ordering as set or source")
		}
	}
	keys := make([]string, 0, len(mapping))
	for key := range mapping {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		childPointer := pointer + "/" + escapeJSONPointer(key)
		switch child := mapping[key].(type) {
		case map[string]any:
			if err := validateOrderingAnnotations(child, childPointer); err != nil {
				return err
			}
		case []any:
			for i, item := range child {
				if err := validateOrderingAnnotations(item, fmt.Sprintf("%s/%d", childPointer, i)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (s *Schemas) ValidateExtension(data map[string]any, definition ExtensionDefinition) error {
	if err := s.semantic.Validate(data); err != nil {
		return diagnostic("structural", "RCB1102", definition.Metadata.ID, "", fmt.Sprintf("extension semantic schema validation failed: %v", err))
	}
	if definition.APIVersion != "runtimeconditions.io/v1alpha1" {
		return diagnostic("structural", "RCB1103", definition.Metadata.ID, "/apiVersion", "unsupported extension API version")
	}
	if definition.Kind != ExtensionKind {
		return diagnostic("structural", "RCB1104", definition.Metadata.ID, "/kind", "document is not a RuntimeConditionsExtensionDefinition")
	}
	parsedID, err := url.Parse(definition.Metadata.ID)
	if err != nil || !parsedID.IsAbs() || parsedID.Fragment != "" {
		return diagnostic("structural", "RCB1105", definition.Metadata.ID, "/metadata/id", "extension identifier must be an absolute URI without a fragment")
	}
	if parsedID.Scheme == "http" {
		return diagnostic("structural", "RCB1106", definition.Metadata.ID, "/metadata/id", "plain HTTP extension identifiers are not supported")
	}
	if err := validateUniqueExtensionEntries(definition); err != nil {
		return err
	}
	for i, schema := range definition.Spec.Schemas {
		pointer := fmt.Sprintf("/spec/schemas/%d/schema", i)
		if dialect, exists := schema.Schema["$schema"]; exists && dialect != "https://json-schema.org/draft/2020-12/schema" {
			return diagnostic("schema", "RCB1107", definition.Metadata.ID+"#schema:"+schema.ID, pointer+"/$schema", "schema dialect must be JSON Schema Draft 2020-12")
		}
		if err := validateLocalReferences(schema.Schema, definition.Metadata.ID+"#schema:"+schema.ID, pointer); err != nil {
			return err
		}
		if err := compileExtensionSchema(schema.Schema, definition.Metadata.ID, schema.ID); err != nil {
			return diagnostic("schema", "RCB1108", definition.Metadata.ID+"#schema:"+schema.ID, pointer, fmt.Sprintf("invalid JSON Schema Draft 2020-12 document: %v", err))
		}
	}
	return nil
}

func validateUniqueExtensionEntries(definition ExtensionDefinition) error {
	schemaIDs := map[string]struct{}{}
	for i, schema := range definition.Spec.Schemas {
		if _, exists := schemaIDs[schema.ID]; exists {
			return diagnostic("vocabulary-conflict", "RCB1109", definition.Metadata.ID, fmt.Sprintf("/spec/schemas/%d/id", i), fmt.Sprintf("duplicate schema id %q", schema.ID))
		}
		schemaIDs[schema.ID] = struct{}{}
	}
	seenDeps := map[string]struct{}{}
	for i, dependency := range definition.Spec.Dependencies {
		if _, exists := seenDeps[dependency]; exists {
			return diagnostic("extension-dependency", "RCB1110", definition.Metadata.ID, fmt.Sprintf("/spec/dependencies/%d", i), fmt.Sprintf("duplicate dependency %q", dependency))
		}
		seenDeps[dependency] = struct{}{}
	}
	for i, field := range definition.Spec.ConditionFields {
		if reservedConditionField(field.Name) {
			return diagnostic("vocabulary-conflict", "RCB1111", definition.Metadata.ID, fmt.Sprintf("/spec/conditionFields/%d/name", i), fmt.Sprintf("condition field %q is reserved by the core", field.Name))
		}
	}
	for i, field := range definition.Spec.InterfaceFields {
		if field.Name == "type" {
			return diagnostic("vocabulary-conflict", "RCB1112", definition.Metadata.ID, fmt.Sprintf("/spec/interfaceFields/%d/name", i), "interface field type is reserved by the core")
		}
	}
	return nil
}

func reservedConditionField(name string) bool {
	switch name {
	case "name", "optional", "kind", "interface":
		return true
	default:
		return false
	}
}

func validateLocalReferences(value any, coordinate, pointer string) error {
	switch typed := value.(type) {
	case map[string]any:
		for _, keyword := range []string{"$anchor", "$dynamicAnchor", "$recursiveAnchor"} {
			if _, exists := typed[keyword]; exists {
				return diagnostic("schema", "RCB1115", coordinate, pointer+"/"+escapeJSONPointer(keyword), fmt.Sprintf("schema anchor keyword %q is unsupported; use local JSON Pointer references", keyword))
			}
		}
		if reference, ok := typed["$ref"].(string); ok && !isLocalJSONPointerReference(reference) {
			return diagnostic("schema", "RCB1113", coordinate, pointer+"/$ref", fmt.Sprintf("schema reference %q is unsupported; use # or a local JSON Pointer beginning with #/", reference))
		}
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if err := validateLocalReferences(typed[key], coordinate, pointer+"/"+escapeJSONPointer(key)); err != nil {
				return err
			}
		}
	case []any:
		for i, item := range typed {
			if err := validateLocalReferences(item, coordinate, fmt.Sprintf("%s/%d", pointer, i)); err != nil {
				return err
			}
		}
	}
	return nil
}

func isLocalJSONPointerReference(reference string) bool {
	if reference == "#" {
		return true
	}
	if !strings.HasPrefix(reference, "#/") || strings.Contains(reference, "%") {
		return false
	}
	for index := 2; index < len(reference); index++ {
		if reference[index] != '~' {
			continue
		}
		if index+1 >= len(reference) || (reference[index+1] != '0' && reference[index+1] != '1') {
			return false
		}
		index++
	}
	return true
}

func compileExtensionSchema(schema map[string]any, owner, schemaID string) error {
	copySchema := cloneMap(schema)
	if _, exists := copySchema["$schema"]; !exists {
		copySchema["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	}
	encoded, err := json.Marshal(copySchema)
	if err != nil {
		return err
	}
	resource, err := jsonschema.UnmarshalJSON(bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	compiler := jsonschema.NewCompiler()
	resourceURL := "urn:runtimeconditions:extension-schema:" + SHA256Hex([]byte(owner+"\x00"+schemaID))
	if err := compiler.AddResource(resourceURL, resource); err != nil {
		return err
	}
	_, err = compiler.Compile(resourceURL)
	return err
}

func validateExtensionSchemaFragment(root map[string]any, pointer string, value any) error {
	copySchema := cloneMap(root)
	if _, exists := copySchema["$schema"]; !exists {
		copySchema["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	}
	encoded, err := json.Marshal(copySchema)
	if err != nil {
		return err
	}
	resource, err := jsonschema.UnmarshalJSON(bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	compiler := jsonschema.NewCompiler()
	resourceURL := "urn:runtimeconditions:extension-schema-fragment:" + SHA256Hex(encoded)
	if err := compiler.AddResource(resourceURL, resource); err != nil {
		return err
	}
	location := resourceURL
	if pointer != "" {
		location += "#" + (&url.URL{Fragment: pointer}).EscapedFragment()
	}
	compiled, err := compiler.Compile(location)
	if err != nil {
		return err
	}
	return compiled.Validate(value)
}

func (s *Schemas) ValidateModel(model BindingModel) error {
	value, err := genericJSONValue(model)
	if err != nil {
		return err
	}
	if err := s.model.Validate(value); err != nil {
		return diagnostic("model", "RCB1114", model.RootExtension.ID, "", fmt.Sprintf("binding model schema validation failed: %v", err))
	}
	return nil
}

func cloneMap(source map[string]any) map[string]any {
	result := make(map[string]any, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
