// SPDX-License-Identifier: MIT

package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
)

func TestSecureFileServer_AllowlistDenylist(t *testing.T) {
	// Create a temporary directory with test files
	tmpDir := t.TempDir()

	// Create allowed files
	allowed := []string{"playlist.m3u", "xmltv.xml", "epg.xml"}
	for _, name := range allowed {
		if err := os.WriteFile(filepath.Join(tmpDir, name), []byte("allowed"), 0600); err != nil {
			t.Fatalf("Failed to create %s: %v", name, err)
		}
	}

	// Create denied/sensitive files
	denied := []string{"config.yaml", ".env", "secret.key", "other.txt"}
	for _, name := range denied {
		if err := os.WriteFile(filepath.Join(tmpDir, name), []byte("secret"), 0600); err != nil {
			t.Fatalf("Failed to create %s: %v", name, err)
		}
	}

	cfg := config.AppConfig{DataDir: tmpDir, Version: "test"}
	srv := New(cfg, nil)
	handler := http.StripPrefix("/files/", srv.secureFileServer())

	tests := []struct {
		filename   string
		wantStatus int
	}{
		{"playlist.m3u", http.StatusOK},
		{"xmltv.xml", http.StatusOK},
		{"epg.xml", http.StatusOK},
		{"config.yaml", http.StatusForbidden},
		{"playlist.m3u.bak", http.StatusForbidden},
		{".env", http.StatusForbidden},
		{"secret.key", http.StatusForbidden},
		{"other.txt", http.StatusForbidden}, // Default deny
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			// Ensure the request path mimics the router logic
			req := httptest.NewRequest(http.MethodGet, "/files/"+tt.filename, nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != tt.wantStatus {
				t.Errorf("File %s: status = %v, want %v", tt.filename, w.Code, tt.wantStatus)
			}
		})
	}
}

func TestSecureFileServer_RangeRequests(t *testing.T) {
	// Create a temporary directory with test files
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "playlist.m3u")
	testContent := "#EXTM3U\n#EXTINF:-1,Channel 1\nhttp://example.com/stream1\n#EXTINF:-1,Channel 2\nhttp://example.com/stream2\n"
	if err := os.WriteFile(testFile, []byte(testContent), 0600); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	cfg := config.AppConfig{
		DataDir: tmpDir,
		Version: "test",
	}
	srv := New(cfg, nil)

	tests := []struct {
		name        string
		rangeHeader string
		wantStatus  int
		wantPartial bool
		wantBody    string
		wantHeaders map[string]string
	}{
		{
			name:        "No Range header - full content",
			rangeHeader: "",
			wantStatus:  http.StatusOK,
			wantPartial: false,
			wantBody:    testContent,
		},
		{
			name:        "Range: bytes=0-9 - first 10 bytes",
			rangeHeader: "bytes=0-9",
			wantStatus:  http.StatusPartialContent,
			wantPartial: true,
			wantBody:    "#EXTM3U\n#E",
			wantHeaders: map[string]string{
				"Content-Range": "bytes 0-9/",
			},
		},
		{
			name:        "Range: bytes=10- - from byte 10 to end",
			rangeHeader: "bytes=10-",
			wantStatus:  http.StatusPartialContent,
			wantPartial: true,
			wantBody:    strings.TrimPrefix(testContent, "#EXTM3U\n#E"),
		},
		{
			name:        "Range: bytes=-20 - last 20 bytes",
			rangeHeader: "bytes=-20",
			wantStatus:  http.StatusPartialContent,
			wantPartial: true,
			wantBody:    "example.com/stream2\n", // Last 20 bytes
		},
		{
			name:        "Invalid Range header",
			rangeHeader: "bytes=invalid",
			wantStatus:  http.StatusRequestedRangeNotSatisfiable,
			wantPartial: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/files/playlist.m3u", nil)
			if tt.rangeHeader != "" {
				req.Header.Set("Range", tt.rangeHeader)
			}
			w := httptest.NewRecorder()

			handler := http.StripPrefix("/files/", srv.secureFileServer())
			handler.ServeHTTP(w, req)

			// Check status code
			if w.Code != tt.wantStatus {
				t.Errorf("Status = %v, want %v", w.Code, tt.wantStatus)
			}

			// For partial content, verify Content-Range header exists
			if tt.wantPartial {
				contentRange := w.Header().Get("Content-Range")
				if contentRange == "" {
					t.Error("Expected Content-Range header for partial content")
				}
			}

			// Verify body content if specified
			if tt.wantBody != "" && w.Code == http.StatusOK || w.Code == http.StatusPartialContent {
				body := w.Body.String()
				if body != tt.wantBody {
					t.Errorf("Body = %q, want %q", body, tt.wantBody)
				}
			}

			// Verify specific headers if provided
			for header, wantPrefix := range tt.wantHeaders {
				got := w.Header().Get(header)
				if !strings.HasPrefix(got, wantPrefix) {
					t.Errorf("Header %s = %q, want prefix %q", header, got, wantPrefix)
				}
			}
		})
	}
}

