package normalizer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
)

func fixtureExtension(id string, dependencies []string, specBody string) []byte {
	var dependencyYAML strings.Builder
	if len(dependencies) > 0 {
		dependencyYAML.WriteString("  dependencies:\n")
		for _, dependency := range dependencies {
			dependencyYAML.WriteString("    - ")
			dependencyYAML.WriteString(dependency)
			dependencyYAML.WriteByte('\n')
		}
	}
	return []byte(fmt.Sprintf("apiVersion: runtimeconditions.io/v1alpha1\nkind: RuntimeConditionsExtensionDefinition\nmetadata:\n  id: %s\n  version: 1.0.0\nspec:\n%s%s", id, dependencyYAML.String(), specBody))
}

func kindSpec(name string) string {
	return fmt.Sprintf("  kinds:\n    - name: %s\n", name)
}

func writeFixture(t *testing.T, directory, name string, data []byte) string {
	t.Helper()
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func fixtureResolver(t *testing.T, roots ...string) *Resolver {
	t.Helper()
	resolver, err := NewResolver(ResolverConfig{Schemas: testSchemas(t), CatalogRoots: roots})
	if err != nil {
		t.Fatalf("new resolver: %v", err)
	}
	return resolver
}

func diagnosticCode(t *testing.T, err error) string {
	t.Helper()
	diagnosticError, ok := err.(*DiagnosticError)
	if !ok {
		t.Fatalf("expected diagnostic error, got %T: %v", err, err)
	}
	return diagnosticError.Diagnostic.Code
}

func closureIDs(closure ResolvedClosure) []string {
	ids := make([]string, 0, len(closure.Documents))
	for _, document := range closure.Documents {
		ids = append(ids, document.Definition.Metadata.ID)
	}
	return ids
}

func TestResolverDirectDependencyOrder(t *testing.T) {
	directory := t.TempDir()
	rootID := "urn:runtimeconditions:test:direct:root"
	dependencyID := "urn:runtimeconditions:test:direct:dependency"
	writeFixture(t, directory, "root.yaml", fixtureExtension(rootID, []string{dependencyID}, kindSpec("root")))
	writeFixture(t, directory, "dependency.yaml", fixtureExtension(dependencyID, nil, kindSpec("dependency")))
	want := strings.Join([]string{dependencyID, rootID}, "\n")
	for iteration := 0; iteration < 100; iteration++ {
		closure, err := fixtureResolver(t, directory).Resolve(context.Background(), rootID)
		if err != nil {
			t.Fatalf("iteration %d: %v", iteration, err)
		}
		if got := strings.Join(closureIDs(closure), "\n"); got != want {
			t.Fatalf("iteration %d order:\n%s\nwant:\n%s", iteration, got, want)
		}
	}
}

func TestResolverDeterministicDependencyOrder(t *testing.T) {
	directory := t.TempDir()
	rootID := "urn:runtimeconditions:test:order:root"
	bID := "urn:runtimeconditions:test:order:b"
	cID := "urn:runtimeconditions:test:order:c"
	dID := "urn:runtimeconditions:test:order:d"
	writeFixture(t, directory, "root.yaml", fixtureExtension(rootID, []string{cID, bID}, kindSpec("root")))
	writeFixture(t, directory, "b.yaml", fixtureExtension(bID, []string{dID}, kindSpec("b")))
	writeFixture(t, directory, "c.yaml", fixtureExtension(cID, nil, kindSpec("c")))
	writeFixture(t, directory, "d.yaml", fixtureExtension(dID, nil, kindSpec("d")))
	want := strings.Join([]string{cID, dID, bID, rootID}, "\n")
	for iteration := 0; iteration < 100; iteration++ {
		closure, err := fixtureResolver(t, directory).Resolve(context.Background(), rootID)
		if err != nil {
			t.Fatalf("iteration %d: %v", iteration, err)
		}
		if got := strings.Join(closureIDs(closure), "\n"); got != want {
			t.Fatalf("iteration %d order:\n%s\nwant:\n%s", iteration, got, want)
		}
	}
}

func TestResolverThreeLevelClosure(t *testing.T) {
	directory := t.TempDir()
	rootID := "urn:runtimeconditions:test:chain:root"
	middleID := "urn:runtimeconditions:test:chain:middle"
	leafID := "urn:runtimeconditions:test:chain:leaf"
	writeFixture(t, directory, "root.yaml", fixtureExtension(rootID, []string{middleID}, kindSpec("root")))
	writeFixture(t, directory, "middle.yaml", fixtureExtension(middleID, []string{leafID}, kindSpec("middle")))
	writeFixture(t, directory, "leaf.yaml", fixtureExtension(leafID, nil, kindSpec("leaf")))
	want := strings.Join([]string{leafID, middleID, rootID}, "\n")
	for iteration := 0; iteration < 100; iteration++ {
		closure, err := fixtureResolver(t, directory).Resolve(context.Background(), rootID)
		if err != nil {
			t.Fatalf("iteration %d: %v", iteration, err)
		}
		if got := strings.Join(closureIDs(closure), "\n"); got != want {
			t.Fatalf("iteration %d order:\n%s\nwant:\n%s", iteration, got, want)
		}
	}
}

func TestResolverDependencyFailures(t *testing.T) {
	tests := []struct {
		name      string
		documents map[string][]byte
		root      string
		code      string
	}{
		{
			name: "two node cycle", root: "urn:runtimeconditions:test:cycle2:a", code: "RCB1204",
			documents: map[string][]byte{
				"a.yaml": fixtureExtension("urn:runtimeconditions:test:cycle2:a", []string{"urn:runtimeconditions:test:cycle2:b"}, kindSpec("a")),
				"b.yaml": fixtureExtension("urn:runtimeconditions:test:cycle2:b", []string{"urn:runtimeconditions:test:cycle2:a"}, kindSpec("b")),
			},
		},
		{
			name: "three node cycle", root: "urn:runtimeconditions:test:cycle3:a", code: "RCB1204",
			documents: map[string][]byte{
				"a.yaml": fixtureExtension("urn:runtimeconditions:test:cycle3:a", []string{"urn:runtimeconditions:test:cycle3:b"}, kindSpec("a")),
				"b.yaml": fixtureExtension("urn:runtimeconditions:test:cycle3:b", []string{"urn:runtimeconditions:test:cycle3:c"}, kindSpec("b")),
				"c.yaml": fixtureExtension("urn:runtimeconditions:test:cycle3:c", []string{"urn:runtimeconditions:test:cycle3:a"}, kindSpec("c")),
			},
		},
		{
			name: "missing direct", root: "urn:runtimeconditions:test:missing-direct:root", code: "RCB1207",
			documents: map[string][]byte{
				"root.yaml": fixtureExtension("urn:runtimeconditions:test:missing-direct:root", []string{"urn:runtimeconditions:test:missing-direct:absent"}, kindSpec("root")),
			},
		},
		{
			name: "missing transitive", root: "urn:runtimeconditions:test:missing-transitive:root", code: "RCB1207",
			documents: map[string][]byte{
				"root.yaml":   fixtureExtension("urn:runtimeconditions:test:missing-transitive:root", []string{"urn:runtimeconditions:test:missing-transitive:middle"}, kindSpec("root")),
				"middle.yaml": fixtureExtension("urn:runtimeconditions:test:missing-transitive:middle", []string{"urn:runtimeconditions:test:missing-transitive:absent"}, kindSpec("middle")),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			for name, data := range test.documents {
				writeFixture(t, directory, name, data)
			}
			_, err := fixtureResolver(t, directory).Resolve(context.Background(), test.root)
			if err == nil {
				t.Fatal("expected resolution failure")
			}
			if got := diagnosticCode(t, err); got != test.code {
				t.Fatalf("diagnostic code %s, want %s", got, test.code)
			}
		})
	}
}

func TestResolverRejectsDuplicateIdentifierBytes(t *testing.T) {
	id := "urn:runtimeconditions:test:duplicate-id"
	first := t.TempDir()
	second := t.TempDir()
	writeFixture(t, first, "extension.yaml", fixtureExtension(id, nil, kindSpec("first")))
	writeFixture(t, second, "extension.yaml", fixtureExtension(id, nil, kindSpec("second")))
	_, err := NewResolver(ResolverConfig{Schemas: testSchemas(t), CatalogRoots: []string{first, second}})
	if err == nil || diagnosticCode(t, err) != "RCB1202" {
		t.Fatalf("expected RCB1202, got %v", err)
	}
}

func TestResolverRejectsIdenticalVocabularyConflict(t *testing.T) {
	directory := t.TempDir()
	rootID := "urn:runtimeconditions:test:vocabulary:root"
	firstID := "urn:runtimeconditions:test:vocabulary:first"
	secondID := "urn:runtimeconditions:test:vocabulary:second"
	writeFixture(t, directory, "root.yaml", fixtureExtension(rootID, []string{firstID, secondID}, kindSpec("root")))
	writeFixture(t, directory, "first.yaml", fixtureExtension(firstID, nil, kindSpec("shared")))
	writeFixture(t, directory, "second.yaml", fixtureExtension(secondID, nil, kindSpec("shared")))
	_, err := fixtureResolver(t, directory).Resolve(context.Background(), rootID)
	if err == nil || diagnosticCode(t, err) != "RCB1221" {
		t.Fatalf("expected RCB1221, got %v", err)
	}
}

func TestResolverRejectsInvalidFieldValuesPath(t *testing.T) {
	directory := t.TempDir()
	id := "urn:runtimeconditions:test:invalid-field-values-path"
	document := fixtureExtension(id, nil, `  kinds:
    - name: service
  fieldValues:
    - field: entries[0].value
      targetKind: service
      values: [one]
`)
	writeFixture(t, directory, "extension.yaml", document)
	_, err := fixtureResolver(t, directory).Resolve(context.Background(), id)
	if err == nil || diagnosticCode(t, err) != "RCB1235" {
		t.Fatalf("expected RCB1235, got %v", err)
	}
}

type fakeOCIFetcher struct {
	data    []byte
	locator string
}

type staticHTTPTransport struct {
	data []byte
}

func (transport staticHTTPTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Body:       io.NopCloser(bytes.NewReader(transport.data)),
		Request:    request,
		Header:     make(http.Header),
	}, nil
}

