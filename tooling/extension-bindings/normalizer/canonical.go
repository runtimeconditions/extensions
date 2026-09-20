package normalizer

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

const (
	maxYAMLBytes   = 64 << 20
	maxYAMLNodes   = 1_000_000
	maxYAMLDepth   = 256
	maxYAMLAliases = 100
)

// ParseYAMLData parses one YAML document into JSON-compatible data while
// enforcing the input limits and duplicate-key rules in the implementation
// standard.
func ParseYAMLData(data []byte) (map[string]any, error) {
	if len(data) > maxYAMLBytes {
		return nil, diagnostic("structural", "RCB1001", "", "", "YAML input exceeds 64 MiB")
	}
	var document yaml.Node
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&document); err != nil {
		return nil, diagnostic("structural", "RCB1002", "", "", fmt.Sprintf("invalid YAML: %v", err))
	}
	var trailing yaml.Node
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err != nil {
			return nil, diagnostic("structural", "RCB1002", "", "", fmt.Sprintf("invalid YAML: %v", err))
		}
		return nil, diagnostic("structural", "RCB1003", "", "", "YAML input must contain exactly one document")
	}
	if len(document.Content) != 1 {
		return nil, diagnostic("structural", "RCB1003", "", "", "YAML input must contain exactly one document")
	}
	counts := yamlCounts{}
	if err := inspectYAMLNode(document.Content[0], 1, &counts); err != nil {
		return nil, err
	}
	value, err := yamlNodeValue(document.Content[0], &counts)
	if err != nil {
		return nil, err
	}
	mapping, ok := value.(map[string]any)
	if !ok {
		return nil, diagnostic("structural", "RCB1004", "", "", "YAML document root must be a mapping")
	}
	return mapping, nil
}

type yamlCounts struct {
	nodes   int
	aliases int
}

func inspectYAMLNode(node *yaml.Node, depth int, counts *yamlCounts) error {
	counts.nodes++
	if counts.nodes > maxYAMLNodes {
		return diagnostic("structural", "RCB1005", "", "", "YAML input exceeds 1,000,000 decoded nodes")
	}
	if depth > maxYAMLDepth {
		return diagnostic("structural", "RCB1006", "", "", "YAML input exceeds 256 levels of nesting")
	}
	if node.Kind == yaml.AliasNode {
		counts.aliases++
		if counts.aliases > maxYAMLAliases {
			return diagnostic("structural", "RCB1007", "", "", "YAML input exceeds 100 alias references")
		}
	}
	if node.Kind == yaml.MappingNode {
		seen := map[string]struct{}{}
		for i := 0; i < len(node.Content); i += 2 {
			key := node.Content[i]
			if key.Kind != yaml.ScalarNode || key.Tag != "!!str" {
				return diagnostic("structural", "RCB1008", "", "", "YAML mapping keys must be strings")
			}
			if _, exists := seen[key.Value]; exists {
				return diagnostic("structural", "RCB1009", "", "", fmt.Sprintf("duplicate YAML mapping key %q", key.Value))
			}
			seen[key.Value] = struct{}{}
		}
	}
	for _, child := range node.Content {
		if err := inspectYAMLNode(child, depth+1, counts); err != nil {
			return err
		}
	}
	return nil
}

func yamlNodeValue(node *yaml.Node, counts *yamlCounts) (any, error) {
	if node.Kind == yaml.AliasNode {
		if node.Alias == nil {
			return nil, diagnostic("structural", "RCB1010", "", "", "YAML alias has no target")
		}
		return yamlNodeValue(node.Alias, counts)
	}
	switch node.Kind {
	case yaml.MappingNode:
		result := make(map[string]any, len(node.Content)/2)
		for i := 0; i < len(node.Content); i += 2 {
			value, err := yamlNodeValue(node.Content[i+1], counts)
			if err != nil {
				return nil, err
			}
			result[node.Content[i].Value] = value
		}
		return result, nil
	case yaml.SequenceNode:
		result := make([]any, len(node.Content))
		for i, child := range node.Content {
			value, err := yamlNodeValue(child, counts)
			if err != nil {
				return nil, err
			}
			result[i] = value
		}
		return result, nil
	case yaml.ScalarNode:
		switch node.Tag {
		case "!!str":
			return node.Value, nil
		case "!!bool":
			return strconv.ParseBool(node.Value)
		case "!!null":
			return nil, nil
		case "!!int":
			value, err := strconv.ParseInt(node.Value, 0, 64)
			if err == nil {
				return value, nil
			}
			unsigned, unsignedErr := strconv.ParseUint(node.Value, 0, 64)
			if unsignedErr != nil {
				return nil, diagnostic("structural", "RCB1011", "", "", fmt.Sprintf("unsupported integer %q", node.Value))
			}
			return unsigned, nil
		case "!!float":
			value, err := strconv.ParseFloat(node.Value, 64)
			if err != nil || math.IsInf(value, 0) || math.IsNaN(value) {
				return nil, diagnostic("structural", "RCB1012", "", "", fmt.Sprintf("unsupported number %q", node.Value))
			}
			return value, nil
		default:
			return nil, diagnostic("structural", "RCB1013", "", "", fmt.Sprintf("custom YAML tag %q is not supported", node.Tag))
		}
	default:
		return nil, diagnostic("structural", "RCB1014", "", "", "unsupported YAML node")
	}
}

