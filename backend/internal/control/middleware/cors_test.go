package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCORS_WildcardReflectsOrigin(t *testing.T) {
	allowed := []string{"*"}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	cors := CORS(allowed, false)(handler)

	// Case 1: With Origin
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Origin", "http://example.com")
	w := httptest.NewRecorder()

	cors.ServeHTTP(w, req)

	if val := w.Header().Get("Access-Control-Allow-Origin"); val != "http://example.com" {
		t.Errorf("expected reflected origin http://example.com, got %q", val)
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

func TestCORS_CredentialsToggle(t *testing.T) {
	allowed := []string{"*"}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Case 1: credentials=false
	cors := CORS(allowed, false)(handler)
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Origin", "http://example.com")
	w := httptest.NewRecorder()

	cors.ServeHTTP(w, req)

	if val := w.Header().Get("Access-Control-Allow-Credentials"); val != "" {
		t.Errorf("expected no Access-Control-Allow-Credentials when disabled, got %q", val)
	}

	// Case 2: credentials=true
	cors = CORS(allowed, true)(handler)
	w = httptest.NewRecorder()

	cors.ServeHTTP(w, req)

	if val := w.Header().Get("Access-Control-Allow-Credentials"); val != "true" {
		t.Errorf("expected Access-Control-Allow-Credentials: true when enabled, got %q", val)
	}
}

func TestCORS_SpecificOrigin(t *testing.T) {
	allowed := []string{"http://trusted.com"}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	cors := CORS(allowed, true)(handler)

	// Trusted origin
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Origin", "http://trusted.com")
	w := httptest.NewRecorder()
	cors.ServeHTTP(w, req)

	if val := w.Header().Get("Access-Control-Allow-Origin"); val != "http://trusted.com" {
		t.Errorf("expected http://trusted.com, got %q", val)
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
