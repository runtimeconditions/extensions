package orchestrator

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/runtimeconditions/extensions/tooling/extension-bindings/normalizer"
	"gopkg.in/yaml.v3"
)

type FileManifest struct {
	APIVersion string            `yaml:"apiVersion" json:"apiVersion"`
	Kind       string            `yaml:"kind" json:"kind"`
	Files      map[string]string `yaml:"files" json:"files"`
}

func readTree(root string) (map[string][]byte, error) {
	if err := rejectSymlinkPath(root); err != nil {
		return nil, err
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("not a directory: %s", root)
	}
	result := map[string][]byte{}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("non-regular generated file: %s", path)
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if strings.ContainsAny(relative, "\\:\x00") {
			return fmt.Errorf("unsafe generated path %s", relative)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		result[relative] = data
		return nil
	})
	return result, err
}
func writeFileManifest(root string) error {
	tree, err := readTree(root)
	if err != nil {
		return err
	}
	manifest := FileManifest{APIVersion: "runtimeconditions.io/file-manifest/v1alpha1", Kind: "RuntimeConditionsFileManifest", Files: map[string]string{}}
	for name, data := range tree {
		if name != fileManifestName {
			manifest.Files[name] = normalizer.SHA256Hex(data)
		}
	}
	data, err := yaml.Marshal(manifest)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, fileManifestName), data, 0644)
}
func verifyFileManifest(root, schemaPath string) error {
	var manifest FileManifest
	if err := decodeSchemaFile(filepath.Join(root, fileManifestName), schemaPath, &manifest); err != nil {
		return err
	}
	tree, err := readTree(root)
	if err != nil {
		return err
	}
	delete(tree, fileManifestName)
	if len(tree) != len(manifest.Files) {
		return fmt.Errorf("file manifest inventory differs: actual %d, declared %d", len(tree), len(manifest.Files))
	}
	for name, data := range tree {
		if manifest.Files[name] != normalizer.SHA256Hex(data) {
			return fmt.Errorf("file manifest digest differs: %s", name)
		}
	}
	return nil
}
func compareTrees(left, right string) error {
	a, err := readTree(left)
	if err != nil {
		return err
	}
	b, err := readTree(right)
	if err != nil {
		return err
	}
	names := map[string]bool{}
	for name := range a {
		names[name] = true
	}
	for name := range b {
		names[name] = true
	}
	for _, name := range sortedKeys(names) {
		av, aok := a[name]
		bv, bok := b[name]
		if !aok {
			return fmt.Errorf("stale file %s", name)
		}
		if !bok {
			return fmt.Errorf("missing file %s", name)
		}
		if !bytes.Equal(av, bv) {
			return fmt.Errorf("file bytes differ: %s", name)
		}
	}
	return nil
}
func copyTree(source, destination string) error {
	files, err := readTree(source)
	if err != nil {
		return err
	}
	if err = rejectSymlinkPath(destination); err != nil {
		return err
	}
	for _, name := range sortedKeys(files) {
		path := filepath.Join(destination, filepath.FromSlash(name))
		if err = os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
		if err = writeNewFile(path, files[name]); err != nil {
			return err
		}
	}
	return nil
}

