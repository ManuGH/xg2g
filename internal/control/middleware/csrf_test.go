package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCSRFProtection_AllowsSafeMethodsWithoutOrigin(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	csrfHandler := CSRFProtection(nil)(handler)

	// GET and HEAD are safe methods - should not require origin
	safeMethods := []string{http.MethodGet, http.MethodHead, http.MethodOptions}

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

	csrfHandler := CSRFProtection(nil)(handler)

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

func TestCSRFProtection_AllowsSameOriginNoProxy(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	csrfHandler := CSRFProtection(nil)(handler)

	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	req.Host = "example.com"
	req.Header.Set("Origin", "http://example.com")

	w := httptest.NewRecorder()
	csrfHandler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Same-origin POST without proxies: expected 200, got %d", w.Code)
	}
}

func TestCSRFProtection_BlocksProxyHeadersWhenAllowlistEmpty(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	csrfHandler := CSRFProtection(nil)(handler)

	proxyHeaders := []string{
		"Forwarded",
		"X-Forwarded-For",
		"X-Forwarded-Host",
		"X-Forwarded-Proto",
	}

	for _, h := range proxyHeaders {
		req := httptest.NewRequest(http.MethodPost, "/test", nil)
		req.Host = "example.com"
		req.Header.Set("Origin", "http://example.com")
		req.Header.Set(h, "any-value")

		w := httptest.NewRecorder()
		csrfHandler.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("Same-origin POST with %s: expected 403, got %d", h, w.Code)
		}
	}
}

func TestCSRFProtection_AllowsAllowedOriginEvenWithProxies(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	allowed := []string{"http://trusted.com"}
	csrfHandler := CSRFProtection(allowed)(handler)

	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	req.Host = "example.com"
	req.Header.Set("Origin", "http://trusted.com")
	req.Header.Set("X-Forwarded-Proto", "https") // proxy header present

	w := httptest.NewRecorder()
	csrfHandler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Allowed origin POST with proxy headers: expected 200, got %d", w.Code)
	}
}

func TestCSRFProtection_AllowsWildcardOriginEvenWithProxies(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	allowed := []string{"*"}
	csrfHandler := CSRFProtection(allowed)(handler)

	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	req.Host = "example.com"
	req.Header.Set("Origin", "http://any-origin.com")
	req.Header.Set("Forwarded", "for=1.2.3.4")

	w := httptest.NewRecorder()
	csrfHandler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Wildcard origin POST with proxy headers: expected 200, got %d", w.Code)
	}
}

func TestCSRFProtection_ProblemResponse(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	csrfHandler := CSRFProtection(nil)(handler)

	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	req.Host = "example.com"
	w := httptest.NewRecorder()

	csrfHandler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("unexpected content type: %s", ct)
	}

	var payload map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode problem response: %v", err)
	}
	if payload["code"] != "CSRF_FORBIDDEN" {
		t.Fatalf("unexpected code: %v", payload["code"])
	}
}

func TestCSRFProtection_NormalizesDefaultPortOrigins(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	csrfHandler := CSRFProtection([]string{"https://example.com:443"})(handler)

	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	req.Host = "example.com"
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("X-Forwarded-Proto", "https")

	w := httptest.NewRecorder()
	csrfHandler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for normalized default-port origin, got %d", w.Code)
	}
}

func TestCSRFProtection_BlocksRelativeReferer(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	csrfHandler := CSRFProtection(nil)(handler)

	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	req.Host = "example.com"
	req.Header.Set("Referer", "/relative/path")

	w := httptest.NewRecorder()
	csrfHandler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for relative referer, got %d", w.Code)
	}
}
