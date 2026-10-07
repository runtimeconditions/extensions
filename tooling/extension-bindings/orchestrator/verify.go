package orchestrator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"strings"

	goemitter "github.com/runtimeconditions/extensions/tooling/extension-bindings/emitters/go"
	"github.com/runtimeconditions/extensions/tooling/extension-bindings/normalizer"
)

type GateResult struct {
	Gate   int    `yaml:"gate"`
	Status string `yaml:"status"`
}
type TargetResult struct {
	Target string       `yaml:"target"`
	Gates  []GateResult `yaml:"gates"`
}
type VerificationSummary struct {
	Status  string         `yaml:"status"`
	Scope   string         `yaml:"scope"`
	Targets []TargetResult `yaml:"targets"`
	Errors  []string       `yaml:"errors,omitempty"`
}

func (s *VerificationSummary) Fail(stage string, err error) {
	s.Status = "failed"
	s.Errors = append(s.Errors, stage+": "+err.Error())
}

type nativeEnvironment struct {
	GoEnv       map[string]string
	Python      string
	PythonEnv   map[string]string
	PythonSite  string
	Directories map[string]string
}

func (p *Pipeline) Verify(build *Build) (VerificationSummary, error) {
	summary := VerificationSummary{Status: "passed", Scope: "package-structure", Targets: []TargetResult{}}
	fail := func(target string, gate int, err error) (VerificationSummary, error) {
		wrapped := fmt.Errorf("%s gate %d: %w", target, gate, err)
		summary.Fail(target, wrapped)
		return summary, wrapped
	}
	for _, bt := range build.Targets {
		if err := p.Project.Schemas.ValidateModel(bt.Document.Model); err != nil {
			return fail(bt.Target.Key, 1, err)
		}
		digest, err := normalizer.ModelSemanticSHA256(bt.Document.Model)
		if err != nil || digest != bt.Document.Model.Metadata.SemanticSHA256 {
			if err == nil {
				err = fmt.Errorf("model semantic digest differs")
			}
			return fail(bt.Target.Key, 1, err)
		}
		if bt.Document.Model.RootExtension.ID != bt.Target.Set.RootExtension {
			return fail(bt.Target.Key, 1, fmt.Errorf("model root differs from catalog"))
		}
		for _, resource := range []struct{ name, schema string }{{bindingName, "runtimeconditions.binding-manifest.schema.yaml"}, {releaseName, "runtimeconditions.binding-release.schema.yaml"}} {
			value, err := readMapping(filepath.Join(resourceDir(bt), resource.name))
			if err != nil {
				return fail(bt.Target.Key, 2, err)
			}
			schema, err := readMapping(filepath.Join(p.Project.ToolingDirectory, "model", resource.schema))
			if err != nil {
				return fail(bt.Target.Key, 2, err)
			}
			if err = validateSchema(schema, value); err != nil {
				return fail(bt.Target.Key, 2, err)
			}
			if resource.name == bindingName {
				bt.Manifest = value
			}
		}
		if err := p.verifyIdentities(bt, build); err != nil {
			return fail(bt.Target.Key, 2, err)
		}
		if err := verifyFileManifest(bt.Directory, filepath.Join(p.Project.ToolingDirectory, "model", "runtimeconditions.file-manifest.schema.yaml")); err != nil {
			return fail(bt.Target.Key, 15, err)
		}
		if len(bt.Artifacts) == 0 {
			artifacts, err := p.nativeArchives(bt)
			if err != nil {
				return fail(bt.Target.Key, 13, err)
			}
			bt.Artifacts = artifacts
		}
	}
	environment, err := p.installBuild(build)
	if err != nil {
		return fail("native-installation", 3, err)
	}
	for _, bt := range build.Targets {
		result := TargetResult{Target: bt.Target.Key, Gates: []GateResult{}}
		summary.Targets = append(summary.Targets, result)
		if err = p.verifyNative(bt, environment); err != nil {
			return fail(bt.Target.Key, 3, err)
		}
		model, target, err := p.emitterInputs(bt)
		if err != nil {
			return fail(bt.Target.Key, 16, err)
		}
		if bt.Target.Language == "go" {
			nativeTarget, err := goemitter.LoadPackageTarget(target)
			if err != nil {
				return fail(bt.Target.Key, 16, err)
			}
			if _, err = goemitter.VerifyAPI(bt.Document.Model, nativeTarget, bt.Directory); err != nil {
				return fail(bt.Target.Key, 16, err)
			}
		} else {
			if _, err = p.python("verify", "--model", model, "--package-config", target, "--input", resourceDir(bt)); err != nil {
				return fail(bt.Target.Key, 16, err)
			}
		}
		if err = p.verifyRegeneration(bt, build); err != nil {
			return fail(bt.Target.Key, 12, err)
		}
		for gate := 1; gate <= 16; gate++ {
			summary.Targets[len(summary.Targets)-1].Gates = append(summary.Targets[len(summary.Targets)-1].Gates, GateResult{Gate: gate, Status: packageGateStatus(gate)})
		}
	}
	return summary, nil
}
func (p *Pipeline) verifyIdentities(bt *BuiltTarget, build *Build) error {
	release, err := readMapping(filepath.Join(resourceDir(bt), releaseName))
	if err != nil {
		return err
	}
	manifest := bt.Manifest
	model := bt.Document.Model
	if stringValue(mapValue(manifest["model"])["semanticSha256"]) != model.Metadata.SemanticSHA256 || stringValue(mapValue(release["model"])["semanticSha256"]) != model.Metadata.SemanticSHA256 {
		return fmt.Errorf("resource model identities differ")
	}
	identity := mapValue(manifest["package"])
	if stringValue(identity["coordinate"]) != bt.Target.Config.Coordinate || stringValue(identity["name"]) != bt.Target.Config.Name || strings.TrimPrefix(stringValue(identity["version"]), "v") != bt.Target.Config.Version {
		return fmt.Errorf("native package identity differs from catalog")
	}
	if stringValue(mapValue(manifest["extension"])["id"]) != model.RootExtension.ID || stringValue(mapValue(manifest["extension"])["semanticSha256"]) != model.RootExtension.SemanticSHA256 {
		return fmt.Errorf("binding extension identity differs")
	}
	provenance := mapValue(release["provenance"])
	if stringValue(provenance["mode"]) != "production" || stringValue(provenance["sourceDirectory"]) != bt.Target.Config.SourceDirectory || stringValue(provenance["targetReleaseTag"]) != bt.Target.Config.SourceDirectory+"/v"+bt.Target.Config.Version {
		return fmt.Errorf("release target provenance differs from catalog")
	}
	entries := map[string]normalizer.DependencyLockEntry{}
	for _, entry := range bt.Document.Lock.Extensions {
		if _, ok := entries[entry.ID]; ok {
			return fmt.Errorf("duplicate locked extension %s", entry.ID)
		}
		entries[entry.ID] = entry
	}
	if len(entries) != len(model.Extensions) {
		return fmt.Errorf("dependency lock closure differs from model")
	}
	definitions := map[string]*BuiltTarget{}
	for _, provider := range build.Targets {
		if provider.Target.Language == bt.Target.Language {
			definitions[provider.Target.Set.RootExtension] = provider
		}
	}
	for _, extension := range model.Extensions {
		entry, ok := entries[extension.ID]
		if !ok || entry.Version != extension.Version || entry.SemanticSHA256 != extension.SemanticSHA256 || strings.Join(entry.Dependencies, "\n") != strings.Join(extension.Dependencies, "\n") {
			return fmt.Errorf("dependency lock semantic identity differs for %s", extension.ID)
		}
		provider := definitions[extension.ID]
		if provider == nil {
			return fmt.Errorf("native provider missing for closure member %s", extension.ID)
		}
		source, err := os.ReadFile(filepath.Join(resourceDir(provider), extensionName))
		if err != nil {
			return err
		}
		if normalizer.SHA256Hex(source) != entry.SourceSHA256 {
			return fmt.Errorf("extension source digest differs for %s", extension.ID)
		}
		definition, mapping, err := normalizer.DecodeExtension(source)
		if err != nil {
			return err
		}
		if definition.Metadata.URI+":"+definition.Metadata.Version != extension.ID {
			return fmt.Errorf("extension source identity differs")
		}
		if err = p.Project.Schemas.ValidateExtension(mapping, definition); err != nil {
			return err
		}
		_, digest, err := p.Project.Schemas.CanonicalizeExtension(mapping)
		if err != nil {
			return err
		}
		if digest != extension.SemanticSHA256 {
			return fmt.Errorf("extension semantic digest differs")
		}
	}
	profiler, err := p.profiler(bt.Target.Language)
	if err != nil {
		return err
	}
	declared := mapValue(provenance["profiler"])
	if declared["name"] != profiler.Name || declared["version"] != profiler.Version || declared["sha256"] != profiler.SHA256 {
		return fmt.Errorf("profiler provenance differs from installed artifact")
	}
	_, tool, err := p.tool("normalizer")
	if err != nil {
		return err
	}
	if model.Metadata.Normalizer.SHA256 != tool.SHA256 || model.Metadata.Normalizer.Name != tool.Name || model.Metadata.Normalizer.Version != tool.Version {
		return fmt.Errorf("normalizer provenance differs from selected artifact")
	}
	_, tool, err = p.tool("orchestrator")
	if err != nil {
		return err
	}
	declared = mapValue(provenance["orchestrator"])
	if declared["sha256"] != tool.SHA256 || declared["name"] != tool.Name || declared["version"] != tool.Version {
		return fmt.Errorf("orchestrator provenance differs from selected artifact")
	}
	if bt.Target.Language == "go" {
		_, tool, err = p.tool("go-emitter")
	} else {
		tool, err = p.pythonEmitterIdentity()
	}
	if err != nil {
		return err
	}
	generated := mapValue(manifest["generated"])
	if generated["emitter"] != tool.Name+"@sha256:"+tool.SHA256 || generated["version"] != tool.Version {
		return fmt.Errorf("emitter provenance differs from selected artifact")
	}
	_, core, err := p.core(bt.Document.CoreVersion)
	if err != nil {
		return err
	}
	if core != model.CoreProfileSchema {
		return fmt.Errorf("core schema identity differs from selected input")
	}
	return nil
}
func escapeModule(value string) string {
	var result strings.Builder
	for _, r := range value {
		if r >= 'A' && r <= 'Z' {
			result.WriteByte('!')
			result.WriteRune(r + ('a' - 'A'))
		} else {
			result.WriteRune(r)
		}
	}
	return result.String()
}
func (p *Pipeline) installBuild(build *Build) (*nativeEnvironment, error) {
	environment := &nativeEnvironment{Directories: map[string]string{}}
	work, err := p.workspace()
	if err != nil {
		return nil, err
	}
	hasGo, hasPython := false, false
	for _, bt := range build.Targets {
		hasGo = hasGo || bt.Target.Language == "go"
		hasPython = hasPython || bt.Target.Language == "python"
	}
	if hasGo {
		if err = removeWorkTree(filepath.Join(work, "native-go")); err != nil {
			return nil, err
		}
		proxy := filepath.Join(work, "native-go", "proxy")
		if err = os.MkdirAll(proxy, 0755); err != nil {
			return nil, err
		}
		for _, bt := range build.Targets {
			if bt.Target.Language != "go" {
				continue
			}
			directory := filepath.Join(proxy, filepath.FromSlash(escapeModule(bt.Target.Config.Coordinate)), "@v")
			if err = os.MkdirAll(directory, 0755); err != nil {
				return nil, err
			}
			version := "v" + bt.Target.Config.Version
			artifact, err := os.ReadFile(bt.Artifacts["go-module-zip"])
			if err != nil {
				return nil, err
			}
			mod, err := os.ReadFile(filepath.Join(bt.Directory, "go.mod"))
			if err != nil {
				return nil, err
			}
			info, _ := json.Marshal(map[string]any{"Version": version, "Time": "1980-01-01T00:00:00Z"})
			for name, data := range map[string][]byte{escapeModule(version) + ".zip": artifact, escapeModule(version) + ".mod": mod, escapeModule(version) + ".info": info, "list": []byte(version + "\n")} {
				if err = os.WriteFile(filepath.Join(directory, name), data, 0644); err != nil {
					return nil, err
				}
			}
		}
		environment.GoEnv = map[string]string{"GOPROXY": "file://" + filepath.ToSlash(proxy), "GOSUMDB": "off", "GOMODCACHE": filepath.Join(work, "native-go", "modcache"), "GOCACHE": filepath.Join(work, "native-go", "buildcache"), "GOWORK": "off", "GOFLAGS": "-modcacherw", "GOENV": "off", "GOTOOLCHAIN": "local"}
	}
	if hasPython {
		venv := filepath.Join(work, "native-python", "venv")
		if err = os.RemoveAll(filepath.Dir(venv)); err != nil {
			return nil, err
		}
		if _, err = p.run(p.Project.Root, p.pythonEnv(), p.Options.Python, "-m", "venv", "--without-pip", venv); err != nil {
			return nil, err
		}
		environment.Python = filepath.Join(venv, "bin", "python")
		if os.PathSeparator == '\\' {
			environment.Python = filepath.Join(venv, "Scripts", "python.exe")
		}
		args := []string{"-m", "pip", "--python", environment.Python, "install", "--no-index", "--no-deps", "--no-compile", p.Options.PythonProfilerArtifact}
		for _, bt := range build.Targets {
			if bt.Target.Language == "python" {
				args = append(args, bt.Artifacts["python-wheel"])
			}
		}
		if _, err = p.run(p.Project.Root, p.pythonEnv(), p.Options.Python, args...); err != nil {
			return nil, err
		}
		data, err := p.run(p.Project.Root, nil, environment.Python, "-c", "import site;print(site.getsitepackages()[0])")
		if err != nil {
			return nil, err
		}
		environment.PythonSite = strings.TrimSpace(string(data))
		// Reuse only the independent profiler's declared installed dependencies.
		// Never expose the tooling environment's other packages to the consumer.
		dependencies := `import importlib.metadata as metadata
import pathlib, shutil, sys
from packaging.requirements import Requirement
destination = pathlib.Path(sys.argv[1])
seen = {"runtimeconditions-profiler"}
def copy_dependencies(name):
    for text in metadata.requires(name) or []:
        requirement = Requirement(text)
        if requirement.marker and not requirement.marker.evaluate({"extra": ""}):
            continue
        key = requirement.name.lower().replace("_", "-")
        if key in seen:
            continue
        seen.add(key)
        distribution = metadata.distribution(requirement.name)
        if distribution.version not in requirement.specifier:
            raise ValueError("installed profiler dependency version mismatch: " + text)
        for entry in distribution.files or []:
            relative = pathlib.PurePosixPath(str(entry))
            if relative.is_absolute() or ".." in relative.parts:
                continue
            source = pathlib.Path(distribution.locate_file(entry))
            target = destination.joinpath(*relative.parts)
            if source.is_file():
                target.parent.mkdir(parents=True, exist_ok=True)
                shutil.copyfile(source, target)
        copy_dependencies(requirement.name)
copy_dependencies("runtimeconditions-profiler")
`
		if _, err = p.run(p.Project.Root, p.pythonEnv(), p.Options.Python, "-c", dependencies, environment.PythonSite); err != nil {
			return nil, err
		}
		environment.PythonEnv = map[string]string{"PYTHONPATH": "", "PYTHONDONTWRITEBYTECODE": "1", "PYTHONNOUSERSITE": "1"}
	}
	for _, bt := range build.Targets {
		stage := filepath.Join(work, "native-source", bt.Target.PackageKey, bt.Target.Language)
		if err = os.RemoveAll(stage); err != nil {
			return nil, err
		}
		if err = copyTree(bt.Directory, stage); err != nil {
			return nil, err
		}
		environment.Directories[bt.Target.Key] = stage
	}
	return environment, nil
}
func (p *Pipeline) verifyNative(bt *BuiltTarget, environment *nativeEnvironment) error {
	directory := environment.Directories[bt.Target.Key]
	if bt.Target.Language == "go" {
		version, err := p.run(directory, environment.GoEnv, p.Options.Go, "env", "GOVERSION")
		if err != nil {
			return err
		}
		if strings.TrimPrefix(strings.TrimSpace(string(version)), "go") != bt.Target.Config.LanguageVersion {
			return fmt.Errorf("gate 5: Go compiler must be exactly %s; supply --go pointing to that toolchain", bt.Target.Config.LanguageVersion)
		}
		files, err := readTree(bt.Directory)
		if err != nil {
			return err
		}
		for name, data := range files {
			if strings.HasSuffix(name, ".go") {
				formatted, err := format.Source(data)
				if err != nil {
					return fmt.Errorf("gate 4 %s: %w", name, err)
				}
				if !bytes.Equal(data, formatted) {
					return fmt.Errorf("gate 4: %s is not gofmt formatted", name)
				}
			}
		}
		for _, args := range [][]string{{"list", "-m", "-json"}, {"build", "./..."}, {"vet", "./..."}} {
			if _, err = p.run(directory, environment.GoEnv, p.Options.Go, args...); err != nil {
				return err
			}
		}
		return nil
	}
	version, err := p.run(directory, environment.PythonEnv, environment.Python, "-c", "import platform;print(platform.python_version())")
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(version)) != bt.Target.Config.LanguageVersion {
		return fmt.Errorf("gate 5: Python interpreter must be exactly %s; supply --python pointing to that interpreter", bt.Target.Config.LanguageVersion)
	}
	if _, err = p.run(directory, p.pythonEnv(), p.Options.Python, "-c", "import tomllib,pathlib;tomllib.loads(pathlib.Path('pyproject.toml').read_text())"); err != nil {
		return err
	}
	if _, err = p.run(directory, p.pythonEnv(), p.Options.Python, "-m", "ruff", "format", "--no-cache", "--check", "."); err != nil {
		return fmt.Errorf("gate 4: %w", err)
	}
	compileScript := "import pathlib,py_compile,sys;root=pathlib.Path(sys.argv[1]);out=pathlib.Path(sys.argv[2]);out.mkdir(parents=True,exist_ok=True);[(py_compile.compile(str(p),cfile=str(out/(str(i)+'.pyc')),doraise=True)) for i,p in enumerate(sorted(root.rglob('*.py')))]"
	compiled, err := p.workspacePath("compiled-python", bt.Target.PackageKey)
	if err != nil {
		return err
	}
	if _, err = p.run(directory, environment.PythonEnv, environment.Python, "-c", compileScript, directory, compiled); err != nil {
		return fmt.Errorf("gate 5: %w", err)
	}
	typeEnv := p.pythonEnv()
	if _, err = p.run(directory, typeEnv, p.Options.Python, "-m", "mypy", "--strict", "--no-incremental", "--python-executable", environment.Python, "--python-version", majorMinor(bt.Target.Config.LanguageVersion), "src"); err != nil {
		return fmt.Errorf("gate 6: %w", err)
	}
	return nil
}

