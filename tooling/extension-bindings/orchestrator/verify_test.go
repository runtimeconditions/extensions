package orchestrator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/runtimeconditions/extensions/tooling/extension-bindings/normalizer"
	"gopkg.in/yaml.v3"
)

func TestManifestRejectsTamperingAndUndeclaredFiles(t *testing.T) {
	root := t.TempDir()
	_ = os.WriteFile(filepath.Join(root, "source.go"), []byte("package generated\n"), 0644)
	if err := writeFileManifest(root); err != nil {
		t.Fatal(err)
	}
	schema := filepath.Join("..", "model", "runtimeconditions.file-manifest.schema.yaml")
	if err := verifyFileManifest(root, schema); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(root, "source.go"), []byte("package changed\n"), 0644)
	if err := verifyFileManifest(root, schema); err == nil {
		t.Fatal("tampering accepted")
	}
	_ = writeFileManifest(root)
	_ = os.WriteFile(filepath.Join(root, "undeclared.txt"), []byte("extra"), 0644)
	if err := verifyFileManifest(root, schema); err == nil {
		t.Fatal("undeclared file accepted")
	}
}

func TestReleasedPythonEmitterUsesInstalledArtifactAndRejectsWrongLock(t *testing.T) {
	artifact := os.Getenv("RC_BINDINGS_PYTHON_EMITTER_ARTIFACT")
	if artifact == "" {
		t.Skip("set the installed emitter release wheel for released-path checks")
	}
	project, options := testProject(t, "python")
	data, err := os.ReadFile(artifact)
	if err != nil {
		t.Fatal(err)
	}
	project.Toolchain.Status = "released"
	project.Toolchain.Tools.Emitters = map[string]Tool{"python": {Name: "runtimeconditions-binding-emitter", Version: Version, SHA256: fmt.Sprintf("%x", sha256.Sum256(data))}}
	shadow := filepath.Join(project.Root, "runtimeconditions_binding_emitter")
	_ = os.MkdirAll(shadow, 0755)
	_ = os.WriteFile(filepath.Join(shadow, "__init__.py"), nil, 0644)
	_ = os.WriteFile(filepath.Join(shadow, "__main__.py"), []byte("raise RuntimeError('unreleased project emitter executed')\n"), 0644)
	t.Setenv("PYTHONPATH", project.Root)
	pipeline, err := NewPipeline(context.Background(), project, options)
	if err != nil {
		t.Fatal(err)
	}
	defer pipeline.Close()
	if _, err = pipeline.pythonEmitterIdentity(); err != nil {
		t.Fatal("installed artifact rejected or project source executed", err)
	}
	project.Toolchain.Tools.Emitters["python"] = Tool{Name: "runtimeconditions-binding-emitter", Version: Version, SHA256: strings.Repeat("0", 64)}
	wrong, _ := NewPipeline(context.Background(), project, options)
	defer wrong.Close()
	if _, err = wrong.pythonEmitterIdentity(); err == nil {
		t.Fatal("released emitter accepted a mismatched toolchain digest")
	}
}

