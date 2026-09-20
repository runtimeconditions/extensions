package goemitter

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/runtimeconditions/extensions/tooling/extension-bindings/normalizer"
	"gopkg.in/yaml.v3"
)

const (
	EmitterName             = "rc-go-bindings"
	EmitterVersion          = "0.1.0"
	PackageTargetAPIVersion = "runtimeconditions.io/go-package-target/v1alpha1"
	PackageTargetKind       = "RuntimeConditionsGoPackageTarget"
	ManifestAPIVersion      = "runtimeconditions.io/bindings/v1alpha1"
	ManifestKind            = "RuntimeConditionsBindingManifest"
)

type Diagnostic struct {
	Category   string `yaml:"category" json:"category"`
	Code       string `yaml:"code" json:"code"`
	Coordinate string `yaml:"coordinate,omitempty" json:"coordinate,omitempty"`
	Message    string `yaml:"message" json:"message"`
}

type DiagnosticError struct {
	Diagnostic Diagnostic
}

func (e *DiagnosticError) Error() string {
	return fmt.Sprintf("%s %s: %s", e.Diagnostic.Code, e.Diagnostic.Coordinate, e.Diagnostic.Message)
}

func diagnostic(category, code, coordinate, message string) error {
	return &DiagnosticError{Diagnostic: Diagnostic{
		Category: category, Code: code, Coordinate: coordinate, Message: message,
	}}
}

type PackageTarget struct {
	APIVersion       string              `yaml:"apiVersion" json:"apiVersion"`
	Kind             string              `yaml:"kind" json:"kind"`
	PackageKey       string              `yaml:"packageKey" json:"packageKey"`
	RootExtension    string              `yaml:"rootExtension" json:"rootExtension"`
	ModulePath       string              `yaml:"modulePath" json:"modulePath"`
	PackageName      string              `yaml:"packageName" json:"packageName"`
	Version          string              `yaml:"version" json:"version"`
	MinimumGoVersion string              `yaml:"minimumGoVersion" json:"minimumGoVersion"`
	PublicationMode  string              `yaml:"publicationMode" json:"publicationMode"`
	Dependencies     []PackageDependency `yaml:"dependencies,omitempty" json:"dependencies,omitempty"`
}

type PackageDependency struct {
	Extension   string `yaml:"extension" json:"extension"`
	ModulePath  string `yaml:"modulePath" json:"modulePath"`
	PackageName string `yaml:"packageName" json:"packageName"`
	Version     string `yaml:"version" json:"version"`
}

func LoadPackageTarget(path string) (PackageTarget, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return PackageTarget{}, err
	}
	mapping, err := normalizer.ParseYAMLData(data)
	if err != nil {
		return PackageTarget{}, err
	}
	if err := rejectUnknownKeys(mapping, map[string]bool{
		"apiVersion": true, "kind": true, "packageKey": true, "rootExtension": true,
		"modulePath": true, "packageName": true, "version": true,
		"minimumGoVersion": true, "publicationMode": true, "dependencies": true,
	}); err != nil {
		return PackageTarget{}, err
	}
	encoded, err := json.Marshal(mapping)
	if err != nil {
		return PackageTarget{}, err
	}
	var target PackageTarget
	if err := decodeStrictJSON(encoded, &target); err != nil {
		return PackageTarget{}, diagnostic("package-config", "RCG1001", path, fmt.Sprintf("invalid package target fields: %v", err))
	}
	return target, nil
}

func rejectUnknownKeys(mapping map[string]any, allowed map[string]bool) error {
	for key := range mapping {
		if !allowed[key] {
			return diagnostic("package-config", "RCG1002", "package-target", fmt.Sprintf("unknown package target field %q", key))
		}
	}
	dependencies, _ := mapping["dependencies"].([]any)
	for index, value := range dependencies {
		entry, ok := value.(map[string]any)
		if !ok {
			return diagnostic("package-config", "RCG1001", "package-target", fmt.Sprintf("dependency %d must be a mapping", index))
		}
		for key := range entry {
			if key != "extension" && key != "modulePath" && key != "packageName" && key != "version" {
				return diagnostic("package-config", "RCG1002", "package-target", fmt.Sprintf("unknown dependency field %q", key))
			}
		}
	}
	return nil
}