func (fetcher fakeOCIFetcher) Fetch(_ context.Context, _ string, lock LockEntry) ([]byte, string, error) {
	locator := fetcher.locator
	if locator == "" {
		locator = lock.Locator
	}
	return append([]byte(nil), fetcher.data...), locator, nil
}

func TestResolverBackendsProduceOneSemanticModel(t *testing.T) {
	id := "urn:runtimeconditions:test:backends"
	data := fixtureExtension(id, nil, kindSpec("backend"))
	sourceDigest := SHA256Hex(data)
	directory := t.TempDir()
	path := writeFixture(t, directory, "extension.yaml", data)
	schemas := testSchemas(t)

	ociDigest := sha256.Sum256(data)
	ociLocator := fmt.Sprintf("oci://registry.invalid/runtimeconditions/extension@sha256:%x", ociDigest)
	fileLocator := (&url.URL{Scheme: "file", Path: path}).String()
	httpsLocator := "https://resolver.invalid/runtimeconditions.extension.yaml"

	configs := map[string]ResolverConfig{
		"override": {Schemas: schemas, Overrides: map[string]string{id: path}},
		"catalog":  {Schemas: schemas, CatalogRoots: []string{directory}},
		"file": {
			Schemas: schemas,
			Locks:   map[string]LockEntry{id: {SourceSHA256: sourceDigest, Locator: fileLocator}},
		},
		"https": {
			Schemas: schemas, Network: true, HTTPClient: &http.Client{Transport: staticHTTPTransport{data: data}},
			Locks: map[string]LockEntry{id: {SourceSHA256: sourceDigest, Locator: httpsLocator}},
		},
		"oci": {
			Schemas: schemas, Network: true, OCIFetcher: fakeOCIFetcher{data: data},
			Locks: map[string]LockEntry{id: {SourceSHA256: sourceDigest, Locator: ociLocator}},
		},
	}

	var expected []byte
	names := make([]string, 0, len(configs))
	for name := range configs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		resolver, err := NewResolver(configs[name])
		if err != nil {
			t.Fatalf("%s resolver: %v", name, err)
		}
		closure, err := resolver.Resolve(context.Background(), id)
		if err != nil {
			t.Fatalf("%s resolve: %v", name, err)
		}
		if len(closure.Documents) != 1 || closure.Documents[0].Backend != name || closure.Documents[0].Locator == "" {
			t.Fatalf("%s resolution provenance = %#v", name, closure.Documents)
		}
		model, err := Normalize(closure, BuildDependencyLock(closure), schemas, testNormalizeConfig())
		if err != nil {
			t.Fatalf("%s normalize: %v", name, err)
		}
		encoded, err := CanonicalModelYAML(model)
		if err != nil {
			t.Fatal(err)
		}
		if expected == nil {
			expected = encoded
		} else if string(encoded) != string(expected) {
			t.Fatalf("%s backend changed semantic model bytes", name)
		}
	}
}

