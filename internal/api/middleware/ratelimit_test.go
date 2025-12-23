// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimit_EnforcesLimit(t *testing.T) {
	// Create a test handler that always returns 200 OK
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	// Apply rate limiting: 3 requests per second
	limiter := RateLimit(RateLimitConfig{
		RequestLimit: 3,
		WindowSize:   time.Second,
	})
	limitedHandler := limiter(handler)

	// First 3 requests should succeed
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "192.168.1.1:12345" // Same IP for all requests
		w := httptest.NewRecorder()

		limitedHandler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Request %d: expected status 200, got %d", i+1, w.Code)
		}
	}

	// 4th request should be rate limited
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	w := httptest.NewRecorder()

	limitedHandler.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("4th request: expected status 429, got %d", w.Code)
	}

	// Check for Retry-After header
	retryAfter := w.Header().Get("Retry-After")
	if retryAfter == "" {
		t.Error("Expected Retry-After header in rate limit response")
	}

	// Check for JSON error response
	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %s", contentType)
	}
}

func TestRateLimit_DifferentIPsIndependent(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	limiter := RateLimit(RateLimitConfig{
		RequestLimit: 2,
		WindowSize:   time.Second,
	})
	limitedHandler := limiter(handler)

	// IP 1: Make 2 requests (at limit)
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		w := httptest.NewRecorder()
		limitedHandler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("IP1 request %d: expected 200, got %d", i+1, w.Code)
		}
	}

	// IP 2: Should still be able to make requests
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "192.168.1.2:12345"
	w := httptest.NewRecorder()
	limitedHandler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("IP2 request: expected 200, got %d", w.Code)
	}

	// IP 1: 3rd request should be rate limited
	req = httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	w = httptest.NewRecorder()
	limitedHandler.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("IP1 3rd request: expected 429, got %d", w.Code)
	}
}

func TestRefreshRateLimit_Configuration(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Apply refresh rate limiter
	limitedHandler := RefreshRateLimit()(handler)

	// Make 10 requests (at limit)
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/refresh", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		w := httptest.NewRecorder()
		limitedHandler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Request %d: expected 200, got %d", i+1, w.Code)
		}
	}

	// 11th request should be rate limited
	req := httptest.NewRequest(http.MethodPost, "/api/refresh", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	w := httptest.NewRecorder()
	limitedHandler.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("11th request: expected 429, got %d", w.Code)
	}
}

func TestAPIRateLimit_Configuration(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Apply API rate limiter: Enabled=true, RPS=1 (60 RPM), Burst=20, Whitelist=nil
	limitedHandler := APIRateLimit(true, 1, 20, nil)(handler)

	// Make 60 requests (at limit)
	for i := 0; i < 60; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v2/status", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		w := httptest.NewRecorder()
		limitedHandler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Request %d: expected 200, got %d", i+1, w.Code)
		}
	}

	// 61st request should be rate limited
	req := httptest.NewRequest(http.MethodGet, "/api/v2/status", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	w := httptest.NewRecorder()
	limitedHandler.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("61st request: expected 429, got %d", w.Code)
	}
}

func TestRateLimit_WhitelistCIDR(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	limiter := RateLimit(RateLimitConfig{
		RequestLimit: 1,
		WindowSize:   time.Second,
		Whitelist:    []string{"192.168.0.0/16"},
	})
	limitedHandler := limiter(handler)

	// Whitelisted subnet should bypass rate limiting
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "192.168.1.10:12345"
		w := httptest.NewRecorder()
		limitedHandler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("whitelisted request %d: expected 200, got %d", i+1, w.Code)
		}
	}

	// Non-whitelisted IP should be rate limited after first request
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	w := httptest.NewRecorder()
	limitedHandler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("non-whitelisted first request: expected 200, got %d", w.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	w = httptest.NewRecorder()
	limitedHandler.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("non-whitelisted second request: expected 429, got %d", w.Code)
	}
}
