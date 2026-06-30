//go:build !greektranslit

package main

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestDoAPIRequestRetriesTransientHTTPStatus(t *testing.T) {
	var requests int
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		if requests < 3 {
			return testHTTPResponse(http.StatusInternalServerError, "resource exhausted"), nil
		}
		return testHTTPResponse(http.StatusOK, `{"ok":true}`), nil
	})}

	restore := setTestHTTPRetrySettings(3, 0)
	defer restore()

	req, err := http.NewRequest(http.MethodGet, "https://example.test/api", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := doAPIRequest(client, req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if requests != 3 {
		t.Fatalf("requests = %d, want 3", requests)
	}
}

func TestDoAPIRequestStopsAfterRetryBudget(t *testing.T) {
	var requests int
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		return testHTTPResponse(http.StatusInternalServerError, "resource exhausted"), nil
	})}

	restore := setTestHTTPRetrySettings(2, 0)
	defer restore()

	req, err := http.NewRequest(http.MethodGet, "https://example.test/api", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := doAPIRequest(client, req)
	if resp != nil {
		resp.Body.Close()
	}
	if err == nil {
		t.Fatal("expected retry exhaustion error")
	}
	if !strings.Contains(err.Error(), "500 Internal Server Error") || !strings.Contains(err.Error(), "resource exhausted") {
		t.Fatalf("error = %q, want status and body", err)
	}
	if requests != 3 {
		t.Fatalf("requests = %d, want 3", requests)
	}
}

func TestHTTPRetryBackoffDelayUsesRetryAfter(t *testing.T) {
	restore := setTestHTTPRetrySettings(5, 2*time.Second)
	defer restore()

	if got := httpRetryBackoffDelay(3, "7"); got != 7*time.Second {
		t.Fatalf("Retry-After seconds delay = %s, want 7s", got)
	}
}

func TestNewAPIRequestSetsUserAgentAndMaxlag(t *testing.T) {
	oldUserAgent := apiUserAgent
	oldMaxlag := apiMaxlag
	apiUserAgent = "test-rulat/0.1 (https://example.test/contact)"
	apiMaxlag = 7
	defer func() {
		apiUserAgent = oldUserAgent
		apiMaxlag = oldMaxlag
	}()

	req, err := newAPIRequest(map[string][]string{"action": {"query"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get("User-Agent"); got != apiUserAgent {
		t.Fatalf("User-Agent = %q, want %q", got, apiUserAgent)
	}
	if got := req.URL.Query().Get("maxlag"); got != "7" {
		t.Fatalf("maxlag = %q, want 7", got)
	}
}

func TestDoAPIQueryRetriesMaxlagErrors(t *testing.T) {
	var requests int
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		if requests == 1 {
			return testHTTPResponse(http.StatusOK, `{"error":{"code":"maxlag","info":"Waiting for 10.1.1.1: 7 seconds lagged","lag":7}}`), nil
		}
		return testHTTPResponse(http.StatusOK, `{"query":{"pages":[{"title":"ёлка"}]}}`), nil
	})}

	restore := setTestHTTPRetrySettings(3, 0)
	defer restore()

	parsed, err := doAPIQuery(client, map[string][]string{"action": {"query"}})
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
	if len(parsed.Query.Pages) != 1 || parsed.Query.Pages[0].Title != "ёлка" {
		t.Fatalf("pages = %#v, want one ёлка page", parsed.Query.Pages)
	}
}

func TestCrawlWordPagesSkipsNonCyrillicExactTitleBeforeFetch(t *testing.T) {
	var requests int
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		return testHTTPResponse(http.StatusOK, `{}`), nil
	})}

	rows, skipped, filtered, inspected, err := crawlWordPages(client, crawlOptions{Title: "alpha"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("rows = %d, want 0", len(rows))
	}
	if skipped != 1 || filtered != 0 || inspected != 1 {
		t.Fatalf("skipped=%d filtered=%d inspected=%d, want skipped=1 filtered=0 inspected=1", skipped, filtered, inspected)
	}
	if requests != 0 {
		t.Fatalf("requests = %d, want 0", requests)
	}
}

func TestCrawlWordPagesFetchesOnlyRussianAlphabetTitles(t *testing.T) {
	var fullFetches []string
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		q := req.URL.Query()
		if q.Get("generator") == "allpages" {
			if q.Get("prop") != "" || q.Get("rvprop") != "" || q.Get("cllimit") != "" {
				t.Fatalf("allpages request loaded page props: %s", req.URL.RawQuery)
			}
			return testHTTPResponse(http.StatusOK, `{"query":{"pages":[{"title":"*ainaz"},{"title":"ікра"},{"title":"ёлка"}]}}`), nil
		}
		title := q.Get("titles")
		fullFetches = append(fullFetches, title)
		if title != "ёлка" {
			t.Fatalf("full fetch for %q, want only ёлка", title)
		}
		return testHTTPResponse(http.StatusOK, `{"query":{"pages":[{"title":"ёлка","revisions":[{"slots":{"main":{"content":"={{-ru-}}=\n=== Этимология ===\n{{lang|en|yolka}}\n"}}}],"categories":[]}]}}`), nil
	})}

	rows, skipped, filtered, inspected, err := crawlWordPages(client, crawlOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if inspected != 3 {
		t.Fatalf("inspected = %d, want 3", inspected)
	}
	if skipped != 2 || filtered != 0 {
		t.Fatalf("skipped=%d filtered=%d, want skipped=2 filtered=0", skipped, filtered)
	}
	if len(fullFetches) != 1 || fullFetches[0] != "ёлка" {
		t.Fatalf("fullFetches = %v, want [ёлка]", fullFetches)
	}
	if len(rows) != 1 || rows[0].CyrillicStem != "ёлка" || rows[0].LatinStem != "yolka" {
		t.Fatalf("rows = %#v, want one ёлка/yolka row", rows)
	}
}

func TestIsRussianAlphabetPageTitle(t *testing.T) {
	cases := map[string]bool{
		"а":        true,
		"А":        true,
		"ё":        true,
		"Ё":        true,
		"эконом":   true,
		"*ainaz":   false,
		"alpha":    false,
		"ікра":     false,
		"рус-ский": false,
		"а темпо":  false,
		"русский1": false,
		"":         false,
	}
	for input, want := range cases {
		if got := isRussianAlphabetPageTitle(input); got != want {
			t.Fatalf("isRussianAlphabetPageTitle(%q) = %v, want %v", input, got, want)
		}
	}
}

func setTestHTTPRetrySettings(retries int, delay time.Duration) func() {
	oldRetries := httpRetries
	oldDelay := httpRetryDelay
	oldRequestDelay := apiRequestDelay
	oldLastAPIRequestAt := lastAPIRequestAt
	oldSleep := retrySleep
	httpRetries = retries
	httpRetryDelay = delay
	apiRequestDelay = 0
	lastAPIRequestAt = time.Time{}
	retrySleep = func(time.Duration) {}
	return func() {
		httpRetries = oldRetries
		httpRetryDelay = oldDelay
		apiRequestDelay = oldRequestDelay
		lastAPIRequestAt = oldLastAPIRequestAt
		retrySleep = oldSleep
	}
}

func testHTTPResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
