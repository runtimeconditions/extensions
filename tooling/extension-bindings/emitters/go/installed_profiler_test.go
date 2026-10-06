package goemitter

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/runtimeconditions/extensions/tooling/extension-bindings/normalizer"
	"gopkg.in/yaml.v3"
)

// This integration test owns the fixture and the local Go proxy. The profiler
// receives only a workload module and a dependency already fetched by Go.
// Set RC_GO_PROFILER_BIN to an installed go-rc-profiler executable to run it.
func TestInstalledGoProfilerBindingPackage(t *testing.T) {
	profiler := os.Getenv("RC_GO_PROFILER_BIN")
	if profiler == "" {
		t.Skip("set RC_GO_PROFILER_BIN to an installed profiler binary")
	}
	profiler, err := filepath.Abs(profiler)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(profiler); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name         string
		alterRoot    bool
		alterRelease bool
		wantError    string
	}{
		{name: "complete package"},
		{name: "altered packaged extension bytes", alterRoot: true, wantError: "sourceSha256 differs"},
		{name: "wrong release package version", alterRelease: true, wantError: "binding release package.version does not match"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			packageDir := filepath.Join(root, "assembled-package")
			target := assembleOwnedGoFixture(t, packageDir, profiler)
			if test.alterRoot {
				path := filepath.Join(packageDir, "runtimeconditions.extension.yaml")
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if test.alterRelease {
				path := filepath.Join(packageDir, "runtimeconditions.binding-release.yaml")
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var release map[string]any
				if err := yaml.Unmarshal(data, &release); err != nil {
					t.Fatal(err)
				}
				release["package"].(map[string]any)["version"] = "v9.9.9"
				data, err = yaml.Marshal(release)
				if err != nil {
					t.Fatal(err)
				}
				writeFixtureFile(t, path, data)
			}
			proxy := filepath.Join(root, "proxy")
			writeFixtureModuleProxy(t, proxy, target, packageDir)
			workload := filepath.Join(root, "workload")
			copyFixtureWorkload(t, workload)
			moduleCache := filepath.Join(root, "module-cache")
			t.Cleanup(func() {
				_ = filepath.Walk(moduleCache, func(path string, info os.FileInfo, err error) error {
					if err == nil {
						_ = os.Chmod(path, 0o755)
					}
					return nil
				})
			})
			env := append(os.Environ(),
				"GOWORK=off", "GOSUMDB=off", "GOMODCACHE="+moduleCache,
				"GOCACHE="+filepath.Join(root, "build-cache"),
				"GOPROXY=file://"+filepath.ToSlash(proxy),
			)
			runFixtureCommand(t, workload, env, "go", "mod", "download", "all")
			offline := append(env, "GOPROXY=off")
			runFixtureCommand(t, workload, offline, "go", "mod", "verify")
			listed := runFixtureCommand(t, workload, offline, "go", "list", "-json", target.ModulePath)
			var resolved struct {
				Dir    string
				Module struct{ Version string }
			}
			if err := json.Unmarshal([]byte(listed), &resolved); err != nil {
				t.Fatal(err)
			}
			if resolved.Dir == "" || resolved.Module.Version != target.Version || !strings.HasPrefix(resolved.Dir, moduleCache+string(filepath.Separator)) {
				t.Fatalf("binding was not resolved from the downloaded Go module: %+v", resolved)
			}
			for _, name := range []string{"runtimeconditions.bindings.yaml", "runtimeconditions.binding-model.yaml", "runtimeconditions.extension.yaml", "runtimeconditions.binding-release.yaml"} {
				if _, err := os.Stat(filepath.Join(resolved.Dir, name)); err != nil {
					t.Fatalf("%s missing from resolved Go package: %v", name, err)
				}
			}
			command := exec.CommandContext(context.Background(), profiler, "validate-extension", "-dir", workload, "-package", target.ModulePath)
			command.Dir = workload
			command.Env = offline
			output, err := command.CombinedOutput()
			if test.wantError == "" {
				if err != nil {
					t.Fatalf("installed profiler rejected complete package: %v\n%s", err, output)
				}
			} else if err == nil || !strings.Contains(string(output), test.wantError) {
				t.Fatalf("expected %q from installed profiler, got %v\n%s", test.wantError, err, output)
			}
		})
	}
}

