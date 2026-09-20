package normalizer

import (
	"context"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type LockEntry struct {
	SourceSHA256   string
	SemanticSHA256 string
	Locator        string
}

type OCIFetcher interface {
	Fetch(context.Context, string, LockEntry) ([]byte, string, error)
}

type ResolverConfig struct {
	Schemas      *Schemas
	Overrides    map[string]string
	CatalogRoots []string
	PackageRoots []string
	CacheDir     string
	Network      bool
	Locks        map[string]LockEntry
	HTTPClient   *http.Client
	OCIFetcher   OCIFetcher
}

type Resolver struct {
	config     ResolverConfig
	candidates map[string][]sourceCandidate
	loaded     map[string]ResolvedDocument
}

type sourceCandidate struct {
	path    string
	backend string
	locator string
}

func NewResolver(config ResolverConfig) (*Resolver, error) {
	if config.Schemas == nil {
		return nil, fmt.Errorf("resolver requires semantic and model schemas")
	}
	if config.HTTPClient == nil {
		config.HTTPClient = secureHTTPClient()
	}
	if config.OCIFetcher == nil {
		config.OCIFetcher = registryOCIFetcher{client: config.HTTPClient}
	}
	resolver := &Resolver{
		config:     config,
		candidates: map[string][]sourceCandidate{},
		loaded:     map[string]ResolvedDocument{},
	}
	for id, path := range config.Overrides {
		if err := resolver.addFileCandidate(id, path, "override", "override:"+id); err != nil {
			return nil, err
		}
	}
	for _, root := range config.CatalogRoots {
		if err := resolver.indexRoot(root, "catalog"); err != nil {
			return nil, err
		}
	}
	for _, root := range config.PackageRoots {
		if err := resolver.indexRoot(root, "package"); err != nil {
			return nil, err
		}
	}
	if config.CacheDir != "" {
		if err := resolver.indexCache(config.CacheDir); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}
	if err := resolver.rejectCandidateConflicts(); err != nil {
		return nil, err
	}
	return resolver, nil
}

func secureHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	transport.DialContext = (&net.Dialer{Timeout: 10 * time.Second}).DialContext
	return &http.Client{
		Transport: transport,
		Timeout:   60 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return fmt.Errorf("HTTPS redirects are forbidden")
		},
	}
}

func BuildDependencyLock(closure ResolvedClosure) DependencyLock {
	lock := DependencyLock{Extensions: make([]DependencyLockEntry, 0, len(closure.Documents))}
	for _, document := range closure.Documents {
		dependencies := append([]string(nil), document.Definition.Spec.Dependencies...)
		sort.Strings(dependencies)
		lock.Extensions = append(lock.Extensions, DependencyLockEntry{
			ID:             document.Definition.Metadata.ID,
			Version:        document.Definition.Metadata.Version,
			SourceSHA256:   document.SourceSHA256,
			SemanticSHA256: document.SemanticSHA256,
			SourceBackend:  document.Backend,
			SourceLocator:  document.Locator,
			Dependencies:   dependencies,
		})
	}
	return lock
}

func ValidateDependencyLock(closure ResolvedClosure, lock DependencyLock) error {
	expected := make(map[string]ResolvedDocument, len(closure.Documents))
	for _, document := range closure.Documents {
		expected[document.Definition.Metadata.ID] = document
	}
	actual := make(map[string]DependencyLockEntry, len(lock.Extensions))
	for _, entry := range lock.Extensions {
		if _, duplicate := actual[entry.ID]; duplicate {
			return diagnostic("extension-dependency", "RCB1222", entry.ID, "", "dependency lock contains a duplicate entry")
		}
		actual[entry.ID] = entry
	}
	ids := make([]string, 0, len(expected))
	for id := range expected {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		document := expected[id]
		entry, exists := actual[id]
		if !exists {
			return diagnostic("extension-dependency", "RCB1223", id, "", "dependency lock is missing an extension")
		}
		expectedDependencies := sortedStrings(document.Definition.Spec.Dependencies)
		actualDependencies := sortedStrings(entry.Dependencies)
		if entry.Version != document.Definition.Metadata.Version ||
			entry.SemanticSHA256 != document.SemanticSHA256 ||
			!stringSlicesEqual(actualDependencies, expectedDependencies) {
			return diagnostic("extension-dependency", "RCB1224", id, "", "dependency lock semantic record does not match the resolved closure")
		}
		locatorMatches := entry.SourceLocator == document.Locator
		if !locatorMatches && entry.SourceBackend == "oci" && document.Backend == "oci" {
			locatorMatches = ociLocatorResolvesTo(entry.SourceLocator, document.Locator)
		}
		if entry.SourceSHA256 != document.SourceSHA256 || entry.SourceBackend != document.Backend || !locatorMatches {
			return diagnostic("extension-dependency", "RCB1225", id, "", "dependency lock source record does not match resolved content")
		}
		delete(actual, id)
	}
	if len(actual) > 0 {
		extra := make([]string, 0, len(actual))
		for id := range actual {
			extra = append(extra, id)
		}
		sort.Strings(extra)
		return diagnostic("extension-dependency", "RCB1226", extra[0], "", "dependency lock contains an extension outside the resolved closure")
	}
	return nil
}

func stringSlicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func (r *Resolver) indexRoot(root, backend string) error {
	root = filepath.Clean(root)
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		extension := strings.ToLower(filepath.Ext(entry.Name()))
		if extension != ".yaml" && extension != ".yml" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		definition, _, err := DecodeExtension(data)
		if err != nil {
			// Non-extension YAML in a catalog is not a resolver candidate.
			return nil
		}
		if definition.Kind != ExtensionKind || definition.Metadata.ID == "" {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		locator := backend + ":" + filepath.ToSlash(relative)
		r.candidates[definition.Metadata.ID] = append(r.candidates[definition.Metadata.ID], sourceCandidate{
			path: path, backend: backend, locator: locator,
		})
		return nil
	})
}

func (r *Resolver) indexCache(root string) error {
	root = filepath.Clean(root)
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		digest, candidate := cacheEntryDigest(entry.Name())
		if !candidate {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if actual := SHA256Hex(data); actual != digest {
			return diagnostic("extension-dependency", "RCB1236", "cache:sha256:"+digest, "", fmt.Sprintf("cached content digest is %s", actual))
		}
		definition, _, err := DecodeExtension(data)
		if err != nil {
			return err
		}
		if definition.Kind != ExtensionKind || definition.Metadata.ID == "" {
			return diagnostic("structural", "RCB1237", "cache:sha256:"+digest, "", "cached content is not an extension definition")
		}
		r.candidates[definition.Metadata.ID] = append(r.candidates[definition.Metadata.ID], sourceCandidate{
			path: path, backend: "cache", locator: "cache:sha256:" + digest,
		})
		return nil
	})
}

func cacheEntryDigest(name string) (string, bool) {
	extension := strings.ToLower(filepath.Ext(name))
	if extension == ".yaml" || extension == ".yml" {
		name = strings.TrimSuffix(name, filepath.Ext(name))
	} else if extension != "" {
		return "", false
	}
	if len(name) != 64 || strings.ToLower(name) != name {
		return "", false
	}
	decoded, err := hex.DecodeString(name)
	return name, err == nil && len(decoded) == 32
}

func (r *Resolver) addFileCandidate(id, path, backend, locator string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	definition, _, err := DecodeExtension(data)
	if err != nil {
		return err
	}
	if definition.Metadata.ID != id {
		return diagnostic("unknown-extension", "RCB1201", id, "/metadata/id", fmt.Sprintf("resolved definition identifies %q", definition.Metadata.ID))
	}
	r.candidates[id] = append(r.candidates[id], sourceCandidate{path: path, backend: backend, locator: locator})
	return nil
}

func (r *Resolver) rejectCandidateConflicts() error {
	ids := make([]string, 0, len(r.candidates))
	for id := range r.candidates {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		candidates := r.candidates[id]
		digests := map[string]struct{}{}
		for _, candidate := range candidates {
			data, err := os.ReadFile(candidate.path)
			if err != nil {
				return err
			}
			digests[SHA256Hex(data)] = struct{}{}
		}
		if len(digests) > 1 {
			return diagnostic("vocabulary-conflict", "RCB1202", id, "", "extension identifier resolves to different source bytes")
		}
		sort.Slice(candidates, func(i, j int) bool {
			if candidates[i].backend != candidates[j].backend {
				return backendRank(candidates[i].backend) < backendRank(candidates[j].backend)
			}
			return candidates[i].locator < candidates[j].locator
		})
		r.candidates[id] = candidates
	}
	return nil
}

