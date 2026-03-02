// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStack_EnforcesCSRF(t *testing.T) {
	r := NewRouter(StackConfig{
		EnableCORS:            true,
		AllowedOrigins:        nil,
		EnableSecurityHeaders: false,
		EnableMetrics:         false,
		EnableLogging:         false,
		EnableRateLimit:       false,
	})

	r.Post("/mutate", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/mutate", nil)
	req.Host = "example.com"
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 from CSRF middleware, got %d", w.Code)
	}
}

func TestStack_AllowsSameOrigin(t *testing.T) {
	r := NewRouter(StackConfig{
		EnableCORS:            true,
		AllowedOrigins:        nil,
		EnableSecurityHeaders: false,
		EnableMetrics:         false,
		EnableLogging:         false,
		EnableRateLimit:       false,
	})

	r.Post("/mutate", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/mutate", nil)
	req.Host = "example.com"
	req.Header.Set("Origin", "http://example.com")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for same-origin request, got %d", w.Code)
	}
}
