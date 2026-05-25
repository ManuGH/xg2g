// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/authz"
)

func TestExposureSecurityMiddlewareRateLimitsSensitiveClasses(t *testing.T) {
	srv := NewServer(config.AppConfig{
		RateLimitEnabled: true,
		RateLimitAuth:    2,
	}, nil, nil)
	policy, ok := authz.ExposurePolicyForOperation("StartPairing")
	if !ok {
		t.Fatal("missing StartPairing exposure policy")
	}

	handler := srv.ExposureSecurityMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	for i := 0; i < 2; i++ {
		req := exposureRequest("StartPairing", policy)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusNoContent {
			t.Fatalf("request %d status = %d, want %d", i, w.Code, http.StatusNoContent)
		}
		if got := w.Header().Get("Cache-Control"); got != "no-store" {
			t.Fatalf("request %d cache-control = %q, want no-store", i, got)
		}
	}

	req := exposureRequest("StartPairing", policy)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("limited request status = %d, want %d", w.Code, http.StatusTooManyRequests)
	}
	if got := w.Header().Get("X-RateLimit-Class"); got != string(authz.ExposureRateLimitPairingStart) {
		t.Fatalf("rate limit class header = %q", got)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("limited request cache-control = %q, want no-store", got)
	}
}

// TestExposureSecurityMiddlewareThrottlesHouseholdPINUnlock proves the PIN
// unlock endpoint is brute-force throttled by default: PostHouseholdUnlock
// carries the dedicated "auth" rate-limit class, and the middleware blocks once
// the configured per-IP attempt budget is spent. This is the load-bearing proof
// that the guardrail fires (the throttle existed but was previously unproven).
func TestExposureSecurityMiddlewareThrottlesHouseholdPINUnlock(t *testing.T) {
	srv := NewServer(config.AppConfig{
		RateLimitEnabled: true,
		RateLimitAuth:    2,
	}, nil, nil)
	policy, ok := authz.ExposurePolicyForOperation("PostHouseholdUnlock")
	if !ok {
		t.Fatal("missing PostHouseholdUnlock exposure policy")
	}
	if policy.RateLimitClass != authz.ExposureRateLimitAuth {
		t.Fatalf("PostHouseholdUnlock rate-limit class = %q, want %q", policy.RateLimitClass, authz.ExposureRateLimitAuth)
	}

	handler := srv.ExposureSecurityMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	for i := 0; i < 2; i++ {
		req := exposureRequest("PostHouseholdUnlock", policy)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusNoContent {
			t.Fatalf("attempt %d status = %d, want %d (throttled too early)", i, w.Code, http.StatusNoContent)
		}
	}

	req := exposureRequest("PostHouseholdUnlock", policy)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("PIN unlock not throttled after budget: status = %d, want %d", w.Code, http.StatusTooManyRequests)
	}
	if got := w.Header().Get("X-RateLimit-Class"); got != string(authz.ExposureRateLimitAuth) {
		t.Fatalf("rate-limit class header = %q, want %q", got, string(authz.ExposureRateLimitAuth))
	}
}

func exposureRequest(operationID string, policy authz.ExposurePolicy) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/v3/pairing/start", nil)
	req.RemoteAddr = "203.0.113.10:55000"
	ctx := context.WithValue(req.Context(), operationIDKey, operationID)
	ctx = context.WithValue(ctx, exposurePolicyKey, policy)
	return req.WithContext(ctx)
}
