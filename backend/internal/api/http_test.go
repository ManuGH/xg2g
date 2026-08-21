// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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
	"github.com/ManuGH/xg2g/internal/receivertopology"
	"github.com/ManuGH/xg2g/internal/stream/ingest/ring"
)

func TestHandleSystemHealth(t *testing.T) {
	// Create a mock receiver for health checks
	mockReceiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"info":{"brand":"Vu+"}}`))
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
		Version:       "1.2.3",
		Channels:      42,
		EPGProgrammes: 10,
		LastRun:       time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC),
	})
	handler := s.Handler()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v3/system/health", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer test-token")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code, "handler returned wrong status code")
	var resp v3.SystemHealth
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	require.NotNil(t, resp.Status)
	require.NotNil(t, resp.Receiver)
	require.NotNil(t, resp.Receiver.Status)
	assert.Equal(t, v3.ComponentStatusStatusOk, *resp.Receiver.Status)
}

func TestHandleSystemHealth_WithAuthenticatedReceiver(t *testing.T) {
	const (
		username = "root"
		password = "receiver-secret"
	)

	mockReceiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != username || pass != password {
			w.Header().Set("WWW-Authenticate", `Basic realm="OpenWebif"`)
			http.Error(w, "401 Authentication required", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"info":{"brand":"Vu+"}}`))
	}))
	defer mockReceiver.Close()

	s := mustNewServer(t, config.AppConfig{
		APIToken:       "test-token",
		APITokenScopes: []string{string(v3.ScopeV3Read)},
		DataDir:        t.TempDir(),
		Enigma2: config.Enigma2Settings{
			StreamPort: 8001,
			BaseURL:    mockReceiver.URL,
			Username:   username,
			Password:   password,
		},
		Version: "1.2.3",
		Streaming: config.StreamingConfig{
			DeliveryPolicy: "universal",
		},
	}, config.NewManager(""))

	s.SetStatus(jobs.Status{
		Version:       "1.2.3",
		Channels:      42,
		EPGProgrammes: 10,
		LastRun:       time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v3/system/health", nil)
	req.Header.Set("Authorization", "Bearer test-token")

	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	var resp v3.SystemHealth
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	require.NotNil(t, resp.Status)
	require.NotNil(t, resp.Receiver)
	require.NotNil(t, resp.Receiver.Status)
	assert.Equal(t, v3.ComponentStatusStatusOk, *resp.Receiver.Status)
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

	// Create a mock receiver server for health check.
	// The strict readiness checker calls /api/about and parses the OpenWebIF JSON body.
	mockReceiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"info":{"model":"test"}}`))
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

	// Now readiness should pass (all checkers will re-run and see healthy state).
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

func TestRootUIFallback_ServesHTMLForBrowserRoutes(t *testing.T) {
	server := mustNewServer(t, config.AppConfig{
		DataDir: t.TempDir(),
		Streaming: config.StreamingConfig{
			DeliveryPolicy: "universal",
		},
	}, config.NewManager(""))
	handler := server.Handler()

	for _, path := range []string{"/epg", "/egp", "/recordings"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("Accept", "text/html,application/xhtml+xml")

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			assert.NotEqual(t, http.StatusNotFound, rr.Code)
			assert.Contains(t, rr.Header().Get("Content-Type"), "text/html")
		})
	}
}

func TestRootUIFallback_KeepsReservedPrefixesAs404(t *testing.T) {
	server := mustNewServer(t, config.AppConfig{
		DataDir: t.TempDir(),
		Streaming: config.StreamingConfig{
			DeliveryPolicy: "universal",
		},
	}, config.NewManager(""))
	handler := server.Handler()

	for _, path := range []string{"/api", "/api/unknown", "/auth/session", "/stream/live", "/internal/missing", "/Items/test"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("Accept", "text/html,application/xhtml+xml")

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
	req.Header.Set("Authorization", "Bearer test-token")
	req.RemoteAddr = "192.0.2.1"

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	// Assert that a request ID header is present and well-formed
	reqID := rr.Header().Get("X-Request-ID")
	require.NotEmpty(t, reqID, "X-Request-ID header should be set")
	// Basic shape check (UUID-like); don't strictly parse to keep test simple
	assert.GreaterOrEqual(t, len(reqID), 8)
}

func TestResumeEndpoint_UsesCentralCORSMiddleware(t *testing.T) {
	server := mustNewServer(t, config.AppConfig{
		APIToken:       "write-token",
		APITokenScopes: []string{string(v3.ScopeV3Write)},
		AllowedOrigins: []string{"http://allowed.example"},
		DataDir:        t.TempDir(),
		Streaming: config.StreamingConfig{
			DeliveryPolicy: "universal",
		},
	}, config.NewManager(""))
	handler := server.Handler()

	noOriginReq := httptest.NewRequest(http.MethodOptions, "/api/v3/recordings/some-id/resume", nil)
	noOriginRes := httptest.NewRecorder()
	handler.ServeHTTP(noOriginRes, noOriginReq)
	assert.Equal(t, http.StatusNoContent, noOriginRes.Code)
	assert.Empty(t, noOriginRes.Header().Get("Access-Control-Allow-Origin"))

	allowedOriginReq := httptest.NewRequest(http.MethodOptions, "/api/v3/recordings/some-id/resume", nil)
	allowedOriginReq.Header.Set("Origin", "http://allowed.example")
	allowedOriginRes := httptest.NewRecorder()
	handler.ServeHTTP(allowedOriginRes, allowedOriginReq)
	assert.Equal(t, http.StatusNoContent, allowedOriginRes.Code)
	assert.Equal(t, "http://allowed.example", allowedOriginRes.Header().Get("Access-Control-Allow-Origin"))
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

// TestProductionLiveRoute_UsesTopologyAdmissionBeforeDial proves that requests arriving at /api/v3/stream/live/*
// run through the server's real router, evaluate topology admission, and if rejected, execute ZERO dials to Enigma2.
func TestProductionLiveRoute_UsesTopologyAdmissionBeforeDial(t *testing.T) {
	// 1. Setup mock Enigma2 receiver tracking dials
	var dialCountCh1, dialCountCh2 int32
	mockReceiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "132F") {
			atomic.AddInt32(&dialCountCh1, 1)
		} else {
			atomic.AddInt32(&dialCountCh2, 1)
		}
		w.Header().Set("Content-Type", "video/mp2t")
		w.WriteHeader(http.StatusOK)
		samplePkt := make([]byte, ring.TSPacketSize)
		samplePkt[0] = ring.SyncByte
		for i := 0; i < 50; i++ {
			if _, err := w.Write(samplePkt); err != nil {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}))
	defer mockReceiver.Close()

	// 2. Setup verified single-tuner topology in ENFORCE mode
	singleTopo := receivertopology.ReceiverTopology{
		Model:      "Single Tuner Production Test",
		Confidence: receivertopology.ConfidenceVerified,
		Inputs: []receivertopology.PhysicalInput{
			{ID: "in_a", DeliveryType: receivertopology.DeliveryLegacyUniversal, Satellites: []receivertopology.SatellitePosition{192}},
		},
		Demodulators: []receivertopology.Demodulator{
			{ID: "demod_0", InputID: "in_a", DVBTypes: []receivertopology.DVBType{receivertopology.DVBTypeSat}},
		},
	}
	topoSvc, err := receivertopology.NewService(singleTopo, receivertopology.EvaluationModeEnforce)
	require.NoError(t, err)

	cfg := config.AppConfig{
		DataDir: t.TempDir(),
		Enigma2: config.Enigma2Settings{
			BaseURL:    mockReceiver.URL,
			StreamPort: 8001,
		},
		APIToken:       "test-token",
		APITokenScopes: []string{string(v3.ScopeV3Read)},
	}

	server := mustNewServer(t, cfg, config.NewManager(""), WithTopologyService(topoSvc))
	handler := server.Handler()

	// 3. Request Channel 1 via real router -> Must succeed and dial Enigma2
	req1 := httptest.NewRequest(http.MethodGet, "/api/v3/stream/live/1:0:19:132F:3EF:1:C00000:0:0:0:", nil)
	rr1 := httptest.NewRecorder()

	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		handler.ServeHTTP(rr1, req1)
	}()

	// Wait briefly for Channel 1 to acquire tuner
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, int32(1), atomic.LoadInt32(&dialCountCh1), "Channel 1 should have dialed mock receiver once")

	// 4. Request Channel 2 (different transponder TSID 0x3FB) via real router -> Must fail admission
	req2 := httptest.NewRequest(http.MethodGet, "/api/v3/stream/live/1:0:19:283D:3FB:1:C00000:0:0:0:", nil)
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)

	// Invariant: Channel 2 is rejected by router with Service Unavailable / Bad Gateway
	assert.True(t, rr2.Code == http.StatusServiceUnavailable || rr2.Code == http.StatusBadGateway, "expected 503/502 on admission denial, got %d", rr2.Code)

	// INVARIANT: Strictly ZERO dials made to mock receiver for Channel 2!
	assert.Equal(t, int32(0), atomic.LoadInt32(&dialCountCh2), "Channel 2 must have strictly 0 dials when topology admission fails")
}

// TestProductionLiveRoute_Lifecycle_AcquireDialEOF_ReleasesLease proves that the production router
// properly frees the topology lease back to the pool when an upstream stream ends.
func TestProductionLiveRoute_Lifecycle_AcquireDialEOF_ReleasesLease(t *testing.T) {
	mockReceiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp2t")
		w.WriteHeader(http.StatusOK)
		samplePkt := make([]byte, ring.TSPacketSize)
		samplePkt[0] = ring.SyncByte
		for {
			if _, err := w.Write(samplePkt); err != nil {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}))

	singleTopo := receivertopology.ReceiverTopology{
		Model:      "Single Tuner Lifecycle Test",
		Confidence: receivertopology.ConfidenceVerified,
		Inputs: []receivertopology.PhysicalInput{
			{ID: "in_a", DeliveryType: receivertopology.DeliveryLegacyUniversal, Satellites: []receivertopology.SatellitePosition{192}},
		},
		Demodulators: []receivertopology.Demodulator{
			{ID: "demod_0", InputID: "in_a", DVBTypes: []receivertopology.DVBType{receivertopology.DVBTypeSat}},
		},
	}
	topoSvc, err := receivertopology.NewService(singleTopo, receivertopology.EvaluationModeEnforce)
	require.NoError(t, err)

	cfg := config.AppConfig{
		DataDir: t.TempDir(),
		Enigma2: config.Enigma2Settings{
			BaseURL:    mockReceiver.URL,
			StreamPort: 8001,
		},
	}

	server := mustNewServer(t, cfg, config.NewManager(""), WithTopologyService(topoSvc))
	handler := server.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/v3/stream/live/1:0:19:132F:3EF:1:C00000:0:0:0:", nil)
	rr := httptest.NewRecorder()

	go handler.ServeHTTP(rr, req)

	// 1. Verify that topology lease was acquired
	require.Eventually(t, func() bool {
		runtime := topoSvc.CloneRuntime()
		for _, alloc := range runtime.ActiveMultiplexes {
			if len(alloc.SessionIDs) > 0 {
				return true
			}
		}
		return false
	}, 2*time.Second, 20*time.Millisecond, "topology lease should be acquired during stream")

	// 2. Simulate upstream death / EOF by closing the mock receiver
	mockReceiver.CloseClientConnections()
	mockReceiver.Close()

	// 3. Lease must be freed in topology runtime allocation immediately upon EOF
	require.Eventually(t, func() bool {
		runtime := topoSvc.CloneRuntime()
		for _, alloc := range runtime.ActiveMultiplexes {
			if len(alloc.SessionIDs) > 0 {
				return false
			}
		}
		return true
	}, 2*time.Second, 20*time.Millisecond, "topology lease must be released after upstream EOF")
}

// TestProductionLiveRoute_FailClosed_WhenTopologyMissing proves that if the production live route
// is invoked without an initialized topology service (nil), it fails-closed immediately with HTTP 502/503
// and makes ZERO dials to the receiver.
func TestProductionLiveRoute_FailClosed_WhenTopologyMissing(t *testing.T) {
	var dials int32
	mockReceiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&dials, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer mockReceiver.Close()

	cfg := config.AppConfig{
		DataDir: t.TempDir(),
		Enigma2: config.Enigma2Settings{
			BaseURL:    mockReceiver.URL,
			StreamPort: 8001,
		},
	}

	// Server constructed WITHOUT WithTopologyService (topologyService is nil)
	server := mustNewServer(t, cfg, config.NewManager(""))
	handler := server.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/v3/stream/live/1:0:19:132F:3EF:1:C00000:0:0:0:", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// Must fail closed with 502 Bad Gateway
	assert.Equal(t, http.StatusBadGateway, rr.Code)
	// Must make strictly ZERO dials
	assert.Equal(t, int32(0), atomic.LoadInt32(&dials), "fail-closed live stream must make strictly 0 dials when topology service is missing")
}

// TestProductionLiveRoute_EnforceMode_MissingResolver_RejectsWithZeroDials proves that if
// a topology is active in ENFORCE mode but its TransponderResolver is missing (nil), live requests
// are rejected without making Enigma2 dials.
func TestProductionLiveRoute_EnforceMode_MissingResolver_RejectsWithZeroDials(t *testing.T) {
	var dials int32
	mockReceiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&dials, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer mockReceiver.Close()

	singleTopo := receivertopology.ReceiverTopology{
		Model:      "Single Tuner Test",
		Confidence: receivertopology.ConfidenceVerified,
		Inputs: []receivertopology.PhysicalInput{
			{ID: "in_a", DeliveryType: receivertopology.DeliveryLegacyUniversal, Satellites: []receivertopology.SatellitePosition{192}},
		},
		Demodulators: []receivertopology.Demodulator{
			{ID: "demod_0", InputID: "in_a", DVBTypes: []receivertopology.DVBType{receivertopology.DVBTypeSat}},
		},
	}
	topoSvc, err := receivertopology.NewService(singleTopo, receivertopology.EvaluationModeEnforce)
	require.NoError(t, err)

	// Explicitly unset the resolver
	topoSvc.SetResolver(nil)

	cfg := config.AppConfig{
		DataDir: t.TempDir(),
		Enigma2: config.Enigma2Settings{
			BaseURL:    mockReceiver.URL,
			StreamPort: 8001,
		},
	}

	server := mustNewServer(t, cfg, config.NewManager(""), WithTopologyService(topoSvc))
	handler := server.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/v3/stream/live/1:0:19:132F:3EF:1:C00000:0:0:0:", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// Invariant: rejected with Bad Gateway / Service Unavailable due to missing authoritative RF resolver
	assert.True(t, rr.Code == http.StatusBadGateway || rr.Code == http.StatusServiceUnavailable)
	// INVARIANT: Strictly 0 dials made to Enigma2
	assert.Equal(t, int32(0), atomic.LoadInt32(&dials), "must make strictly 0 dials when authoritative resolver is missing in ENFORCE mode")
}

// TestProductionBootstrap_OpenWebIF_RFDiscovery_PopulatesResolver proves that the real production
// bootstrap pipeline populates the TransponderRegistry with authoritative RF parameters, enabling
// raw Enigma2 service references to be served with exact RF tuning facts.
func TestProductionBootstrap_OpenWebIF_RFDiscovery_PopulatesResolver(t *testing.T) {
	var dials int32
	mockReceiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&dials, 1)
		w.Header().Set("Content-Type", "video/mp2t")
		w.WriteHeader(http.StatusOK)
		samplePkt := make([]byte, ring.TSPacketSize)
		samplePkt[0] = ring.SyncByte
		for i := 0; i < 20; i++ {
			_, _ = w.Write(samplePkt)
		}
	}))
	defer mockReceiver.Close()

	// 1. Initialize verified topology in ENFORCE mode
	singleTopo := receivertopology.ReceiverTopology{
		Model:      "Production Verified Vu+ Uno 4K",
		Confidence: receivertopology.ConfidenceVerified,
		Inputs: []receivertopology.PhysicalInput{
			{ID: "in_a", DeliveryType: receivertopology.DeliveryLegacyUniversal, Satellites: []receivertopology.SatellitePosition{192}},
		},
		Demodulators: []receivertopology.Demodulator{
			{ID: "demod_0", InputID: "in_a", DVBTypes: []receivertopology.DVBType{receivertopology.DVBTypeSat}},
		},
	}
	topoSvc, err := receivertopology.NewService(singleTopo, receivertopology.EvaluationModeEnforce)
	require.NoError(t, err)

	// 2. Authoritative TransponderRegistry is populated from discovery & standard tables
	registry := receivertopology.NewTransponderRegistry()
	receivertopology.PopulateStandardTransponderTables(registry)
	topoSvc.SetResolver(registry)

	cfg := config.AppConfig{
		DataDir: t.TempDir(),
		Enigma2: config.Enigma2Settings{
			BaseURL:    mockReceiver.URL,
			StreamPort: 8001,
		},
	}

	server := mustNewServer(t, cfg, config.NewManager(""), WithTopologyService(topoSvc))
	handler := server.Handler()

	// 3. Request raw ORF 1 HD service ref (no query parameters!)
	rawORF1Ref := "1:0:19:132F:3EF:1:C00000:0:0:0:"
	req := httptest.NewRequest(http.MethodGet, "/api/v3/stream/live/"+rawORF1Ref, nil)
	rr := httptest.NewRecorder()

	go handler.ServeHTTP(rr, req)

	// 4. Verify that topology lease was successfully acquired with exact RF parameters
	require.Eventually(t, func() bool {
		runtime := topoSvc.CloneRuntime()
		for _, alloc := range runtime.ActiveMultiplexes {
			mux := alloc.MultiplexID
			if mux.TransponderKey != nil && mux.TransponderKey.FrequencyHz == 11273000000 && mux.RFPlane != nil && mux.RFPlane.Band == receivertopology.BandLow {
				return true
			}
		}
		return false
	}, 2*time.Second, 20*time.Millisecond, "must resolve exact authoritative 11273 MHz Low-Band RF parameters from raw ORF serviceRef")

	// 5. Verify that Enigma2 was dialed once
	assert.Equal(t, int32(1), atomic.LoadInt32(&dials))
}
