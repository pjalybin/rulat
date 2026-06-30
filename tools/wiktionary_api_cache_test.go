//go:build !greektranslit

package main

import (
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestDoAPIQueryUsesCachedResponse(t *testing.T) {
	restoreRetries := setTestHTTPRetrySettings(1, 0)
	defer restoreRetries()
	restoreCache := setTestAPICacheSettings(t.TempDir(), defaultCacheTTL, time.Unix(1700000000, 0))
	defer restoreCache()

	var requests int
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		return testHTTPResponse(http.StatusOK, `{"query":{"pages":[{"title":"cached"}]}}`), nil
	})}
	values := map[string][]string{"action": {"query"}}

	parsed, err := doAPIQuery(client, values)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Query.Pages) != 1 || parsed.Query.Pages[0].Title != "cached" {
		t.Fatalf("pages = %#v, want cached page", parsed.Query.Pages)
	}

	parsed, err = doAPIQuery(client, values)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Query.Pages) != 1 || parsed.Query.Pages[0].Title != "cached" {
		t.Fatalf("pages = %#v, want cached page", parsed.Query.Pages)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
	if got := apiCacheMissCount(); got != 1 {
		t.Fatalf("apiCacheMissCount() = %d, want 1", got)
	}

	req, err := newAPIRequest(values)
	if err != nil {
		t.Fatal(err)
	}
	rawURL := req.URL.String()
	data, err := os.ReadFile(apiCachePath(rawURL))
	if err != nil {
		t.Fatal(err)
	}
	firstLine := strings.SplitN(string(data), "\n", 2)[0]
	if firstLine != rawURL {
		t.Fatalf("cache first line = %q, want %q", firstLine, rawURL)
	}
}

func TestDoAPIQueryRedownloadsCacheCollision(t *testing.T) {
	restoreRetries := setTestHTTPRetrySettings(1, 0)
	defer restoreRetries()
	restoreCache := setTestAPICacheSettings(t.TempDir(), defaultCacheTTL, time.Unix(1700000000, 0))
	defer restoreCache()

	values := map[string][]string{"action": {"query"}}
	req, err := newAPIRequest(values)
	if err != nil {
		t.Fatal(err)
	}
	rawURL := req.URL.String()
	if err := os.MkdirAll(apiCacheDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(apiCachePath(rawURL), []byte("https://other.example/api\n{\"query\":{\"pages\":[{\"title\":\"wrong\"}]}}"), 0644); err != nil {
		t.Fatal(err)
	}

	var requests int
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		return testHTTPResponse(http.StatusOK, `{"query":{"pages":[{"title":"fresh"}]}}`), nil
	})}

	parsed, err := doAPIQuery(client, values)
	if err != nil {
		t.Fatal(err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
	if len(parsed.Query.Pages) != 1 || parsed.Query.Pages[0].Title != "fresh" {
		t.Fatalf("pages = %#v, want fresh page", parsed.Query.Pages)
	}
	if got := apiCacheMissCount(); got != 1 {
		t.Fatalf("apiCacheMissCount() = %d, want 1", got)
	}
	data, err := os.ReadFile(apiCachePath(rawURL))
	if err != nil {
		t.Fatal(err)
	}
	firstLine := strings.SplitN(string(data), "\n", 2)[0]
	if firstLine != rawURL {
		t.Fatalf("cache first line = %q, want refreshed URL %q", firstLine, rawURL)
	}
}

func TestDoAPIQueryRedownloadsStaleCache(t *testing.T) {
	restoreRetries := setTestHTTPRetrySettings(1, 0)
	defer restoreRetries()
	now := time.Unix(1700000000, 0)
	restoreCache := setTestAPICacheSettings(t.TempDir(), 30*24*time.Hour, now)
	defer restoreCache()

	values := map[string][]string{"action": {"query"}}
	req, err := newAPIRequest(values)
	if err != nil {
		t.Fatal(err)
	}
	rawURL := req.URL.String()
	if err := os.MkdirAll(apiCacheDir, 0755); err != nil {
		t.Fatal(err)
	}
	path := apiCachePath(rawURL)
	if err := os.WriteFile(path, []byte(rawURL+"\n"+`{"query":{"pages":[{"title":"stale"}]}}`), 0644); err != nil {
		t.Fatal(err)
	}
	oldTime := now.Add(-31 * 24 * time.Hour)
	if err := os.Chtimes(path, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	var requests int
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		return testHTTPResponse(http.StatusOK, `{"query":{"pages":[{"title":"fresh"}]}}`), nil
	})}

	parsed, err := doAPIQuery(client, values)
	if err != nil {
		t.Fatal(err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
	if len(parsed.Query.Pages) != 1 || parsed.Query.Pages[0].Title != "fresh" {
		t.Fatalf("pages = %#v, want fresh page", parsed.Query.Pages)
	}
	if got := apiCacheMissCount(); got != 1 {
		t.Fatalf("apiCacheMissCount() = %d, want 1", got)
	}
}

func setTestAPICacheSettings(dir string, ttl time.Duration, now time.Time) func() {
	oldDir := apiCacheDir
	oldTTL := apiCacheTTL
	oldNow := apiCacheNow
	oldMisses := apiCacheMisses
	apiCacheDir = dir
	apiCacheTTL = ttl
	apiCacheNow = func() time.Time { return now }
	apiCacheMisses = 0
	return func() {
		apiCacheDir = oldDir
		apiCacheTTL = oldTTL
		apiCacheNow = oldNow
		apiCacheMisses = oldMisses
	}
}
