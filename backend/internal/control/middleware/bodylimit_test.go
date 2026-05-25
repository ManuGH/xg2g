// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package middleware

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestMaxBodyBytesRejectsOversizedBody proves the guardrail fires: a body past
// the cap fails the handler's read, while a body within the cap passes.
func TestMaxBodyBytesRejectsOversizedBody(t *testing.T) {
	const limit = 16
	handler := MaxBodyBytes(limit)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			http.Error(w, "too large", http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	small := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("12345"))
	wSmall := httptest.NewRecorder()
	handler.ServeHTTP(wSmall, small)
	if wSmall.Code != http.StatusOK {
		t.Fatalf("within-cap body status = %d, want %d", wSmall.Code, http.StatusOK)
	}

	big := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(make([]byte, limit+1)))
	wBig := httptest.NewRecorder()
	handler.ServeHTTP(wBig, big)
	if wBig.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body status = %d, want %d", wBig.Code, http.StatusRequestEntityTooLarge)
	}
}

// TestMaxBodyBytesDisabledPassesThrough proves a non-positive limit is an
// explicit opt-out, not an accidental zero-byte cap.
func TestMaxBodyBytesDisabledPassesThrough(t *testing.T) {
	handler := MaxBodyBytes(0)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, err := io.Copy(io.Discard, r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if n == 0 {
			http.Error(w, "empty", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	big := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(make([]byte, 1<<20)))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, big)
	if w.Code != http.StatusOK {
		t.Fatalf("passthrough status = %d, want %d", w.Code, http.StatusOK)
	}
}

// TestApplyStackInstallsBodyLimit proves the cap is actually wired into the
// canonical stack when configured. A GET carries the body so the CSRF guard
// (safe-method) lets the request through to the body-reading handler.
func TestApplyStackInstallsBodyLimit(t *testing.T) {
	r := NewRouter(StackConfig{MaxRequestBodyBytes: 16})
	r.Get("/echo", func(w http.ResponseWriter, req *http.Request) {
		if _, err := io.ReadAll(req.Body); err != nil {
			http.Error(w, "too large", http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewServer(r)
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/echo", bytes.NewReader(make([]byte, 64)))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body through stack status = %d, want %d", resp.StatusCode, http.StatusRequestEntityTooLarge)
	}
}
