package orchestrator

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type repeated []string

func (r *repeated) String() string         { return strings.Join(*r, ",") }
func (r *repeated) Set(value string) error { *r = append(*r, value); return nil }

type Options struct {
	Packages, ToolchainLock, Cache, WorkDir, ToolingDir                                               string
	Output, Input, Report, ClosureOutput, DependencyLockOutput, DependencyLock                        string
	CoreSchema, Go, Python, GoEmitter, Normalizer, GoProfiler, PythonProfiler, PythonProfilerArtifact string
	Targets, ExtensionRoots, Overrides, CatalogRoots                                                  repeated
}

// Run is the reusable command boundary; the rc executable only supplies streams.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprintln(stdout, "Usage: rc bindings <resolve|normalize|generate|verify|package|plan-release|check|update> [flags]")
		return nil
	}
	if args[0] == "--version" {
		fmt.Fprintln(stdout, "rc", Version)
		return nil
	}
	if args[0] != "bindings" || len(args) < 2 {
		return fmt.Errorf("usage: rc bindings <command> [flags]")
	}
	name := args[1]
	supported := map[string]bool{"resolve": true, "normalize": true, "generate": true, "verify": true, "package": true, "plan-release": true, "check": true, "update": true}
	if name == "--help" || name == "-h" {
		fmt.Fprintln(stdout, "Commands: resolve normalize generate verify package plan-release check update")
		return nil
	}
	if !supported[name] {
		return fmt.Errorf("unknown bindings command %q", name)
	}
	var o Options
	f := flag.NewFlagSet("rc bindings "+name, flag.ContinueOnError)
	f.SetOutput(stderr)
	f.StringVar(&o.Packages, "packages", "", "package catalog path")
	f.StringVar(&o.ToolchainLock, "toolchain-lock", "", "toolchain lock path")
	f.StringVar(&o.Cache, "cache", "", "explicit persistent content-addressed extension cache")
	f.StringVar(&o.WorkDir, "work-dir", "", "retain required intermediates in this directory")
	f.StringVar(&o.ToolingDir, "tooling-dir", "", "tool installation containing schemas and development tools")
	f.Var(&o.Targets, "target", "package-key:language; repeatable, defaults to all targets")
	resolution := name == "resolve" || name == "normalize" || name == "generate" || name == "package" || name == "check" || name == "update"
	if resolution {
		f.Var(&o.ExtensionRoots, "extension-root", "exact root extension identifier; repeatable, replaces configured roots")
		f.Var(&o.Overrides, "extension-override", "exact identifier=local-path; repeatable, development only")
		f.Var(&o.CatalogRoots, "catalog-root", "explicit local definition catalog; repeatable")
		f.StringVar(&o.DependencyLock, "dependency-lock", "", "existing exact dependency lock")
	}
	if name == "resolve" {
		f.StringVar(&o.ClosureOutput, "closure-output", "", "write semantic closure here")
	}
	if name == "resolve" || name == "normalize" {
		f.StringVar(&o.DependencyLockOutput, "dependency-lock-output", "", "write resolved dependency lock here")
	}
	if name == "normalize" || name == "generate" || name == "package" || name == "plan-release" {
		f.StringVar(&o.Output, "output", "", "requested final output file or directory")
	}
	if name == "verify" {
		f.StringVar(&o.Input, "input", "", "generated tree root; defaults to committed targets")
	}
	if name == "verify" || name == "check" {
		f.StringVar(&o.Report, "report", "", "write the YAML verification summary here")
	}
	f.StringVar(&o.CoreSchema, "core-schema", "", "explicit core schema file for local development")
	f.StringVar(&o.Go, "go", envOr("RC_BINDINGS_GO", "go"), "native Go command")
	f.StringVar(&o.Python, "python", envOr("RC_BINDINGS_PYTHON", "python3"), "native Python interpreter")
	f.StringVar(&o.Normalizer, "normalizer", os.Getenv("RC_BINDINGS_NORMALIZER"), "installed locked normalizer; development defaults to workspace build")
	f.StringVar(&o.GoEmitter, "go-emitter", os.Getenv("RC_BINDINGS_GO_EMITTER"), "installed locked Go emitter; development defaults to workspace build")
	f.StringVar(&o.GoProfiler, "go-profiler", envOr("RC_GO_PROFILER_BIN", "go-rc-profiler"), "independently installed Go profiler CLI")
	f.StringVar(&o.PythonProfiler, "python-profiler", envOr("RC_PYTHON_PROFILER_BIN", "runtimeconditions-python-profiler"), "independently installed Python profiler CLI")
	f.StringVar(&o.PythonProfilerArtifact, "python-profiler-artifact", os.Getenv("RC_PYTHON_PROFILER_ARTIFACT"), "installed Python profiler wheel for artifact identity verification")
	if err := f.Parse(args[2:]); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if f.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %s", strings.Join(f.Args(), " "))
	}
	if (name == "generate" || name == "package") && o.Output == "" {
		return fmt.Errorf("%s requires --output", name)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	for _, ptr := range []*string{&o.Cache, &o.WorkDir, &o.Output, &o.Input, &o.Report, &o.ClosureOutput, &o.DependencyLockOutput, &o.DependencyLock, &o.CoreSchema, &o.PythonProfilerArtifact} {
		if *ptr != "" {
			*ptr = absolute(cwd, *ptr)
		}
	}
	for i, dir := range o.CatalogRoots {
		o.CatalogRoots[i] = absolute(cwd, dir)
	}
	project, err := DiscoverProject(cwd, o.Packages, o.ToolchainLock, o.ToolingDir)
	if err != nil {
		return err
	}
	pipeline, err := NewPipeline(ctx, project, o)
	if err != nil {
		return err
	}
	defer pipeline.Close()
	outputs := []string{o.Output, o.Report, o.ClosureOutput, o.DependencyLockOutput}
	seenOutputs := map[string]bool{}
	for _, path := range outputs {
		if path == "" {
			continue
		}
		if err = rejectSymlinkPath(path); err != nil {
			return err
		}
		if within(filepath.Join(project.RepositoryRoot, "bindings"), path) {
			return fmt.Errorf("explicit outputs must be outside committed bindings; use update to synchronize package source")
		}
		if seenOutputs[path] {
			return fmt.Errorf("output paths must be different: %s", path)
		}
		seenOutputs[path] = true
		if name != "generate" && name != "package" {
			if _, statErr := os.Lstat(path); !os.IsNotExist(statErr) {
				return fmt.Errorf("output must be a new file: %s", path)
			}
		}
	}
	plan, err := project.SelectTargets(o.Targets)
	if err != nil {
		return err
	}
	if len(o.ExtensionRoots) > 0 && name != "resolve" && name != "normalize" {
		roots := map[string]bool{}
		for _, root := range o.ExtensionRoots {
			roots[root] = true
		}
		filtered := BuildPlan{Targets: []Target{}}
		foundRoots := map[string]bool{}
		for _, target := range plan.Targets {
			if roots[target.Set.RootExtension] {
				filtered.Targets = append(filtered.Targets, target)
				foundRoots[target.Set.RootExtension] = true
			}
		}
		for root := range foundRoots {
			delete(roots, root)
		}
		if len(roots) != 0 {
			return fmt.Errorf("extension roots require package delivery metadata in the selected catalog targets: %s", strings.Join(sortedKeys(roots), ", "))
		}
		plan = filtered
	}
	switch name {
	case "resolve", "normalize":
		documents, err := pipeline.Resolve(plan)
		if err != nil {
			return err
		}
		for i, doc := range documents {
			if i > 0 {
				fmt.Fprintln(stdout, "---")
			}
			var data []byte
			if name == "resolve" {
				data, err = yaml.Marshal(doc.SemanticClosure())
			} else {
				if err = pipeline.Normalize(doc); err == nil {
					data, err = doc.ModelYAML()
				}
			}
			if err != nil {
				return err
			}
			if _, err = stdout.Write(data); err != nil {
				return err
			}
			if name == "resolve" {
				lock, err := doc.LockYAML()
				if err != nil {
					return err
				}
				fmt.Fprintln(stdout, "---")
				if _, err = stdout.Write(lock); err != nil {
					return err
				}
			}
		}
		if o.ClosureOutput != "" || o.DependencyLockOutput != "" || o.Output != "" {
			if len(documents) != 1 {
				return fmt.Errorf("file outputs require one extension root; use --extension-root or --target")
			}
			doc := documents[0]
			if o.ClosureOutput != "" {
				data, _ := yaml.Marshal(doc.SemanticClosure())
				if err = writeNewFile(o.ClosureOutput, data); err != nil {
					return err
				}
			}
			if o.Output != "" {
				data, err := doc.ModelYAML()
				if err != nil {
					return err
				}
				if err = writeNewFile(o.Output, data); err != nil {
					return err
				}
			}
			if o.DependencyLockOutput != "" {
				data, err := doc.LockYAML()
				if err != nil {
					return err
				}
				if err = writeNewFile(o.DependencyLockOutput, data); err != nil {
					return err
				}
			}
		}
		return nil
	case "generate":
		if err = validateEmptyDirectory(o.Output); err != nil {
			return err
		}
		build, err := pipeline.Generate(plan)
		if err != nil {
			return err
		}
		if err = prepareEmptyDirectory(o.Output); err != nil {
			return err
		}
		return build.CopySelected(o.Output)
	case "plan-release":
		release, err := pipeline.PlanRelease(plan)
		if err != nil {
			return err
		}
		data, err := yaml.Marshal(release)
		if err != nil {
			return err
		}
		if _, err = stdout.Write(data); err != nil {
			return err
		}
		if o.Output != "" {
			return writeNewFile(o.Output, data)
		}
		return nil
	case "verify", "check", "package", "update":
		var build *Build
		if name == "verify" {
			build, err = pipeline.LoadBuild(plan, o.Input)
		} else {
			build, err = pipeline.Generate(plan)
		}
		if err != nil {
			failure := VerificationSummary{Status: "failed", Targets: []TargetResult{}}
			failure.Fail("build", err)
			return writeSummary(name, o, failure, err, stdout, stderr)
		}
		summary, err := pipeline.Verify(build)
		if err == nil && (name == "check" || name == "package") {
			err = build.CompareCommitted()
			if err != nil {
				summary.Fail("committed-source", err)
			}
		}
		if err == nil && name == "update" {
			err = build.Update()
			if err != nil {
				summary.Fail("update", err)
			}
		}
		if err == nil && name == "package" {
			committed, loadErr := pipeline.LoadBuild(plan, "")
			if loadErr != nil {
				err = loadErr
			} else {
				_, err = pipeline.Verify(committed)
				if err == nil {
					err = pipeline.Package(committed, o.Output)
				}
			}
			if err != nil {
				summary.Fail("package", err)
			}
		}
		return writeSummary(name, o, summary, err, stdout, stderr)
	}
	return fmt.Errorf("unhandled bindings command %s", name)
}

