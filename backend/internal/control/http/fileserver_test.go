// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

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

// testMetrics provides a test implementation of FileMetrics
type testMetrics struct {
	denied  map[string]int
	allowed int
	hit     int
	miss    int
}

func newTestMetrics() *testMetrics {
	return &testMetrics{
		denied: make(map[string]int),
	}
}

func (m *testMetrics) Denied(reason string) {
	m.denied[reason]++
}

func (m *testMetrics) Allowed() {
	m.allowed++
}

func (m *testMetrics) CacheHit() {
	m.hit++
}

func (m *testMetrics) CacheMiss() {
	m.miss++
}

// Test that SecureFileServer blocks non-GET/HEAD methods
func TestSecureFileServer_MethodNotAllowed(t *testing.T) {
	tmpDir := t.TempDir()
	metrics := newTestMetrics()
	handler := SecureFileServer(tmpDir, metrics)

	req := httptest.NewRequest(http.MethodPost, "/playlist.m3u", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected 405, got %d", w.Code)
	}
	if metrics.denied["method_not_allowed"] != 1 {
		t.Errorf("Expected method_not_allowed metric, got %v", metrics.denied)
	}
}

// Test that SecureFileServer only allows allowlisted files
func TestSecureFileServer_OnlyAllowlist(t *testing.T) {
	tmpDir := t.TempDir()
	metrics := newTestMetrics()

	// Create a non-allowlisted file
	forbiddenPath := filepath.Join(tmpDir, "forbidden.txt")
	if err := os.WriteFile(forbiddenPath, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}

	handler := SecureFileServer(tmpDir, metrics)
	req := httptest.NewRequest(http.MethodGet, "/forbidden.txt", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Expected 403, got %d", w.Code)
	}
	if metrics.denied["forbidden_file"] != 1 {
		t.Errorf("Expected forbidden_file metric")
	}
}

// Test that SecureFileServer allows allowlisted file
func TestSecureFileServer_AllowlistAllowed(t *testing.T) {
	tmpDir := t.TempDir()
	metrics := newTestMetrics()

	// Create allowlisted public files
	playlistPath := filepath.Join(tmpDir, "playlist_public.m3u")
	content := []byte("#EXTM3U\ntest")
	if err := os.WriteFile(playlistPath, content, 0644); err != nil {
		t.Fatal(err)
	}

	handler := SecureFileServer(tmpDir, metrics)
	req := httptest.NewRequest(http.MethodGet, "/playlist.m3u", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}
	if metrics.allowed != 1 {
		t.Errorf("Expected allowed metric")
	}
	if metrics.miss != 1 {
		t.Errorf("Expected cache miss")
	}
}

// TestSecureFileServer_PlaylistSeparation verifies that requests for /playlist.m3u
// always serve playlist_public.m3u (masked) and never the internal playlist.m3u (raw).
func TestSecureFileServer_PlaylistSeparation(t *testing.T) {
	tmpDir := t.TempDir()
	metrics := newTestMetrics()

	rawPath := filepath.Join(tmpDir, "playlist.m3u")
	rawContent := []byte("#EXTM3U\n#EXTINF:-1,Raw Secret Channel\nhttp://canary.invalid/SECRET-TOKEN/live.ts\n")
	if err := os.WriteFile(rawPath, rawContent, 0600); err != nil {
		t.Fatal(err)
	}

	publicPath := filepath.Join(tmpDir, "playlist_public.m3u")
	publicContent := []byte("#EXTM3U\n#EXTINF:-1,Masked Public Channel\n/api/v3/stream/live/iptv_maskedid123\n")
	if err := os.WriteFile(publicPath, publicContent, 0644); err != nil {
		t.Fatal(err)
	}

	handler := SecureFileServer(tmpDir, metrics)

	// 1. Requesting /playlist.m3u must serve publicContent, NEVER rawContent
	req := httptest.NewRequest(http.MethodGet, "/playlist.m3u", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if body != string(publicContent) {
		t.Fatalf("Expected publicContent, got %q", body)
	}
	if strings.Contains(body, "SECRET-TOKEN") || strings.Contains(body, "canary.invalid") {
		t.Fatalf("Raw secrets leaked in /playlist.m3u response: %s", body)
	}

	// 2. Requesting /playlist_public.m3u directly also serves publicContent
	reqPub := httptest.NewRequest(http.MethodGet, "/playlist_public.m3u", nil)
	wPub := httptest.NewRecorder()
	handler.ServeHTTP(wPub, reqPub)
	if wPub.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", wPub.Code)
	}
	if wPub.Body.String() != string(publicContent) {
		t.Fatalf("Expected publicContent for /playlist_public.m3u")
	}

	// 3. When playlist_public.m3u is missing, /playlist.m3u fails closed (404) and never serves raw playlist.m3u
	if err := os.Remove(publicPath); err != nil {
		t.Fatal(err)
	}
	wMissing := httptest.NewRecorder()
	handler.ServeHTTP(wMissing, req)
	if wMissing.Code != http.StatusNotFound {
		t.Fatalf("Expected 404 when public playlist is missing, got %d", wMissing.Code)
	}
}

// Test path traversal is blocked
func TestSecureFileServer_PathTraversal(t *testing.T) {
	tmpDir := t.TempDir()
	metrics := newTestMetrics()
	handler := SecureFileServer(tmpDir, metrics)

	tests := []string{
		"/../playlist.m3u",
		"/playlist.m3u/../../../etc/passwd",
		"/playlist.m3u%2F..%2F..%2Fetc%2Fpasswd",
	}

	for _, path := range tests {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("Path %s: expected 403, got %d", path, w.Code)
		}
	}

	if metrics.denied["path_escape"] == 0 {
		t.Error("Expected path_escape denials")
	}
}

// Test directory listing is blocked
func TestSecureFileServer_NoDirectoryListing(t *testing.T) {
	tmpDir := t.TempDir()
	metrics := newTestMetrics()
	handler := SecureFileServer(tmpDir, metrics)

	// Use allowlisted filename + trailing slash to reach directory_listing branch
	// (avoids allowlist preemption)
	req := httptest.NewRequest(http.MethodGet, "/xmltv.xml/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Expected 403, got %d", w.Code)
	}
	if metrics.denied["directory_listing"] == 0 {
		t.Errorf("Expected directory_listing denial, got %v", metrics.denied)
	}
}

// Test ETag caching works
func TestSecureFileServer_ETagCaching(t *testing.T) {
	tmpDir := t.TempDir()
	metrics := newTestMetrics()

	playlistPath := filepath.Join(tmpDir, "playlist_public.m3u")
	if err := os.WriteFile(playlistPath, []byte("#EXTM3U"), 0644); err != nil {
		t.Fatal(err)
	}

	handler := SecureFileServer(tmpDir, metrics)

	// First request - should get ETag
	req1 := httptest.NewRequest(http.MethodGet, "/playlist.m3u", nil)
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req1)

	etag := w1.Header().Get("ETag")
	if etag == "" {
		t.Fatal("Expected ETag header")
	}

	// Second request with If-None-Match - should get 304
	req2 := httptest.NewRequest(http.MethodGet, "/playlist.m3u", nil)
	req2.Header.Set("If-None-Match", etag)
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)

	if w2.Code != http.StatusNotModified {
		t.Errorf("Expected 304, got %d", w2.Code)
	}
	if metrics.hit != 1 {
		t.Error("Expected cache hit")
	}
}
