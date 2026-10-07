package orchestrator

import (
	"context"
	"debug/buildinfo"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	goemitter "github.com/runtimeconditions/extensions/tooling/extension-bindings/emitters/go"
	"github.com/runtimeconditions/extensions/tooling/extension-bindings/normalizer"
	"gopkg.in/yaml.v3"
)

type Document struct {
	Closure     normalizer.ResolvedClosure
	Lock        normalizer.DependencyLock
	Model       normalizer.BindingModel
	CoreVersion string
}

func (d *Document) SemanticClosure() map[string]any {
	definitions := []any{}
	for _, doc := range d.Closure.Documents {
		definitions = append(definitions, doc.SemanticData)
	}
	return map[string]any{"rootExtension": d.Closure.Root, "extensions": definitions}
}
func (d *Document) LockYAML() ([]byte, error)  { return normalizer.CanonicalDependencyLockYAML(d.Lock) }
func (d *Document) ModelYAML() ([]byte, error) { return normalizer.CanonicalModelYAML(d.Model) }

type Pipeline struct {
	Context    context.Context
	Project    *Project
	Options    Options
	HTTPClient *http.Client
	work       string
	temporary  bool
	tools      map[string]string
	identities map[string]Tool
	cores      map[string]map[string]any
}
type BuiltTarget struct {
	Target        Target
	Document      *Document
	Directory     string
	PackageConfig map[string]any
	Manifest      map[string]any
	Artifacts     map[string]string
}
type Build struct {
	Pipeline *Pipeline
	Plan     BuildPlan
	Targets  []*BuiltTarget
}

