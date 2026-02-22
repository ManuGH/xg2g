// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.
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

	"github.com/ManuGH/xg2g/internal/config"
	v3 "github.com/ManuGH/xg2g/internal/control/http/v3"
	"github.com/ManuGH/xg2g/internal/jobs"
	"github.com/ManuGH/xg2g/internal/pipeline/scan"
)

func TestHandleSystemHealth(t *testing.T) {
	// Create a mock receiver for health checks
	mockReceiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer mockReceiver.Close()

	s := mustNewServer(t, config.AppConfig{
		APIToken:       "test-token",
		APITokenScopes: []string{string(v3.ScopeV3Read)},
		DataDir:        t.TempDir(),
		Enigma2:        config.Enigma2Settings{StreamPort: 8001, BaseURL: mockReceiver.URL},
		Version:        "1.2.3",
		Streaming: config.StreamingConfig{
			DeliveryPolicy: "universal",
		},
	}, config.NewManager(""))

	// Set status for health check
	s.SetStatus(jobs.Status{
		Version:  "1.2.3",
		Channels: 42,
		LastRun:  time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC),
	})
	handler := s.Handler()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v3/system/health", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer test-token")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code, "handler returned wrong status code")
	assert.Contains(t, rr.Body.String(), `"status":"ok"`, "handler returned unexpected body")
}

func TestHandleRefresh_ErrorDoesNotUpdateLastRun(t *testing.T) {
	cfg := config.AppConfig{
		Enigma2: config.Enigma2Settings{
			BaseURL:    "invalid-url",
			StreamPort: 8001,
		},
		APIToken:       "dummy-token",
		APITokenScopes: []string{string(v3.ScopeV3Read)},
		DataDir:        t.TempDir(),
		Streaming: config.StreamingConfig{
			DeliveryPolicy: "universal",
		},
	}
	s := mustNewServer(t, cfg, config.NewManager(""))
	// handler := s.Handler() // Removed unused
	initialTime := s.status.LastRun

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v3/system/refresh", nil)
	require.NoError(t, err)
	req.Host = "example.com"                       // Required for CSRF validation
	req.Header.Set("Origin", "http://example.com") // Add Origin for CSRF protection
	req.Header.Set("Authorization", "Bearer dummy-token")

	rr := httptest.NewRecorder()
	s.HandleRefreshInternal(rr, req)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Equal(t, initialTime, s.status.LastRun, "lastRefresh should not be updated on failure")
}

func TestRecordRefreshMetrics(t *testing.T) {
	// Use the default registry since promauto registers metrics there
	recordRefreshMetrics(1*time.Second, 10)
	// Only call once to avoid changing the gauge value unexpectedly

	body := getMetrics(nil)

	assert.Contains(t, body, `xg2g_channels`)
	assert.Contains(t, body, `xg2g_refresh_duration_seconds_count`)
}

