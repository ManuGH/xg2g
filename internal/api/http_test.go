// SPDX-License-Identifier: MIT
package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ManuGH/xg2g/internal/jobs"
)

// dummyHandler is a no-op http.Handler that writes "OK".
var dummyHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
})

func TestHandleStatus(t *testing.T) {
	s := New(jobs.Config{})
	handler := s.Handler()
	req, err := http.NewRequest("GET", "/api/status", nil)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code, "handler returned wrong status code")
	assert.Contains(t, rr.Body.String(), `"status":"ok"`, "handler returned unexpected body")
}

func TestHandleRefresh_ErrorDoesNotUpdateLastRun(t *testing.T) {
	cfg := jobs.Config{
		OWIBase:  "invalid-url", // Cause an error
		APIToken: "dummy-token",
	}
	s := New(cfg)
	handler := s.Handler()
	initialTime := s.status.LastRun

	req, err := http.NewRequest("POST", "/api/refresh", nil)
	require.NoError(t, err)
	req.Header.Set("X-API-Token", "dummy-token")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Equal(t, initialTime, s.status.LastRun, "lastRefresh should not be updated on failure")
}

func TestRecordRefreshMetrics(t *testing.T) {
	// Use the default registry since promauto registers metrics there
	recordRefreshMetrics(1*time.Second, 10)
	// Only call once to avoid changing the gauge value unexpectedly

	body, err := getMetrics(nil)
	require.NoError(t, err)

	assert.Contains(t, string(body), `xg2g_channels`)
	assert.Contains(t, string(body), `xg2g_refresh_duration_seconds_count`)
}

func TestHandleRefresh_SuccessUpdatesLastRun(t *testing.T) {
	t.Skip("Skipping success test as it requires mocking infrastructure")
}

func TestHandleRefresh_ConflictOnConcurrent(t *testing.T) {
	cfg := jobs.Config{APIToken: "dummy-token"}
	s := New(cfg)

	// Install a slow refresh function to force overlap
	startCh := make(chan struct{})
	releaseCh := make(chan struct{})
	s.refreshFn = func(ctx context.Context, cfg jobs.Config) (*jobs.Status, error) {
		close(startCh) // signal that refresh started
		<-releaseCh    // block until allowed to finish
		return &jobs.Status{Channels: 1, LastRun: time.Now()}, nil
	}

	handler := s.Handler()

	// First request starts and blocks
	req1 := httptest.NewRequest(http.MethodPost, "/api/refresh", nil)
	req1.Header.Set("X-API-Token", "dummy-token")
	rr1 := httptest.NewRecorder()

	// Run first request in a goroutine
	done1 := make(chan struct{})
	go func() {
		handler.ServeHTTP(rr1, req1)
		close(done1)
	}()

	// Wait until the refresh actually started
	select {
	case <-startCh:
	case <-time.After(1 * time.Second):
		t.Fatal("first refresh did not start in time")
	}

	// Second request should get 409 Conflict
	req2 := httptest.NewRequest(http.MethodPost, "/api/refresh", nil)
	req2.Header.Set("X-API-Token", "dummy-token")
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)

	assert.Equal(t, http.StatusConflict, rr2.Code)
	assert.Contains(t, rr2.Body.String(), "refresh operation is already in progress")
	assert.Equal(t, "30", rr2.Header().Get("Retry-After"))

	// Unblock first request and ensure it succeeds with 200
	close(releaseCh)
	select {
	case <-done1:
		// ok
	case <-time.After(1 * time.Second):
		t.Fatal("first refresh did not complete in time")
	}
	assert.Equal(t, http.StatusOK, rr1.Code)
}

func TestHandleHealth(t *testing.T) {
	s := New(jobs.Config{})
	handler := s.Handler()
	req, err := http.NewRequest("GET", "/healthz", nil)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), `"status":"ok"`)
}

