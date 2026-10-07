package orchestrator

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/runtimeconditions/extensions/tooling/extension-bindings/normalizer"
)

func TestRequiredVersion(t *testing.T) {
	for _, test := range []struct{ old, impact, want string }{{"1.2.3", "major", "2.0.0"}, {"1.2.3", "minor", "1.3.0"}, {"1.2.3", "patch", "1.2.4"}, {"0.7.3", "major", "0.8.0"}, {"0.7.3", "minor", "0.7.4"}, {"0.7.3", "patch", "0.7.4"}, {"0.7.3", "none", "0.7.3"}} {
		actual, err := requiredVersion(test.old, test.impact)
		if err != nil || actual != test.want {
			t.Fatalf("%+v: %s %v", test, actual, err)
		}
	}
}
func TestSchemaCompatibility(t *testing.T) {
	tests := []struct {
		a, b map[string]any
		want string
	}{{map[string]any{"type": "string", "minLength": 2}, map[string]any{"type": "string", "minLength": 1}, "minor"}, {map[string]any{"type": "string", "maxLength": 5}, map[string]any{"type": "string", "maxLength": 3}, "major"}, {map[string]any{"enum": []any{"a"}}, map[string]any{"enum": []any{"a", "b"}}, "minor"}, {map[string]any{"enum": []any{"a", "b"}}, map[string]any{"enum": []any{"a"}}, "major"}, {map[string]any{"type": "string", "pattern": "a"}, map[string]any{"type": "string", "pattern": "b"}, "major"}}
	for _, test := range tests {
		if result := schemaImpact(test.a, test.b); result != test.want {
			t.Fatalf("%v -> %v: %s", test.a, test.b, result)
		}
	}
}
func TestPreviousReleaseIsAutomaticallySelected(t *testing.T) {
	p, o := testProject(t, "go")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[{"tag_name":"bindings/other/go/v9.0.0","published_at":"2026-01-01"},{"tag_name":"bindings/new-channel/go/v0.6.0","published_at":"2026-01-01"},{"tag_name":"bindings/new-channel/go/v0.7.0","published_at":"2026-02-01"},{"tag_name":"bindings/new-channel/go/v0.8.0","published_at":"2026-03-01","draft":true}]`)
	}))
	defer server.Close()
	t.Setenv("RC_BINDINGS_GITHUB_API", server.URL)
	pipeline, err := NewPipeline(context.Background(), p, o)
	if err != nil {
		t.Fatal(err)
	}
	defer pipeline.Close()
	plan, _ := p.SelectTargets(nil)
	release, err := pipeline.previousRelease(plan.Targets[0])
	if err != nil {
		t.Fatal(err)
	}
	if release.TagName != "bindings/new-channel/go/v0.7.0" {
		t.Fatal(release.TagName)
	}
}
func TestUnchangedCompatibilityNeedsNoRelease(t *testing.T) {
	model := normalizer.BindingModel{}
	manifest := map[string]any{"package": map[string]any{"coordinate": "example.test/a", "name": "a", "minimumGoVersion": "1.26"}}
	impact, _ := ClassifyCompatibility(model, model, []string{"type|A|string"}, []string{"type|A|string"}, manifest, manifest, map[string]any{}, map[string]any{})
	if impact != "none" {
		t.Fatal(impact)
	}
}

func TestNativeAPIBreakIsNotMaskedByMetadataChange(t *testing.T) {
	old := normalizer.BindingModel{}
	new := old
	new.Metadata.SemanticSHA256 = "changed"
	manifest := map[string]any{"package": map[string]any{"coordinate": "example.test/a", "name": "a"}}
	impact, _ := ClassifyCompatibility(old, new, []string{"function|future|value: str|Declaration"}, []string{"function|future|value: int|Declaration"}, manifest, manifest, map[string]any{}, map[string]any{})
	if impact != "major" {
		t.Fatal("API break was masked", impact)
	}
}

func TestChangedExtensionVersionKeepsSchemaCoordinatesComparable(t *testing.T) {
	old := normalizer.BindingModel{Extensions: []normalizer.ResolvedExtension{{ID: "https://new.example.test/future:1.0.0", Version: "1.0.0"}}, Schemas: []normalizer.NormalizedSchema{{Coordinate: "https://new.example.test/future:1.0.0#schema:channel", Exact: map[string]any{"type": "string"}}}}
	new := normalizer.BindingModel{Extensions: []normalizer.ResolvedExtension{{ID: "https://new.example.test/future:1.0.1", Version: "1.0.1"}}, Schemas: []normalizer.NormalizedSchema{{Coordinate: "https://new.example.test/future:1.0.1#schema:channel", Exact: map[string]any{"type": "string"}}}}
	new.Metadata.SemanticSHA256 = "changed"
	manifest := map[string]any{"package": map[string]any{"coordinate": "example.test/a", "name": "a"}}
	impact, _ := ClassifyCompatibility(old, new, nil, nil, manifest, manifest, map[string]any{}, map[string]any{})
	if impact != "patch" {
		t.Fatal("version change hid an unchanged schema", impact)
	}
}

func TestPreviousTargetFoundInSharedCommitRelease(t *testing.T) {
	p, o := testProject(t, "python")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `[{"tag_name":"bindings/releases/abcdef","published_at":"2026-01-01","assets":[{"name":"new-channel-python-0.6.1.source.zip","browser_download_url":"https://example.test/source.zip"},{"name":"other-python-9.0.0.source.zip","browser_download_url":"https://example.test/other.zip"}]}]`)
	}))
	defer server.Close()
	t.Setenv("RC_BINDINGS_GITHUB_API", server.URL)
	pipeline, err := NewPipeline(context.Background(), p, o)
	if err != nil {
		t.Fatal(err)
	}
	defer pipeline.Close()
	plan, _ := p.SelectTargets(nil)
	release, err := pipeline.previousRelease(plan.Targets[0])
	if err != nil || release == nil || release.TagName != "bindings/new-channel/python/v0.6.1" {
		t.Fatalf("shared target release not found: %+v %v", release, err)
	}
}