func DecodeExtension(data []byte) (ExtensionDefinition, map[string]any, error) {
	mapping, err := ParseYAMLData(data)
	if err != nil {
		return ExtensionDefinition{}, nil, err
	}
	canonical, err := json.Marshal(mapping)
	if err != nil {
		return ExtensionDefinition{}, nil, err
	}
	var definition ExtensionDefinition
	if err := json.Unmarshal(canonical, &definition); err != nil {
		return ExtensionDefinition{}, nil, diagnostic("structural", "RCB1015", "", "", fmt.Sprintf("extension fields have invalid types: %v", err))
	}
	if spec, ok := mapping["spec"].(map[string]any); ok {
		if fieldValues, ok := spec["fieldValues"].([]any); ok {
			for index, value := range fieldValues {
				if index >= len(definition.Spec.FieldValues) {
					break
				}
				entry, _ := value.(map[string]any)
				values, _ := entry["values"].([]any)
				definition.Spec.FieldValues[index].Values = append([]any(nil), values...)
			}
		}
		if schemas, ok := spec["schemas"].([]any); ok {
			for index, value := range schemas {
				if index >= len(definition.Spec.Schemas) {
					break
				}
				entry, _ := value.(map[string]any)
				schema, _ := entry["schema"].(map[string]any)
				definition.Spec.Schemas[index].Schema = schema
			}
		}
	}
	return definition, mapping, nil
}

func DecodeDependencyLock(data []byte) (DependencyLock, error) {
	mapping, err := ParseYAMLData(data)
	if err != nil {
		return DependencyLock{}, err
	}
	encoded, err := json.Marshal(mapping)
	if err != nil {
		return DependencyLock{}, err
	}
	var lock DependencyLock
	if err := json.Unmarshal(encoded, &lock); err != nil {
		return DependencyLock{}, diagnostic("structural", "RCB1016", "dependency-lock", "", fmt.Sprintf("dependency lock fields have invalid types: %v", err))
	}
	return lock, nil
}

func SHA256Hex(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func (s *Schemas) CanonicalizeExtension(data map[string]any) (map[string]any, string, error) {
	normalized, err := normalizeSemanticWithSchema(data, s.semanticData, s.semanticData)
	if err != nil {
		return nil, "", err
	}
	mapping, ok := normalized.(map[string]any)
	if !ok {
		return nil, "", fmt.Errorf("canonical extension data is not an object")
	}
	encoded, err := canonicalJSON(mapping)
	if err != nil {
		return nil, "", err
	}
	return mapping, SHA256Hex(encoded), nil
}

func normalizeSemanticWithSchema(value any, schema, root map[string]any) (any, error) {
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
				return nil, fmt.Errorf("semantic schema has no rule for mapping key %q", key)
			}
			normalized, err := normalizeSemanticWithSchema(child, childSchema, root)
			if err != nil {
				return nil, err
			}
			result[key] = normalized
		}
		return result, nil
	case []any:
		ordering, ok := selected["x-runtimeconditions-ordering"].(string)
		if !ok || (ordering != "set" && ordering != "source") {
			return nil, fmt.Errorf("semantic array schema has no valid ordering annotation")
		}
		itemSchema, _ := selected["items"].(map[string]any)
		if itemSchema == nil {
			return nil, fmt.Errorf("semantic array schema has no item schema")
		}
		result := make([]any, len(typed))
		for index, item := range typed {
			normalized, err := normalizeSemanticWithSchema(item, itemSchema, root)
			if err != nil {
				return nil, err
			}
			result[index] = normalized
		}
		if ordering == "set" {
			if err := sortAny(result); err != nil {
				return nil, err
			}
		}
		return result, nil
	default:
		return value, nil
	}
}