func backendRank(backend string) int {
	switch backend {
	case "override":
		return 0
	case "catalog":
		return 1
	case "package":
		return 2
	case "cache":
		return 3
	case "file":
		return 4
	case "https":
		return 5
	case "oci":
		return 6
	default:
		return 100
	}
}

func (r *Resolver) Resolve(ctx context.Context, rootID string) (ResolvedClosure, error) {
	parsed, err := url.Parse(rootID)
	if err != nil || !parsed.IsAbs() {
		return ResolvedClosure{}, diagnostic("unknown-extension", "RCB1203", rootID, "", "root extension identifier must be an absolute URI")
	}
	state := visitState{activeIndex: map[string]int{}, complete: map[string]bool{}}
	if err := r.visit(ctx, rootID, &state); err != nil {
		return ResolvedClosure{}, err
	}
	ordered, err := topologicalDocuments(state.ordered, state.edges)
	if err != nil {
		return ResolvedClosure{}, err
	}
	closure := ResolvedClosure{
		Root: rootID, Documents: ordered, ByID: map[string]ResolvedDocument{}, Edges: state.edges,
	}
	for _, document := range state.ordered {
		closure.ByID[document.Definition.Metadata.ID] = document
	}
	sort.Slice(closure.Edges, func(i, j int) bool {
		if closure.Edges[i].From != closure.Edges[j].From {
			return closure.Edges[i].From < closure.Edges[j].From
		}
		return closure.Edges[i].To < closure.Edges[j].To
	})
	if err := validateVocabularyConflicts(closure.Documents); err != nil {
		return ResolvedClosure{}, err
	}
	if err := validateVocabularyReferences(closure.Documents); err != nil {
		return ResolvedClosure{}, err
	}
	return closure, nil
}

func topologicalDocuments(documents []ResolvedDocument, edges []DependencyEdge) ([]ResolvedDocument, error) {
	byID := make(map[string]ResolvedDocument, len(documents))
	indegree := make(map[string]int, len(documents))
	dependents := make(map[string][]string, len(documents))
	for _, document := range documents {
		id := document.Definition.Metadata.ID
		byID[id] = document
		indegree[id] = 0
	}
	for _, edge := range edges {
		indegree[edge.From]++
		dependents[edge.To] = append(dependents[edge.To], edge.From)
	}
	for dependency := range dependents {
		sort.Strings(dependents[dependency])
	}
	ready := make([]string, 0, len(documents))
	for id, count := range indegree {
		if count == 0 {
			ready = append(ready, id)
		}
	}
	sort.Strings(ready)
	ordered := make([]ResolvedDocument, 0, len(documents))
	for len(ready) > 0 {
		id := ready[0]
		ready = ready[1:]
		ordered = append(ordered, byID[id])
		for _, dependent := range dependents[id] {
			indegree[dependent]--
			if indegree[dependent] == 0 {
				ready = append(ready, dependent)
				sort.Strings(ready)
			}
		}
	}
	if len(ordered) != len(documents) {
		return nil, diagnostic("extension-dependency", "RCB1234", "", "", "dependency graph cannot be topologically ordered")
	}
	return ordered, nil
}

type visitState struct {
	stack       []string
	activeIndex map[string]int
	complete    map[string]bool
	ordered     []ResolvedDocument
	edges       []DependencyEdge
}

func (r *Resolver) visit(ctx context.Context, id string, state *visitState) error {
	if state.complete[id] {
		return nil
	}
	if index, active := state.activeIndex[id]; active {
		cycle := append(append([]string{}, state.stack[index:]...), id)
		return diagnostic("extension-dependency", "RCB1204", id, "", "dependency cycle: "+strings.Join(cycle, " -> "))
	}
	document, err := r.load(ctx, id)
	if err != nil {
		return err
	}
	state.activeIndex[id] = len(state.stack)
	state.stack = append(state.stack, id)
	dependencies := append([]string(nil), document.Definition.Spec.Dependencies...)
	sort.Strings(dependencies)
	for _, dependency := range dependencies {
		state.edges = append(state.edges, DependencyEdge{From: id, To: dependency})
		if err := r.visit(ctx, dependency, state); err != nil {
			return err
		}
	}
	state.stack = state.stack[:len(state.stack)-1]
	delete(state.activeIndex, id)
	state.complete[id] = true
	state.ordered = append(state.ordered, document)
	return nil
}