func LoadModel(path string) (normalizer.BindingModel, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return normalizer.BindingModel{}, err
	}
	mapping, err := normalizer.ParseYAMLData(data)
	if err != nil {
		return normalizer.BindingModel{}, err
	}
	if err := rejectMissingModelFields(mapping, reflect.TypeOf(normalizer.BindingModel{}), ""); err != nil {
		return normalizer.BindingModel{}, err
	}
	encoded, err := json.Marshal(mapping)
	if err != nil {
		return normalizer.BindingModel{}, err
	}
	var model normalizer.BindingModel
	if err := decodeStrictJSON(encoded, &model); err != nil {
		return normalizer.BindingModel{}, diagnostic("model", "RCG1003", path, fmt.Sprintf("invalid binding model fields: %v", err))
	}
	if err := validateRequiredModelValues(reflect.ValueOf(model), ""); err != nil {
		return normalizer.BindingModel{}, err
	}
	return model, nil
}

func rejectMissingModelFields(value any, modelType reflect.Type, pointer string) error {
	for modelType.Kind() == reflect.Pointer {
		modelType = modelType.Elem()
	}
	switch modelType.Kind() {
	case reflect.Struct:
		mapping, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		for index := 0; index < modelType.NumField(); index++ {
			field := modelType.Field(index)
			name, optional := modelJSONField(field)
			if name == "" {
				continue
			}
			child, exists := mapping[name]
			childPointer := pointer + "/" + escapeJSONPointer(name)
			if !exists {
				if !optional {
					return missingModelField(childPointer)
				}
				continue
			}
			if child == nil && !optional && field.Type.Kind() != reflect.Interface {
				return missingModelField(childPointer)
			}
			if err := rejectMissingModelFields(child, field.Type, childPointer); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		values, ok := value.([]any)
		if !ok {
			return nil
		}
		for index, child := range values {
			if err := rejectMissingModelFields(child, modelType.Elem(), fmt.Sprintf("%s/%d", pointer, index)); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateRequiredModelValues(value reflect.Value, pointer string) error {
	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil
		}
		value = value.Elem()
	}
	switch value.Kind() {
	case reflect.Struct:
		if value.Type() == reflect.TypeOf(normalizer.NormalizedValue{}) {
			normalizedValue := value.Interface().(normalizer.NormalizedValue)
			if _, isString := normalizedValue.Value.(string); isString && len(normalizedValue.Tokens) == 0 {
				return missingModelField(pointer + "/tokens")
			}
		}
		valueType := value.Type()
		for index := 0; index < value.NumField(); index++ {
			fieldType := valueType.Field(index)
			name, optional := modelJSONField(fieldType)
			if name == "" {
				continue
			}
			fieldValue := value.Field(index)
			childPointer := pointer + "/" + escapeJSONPointer(name)
			if !optional && requiredModelValueMissing(fieldValue) {
				return missingModelField(childPointer)
			}
			if optional && fieldValue.IsZero() {
				continue
			}
			if err := validateRequiredModelValues(fieldValue, childPointer); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		for index := 0; index < value.Len(); index++ {
			if err := validateRequiredModelValues(value.Index(index), fmt.Sprintf("%s/%d", pointer, index)); err != nil {
				return err
			}
		}
	}
	return nil
}

func modelJSONField(field reflect.StructField) (string, bool) {
	tag := field.Tag.Get("json")
	parts := strings.Split(tag, ",")
	if parts[0] == "-" {
		return "", false
	}
	name := parts[0]
	if name == "" {
		name = field.Name
	}
	for _, option := range parts[1:] {
		if option == "omitempty" {
			return name, true
		}
	}
	return name, false
}

func requiredModelValueMissing(value reflect.Value) bool {
	for value.Kind() == reflect.Pointer {
		return value.IsNil()
	}
	switch value.Kind() {
	case reflect.String, reflect.Slice, reflect.Array:
		return value.Len() == 0
	case reflect.Map:
		return value.IsNil()
	default:
		return false
	}
}

func missingModelField(pointer string) error {
	return diagnostic("model", "RCG1021", pointer, "binding model is missing a required field")
}

func escapeJSONPointer(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "~", "~0"), "/", "~1")
}

func decodeStrictJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}