func selectSemanticSchema(value any, schema, root map[string]any) (map[string]any, error) {
	selected, err := dereferenceSemanticSchema(schema, root)
	if err != nil {
		return nil, err
	}
	branches, hasBranches := selected["oneOf"].([]any)
	if !hasBranches {
		return selected, nil
	}
	var matches []map[string]any
	for _, branchValue := range branches {
		branch, ok := branchValue.(map[string]any)
		if !ok {
			continue
		}
		resolved, err := dereferenceSemanticSchema(branch, root)
		if err != nil {
			return nil, err
		}
		if semanticValueMatchesSchema(value, resolved, root) {
			matches = append(matches, resolved)
		}
	}
	if len(matches) != 1 {
		return nil, fmt.Errorf("semantic schema selected %d oneOf branches", len(matches))
	}
	return matches[0], nil
}

func dereferenceSemanticSchema(schema, root map[string]any) (map[string]any, error) {
	seen := map[string]bool{}
	for {
		reference, ok := schema["$ref"].(string)
		if !ok {
			return schema, nil
		}
		if seen[reference] {
			return nil, fmt.Errorf("semantic schema reference cycle at %q", reference)
		}
		seen[reference] = true
		resolved, ok := resolveLocalReference(root, reference)
		if !ok {
			return nil, fmt.Errorf("semantic schema reference %q does not resolve", reference)
		}
		schema = resolved
	}
}

func semanticValueMatchesSchema(value any, schema, root map[string]any) bool {
	resolved, err := dereferenceSemanticSchema(schema, root)
	if err != nil {
		return false
	}
	if branches, ok := resolved["oneOf"].([]any); ok {
		matches := 0
		for _, branchValue := range branches {
			branch, ok := branchValue.(map[string]any)
			if ok && semanticValueMatchesSchema(value, branch, root) {
				matches++
			}
		}
		return matches == 1
	}
	if constant, exists := resolved["const"]; exists {
		left, _ := canonicalJSON(constant)
		right, _ := canonicalJSON(value)
		if !bytes.Equal(left, right) {
			return false
		}
	}
	types := schemaTypes(resolved)
	if len(types) == 0 {
		return true
	}
	actual := semanticValueType(value)
	for _, expected := range types {
		if expected == actual || (expected == "number" && actual == "integer") {
			return true
		}
	}
	return false
}

func semanticValueType(value any) string {
	switch value.(type) {
	case map[string]any:
		return "object"
	case []any:
		return "array"
	default:
		return scalarType(value)
	}
}

func canonicalJSON(value any) ([]byte, error) {
	var buffer bytes.Buffer
	if err := appendCanonicalJSON(&buffer, value); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func appendCanonicalJSON(buffer *bytes.Buffer, value any) error {
	if value == nil {
		buffer.WriteString("null")
		return nil
	}
	switch typed := value.(type) {
	case bool:
		buffer.WriteString(strconv.FormatBool(typed))
	case string:
		if err := appendCanonicalJSONString(buffer, typed); err != nil {
			return err
		}
	case int:
		buffer.WriteString(strconv.Itoa(typed))
	case int64:
		buffer.WriteString(strconv.FormatInt(typed, 10))
	case uint64:
		buffer.WriteString(strconv.FormatUint(typed, 10))
	case float64:
		if math.IsInf(typed, 0) || math.IsNaN(typed) {
			return fmt.Errorf("non-finite number is not JSON-compatible")
		}
		if typed == 0 {
			buffer.WriteByte('0')
			break
		}
		encoded, err := json.Marshal(typed)
		if err != nil {
			return err
		}
		buffer.Write(encoded)
	case json.Number:
		buffer.WriteString(string(typed))
	case []any:
		buffer.WriteByte('[')
		for i, item := range typed {
			if i > 0 {
				buffer.WriteByte(',')
			}
			if err := appendCanonicalJSON(buffer, item); err != nil {
				return err
			}
		}
		buffer.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(i, j int) bool { return canonicalKeyLess(keys[i], keys[j]) })
		buffer.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				buffer.WriteByte(',')
			}
			if err := appendCanonicalJSONString(buffer, key); err != nil {
				return err
			}
			buffer.WriteByte(':')
			if err := appendCanonicalJSON(buffer, typed[key]); err != nil {
				return err
			}
		}
		buffer.WriteByte('}')
	default:
		converted, err := genericJSONValue(value)
		if err != nil {
			return err
		}
		return appendCanonicalJSON(buffer, converted)
	}
	return nil
}

