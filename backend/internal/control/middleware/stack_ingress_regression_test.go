// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package middleware

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ingressStack builds the canonical stack the API server uses, with a low rate
// limit so budgets are reachable inside a test.
func ingressStack(t *testing.T, rps int, trusted []*net.IPNet) http.Handler {
	t.Helper()
	r := NewRouter(StackConfig{
		EnableCORS:            true,
		AllowedOrigins:        []string{"https://app.example"},
		EnableSecurityHeaders: true,
		CSP:                   DefaultCSP,
		TrustedProxies:        trusted,
		EnableCompression:     true,
		EnableRateLimit:       true,
		RateLimitEnabled:      true,
		RateLimitGlobalRPS:    rps,
		MaxRequestBodyBytes:   DefaultMaxRequestBodyBytes,
	})
	r.Get("/api/v3/things", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	r.Post("/api/v3/things", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	return r
}

// Regression: before the shared client-IP resolver, the limiter keyed on the
// socket peer. Behind a reverse proxy every client collapsed into one bucket, so
// a single abusive client rate-limited everybody else.
func TestIngress_RateLimitDoesNotCollapseBehindProxy(t *testing.T) {
	trusted, err := ParseCIDRs([]string{"172.20.0.0/16"})
	if err != nil {
		t.Fatalf("ParseCIDRs: %v", err)
	}
	h := ingressStack(t, 1, trusted) // 1 rps => 60 requests/minute per client

	// Must exceed the per-bucket budget (60/min), otherwise a collapsed shared
	// bucket would still fit inside it and the test would pass against the bug.
	const clients = 150
	blocked := 0
	for i := range clients {
		req := httptest.NewRequest(http.MethodGet, "/api/v3/things", nil)
		req.RemoteAddr = "172.20.0.5:5555" // one shared proxy
		req.Header.Set("X-Forwarded-For", fmt.Sprintf("198.51.100.%d", i+1))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code == http.StatusTooManyRequests {
			blocked++
		}
	}
	if blocked != 0 {
		t.Errorf("%d/%d distinct clients behind one proxy were rate limited; buckets collapsed", blocked, clients)
	}

	// The limit must still bite per client.
	over := 0
	for range 100 {
		req := httptest.NewRequest(http.MethodGet, "/api/v3/things", nil)
		req.RemoteAddr = "172.20.0.5:5555"
		req.Header.Set("X-Forwarded-For", "198.51.100.200")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code == http.StatusTooManyRequests {
			over++
		}
	}
	if over == 0 {
		t.Error("a single client exceeding its budget was never rate limited")
	}
}

// Regression: an untrusted caller must not be able to mint a fresh bucket per
// request by rotating X-Forwarded-For. This is the failure mode that switching
// to httprate.KeyByRealIP would have introduced.
func TestIngress_SpoofedXFFCannotEvadeRateLimit(t *testing.T) {
	trusted, err := ParseCIDRs([]string{"172.20.0.0/16"})
	if err != nil {
		t.Fatalf("ParseCIDRs: %v", err)
	}
	h := ingressStack(t, 1, trusted)

	blocked := 0
	for i := range 100 {
		req := httptest.NewRequest(http.MethodGet, "/api/v3/things", nil)
		req.RemoteAddr = "203.0.113.77:44321" // NOT a trusted proxy
		req.Header.Set("X-Forwarded-For", fmt.Sprintf("198.51.100.%d", i+1))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code == http.StatusTooManyRequests {
			blocked++
		}
	}
	if blocked == 0 {
		t.Error("rotating X-Forwarded-For evaded the rate limit entirely")
	}
}

// Regression: CORS used to answer OPTIONS and return before the limiter ever
// ran, leaving preflights completely unmetered.
func TestIngress_PreflightIsRateLimited(t *testing.T) {
	h := ingressStack(t, 1, nil)
	// Preflight budget is 1 rps * multiplier * 60s; exceed it decisively.
	total := PreflightRateLimitMultiplier*60 + 200

	blocked := 0
	for range total {
		req := httptest.NewRequest(http.MethodOptions, "/api/v3/things", nil)
		req.RemoteAddr = "198.51.100.42:1234"
		req.Header.Set("Origin", "https://app.example")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code == http.StatusTooManyRequests {
			blocked++
		}
	}
	if blocked == 0 {
		t.Errorf("preflight flood of %d was never rate limited", total)
	}
}

// Preflights get their own budget, so browser preflight volume cannot starve
// real API calls — the reason for a separate limiter rather than reordering.
func TestIngress_PreflightBudgetIsSeparateFromAPIBudget(t *testing.T) {
	h := ingressStack(t, 1, nil)
	const client = "198.51.100.43:1234"

	// Burn well past the API budget (60/min) using preflights only.
	for range 300 {
		req := httptest.NewRequest(http.MethodOptions, "/api/v3/things", nil)
		req.RemoteAddr = client
		req.Header.Set("Origin", "https://app.example")
		h.ServeHTTP(httptest.NewRecorder(), req)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v3/things", nil)
	req.RemoteAddr = client
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("preflights consumed the API budget: GET got %d, want 200", rec.Code)
	}
}

// A rate-limit rejection must still carry CORS headers, or the browser reports
// an opaque network error and the UI cannot tell throttling from an outage.
func TestIngress_RateLimitedResponseKeepsCORSAndSecurityHeaders(t *testing.T) {
	h := ingressStack(t, 1, nil)

	var throttled *httptest.ResponseRecorder
	for range 200 {
		req := httptest.NewRequest(http.MethodGet, "/api/v3/things", nil)
		req.RemoteAddr = "198.51.100.44:1234"
		req.Header.Set("Origin", "https://app.example")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code == http.StatusTooManyRequests {
			throttled = rec
			break
		}
	}
	if throttled == nil {
		t.Fatal("never produced a 429 to inspect")
	}
	if got := throttled.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example" {
		t.Errorf("429 missing CORS origin header, got %q", got)
	}
	if got := throttled.Header().Get("Content-Security-Policy"); got == "" {
		t.Error("429 missing security headers")
	}
	if got := throttled.Header().Get("X-Request-ID"); got == "" {
		t.Error("429 missing request ID; the rejection is untraceable")
	}
}

// The preflight responder advertises what a path really accepts instead of one
// blanket list for every URL.
func TestIngress_PreflightAdvertisesRealMethods(t *testing.T) {
	r := NewRouter(StackConfig{
		EnableCORS:     true,
		AllowedOrigins: []string{"https://app.example"},
	})
	r.Get("/read-only", func(w http.ResponseWriter, _ *http.Request) {})
	r.Get("/both", func(w http.ResponseWriter, _ *http.Request) {})
	r.Delete("/both", func(w http.ResponseWriter, _ *http.Request) {})

	check := func(path, want string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodOptions, path, nil)
		req.Header.Set("Origin", "https://app.example")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("%s: got %d, want 204", path, rec.Code)
		}
		if got := rec.Header().Get("Access-Control-Allow-Methods"); got != want {
			t.Errorf("%s: Allow-Methods = %q, want %q", path, got, want)
		}
	}

	check("/read-only", "GET, OPTIONS")
	check("/both", "GET, DELETE, OPTIONS")
}