func (r *Resolver) load(ctx context.Context, id string) (ResolvedDocument, error) {
	if loaded, exists := r.loaded[id]; exists {
		return loaded, nil
	}
	if candidates := r.candidates[id]; len(candidates) > 0 {
		candidate := candidates[0]
		data, err := os.ReadFile(candidate.path)
		if err != nil {
			return ResolvedDocument{}, err
		}
		return r.finishLoad(id, data, candidate.backend, candidate.locator)
	}
	if lock, locked := r.config.Locks[id]; locked && lock.Locator != "" {
		locator, err := url.Parse(lock.Locator)
		if err != nil || !locator.IsAbs() {
			return ResolvedDocument{}, diagnostic("extension-dependency", "RCB1205", id, "", "dependency lock source locator must be an absolute URI")
		}
		switch locator.Scheme {
		case "file":
			data, err := os.ReadFile(locator.Path)
			if err != nil {
				return ResolvedDocument{}, diagnostic("unknown-extension", "RCB1205", id, "", fmt.Sprintf("file resolution failed: %v", err))
			}
			if lock.SourceSHA256 == "" || SHA256Hex(data) != lock.SourceSHA256 {
				return ResolvedDocument{}, diagnostic("extension-dependency", "RCB1212", id, "", "file content does not match the dependency lock")
			}
			return r.finishLoad(id, data, "file", lock.Locator)
		case "https":
			return r.loadHTTPS(ctx, id, lock.Locator)
		case "oci":
			return r.loadOCI(ctx, id)
		case "http":
			return ResolvedDocument{}, diagnostic("unknown-extension", "RCB1206", id, "", "plain HTTP resolution is forbidden")
		}
	}
	parsed, err := url.Parse(id)
	if err != nil {
		return ResolvedDocument{}, err
	}
	switch parsed.Scheme {
	case "file":
		data, err := os.ReadFile(parsed.Path)
		if err != nil {
			return ResolvedDocument{}, diagnostic("unknown-extension", "RCB1205", id, "", fmt.Sprintf("file resolution failed: %v", err))
		}
		return r.finishLoad(id, data, "file", id)
	case "https":
		return r.loadHTTPS(ctx, id, id)
	case "oci":
		return r.loadOCI(ctx, id)
	case "http":
		return ResolvedDocument{}, diagnostic("unknown-extension", "RCB1206", id, "", "plain HTTP resolution is forbidden")
	default:
		return ResolvedDocument{}, diagnostic("unknown-extension", "RCB1207", id, "", fmt.Sprintf("no declared source resolved URI scheme %q", parsed.Scheme))
	}
}

func (r *Resolver) loadHTTPS(ctx context.Context, id, locator string) (ResolvedDocument, error) {
	if !r.config.Network {
		return ResolvedDocument{}, diagnostic("unknown-extension", "RCB1208", id, "", "network resolution is disabled")
	}
	lock, locked := r.config.Locks[id]
	if !locked || lock.SourceSHA256 == "" || lock.Locator == "" {
		return ResolvedDocument{}, diagnostic("extension-dependency", "RCB1209", id, "", "HTTPS resolution requires source digest and locator locks")
	}
	if lock.Locator != locator {
		return ResolvedDocument{}, diagnostic("extension-dependency", "RCB1209", id, "", "HTTPS locator does not match the dependency lock")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, locator, nil)
	if err != nil {
		return ResolvedDocument{}, err
	}
	response, err := r.config.HTTPClient.Do(request)
	if err != nil {
		return ResolvedDocument{}, diagnostic("unknown-extension", "RCB1210", id, "", fmt.Sprintf("HTTPS resolution failed: %v", err))
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return ResolvedDocument{}, diagnostic("unknown-extension", "RCB1211", id, "", fmt.Sprintf("HTTPS resolution returned %s", response.Status))
	}
	data, err := readLimited(response.Body)
	if err != nil {
		return ResolvedDocument{}, err
	}
	if SHA256Hex(data) != lock.SourceSHA256 {
		return ResolvedDocument{}, diagnostic("extension-dependency", "RCB1212", id, "", "HTTPS content does not match the dependency lock")
	}
	document, err := r.finishLoad(id, data, "https", response.Request.URL.String())
	if err != nil {
		return ResolvedDocument{}, err
	}
	if lock.SemanticSHA256 != "" && document.SemanticSHA256 != lock.SemanticSHA256 {
		return ResolvedDocument{}, diagnostic("extension-dependency", "RCB1213", id, "", "HTTPS semantic digest does not match the dependency lock")
	}
	return document, nil
}

