package normalizer

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

type registryOCIFetcher struct {
	client *http.Client
}

type ociManifest struct {
	SchemaVersion int             `json:"schemaVersion"`
	Layers        []ociDescriptor `json:"layers"`
}

type ociDescriptor struct {
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
	MediaType string `json:"mediaType"`
}

func (fetcher registryOCIFetcher) Fetch(ctx context.Context, _ string, lock LockEntry) ([]byte, string, error) {
	locator, err := parseOCILocator(lock.Locator)
	if err != nil {
		return nil, "", err
	}
	manifestURL := &url.URL{
		Scheme: "https",
		Host:   locator.registry,
		Path:   "/v2/" + locator.repository + "/manifests/" + locator.reference,
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL.String(), nil)
	if err != nil {
		return nil, "", err
	}
	request.Header.Set("Accept", strings.Join([]string{
		"application/vnd.oci.image.manifest.v1+json",
		"application/vnd.oci.artifact.manifest.v1+json",
		"application/vnd.docker.distribution.manifest.v2+json",
	}, ", "))
	response, err := fetcher.client.Do(request)
	if err != nil {
		return nil, "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("OCI manifest request returned %s", response.Status)
	}
	manifestBytes, err := readLimited(response.Body)
	if err != nil {
		return nil, "", err
	}
	manifestDigest := "sha256:" + SHA256Hex(manifestBytes)
	if locator.immutable && manifestDigest != locator.reference {
		return nil, "", fmt.Errorf("OCI manifest bytes do not match immutable locator digest")
	}
	if advertised := response.Header.Get("Docker-Content-Digest"); advertised != "" && advertised != manifestDigest {
		return nil, "", fmt.Errorf("OCI manifest bytes do not match Docker-Content-Digest header")
	}
	var manifest ociManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return nil, "", fmt.Errorf("decode OCI manifest: %w", err)
	}
	if manifest.SchemaVersion != 2 {
		return nil, "", fmt.Errorf("OCI manifest schemaVersion must be 2")
	}
	wantedDigest := "sha256:" + lock.SourceSHA256
	var layer *ociDescriptor
	for index := range manifest.Layers {
		if manifest.Layers[index].Digest == wantedDigest {
			layer = &manifest.Layers[index]
			break
		}
	}
	if layer == nil {
		return nil, "", fmt.Errorf("OCI manifest has no layer matching the locked source digest")
	}
	if layer.Size < 0 || layer.Size > maxYAMLBytes {
		return nil, "", fmt.Errorf("OCI extension layer exceeds 64 MiB")
	}
	blobURL := &url.URL{
		Scheme: "https",
		Host:   locator.registry,
		Path:   "/v2/" + locator.repository + "/blobs/" + layer.Digest,
	}
	blobRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, blobURL.String(), nil)
	if err != nil {
		return nil, "", err
	}
	blobResponse, err := fetcher.client.Do(blobRequest)
	if err != nil {
		return nil, "", err
	}
	defer blobResponse.Body.Close()
	if blobResponse.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("OCI blob request returned %s", blobResponse.Status)
	}
	data, err := readLimited(blobResponse.Body)
	if err != nil {
		return nil, "", err
	}
	if int64(len(data)) != layer.Size {
		return nil, "", fmt.Errorf("OCI extension layer size does not match its descriptor")
	}
	if SHA256Hex(data) != lock.SourceSHA256 {
		return nil, "", fmt.Errorf("OCI extension layer does not match the locked source digest")
	}
	return data, immutableOCILocator(locator.registry, locator.repository, manifestDigest), nil
}

type parsedOCILocator struct {
	registry   string
	repository string
	reference  string
	immutable  bool
}

var ociTagPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)

func parseOCILocator(value string) (parsedOCILocator, error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "oci" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return parsedOCILocator{}, fmt.Errorf("OCI locator must be an absolute oci URI without credentials, query, or fragment")
	}
	path := strings.TrimPrefix(parsed.Path, "/")
	if separator := strings.LastIndex(path, "@"); separator > 0 && separator < len(path)-1 {
		repository := path[:separator]
		digest := path[separator+1:]
		if err := validateOCIManifestDigest(digest); err != nil {
			return parsedOCILocator{}, err
		}
		return parsedOCILocator{registry: parsed.Host, repository: repository, reference: digest, immutable: true}, nil
	}
	separator := strings.LastIndex(path, ":")
	lastSlash := strings.LastIndex(path, "/")
	if separator <= lastSlash+1 || separator == len(path)-1 {
		return parsedOCILocator{}, fmt.Errorf("OCI locator must contain a repository and a tag or immutable manifest digest")
	}
	repository := path[:separator]
	tag := path[separator+1:]
	if !ociTagPattern.MatchString(tag) {
		return parsedOCILocator{}, fmt.Errorf("OCI tag is invalid")
	}
	return parsedOCILocator{registry: parsed.Host, repository: repository, reference: tag}, nil
}

func validateOCIManifestDigest(digest string) error {
	if !strings.HasPrefix(digest, "sha256:") || len(strings.TrimPrefix(digest, "sha256:")) != 64 {
		return fmt.Errorf("OCI locator must use an immutable sha256 manifest digest")
	}
	for _, character := range strings.TrimPrefix(digest, "sha256:") {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return fmt.Errorf("OCI manifest digest must use lower-case hexadecimal")
		}
	}
	return nil
}

func parseImmutableOCILocator(value string) (string, string, string, error) {
	locator, err := parseOCILocator(value)
	if err != nil {
		return "", "", "", err
	}
	if !locator.immutable {
		return "", "", "", fmt.Errorf("OCI locator must use an immutable manifest digest")
	}
	return locator.registry, locator.repository, locator.reference, nil
}

func immutableOCILocator(registry, repository, digest string) string {
	return (&url.URL{Scheme: "oci", Host: registry, Path: "/" + repository + "@" + digest}).String()
}

func ociLocatorResolvesTo(requested, resolved string) bool {
	requestedLocator, err := parseOCILocator(requested)
	if err != nil {
		return false
	}
	resolvedLocator, err := parseOCILocator(resolved)
	if err != nil || !resolvedLocator.immutable {
		return false
	}
	if requestedLocator.registry != resolvedLocator.registry || requestedLocator.repository != resolvedLocator.repository {
		return false
	}
	return !requestedLocator.immutable || requestedLocator.reference == resolvedLocator.reference
}
