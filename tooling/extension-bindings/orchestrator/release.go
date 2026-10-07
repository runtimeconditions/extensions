package orchestrator

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"

	goemitter "github.com/runtimeconditions/extensions/tooling/extension-bindings/emitters/go"
	"github.com/runtimeconditions/extensions/tooling/extension-bindings/normalizer"
)

func mapValue(value any) map[string]any { mapping, _ := value.(map[string]any); return mapping }
func stringValue(value any) string      { text, _ := value.(string); return text }
func sequence(value any) []any          { values, _ := value.([]any); return values }
func boolValue(value any) bool          { result, _ := value.(bool); return result }
func jsonCopy(value any) any {
	data, _ := json.Marshal(value)
	var copy any
	_ = json.Unmarshal(data, &copy)
	return copy
}
func number(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case uint64:
		return float64(v)
	case json.Number:
		result, _ := v.Float64()
		return result
	}
	return 0
}

func (p *Pipeline) releaseManifest(bt *BuiltTarget, build *Build) (map[string]any, error) {
	_, orchestrator, err := p.tool("orchestrator")
	if err != nil {
		return nil, err
	}
	profiler, err := p.profiler(bt.Target.Language)
	if err != nil {
		return nil, err
	}
	packageIdentity := mapValue(jsonCopy(bt.Manifest["package"]))
	packageIdentity["packageKey"] = bt.Target.PackageKey
	packageIdentity["publicationMode"] = bt.Target.Config.PublicationMode
	if bt.Target.Config.RegistryID != "" {
		packageIdentity["registryId"] = bt.Target.Config.RegistryID
	}
	dependencies := []any{}
	for _, key := range bt.Target.Dependencies {
		var dependency *BuiltTarget
		for _, item := range build.Targets {
			if item.Target.Key == key {
				dependency = item
				break
			}
		}
		if dependency == nil {
			return nil, fmt.Errorf("dependency %s not built", key)
		}
		kind := "go-module-zip"
		version := "v" + dependency.Target.Config.Version
		minimum := version
		next := nextBreaking(version)
		if bt.Target.Language == "python" {
			kind = "python-wheel"
			version = dependency.Target.Config.Version
			minimum = version
			next = nextBreaking(version)
		}
		artifact := dependency.Artifacts[kind]
		data, err := os.ReadFile(artifact)
		if err != nil {
			return nil, err
		}
		dependencies = append(dependencies, map[string]any{"extension": dependency.Target.Set.RootExtension, "coordinate": dependency.Target.Config.Coordinate, "name": dependency.Target.Config.Name, "testedVersion": version, "compatibleVersionRange": map[string]any{"minimumInclusive": minimum, "nextBreakingExclusive": next}, "artifact": map[string]any{"kind": kind, "sha256": normalizer.SHA256Hex(data)}})
	}
	identity := bt.Document.Model.RootExtension
	result := map[string]any{"apiVersion": "runtimeconditions.io/binding-release/v1alpha1", "kind": "RuntimeConditionsBindingRelease", "package": packageIdentity, "model": map[string]any{"apiVersion": bt.Document.Model.APIVersion, "semanticSha256": bt.Document.Model.Metadata.SemanticSHA256}, "rootExtension": identity, "dependencyLock": bt.Document.Lock, "packageDependencies": dependencies, "provenance": map[string]any{"mode": "production", "orchestrator": orchestrator, "profiler": profiler, "sourceDirectory": bt.Target.Config.SourceDirectory, "targetReleaseTag": bt.Target.Config.SourceDirectory + "/v" + bt.Target.Config.Version}}
	schema, err := readMapping(filepath.Join(p.Project.ToolingDirectory, "model", "runtimeconditions.binding-release.schema.yaml"))
	if err != nil {
		return nil, err
	}
	if err = validateSchema(schema, result); err != nil {
		return nil, err
	}
	return result, nil
}

type semver struct {
	major, minor, patch int
	prerelease          string
}