// Update stages all selected trees first and rolls back replacements on failure.
func (b *Build) Update() error {
	type replacement struct {
		destination, stage, backup string
		hadOriginal, installed     bool
		preserveBackup             bool
	}
	replacements := []*replacement{}
	cleanup := func() {
		for _, entry := range replacements {
			_ = os.RemoveAll(entry.stage)
			if !entry.preserveBackup {
				_ = os.RemoveAll(entry.backup)
			}
		}
	}
	defer cleanup()
	for _, target := range b.selected() {
		destination := filepath.Join(b.Pipeline.Project.RepositoryRoot, filepath.FromSlash(target.Target.Config.SourceDirectory))
		if err := rejectSymlinkPath(destination); err != nil {
			return err
		}
		if compareTrees(target.Directory, destination) == nil {
			continue
		}
		parent := filepath.Dir(destination)
		if err := os.MkdirAll(parent, 0755); err != nil {
			return err
		}
		stage, err := os.MkdirTemp(parent, ".rc-bindings-stage-")
		if err != nil {
			return err
		}
		entry := &replacement{destination: destination, stage: stage, backup: stage + "-previous"}
		replacements = append(replacements, entry)
		if err = copyTree(target.Directory, stage); err != nil {
			return err
		}
	}
	rollback := func(cause error) error {
		failures := []error{cause}
		for i := len(replacements) - 1; i >= 0; i-- {
			entry := replacements[i]
			if entry.installed {
				if err := os.RemoveAll(entry.destination); err != nil {
					entry.preserveBackup = true
					failures = append(failures, fmt.Errorf("rollback failed; original retained at %s: %w", entry.backup, err))
					continue
				}
			}
			if entry.hadOriginal {
				if err := os.Rename(entry.backup, entry.destination); err != nil {
					entry.preserveBackup = true
					failures = append(failures, fmt.Errorf("rollback failed; original retained at %s: %w", entry.backup, err))
				}
			}
		}
		return errors.Join(failures...)
	}
	for _, entry := range replacements {
		if _, err := os.Lstat(entry.destination); err == nil {
			if err = os.Rename(entry.destination, entry.backup); err != nil {
				return rollback(err)
			}
			entry.hadOriginal = true
		} else if !os.IsNotExist(err) {
			return rollback(err)
		}
		if err := os.Rename(entry.stage, entry.destination); err != nil {
			return rollback(err)
		}
		entry.installed = true
	}
	return nil
}
func deterministicZip(files map[string][]byte, prefix string) ([]byte, error) {
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, name := range sortedKeys(files) {
		header := &zip.FileHeader{Name: prefix + name, Method: zip.Deflate}
		header.SetModTime(time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC))
		header.SetMode(0644)
		stream, err := writer.CreateHeader(header)
		if err != nil {
			return nil, err
		}
		if _, err = stream.Write(files[name]); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
func verifyZip(data []byte, files map[string][]byte, prefix string) error {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	if len(reader.File) != len(files) {
		return fmt.Errorf("archive inventory differs")
	}
	seen := map[string]bool{}
	for _, entry := range reader.File {
		if !strings.HasPrefix(entry.Name, prefix) {
			return fmt.Errorf("archive path is outside its declared prefix")
		}
		name := strings.TrimPrefix(entry.Name, prefix)
		expected, ok := files[name]
		if !ok || seen[name] || !entry.Mode().IsRegular() {
			return fmt.Errorf("undeclared or duplicate archive entry %s", entry.Name)
		}
		seen[name] = true
		stream, err := entry.Open()
		if err != nil {
			return err
		}
		actual, err := io.ReadAll(stream)
		_ = stream.Close()
		if err != nil {
			return err
		}
		if !bytes.Equal(expected, actual) {
			return fmt.Errorf("archive bytes differ: %s", name)
		}
	}
	return nil
}
func (p *Pipeline) nativeArchives(bt *BuiltTarget) (map[string]string, error) {
	output, err := p.workspacePath("archives", bt.Target.PackageKey, bt.Target.Language)
	if err != nil {
		return nil, err
	}
	if err = os.RemoveAll(output); err != nil {
		return nil, err
	}
	if err = os.MkdirAll(output, 0755); err != nil {
		return nil, err
	}
	if bt.Target.Language == "go" {
		files, err := readTree(bt.Directory)
		if err != nil {
			return nil, err
		}
		version := "v" + bt.Target.Config.Version
		prefix := bt.Target.Config.Coordinate + "@" + version + "/"
		data, err := deterministicZip(files, prefix)
		if err != nil {
			return nil, err
		}
		if err = verifyZip(data, files, prefix); err != nil {
			return nil, err
		}
		path := filepath.Join(output, version+".zip")
		if err = os.WriteFile(path, data, 0644); err != nil {
			return nil, err
		}
		return map[string]string{"go-module-zip": path}, nil
	}
	model, target, err := p.emitterInputs(bt)
	if err != nil {
		return nil, err
	}
	lock := p.Project.Toolchain
	// Development permits workspace tools, but records and enforces their actual
	// native build versions rather than pretending to use the released lock.
	if lock.Status != "released" {
		data, err := p.run(p.Project.Root, p.pythonEnv(), p.Options.Python, "-c", "import importlib.metadata as m,json,platform;print(json.dumps({'version':platform.python_version(),'tools':{n:m.version(n) for n in ('pip','setuptools','build','wheel','packaging','pyproject_hooks')}}))")
		if err != nil {
			return nil, err
		}
		var actual struct {
			Version string            `json:"version"`
			Tools   map[string]string `json:"tools"`
		}
		if err = json.Unmarshal(data, &actual); err != nil {
			return nil, err
		}
		lock.Python.InterpreterVersion = actual.Version
		lock.Python.Tools = actual.Tools
	} else if native, ok := lock.Languages["python"]; ok {
		lock.Python.InterpreterVersion = native.LanguageVersion
		lock.Python.Tools = native.Tools
	}
	lockPath, err := p.workspacePath("emitter-inputs", bt.Target.PackageKey, bt.Target.Language, "build-toolchain.yaml")
	if err != nil {
		return nil, err
	}
	data, err := yaml.Marshal(lock)
	if err != nil {
		return nil, err
	}
	if err = os.WriteFile(lockPath, data, 0644); err != nil {
		return nil, err
	}
	result, err := p.python("archive", "--model", model, "--package-config", target, "--input", bt.Directory, "--output", output, "--toolchain-lock", lockPath, "--build-python", p.Options.Python)
	if err != nil {
		return nil, err
	}
	var artifacts map[string]string
	if err = json.Unmarshal(result, &artifacts); err != nil {
		return nil, err
	}
	return map[string]string{"python-wheel": artifacts["wheel"], "python-sdist": artifacts["sdist"]}, nil
}
func (p *Pipeline) Package(build *Build, output string) error {
	if within(filepath.Join(p.Project.RepositoryRoot, "bindings"), output) {
		return fmt.Errorf("package output must be outside committed bindings")
	}
	if err := prepareEmptyDirectory(output); err != nil {
		return err
	}
	// Finish every artifact before retaining any final output.
	final := map[string][]byte{}
	for _, bt := range build.selected() {
		artifacts, err := p.nativeArchives(bt)
		if err != nil {
			return err
		}
		stem := bt.Target.PackageKey + "-" + bt.Target.Language + "-" + bt.Target.Config.Version
		for kind, path := range artifacts {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			name := stem + ".module.zip"
			if kind != "go-module-zip" {
				name = filepath.Base(path)
			}
			if _, ok := final[name]; ok {
				return fmt.Errorf("duplicate final artifact name %s", name)
			}
			final[name] = data
		}
		tree, err := readTree(bt.Directory)
		if err != nil {
			return err
		}
		data, err := deterministicZip(tree, bt.Target.Config.SourceDirectory+"/")
		if err != nil {
			return err
		}
		if err = verifyZip(data, tree, bt.Target.Config.SourceDirectory+"/"); err != nil {
			return err
		}
		final[stem+".source.zip"] = data
		for _, name := range []string{modelName, releaseName, bindingName} {
			data, err := os.ReadFile(filepath.Join(resourceDir(bt), name))
			if err != nil {
				return err
			}
			final[stem+"."+name] = data
		}
		data, err = os.ReadFile(filepath.Join(bt.Directory, fileManifestName))
		if err != nil {
			return err
		}
		final[stem+"."+fileManifestName] = data
	}
	checksums := map[string]string{}
	for name, data := range final {
		checksums[name] = normalizer.SHA256Hex(data)
	}
	manifest, err := yaml.Marshal(map[string]any{"apiVersion": "runtimeconditions.io/file-manifest/v1alpha1", "kind": "RuntimeConditionsFileManifest", "files": checksums})
	if err != nil {
		return err
	}
	final[fileManifestName] = manifest
	for _, name := range sortedKeys(final) {
		if err = writeNewFile(filepath.Join(output, name), final[name]); err != nil {
			return err
		}
	}
	return nil
}