func validateInputs(model normalizer.BindingModel, target PackageTarget) error {
	if err := validateRequiredModelValues(reflect.ValueOf(model), ""); err != nil {
		return err
	}
	if model.APIVersion != normalizer.ModelAPIVersion {
		return diagnostic("model", "RCG1004", model.RootExtension.ID, fmt.Sprintf("unsupported model apiVersion %q", model.APIVersion))
	}
	if model.Kind != normalizer.ModelKind {
		return diagnostic("model", "RCG1005", model.RootExtension.ID, fmt.Sprintf("unsupported model kind %q", model.Kind))
	}
	if model.Metadata.SemanticSHA256 == "" || model.RootExtension.ID == "" || model.RootExtension.SemanticSHA256 == "" {
		return diagnostic("model", "RCG1006", model.RootExtension.ID, "binding model is missing required identity fields")
	}
	if target.APIVersion != PackageTargetAPIVersion || target.Kind != PackageTargetKind {
		return diagnostic("package-config", "RCG1007", target.PackageKey, "unsupported Go package target apiVersion or kind")
	}
	if target.RootExtension != model.RootExtension.ID {
		return diagnostic("package-config", "RCG1008", target.PackageKey, fmt.Sprintf("package target root extension %q does not match model root %q", target.RootExtension, model.RootExtension.ID))
	}
	if target.PackageKey == "" || target.ModulePath == "" || target.PackageName == "" || target.Version == "" || target.PublicationMode == "" {
		return diagnostic("package-config", "RCG1009", target.PackageKey, "package target is missing a required field")
	}
	if !token.IsIdentifier(target.PackageName) || token.Lookup(target.PackageName).IsKeyword() {
		return diagnostic("package-config", "RCG1010", target.PackageKey, fmt.Sprintf("invalid Go package name %q", target.PackageName))
	}
	if target.MinimumGoVersion != "1.22" {
		return diagnostic("package-config", "RCG1011", target.PackageKey, fmt.Sprintf("unsupported minimum Go version %q", target.MinimumGoVersion))
	}
	if target.PublicationMode != "github-tag" && target.PublicationMode != "registry" {
		return diagnostic("package-config", "RCG1012", target.PackageKey, fmt.Sprintf("unsupported publication mode %q", target.PublicationMode))
	}
	rootDependencies := map[string]bool{}
	for _, extension := range model.Extensions {
		if extension.ID == model.RootExtension.ID {
			for _, dependency := range extension.Dependencies {
				rootDependencies[dependency] = true
			}
		}
	}
	configured := map[string]bool{}
	for _, dependency := range target.Dependencies {
		if dependency.Extension == "" || dependency.ModulePath == "" || dependency.PackageName == "" || dependency.Version == "" {
			return diagnostic("package-config", "RCG1009", target.PackageKey, "package dependency is missing a required field")
		}
		if configured[dependency.Extension] {
			return diagnostic("package-config", "RCG1013", target.PackageKey, fmt.Sprintf("duplicate package dependency %q", dependency.Extension))
		}
		configured[dependency.Extension] = true
		if !rootDependencies[dependency.Extension] {
			return diagnostic("package-config", "RCG1014", target.PackageKey, fmt.Sprintf("package dependency %q is not a direct extension dependency", dependency.Extension))
		}
	}
	for dependency := range rootDependencies {
		if !configured[dependency] {
			return diagnostic("package-config", "RCG1015", target.PackageKey, fmt.Sprintf("direct extension dependency %q has no Go package target", dependency))
		}
	}
	return validateShapes(model)
}

func validateShapes(model normalizer.BindingModel) error {
	var visit func(normalizer.Shape) error
	visit = func(shape normalizer.Shape) error {
		switch shape.Kind {
		case "scalar":
			if shape.Scalar != "string" && shape.Scalar != "boolean" && shape.Scalar != "integer" && shape.Scalar != "number" && shape.Scalar != "null" {
				return diagnostic("model", "RCG1016", shape.Provenance.Coordinate, fmt.Sprintf("unknown scalar type %q", shape.Scalar))
			}
		case "object":
			for _, property := range shape.Properties {
				if err := visit(property.Shape); err != nil {
					return err
				}
			}
		case "array":
			if shape.Items == nil {
				return diagnostic("model", "RCG1017", shape.Provenance.Coordinate, "array shape has no item shape")
			}
			return visit(*shape.Items)
		case "map":
			if shape.MapValues == nil {
				return diagnostic("model", "RCG1018", shape.Provenance.Coordinate, "map shape has no value shape")
			}
			return visit(*shape.MapValues)
		case "union":
			if len(shape.Variants) == 0 {
				return diagnostic("model", "RCG1019", shape.Provenance.Coordinate, "union shape has no variants")
			}
			for _, variant := range shape.Variants {
				if err := visit(variant); err != nil {
					return err
				}
			}
		case "ref", "any":
		default:
			return diagnostic("model", "RCG1020", shape.Provenance.Coordinate, fmt.Sprintf("unknown structural node %q", shape.Kind))
		}
		return nil
	}
	for _, scope := range model.Scopes {
		if scope.Projection != nil {
			if err := visit(*scope.Projection); err != nil {
				return err
			}
		}
	}
	for _, schema := range model.Schemas {
		if err := visit(schema.Projection); err != nil {
			return err
		}
		for _, definition := range schema.Definitions {
			if err := visit(definition.Shape); err != nil {
				return err
			}
		}
	}
	return nil
}

