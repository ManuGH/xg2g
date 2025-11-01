// SPDX-License-Identifier: MIT
package openwebif

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

func TestStreamURLScenarios(t *testing.T) {
	t.Setenv("XG2G_STREAM_PORT", "")
	// Disable smart stream detection to test explicit port configuration
	t.Setenv("XG2G_SMART_STREAM_DETECTION", "false")

	ref := "1:0:19:1334:3EF:1:C00000:0:0:0:"
	name := "ORF1 HD"

	testcases := []struct {
		name       string
		base       string
		port       int
		envValue   string
		wantHost   string
		wantScheme string
		wantPath   string
	}{
		{
			name:       "configured port",
			base:       "http://127.0.0.1",
			port:       17999,
			wantHost:   "127.0.0.1:17999",
			wantScheme: "http",
			wantPath:   "/" + ref,
		},
		{
			name:       "existing port preserved",
			base:       "http://127.0.0.1:5555",
			port:       19000,
			wantHost:   "127.0.0.1:5555",
			wantScheme: "http",
			wantPath:   "/" + ref,
		},
		{
			name:       "ipv6 without port",
			base:       "http://[fd00::57]",
			port:       17999,
			wantHost:   "[fd00::57]:17999",
			wantScheme: "http",
			wantPath:   "/" + ref,
		},
		{
			name:       "ipv6 with port",
			base:       "http://[fd00::57]:8001",
			port:       17999,
			wantHost:   "[fd00::57]:8001",
			wantScheme: "http",
			wantPath:   "/" + ref,
		},
		{
			name:       "trailing slash",
			base:       "http://example.com/base/",
			port:       17999,
			wantHost:   "example.com:17999",
			wantScheme: "http",
			wantPath:   "/" + ref,
		},
		{
			name:       "env fallback",
			base:       "http://example.com",
			port:       0,
			envValue:   "19000",
			wantHost:   "example.com:19000",
			wantScheme: "http",
			wantPath:   "/" + ref,
		},
		{
			name:       "env invalid falls back to default",
			base:       "http://example.com",
			port:       0,
			envValue:   "abc",
			wantHost:   "example.com:8001",
			wantScheme: "http",
			wantPath:   "/" + ref,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.envValue != "" {
				t.Setenv("XG2G_STREAM_PORT", tc.envValue)
			} else {
				t.Setenv("XG2G_STREAM_PORT", "")
			}

			client := NewWithPort(tc.base, tc.port, Options{Timeout: time.Second})
			got, err := client.StreamURL(context.Background(), ref, name)
			if err != nil {
				t.Fatalf("StreamURL returned error: %v", err)
			}
			parsed, err := url.Parse(got)
			if err != nil {
				t.Fatalf("failed to parse URL %q: %v", got, err)
			}

			if parsed.Scheme != tc.wantScheme {
				t.Fatalf("scheme: want %q, got %q", tc.wantScheme, parsed.Scheme)
			}
			if parsed.Host != tc.wantHost {
				t.Fatalf("host: want %q, got %q", tc.wantHost, parsed.Host)
			}
			if parsed.Path != tc.wantPath {
				t.Fatalf("path: want %q, got %q", tc.wantPath, parsed.Path)
			}

			// Direct streaming format has no query parameters
			if len(parsed.RawQuery) > 0 {
				t.Fatalf("expected no query parameters for direct streaming, got: %s", parsed.RawQuery)
			}
		})
	}
}

func TestBouquetsTimeout(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
	}))
	t.Cleanup(ts.Close)

	client := NewWithPort(ts.URL, 0, Options{Timeout: 50 * time.Millisecond, MaxRetries: 0, Backoff: 10 * time.Millisecond})
	if _, err := client.Bouquets(context.Background()); err == nil {
		t.Fatalf("expected timeout error, got nil")
	}
}

func TestBouquetsRetrySuccess(t *testing.T) {
	var calls int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count := atomic.AddInt32(&calls, 1)
		if count == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"bouquets": [["ref","Premium"]]}`))
	}))
	t.Cleanup(ts.Close)

	client := NewWithPort(ts.URL, 0, Options{Timeout: 100 * time.Millisecond, MaxRetries: 1, Backoff: 10 * time.Millisecond})
	res, err := client.Bouquets(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("expected 1 bouquet, got %d", len(res))
	}
}

func TestBouquetsRetryFailure(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(ts.Close)

	client := NewWithPort(ts.URL, 0, Options{Timeout: 50 * time.Millisecond, MaxRetries: 1, Backoff: 10 * time.Millisecond})
	if _, err := client.Bouquets(context.Background()); err == nil {
		t.Fatalf("expected error after retries, got nil")
	}
}

func TestContextCancellationCleanup(t *testing.T) {
	// Test that cancelled contexts don't leak goroutines
	requestReceived := make(chan struct{})

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(requestReceived)
		// Simulate slow response that would exceed context deadline
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"bouquets": []}`))
	}))
	t.Cleanup(ts.Close)

	client := NewWithPort(ts.URL, 0, Options{Timeout: 100 * time.Millisecond, MaxRetries: 0})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := client.Bouquets(ctx)
	elapsed := time.Since(start)

	// Should fail due to context timeout
	if err == nil {
		t.Fatalf("expected timeout error, got nil")
	}

	// Should fail relatively quickly due to context cancellation
	if elapsed > 80*time.Millisecond {
		t.Errorf("request took too long (%v), context cancellation may not be working properly", elapsed)
	}

	// Ensure the request was actually started
	select {
	case <-requestReceived:
		// Good, request was initiated
	case <-time.After(100 * time.Millisecond):
		t.Error("request was never received by server")
	}
}
