// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package http

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestUIHandler_IndexNoCache verifies index.html is not cached
func TestUIHandler_IndexNoCache(t *testing.T) {
	handler := UIHandler(UIConfig{CSP: "default-src 'self'"})

	tests := []string{"/", "/index.html"}
	for _, path := range tests {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		cacheControl := w.Header().Get("Cache-Control")
		if !strings.Contains(cacheControl, "no-cache") && !strings.Contains(cacheControl, "no-store") {
			t.Errorf("Path %s: expected no-cache for index, got: %s", path, cacheControl)
		}
	}
}

// TestUIHandler_AssetsCached verifies hashed assets are cached
func TestUIHandler_AssetsCached(t *testing.T) {
	handler := UIHandler(UIConfig{CSP: "default-src 'self'"})

	entries, err := fs.ReadDir(uiFS, "dist/assets")
	if err != nil || len(entries) == 0 {
		t.Skip("no embedded assets available for cache test")
	}
	assetName := ""
	for _, entry := range entries {
		if !entry.IsDir() {
			assetName = entry.Name()
			break
		}
	}
	if assetName == "" {
		t.Skip("no embedded assets available for cache test")
	}

	req := httptest.NewRequest(http.MethodGet, "/assets/"+assetName, nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	cacheControl := w.Header().Get("Cache-Control")
	hasImmutable := strings.Contains(cacheControl, "immutable")
	hasMaxAge := strings.Contains(cacheControl, "max-age=31536000")

	if !hasImmutable && !hasMaxAge {
		t.Errorf("Expected immutable or max-age for asset, got: %s", cacheControl)
	}
}

// TestUIHandler_CSPHeader verifies CSP header is set from config
func TestUIHandler_CSPHeader(t *testing.T) {
	testCSP := "default-src 'self'; media-src blob:"
	handler := UIHandler(UIConfig{CSP: testCSP})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	csp := w.Header().Get("Content-Security-Policy")
	if csp != testCSP {
		t.Errorf("Expected CSP %q, got %q", testCSP, csp)
	}
}

// TestUIHandler_EmptyCSP verifies handler works with empty CSP
func TestUIHandler_EmptyCSP(t *testing.T) {
	handler := UIHandler(UIConfig{CSP: ""})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Should not error, just serve with no CSP header
	if w.Code == http.StatusInternalServerError {
		t.Error("Handler should not fail with empty CSP")
	}
}
