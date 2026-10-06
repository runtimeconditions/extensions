package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestRunEmitsPackage(t *testing.T) {
	output := filepath.Join(t.TempDir(), "output")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	tool, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	config, err := os.ReadFile(filepath.Join("..", "..", "testdata", "package-targets", "01-owned-kind-interface.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "package-target.yaml")
	if err := os.WriteFile(configPath, append(config, []byte(fmt.Sprintf("\nemitterSha256: %x\n", sha256.Sum256(tool)))...), 0600); err != nil {
		t.Fatal(err)
	}
	err = run([]string{
		"--model", filepath.Join("..", "..", "..", "..", "model", "conformance", "expected", "01-owned-kind-interface", "runtimeconditions.binding-model.yaml"),
		"--package-config", configPath,
		"--output", output,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(output, "conformance")); !os.IsNotExist(err) {
		t.Fatal("production CLI emitted conformance files")
	}
	for _, path := range []string{
		"bindings.go",
		"go.mod",
		"runtimeconditions.bindings.yaml",
	} {
		if _, err := os.Stat(filepath.Join(output, path)); err != nil {
			t.Errorf("generated %s: %v", path, err)
		}
	}
}

func TestRunRequiresContractFlags(t *testing.T) {
	if err := run(nil); err == nil {
		t.Fatal("expected missing required flags to fail")
	}
}