func writeSummary(name string, o Options, summary VerificationSummary, result error, stdout, stderr io.Writer) error {
	data, marshalErr := yaml.Marshal(summary)
	if marshalErr != nil {
		return marshalErr
	}
	if _, writeErr := stdout.Write(data); writeErr != nil {
		return writeErr
	}
	if o.Report != "" {
		if writeErr := writeNewFile(o.Report, data); writeErr != nil {
			return writeErr
		}
	}
	fmt.Fprintf(stderr, "bindings %s: %s (%d targets)\n", name, summary.Status, len(summary.Targets))
	return result
}
func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
func writeNewFile(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	_, err = file.Write(data)
	closeErr := file.Close()
	if err != nil {
		return err
	}
	return closeErr
}
func prepareEmptyDirectory(path string) error {
	if err := validateEmptyDirectory(path); err != nil {
		return err
	}
	return os.MkdirAll(path, 0755)
}
func validateEmptyDirectory(path string) error {
	if err := rejectSymlinkPath(path); err != nil {
		return err
	}
	if info, err := os.Stat(path); err == nil {
		if !info.IsDir() {
			return fmt.Errorf("output is not a directory: %s", path)
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			return err
		}
		if len(entries) > 0 {
			return fmt.Errorf("output must be new or empty: %s", path)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}
func rejectSymlinkPath(path string) error {
	for current := filepath.Clean(path); ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err == nil && info.Mode()&os.ModeSymlink != 0 && current != "/var" && current != "/tmp" {
			return fmt.Errorf("symbolic-link path rejected: %s", current)
		}
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if filepath.Dir(current) == current {
			return nil
		}
	}
}