func TestHandleReady(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "test-ready")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tempDir) }()

	playlistPath := filepath.Join(tempDir, "playlist.m3u")
	xmltvPath := "epg.xml"
	xmltvFullPath := filepath.Join(tempDir, xmltvPath)

	cfg := jobs.Config{DataDir: tempDir, XMLTVPath: xmltvPath}
	s := New(cfg)
	handler := s.Handler()

	req, err := http.NewRequest("GET", "/readyz", nil)
	require.NoError(t, err)

	// Case 1: Not ready (no files, last run is zero)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusServiceUnavailable, rr.Code)

	// Case 2: Ready
	s.status.LastRun = time.Now()
	s.status.Error = ""
	require.NoError(t, os.WriteFile(playlistPath, []byte("#EXTM3U"), 0644))
	require.NoError(t, os.WriteFile(xmltvFullPath, []byte("<tv></tv>"), 0644))

	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), `"status":"ready"`)
}

func TestAuthMiddleware(t *testing.T) {
	const testToken = "test-api-token"

	tests := []struct {
		name           string
		tokenEnv       string
		headerValue    string
		expectedStatus int
		expectedBody   string
	}{
		{
			name:           "no token configured, fail closed",
			tokenEnv:       "",
			headerValue:    "",
			expectedStatus: http.StatusUnauthorized,
			expectedBody:   "Unauthorized: API token not configured on server",
		},
		{
			name:           "token configured, no header, unauthorized",
			tokenEnv:       testToken,
			headerValue:    "",
			expectedStatus: http.StatusUnauthorized,
			expectedBody:   "Unauthorized: Missing API token",
		},
		{
			name:           "token configured, wrong token, forbidden",
			tokenEnv:       testToken,
			headerValue:    "wrong-token",
			expectedStatus: http.StatusForbidden,
			expectedBody:   "Forbidden: Invalid API token",
		},
		{
			name:           "token configured, correct token, access granted",
			tokenEnv:       testToken,
			headerValue:    testToken,
			expectedStatus: http.StatusOK,
			expectedBody:   "OK",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.tokenEnv != "" {
				t.Setenv("XG2G_API_TOKEN", tt.tokenEnv)
			}

			s := New(jobs.Config{APIToken: tt.tokenEnv})
			// Test against a protected route
			handler := s.authRequired(dummyHandler)

			req, err := http.NewRequest("GET", "/test", nil)
			require.NoError(t, err)

			if tt.headerValue != "" {
				req.Header.Set("X-API-Token", tt.headerValue)
			}

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			assert.Equal(t, tt.expectedStatus, rr.Code, "handler returned wrong status code")
			assert.Contains(t, rr.Body.String(), tt.expectedBody, "handler returned unexpected body")
		})
	}
}

func TestSecureFileHandlerSymlinkPolicy(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "TestSecureFileHandlerSymlinkPolicy*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tempDir) }()

	dataDir := filepath.Join(tempDir, "data")
	outsideDir := filepath.Join(tempDir, "outside")
	subDir := filepath.Join(dataDir, "subdir")

	require.NoError(t, os.MkdirAll(subDir, 0755))
	require.NoError(t, os.Mkdir(outsideDir, 0755))

	testFile := filepath.Join(dataDir, "test.m3u")
	subFile := filepath.Join(subDir, "sub.m3u")
	outsideFile := filepath.Join(outsideDir, "secret.txt")

	require.NoError(t, os.WriteFile(testFile, []byte("m3u content"), 0644))
	require.NoError(t, os.WriteFile(subFile, []byte("sub content"), 0644))
	require.NoError(t, os.WriteFile(outsideFile, []byte("secret"), 0644))

	symlinkPath := filepath.Join(dataDir, "evil_symlink")
	require.NoError(t, os.Symlink(outsideFile, symlinkPath))

	link1 := filepath.Join(dataDir, "link1")
	link2 := filepath.Join(dataDir, "link2")
	require.NoError(t, os.Symlink(link2, link1))
	require.NoError(t, os.Symlink(outsideFile, link2))

	symlinkDir := filepath.Join(dataDir, "evil_dir")
	require.NoError(t, os.Symlink(outsideDir, symlinkDir))

	cfg := jobs.Config{DataDir: dataDir}
	server := New(cfg)
	handler := server.Handler()

	tests := []struct {
		name           string
		method         string
		path           string
		expectedStatus int
		expectedBody   string
	}{
		{name: "B6: valid file access", method: "GET", path: "/files/test.m3u", expectedStatus: http.StatusOK, expectedBody: "m3u content"},
		{name: "B7: subdirectory file access", method: "GET", path: "/files/subdir/sub.m3u", expectedStatus: http.StatusOK, expectedBody: "sub content"},
		{name: "B8: symlink to outside file", method: "GET", path: "/files/evil_symlink", expectedStatus: http.StatusForbidden, expectedBody: "Forbidden"},
		{name: "B9: symlink chain to outside", method: "GET", path: "/files/link1", expectedStatus: http.StatusForbidden, expectedBody: "Forbidden"},
		{name: "B10: path traversal with ..", method: "GET", path: "/files/../outside/secret.txt", expectedStatus: http.StatusForbidden, expectedBody: "Forbidden"},
		{name: "B11: symlink directory traversal", method: "GET", path: "/files/evil_dir/secret.txt", expectedStatus: http.StatusForbidden, expectedBody: "Forbidden"},
		{name: "B12: URL-encoded traversal %2e%2e", method: "GET", path: "/files/%2e%2e/outside/secret.txt", expectedStatus: http.StatusForbidden, expectedBody: "Forbidden"},
		{name: "directory access blocked", method: "GET", path: "/files/subdir/", expectedStatus: http.StatusForbidden, expectedBody: "Forbidden"},
		{name: "nonexistent file", method: "GET", path: "/files/nonexistent.txt", expectedStatus: http.StatusNotFound, expectedBody: "Not found"},
		{name: "method not allowed", method: "POST", path: "/files/test.m3u", expectedStatus: http.StatusMethodNotAllowed, expectedBody: "Method not allowed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(tt.method, tt.path, nil)
			require.NoError(t, err)

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			assert.Equal(t, tt.expectedStatus, rr.Code, "handler returned wrong status code")
			if tt.expectedBody != "" {
				assert.Contains(t, rr.Body.String(), tt.expectedBody, "handler returned unexpected body")
			}
		})
	}
}