// An unknown path must not get a success-shaped preflight.
func TestIngress_PreflightOnUnknownPathIsNot204(t *testing.T) {
	r := NewRouter(StackConfig{
		EnableCORS:     true,
		AllowedOrigins: []string{"https://app.example"},
	})
	r.Get("/known", func(w http.ResponseWriter, _ *http.Request) {})

	req := httptest.NewRequest(http.MethodOptions, "/definitely/not/here", nil)
	req.Header.Set("Origin", "https://app.example")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code == http.StatusNoContent {
		t.Error("unknown path produced a successful preflight")
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); got != "" {
		t.Errorf("unknown path leaked a method surface: %q", got)
	}
}

// A disallowed origin gets no method surface, which is what makes the browser
// fail the preflight.
func TestIngress_PreflightWithheldFromDisallowedOrigin(t *testing.T) {
	h := ingressStack(t, 100, nil)

	req := httptest.NewRequest(http.MethodOptions, "/api/v3/things", nil)
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Methods"); got != "" {
		t.Errorf("disallowed origin was told the method surface: %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("disallowed origin got an Allow-Origin: %q", got)
	}
}

// Ordering guard: every rejection the stack can emit must still be traceable and
// carry security headers.
func TestIngress_SecurityHeadersPrecedeRejections(t *testing.T) {
	h := ingressStack(t, 100, nil)

	// CSRF rejection (no Origin on an unsafe method).
	req := httptest.NewRequest(http.MethodPost, "/api/v3/things", strings.NewReader("{}"))
	req.RemoteAddr = "198.51.100.45:1234"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 from CSRF, got %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Security-Policy"); got == "" {
		t.Error("CSRF rejection missing security headers")
	}
	if got := rec.Header().Get("X-Request-ID"); got == "" {
		t.Error("CSRF rejection missing request ID")
	}
}