func (r *Resolver) loadOCI(ctx context.Context, id string) (ResolvedDocument, error) {
	if !r.config.Network {
		return ResolvedDocument{}, diagnostic("unknown-extension", "RCB1214", id, "", "network resolution is disabled")
	}
	lock, locked := r.config.Locks[id]
	if !locked || lock.SourceSHA256 == "" || lock.Locator == "" {
		return ResolvedDocument{}, diagnostic("extension-dependency", "RCB1215", id, "", "OCI resolution requires source and immutable locator locks")
	}
	if r.config.OCIFetcher == nil {
		return ResolvedDocument{}, diagnostic("unknown-extension", "RCB1216", id, "", "OCI resolver backend is not configured")
	}
	data, locator, err := r.config.OCIFetcher.Fetch(ctx, id, lock)
	if err != nil {
		return ResolvedDocument{}, diagnostic("unknown-extension", "RCB1217", id, "", fmt.Sprintf("OCI resolution failed: %v", err))
	}
	if !ociLocatorResolvesTo(lock.Locator, locator) || SHA256Hex(data) != lock.SourceSHA256 {
		return ResolvedDocument{}, diagnostic("extension-dependency", "RCB1218", id, "", "OCI content or immutable locator does not match the dependency lock")
	}
	document, err := r.finishLoad(id, data, "oci", locator)
	if err != nil {
		return ResolvedDocument{}, err
	}
	if lock.SemanticSHA256 != "" && document.SemanticSHA256 != lock.SemanticSHA256 {
		return ResolvedDocument{}, diagnostic("extension-dependency", "RCB1213", id, "", "OCI semantic digest does not match the dependency lock")
	}
	return document, nil
}

func readLimited(reader io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maxYAMLBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxYAMLBytes {
		return nil, diagnostic("structural", "RCB1219", "", "", "resolver response exceeds 64 MiB")
	}
	return data, nil
}

func (r *Resolver) finishLoad(requestedID string, data []byte, backend, locator string) (ResolvedDocument, error) {
	definition, mapping, err := DecodeExtension(data)
	if err != nil {
		return ResolvedDocument{}, err
	}
	if definition.Metadata.ID != requestedID {
		return ResolvedDocument{}, diagnostic("unknown-extension", "RCB1220", requestedID, "/metadata/id", fmt.Sprintf("resolved definition identifies %q", definition.Metadata.ID))
	}
	if err := r.config.Schemas.ValidateExtension(mapping, definition); err != nil {
		return ResolvedDocument{}, err
	}
	semanticData, semanticDigest, err := r.config.Schemas.CanonicalizeExtension(mapping)
	if err != nil {
		return ResolvedDocument{}, err
	}
	document := ResolvedDocument{
		Definition: definition, Data: mapping, SemanticData: semanticData, Bytes: data,
		SourceSHA256: SHA256Hex(data), SemanticSHA256: semanticDigest,
		Backend: backend, Locator: locator,
	}
	r.loaded[requestedID] = document
	return document, nil
}

func validateVocabularyConflicts(documents []ResolvedDocument) error {
	owners := map[string]string{}
	for _, document := range documents {
		definition := document.Definition
		interfaces := interfacesByKind(documents)
		coordinates := vocabularyCoordinates(definition, interfaces)
		sort.Strings(coordinates)
		for _, coordinate := range coordinates {
			if previous, exists := owners[coordinate]; exists {
				return diagnostic("vocabulary-conflict", "RCB1221", coordinate, "", fmt.Sprintf("defined by both %q and %q", previous, definition.Metadata.ID))
			}
			owners[coordinate] = definition.Metadata.ID
		}
	}
	return nil
}

func interfacesByKind(documents []ResolvedDocument) map[string][]string {
	result := map[string][]string{}
	for _, document := range documents {
		for _, interfaceType := range document.Definition.Spec.InterfaceTypes {
			result[interfaceType.TargetKind] = append(result[interfaceType.TargetKind], interfaceType.Name)
		}
	}
	for kind := range result {
		sort.Strings(result[kind])
	}
	return result
}

