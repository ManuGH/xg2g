// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package openwebif

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/ManuGH/xg2g/internal/resilience"
	"golang.org/x/time/rate"
)

// M12: a legitimately-empty primary response must not be discarded when the fallback
// endpoint then fails — the whole services refresh would otherwise abort with an error.
func TestServices_EmptyPrimaryNotDiscardedByFailingFallback(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/getservices": // nested primary: valid but empty
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"services":[]}`))
		case "/api/getallservices": // flat fallback: hard 5xx failure
			http.Error(w, "boom", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer s.Close()

	c := newTestClient(s.URL)
	svcs, err := c.Services(context.Background(), "1:0:0")
	if err != nil {
		t.Fatalf("empty primary must not be discarded by a failing fallback: %v", err)
	}
	if len(svcs) != 0 {
		t.Fatalf("expected 0 services, got %d", len(svcs))
	}
}

// M13: upstream HTTP 5xx must be classified as a technical failure so the circuit
// breaker can trip. get() previously called the status-blind package isTechnicalError,
// so 5xx were never recorded and the breaker never opened on a failing receiver.
func TestGet_RecordsUpstream5xxIntoBreaker(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "upstream boom", http.StatusInternalServerError)
	}))
	defer s.Close()

	c := newTestClient(s.URL)
	// Keep the test fast and deterministic: no HTTP retries, no rate-limit waits.
	c.maxRetries = 0
	c.backoff = 0
	c.receiverLimiter = rate.NewLimiter(rate.Inf, 1)

	// Breaker is configured threshold=5 failures, minAttempts=10. Drive enough failing
	// requests to cross minAttempts; the breaker must open (and short-circuit further calls).
	var sawCircuitOpen bool
	for i := 0; i < 20; i++ {
		_, err := c.Bouquets(context.Background())
		if errors.Is(err, resilience.ErrCircuitOpen) {
			sawCircuitOpen = true
			break
		}
	}
	if !sawCircuitOpen {
		t.Fatal("circuit breaker never opened on repeated upstream 5xx — 5xx not recorded as a technical failure")
	}
}

// L20: a successful-but-empty primary EPG response is authoritative and must not fall
// through to the fallback endpoint (which logged a spurious failure and doubled load).
func TestGetEPG_EmptyPrimaryNotTreatedAsFailure(t *testing.T) {
	var fallbackCalls atomic.Int64
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/epgservice": // primary: valid, empty
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"events": []any{}})
		case "/web/epgservice": // fallback must NOT be reached
			fallbackCalls.Add(1)
			http.Error(w, "should not be called", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer s.Close()

	c := newTestClient(s.URL)
	events, err := c.GetEPG(context.Background(), "1:0:1:100:200:300:0:0:0:0:", 7)
	if err != nil {
		t.Fatalf("empty-but-OK primary EPG must not error: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("expected 0 events, got %d", len(events))
	}
	if n := fallbackCalls.Load(); n != 0 {
		t.Fatalf("fallback endpoint must not be called for an empty-but-successful primary; got %d calls", n)
	}
}
