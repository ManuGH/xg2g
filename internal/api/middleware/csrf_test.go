// SPDX-License-Identifier: MIT

package middleware

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestCSRFProtection_AllowsSafeMethodsWithoutOrigin(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	csrfHandler := CSRFProtection()(handler)

	// GET and HEAD are safe methods - should not require origin
	safeMethods := []string{http.MethodGet, http.MethodHead}

	for _, method := range safeMethods {
		req := httptest.NewRequest(method, "/test", nil)
		req.Host = "example.com"
		w := httptest.NewRecorder()

		csrfHandler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("%s request without origin: expected 200, got %d", method, w.Code)
		}
	}
}

func TestCSRFProtection_BlocksUnsafeMethodsWithoutOrigin(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	csrfHandler := CSRFProtection()(handler)

	// POST, PUT, DELETE, PATCH are unsafe methods - require origin
	unsafeMethods := []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch}

	for _, method := range unsafeMethods {
		req := httptest.NewRequest(method, "/test", nil)
		req.Host = "example.com"
		// No Origin or Referer header
		w := httptest.NewRecorder()

		csrfHandler.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("%s request without origin: expected 403, got %d", method, w.Code)
		}
	}
}

func TestCSRFProtection_AllowsSameOriginRequests(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	csrfHandler := CSRFProtection()(handler)

	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	req.Host = "example.com"
	req.Header.Set("Origin", "http://example.com")

	w := httptest.NewRecorder()
	csrfHandler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Same-origin POST: expected 200, got %d", w.Code)
	}
}

func TestCSRFProtection_AllowsSameOriginWithHTTPS(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	csrfHandler := CSRFProtection()(handler)

	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	req.Host = "example.com"
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("X-Forwarded-Proto", "https")

	w := httptest.NewRecorder()
	csrfHandler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Same-origin HTTPS POST: expected 200, got %d", w.Code)
	}
}

func TestCSRFProtection_BlocksCrossOriginRequests(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	csrfHandler := CSRFProtection()(handler)

	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	req.Host = "example.com"
	req.Header.Set("Origin", "http://evil.com")

	w := httptest.NewRecorder()
	csrfHandler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Cross-origin POST: expected 403, got %d", w.Code)
	}
}

func TestCSRFProtection_AllowsConfiguredOrigins(t *testing.T) {
	// Set allowed origins
	os.Setenv("XG2G_ALLOWED_ORIGINS", "http://trusted.com,https://another.com") //nolint:errcheck // Test setup
	defer os.Unsetenv("XG2G_ALLOWED_ORIGINS")                                   //nolint:errcheck // Test cleanup

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	csrfHandler := CSRFProtection()(handler)

	// Request from allowed origin
	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	req.Host = "example.com"
	req.Header.Set("Origin", "http://trusted.com")

	w := httptest.NewRecorder()
	csrfHandler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Allowed origin POST: expected 200, got %d", w.Code)
	}

	// Request from another allowed origin
	req = httptest.NewRequest(http.MethodPost, "/test", nil)
	req.Host = "example.com"
	req.Header.Set("Origin", "https://another.com")

	w = httptest.NewRecorder()
	csrfHandler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Another allowed origin POST: expected 200, got %d", w.Code)
	}

	// Request from non-allowed origin
	req = httptest.NewRequest(http.MethodPost, "/test", nil)
	req.Host = "example.com"
	req.Header.Set("Origin", "http://evil.com")

	w = httptest.NewRecorder()
	csrfHandler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Non-allowed origin POST: expected 403, got %d", w.Code)
	}
}

func TestCSRFProtection_FallbackToReferer(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	csrfHandler := CSRFProtection()(handler)

	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	req.Host = "example.com"
	// No Origin header, but valid Referer
	req.Header.Set("Referer", "http://example.com/page")

	w := httptest.NewRecorder()
	csrfHandler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Same-origin via Referer: expected 200, got %d", w.Code)
	}
}

func TestCSRFProtection_RefererCrossOriginBlocked(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	csrfHandler := CSRFProtection()(handler)

	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	req.Host = "example.com"
	req.Header.Set("Referer", "http://evil.com/page")

	w := httptest.NewRecorder()
	csrfHandler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Cross-origin via Referer: expected 403, got %d", w.Code)
	}
}

func TestCSRFProtection_OriginPriorityOverReferer(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	csrfHandler := CSRFProtection()(handler)

	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	req.Host = "example.com"
	// Both Origin and Referer - Origin should take priority
	req.Header.Set("Origin", "http://example.com")
	req.Header.Set("Referer", "http://evil.com/page")

	w := httptest.NewRecorder()
	csrfHandler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Origin priority: expected 200 (Origin takes priority), got %d", w.Code)
	}
}

func TestGetRequestOrigin_ExtractsFromOriginHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	req.Header.Set("Origin", "http://example.com")

	origin := getRequestOrigin(req)
	if origin != "http://example.com" {
		t.Errorf("Expected http://example.com, got %s", origin)
	}
}

func TestGetRequestOrigin_ExtractsFromReferer(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	req.Header.Set("Referer", "http://example.com/page?query=value")

	origin := getRequestOrigin(req)
	if origin != "http://example.com" {
		t.Errorf("Expected http://example.com, got %s", origin)
	}
}

func TestGetRequestOrigin_ReturnsEmptyWhenMissing(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/test", nil)

	origin := getRequestOrigin(req)
	if origin != "" {
		t.Errorf("Expected empty string, got %s", origin)
	}
}