func vocabularyCoordinates(definition ExtensionDefinition, interfaces map[string][]string) []string {
	var result []string
	for _, kind := range definition.Spec.Kinds {
		result = append(result, "kind:"+kind.Name)
	}
	for _, interfaceType := range definition.Spec.InterfaceTypes {
		result = append(result, "interface:"+interfaceType.TargetKind+":"+interfaceType.Name)
	}
	for _, field := range definition.Spec.ConditionFields {
		for _, kind := range field.AppliesToKinds {
			types := field.AppliesToInterfaceTypes
			if len(types) == 0 {
				types = interfaces[kind]
			}
			if len(types) == 0 {
				result = append(result, "condition-field:"+kind+"::"+field.Name)
			}
			for _, interfaceType := range types {
				result = append(result, "condition-field:"+kind+":"+interfaceType+":"+field.Name)
			}
		}
	}
	for _, field := range definition.Spec.InterfaceFields {
		result = append(result, "interface-field:"+field.TargetKind+":"+field.TargetType+":"+field.Name)
	}
	for _, domain := range definition.Spec.FieldValues {
		for _, value := range domain.Values {
			encoded, _ := canonicalJSON(value)
			result = append(result, "field-value:"+domain.TargetKind+":"+domain.TargetType+":"+domain.Field+":"+string(encoded))
		}
	}
	return result
}

func validateVocabularyReferences(documents []ResolvedDocument) error {
	kinds := map[string]bool{}
	interfaces := map[string]map[string]bool{}
	for _, document := range documents {
		for _, kind := range document.Definition.Spec.Kinds {
			kinds[kind.Name] = true
		}
		for _, interfaceType := range document.Definition.Spec.InterfaceTypes {
			if interfaces[interfaceType.TargetKind] == nil {
				interfaces[interfaceType.TargetKind] = map[string]bool{}
			}
			interfaces[interfaceType.TargetKind][interfaceType.Name] = true
		}
	}
	for _, document := range documents {
		owner := document.Definition.Metadata.ID
		for _, interfaceType := range document.Definition.Spec.InterfaceTypes {
			if !kinds[interfaceType.TargetKind] {
				return diagnostic("unknown-extension", "RCB1227", owner, "", fmt.Sprintf("interface type %q targets unknown kind %q", interfaceType.Name, interfaceType.TargetKind))
			}
		}
		for _, field := range document.Definition.Spec.ConditionFields {
			for _, kind := range field.AppliesToKinds {
				if !kinds[kind] {
					return diagnostic("unknown-extension", "RCB1228", owner, "", fmt.Sprintf("condition field %q targets unknown kind %q", field.Name, kind))
				}
				for _, interfaceType := range field.AppliesToInterfaceTypes {
					if !interfaces[kind][interfaceType] {
						return diagnostic("unknown-extension", "RCB1229", owner, "", fmt.Sprintf("condition field %q targets unknown scope %s:%s", field.Name, kind, interfaceType))
					}
				}
			}
		}
		for _, field := range document.Definition.Spec.InterfaceFields {
			if !kinds[field.TargetKind] || !interfaces[field.TargetKind][field.TargetType] {
				return diagnostic("unknown-extension", "RCB1230", owner, "", fmt.Sprintf("interface field %q targets unknown scope %s:%s", field.Name, field.TargetKind, field.TargetType))
			}
		}
		for _, domain := range document.Definition.Spec.FieldValues {
			if _, err := parsePath(domain.Field); err != nil {
				return diagnostic("structural", "RCB1235", owner, "", fmt.Sprintf("invalid fieldValues path %q: %v", domain.Field, err))
			}
			if !kinds[domain.TargetKind] || (domain.TargetType != "" && !interfaces[domain.TargetKind][domain.TargetType]) {
				return diagnostic("unknown-extension", "RCB1231", owner, "", fmt.Sprintf("fieldValues path %q targets unknown scope %s:%s", domain.Field, domain.TargetKind, domain.TargetType))
			}
		}
		for _, schema := range document.Definition.Spec.Schemas {
			if schema.AppliesToKind != "" && !kinds[schema.AppliesToKind] {
				return diagnostic("unknown-extension", "RCB1232", owner+"#schema:"+schema.ID, "", fmt.Sprintf("schema targets unknown kind %q", schema.AppliesToKind))
			}
			if schema.AppliesToInterfaceType != "" && !interfaces[schema.AppliesToKind][schema.AppliesToInterfaceType] {
				return diagnostic("unknown-extension", "RCB1233", owner+"#schema:"+schema.ID, "", fmt.Sprintf("schema targets unknown scope %s:%s", schema.AppliesToKind, schema.AppliesToInterfaceType))
			}
		}
	}
	return nil
}
