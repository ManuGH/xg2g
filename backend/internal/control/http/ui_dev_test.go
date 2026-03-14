//go:build dev

// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package http

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUIHandler_DevProxyRewritesToUIBasePath(t *testing.T) {
	t.Setenv("XG2G_UI_DEV_DIR", "")

	gotPath := ""
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("proxied"))
	}))
	defer upstream.Close()

	t.Setenv("XG2G_UI_DEV_PROXY_URL", upstream.URL)

	handler := UIHandler(UIConfig{
		CSP: "default-src 'self'; connect-src 'self'",
	})

	req := httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if gotPath != "/ui/assets/app.js" {
		t.Fatalf("proxied path = %q, want %q", gotPath, "/ui/assets/app.js")
	}

	cacheControl := w.Header().Get("Cache-Control")
	if !strings.Contains(cacheControl, "no-cache") || !strings.Contains(cacheControl, "no-store") {
		t.Fatalf("expected dev no-cache headers, got %q", cacheControl)
	}

	csp := w.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "ws:") || !strings.Contains(csp, "wss:") {
		t.Fatalf("expected websocket-capable CSP in dev mode, got %q", csp)
	}
	if !strings.Contains(csp, "script-src 'self' 'unsafe-inline'") {
		t.Fatalf("expected Vite-compatible script-src in dev mode, got %q", csp)
	}
}

func TestUIHandler_DevStaticDirServesIndex(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<!doctype html><html><body>dev ui</body></html>"), 0o644); err != nil {
		t.Fatalf("write index.html: %v", err)
	}

	t.Setenv("XG2G_UI_DEV_DIR", dir)
	t.Setenv("XG2G_UI_DEV_PROXY_URL", "")

	handler := UIHandler(UIConfig{CSP: "default-src 'self'"})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if !strings.Contains(w.Body.String(), "dev ui") {
		t.Fatalf("expected static dir index.html body, got %q", w.Body.String())
	}

	cacheControl := w.Header().Get("Cache-Control")
	if !strings.Contains(cacheControl, "no-cache") || !strings.Contains(cacheControl, "no-store") {
		t.Fatalf("expected dev no-cache headers, got %q", cacheControl)
	}
}