func canonicalKeyLess(left, right string) bool {
	leftUnits := utf16.Encode([]rune(left))
	rightUnits := utf16.Encode([]rune(right))
	limit := len(leftUnits)
	if len(rightUnits) < limit {
		limit = len(rightUnits)
	}
	for index := 0; index < limit; index++ {
		if leftUnits[index] != rightUnits[index] {
			return leftUnits[index] < rightUnits[index]
		}
	}
	return len(leftUnits) < len(rightUnits)
}

func appendCanonicalJSONString(buffer *bytes.Buffer, value string) error {
	if !utf8.ValidString(value) {
		return fmt.Errorf("string is not valid UTF-8")
	}
	buffer.WriteByte('"')
	for _, character := range value {
		switch character {
		case '"', '\\':
			buffer.WriteByte('\\')
			buffer.WriteRune(character)
		case '\b':
			buffer.WriteString(`\b`)
		case '\t':
			buffer.WriteString(`\t`)
		case '\n':
			buffer.WriteString(`\n`)
		case '\f':
			buffer.WriteString(`\f`)
		case '\r':
			buffer.WriteString(`\r`)
		default:
			if character < 0x20 {
				buffer.WriteString(fmt.Sprintf(`\u%04x`, character))
			} else {
				buffer.WriteRune(character)
			}
		}
	}
	buffer.WriteByte('"')
	return nil
}

func genericJSONValue(value any) (any, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var result any
	if err := decoder.Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}

func CanonicalModelYAML(model BindingModel) ([]byte, error) {
	data, err := yaml.Marshal(model)
	if err != nil {
		return nil, err
	}
	data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	data = bytes.TrimRight(data, "\n")
	return append(data, '\n'), nil
}

func CanonicalDependencyLockYAML(lock DependencyLock) ([]byte, error) {
	copyLock := lock
	copyLock.Extensions = append([]DependencyLockEntry(nil), lock.Extensions...)
	for index := range copyLock.Extensions {
		copyLock.Extensions[index].Dependencies = sortedStrings(copyLock.Extensions[index].Dependencies)
	}
	data, err := yaml.Marshal(copyLock)
	if err != nil {
		return nil, err
	}
	data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	data = bytes.TrimRight(data, "\n")
	return append(data, '\n'), nil
}

func ModelSemanticSHA256(model BindingModel) (string, error) {
	copyModel := model
	copyModel.Metadata.SemanticSHA256 = ""
	value, err := genericJSONValue(copyModel)
	if err != nil {
		return "", err
	}
	mapping, ok := value.(map[string]any)
	if !ok {
		return "", fmt.Errorf("binding model did not serialize as an object")
	}
	metadata := mapping["metadata"].(map[string]any)
	delete(metadata, "semanticSha256")
	encoded, err := canonicalJSON(mapping)
	if err != nil {
		return "", err
	}
	return SHA256Hex(encoded), nil
}

func sortAny(values []any) error {
	type sortableValue struct {
		value any
		key   []byte
	}
	sortable := make([]sortableValue, len(values))
	for index, value := range values {
		key, err := canonicalJSON(value)
		if err != nil {
			return err
		}
		sortable[index] = sortableValue{value: value, key: key}
	}
	sort.SliceStable(sortable, func(i, j int) bool {
		return bytes.Compare(sortable[i].key, sortable[j].key) < 0
	})
	for index := range sortable {
		values[index] = sortable[index].value
	}
	return nil
}

func diagnostic(category, code, coordinate, pointer, message string) error {
	return &DiagnosticError{Diagnostic: Diagnostic{
		Category: category, Code: code, Coordinate: coordinate, JSONPointer: pointer, Message: message,
	}}
}

func escapeJSONPointer(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "~", "~0"), "/", "~1")
}