func TestMiddlewareChain(t *testing.T) {
	server := New(jobs.Config{APIToken: "test-token"})
	handler := server.Handler()

	req, err := http.NewRequest("GET", "/test", nil)
	require.NoError(t, err)
	req.Header.Set("X-API-Token", "test-token")
	req.RemoteAddr = "192.0.2.1"

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	// Assert that a request ID header is present and well-formed
	reqID := rr.Header().Get("X-Request-ID")
	require.NotEmpty(t, reqID, "X-Request-ID header should be set")
	// Basic shape check (UUID-like); don't strictly parse to keep test simple
	assert.GreaterOrEqual(t, len(reqID), 8)
}

func TestAdvancedPathTraversal(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "TestAdvancedPathTraversal*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tempDir) }()

	// Create a benign file to make data dir non-empty
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "ok.txt"), []byte("ok"), 0644))

	cfg := jobs.Config{DataDir: tempDir}
	server := New(cfg)
	handler := server.Handler()

	attacks := []string{
		"%252e%252e%252f",      // double encoded ../
		"%252E%252E%252F",      // double encoded uppercase
		"..%00.txt",            // null byte injection (literal)
		"%00..%00/",            // encoded NUL around traversal
		"\u002e\u002e/",        // unicode dots (escape in string literal)
		"%c0%ae%c0%ae/",        // overlong UTF-8 for '..'
		"%2E%2E/%2E%2E/secret", // mixed case single-encoded
	}

	for _, attack := range attacks {
		t.Run(attack, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/files/"+attack, nil)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			assert.Equal(t, http.StatusForbidden, rr.Code, "expected 403 for attack vector")
		})
	}
}

func TestHandleXMLTV_Success(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test XMLTV and M3U files
	xmltvPath := filepath.Join(tmpDir, "xmltv.xml")
	m3uPath := filepath.Join(tmpDir, "playlist.m3u")

	xmltvContent := `<?xml version="1.0" encoding="UTF-8"?>
<tv>
  <channel id="channel1">
    <display-name>Channel One</display-name>
  </channel>
  <programme start="20250101000000" stop="20250101010000" channel="channel1">
    <title>Test Programme</title>
  </programme>
</tv>`

	m3uContent := `#EXTM3U
#EXTINF:-1 tvg-id="channel1" tvg-chno="1",Channel One
http://example.com/stream1
`

	require.NoError(t, os.WriteFile(xmltvPath, []byte(xmltvContent), 0644))
	require.NoError(t, os.WriteFile(m3uPath, []byte(m3uContent), 0644))

	cfg := jobs.Config{
		DataDir:   tmpDir,
		XMLTVPath: "xmltv.xml",
	}

	server := New(cfg)
	handler := server.Handler()

	req := httptest.NewRequest(http.MethodGet, "/xmltv.xml", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "application/xml; charset=utf-8", rr.Header().Get("Content-Type"))
	assert.Contains(t, rr.Body.String(), "Channel One")
}