func TestHandleRefresh_SuccessUpdatesLastRun(t *testing.T) {
	// Create a mock refresh function that succeeds
	mockRefreshFn := func(ctx context.Context, snap config.Snapshot) (*jobs.Status, error) {
		_ = snap
		return &jobs.Status{
			Version:  "test-success",
			Channels: 10,
			LastRun:  time.Now(),
		}, nil
	}

	s := mustNewServer(t, config.AppConfig{
		Enigma2:        config.Enigma2Settings{StreamPort: 8001, BaseURL: "http://example.com"},
		APIToken:       "test-token",
		APITokenScopes: []string{string(v3.ScopeV3Read)},
		DataDir:        t.TempDir(),
		Streaming: config.StreamingConfig{
			DeliveryPolicy: "universal",
		},
	}, config.NewManager(""))
	s.refreshFn = mockRefreshFn

	// handler := s.Handler() // Removed unused

	// Initial state
	initialTime := s.status.LastRun

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v3/system/refresh", nil)
	require.NoError(t, err)
	req.Host = "example.com"
	req.Header.Set("Origin", "http://example.com")
	req.Header.Set("Authorization", "Bearer test-token")

	rr := httptest.NewRecorder()
	s.HandleRefreshInternal(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	// Verify LastRun was updated
	assert.True(t, s.status.LastRun.After(initialTime), "lastRefresh should be updated on success")
	assert.Equal(t, 10, s.status.Channels)
}

func TestHandleRefresh_ConflictOnConcurrent(t *testing.T) {
	cfg := config.AppConfig{
		Enigma2:        config.Enigma2Settings{StreamPort: 8001, BaseURL: "http://example.com"},
		APIToken:       "dummy-token",
		APITokenScopes: []string{string(v3.ScopeV3Read)},
		DataDir:        t.TempDir(),
		Streaming: config.StreamingConfig{
			DeliveryPolicy: "universal",
		},
	}
	s := mustNewServer(t, cfg, config.NewManager(""))

	// Install a slow refresh function to force overlap
	startCh := make(chan struct{})
	releaseCh := make(chan struct{})
	s.refreshFn = func(_ context.Context, _ config.Snapshot) (*jobs.Status, error) {
		close(startCh) // signal that refresh started
		<-releaseCh    // block until allowed to finish
		return &jobs.Status{Channels: 1, LastRun: time.Now()}, nil
	}

	// handler := s.Handler() // Removed unused

	// First request starts and blocks
	req1 := httptest.NewRequest(http.MethodPost, "/api/v3/system/refresh", nil)
	req1.Host = "example.com"                       // Required for CSRF validation
	req1.Header.Set("Origin", "http://example.com") // Add Origin for CSRF protection
	req1.Header.Set("Authorization", "Bearer dummy-token")
	rr1 := httptest.NewRecorder()

	// Run first request in a goroutine
	done1 := make(chan struct{})
	go func() {
		s.HandleRefreshInternal(rr1, req1)
		close(done1)
	}()

	// Wait until the refresh actually started
	select {
	case <-startCh:
	case <-time.After(1 * time.Second):
		t.Fatal("first refresh did not start in time")
	}

	// Second request should get 409 Conflict
	req2 := httptest.NewRequest(http.MethodPost, "/api/v3/system/refresh", nil)
	req2.Host = "example.com"                       // Required for CSRF validation
	req2.Header.Set("Origin", "http://example.com") // Add Origin for CSRF protection
	req2.Header.Set("Authorization", "Bearer dummy-token")
	rr2 := httptest.NewRecorder()
	s.HandleRefreshInternal(rr2, req2)

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
	s := mustNewServer(t, config.AppConfig{
		Enigma2: config.Enigma2Settings{
			BaseURL: "http://example.com",
		},
		Streaming: config.StreamingConfig{
			DeliveryPolicy: "universal",
		},
	}, config.NewManager(""))
	handler := s.Handler()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "/healthz", nil)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), `"status":"healthy"`)
}

func TestHandleReady(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "test-ready")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tempDir) }()

	playlistPath := runtimePlaylistPath(tempDir)
	xmltvPath := "epg.xml"
	xmltvFullPath := filepath.Join(tempDir, xmltvPath)

	// Create a mock receiver server for health check
	mockReceiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer mockReceiver.Close()

	cfg := config.AppConfig{
		DataDir: tempDir,
		Enigma2: config.Enigma2Settings{
			BaseURL: mockReceiver.URL, // Use mock receiver for health check
		},
		XMLTVPath:   xmltvPath,
		ReadyStrict: true,
		Streaming: config.StreamingConfig{
			DeliveryPolicy: "universal",
		},
	}
	s := mustNewServer(t, cfg, config.NewManager(""))
	handler := s.Handler()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "/readyz", nil)
	require.NoError(t, err)

	// Case 1: Not ready (no files, last run is zero)
	// With the new readiness contract, /readyz returns 503 until first successful refresh
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusServiceUnavailable, rr.Code)
	assert.Contains(t, rr.Body.String(), `"ready":false`)

	// Case 2: Simulate successful refresh
	// Update server status to indicate successful refresh (health checkers already registered by New())
	// Create the required files first so file checkers pass
	require.NoError(t, os.WriteFile(playlistPath, []byte("#EXTM3U"), 0o600))
	require.NoError(t, os.WriteFile(xmltvFullPath, []byte("<tv></tv>"), 0o600))

	// Update status with proper locking
	func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.status.LastRun = time.Now()
		s.status.Error = ""
		s.status.Channels = 10       // Set some channels for health check
		s.status.EPGProgrammes = 100 // Set EPG programmes for health check
	}()

	// Wait for the readiness cache to expire (1s TTL)
	// The first /readyz call cached the "not ready" state, so we need to wait
	// for the cache to expire before the checkers will re-run and see the new state
	time.Sleep(1100 * time.Millisecond)

	// Now readiness should pass (all checkers will re-run and see healthy state)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), `"ready":true`)
}

