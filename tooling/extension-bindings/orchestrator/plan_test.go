package orchestrator

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/runtimeconditions/extensions/tooling/extension-bindings/normalizer"
	"gopkg.in/yaml.v3"
)

func testProject(t *testing.T, language string) (*Project, Options) {
	t.Helper()
	root := t.TempDir()
	// Keep repository discovery local when TMPDIR is inside the extensions checkout.
	if err := os.Mkdir(filepath.Join(root, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	tooling, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	definitions := filepath.Join(root, "catalog")
	if err = os.MkdirAll(definitions, 0755); err != nil {
		t.Fatal(err)
	}
	// Previously unseen identity and vocabulary, deliberately unrelated to repository fixtures.
	extension := `apiVersion: runtimeconditions.io/v1alpha1
kind: RuntimeConditionsExtensionDefinition
metadata:
  uri: https://new.example.test/provider/future-channel
  version: 0.7.0
spec:
  kinds: [{name: future.channel}]
  interfaceTypes: [{name: stream, targetKind: future.channel}]
  interfaceFields: [{name: address, targetKind: future.channel, targetType: stream}]
  schemas:
    - id: future-channel-schema
      description: A future extension used to exercise the generic mechanism.
      appliesToKind: future.channel
      appliesToInterfaceType: stream
      schema:
        type: object
        required: [kind, interface]
        properties:
          kind: {const: future.channel}
          interface:
            type: object
            required: [type, address]
            properties:
              type: {const: stream}
              address: {type: string, minLength: 1, maxLength: 10}
            additionalProperties: false
        additionalProperties: false
`
	if err = os.WriteFile(filepath.Join(definitions, extensionName), []byte(extension), 0644); err != nil {
		t.Fatal(err)
	}
	goCommand := envOr("RC_BINDINGS_GO", "go")
	goVersion, err := exec.Command(goCommand, "version").Output()
	if err != nil || len(strings.Fields(string(goVersion))) < 3 {
		t.Fatalf("native Go version: %s %v", goVersion, err)
	}
	target := LanguageTarget{Coordinate: "example.test/bindings/new-channel", Name: "newchannel", Version: "0.7.0", SourceDirectory: "bindings/new-channel/" + language, LanguageVersion: strings.TrimPrefix(strings.Fields(string(goVersion))[2], "go"), PublicationMode: "github-tag"}
	if language == "python" {
		target.Coordinate = "runtimeconditions-new-channel"
		target.Name = "runtimeconditions_new_channel"
		target.LanguageVersion = "3.12.10"
	}
	catalog := Catalog{APIVersion: "runtimeconditions.io/package-catalog/v1alpha1", Kind: "RuntimeConditionsPackageCatalog", RepositoryURL: "https://github.com/example/future-bindings", Packages: map[string]PackageSet{"new-channel": {RootExtension: "https://new.example.test/provider/future-channel:0.7.0", CoreProfileSchemaVersion: "0.2.0", Languages: map[string]LanguageTarget{language: target}}}}
	data, _ := yaml.Marshal(catalog)
	if err = os.WriteFile(filepath.Join(root, "packages.yaml"), data, 0644); err != nil {
		t.Fatal(err)
	}
	lock := []byte("apiVersion: runtimeconditions.io/toolchain-lock/v1alpha1\nkind: RuntimeConditionsToolchainLock\nstatus: development\n")
	if err = os.WriteFile(filepath.Join(root, "toolchain.lock.yaml"), lock, 0644); err != nil {
		t.Fatal(err)
	}
	project, err := DiscoverProject(root, "", "", tooling)
	if err != nil {
		t.Fatal(err)
	}
	core := filepath.Join(tooling, "..", "..", "..", "spec", "schema", "runtimeconditions.profile.schema.yaml")
	options := Options{ToolingDir: tooling, Go: envOr("RC_BINDINGS_GO", "go"), Python: envOr("RC_BINDINGS_PYTHON", "python3"), GoProfiler: envOr("RC_GO_PROFILER_BIN", "go-rc-profiler"), PythonProfiler: envOr("RC_PYTHON_PROFILER_BIN", "runtimeconditions-python-profiler"), PythonProfilerArtifact: os.Getenv("RC_PYTHON_PROFILER_ARTIFACT"), CoreSchema: core}
	return project, options
}
func TestProjectDiscoveryAndStrictMetadata(t *testing.T) {
	p, _ := testProject(t, "go")
	nested := filepath.Join(p.Root, "nested", "directory")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatal(err)
	}
	found, err := DiscoverProject(nested, "", "", p.ToolingDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if found.Root != p.Root {
		t.Fatalf("root %s", found.Root)
	}
	if _, err = DiscoverProject(t.TempDir(), filepath.Join(t.TempDir(), "missing-packages.yaml"), "", p.ToolingDirectory); err == nil {
		t.Fatal("missing configs accepted")
	}
	catalog, _ := readMapping(p.PackagesPath)
	mapValue(mapValue(catalog["packages"])["new-channel"])["kinds"] = []any{"special"}
	data, _ := yaml.Marshal(catalog)
	_ = os.WriteFile(p.PackagesPath, data, 0644)
	if _, err = DiscoverProject(p.Root, "", "", p.ToolingDirectory); err == nil {
		t.Fatal("semantics in package metadata accepted")
	}
}
func TestResolveRetainsOnlyRequestedFiles(t *testing.T) {
	p, o := testProject(t, "go")
	var stdout, stderr bytes.Buffer
	lock := filepath.Join(p.Root, "requested-lock.yaml")
	args := []string{"bindings", "resolve", "--packages", p.PackagesPath, "--toolchain-lock", p.ToolchainPath, "--tooling-dir", p.ToolingDirectory, "--dependency-lock-output", lock}
	if err := Run(context.Background(), args, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "---\n") || !exists(lock) {
		t.Fatal("missing closure/lock output")
	}
	if _, err := os.Stat(filepath.Join(p.Root, "bindings")); !os.IsNotExist(err) {
		t.Fatal("resolve created generated tree")
	}
	if _, err := os.Stat(filepath.Join(p.Root, "work")); !os.IsNotExist(err) {
		t.Fatal("resolve created fixed work directory")
	}
	_ = o
}
func TestTargetOrderingUsesResolvedEdges(t *testing.T) {
	p, _ := testProject(t, "go")
	root := p.Catalog.Packages["new-channel"]
	provider := root
	provider.RootExtension = "https://other.example.test/future-base:0.1.0"
	provider.Languages = map[string]LanguageTarget{"go": {Coordinate: "example.test/base", Name: "base", Version: "0.1.0", SourceDirectory: "bindings/base/go", LanguageVersion: "1.26.5", PublicationMode: "github-tag"}}
	p.Catalog.Packages["base"] = provider
	plan, err := p.SelectTargets([]string{"new-channel:go"})
	if err != nil {
		t.Fatal(err)
	}
	models := map[string]normalizer.BindingModel{root.RootExtension: {DependencyEdges: []normalizer.DependencyEdge{{From: root.RootExtension, To: provider.RootExtension}}}, provider.RootExtension: {}}
	ordered, err := p.OrderTargets(plan, models)
	if err != nil {
		t.Fatal(err)
	}
	if len(ordered.Targets) != 2 || ordered.Targets[0].Key != "base:go" || ordered.Targets[0].Selected || !ordered.Targets[1].Selected {
		t.Fatalf("bad plan: %+v", ordered)
	}
}
func TestCLIHelpUsesFlagPackage(t *testing.T) {
	for _, command := range []string{"resolve", "normalize", "generate", "verify", "package", "plan-release", "check", "update"} {
		var out, errout bytes.Buffer
		if err := Run(context.Background(), []string{"bindings", command, "--help"}, &out, &errout); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(errout.String(), "-packages") {
			t.Fatalf("%s missing common flags", command)
		}
	}
}

func TestResolveValidatesExistingDependencyLock(t *testing.T) {
	p, o := testProject(t, "go")
	first, err := NewPipeline(context.Background(), p, o)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	plan, _ := p.SelectTargets(nil)
	docs, err := first.Resolve(plan)
	if err != nil {
		t.Fatal(err)
	}
	lock := docs[0].Lock
	lock.Extensions[0].Dependencies = []string{"https://new.example.test/missing:1.0.0"}
	data, _ := yaml.Marshal(lock)
	o.DependencyLock = filepath.Join(p.Root, "wrong-lock.yaml")
	if err = os.WriteFile(o.DependencyLock, data, 0644); err != nil {
		t.Fatal(err)
	}
	second, err := NewPipeline(context.Background(), p, o)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if _, err = second.Resolve(plan); err == nil {
		t.Fatal("existing dependency lock was replaced with a fresh lock")
	}
}

func TestCatalogRejectsUnsafeModuleCoordinate(t *testing.T) {
	p, _ := testProject(t, "go")
	set := p.Catalog.Packages["new-channel"]
	target := set.Languages["go"]
	target.Coordinate = "../../outside"
	set.Languages["go"] = target
	p.Catalog.Packages["new-channel"] = set
	data, _ := yaml.Marshal(p.Catalog)
	_ = os.WriteFile(p.PackagesPath, data, 0644)
	if _, err := DiscoverProject(p.Root, "", "", p.ToolingDirectory); err == nil {
		t.Fatal("unsafe module coordinate accepted")
	}
}
