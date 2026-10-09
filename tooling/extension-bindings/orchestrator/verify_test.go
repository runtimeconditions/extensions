package orchestrator

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