func TestProductionRejectsMissingMalformedAndMismatchedEmitterDigests(t *testing.T) {
	if os.Getenv("RC_GO_PROFILER_BIN") == "" || os.Getenv("RC_PYTHON_PROFILER_ARTIFACT") == "" {
		t.Skip("set independent profiler artifact paths")
	}
	for _, language := range []string{"go", "python"} {
		t.Run(language, func(t *testing.T) {
			project, options := testProject(t, language)
			pipeline, err := NewPipeline(context.Background(), project, options)
			if err != nil {
				t.Fatal(err)
			}
			defer pipeline.Close()
			plan, _ := project.SelectTargets(nil)
			build, err := pipeline.Generate(plan)
			if err != nil {
				t.Fatal(err)
			}
			bt := build.Targets[0]
			path := filepath.Join(resourceDir(bt), bindingName)
			original, _ := os.ReadFile(path)
			emitterName, _, _ := strings.Cut(stringValue(mapValue(bt.Manifest["generated"])["emitter"]), "@sha256:")
			for _, invalid := range []string{"", emitterName + "@sha256:malformed", emitterName + "@sha256:" + strings.Repeat("0", 64)} {
				manifest, _ := readMapping(path)
				generated := mapValue(manifest["generated"])
				if invalid == "" {
					delete(generated, "emitter")
				} else {
					generated["emitter"] = invalid
				}
				data, _ := yaml.Marshal(manifest)
				_ = os.WriteFile(path, data, 0644)
				if err = writeFileManifest(bt.Directory); err != nil {
					t.Fatal(err)
				}
				if summary, err := pipeline.Verify(build); err == nil || len(summary.Errors) == 0 || !strings.Contains(summary.Errors[0], "gate 2") {
					t.Fatalf("emitter digest %q escaped the identity gate: %+v %v", invalid, summary, err)
				}
				_ = os.WriteFile(path, original, 0644)
			}
			_ = writeFileManifest(bt.Directory)
			releasePath := filepath.Join(resourceDir(bt), releaseName)
			release, _ := readMapping(releasePath)
			provenance := mapValue(release["provenance"])
			release["provenance"] = map[string]any{"mode": "test-fixture", "fixtureAssembler": provenance["orchestrator"], "profiler": provenance["profiler"]}
			data, _ := yaml.Marshal(release)
			_ = os.WriteFile(releasePath, data, 0644)
			_ = writeFileManifest(bt.Directory)
			if _, err = pipeline.Verify(build); err == nil {
				t.Fatal("fixture provenance accepted as production")
			}
		})
	}
}

func TestEightCLICommandsWithInstalledPackages(t *testing.T) {
	if os.Getenv("RC_GO_PROFILER_BIN") == "" || os.Getenv("RC_PYTHON_PROFILER_ARTIFACT") == "" {
		t.Skip("set independent profiler artifact paths for CLI integration")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "[]")
	}))
	defer server.Close()
	t.Setenv("RC_BINDINGS_GITHUB_API", server.URL)
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	for _, language := range []string{"go", "python"} {
		t.Run(language, func(t *testing.T) {
			project, options := testProject(t, language)
			common := []string{"--packages", project.PackagesPath, "--toolchain-lock", project.ToolchainPath, "--tooling-dir", project.ToolingDirectory, "--core-schema", options.CoreSchema}
			run := func(command string, flags ...string) []byte {
				t.Helper()
				var out, diagnostic bytes.Buffer
				args := append([]string{"bindings", command}, common...)
				args = append(args, flags...)
				if err := Run(context.Background(), args, &out, &diagnostic); err != nil {
					t.Fatalf("%s: %v\n%s\n%s", command, err, out.String(), diagnostic.String())
				}
				return out.Bytes()
			}
			generated := filepath.Join(project.Root, "generated")
			run("resolve")
			run("normalize", "--output", filepath.Join(project.Root, "model.yaml"))
			run("generate", "--output", generated)
			run("verify", "--input", generated, "--report", filepath.Join(project.Root, "verification.yaml"))
			run("update")
			run("check")
			run("package", "--output", filepath.Join(project.Root, "dist"))
			run("plan-release", "--output", filepath.Join(project.Root, "release-plan.yaml"))
			before, err := readTree(filepath.Join(project.Root, "bindings"))
			if err != nil {
				t.Fatal(err)
			}
			run("update")
			after, err := readTree(filepath.Join(project.Root, "bindings"))
			if err != nil || !equalFileTrees(before, after) {
				t.Fatal("second update changed generated bytes", err)
			}
		})
	}
}