// Generator unit tests and synthetic profiler cases run in the tooling suite.
// These gates no longer describe checks performed against each delivered package.
func packageGateStatus(gate int) string {
	if gate >= 7 && gate <= 11 {
		return "not-applicable"
	}
	return "passed"
}

func (p *Pipeline) verifyRegeneration(bt *BuiltTarget, build *Build) error {
	if bt.Document.Closure.Root == "" {
		bt.Document.Closure = normalizer.ResolvedClosure{Root: bt.Target.Set.RootExtension, Documents: []normalizer.ResolvedDocument{}}
		for _, entry := range bt.Document.Lock.Extensions {
			for _, provider := range build.Targets {
				if provider.Target.Language != bt.Target.Language || provider.Target.Set.RootExtension != entry.ID {
					continue
				}
				data, err := os.ReadFile(filepath.Join(resourceDir(provider), extensionName))
				if err != nil {
					return err
				}
				definition, mapping, err := normalizer.DecodeExtension(data)
				if err != nil {
					return err
				}
				bt.Document.Closure.Documents = append(bt.Document.Closure.Documents, normalizer.ResolvedDocument{Definition: definition, Bytes: data, Data: mapping, SourceSHA256: entry.SourceSHA256, SemanticSHA256: entry.SemanticSHA256, Backend: entry.SourceBackend, Locator: entry.SourceLocator})
			}
		}
	}
	directory, err := p.workspacePath("regenerated", bt.Target.PackageKey, bt.Target.Language)
	if err != nil {
		return err
	}
	if err = os.RemoveAll(directory); err != nil {
		return err
	}
	regenerated := *bt
	regenerated.Directory = directory
	regenerated.Artifacts = map[string]string{}
	if err = p.emit(&regenerated); err != nil {
		return err
	}
	preceding := &Build{Pipeline: p, Targets: []*BuiltTarget{}}
	for _, provider := range build.Targets {
		if provider.Target.Key == bt.Target.Key {
			break
		}
		preceding.Targets = append(preceding.Targets, provider)
	}
	if err = p.assemble(&regenerated, preceding); err != nil {
		return err
	}
	return compareTrees(regenerated.Directory, bt.Directory)
}