func NewPipeline(ctx context.Context, p *Project, o Options) (*Pipeline, error) {
	if p.Toolchain.Status == "released" && len(o.Overrides) > 0 {
		return nil, fmt.Errorf("released generation forbids extension overrides")
	}
	pipeline := &Pipeline{Context: ctx, Project: p, Options: o, tools: map[string]string{}, identities: map[string]Tool{}, cores: map[string]map[string]any{}, HTTPClient: &http.Client{Timeout: 60 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return fmt.Errorf("redirects are forbidden") }}}
	for _, path := range []string{o.WorkDir, o.Cache} {
		if path != "" {
			if err := rejectSymlinkPath(path); err != nil {
				return nil, err
			}
			if within(filepath.Join(p.RepositoryRoot, "bindings"), path) {
				return nil, fmt.Errorf("work/cache directory must be outside committed bindings")
			}
		}
	}
	return pipeline, nil
}
func within(parent, path string) bool {
	rel, err := filepath.Rel(parent, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
func (p *Pipeline) Close() {
	if p.temporary && p.work != "" {
		_ = removeWorkTree(p.work)
	}
}

// Go's module cache uses read-only directories unless modcacherw is enabled.
// Make only our own workspace directories writable before cleanup.
func removeWorkTree(root string) error {
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return os.Chmod(path, 0755)
		}
		return nil
	}); err != nil {
		return err
	}
	return os.RemoveAll(root)
}
func (p *Pipeline) workspace() (string, error) {
	if p.work != "" {
		return p.work, nil
	}
	var err error
	if p.Options.WorkDir != "" {
		p.work = p.Options.WorkDir
		err = os.MkdirAll(p.work, 0755)
	} else {
		p.work, err = os.MkdirTemp("", "rc-bindings-*")
		p.temporary = true
	}
	if err != nil {
		return "", err
	}
	return p.work, nil
}
func (p *Pipeline) workspacePath(parts ...string) (string, error) {
	root, err := p.workspace()
	if err != nil {
		return "", err
	}
	return filepath.Join(append([]string{root}, parts...)...), nil
}
func (p *Pipeline) run(directory string, env map[string]string, command string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(p.Context, command, args...)
	cmd.Dir = directory
	cmd.Env = os.Environ()
	if p.work != "" {
		if env == nil {
			env = map[string]string{}
		}
		env["TMPDIR"] = p.work
	}
	for key, value := range env {
		prefix := key + "="
		kept := cmd.Env[:0]
		for _, entry := range cmd.Env {
			if !strings.HasPrefix(entry, prefix) {
				kept = append(kept, entry)
			}
		}
		cmd.Env = append(kept, prefix+value)
	}
	data, err := cmd.CombinedOutput()
	if err != nil {
		return data, fmt.Errorf("%s %s: %w\n%s", command, strings.Join(args, " "), err, data)
	}
	return data, nil
}
func (p *Pipeline) pythonEnv() map[string]string {
	env := map[string]string{"PYTHONDONTWRITEBYTECODE": "1", "PYTHONNOUSERSITE": "1", "PYTHONPATH": ""}
	if p.Project.Toolchain.Status != "released" {
		env["PYTHONPATH"] = filepath.Join(p.Project.ToolingDirectory, "emitters", "python", "src")
	}
	return env
}
func (p *Pipeline) python(args ...string) ([]byte, error) {
	flags := []string{"-m", "runtimeconditions_binding_emitter"}
	if p.Project.Toolchain.Status == "released" {
		flags = append([]string{"-B", "-I"}, flags...)
	}
	return p.run(p.Project.Root, p.pythonEnv(), p.Options.Python, append(flags, args...)...)
}
func (p *Pipeline) tool(key string) (string, Tool, error) {
	if path, ok := p.tools[key]; ok {
		return path, p.identities[key], nil
	}
	var path string
	var expected Tool
	switch key {
	case "normalizer":
		path = p.Options.Normalizer
		expected = p.Project.Toolchain.Tools.Normalizer
	case "go-emitter":
		path = p.Options.GoEmitter
		expected = p.Project.Toolchain.Tools.Emitters["go"]
	case "go-profiler":
		path = p.Options.GoProfiler
		expected = p.Project.Toolchain.Tools.Profilers["go"]
	case "orchestrator":
		path, _ = os.Executable()
		expected = p.Project.Toolchain.Tools.Orchestrator
	default:
		return "", Tool{}, fmt.Errorf("unknown tool %s", key)
	}
	if path == "" {
		if p.Project.Toolchain.Status == "released" {
			return "", Tool{}, fmt.Errorf("%s: installed tool path required by released lock", key)
		}
		path, err := p.workspacePath("tools", key)
		if err != nil {
			return "", Tool{}, err
		}
		if err = os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return "", Tool{}, err
		}
		module, command := "normalizer", "rc-binding-model"
		if key == "go-emitter" {
			module, command = "emitters/go", "rc-go-bindings"
		}
		if _, err = p.run(filepath.Join(p.Project.ToolingDirectory, module), nil, p.Options.Go, "build", "-trimpath", "-buildvcs=false", "-o", path, "./cmd/"+command); err != nil {
			return "", Tool{}, err
		}
		p.tools[key] = path
	} else {
		resolved, err := exec.LookPath(path)
		if err != nil {
			return "", Tool{}, fmt.Errorf("%s: %w", key, err)
		}
		path, err = filepath.Abs(resolved)
		if err != nil {
			return "", Tool{}, err
		}
		p.tools[key] = path
	}
	path = p.tools[key]
	data, err := os.ReadFile(path)
	if err != nil {
		return "", Tool{}, err
	}
	identity := Tool{Name: key, Version: Version, SHA256: normalizer.SHA256Hex(data)}
	switch key {
	case "normalizer":
		identity.Name = normalizer.NormalizerName
		identity.Version = normalizer.NormalizerVersion
	case "go-emitter":
		identity.Name = goemitter.EmitterName
		identity.Version = goemitter.EmitterVersion
	case "go-profiler":
		identity.Name = "go-rc-profiler"
		info, err := buildinfo.ReadFile(path)
		if err != nil {
			return "", Tool{}, err
		}
		identity.Version = info.Main.Version
	case "orchestrator":
		identity.Name = "rc"
	}
	if expected.SHA256 != "" {
		if expected.SHA256 != identity.SHA256 {
			return "", Tool{}, fmt.Errorf("%s artifact digest differs from toolchain lock", key)
		}
		if identity.Version != "(devel)" && expected.Version != identity.Version {
			return "", Tool{}, fmt.Errorf("%s version differs from toolchain lock", key)
		}
		identity.Name, identity.Version = expected.Name, expected.Version
	}
	p.identities[key] = identity
	return path, identity, nil
}
func (p *Pipeline) pythonEmitterIdentity() (Tool, error) {
	if identity, ok := p.identities["python-emitter"]; ok {
		return identity, nil
	}
	expected := p.Project.Toolchain.Tools.Emitters["python"]
	var identity Tool
	if p.Project.Toolchain.Status == "released" {
		// Installed Python tooling supplies the digest of its release wheel through an explicit artifact input.
		artifact := os.Getenv("RC_BINDINGS_PYTHON_EMITTER_ARTIFACT")
		if artifact == "" {
			return Tool{}, fmt.Errorf("released Python emitter requires RC_BINDINGS_PYTHON_EMITTER_ARTIFACT")
		}
		data, err := os.ReadFile(artifact)
		if err != nil {
			return Tool{}, err
		}
		identity = Tool{Name: "runtimeconditions-binding-emitter", Version: Version, SHA256: normalizer.SHA256Hex(data)}
		if _, err = p.python("verify-installation", "--artifact", artifact, "--distribution", "runtimeconditions-binding-emitter"); err != nil {
			return Tool{}, err
		}
	} else {
		base := filepath.Join(p.Project.ToolingDirectory, "emitters", "python")
		files := map[string]string{}
		paths := []string{filepath.Join(base, "pyproject.toml")}
		err := filepath.WalkDir(filepath.Join(base, "src", "runtimeconditions_binding_emitter"), func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !entry.IsDir() && strings.HasSuffix(path, ".py") {
				paths = append(paths, path)
			}
			return nil
		})
		if err != nil {
			return Tool{}, err
		}
		for _, path := range paths {
			data, err := os.ReadFile(path)
			if err != nil {
				return Tool{}, err
			}
			relative, _ := filepath.Rel(base, path)
			files[filepath.ToSlash(relative)] = normalizer.SHA256Hex(data)
		}
		data, err := normalizer.CanonicalJSON(files)
		if err != nil {
			return Tool{}, err
		}
		identity = Tool{Name: "runtimeconditions-binding-emitter", Version: Version, SHA256: normalizer.SHA256Hex(data)}
	}
	if expected.SHA256 != "" && (expected.SHA256 != identity.SHA256 || expected.Version != identity.Version) {
		return Tool{}, fmt.Errorf("Python emitter identity differs from lock")
	}
	p.identities["python-emitter"] = identity
	return identity, nil
}
func (p *Pipeline) profiler(language string) (Tool, error) {
	if language == "go" {
		_, identity, err := p.tool("go-profiler")
		return identity, err
	}
	if language != "python" {
		return Tool{}, fmt.Errorf("unsupported native adapter %s", language)
	}
	if identity, ok := p.identities["python-profiler"]; ok {
		return identity, nil
	}
	if p.Options.PythonProfilerArtifact == "" {
		return Tool{}, fmt.Errorf("Python verification requires --python-profiler-artifact identifying the independently installed wheel")
	}
	if _, err := exec.LookPath(p.Options.PythonProfiler); err != nil {
		return Tool{}, err
	}
	data, err := p.python("verify-installation", "--artifact", p.Options.PythonProfilerArtifact, "--distribution", "runtimeconditions-profiler")
	if err != nil {
		return Tool{}, err
	}
	var actual struct {
		Version string `json:"version"`
		SHA256  string `json:"sha256"`
	}
	if err = json.Unmarshal(data, &actual); err != nil {
		return Tool{}, err
	}
	identity := Tool{Name: "python-rc-profiler", Version: actual.Version, SHA256: actual.SHA256}
	expected := p.Project.Toolchain.Tools.Profilers[language]
	if expected.SHA256 != "" && (expected.SHA256 != identity.SHA256 || expected.Version != identity.Version) {
		return Tool{}, fmt.Errorf("Python profiler identity differs from lock")
	}
	p.identities["python-profiler"] = identity
	return identity, nil
}
func (p *Pipeline) Resolve(plan BuildPlan) ([]*Document, error) {
	roots := map[string]string{}
	for _, target := range plan.Targets {
		roots[target.Set.RootExtension] = target.Set.CoreProfileSchemaVersion
	}
	if len(p.Options.ExtensionRoots) > 0 {
		defaultVersion := ""
		mixedVersions := false
		for _, version := range roots {
			if defaultVersion != "" && defaultVersion != version {
				mixedVersions = true
			}
			defaultVersion = version
		}
		roots = map[string]string{}
		for _, root := range p.Options.ExtensionRoots {
			if _, ok := roots[root]; ok {
				return nil, fmt.Errorf("duplicate extension root %s", root)
			}
			version := ""
			for _, set := range p.Project.Catalog.Packages {
				if set.RootExtension == root {
					if version != "" && version != set.CoreProfileSchemaVersion {
						return nil, fmt.Errorf("extension root %s has conflicting catalog core schema versions", root)
					}
					version = set.CoreProfileSchemaVersion
				}
			}
			if version == "" {
				if mixedVersions {
					return nil, fmt.Errorf("uncataloged extension root %s requires targets with one core schema version", root)
				}
				version = defaultVersion
			}
			roots[root] = version
		}
	}
	resolver, err := p.resolver()
	if err != nil {
		return nil, err
	}
	lockedEntries := map[string]normalizer.DependencyLockEntry{}
	if p.Options.DependencyLock != "" {
		data, err := os.ReadFile(p.Options.DependencyLock)
		if err != nil {
			return nil, err
		}
		lock, err := normalizer.DecodeDependencyLock(data)
		if err != nil {
			return nil, err
		}
		for _, entry := range lock.Extensions {
			lockedEntries[entry.ID] = entry
		}
	}
	documents := []*Document{}
	for _, root := range sortedKeys(roots) {
		closure, err := resolver.Resolve(p.Context, root)
		if err != nil {
			return nil, err
		}
		lock := normalizer.BuildDependencyLock(closure)
		if p.Options.DependencyLock != "" {
			for i, document := range closure.Documents {
				id := document.Definition.Metadata.URI + ":" + document.Definition.Metadata.Version
				entry, ok := lockedEntries[id]
				if !ok {
					return nil, fmt.Errorf("dependency lock is missing extension %s", id)
				}
				lock.Extensions[i] = entry
			}
		}
		if err = normalizer.ValidateDependencyLock(closure, lock); err != nil {
			return nil, err
		}
		documents = append(documents, &Document{Closure: closure, Lock: lock, CoreVersion: roots[root]})
	}
	return documents, nil
}
func (p *Pipeline) resolver() (*normalizer.Resolver, error) {
	overrides := map[string]string{}
	for _, entry := range p.Options.Overrides {
		key, path, ok := strings.Cut(entry, "=")
		if !ok || key == "" || path == "" {
			return nil, fmt.Errorf("invalid extension override %q", entry)
		}
		if _, ok := overrides[key]; ok {
			return nil, fmt.Errorf("duplicate extension override %s", key)
		}
		cwd, _ := os.Getwd()
		overrides[key] = absolute(cwd, path)
	}
	locks := map[string]normalizer.LockEntry{}
	if p.Options.DependencyLock != "" {
		data, err := os.ReadFile(p.Options.DependencyLock)
		if err != nil {
			return nil, err
		}
		lock, err := normalizer.DecodeDependencyLock(data)
		if err != nil {
			return nil, err
		}
		for _, entry := range lock.Extensions {
			if _, exists := locks[entry.ID]; exists {
				return nil, fmt.Errorf("duplicate locked identifier %s", entry.ID)
			}
			locks[entry.ID] = normalizer.LockEntry{SourceSHA256: entry.SourceSHA256, SemanticSHA256: entry.SemanticSHA256, SourceBackend: entry.SourceBackend, Locator: entry.SourceLocator}
		}
	}
	catalogs := []string(p.Options.CatalogRoots)
	if len(catalogs) == 0 {
		candidate := filepath.Join(p.Project.RepositoryRoot, "catalog")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			catalogs = append(catalogs, candidate)
		}
	}
	return normalizer.NewResolver(normalizer.ResolverConfig{Schemas: p.Project.Schemas, Overrides: overrides, CatalogRoots: catalogs, CacheDir: p.Options.Cache, Network: true, RecordNetworkLocks: p.Project.Toolchain.Status != "released", Locks: locks, HTTPClient: p.HTTPClient})
}
func (p *Pipeline) core(version string) (map[string]any, normalizer.CoreProfileIdentity, error) {
	if doc, ok := p.cores[version]; ok {
		identity, err := coreIdentity(doc, version)
		return doc, identity, err
	}
	var data []byte
	var err error
	if p.Options.CoreSchema != "" {
		data, err = os.ReadFile(p.Options.CoreSchema)
	} else {
		url := "https://runtimeconditions.io/schemas/profile/" + version + "/runtimeconditions.profile.schema.yaml"
		request, requestErr := http.NewRequestWithContext(p.Context, http.MethodGet, url, nil)
		if requestErr != nil {
			return nil, normalizer.CoreProfileIdentity{}, requestErr
		}
		response, requestErr := p.HTTPClient.Do(request)
		if requestErr != nil {
			return nil, normalizer.CoreProfileIdentity{}, requestErr
		}
		defer response.Body.Close()
		if response.StatusCode != 200 {
			return nil, normalizer.CoreProfileIdentity{}, fmt.Errorf("core schema %s: %s", url, response.Status)
		}
		data, err = io.ReadAll(io.LimitReader(response.Body, 64<<20+1))
		if len(data) > 64<<20 {
			return nil, normalizer.CoreProfileIdentity{}, fmt.Errorf("core schema exceeds input limit")
		}
	}
	if err != nil {
		return nil, normalizer.CoreProfileIdentity{}, err
	}
	doc, err := normalizer.ParseYAMLData(data)
	if err != nil {
		return nil, normalizer.CoreProfileIdentity{}, err
	}
	identity, err := coreIdentity(doc, version)
	if err != nil {
		return nil, identity, err
	}
	p.cores[version] = doc
	return doc, identity, nil
}
func coreIdentity(doc map[string]any, version string) (normalizer.CoreProfileIdentity, error) {
	id, _ := doc["$id"].(string)
	expected := "https://runtimeconditions.io/schemas/profile/" + version + "/runtimeconditions.profile.schema.yaml"
	if id != expected {
		return normalizer.CoreProfileIdentity{}, fmt.Errorf("core schema identifier %q differs from selected version %s", id, version)
	}
	data, err := normalizer.CanonicalJSON(doc)
	if err != nil {
		return normalizer.CoreProfileIdentity{}, err
	}
	return normalizer.CoreProfileIdentity{ID: id, Version: version, SemanticSHA256: normalizer.SHA256Hex(data)}, nil
}
func (p *Pipeline) Normalize(doc *Document) error {
	if doc.Model.APIVersion != "" {
		return nil
	}
	_, core, err := p.core(doc.CoreVersion)
	if err != nil {
		return err
	}
	binary, tool, err := p.tool("normalizer")
	if err != nil {
		return err
	}
	if err = normalizer.ValidateDependencyLock(doc.Closure, doc.Lock); err != nil {
		return err
	}
	directory, err := p.workspacePath("normalizer-inputs", normalizer.SHA256Hex([]byte(doc.Closure.Root)))
	if err != nil {
		return err
	}
	if err = os.MkdirAll(directory, 0755); err != nil {
		return err
	}
	args := []string{"--root", doc.Closure.Root,
		"--semantic-schema", filepath.Join(p.Project.ToolingDirectory, "model", "runtimeconditions.extension-semantic.schema.yaml"),
		"--model-schema", filepath.Join(p.Project.ToolingDirectory, "model", "runtimeconditions.binding-model.schema.yaml"),
		"--core-profile-id", core.ID, "--core-profile-version", core.Version,
		"--core-profile-semantic-sha256", core.SemanticSHA256, "--normalizer-sha256", tool.SHA256}
	// The resolver has already verified transport bytes. The selected executable
	// receives those exact bytes without resolving or fetching them again.
	for _, source := range doc.Closure.Documents {
		id := source.Definition.Metadata.URI + ":" + source.Definition.Metadata.Version
		path := filepath.Join(directory, normalizer.SHA256Hex([]byte(id))+".yaml")
		if err = os.WriteFile(path, source.Bytes, 0644); err != nil {
			return err
		}
		args = append(args, "--extension-override", id+"="+path)
	}
	data, err := p.run(directory, nil, binary, args...)
	if err != nil {
		return err
	}
	mapping, err := normalizer.ParseYAMLData(data)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(mapping)
	if err != nil {
		return err
	}
	if err = json.Unmarshal(encoded, &doc.Model); err != nil {
		return err
	}
	if doc.Model.Metadata.Normalizer.SHA256 != tool.SHA256 || doc.Model.RootExtension.ID != doc.Closure.Root || doc.Model.CoreProfileSchema != core {
		return fmt.Errorf("normalizer output identity differs from selected inputs")
	}
	return p.Project.Schemas.ValidateModel(doc.Model)
}
func (p *Pipeline) Generate(plan BuildPlan) (*Build, error) {
	docs, err := p.Resolve(plan)
	if err != nil {
		return nil, err
	}
	documents := map[string]*Document{}
	models := map[string]normalizer.BindingModel{}
	for _, doc := range docs {
		if err = p.Normalize(doc); err != nil {
			return nil, err
		}
		documents[doc.Closure.Root] = doc
		models[doc.Closure.Root] = doc.Model
	}
	// Dependency providers normalize their own roots using their catalog's core version.
	resolver, err := p.resolver()
	if err != nil {
		return nil, err
	}
	for _, doc := range docs {
		for _, extension := range doc.Model.Extensions {
			if _, ok := models[extension.ID]; ok {
				continue
			}
			for _, set := range p.Project.Catalog.Packages {
				if set.RootExtension != extension.ID {
					continue
				}
				closure, err := resolver.Resolve(p.Context, extension.ID)
				if err != nil {
					return nil, err
				}
				provider := &Document{Closure: closure, Lock: normalizer.BuildDependencyLock(closure), CoreVersion: set.CoreProfileSchemaVersion}
				if err = p.Normalize(provider); err != nil {
					return nil, err
				}
				documents[extension.ID] = provider
				models[extension.ID] = provider.Model
			}
		}
	}
	ordered, err := p.Project.OrderTargets(plan, models)
	if err != nil {
		return nil, err
	}
	build := &Build{Pipeline: p, Plan: ordered, Targets: []*BuiltTarget{}}
	for _, target := range ordered.Targets {
		directory, err := p.workspacePath("generated", target.PackageKey, target.Language)
		if err != nil {
			return nil, err
		}
		if err = os.MkdirAll(filepath.Dir(directory), 0755); err != nil {
			return nil, err
		}
		// Retained work directories may be reused, but never trust their old outputs.
		if err = os.RemoveAll(directory); err != nil {
			return nil, err
		}
		bt := &BuiltTarget{Target: target, Document: documents[target.Set.RootExtension], Directory: directory, Artifacts: map[string]string{}}
		if bt.Document == nil {
			return nil, fmt.Errorf("%s: selected extension roots do not include catalog root %s", target.Key, target.Set.RootExtension)
		}
		bt.PackageConfig, err = p.nativeConfig(target, ordered)
		if err != nil {
			return nil, err
		}
		if err = p.emit(bt); err != nil {
			return nil, err
		}
		if err = p.assemble(bt, build); err != nil {
			return nil, err
		}
		build.Targets = append(build.Targets, bt)
	}
	return build, nil
}
func (p *Pipeline) nativeConfig(target Target, plan BuildPlan) (map[string]any, error) {
	config := map[string]any{"packageKey": target.PackageKey, "rootExtension": target.Set.RootExtension, "version": target.Config.Version, "publicationMode": target.Config.PublicationMode}
	dependencies := []any{}
	for _, key := range target.Dependencies {
		var provider Target
		for _, item := range plan.Targets {
			if item.Key == key {
				provider = item
				break
			}
		}
		if target.Language == "go" {
			dependencies = append(dependencies, map[string]any{"extension": provider.Set.RootExtension, "modulePath": provider.Config.Coordinate, "packageName": provider.Config.Name, "version": "v" + provider.Config.Version})
		} else {
			dependencies = append(dependencies, map[string]any{"extension": provider.Set.RootExtension, "distributionName": provider.Config.Coordinate, "importPackage": provider.Config.Name, "version": provider.Config.Version})
		}
	}
	config["dependencies"] = dependencies
	switch target.Language {
	case "go":
		_, identity, err := p.tool("go-emitter")
		if err != nil {
			return nil, err
		}
		config["apiVersion"] = goemitter.PackageTargetAPIVersion
		config["kind"] = goemitter.PackageTargetKind
		config["modulePath"] = target.Config.Coordinate
		config["packageName"] = target.Config.Name
		config["minimumGoVersion"] = majorMinor(target.Config.LanguageVersion)
		config["version"] = "v" + target.Config.Version
		config["emitterSha256"] = identity.SHA256
	case "python":
		identity, err := p.pythonEmitterIdentity()
		if err != nil {
			return nil, err
		}
		config["apiVersion"] = "runtimeconditions.io/python-package-target/v1alpha1"
		config["kind"] = "RuntimeConditionsPythonPackageTarget"
		config["distributionName"] = target.Config.Coordinate
		config["importPackage"] = target.Config.Name
		config["minimumPythonVersion"] = majorMinor(target.Config.LanguageVersion)
		config["sourceDirectory"] = target.Config.SourceDirectory
		config["repositoryUrl"] = p.Project.Catalog.RepositoryURL
		config["emitterSha256"] = identity.SHA256
		if target.Config.RegistryID != "" {
			config["registryId"] = target.Config.RegistryID
		}
	default:
		return nil, fmt.Errorf("%s: unsupported native language adapter %s", target.Key, target.Language)
	}
	return config, nil
}
func (p *Pipeline) emitterInputs(bt *BuiltTarget) (string, string, error) {
	dir, err := p.workspacePath("emitter-inputs", bt.Target.PackageKey, bt.Target.Language)
	if err != nil {
		return "", "", err
	}
	if err = os.MkdirAll(dir, 0755); err != nil {
		return "", "", err
	}
	model, err := bt.Document.ModelYAML()
	if err != nil {
		return "", "", err
	}
	target, err := yaml.Marshal(bt.PackageConfig)
	if err != nil {
		return "", "", err
	}
	modelPath, targetPath := filepath.Join(dir, modelName), filepath.Join(dir, "target.yaml")
	if err = os.WriteFile(modelPath, model, 0644); err != nil {
		return "", "", err
	}
	if err = os.WriteFile(targetPath, target, 0644); err != nil {
		return "", "", err
	}
	return modelPath, targetPath, nil
}
func (p *Pipeline) emit(bt *BuiltTarget) error {
	modelPath, targetPath, err := p.emitterInputs(bt)
	if err != nil {
		return err
	}
	if bt.Target.Language == "go" {
		tool, _, err := p.tool("go-emitter")
		if err != nil {
			return err
		}
		_, err = p.run(p.Project.Root, nil, tool, "--model", modelPath, "--package-config", targetPath, "--output", bt.Directory)
		return err
	}
	_, err = p.python("emit", "--model", modelPath, "--package-config", targetPath, "--output", bt.Directory, "--assembled")
	return err
}
func resourceDir(bt *BuiltTarget) string {
	if bt.Target.Language == "python" {
		return filepath.Join(bt.Directory, "src", bt.Target.Config.Name)
	}
	return bt.Directory
}
func (p *Pipeline) assemble(bt *BuiltTarget, build *Build) error {
	resource := resourceDir(bt)
	model, err := bt.Document.ModelYAML()
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(resource, modelName), model, 0644); err != nil {
		return err
	}
	var root normalizer.ResolvedDocument
	for _, doc := range bt.Document.Closure.Documents {
		if doc.Definition.Metadata.URI+":"+doc.Definition.Metadata.Version == bt.Target.Set.RootExtension {
			root = doc
			break
		}
	}
	if err = os.WriteFile(filepath.Join(resource, extensionName), root.Bytes, 0644); err != nil {
		return err
	}
	bt.Manifest, err = readMapping(filepath.Join(resource, bindingName))
	if err != nil {
		return err
	}
	release, err := p.releaseManifest(bt, build)
	if err != nil {
		return err
	}
	data, err := yaml.Marshal(release)
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(resource, releaseName), data, 0644); err != nil {
		return err
	}
	if err = writeFileManifest(bt.Directory); err != nil {
		return err
	}
	bt.Artifacts, err = p.nativeArchives(bt)
	return err
}
func (b *Build) selected() []*BuiltTarget {
	result := []*BuiltTarget{}
	for _, target := range b.Targets {
		if target.Target.Selected {
			result = append(result, target)
		}
	}
	return result
}
func (b *Build) CopySelected(output string) error {
	for _, target := range b.selected() {
		if err := copyTree(target.Directory, filepath.Join(output, filepath.FromSlash(target.Target.Config.SourceDirectory))); err != nil {
			return err
		}
	}
	return nil
}
func (b *Build) CompareCommitted() error {
	for _, target := range b.selected() {
		committed := filepath.Join(b.Pipeline.Project.RepositoryRoot, filepath.FromSlash(target.Target.Config.SourceDirectory))
		if err := compareTrees(target.Directory, committed); err != nil {
			return fmt.Errorf("%s: generated source drift: %w", target.Target.Key, err)
		}
	}
	return nil
}
func (p *Pipeline) LoadBuild(plan BuildPlan, input string) (*Build, error) {
	build := &Build{Pipeline: p, Plan: plan, Targets: []*BuiltTarget{}}
	// Include closure providers even when only a dependent target was selected.
	targets := map[string]Target{}
	for key, set := range p.Project.Catalog.Packages {
		for language, config := range set.Languages {
			id := key + ":" + language
			targets[id] = Target{Key: id, PackageKey: key, Language: language, Config: config, Set: set}
		}
	}
	selected := map[string]bool{}
	for _, target := range plan.Targets {
		selected[target.Key] = true
	}
	loaded := map[string]*BuiltTarget{}
	active := map[string]bool{}
	var load func(string) error
	load = func(key string) error {
		if _, ok := loaded[key]; ok {
			return nil
		}
		if active[key] {
			return fmt.Errorf("package dependency cycle at %s", key)
		}
		active[key] = true
		target := targets[key]
		target.Selected = selected[key]
		root := p.Project.RepositoryRoot
		if input != "" {
			root = input
		}
		directory := filepath.Join(root, filepath.FromSlash(target.Config.SourceDirectory))
		// --input can name one exact target directory when one target was selected.
		if input != "" && len(plan.Targets) == 1 && exists(filepath.Join(input, fileManifestName)) {
			directory = input
		}
		bt := &BuiltTarget{Target: target, Directory: directory, Artifacts: map[string]string{}}
		resource := resourceDir(bt)
		model, err := goemitter.LoadModel(filepath.Join(resource, modelName))
		if err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
		release, err := readMapping(filepath.Join(resource, releaseName))
		if err != nil {
			return err
		}
		var lock normalizer.DependencyLock
		data, _ := yaml.Marshal(release["dependencyLock"])
		if err = yaml.Unmarshal(data, &lock); err != nil {
			return err
		}
		bt.Document = &Document{Model: model, Lock: lock, CoreVersion: target.Set.CoreProfileSchemaVersion}
		bt.Manifest, err = readMapping(filepath.Join(resource, bindingName))
		if err != nil {
			return err
		}
		for _, edge := range model.DependencyEdges {
			if edge.From != target.Set.RootExtension {
				continue
			}
			matches := []string{}
			for providerKey, provider := range targets {
				if provider.Language == target.Language && provider.Set.RootExtension == edge.To {
					matches = append(matches, providerKey)
				}
			}
			if len(matches) != 1 {
				return fmt.Errorf("%s: dependency %s has %d catalog providers", key, edge.To, len(matches))
			}
			bt.Target.Dependencies = append(bt.Target.Dependencies, matches[0])
		}
		sort.Strings(bt.Target.Dependencies)
		for _, dep := range bt.Target.Dependencies {
			if err = load(dep); err != nil {
				return err
			}
		}
		loaded[key] = bt
		build.Targets = append(build.Targets, bt)
		delete(active, key)
		return nil
	}
	for _, target := range plan.Targets {
		if err := load(target.Key); err != nil {
			return nil, err
		}
	}
	ordered := BuildPlan{Targets: []Target{}}
	for _, bt := range build.Targets {
		ordered.Targets = append(ordered.Targets, bt.Target)
	}
	build.Plan = ordered
	for _, bt := range build.Targets {
		config, err := p.nativeConfig(bt.Target, ordered)
		if err != nil {
			return nil, err
		}
		bt.PackageConfig = config
	}
	return build, nil
}