type Manifest struct {
	APIVersion string            `yaml:"apiVersion"`
	Kind       string            `yaml:"kind"`
	Generated  ManifestGenerated `yaml:"generated"`
	Model      ManifestModel     `yaml:"model"`
	Extension  ManifestExtension `yaml:"extension"`
	Package    ManifestPackage   `yaml:"package"`
	Symbols    []ManifestSymbol  `yaml:"symbols"`
}

type ManifestGenerated struct {
	NonEditable bool   `yaml:"nonEditable"`
	Emitter     string `yaml:"emitter"`
	Version     string `yaml:"version"`
}

type ManifestModel struct {
	APIVersion     string `yaml:"apiVersion"`
	SemanticSHA256 string `yaml:"semanticSha256"`
}

type ManifestExtension struct {
	ID             string `yaml:"id"`
	SemanticSHA256 string `yaml:"semanticSha256"`
}

type ManifestPackage struct {
	Language         string `yaml:"language"`
	Coordinate       string `yaml:"coordinate"`
	Name             string `yaml:"name"`
	MinimumGoVersion string `yaml:"minimumGoVersion"`
}

type ManifestSymbol struct {
	Construct  string `yaml:"construct"`
	Coordinate string `yaml:"coordinate"`
	SourceName string `yaml:"sourceName,omitempty"`
	NativeName string `yaml:"nativeName"`
	Parent     string `yaml:"parent,omitempty"`
	File       string `yaml:"file,omitempty"`
}

func marshalManifest(manifest Manifest) ([]byte, error) {
	sort.Slice(manifest.Symbols, func(i, j int) bool {
		if manifest.Symbols[i].Coordinate != manifest.Symbols[j].Coordinate {
			return manifest.Symbols[i].Coordinate < manifest.Symbols[j].Coordinate
		}
		if manifest.Symbols[i].Construct != manifest.Symbols[j].Construct {
			return manifest.Symbols[i].Construct < manifest.Symbols[j].Construct
		}
		return manifest.Symbols[i].NativeName < manifest.Symbols[j].NativeName
	})
	data, err := yaml.Marshal(manifest)
	if err != nil {
		return nil, err
	}
	data = bytes.TrimRight(data, "\n")
	var result bytes.Buffer
	writeGeneratedHeader(&result, "#", manifest.Model.SemanticSHA256)
	result.Write(data)
	result.WriteByte('\n')
	return result.Bytes(), nil
}

func prepareOutput(path string) error {
	entries, err := os.ReadDir(path)
	if err == nil {
		if len(entries) != 0 {
			return diagnostic("output", "RCG1021", path, "output directory must be empty")
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	return os.MkdirAll(path, 0o755)
}

func writeFiles(root string, files map[string][]byte) error {
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		fullPath := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(fullPath, files[path], 0o644); err != nil {
			return err
		}
	}
	return nil
}

func SourceArchive(root string) ([]byte, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
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
		paths = append(paths, filepath.ToSlash(relative))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	for _, path := range paths {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			return nil, err
		}
		header := &zip.FileHeader{Name: path, Method: zip.Deflate}
		header.SetMode(0o644)
		header.Modified = time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)
		writer, err := archive.CreateHeader(header)
		if err != nil {
			return nil, err
		}
		if _, err := writer.Write(data); err != nil {
			return nil, err
		}
	}
	if err := archive.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func VerifySourceArchive(data []byte, declared []string) error {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	want := append([]string(nil), declared...)
	sort.Strings(want)
	seen := map[string]bool{}
	var actual []string
	for _, file := range reader.File {
		name := file.Name
		if name == "" || name == "." || name == ".." || path.IsAbs(name) || path.Clean(name) != name ||
			strings.HasPrefix(name, "../") || strings.Contains(name, "\\") || seen[name] {
			return diagnostic("archive", "RCG1022", name, "archive contains an invalid or duplicate path")
		}
		seen[name] = true
		actual = append(actual, name)
		stream, err := file.Open()
		if err != nil {
			return err
		}
		if _, err := io.Copy(io.Discard, stream); err != nil {
			stream.Close()
			return err
		}
		if err := stream.Close(); err != nil {
			return err
		}
	}
	sort.Strings(actual)
	if strings.Join(actual, "\n") != strings.Join(want, "\n") {
		return diagnostic("archive", "RCG1023", "source-archive", fmt.Sprintf("archive files %v do not match declared files %v", actual, want))
	}
	return nil
}
