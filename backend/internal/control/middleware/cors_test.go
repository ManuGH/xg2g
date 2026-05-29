package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCORS_WildcardEmitsStar(t *testing.T) {
	allowed := []string{"*"}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	cors := CORS(allowed, false)(handler)

	// Case 1: With Origin — wildcard emits * instead of reflecting
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Origin", "http://example.com")
	w := httptest.NewRecorder()

	cors.ServeHTTP(w, req)

	if val := w.Header().Get("Access-Control-Allow-Origin"); val != "*" {
		t.Errorf("expected wildcard *, got %q", val)
	}
	if val := w.Header().Get("Vary"); !strings.Contains(val, "Origin") {
		t.Errorf("expected Vary header to contain Origin, got %q", val)
	}

	// Case 2: No Origin
	req = httptest.NewRequest("GET", "/test", nil)
	w = httptest.NewRecorder()

	cors.ServeHTTP(w, req)

	if val := w.Header().Get("Access-Control-Allow-Origin"); val != "" {
		t.Errorf("expected no Access-Control-Allow-Origin when Origin header is missing, got %q", val)
	}
}

func TestCORS_CredentialsWithWildcardSuppressed(t *testing.T) {
	allowed := []string{"*"}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Origin", "http://example.com")

	// credentials=false: no credentials header
	cors := CORS(allowed, false)(handler)
	w := httptest.NewRecorder()
	cors.ServeHTTP(w, req)

	if val := w.Header().Get("Access-Control-Allow-Credentials"); val != "" {
		t.Errorf("expected no Access-Control-Allow-Credentials when disabled, got %q", val)
	}

	// credentials=true with wildcard: credentials header suppressed
	// because wildcard * cannot carry credentials per the Fetch spec.
	cors = CORS(allowed, true)(handler)
	w = httptest.NewRecorder()
	cors.ServeHTTP(w, req)

	if val := w.Header().Get("Access-Control-Allow-Credentials"); val != "" {
		t.Errorf("expected Access-Control-Allow-Credentials to be suppressed when wildcard origin is allowed, got %q", val)
	}
	// With wildcard we still emit * (not reflected origin)
	if val := w.Header().Get("Access-Control-Allow-Origin"); val != "*" {
		t.Errorf("expected Access-Control-Allow-Origin: * for wildcard config, got %q", val)
	}
}

func TestCORS_SpecificOriginWithCredentials(t *testing.T) {
	allowed := []string{"http://trusted.com"}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	cors := CORS(allowed, true)(handler)

	// Trusted origin — credentials header should be present
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Origin", "http://trusted.com")
	w := httptest.NewRecorder()
	cors.ServeHTTP(w, req)

	if val := w.Header().Get("Access-Control-Allow-Origin"); val != "http://trusted.com" {
		t.Errorf("expected http://trusted.com, got %q", val)
	}
	if val := w.Header().Get("Access-Control-Allow-Credentials"); val != "true" {
		t.Errorf("expected Access-Control-Allow-Credentials: true for trusted origin, got %q", val)
	}

	// Untrusted origin
	req = httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Origin", "http://evil.com")
	w = httptest.NewRecorder()
	cors.ServeHTTP(w, req)

	if val := w.Header().Get("Access-Control-Allow-Origin"); val != "" {
		t.Errorf("expected empty Access-Control-Allow-Origin for untrusted request, got %q", val)
	}
}

func TestCORS_OnlyEmitsAllowHeadersForAllowedOrigin(t *testing.T) {
	allowed := []string{"http://trusted.com"}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	cors := CORS(allowed, false)(handler)

	corsHeaders := []string{
		"Access-Control-Allow-Methods",
		"Access-Control-Allow-Headers",
		"Access-Control-Expose-Headers",
		"Access-Control-Max-Age",
	}

	// Allowed origin: the Allow-* response headers are present.
	req := httptest.NewRequest("OPTIONS", "/test", nil)
	req.Header.Set("Origin", "http://trusted.com")
	w := httptest.NewRecorder()
	cors.ServeHTTP(w, req)
	for _, h := range corsHeaders {
		if w.Header().Get(h) == "" {
			t.Errorf("allowed origin: expected %s to be set", h)
		}
	}

	// Disallowed origin: no Allow-* surface leaked.
	req = httptest.NewRequest("OPTIONS", "/test", nil)
	req.Header.Set("Origin", "http://evil.com")
	w = httptest.NewRecorder()
	cors.ServeHTTP(w, req)
	for _, h := range corsHeaders {
		if val := w.Header().Get(h); val != "" {
			t.Errorf("disallowed origin: %s must not be emitted, got %q", h, val)
		}
	}

	// No Origin header: no Allow-* surface leaked.
	req = httptest.NewRequest("GET", "/test", nil)
	w = httptest.NewRecorder()
	cors.ServeHTTP(w, req)
	for _, h := range corsHeaders {
		if val := w.Header().Get(h); val != "" {
			t.Errorf("no-origin request: %s must not be emitted, got %q", h, val)
		}
	}
}
