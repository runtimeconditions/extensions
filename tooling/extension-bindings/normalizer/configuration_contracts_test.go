package normalizer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const catalogContractFixture = `apiVersion: runtimeconditions.io/package-catalog/v1alpha1
kind: RuntimeConditionsPackageCatalog
repositoryUrl: https://example.test/future-extensions
packages:
  future-extension:
    rootExtension: https://example.test/unseen-extension:2.3.0
    coreProfileSchemaVersion: 0.2.0
    languages:
      future-language:
        coordinate: example.test/future-binding
        name: future_binding
        version: 1.2.3-beta.1+build.7
        sourceDirectory: bindings/future-extension/future-language
        languageVersion: 4.5.6
        publicationMode: github-tag
`

const releasedLockContractFixture = `apiVersion: runtimeconditions.io/toolchain-lock/v1alpha1
kind: RuntimeConditionsToolchainLock
status: released
tools:
  normalizer: &tool
    name: future-tool
    version: 1.2.3
    sha256: DIGEST
  orchestrator: *tool
  emitters:
    future-language: *tool
  profilers:
    future-language: *tool
languages:
  future-language:
    languageVersion: 4.5.6
    packageManager: future-package-manager
    tools:
      compiler: 4.5.6
`

func TestPackageCatalogContract(t *testing.T) {
	cases := []struct {
		name, old, replacement string
		valid                  bool
	}{
		{"future extension and language", "", "", true},
		{"registry target", "publicationMode: github-tag", "publicationMode: registry\n        registryId: example-registry", true},
		{"registry requires identity", "publicationMode: github-tag", "publicationMode: registry", false},
		{"tag has no registry identity", "publicationMode: github-tag", "publicationMode: github-tag\n        registryId: unexpected", false},
		{"no extension semantics", "coreProfileSchemaVersion: 0.2.0", "coreProfileSchemaVersion: 0.2.0\n    vocabulary: {}", false},
		{"dependencies come from model", "coreProfileSchemaVersion: 0.2.0", "coreProfileSchemaVersion: 0.2.0\n    dependencies: []", false},
		{"runtime includes patch", "languageVersion: 4.5.6", "languageVersion: '4.5'", false},
		{"relative generated location", "bindings/future-extension/future-language", "../bindings/future-extension/future-language", false},
		{"package version is semver", "1.2.3-beta.1+build.7", "01.2.3", false},
		{"root permits opaque scheme", "https://example.test/unseen-extension:2.3.0", "urn:future-extension:2.3.0", true},
		{"root permits direct document URL", "https://example.test/unseen-extension:2.3.0", "https://example.test/releases/unseen/2.3.0/definition.yaml", true},
		{"root requires absolute identifier", "https://example.test/unseen-extension:2.3.0", "releases/definition.yaml", false},
		{"package keys are slugs", "future-extension:", "Future-Extension:", false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			document := catalogContractFixture
			if test.old != "" {
				document = strings.Replace(document, test.old, test.replacement, 1)
			}
			assertConfigurationContract(t, "package-catalog", document, test.valid)
		})
	}
}

func TestToolchainLockContract(t *testing.T) {
	development, err := os.ReadFile(filepath.Join("..", "toolchain.lock.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	assertConfigurationContract(t, "toolchain-lock", string(development), true)
	released := strings.ReplaceAll(releasedLockContractFixture, "DIGEST", SHA256Hex([]byte("schema fixture tool")))
	cases := []struct {
		name, old, replacement string
		valid                  bool
	}{
		{"complete future toolchain", "", "", true},
		{"development workspace tools", "status: released", "status: development", true},
		{"profiler required", "  profilers:", "  unknown-profilers:", false},
		{"orchestrator required", "  orchestrator: *tool\n", "", false},
		{"exact language version", "languageVersion: 4.5.6", "languageVersion: '4.5'", false},
		{"bad artifact digest", SHA256Hex([]byte("schema fixture tool")), "bad", false},
		{"unknown lock field", "status: released", "status: released\nunknown: true", false},
		{"unknown release status", "status: released", "status: ready", false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			document := released
			if test.old != "" {
				document = strings.Replace(document, test.old, test.replacement, 1)
			}
			assertConfigurationContract(t, "toolchain-lock", document, test.valid)
		})
	}
	assertConfigurationContract(t, "toolchain-lock", "apiVersion: runtimeconditions.io/toolchain-lock/v1alpha1\nkind: RuntimeConditionsToolchainLock\nstatus: released\n", false)
}

func TestFileManifestContract(t *testing.T) {
	schema, _, err := compileYAMLSchema(filepath.Join("..", "model", "runtimeconditions.file-manifest.schema.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"bindings.go", "src/future/metadata.yaml", ".hidden", "..hidden", "src/café data.py"} {
		if err := schema.Validate(fileContractDocument(path, SHA256Hex([]byte(path)))); err != nil {
			t.Errorf("valid path %q: %v", path, err)
		}
	}
	for _, path := range []string{"", "/absolute", "../escape", "src/../escape", "./local", "src/./local", "src//empty", "src/", "C:/drive", "src\\file", "src/line\nfeed", "runtimeconditions.file-manifest.yaml"} {
		if err := schema.Validate(fileContractDocument(path, SHA256Hex([]byte(path)))); err == nil {
			t.Errorf("unsafe path %q accepted", path)
		}
	}
	for _, digest := range []string{"", strings.Repeat("A", 64), strings.Repeat("a", 63)} {
		if err := schema.Validate(fileContractDocument("bindings.go", digest)); err == nil {
			t.Errorf("invalid digest %q accepted", digest)
		}
	}
	document := fileContractDocument("bindings.go", SHA256Hex([]byte("bindings")))
	document["timestamp"] = "2026-01-01"
	if err := schema.Validate(document); err == nil {
		t.Error("host metadata accepted")
	}
	duplicate := "files:\n  bindings.go: first\n  bindings.go: second\n"
	if _, err := ParseYAMLData([]byte(duplicate)); err == nil {
		t.Error("duplicate file paths accepted by YAML loader")
	}
}

func fileContractDocument(path, digest string) map[string]any {
	return map[string]any{
		"apiVersion": "runtimeconditions.io/file-manifest/v1alpha1",
		"kind":       "RuntimeConditionsFileManifest",
		"files":      map[string]any{path: digest},
	}
}

func assertConfigurationContract(t *testing.T, name, document string, valid bool) {
	t.Helper()
	schema, _, err := compileYAMLSchema(filepath.Join("..", "model", "runtimeconditions."+name+".schema.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseYAMLData([]byte(document))
	if err != nil {
		t.Fatal(err)
	}
	err = schema.Validate(parsed)
	if (err == nil) != valid {
		t.Fatalf("schema validation = %v, want valid=%v", err, valid)
	}
}