func equalFileTrees(a, b map[string][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for name, content := range a {
		if other, ok := b[name]; !ok || !bytes.Equal(content, other) {
			return false
		}
	}
	return true
}

func TestFutureBranchConstraintsAndCompactPackages(t *testing.T) {
	if os.Getenv("RC_GO_PROFILER_BIN") == "" || os.Getenv("RC_PYTHON_PROFILER_ARTIFACT") == "" {
		t.Skip("set independent profiler artifact paths")
	}
	for _, language := range []string{"go", "python"} {
		t.Run(language, func(t *testing.T) {
			project, options := testProject(t, language)
			path := filepath.Join(project.Root, "catalog", extensionName)
			extension, err := readMapping(path)
			if err != nil {
				t.Fatal(err)
			}
			spec := mapValue(extension["spec"])
			spec["interfaceFields"] = append(sequence(spec["interfaceFields"]), map[string]any{"name": "mode", "targetKind": "future.channel", "targetType": "stream"})
			schema := mapValue(mapValue(sequence(spec["schemas"])[0])["schema"])
			object := mapValue(mapValue(schema["properties"])["interface"])
			mapValue(object["properties"])["mode"] = map[string]any{"type": "string"}
			object["required"] = append(sequence(object["required"]), "mode")
			object["oneOf"] = []any{
				map[string]any{"properties": map[string]any{"mode": map[string]any{"const": "compact"}, "address": map[string]any{"type": "string", "maxLength": 2}}},
				map[string]any{"properties": map[string]any{"mode": map[string]any{"const": "expanded"}, "address": map[string]any{"type": "string", "minLength": 4}}},
			}
			data, _ := yaml.Marshal(extension)
			if err = os.WriteFile(path, data, 0644); err != nil {
				t.Fatal(err)
			}
			pipeline, err := NewPipeline(context.Background(), project, options)
			if err != nil {
				t.Fatal(err)
			}
			defer pipeline.Close()
			plan, _ := project.SelectTargets(nil)
			build, err := pipeline.Generate(plan)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = pipeline.Verify(build); err != nil {
				t.Fatal(err)
			}
			assertCompactPackage(t, build.Targets[0])
			for _, test := range []struct {
				mode, address string
				reject        bool
			}{
				{"compact", "a", false}, {"expanded", "abcd", false},
				{"compact", "abc", true}, {"expanded", "abc", true},
			} {
				condition := map[string]any{"kind": "future.channel", "interface": map[string]any{"type": "stream", "address": test.address, "mode": test.mode}}
				if rejected := validateSchema(schema, condition) != nil; rejected != test.reject {
					t.Fatalf("synthetic branch %s/address=%s: rejected=%v", test.mode, test.address, rejected)
				}
			}
		})
	}
}

func TestFutureThreeLevelPackagesWithInstalledProfilers(t *testing.T) {
	if os.Getenv("RC_GO_PROFILER_BIN") == "" || os.Getenv("RC_PYTHON_PROFILER_ARTIFACT") == "" {
		t.Skip("set independent profiler artifact paths")
	}
	for _, language := range []string{"go", "python"} {
		t.Run(language, func(t *testing.T) {
			project, options := testProject(t, language)
			basePath := filepath.Join(project.Root, "catalog", extensionName)
			base, err := readMapping(basePath)
			if err != nil {
				t.Fatal(err)
			}
			mapValue(mapValue(sequence(mapValue(base["spec"])["schemas"])[0])["schema"])["additionalProperties"] = true
			declaringSpec := mapValue(jsonCopy(base["spec"]))
			// The first provider supplies only a schema; the middle package owns the vocabulary.
			base["spec"] = map[string]any{"schemas": []any{map[string]any{"id": "address-policy", "description": "A schema-only dependency constraint.", "schema": map[string]any{"type": "object", "properties": map[string]any{"interface": map[string]any{"type": "object", "properties": map[string]any{"address": map[string]any{"type": "string", "maxLength": 2}}}}}}}}
			data, _ := yaml.Marshal(base)
			if err = os.WriteFile(basePath, data, 0644); err != nil {
				t.Fatal(err)
			}
			previous := project.Catalog.Packages["new-channel"].RootExtension
			for _, name := range []string{"middle", "leaf"} {
				id := "https://new.example.test/provider/future-" + name + ":0.7.0"
				field, shape := "credentials", map[string]any{"type": "string", "minLength": 1}
				if name == "leaf" {
					field, shape = "timeout", map[string]any{"type": "integer", "minimum": 0}
				}
				extension := map[string]any{
					"apiVersion": "runtimeconditions.io/v1alpha1", "kind": "RuntimeConditionsExtensionDefinition",
					"metadata": map[string]any{"uri": "https://new.example.test/provider/future-" + name, "version": "0.7.0"},
					"spec": map[string]any{
						"dependencies":    []string{previous},
						"conditionFields": []any{map[string]any{"name": field, "appliesToKinds": []string{"future.channel"}}},
						"schemas":         []any{map[string]any{"id": name + "-constraint", "description": "A future dependency constraint.", "appliesToKind": "future.channel", "appliesToInterfaceType": "stream", "schema": map[string]any{"type": "object", "required": []string{field}, "properties": map[string]any{field: shape}}}},
					},
				}
				if name == "middle" {
					spec := mapValue(jsonCopy(declaringSpec))
					spec["dependencies"] = []string{previous}
					extension["spec"] = spec
				}
				data, _ = yaml.Marshal(extension)
				if err = os.WriteFile(filepath.Join(project.Root, "catalog", name+".yaml"), data, 0644); err != nil {
					t.Fatal(err)
				}
				set := project.Catalog.Packages["new-channel"]
				target := set.Languages[language]
				target.SourceDirectory = "bindings/" + name + "/" + language
				if language == "go" {
					target.Coordinate, target.Name = "example.test/bindings/"+name, name
				} else {
					target.Coordinate, target.Name = "runtimeconditions-"+name, "runtimeconditions_"+name
				}
				project.Catalog.Packages[name] = PackageSet{RootExtension: id, CoreProfileSchemaVersion: set.CoreProfileSchemaVersion, Languages: map[string]LanguageTarget{language: target}}
				previous = id
			}
			pipeline, err := NewPipeline(context.Background(), project, options)
			if err != nil {
				t.Fatal(err)
			}
			defer pipeline.Close()
			plan, _ := project.SelectTargets([]string{"leaf:" + language})
			build, err := pipeline.Generate(plan)
			if err != nil {
				t.Fatal(err)
			}
			if len(build.Targets) != 3 || build.Targets[0].Target.PackageKey != "new-channel" || build.Targets[1].Target.PackageKey != "middle" || build.Targets[2].Target.PackageKey != "leaf" || build.Targets[0].Target.Selected || build.Targets[1].Target.Selected || !build.Targets[2].Target.Selected {
				t.Fatalf("incorrect provider build order: %+v", build.Plan)
			}
			if _, err = pipeline.Verify(build); err != nil {
				t.Fatal(err)
			}
			for _, bt := range build.Targets {
				assertCompactPackage(t, bt)
			}
			environment, err := pipeline.installBuild(build)
			if err != nil {
				t.Fatal(err)
			}
			middle, leaf := build.Targets[1], build.Targets[2]
			for _, address := range []string{"a", "abc"} {
				source := fmt.Sprintf("import %s as m\nimport %s as l\nm.future_channel(m.Stream(address=%q), l.Timeout(value=0))\n", middle.Target.Config.Name, leaf.Target.Config.Name, address)
				if language == "go" {
					source = fmt.Sprintf("package main\nimport m %q\nimport l %q\nvar _ = m.FutureChannel(m.Stream{Address:%q}, l.Timeout(0))\nfunc main() {}\n", middle.Target.Config.Coordinate, leaf.Target.Config.Coordinate, address)
				}
				condition := map[string]any{"kind": "future.channel", "interface": map[string]any{"type": "stream", "address": address}, "timeout": 0}
				assertSyntheticProfile(t, pipeline, build, environment, source, condition, []string{middle.Target.Set.RootExtension, leaf.Target.Set.RootExtension}, address == "abc")
			}

		})
	}
}
func TestAtomicUpdateRemovesStaleSelectedFiles(t *testing.T) {
	p, o := testProject(t, "go")
	pipeline, err := NewPipeline(context.Background(), p, o)
	if err != nil {
		t.Fatal(err)
	}
	defer pipeline.Close()
	plan, _ := p.SelectTargets(nil)
	target := plan.Targets[0]
	source := t.TempDir()
	_ = os.WriteFile(filepath.Join(source, "new.go"), []byte("package generated\n"), 0644)
	destination := filepath.Join(p.RepositoryRoot, target.Config.SourceDirectory)
	_ = os.MkdirAll(destination, 0755)
	_ = os.WriteFile(filepath.Join(destination, "stale.go"), []byte("stale"), 0644)
	unselected := filepath.Join(p.RepositoryRoot, "bindings", "other", "go")
	_ = os.MkdirAll(unselected, 0755)
	_ = os.WriteFile(filepath.Join(unselected, "keep.go"), []byte("keep"), 0644)
	build := &Build{Pipeline: pipeline, Targets: []*BuiltTarget{{Target: target, Directory: source}}}
	if err = build.Update(); err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(destination, "stale.go")) || !exists(filepath.Join(destination, "new.go")) || !exists(filepath.Join(unselected, "keep.go")) {
		t.Fatal("update affected wrong file set")
	}
}
func TestTemporaryWorkspaceIsLazyAndCleaned(t *testing.T) {
	p, o := testProject(t, "go")
	pipeline, err := NewPipeline(context.Background(), p, o)
	if err != nil {
		t.Fatal(err)
	}
	if pipeline.work != "" {
		t.Fatal("eager working directory")
	}
	root, err := pipeline.workspace()
	if err != nil {
		t.Fatal(err)
	}
	pipeline.Close()
	if _, err = os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("temporary workspace survived")
	}
	o.WorkDir = t.TempDir()
	pipeline, _ = NewPipeline(context.Background(), p, o)
	root, _ = pipeline.workspace()
	pipeline.Close()
	if _, err = os.Stat(root); err != nil {
		t.Fatal("explicit workspace not retained")
	}
}
func TestFutureExtensionInstalledPipeline(t *testing.T) {
	if os.Getenv("RC_GO_PROFILER_BIN") == "" {
		t.Skip("set RC_GO_PROFILER_BIN for installed independent Go profiler verification")
	}
	for _, language := range []string{"go", "python"} {
		t.Run(language, func(t *testing.T) {
			if language == "python" && (os.Getenv("RC_BINDINGS_PYTHON") == "" || os.Getenv("RC_PYTHON_PROFILER_ARTIFACT") == "") {
				t.Skip("set Python interpreter and independent profiler artifact")
			}
			project, options := testProject(t, language)
			pipeline, err := NewPipeline(context.Background(), project, options)
			if err != nil {
				t.Fatal(err)
			}
			defer pipeline.Close()
			plan, _ := project.SelectTargets(nil)
			build, err := pipeline.Generate(plan)
			if err != nil {
				t.Fatal(err)
			}
			summary, err := pipeline.Verify(build)
			if err != nil {
				t.Fatal(err)
			}
			if summary.Status != "passed" || summary.Scope != "package-structure" || len(summary.Targets[0].Gates) != 16 {
				t.Fatalf("incomplete verification %+v", summary)
			}
			for _, gate := range summary.Targets[0].Gates {
				if gate.Status != packageGateStatus(gate.Gate) {
					t.Fatalf("misreported gate: %+v", gate)
				}
			}
			bt := build.Targets[0]
			assertCompactPackage(t, bt)
			environment, err := pipeline.installBuild(build)
			if err != nil {
				t.Fatal(err)
			}
			source := fmt.Sprintf("import %s as b\nb.future_channel(b.Stream(address=\"a\"))\n", bt.Target.Config.Name)
			if language == "go" {
				source = fmt.Sprintf("package main\nimport b %q\nvar _ = b.FutureChannel(b.Stream{Address:\"a\"})\nfunc main() {}\n", bt.Target.Config.Coordinate)
			}
			condition := map[string]any{"kind": "future.channel", "interface": map[string]any{"type": "stream", "address": "a"}}
			assertSyntheticProfile(t, pipeline, build, environment, source, condition, []string{bt.Target.Set.RootExtension}, false)
			if err = build.Update(); err != nil {
				t.Fatal(err)
			}
			committed, err := pipeline.LoadBuild(plan, "")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = pipeline.Verify(committed); err != nil {
				t.Fatal(err)
			}
			output := filepath.Join(project.Root, "dist")
			if err = pipeline.Package(committed, output); err != nil {
				t.Fatal(err)
			}
			if !exists(filepath.Join(output, fileManifestName)) {
				t.Fatal("final artifact manifest absent")
			}
		})
	}
}