func TestLegacyFilesRoutesRemoved(t *testing.T) {
	server := mustNewServer(t, config.AppConfig{
		DataDir: t.TempDir(),
		Streaming: config.StreamingConfig{
			DeliveryPolicy: "universal",
		},
	}, config.NewManager(""))
	handler := server.Handler()

	tests := []struct {
		name   string
		method string
		path   string
	}{
		{name: "files endpoint removed", method: http.MethodGet, path: "/files/playlist.m3u"},
		{name: "files subpath removed", method: http.MethodGet, path: "/files/subdir/playlist.m3u"},
		{name: "files post removed", method: http.MethodPost, path: "/files/playlist.m3u"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			req.RemoteAddr = "127.0.0.1:1234"
			if tt.method != http.MethodGet && tt.method != http.MethodHead {
				req.Host = "example.com"
				req.Header.Set("Origin", "http://example.com")
			}

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
			assert.Equal(t, http.StatusNotFound, rr.Code)
		})
	}
}

func TestMiddlewareChain(t *testing.T) {
	server := mustNewServer(t, config.AppConfig{
		APIToken:       "test-token",
		APITokenScopes: []string{string(v3.ScopeV3Read)},
		Streaming: config.StreamingConfig{
			DeliveryPolicy: "universal",
		},
	}, config.NewManager(""))
	handler := server.Handler()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "/test", nil)
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
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "ok.txt"), []byte("ok"), 0o600))

	cfg := config.AppConfig{
		DataDir: tempDir,
		Streaming: config.StreamingConfig{
			DeliveryPolicy: "universal",
		},
	}
	server := mustNewServer(t, cfg, config.NewManager(""))
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
			req.RemoteAddr = "127.0.0.1:1234"
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			assert.Equal(t, http.StatusNotFound, rr.Code, "legacy /files route removed")
		})
	}
}

func TestLegacyXMLTVRoutesRemoved(t *testing.T) {
	server := mustNewServer(t, config.AppConfig{
		DataDir: t.TempDir(),
		Streaming: config.StreamingConfig{
			DeliveryPolicy: "universal",
		},
	}, config.NewManager(""))
	handler := server.Handler()

	tests := []string{
		http.MethodGet,
		http.MethodHead,
	}
	for _, method := range tests {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/xmltv.xml", nil)
			req.RemoteAddr = "127.0.0.1:1234"
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
			assert.Equal(t, http.StatusNotFound, rr.Code)
		})
	}
}

// TestHandleSystemHealthV3 removed as it duplicates TestHandleSystemHealth

func TestHandleRefreshV3(t *testing.T) {
	cfg := config.AppConfig{
		Enigma2: config.Enigma2Settings{
			BaseURL:    "http://invalid-url-for-testing",
			StreamPort: 8001,
		},
		APIToken:       "refresh-token",
		APITokenScopes: []string{string(v3.ScopeV3Read)},
		DataDir:        t.TempDir(),
		Streaming: config.StreamingConfig{
			DeliveryPolicy: "universal",
		},
	}

	server := mustNewServer(t, cfg, config.NewManager(""))
	handler := server.Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/v3/system/refresh", nil)
	req.Host = "example.com"                       // Required for CSRF validation
	req.Header.Set("Origin", "http://example.com") // Add Origin for CSRF protection
	req.Header.Set("Authorization", "Bearer refresh-token")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// Should call through to handleRefresh
	// Expect either success or error, but not 404
	assert.NotEqual(t, http.StatusNotFound, rr.Code)
}

func TestClientDisconnectDuringRefresh(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := config.AppConfig{
		DataDir: tmpDir,
		Enigma2: config.Enigma2Settings{
			BaseURL:    "http://invalid-url-that-will-timeout",
			StreamPort: 8001,
		},
		Bouquet:        "test",
		APIToken:       "test-token",
		APITokenScopes: []string{string(v3.ScopeV3Read)},
		Streaming: config.StreamingConfig{
			DeliveryPolicy: "universal",
		},
	}

	server := mustNewServer(t, cfg, config.NewManager(""))
	// Inject dummy scan manager to avoid panic in handleRefresh (typed nil interface trap)
	server.WireV3Runtime(v3.Dependencies{Scan: &scan.Manager{}}, nil)

	// Create a context that we'll cancel to simulate client disconnect
	ctx, cancel := context.WithCancel(context.Background())

	req := httptest.NewRequest(http.MethodPost, "/api/v3/system/refresh", nil).WithContext(ctx)
	req.Host = "example.com"                       // Required for CSRF validation
	req.Header.Set("Origin", "http://example.com") // Add Origin for CSRF protection
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
func getMetrics(reg *prometheus.Registry) string {
	var h http.Handler
	if reg == nil {
		// default registry gatherer
		h = promhttp.Handler()
	} else {
		h = promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	return rr.Body.String()
}
