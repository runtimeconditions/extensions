package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunEmitsPackage(t *testing.T) {
	output := filepath.Join(t.TempDir(), "output")
	err := run([]string{
		"--model", filepath.Join("..", "..", "..", "..", "model", "conformance", "expected", "01-owned-kind-interface", "runtimeconditions.binding-model.yaml"),
		"--package-config", filepath.Join("..", "..", "testdata", "package-targets", "01-owned-kind-interface.yaml"),
		"--output", output,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"bindings.go",
		"go.mod",
		filepath.Join("conformance", "conformance_test.go"),
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
