package normalizer

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type ociRouteTransport struct {
	routes map[string][]byte
}

func (transport ociRouteTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	data, exists := transport.routes[request.URL.Path]
	status := http.StatusOK
	if !exists {
		status = http.StatusNotFound
	}
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Body:       io.NopCloser(bytes.NewReader(data)),
		Header:     make(http.Header),
		Request:    request,
	}, nil
}

func TestRegistryOCIFetcherUsesImmutableManifestAndLockedLayer(t *testing.T) {
	extension := fixtureExtension("urn:runtimeconditions:test:oci-registry", nil, kindSpec("oci"))
	extensionDigest := SHA256Hex(extension)
	manifest := ociManifest{
		SchemaVersion: 2,
		Layers: []ociDescriptor{{
			Digest:    "sha256:" + extensionDigest,
			Size:      int64(len(extension)),
			MediaType: "application/vnd.runtimeconditions.extension.v1+yaml",
		}},
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifestDigest := "sha256:" + SHA256Hex(manifestBytes)
	locator := "oci://registry.invalid/runtimeconditions/extensions/test@" + manifestDigest
	client := &http.Client{Transport: ociRouteTransport{routes: map[string][]byte{
		"/v2/runtimeconditions/extensions/test/manifests/" + manifestDigest:     manifestBytes,
		"/v2/runtimeconditions/extensions/test/blobs/sha256:" + extensionDigest: extension,
	}}}
	fetcher := registryOCIFetcher{client: client}
	actual, actualLocator, err := fetcher.Fetch(context.Background(), "urn:runtimeconditions:test:oci-registry", LockEntry{
		SourceSHA256: extensionDigest,
		Locator:      locator,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual, extension) || actualLocator != locator {
		t.Fatal("OCI fetch did not return the locked extension layer and locator")
	}
}

func TestRegistryOCIFetcherResolvesMutableTagToImmutableManifest(t *testing.T) {
	extension := fixtureExtension("urn:runtimeconditions:test:oci-tag", nil, kindSpec("oci"))
	extensionDigest := SHA256Hex(extension)
	manifest := ociManifest{
		SchemaVersion: 2,
		Layers: []ociDescriptor{{
			Digest:    "sha256:" + extensionDigest,
			Size:      int64(len(extension)),
			MediaType: "application/vnd.runtimeconditions.extension.v1+yaml",
		}},
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifestDigest := "sha256:" + SHA256Hex(manifestBytes)
	mutableLocator := "oci://registry.invalid/runtimeconditions/extensions/test:stable"
	immutableLocator := "oci://registry.invalid/runtimeconditions/extensions/test@" + manifestDigest
	client := &http.Client{Transport: ociRouteTransport{routes: map[string][]byte{
		"/v2/runtimeconditions/extensions/test/manifests/stable":                manifestBytes,
		"/v2/runtimeconditions/extensions/test/blobs/sha256:" + extensionDigest: extension,
	}}}
	fetcher := registryOCIFetcher{client: client}
	actual, actualLocator, err := fetcher.Fetch(context.Background(), "urn:runtimeconditions:test:oci-tag", LockEntry{
		SourceSHA256: extensionDigest,
		Locator:      mutableLocator,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual, extension) || actualLocator != immutableLocator {
		t.Fatalf("mutable OCI resolution returned locator %q, want %q", actualLocator, immutableLocator)
	}
}

func TestOCILocatorValidation(t *testing.T) {
	valid := []string{
		"oci://registry.invalid/repository:latest",
		"oci://registry.invalid/team/repository:release-1.2",
		"oci://registry.invalid/repository@sha256:" + strings.Repeat("a", 64),
	}
	for _, locator := range valid {
		if _, err := parseOCILocator(locator); err != nil {
			t.Errorf("rejected valid OCI locator %q: %v", locator, err)
		}
	}
	invalid := []string{
		"oci://registry.invalid/repository",
		"oci://registry.invalid/repository:",
		"oci://registry.invalid/repository@sha256:" + strings.Repeat("A", 64),
		"oci://registry.invalid/repository@sha512:" + strings.Repeat("a", 128),
	}
	for _, locator := range invalid {
		if _, err := parseOCILocator(locator); err == nil {
			t.Errorf("accepted invalid OCI locator %q", locator)
		}
	}
}

func TestSecureHTTPClientPolicy(t *testing.T) {
	client := secureHTTPClient()
	if client.Timeout != 60*time.Second {
		t.Fatalf("total timeout is %s", client.Timeout)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport has type %T", client.Transport)
	}
	if transport.TLSClientConfig == nil || transport.TLSClientConfig.MinVersion != tls.VersionTLS12 {
		t.Fatal("TLS minimum is not 1.2")
	}
	if transport.DialContext == nil {
		t.Fatal("connection timeout dialer is not configured")
	}
	if err := client.CheckRedirect(&http.Request{}, nil); err == nil {
		t.Fatal("client accepted an HTTPS redirect")
	}
}