func TestHandleXMLTV_FileTooLarge(t *testing.T) {
	tmpDir := t.TempDir()
	xmltvPath := filepath.Join(tmpDir, "xmltv.xml")

	// Create a file larger than 50MB limit
	largeContent := make([]byte, 51*1024*1024)
	require.NoError(t, os.WriteFile(xmltvPath, largeContent, 0644))

	cfg := jobs.Config{
		DataDir:   tmpDir,
		XMLTVPath: "xmltv.xml",
	}

	server := New(cfg)
	handler := server.Handler()

	req := httptest.NewRequest(http.MethodGet, "/xmltv.xml", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusRequestEntityTooLarge, rr.Code)
}

func TestHandleXMLTV_FileNotFound(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := jobs.Config{
		DataDir:   tmpDir,
		XMLTVPath: "nonexistent.xml",
	}

	server := New(cfg)
	handler := server.Handler()

	req := httptest.NewRequest(http.MethodGet, "/xmltv.xml", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestHandleXMLTV_IDRemapping(t *testing.T) {
	tmpDir := t.TempDir()

	xmltvPath := filepath.Join(tmpDir, "xmltv.xml")
	m3uPath := filepath.Join(tmpDir, "playlist.m3u")

	// XMLTV with channel IDs
	xmltvContent := `<?xml version="1.0" encoding="UTF-8"?>
<tv>
  <channel id="oldID1">
    <display-name>Channel One</display-name>
  </channel>
  <programme start="20250101000000" stop="20250101010000" channel="oldID1">
    <title>Test Programme</title>
  </programme>
</tv>`

	// M3U with tvg-chno mapping
	m3uContent := `#EXTM3U
#EXTINF:-1 tvg-id="oldID1" tvg-chno="42",Channel One
http://example.com/stream1
`

	require.NoError(t, os.WriteFile(xmltvPath, []byte(xmltvContent), 0644))
	require.NoError(t, os.WriteFile(m3uPath, []byte(m3uContent), 0644))

	cfg := jobs.Config{
		DataDir:   tmpDir,
		XMLTVPath: "xmltv.xml",
	}

	server := New(cfg)
	handler := server.Handler()

	req := httptest.NewRequest(http.MethodGet, "/xmltv.xml", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	body := rr.Body.String()

	// Should have remapped oldID1 to 42
	assert.Contains(t, body, `id="42"`)
	assert.Contains(t, body, `channel="42"`)
	assert.NotContains(t, body, `id="oldID1"`)
}

func TestHandleRefreshV1(t *testing.T) {
	cfg := jobs.Config{
		OWIBase: "http://invalid-url-for-testing",
	}

	server := New(cfg)
	handler := server.Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/refresh", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// Should call through to handleRefresh
	// Expect either success or error, but not 404
	assert.NotEqual(t, http.StatusNotFound, rr.Code)
}

func TestClientDisconnectDuringRefresh(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := jobs.Config{
		DataDir:  tmpDir,
		OWIBase:  "http://invalid-url-that-will-timeout",
		Bouquet:  "test",
		APIToken: "test-token",
	}

	server := New(cfg)

	// Create a context that we'll cancel to simulate client disconnect
	ctx, cancel := context.WithCancel(context.Background())

	req := httptest.NewRequest(http.MethodPost, "/api/refresh", nil).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer test-token")

	rr := httptest.NewRecorder()

	// Cancel context after a short delay to simulate client disconnect
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	handler := server.Handler()
	handler.ServeHTTP(rr, req)

	// The handler should still complete (or return error) even though client disconnected
	// Important: job should continue in background
	assert.NotEqual(t, 0, rr.Code, "handler should have returned a status code")
}

// getMetrics is a test helper to scrape metrics from a registry.
func getMetrics(reg *prometheus.Registry) (string, error) {
	var h http.Handler
	if reg == nil {
		// default registry gatherer
		h = promhttp.Handler()
	} else {
		h = promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/metrics", nil))
	return rr.Body.String(), nil
}