func assertCompactPackage(t *testing.T, bt *BuiltTarget) {
	t.Helper()
	files, err := readTree(bt.Directory)
	if err != nil {
		t.Fatal(err)
	}
	for name := range files {
		if strings.HasPrefix(name, "conformance/") || filepath.Base(name) == "_conformance.py" {
			t.Fatalf("package contains test artifacts: %s", name)
		}
	}
	expected := 7
	if bt.Target.Language == "python" {
		expected = 10
	}
	if len(files) != expected {
		t.Fatalf("package has %d files, want %d", len(files), expected)
	}
}

// Six authored synthetic consumers check the installed CLI boundary across both
// languages. Their source and expected profiles remain temporary test inputs.
func assertSyntheticProfile(t *testing.T, p *Pipeline, build *Build, environment *nativeEnvironment, source string, condition map[string]any, contributors []string, reject bool) {
	t.Helper()
	bt := build.Targets[len(build.Targets)-1]
	directory := t.TempDir()
	profilePath := filepath.Join(directory, "profile.yaml")
	var err error
	if bt.Target.Language == "go" {
		if err = os.WriteFile(filepath.Join(directory, "main.go"), []byte(source), 0644); err != nil {
			t.Fatal(err)
		}
		module := "module example.test/synthetic-consumer\n\ngo " + majorMinor(bt.Target.Config.LanguageVersion) + "\n\nrequire (\n"
		for _, provider := range build.Targets {
			module += provider.Target.Config.Coordinate + " v" + provider.Target.Config.Version + "\n"
		}
		module += ")\n"
		if err = os.WriteFile(filepath.Join(directory, "go.mod"), []byte(module), 0644); err != nil {
			t.Fatal(err)
		}
		if _, err = p.run(directory, environment.GoEnv, p.Options.Go, "mod", "download", "all"); err != nil {
			t.Fatal(err)
		}
		if _, err = p.run(directory, environment.GoEnv, p.Options.Go, "build", "-o", filepath.Join(directory, "consumer"), "."); err != nil {
			t.Fatal(err)
		}
		tool, _, toolErr := p.tool("go-profiler")
		if toolErr != nil {
			t.Fatal(toolErr)
		}
		env := map[string]string{}
		for key, value := range environment.GoEnv {
			env[key] = value
		}
		env["GOPROXY"] = "off"
		env["PATH"] = filepath.Dir(p.Options.Go) + string(os.PathListSeparator) + os.Getenv("PATH")
		_, err = p.run(directory, env, tool, "generate", "-dir", directory, "-name", "synthetic", "-workload-uri", "https://example.test/synthetic", "-workload-version", "1.0.0", "-out", profilePath)
	} else {
		if err = os.WriteFile(filepath.Join(directory, "app.py"), []byte(source), 0644); err != nil {
			t.Fatal(err)
		}
		profiler := filepath.Join(filepath.Dir(environment.Python), "runtimeconditions-python-profiler")
		_, err = p.run(directory, environment.PythonEnv, profiler, "profile", "generate", "--project", directory, "--name", "synthetic", "--workload-uri", "https://example.test/synthetic", "--workload-version", "1.0.0", "--out", profilePath)
	}
	if reject {
		if err == nil || exists(profilePath) {
			t.Fatal("synthetic negative consumer was accepted")
		}
		message := err.Error()
		if !strings.Contains(message, "schema") && !strings.Contains(message, "field-domain:") && !strings.Contains(message, "outside domain") {
			t.Fatalf("negative failed before semantic validation: %v", err)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	actual, err := readMapping(profilePath)
	if err != nil {
		t.Fatal(err)
	}
	extensions := append([]string(nil), contributors...)
	sort.Strings(extensions)
	expected := map[string]any{"apiVersion": "runtimeconditions.io/v1alpha1", "kind": "RuntimeConditionsProfile", "metadata": map[string]any{"name": "synthetic"}, "workload": map[string]any{"uri": "https://example.test/synthetic", "version": "1.0.0"}, "extensions": extensions, "conditions": []any{condition}}
	left, err := normalizer.CanonicalJSON(actual)
	if err != nil {
		t.Fatal(err)
	}
	right, err := normalizer.CanonicalJSON(expected)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(left, right) {
		t.Fatalf("synthetic profile differs: actual=%s expected=%s", left, right)
	}
	core, _, err := p.core(bt.Document.CoreVersion)
	if err != nil {
		t.Fatal(err)
	}
	if err = validateSchema(core, expected); err != nil {
		t.Fatal(err)
	}
}