var semverPattern = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z.-]+))?(?:\+[0-9A-Za-z.-]+)?$`)

func parseVersion(value string) (semver, error) {
	matches := semverPattern.FindStringSubmatch(value)
	if matches == nil {
		return semver{}, fmt.Errorf("invalid SemVer %q", value)
	}
	major, _ := strconv.Atoi(matches[1])
	minor, _ := strconv.Atoi(matches[2])
	patch, _ := strconv.Atoi(matches[3])
	return semver{major: major, minor: minor, patch: patch, prerelease: matches[4]}, nil
}
func (v semver) String() string {
	result := fmt.Sprintf("%d.%d.%d", v.major, v.minor, v.patch)
	if v.prerelease != "" {
		result += "-" + v.prerelease
	}
	return result
}
func compareVersions(a, b semver) int {
	for _, pair := range [][2]int{{a.major, b.major}, {a.minor, b.minor}, {a.patch, b.patch}} {
		if pair[0] < pair[1] {
			return -1
		}
		if pair[0] > pair[1] {
			return 1
		}
	}
	if a.prerelease == b.prerelease {
		return 0
	}
	if a.prerelease == "" {
		return 1
	}
	if b.prerelease == "" {
		return -1
	}
	ap, bp := strings.Split(a.prerelease, "."), strings.Split(b.prerelease, ".")
	for i := 0; i < len(ap) && i < len(bp); i++ {
		if ap[i] == bp[i] {
			continue
		}
		an, ae := strconv.Atoi(ap[i])
		bn, be := strconv.Atoi(bp[i])
		if ae == nil && be == nil {
			if an < bn {
				return -1
			}
			return 1
		}
		if ae == nil {
			return -1
		}
		if be == nil {
			return 1
		}
		if ap[i] < bp[i] {
			return -1
		}
		return 1
	}
	if len(ap) < len(bp) {
		return -1
	}
	return 1
}
func nextBreaking(value string) string {
	v, err := parseVersion(value)
	if err != nil {
		return ""
	}
	if v.major == 0 {
		v.minor++
		v.patch = 0
	} else {
		v.major++
		v.minor = 0
		v.patch = 0
	}
	v.prerelease = ""
	prefix := ""
	if strings.HasPrefix(value, "v") {
		prefix = "v"
	}
	return prefix + v.String()
}
func requiredVersion(previous, impact string) (string, error) {
	v, err := parseVersion(previous)
	if err != nil {
		return "", err
	}
	switch impact {
	case "major":
		if v.major == 0 {
			v.minor++
		} else {
			v.major++
			v.minor = 0
		}
		v.patch = 0
	case "minor":
		if v.major == 0 {
			v.patch++
		} else {
			v.minor++
			v.patch = 0
		}
	case "patch":
		v.patch++
	case "none":
		return strings.TrimPrefix(previous, "v"), nil
	default:
		return "", fmt.Errorf("unknown impact %s", impact)
	}
	v.prerelease = ""
	return v.String(), nil
}

type ReleaseTargetPlan struct {
	Target          string   `yaml:"target" json:"target"`
	PreviousVersion string   `yaml:"previousVersion,omitempty" json:"previousVersion,omitempty"`
	DeclaredVersion string   `yaml:"declaredVersion" json:"declaredVersion"`
	RequiredVersion string   `yaml:"requiredVersion" json:"requiredVersion"`
	Impact          string   `yaml:"impact" json:"impact"`
	Reasons         []string `yaml:"reasons" json:"reasons"`
	ReleaseTag      string   `yaml:"releaseTag" json:"releaseTag"`
}
type ReleasePlan struct {
	Build    BuildPlan           `yaml:"build" json:"build"`
	Releases []ReleaseTargetPlan `yaml:"releases" json:"releases"`
}
type githubRelease struct {
	TagName     string `json:"tag_name"`
	Draft       bool   `json:"draft"`
	PublishedAt string `json:"published_at"`
	Assets      []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

func (p *Pipeline) githubRepository() (string, error) {
	parsed, err := url.Parse(p.Project.Catalog.RepositoryURL)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil {
		return "", fmt.Errorf("plan-release requires an HTTPS GitHub repositoryUrl")
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) != 2 {
		return "", fmt.Errorf("repositoryUrl must identify owner/repository")
	}
	host := "https://api.github.com"
	if parsed.Host != "github.com" {
		host = "https://" + parsed.Host + "/api/v3"
	}
	if override := os.Getenv("RC_BINDINGS_GITHUB_API"); override != "" {
		if p.Project.Toolchain.Status == "released" && !strings.HasPrefix(override, "https://") {
			return "", fmt.Errorf("released GitHub API must use HTTPS")
		}
		host = strings.TrimRight(override, "/")
	}
	return host + "/repos/" + url.PathEscape(parts[0]) + "/" + url.PathEscape(strings.TrimSuffix(parts[1], ".git")), nil
}
func (p *Pipeline) githubGet(address string, limit int64) ([]byte, int, error) {
	request, err := http.NewRequestWithContext(p.Context, http.MethodGet, address, nil)
	if err != nil {
		return nil, 0, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	if token := envOr("GH_TOKEN", os.Getenv("GITHUB_TOKEN")); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := p.HTTPClient.Do(request)
	if err != nil {
		return nil, 0, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, response.StatusCode, err
	}
	if int64(len(data)) > limit {
		return nil, response.StatusCode, fmt.Errorf("GitHub response exceeds size limit")
	}
	if response.StatusCode != 200 && response.StatusCode != 404 {
		return nil, response.StatusCode, fmt.Errorf("GitHub request returned %s", response.Status)
	}
	return data, response.StatusCode, nil
}
func (p *Pipeline) previousRelease(target Target) (*githubRelease, error) {
	base, err := p.githubRepository()
	if err != nil {
		return nil, err
	}
	var previous *githubRelease
	var version semver
	prefix := target.Config.SourceDirectory + "/v"
	for page := 1; page <= 1000; page++ {
		data, status, err := p.githubGet(fmt.Sprintf("%s/releases?per_page=100&page=%d", base, page), 16<<20)
		if err != nil {
			return nil, err
		}
		if status == 404 {
			return nil, fmt.Errorf("GitHub repository unavailable; absence of a repository is not proof of no previous release")
		}
		var releases []githubRelease
		if err = json.Unmarshal(data, &releases); err != nil {
			return nil, err
		}
		for _, release := range releases {
			if release.Draft || release.PublishedAt == "" {
				continue
			}
			versions := []string{}
			if strings.HasPrefix(release.TagName, prefix) {
				versions = append(versions, strings.TrimPrefix(release.TagName, prefix))
			}
			// Promotion publishes one release per source commit. Its target
			// source assets identify the language package versions in that release.
			assetPrefix := target.PackageKey + "-" + target.Language + "-"
			if strings.HasPrefix(release.TagName, "bindings/releases/") {
				for _, asset := range release.Assets {
					if strings.HasPrefix(asset.Name, assetPrefix) && strings.HasSuffix(asset.Name, ".source.zip") {
						versions = append(versions, strings.TrimSuffix(strings.TrimPrefix(asset.Name, assetPrefix), ".source.zip"))
					}
				}
			}
			for _, value := range versions {
				candidate, err := parseVersion(value)
				if err != nil {
					continue
				}
				if previous == nil || compareVersions(candidate, version) > 0 {
					copy := release
					copy.TagName = prefix + candidate.String()
					previous = &copy
					version = candidate
				}
			}
		}
		if len(releases) < 100 {
			return previous, nil
		}
	}
	return nil, fmt.Errorf("GitHub release pagination exceeds limit")
}
func (p *Pipeline) releaseSource(previous *githubRelease, target Target) (string, error) {
	address := ""
	version := strings.TrimPrefix(previous.TagName, target.Config.SourceDirectory+"/v")
	expectedAsset := target.PackageKey + "-" + target.Language + "-" + version + ".source.zip"
	for _, asset := range previous.Assets {
		if asset.Name == expectedAsset {
			if address != "" {
				return "", fmt.Errorf("previous target release has multiple source archives")
			}
			address = asset.URL
		}
	}
	if address == "" {
		return "", fmt.Errorf("previous release %s lacks its generated source archive", previous.TagName)
	}
	request, err := http.NewRequestWithContext(p.Context, http.MethodGet, address, nil)
	if err != nil {
		return "", err
	}
	client := *p.HTTPClient
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > 5 || req.URL.Scheme != "https" {
			return fmt.Errorf("unsafe release asset redirect")
		}
		return nil
	}
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return "", fmt.Errorf("previous source archive returned %s", response.Status)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 256<<20+1))
	if err != nil {
		return "", err
	}
	if len(data) > 256<<20 {
		return "", fmt.Errorf("previous source archive exceeds size limit")
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", err
	}
	prefix := target.Config.SourceDirectory + "/"
	files := map[string][]byte{}
	total := uint64(0)
	for _, entry := range reader.File {
		if !strings.HasPrefix(entry.Name, prefix) || !entry.Mode().IsRegular() {
			return "", fmt.Errorf("unsafe previous source archive entry %s", entry.Name)
		}
		name := strings.TrimPrefix(entry.Name, prefix)
		if name == "" || strings.Contains(name, "\\") || strings.Contains(name, ":") || filepath.ToSlash(filepath.Clean(name)) != name || strings.HasPrefix(name, "../") {
			return "", fmt.Errorf("unsafe previous source path")
		}
		if _, ok := files[name]; ok {
			return "", fmt.Errorf("duplicate previous source entry")
		}
		total += entry.UncompressedSize64
		if total > 256<<20 {
			return "", fmt.Errorf("expanded previous source archive exceeds limit")
		}
		stream, err := entry.Open()
		if err != nil {
			return "", err
		}
		content, err := io.ReadAll(io.LimitReader(stream, 64<<20+1))
		_ = stream.Close()
		if err != nil {
			return "", err
		}
		if len(content) > 64<<20 {
			return "", fmt.Errorf("previous source file exceeds limit")
		}
		files[name] = content
	}
	directory, err := p.workspacePath("previous-releases", target.PackageKey, target.Language)
	if err != nil {
		return "", err
	}
	if err = os.MkdirAll(directory, 0755); err != nil {
		return "", err
	}
	for _, name := range sortedKeys(files) {
		path := filepath.Join(directory, filepath.FromSlash(name))
		if err = os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return "", err
		}
		if err = os.WriteFile(path, files[name], 0644); err != nil {
			return "", err
		}
	}
	if err = verifyFileManifest(directory, filepath.Join(p.Project.ToolingDirectory, "model", "runtimeconditions.file-manifest.schema.yaml")); err != nil {
		return "", err
	}
	return directory, nil
}
func (p *Pipeline) nativeAPI(bt *BuiltTarget) ([]string, error) {
	if bt.Target.Language == "go" {
		return goemitter.SourceAPISurface(filepath.Join(bt.Directory, "bindings.go"))
	}
	data, err := p.python("api", "--input", resourceDir(bt))
	if err != nil {
		return nil, err
	}
	var result struct {
		API []string `json:"api"`
	}
	if err = json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result.API, nil
}
func (p *Pipeline) PlanRelease(plan BuildPlan) (ReleasePlan, error) {
	build, err := p.LoadBuild(plan, "")
	if err != nil {
		return ReleasePlan{}, err
	}
	result := ReleasePlan{Build: build.Plan, Releases: []ReleaseTargetPlan{}}
	for _, bt := range build.selected() {
		entry := ReleaseTargetPlan{Target: bt.Target.Key, DeclaredVersion: bt.Target.Config.Version, ReleaseTag: bt.Target.Config.SourceDirectory + "/v" + bt.Target.Config.Version, Reasons: []string{}}
		previous, err := p.previousRelease(bt.Target)
		if err != nil {
			return result, err
		}
		if previous == nil {
			entry.Impact = "initial"
			entry.RequiredVersion = bt.Target.Config.Version
			if version, err := parseVersion(bt.Document.Model.RootExtension.Version); err == nil {
				entry.RequiredVersion = version.String()
			}
			entry.Reasons = append(entry.Reasons, "no previous published target release")
		} else {
			directory, err := p.releaseSource(previous, bt.Target)
			if err != nil {
				return result, err
			}
			old := &BuiltTarget{Target: bt.Target, Directory: directory}
			oldModel, err := goemitter.LoadModel(filepath.Join(resourceDir(old), modelName))
			if err != nil {
				return result, err
			}
			old.Document = &Document{Model: oldModel}
			old.Manifest, err = readMapping(filepath.Join(resourceDir(old), bindingName))
			if err != nil {
				return result, err
			}
			oldAPI, err := p.nativeAPI(old)
			if err != nil {
				return result, err
			}
			newAPI, err := p.nativeAPI(bt)
			if err != nil {
				return result, err
			}
			oldRelease, err := readMapping(filepath.Join(resourceDir(old), releaseName))
			if err != nil {
				return result, err
			}
			newRelease, err := readMapping(filepath.Join(resourceDir(bt), releaseName))
			if err != nil {
				return result, err
			}
			entry.PreviousVersion = stringValue(mapValue(oldRelease["package"])["version"])
			entry.Impact, entry.Reasons = ClassifyCompatibility(oldModel, bt.Document.Model, oldAPI, newAPI, old.Manifest, bt.Manifest, oldRelease, newRelease)
			entry.RequiredVersion, err = requiredVersion(entry.PreviousVersion, entry.Impact)
			if err != nil {
				return result, err
			}
		}
		declared, err := parseVersion(entry.DeclaredVersion)
		if err != nil {
			return result, err
		}
		required, err := parseVersion(entry.RequiredVersion)
		if err != nil {
			return result, err
		}
		if compareVersions(declared, required) < 0 {
			return result, fmt.Errorf("%s: declared %s is below required %s (%s)", entry.Target, entry.DeclaredVersion, entry.RequiredVersion, entry.Impact)
		}
		result.Releases = append(result.Releases, entry)
	}
	return result, nil
}

// ClassifyCompatibility is deliberately conservative when schema compatibility
// cannot be proved. It consumes native AST surfaces, models and behavior records.
func ClassifyCompatibility(old, new normalizer.BindingModel, oldAPI, newAPI []string, oldManifest, newManifest, oldRelease, newRelease map[string]any) (string, []string) {
	impact := "none"
	reasons := []string{}
	rank := map[string]int{"none": 0, "patch": 1, "minor": 2, "major": 3}
	add := func(level, reason string) {
		if rank[level] > rank[impact] {
			impact = level
		}
		reasons = append(reasons, reason)
	}
	oldPackage, newPackage := mapValue(oldManifest["package"]), mapValue(newManifest["package"])
	for _, key := range []string{"coordinate", "name"} {
		if oldPackage[key] != newPackage[key] {
			add("major", "native package "+key+" changed")
		}
	}
	for _, key := range []string{"minimumGoVersion", "minimumPythonVersion"} {
		a, b := stringValue(oldPackage[key]), stringValue(newPackage[key])
		if a != b {
			if a == "" || b == "" {
				add("major", "minimum language version contract changed")
			} else {
				av, _ := parseVersion(a + ".0")
				bv, _ := parseVersion(b + ".0")
				if compareVersions(bv, av) > 0 {
					add("major", "minimum language version increased")
				} else {
					add("minor", "minimum language version relaxed")
				}
			}
		}
	}
	aSchemas, bSchemas := map[string]map[string]any{}, map[string]map[string]any{}
	for _, schema := range old.Schemas {
		aSchemas[stableCoordinate(schema.Coordinate, old)] = schema.Exact
	}
	for _, schema := range new.Schemas {
		bSchemas[stableCoordinate(schema.Coordinate, new)] = schema.Exact
	}
	for key, a := range aSchemas {
		b, ok := bSchemas[key]
		if !ok {
			add("major", "extension schema removed or renamed: "+key)
			continue
		}
		level := schemaImpact(a, b)
		if level != "none" {
			add(level, "extension schema changed: "+key)
		}
	}
	for key := range bSchemas {
		if _, ok := aSchemas[key]; !ok {
			add("major", "new validation schema requires a compatibility proof: "+key)
		}
	}
	for _, group := range []string{"declarations", "importedMarkerContracts", "rootBindings", "types"} {
		a := sequence(stableBindingReferences(oldManifest[group], old))
		b := sequence(stableBindingReferences(newManifest[group], new))
		byName := map[string]map[string]any{}
		for _, value := range b {
			item := mapValue(value)
			key := stringValue(item["nativeName"])
			if key == "" {
				key = stringValue(item["function"])
			}
			if key == "" {
				encoded, _ := json.Marshal(item["modelRef"])
				key = string(encoded)
			}
			byName[key] = item
		}
		for _, value := range a {
			item := mapValue(value)
			key := stringValue(item["nativeName"])
			if key == "" {
				key = stringValue(item["function"])
			}
			if key == "" {
				encoded, _ := json.Marshal(item["modelRef"])
				key = string(encoded)
			}
			other, ok := byName[key]
			if !ok {
				add("major", "public binding removed: "+group+"/"+key)
			} else if !reflect.DeepEqual(item, other) {
				if bindingAddition(item, other) {
					add("minor", "public binding expanded: "+group+"/"+key)
				} else {
					add("major", "public binding changed: "+group+"/"+key)
				}
			}
		}
		if len(b) > len(a) {
			add("minor", "public "+group+" added")
		}
	}
	if strings.Join(oldAPI, "\n") != strings.Join(newAPI, "\n") {
		newEntries := map[string]bool{}
		for _, entry := range newAPI {
			newEntries[entry] = true
		}
		for _, entry := range oldAPI {
			if newEntries[entry] {
				continue
			}
			parts := strings.Split(entry, "|")
			compatible := false
			// Go encodes a whole struct in one native AST record. A proved
			// optional field addition may expand that record compatibly.
			if len(parts) >= 3 && parts[0] == "type" {
				for _, a := range sequence(oldManifest["types"]) {
					for _, b := range sequence(newManifest["types"]) {
						if stringValue(mapValue(a)["nativeName"]) == parts[1] && stringValue(mapValue(b)["nativeName"]) == parts[1] {
							compatible = bindingAddition(mapValue(stableBindingReferences(a, old)), mapValue(stableBindingReferences(b, new)))
						}
					}
				}
			}
			if !compatible {
				add("major", "native AST API removed or changed: "+entry)
			}
		}
		if impact != "major" {
			add("minor", "native AST API expanded")
		}
	}
	oldDeps, newDeps := sequence(oldRelease["packageDependencies"]), sequence(newRelease["packageDependencies"])
	if !reflect.DeepEqual(oldDeps, newDeps) {
		oldByExtension := map[string]map[string]any{}
		for _, value := range oldDeps {
			item := mapValue(value)
			oldByExtension[stableCoordinate(stringValue(item["extension"]), old)] = item
		}
		newByExtension := map[string]map[string]any{}
		for _, value := range newDeps {
			item := mapValue(value)
			newByExtension[stableCoordinate(stringValue(item["extension"]), new)] = item
		}
		for key, a := range oldByExtension {
			b, ok := newByExtension[key]
			if !ok || a["coordinate"] != b["coordinate"] || a["name"] != b["name"] {
				add("major", "direct package dependency removed or renamed: "+key)
				continue
			}
			av, ae := parseVersion(stringValue(a["testedVersion"]))
			bv, be := parseVersion(stringValue(b["testedVersion"]))
			if ae != nil || be != nil || bv.major != av.major || av.major == 0 && bv.minor != av.minor {
				add("major", "exposed dependency crossed a breaking version: "+key)
			} else {
				add("patch", "compatible dependency artifact changed: "+key)
			}
		}
		for key := range newByExtension {
			if _, ok := oldByExtension[key]; !ok {
				add("minor", "direct dependency added: "+key)
			}
		}
	}
	if old.Metadata.SemanticSHA256 != new.Metadata.SemanticSHA256 && impact == "none" {
		add("patch", "normalized closure or metadata changed")
	}
	if !reflect.DeepEqual(oldManifest, newManifest) && impact == "none" {
		add("patch", "binding metadata changed")
	}
	if !reflect.DeepEqual(oldRelease, newRelease) && impact == "none" {
		add("patch", "release metadata changed")
	}
	sort.Strings(reasons)
	return impact, reasons
}

func stableCoordinate(value string, model normalizer.BindingModel) string {
	for _, extension := range model.Extensions {
		uri := strings.TrimSuffix(extension.ID, ":"+extension.Version)
		if value == extension.ID {
			return uri
		}
		if strings.HasPrefix(value, extension.ID+"#") {
			return uri + strings.TrimPrefix(value, extension.ID)
		}
	}
	return value
}

func stableBindingReferences(value any, model normalizer.BindingModel) any {
	switch value := value.(type) {
	case []any:
		result := make([]any, len(value))
		for i, child := range value {
			result[i] = stableBindingReferences(child, model)
		}
		return result
	case map[string]any:
		result := map[string]any{}
		for key, child := range value {
			if key == "coordinate" || key == "extension" || key == "owner" {
				if text, ok := child.(string); ok {
					result[key] = stableCoordinate(text, model)
					continue
				}
			}
			result[key] = stableBindingReferences(child, model)
		}
		return result
	}
	return value
}
func bindingAddition(a, b map[string]any) bool {
	aa, bb := mapValue(jsonCopy(a)), mapValue(jsonCopy(b))
	for _, group := range []string{"fields", "members"} {
		oldItems, newItems := sequence(aa[group]), sequence(bb[group])
		if len(newItems) < len(oldItems) {
			return false
		}
		for _, oldItem := range oldItems {
			found := false
			for _, newItem := range newItems {
				if reflect.DeepEqual(oldItem, newItem) {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
		if group == "fields" {
			for _, newItem := range newItems {
				found := false
				for _, oldItem := range oldItems {
					if reflect.DeepEqual(oldItem, newItem) {
						found = true
						break
					}
				}
				if !found && boolValue(mapValue(newItem)["required"]) {
					return false
				}
			}
		}
		delete(aa, group)
		delete(bb, group)
	}
	return reflect.DeepEqual(aa, bb)
}
func schemaImpact(a, b map[string]any) string {
	if reflect.DeepEqual(a, b) {
		return "none"
	}
	impact := "patch"
	for _, key := range sortedKeys(a) {
		av := a[key]
		bv, ok := b[key]
		switch key {
		case "description", "title", "examples", "$comment":
			continue
		case "properties", "$defs":
			if !ok {
				return "major"
			}
			for name, oldProperty := range mapValue(av) {
				newProperty, found := mapValue(bv)[name]
				if !found {
					return "major"
				}
				level := schemaImpact(mapValue(oldProperty), mapValue(newProperty))
				if level == "major" {
					return level
				}
				if level == "minor" {
					impact = "minor"
				}
			}
			if len(mapValue(bv)) > len(mapValue(av)) {
				if key == "properties" && a["additionalProperties"] != false {
					return "major"
				}
				impact = "minor"
			}
		case "required", "enum":
			oldSet, newSet := sequence(av), sequence(bv)
			contains := func(values []any, value any) bool {
				for _, candidate := range values {
					if reflect.DeepEqual(candidate, value) {
						return true
					}
				}
				return false
			}
			if key == "required" {
				for _, value := range newSet {
					if !contains(oldSet, value) {
						return "major"
					}
				}
				if len(newSet) < len(oldSet) {
					impact = "minor"
				}
			} else {
				for _, value := range oldSet {
					if !contains(newSet, value) {
						return "major"
					}
				}
				if len(newSet) > len(oldSet) {
					impact = "minor"
				}
			}
		case "minimum", "exclusiveMinimum", "minLength", "minItems", "minProperties":
			if ok && number(bv) > number(av) {
				return "major"
			}
			if !ok || number(bv) < number(av) {
				impact = "minor"
			}
		case "maximum", "exclusiveMaximum", "maxLength", "maxItems", "maxProperties":
			if ok && number(bv) < number(av) {
				return "major"
			}
			if !ok || number(bv) > number(av) {
				impact = "minor"
			}
		default:
			if !ok || !reflect.DeepEqual(av, bv) {
				return "major"
			}
		}
	}
	for key, value := range b {
		if _, ok := a[key]; ok {
			continue
		}
		switch key {
		case "description", "title", "examples", "$comment":
			continue
		case "properties":
			if len(mapValue(value)) > 0 {
				if a["additionalProperties"] != false {
					return "major"
				}
				impact = "minor"
			}
		default:
			return "major"
		}
	}
	// An added required field always tightens accepted declarations.
	oldRequired := map[string]bool{}
	for _, item := range sequence(a["required"]) {
		oldRequired[stringValue(item)] = true
	}
	for _, item := range sequence(b["required"]) {
		if !oldRequired[stringValue(item)] {
			return "major"
		}
	}
	return impact
}