func TestSecureFileServer_ETagCaching(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "playlist.m3u")
	testContent := "#EXTM3U\n#EXTINF:-1,Test\nhttp://example.com/test\n"
	if err := os.WriteFile(testFile, []byte(testContent), 0600); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	cfg := config.AppConfig{
		DataDir: tmpDir,
		Version: "test",
	}
	srv := New(cfg, nil)

	// First request - get ETag
	req1 := httptest.NewRequest(http.MethodGet, "/files/playlist.m3u", nil)
	w1 := httptest.NewRecorder()
	handler := http.StripPrefix("/files/", srv.secureFileServer())
	handler.ServeHTTP(w1, req1)

	if w1.Code != http.StatusOK {
		t.Fatalf("First request failed with status %v", w1.Code)
	}

	etag := w1.Header().Get("ETag")
	if etag == "" {
		t.Fatal("Expected ETag header in response")
	}

	// Second request with If-None-Match - should return 304
	req2 := httptest.NewRequest(http.MethodGet, "/files/playlist.m3u", nil)
	req2.Header.Set("If-None-Match", etag)
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)

	if w2.Code != http.StatusNotModified {
		t.Errorf("Expected 304 Not Modified with matching ETag, got %v", w2.Code)
	}

	// Verify Cache-Control header
	cacheControl := w1.Header().Get("Cache-Control")
	if !strings.Contains(cacheControl, "max-age") {
		t.Errorf("Expected Cache-Control with max-age, got %q", cacheControl)
	}
}

func TestSecureFileServer_PathTraversal(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.AppConfig{
		DataDir: tmpDir,
		Version: "test",
	}
	srv := New(cfg, nil)

	tests := []struct {
		name       string
		path       string
		wantStatus int
	}{
		{
			name:       "Simple dot-dot traversal",
			path:       "/files/../etc/passwd",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "URL-encoded dot-dot",
			path:       "/files/%2e%2e/etc/passwd",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "Double-encoded dot-dot",
			path:       "/files/%252e%252e/etc/passwd",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "Backslash traversal",
			path:       "/files/..\\..\\etc\\passwd",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "Directory listing attempt",
			path:       "/files/",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "Valid file request",
			path:       "/files/playlist.m3u",
			wantStatus: http.StatusNotFound, // File doesn't exist, but path is valid
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			w := httptest.NewRecorder()

			handler := http.StripPrefix("/files/", srv.secureFileServer())
			handler.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("Status = %v, want %v", w.Code, tt.wantStatus)
			}
		})
	}
}

func TestSecureFileServer_ContentType(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test files with different extensions
	files := map[string]string{
		"playlist.m3u": "#EXTM3U\n",
		"xmltv.xml":    "<?xml version=\"1.0\"?>\n",
		"epg.xml":      "<?xml version=\"1.0\"?>\n",
	}

	for name, content := range files {
		path := filepath.Join(tmpDir, name)
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatalf("Failed to create %s: %v", name, err)
		}
	}

	cfg := config.AppConfig{
		DataDir: tmpDir,
		Version: "test",
	}
	srv := New(cfg, nil)

	tests := []struct {
		name            string
		file            string
		wantContentType string
	}{
		{
			name:            "M3U file",
			file:            "playlist.m3u",
			wantContentType: "audio/x-mpegurl; charset=utf-8",
		},
		{
			name:            "XMLTV file",
			file:            "xmltv.xml",
			wantContentType: "application/xml; charset=utf-8",
		},
		{
			name:            "EPG XML file",
			file:            "epg.xml",
			wantContentType: "application/xml; charset=utf-8",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/files/"+tt.file, nil)
			w := httptest.NewRecorder()

			handler := http.StripPrefix("/files/", srv.secureFileServer())
			handler.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("Request failed with status %v", w.Code)
			}

			contentType := w.Header().Get("Content-Type")
			if contentType != tt.wantContentType {
				t.Errorf("Content-Type = %q, want %q", contentType, tt.wantContentType)
			}
		})
	}
}

func TestSecureFileServer_MethodNotAllowed(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.AppConfig{
		DataDir: tmpDir,
		Version: "test",
	}
	srv := New(cfg, nil)

	methods := []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/files/playlist.m3u", nil)
			w := httptest.NewRecorder()

			handler := http.StripPrefix("/files/", srv.secureFileServer())
			handler.ServeHTTP(w, req)

			if w.Code != http.StatusMethodNotAllowed {
				t.Errorf("Method %s: status = %v, want %v", method, w.Code, http.StatusMethodNotAllowed)
			}
		})
	}
}
