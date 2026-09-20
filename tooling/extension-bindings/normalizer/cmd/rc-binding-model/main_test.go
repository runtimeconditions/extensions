package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunWritesRepeatableModelAndSeparateLock(t *testing.T) {
	temporary := t.TempDir()
	modelPath := filepath.Join(temporary, "model.yaml")
	lockPath := filepath.Join(temporary, "lock.yaml")
	caseRoot := filepath.Join("..", "..", "..", "model", "conformance", "cases", "01-owned-kind-interface")
	common := []string{
		"--root", "urn:runtimeconditions:conformance:owned-kind-interface",
		"--extension-root", caseRoot,
		"--semantic-schema", filepath.Join("..", "..", "..", "model", "runtimeconditions.extension-semantic.schema.yaml"),
		"--model-schema", filepath.Join("..", "..", "..", "model", "runtimeconditions.binding-model.schema.yaml"),
		"--core-profile-id", "urn:runtimeconditions:test:core",
		"--core-profile-version", "1.0.0",
		"--core-profile-semantic-sha256", strings.Repeat("c", 64),
		"--normalizer-sha256", strings.Repeat("d", 64),
	}
	arguments := append(append([]string{}, common...), "--output", modelPath, "--dependency-lock-output", lockPath)
	if err := run(arguments); err != nil {
		t.Fatal(err)
	}
	model, err := os.ReadFile(modelPath)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(model, []byte("sourceSha256")) || !bytes.Contains(lock, []byte("sourceSha256")) {
		t.Fatal("source identity was not isolated to the dependency lock")
	}

	secondModelPath := filepath.Join(temporary, "second-model.yaml")
	secondArguments := append(append([]string{}, common...), "--dependency-lock", lockPath, "--output", secondModelPath)
	if err := run(secondArguments); err != nil {
		t.Fatal(err)
	}
	secondModel, err := os.ReadFile(secondModelPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(model, secondModel) {
		t.Fatal("reusing the dependency lock changed model bytes")
	}
}
