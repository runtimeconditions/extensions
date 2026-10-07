// Package orchestrator implements the language-neutral rc bindings pipeline.
package orchestrator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"unicode"

	"github.com/runtimeconditions/extensions/tooling/extension-bindings/normalizer"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const Version = "0.1.0"
const fileManifestName = "runtimeconditions.file-manifest.yaml"
const modelName = "runtimeconditions.binding-model.yaml"
const bindingName = "runtimeconditions.bindings.yaml"
const releaseName = "runtimeconditions.binding-release.yaml"
const extensionName = "runtimeconditions.extension.yaml"

type Catalog struct {
	APIVersion    string                `json:"apiVersion" yaml:"apiVersion"`
	Kind          string                `json:"kind" yaml:"kind"`
	RepositoryURL string                `json:"repositoryUrl,omitempty" yaml:"repositoryUrl,omitempty"`
	Packages      map[string]PackageSet `json:"packages" yaml:"packages"`
}
type PackageSet struct {
	RootExtension            string                    `json:"rootExtension" yaml:"rootExtension"`
	CoreProfileSchemaVersion string                    `json:"coreProfileSchemaVersion" yaml:"coreProfileSchemaVersion"`
	Languages                map[string]LanguageTarget `json:"languages" yaml:"languages"`
}
type LanguageTarget struct {
	Coordinate      string `json:"coordinate" yaml:"coordinate"`
	Name            string `json:"name,omitempty" yaml:"name,omitempty"`
	Version         string `json:"version" yaml:"version"`
	SourceDirectory string `json:"sourceDirectory" yaml:"sourceDirectory"`
	LanguageVersion string `json:"languageVersion" yaml:"languageVersion"`
	PublicationMode string `json:"publicationMode" yaml:"publicationMode"`
	RegistryID      string `json:"registryId,omitempty" yaml:"registryId,omitempty"`
}
type Tool struct {
	Name        string `json:"name" yaml:"name"`
	Version     string `json:"version" yaml:"version"`
	SHA256      string `json:"sha256" yaml:"sha256"`
	ArtifactURL string `json:"artifactUrl,omitempty" yaml:"artifactUrl,omitempty"`
}
type Toolchain struct {
	APIVersion string `json:"apiVersion" yaml:"apiVersion"`
	Kind       string `json:"kind" yaml:"kind"`
	Status     string `json:"status" yaml:"status"`
	Tools      struct {
		Normalizer   Tool            `json:"normalizer" yaml:"normalizer"`
		Orchestrator Tool            `json:"orchestrator" yaml:"orchestrator"`
		Emitters     map[string]Tool `json:"emitters" yaml:"emitters"`
		Profilers    map[string]Tool `json:"profilers" yaml:"profilers"`
	} `json:"tools" yaml:"tools"`
	Languages map[string]LanguageToolchain `json:"languages" yaml:"languages"`
	Python    struct {
		MinimumVersion     string            `json:"minimumVersion" yaml:"minimumVersion"`
		InterpreterVersion string            `json:"interpreterVersion" yaml:"interpreterVersion"`
		PackageManager     string            `json:"packageManager" yaml:"packageManager"`
		BuildBackend       string            `json:"buildBackend" yaml:"buildBackend"`
		Tools              map[string]string `json:"tools" yaml:"tools"`
	} `json:"python" yaml:"python"`
}
type LanguageToolchain struct {
	LanguageVersion string            `json:"languageVersion" yaml:"languageVersion"`
	PackageManager  string            `json:"packageManager" yaml:"packageManager"`
	BuildBackend    string            `json:"buildBackend,omitempty" yaml:"buildBackend,omitempty"`
	Tools           map[string]string `json:"tools" yaml:"tools"`
}
type Project struct {
	Root, RepositoryRoot, PackagesPath, ToolchainPath, ToolingDirectory string
	Catalog                                                             Catalog
	Toolchain                                                           Toolchain
	Schemas                                                             *normalizer.Schemas
}