func assembleOwnedGoFixture(t *testing.T, output, profiler string) PackageTarget {
	t.Helper()
	const fixture = "01-owned-kind-interface"
	model := loadExpectedModel(t, fixture)
	target := loadTestTarget(t, fixture+".yaml")
	if err := Emit(model, target, output); err != nil {
		t.Fatal(err)
	}
	modelPath := expectedModelPath(fixture)
	modelBytes, err := os.ReadFile(modelPath)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := normalizer.CanonicalModelYAML(model)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(modelBytes, canonical) {
		t.Fatal("emitter model differs from exact checkpoint bytes")
	}
	schemas, err := normalizer.LoadSchemas(
		filepath.Join("..", "..", "model", "runtimeconditions.extension-semantic.schema.yaml"),
		filepath.Join("..", "..", "model", "runtimeconditions.binding-model.schema.yaml"),
	)
	if err != nil {
		t.Fatal(err)
	}
	caseDir := filepath.Join("..", "..", "model", "conformance", "cases", fixture)
	resolver, err := normalizer.NewResolver(normalizer.ResolverConfig{Schemas: schemas, CatalogRoots: []string{caseDir}})
	if err != nil {
		t.Fatal(err)
	}
	closure, err := resolver.Resolve(context.Background(), target.RootExtension)
	if err != nil {
		t.Fatal(err)
	}
	lock := normalizer.BuildDependencyLock(closure)
	if err := normalizer.ValidateDependencyLock(closure, lock); err != nil {
		t.Fatal(err)
	}
	if len(lock.Extensions) != 1 || len(model.Extensions) != 1 || lock.Extensions[0].ID != model.Extensions[0].ID ||
		lock.Extensions[0].SemanticSHA256 != model.Extensions[0].SemanticSHA256 {
		t.Fatal("fixture dependency lock differs from model closure")
	}
	root := closure.ByID[target.RootExtension]
	if root.SemanticSHA256 != model.RootExtension.SemanticSHA256 {
		t.Fatal("root extension digest differs from model")
	}
	writeFixtureFile(t, filepath.Join(output, "runtimeconditions.binding-model.yaml"), modelBytes)
	writeFixtureFile(t, filepath.Join(output, "runtimeconditions.extension.yaml"), root.Bytes)
	assemblerBinary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	release := map[string]any{
		"apiVersion": "runtimeconditions.io/binding-release/v1alpha1",
		"kind":       "RuntimeConditionsBindingRelease",
		"package": map[string]any{
			"packageKey": target.PackageKey, "language": "go", "coordinate": target.ModulePath,
			"name": target.PackageName, "version": target.Version,
			"minimumGoVersion": target.MinimumGoVersion, "publicationMode": target.PublicationMode,
		},
		"model":               map[string]any{"apiVersion": model.APIVersion, "semanticSha256": model.Metadata.SemanticSHA256},
		"rootExtension":       map[string]any{"id": root.Definition.Metadata.URI + ":" + root.Definition.Metadata.Version, "version": root.Definition.Metadata.Version, "semanticSha256": root.SemanticSHA256},
		"dependencyLock":      lock,
		"packageDependencies": []any{},
		"provenance": map[string]any{
			"mode":             "test-fixture",
			"fixtureAssembler": map[string]any{"name": "rc-go-binding-fixture-test", "version": fixtureBuildVersion(t, assemblerBinary), "sha256": fixtureSHA256(t, assemblerBinary)},
			"profiler":         map[string]any{"name": "go-rc-profiler", "version": fixtureBuildVersion(t, profiler), "sha256": fixtureSHA256(t, profiler)},
		},
	}
	releaseData, err := yaml.Marshal(release)
	if err != nil {
		t.Fatal(err)
	}
	releasePath := filepath.Join(output, "runtimeconditions.binding-release.yaml")
	writeFixtureFile(t, releasePath, releaseData)
	validateYAMLDocument(t, filepath.Join("..", "..", "model", "runtimeconditions.binding-release.schema.yaml"), releasePath)
	return target
}

func fixtureSHA256(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func fixtureBuildVersion(t *testing.T, path string) string {
	t.Helper()
	info, err := buildinfo.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Main.Version == "" {
		t.Fatalf("%s has no Go build version", path)
	}
	return info.Main.Version
}

func writeFixtureFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeFixtureModuleProxy(t *testing.T, proxy string, target PackageTarget, packageDir string) {
	t.Helper()
	versionDir := filepath.Join(proxy, filepath.FromSlash(target.ModulePath), "@v")
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	archiveFile, err := os.Create(filepath.Join(versionDir, target.Version+".zip"))
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(archiveFile)
	var names []string
	if err := filepath.Walk(packageDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			name, err := filepath.Rel(packageDir, path)
			if err != nil {
				return err
			}
			names = append(names, name)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sort.Strings(names)
	for _, name := range names {
		writer, err := archive.Create(target.ModulePath + "@" + target.Version + "/" + filepath.ToSlash(name))
		if err != nil {
			t.Fatal(err)
		}
		file, err := os.Open(filepath.Join(packageDir, name))
		if err != nil {
			t.Fatal(err)
		}
		_, copyErr := io.Copy(writer, file)
		closeErr := file.Close()
		if copyErr != nil {
			t.Fatal(copyErr)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := archiveFile.Close(); err != nil {
		t.Fatal(err)
	}
	mod, err := os.ReadFile(filepath.Join(packageDir, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	writeFixtureFile(t, filepath.Join(versionDir, target.Version+".mod"), mod)
	info, err := json.Marshal(map[string]string{"Version": target.Version, "Time": "2026-01-01T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	writeFixtureFile(t, filepath.Join(versionDir, target.Version+".info"), info)
}

func copyFixtureWorkload(t *testing.T, target string) {
	t.Helper()
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"go.mod", "main.go"} {
		data, err := os.ReadFile(filepath.Join("testdata", "installed-profiler-workload", name))
		if err != nil {
			t.Fatal(err)
		}
		writeFixtureFile(t, filepath.Join(target, name), data)
	}
}

func runFixtureCommand(t *testing.T, dir string, env []string, name string, args ...string) string {
	t.Helper()
	command := exec.Command(name, args...)
	command.Dir, command.Env = dir, env
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatal(fmt.Errorf("%s %s: %w\n%s", name, strings.Join(args, " "), err, output))
	}
	return string(output)
}
