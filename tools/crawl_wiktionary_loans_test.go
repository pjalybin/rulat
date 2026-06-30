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

func setTestHTTPRetrySettings(retries int, delay time.Duration) func() {
	oldRetries := httpRetries
	oldDelay := httpRetryDelay
	oldSleep := retrySleep
	httpRetries = retries
	httpRetryDelay = delay
	retrySleep = func(time.Duration) {}
	return func() {
		httpRetries = oldRetries
		httpRetryDelay = oldDelay
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