func absolute(cwd, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(cwd, path)
}
func exists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// DiscoverProject implements the Section 15 path rules, independently of Git.
func DiscoverProject(cwd, packagesPath, toolchainPath, toolingDirectory string) (*Project, error) {
	cwd, err := filepath.Abs(cwd)
	if err != nil {
		return nil, err
	}
	root := ""
	if packagesPath != "" {
		packagesPath = absolute(cwd, packagesPath)
		root = filepath.Dir(packagesPath)
	}
	if toolchainPath != "" {
		toolchainPath = absolute(cwd, toolchainPath)
		if root == "" {
			root = filepath.Dir(toolchainPath)
		}
	}
	if root == "" {
		for dir := cwd; ; dir = filepath.Dir(dir) {
			if exists(filepath.Join(dir, "packages.yaml")) && exists(filepath.Join(dir, "toolchain.lock.yaml")) {
				root = dir
				break
			}
			if filepath.Dir(dir) == dir {
				break
			}
		}
	}
	if root == "" {
		return nil, fmt.Errorf("no project contains packages.yaml and toolchain.lock.yaml; supply --packages and --toolchain-lock")
	}
	if packagesPath == "" {
		packagesPath = filepath.Join(root, "packages.yaml")
	}
	if toolchainPath == "" {
		toolchainPath = filepath.Join(root, "toolchain.lock.yaml")
	}
	repository := root
	for dir := root; ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			repository = dir
			break
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
	if toolingDirectory != "" {
		toolingDirectory = absolute(cwd, toolingDirectory)
	} else {
		candidates := []string{root, filepath.Join(repository, "tooling", "extension-bindings")}
		executable, _ := os.Executable()
		candidates = append(candidates, filepath.Join(filepath.Dir(executable), "..", "share", "rc", "extension-bindings"))
		_, source, _, _ := runtime.Caller(0)
		candidates = append(candidates, filepath.Dir(filepath.Dir(source)))
		for _, dir := range candidates {
			if exists(filepath.Join(dir, "model", "runtimeconditions.binding-model.schema.yaml")) {
				toolingDirectory = dir
				break
			}
		}
	}
	if toolingDirectory == "" {
		return nil, fmt.Errorf("schema bundle unavailable; supply --tooling-dir")
	}
	p := &Project{Root: root, RepositoryRoot: repository, PackagesPath: packagesPath, ToolchainPath: toolchainPath, ToolingDirectory: toolingDirectory}
	if err := decodeSchemaFile(packagesPath, filepath.Join(toolingDirectory, "model", "runtimeconditions.package-catalog.schema.yaml"), &p.Catalog); err != nil {
		return nil, err
	}
	if err := decodeSchemaFile(toolchainPath, filepath.Join(toolingDirectory, "model", "runtimeconditions.toolchain-lock.schema.yaml"), &p.Toolchain); err != nil {
		return nil, err
	}
	for key, set := range p.Catalog.Packages {
		for language, target := range set.Languages {
			if language == "go" && (target.Coordinate == "." || path.IsAbs(target.Coordinate) || path.Clean(target.Coordinate) != target.Coordinate || strings.HasPrefix(target.Coordinate, "../") || strings.ContainsAny(target.Coordinate, "\\:") || strings.ContainsFunc(target.Coordinate, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) })) {
				return nil, fmt.Errorf("%s:%s: module coordinate must be a safe relative module path", key, language)
			}
			expected := "bindings/" + key + "/" + language
			if target.SourceDirectory != expected {
				return nil, fmt.Errorf("%s:%s: sourceDirectory must equal %s", key, language, expected)
			}
			if target.Name == "" {
				return nil, fmt.Errorf("%s:%s: name is required by the %s native emitter", key, language, language)
			}
			if p.Toolchain.Status == "released" {
				if p.Toolchain.Tools.Emitters[language].SHA256 == "" || p.Toolchain.Tools.Profilers[language].SHA256 == "" {
					return nil, fmt.Errorf("%s: released lock lacks emitter or profiler", language)
				}
				native, ok := p.Toolchain.Languages[language]
				if !ok || native.LanguageVersion != target.LanguageVersion {
					return nil, fmt.Errorf("%s:%s: exact language version differs from released lock", key, language)
				}
			}
		}
	}
	p.Schemas, err = normalizer.LoadSchemas(filepath.Join(toolingDirectory, "model", "runtimeconditions.extension-semantic.schema.yaml"), filepath.Join(toolingDirectory, "model", "runtimeconditions.binding-model.schema.yaml"))
	if err != nil {
		return nil, err
	}
	return p, nil
}
func readMapping(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return normalizer.ParseYAMLData(data)
}
func decodeSchemaFile(path, schemaPath string, target any) error {
	mapping, err := readMapping(path)
	if err != nil {
		return err
	}
	schema, err := readMapping(schemaPath)
	if err != nil {
		return err
	}
	if err = validateSchema(schema, mapping); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	data, err := json.Marshal(mapping)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}
func validateSchema(schema map[string]any, value any) error {
	data, err := json.Marshal(schema)
	if err != nil {
		return err
	}
	resource, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return err
	}
	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()
	uri := "urn:runtimeconditions:orchestrator-schema:" + normalizer.SHA256Hex(data)
	if err = compiler.AddResource(uri, resource); err != nil {
		return err
	}
	compiled, err := compiler.Compile(uri)
	if err != nil {
		return err
	}
	// Marshal typed values to the JSON-compatible representation required by the validator.
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	var decoded any
	if err = json.Unmarshal(encoded, &decoded); err != nil {
		return err
	}
	return compiled.Validate(decoded)
}
func sortedKeys[V any](mapping map[string]V) []string {
	keys := make([]string, 0, len(mapping))
	for key := range mapping {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
func majorMinor(version string) string {
	parts := strings.Split(version, ".")
	return strings.Join(parts[:2], ".")
}
