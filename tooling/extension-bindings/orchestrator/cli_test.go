package orchestrator

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRunAcceptsArgumentsWithAndWithoutBindingsPrefix(t *testing.T) {
	for _, args := range [][]string{{"bindings", "resolve", "--help"}, {"resolve", "--help"}} {
		var out, errout bytes.Buffer
		if err := Run(context.Background(), args, &out, &errout); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if !strings.Contains(errout.String(), "-packages") {
			t.Fatalf("%v: missing common flags", args)
		}
	}
}

func TestRunRejectsMissingOrUnknownCommand(t *testing.T) {
	for _, args := range [][]string{{"bindings"}, {"bindings", "nope"}, {"nope"}} {
		var out, errout bytes.Buffer
		if err := Run(context.Background(), args, &out, &errout); err == nil {
			t.Fatalf("%v accepted", args)
		}
	}
}

func TestModuleIdentityRequiresALinkedModule(t *testing.T) {
	if _, err := ModuleIdentity("example.com/absent", "x"); err == nil {
		t.Fatal("absent module identified")
	}
}

func linkedPipeline(status string, expected, linked Tool) *Pipeline {
	p := &Pipeline{Project: &Project{}, tools: map[string]string{}, identities: map[string]Tool{}}
	p.Project.Toolchain.Status = status
	p.Project.Toolchain.Tools.Orchestrator = expected
	p.Project.Toolchain.Tools.Profilers = map[string]Tool{"go": expected}
	p.Options.BuiltinGoProfiler = true
	p.Options.Hooks.ProfilerIdentity = func(string) (Tool, bool, error) { return linked, true, nil }
	return p
}

func TestDevelopmentBuildPassesAnUnreleasedLockThatPinsADigest(t *testing.T) {
	pinned := Tool{Name: "go-rc-profiler", Version: "v1.0.0", SHA256: "pinned"}
	devel := Tool{Name: "go-rc-profiler", Version: "(devel)", SHA256: "other"}
	p := linkedPipeline("development", pinned, devel)
	if got, err := p.profiler("go"); err != nil || got != devel {
		t.Fatalf("%+v %v", got, err)
	}
	if _, got, err := p.tool("orchestrator"); err != nil || got.Version != "(devel)" {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestReleasedLockRejectsALinkedToolThatDoesNotMatch(t *testing.T) {
	pinned := Tool{Name: "go-rc-profiler", Version: "v1.0.0", SHA256: "pinned"}
	for name, linked := range map[string]Tool{
		"development build": {Name: "go-rc-profiler", Version: "(devel)", SHA256: "x"},
		"other digest":      {Name: "go-rc-profiler", Version: "v1.0.0", SHA256: "other"},
		"other version":     {Name: "go-rc-profiler", Version: "v1.0.1", SHA256: "pinned"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := linkedPipeline("released", pinned, linked).profiler("go"); err == nil || !strings.Contains(err.Error(), "differs from toolchain lock") {
				t.Fatalf("accepted: %v", err)
			}
			if _, _, err := linkedPipeline("released", pinned, linked).tool("orchestrator"); err == nil {
				t.Fatal("orchestrator accepted")
			}
		})
	}
	if got, err := linkedPipeline("released", pinned, pinned).profiler("go"); err != nil || got != pinned {
		t.Fatalf("%+v %v", got, err)
	}
}