type countingTransport struct {
	requests atomic.Int64
}

func (transport *countingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	transport.requests.Add(1)
	return nil, fmt.Errorf("request should not be sent")
}

func TestNetworkDisabledPerformsNoRequest(t *testing.T) {
	id := "urn:runtimeconditions:test:network-disabled"
	transport := &countingTransport{}
	resolver, err := NewResolver(ResolverConfig{
		Schemas: testSchemas(t), Network: false,
		HTTPClient: &http.Client{Transport: transport},
		Locks: map[string]LockEntry{id: {
			SourceSHA256: strings.Repeat("a", 64), Locator: "https://example.invalid/extension.yaml",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = resolver.Resolve(context.Background(), id)
	if err == nil || diagnosticCode(t, err) != "RCB1208" {
		t.Fatalf("expected RCB1208, got %v", err)
	}
	if requests := transport.requests.Load(); requests != 0 {
		t.Fatalf("performed %d requests with network disabled", requests)
	}
}

func TestMutableOCILockResolvesToImmutableProvenance(t *testing.T) {
	id := "urn:runtimeconditions:test:mutable-oci"
	data := fixtureExtension(id, nil, kindSpec("oci"))
	sourceDigest := SHA256Hex(data)
	mutableLocator := "oci://registry.invalid/runtimeconditions/extensions/test:stable"
	immutableLocator := "oci://registry.invalid/runtimeconditions/extensions/test@sha256:" + strings.Repeat("a", 64)
	schemas := testSchemas(t)
	resolver, err := NewResolver(ResolverConfig{
		Schemas: schemas,
		Network: true,
		Locks: map[string]LockEntry{id: {
			SourceSHA256: sourceDigest,
			Locator:      mutableLocator,
		}},
		OCIFetcher: fakeOCIFetcher{data: data, locator: immutableLocator},
	})
	if err != nil {
		t.Fatal(err)
	}
	closure, err := resolver.Resolve(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if closure.Documents[0].Locator != immutableLocator {
		t.Fatalf("resolved locator = %q, want %q", closure.Documents[0].Locator, immutableLocator)
	}
	suppliedLock := BuildDependencyLock(closure)
	suppliedLock.Extensions[0].SourceLocator = mutableLocator
	if err := ValidateDependencyLock(closure, suppliedLock); err != nil {
		t.Fatalf("validate mutable OCI lookup lock: %v", err)
	}
	resolvedLock := BuildDependencyLock(closure)
	if resolvedLock.Extensions[0].SourceLocator != immutableLocator {
		t.Fatalf("resolved lock locator = %q, want %q", resolvedLock.Extensions[0].SourceLocator, immutableLocator)
	}
	if _, err := Normalize(closure, resolvedLock, schemas, testNormalizeConfig()); err != nil {
		t.Fatal(err)
	}
}

func TestContentAddressedCacheVerifiesEntryDigest(t *testing.T) {
	id := "urn:runtimeconditions:test:content-addressed-cache"
	data := fixtureExtension(id, nil, kindSpec("cache"))
	digest := SHA256Hex(data)
	cacheDirectory := t.TempDir()
	writeFixture(t, cacheDirectory, digest+".yaml", data)

	resolver, err := NewResolver(ResolverConfig{Schemas: testSchemas(t), CacheDir: cacheDirectory})
	if err != nil {
		t.Fatal(err)
	}
	closure, err := resolver.Resolve(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if len(closure.Documents) != 1 || closure.Documents[0].Backend != "cache" || closure.Documents[0].Locator != "cache:sha256:"+digest {
		t.Fatalf("cache provenance = %#v", closure.Documents)
	}

	tamperedDirectory := t.TempDir()
	writeFixture(t, tamperedDirectory, digest+".yaml", append(append([]byte(nil), data...), []byte("# changed bytes\n")...))
	_, err = NewResolver(ResolverConfig{Schemas: testSchemas(t), CacheDir: tamperedDirectory})
	if err == nil || diagnosticCode(t, err) != "RCB1236" {
		t.Fatalf("expected RCB1236, got %v", err)
	}
}

func TestResolverRejectsAnchorsAndNonPointerReferences(t *testing.T) {
	tests := []struct {
		name   string
		schema string
		code   string
	}{
		{
			name: "anchor declaration",
			schema: `        $anchor: target
        type: string
`,
			code: "RCB1115",
		},
		{
			name: "anchor reference",
			schema: `        $ref: '#target'
`,
			code: "RCB1113",
		},
		{
			name: "invalid JSON Pointer escape",
			schema: `        $ref: '#/$defs/a~2b'
`,
			code: "RCB1113",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			id := "urn:runtimeconditions:test:references:" + strings.ReplaceAll(test.name, " ", "-")
			document := fixtureExtension(id, nil, `  schemas:
    - id: references
      description: Reference validation fixture.
      schema:
`+test.schema)
			directory := t.TempDir()
			writeFixture(t, directory, "extension.yaml", document)
			_, err := fixtureResolver(t, directory).Resolve(context.Background(), id)
			if err == nil || diagnosticCode(t, err) != test.code {
				t.Fatalf("expected %s, got %v", test.code, err)
			}
		})
	}
}

func TestSemanticAndSourceDigestOrdering(t *testing.T) {
	id := "urn:runtimeconditions:test:digest-ordering"
	first := []byte("apiVersion: runtimeconditions.io/v1alpha1\nkind: RuntimeConditionsExtensionDefinition\nmetadata:\n  id: urn:runtimeconditions:test:digest-ordering\n  version: 1.0.0\nspec:\n  kinds:\n    - name: beta\n    - name: alpha\n")
	second := []byte("kind: RuntimeConditionsExtensionDefinition\napiVersion: runtimeconditions.io/v1alpha1\nmetadata: {version: '1.0.0', id: urn:runtimeconditions:test:digest-ordering}\nspec:\n  kinds: [{name: alpha}, {name: beta}]\n")
	firstModel, firstLock := normalizeSingleSource(t, id, first)
	secondModel, secondLock := normalizeSingleSource(t, id, second)
	if firstLock.Extensions[0].SourceSHA256 == secondLock.Extensions[0].SourceSHA256 {
		t.Fatal("presentation changes did not change source digest")
	}
	if firstLock.Extensions[0].SemanticSHA256 != secondLock.Extensions[0].SemanticSHA256 {
		t.Fatal("presentation and set ordering changes changed semantic digest")
	}
	if string(firstModel) != string(secondModel) {
		t.Fatal("presentation and set ordering changes changed model bytes")
	}

	sourceFirst := []byte("apiVersion: runtimeconditions.io/v1alpha1\nkind: RuntimeConditionsExtensionDefinition\nmetadata:\n  id: urn:runtimeconditions:test:digest-ordering\n  version: 1.0.0\nspec:\n  kinds: [{name: alpha}]\n  schemas:\n    - id: examples\n      description: Ordered examples\n      schema:\n        type: string\n        examples: [first, second]\n")
	sourceSecond := []byte(strings.Replace(string(sourceFirst), "examples: [first, second]", "examples: [second, first]", 1))
	firstModel, firstLock = normalizeSingleSource(t, id, sourceFirst)
	secondModel, secondLock = normalizeSingleSource(t, id, sourceSecond)
	if firstLock.Extensions[0].SourceSHA256 == secondLock.Extensions[0].SourceSHA256 ||
		firstLock.Extensions[0].SemanticSHA256 == secondLock.Extensions[0].SemanticSHA256 ||
		string(firstModel) == string(secondModel) {
		t.Fatal("source-ordered sequence change did not change source, semantic, and model identities")
	}
}

func normalizeSingleSource(t *testing.T, id string, data []byte) ([]byte, DependencyLock) {
	t.Helper()
	directory := t.TempDir()
	path := writeFixture(t, directory, "extension.yaml", data)
	schemas := testSchemas(t)
	resolver, err := NewResolver(ResolverConfig{Schemas: schemas, Overrides: map[string]string{id: path}})
	if err != nil {
		t.Fatal(err)
	}
	closure, err := resolver.Resolve(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	lock := BuildDependencyLock(closure)
	model, err := Normalize(closure, lock, schemas, testNormalizeConfig())
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := CanonicalModelYAML(model)
	if err != nil {
		t.Fatal(err)
	}
	return encoded, lock
}

func TestDependencyLockMustMatchClosure(t *testing.T) {
	id := "urn:runtimeconditions:test:lock"
	data := fixtureExtension(id, nil, kindSpec("lock"))
	directory := t.TempDir()
	path := writeFixture(t, directory, "extension.yaml", data)
	schemas := testSchemas(t)
	resolver, err := NewResolver(ResolverConfig{Schemas: schemas, Overrides: map[string]string{id: path}})
	if err != nil {
		t.Fatal(err)
	}
	closure, err := resolver.Resolve(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		lock DependencyLock
		code string
	}{
		{name: "missing", lock: DependencyLock{}, code: "RCB1223"},
		{name: "extra", lock: DependencyLock{Extensions: append(BuildDependencyLock(closure).Extensions, DependencyLockEntry{ID: "urn:runtimeconditions:test:extra"})}, code: "RCB1226"},
		{name: "semantic mismatch", lock: func() DependencyLock {
			lock := BuildDependencyLock(closure)
			lock.Extensions[0].SemanticSHA256 = strings.Repeat("f", 64)
			return lock
		}(), code: "RCB1224"},
		{name: "source mismatch", lock: func() DependencyLock {
			lock := BuildDependencyLock(closure)
			lock.Extensions[0].SourceSHA256 = strings.Repeat("f", 64)
			return lock
		}(), code: "RCB1225"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Normalize(closure, test.lock, schemas, testNormalizeConfig())
			if err == nil || diagnosticCode(t, err) != test.code {
				t.Fatalf("expected %s, got %v", test.code, err)
			}
		})
	}
}
