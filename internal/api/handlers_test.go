// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/hdhr"
	"github.com/ManuGH/xg2g/internal/jobs"
	"github.com/ManuGH/xg2g/internal/resilience"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetConfig tests the GetConfig method.
func TestGetConfig(t *testing.T) {
	cfg := config.AppConfig{
		Bouquet: "test-bouquet",
		Enigma2: config.Enigma2Settings{
			Username: "testuser",
		},
		DataDir: "/tmp/test",
	}

	s := &Server{
		cfg: cfg,
	}

	got := s.GetConfig()
	if got.Bouquet != cfg.Bouquet {
		t.Errorf("expected Bouquet %q, got %q", cfg.Bouquet, got.Bouquet)
	}
	if got.Enigma2.Username != cfg.Enigma2.Username {
		t.Errorf("expected Enigma2.Username %q, got %q", cfg.Enigma2.Username, got.Enigma2.Username)
	}
	if got.DataDir != cfg.DataDir {
		t.Errorf("expected DataDir %q, got %q", cfg.DataDir, got.DataDir)
	}
}

// TestHandleRefreshInternal tests the HandleRefreshInternal wrapper.
func TestHandleRefreshInternal(t *testing.T) {
	// Create a mock refresh function that succeeds immediately
	mockRefreshFn := func(ctx context.Context, snap config.Snapshot) (*jobs.Status, error) {
		_ = snap
		return &jobs.Status{
			Version:  "test",
			Channels: 42,
			LastRun:  time.Now(),
		}, nil
	}

	cfg := config.AppConfig{Bouquet: "test"}
	s := &Server{
		cfg:       cfg,
		snap:      config.BuildSnapshot(cfg, config.ReadOSRuntimeEnvOrDefault()),
		refreshFn: mockRefreshFn,
		cb:        resilience.NewCircuitBreaker("test", 3, 5*time.Second),
	}

	req := httptest.NewRequest(http.MethodPost, "/api/refresh", nil)
	req = req.WithContext(context.Background())
	rr := httptest.NewRecorder()

	s.HandleRefreshInternal(rr, req)

	// Should delegate to handleRefresh and return success
	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
}

// Obsolete tests for V1 API removed

// TestHandleLineupJSON tests the HDHomeRun lineup.json endpoint.
func TestHandleLineupJSON(t *testing.T) {
	t.Run("valid_playlist", func(t *testing.T) {
		// Disable H264 repair for this test (testing basic lineup, not URL rewriting)
		t.Setenv("XG2G_H264_STREAM_REPAIR", "false")

		// Create temp directory with M3U file
		tmpDir := t.TempDir()
		m3uContent := `#EXTM3U
#EXTINF:-1 tvg-chno="1" tvg-id="sref-1" tvg-name="Channel One",Channel One
http://example.com/stream1
#EXTINF:-1 tvg-chno="2" tvg-id="sref-2" tvg-name="Channel Two",Channel Two
http://example.com/stream2
#EXTINF:-1 tvg-chno="3" tvg-id="sref-3" tvg-name="Channel Three",Channel Three
http://example.com/stream3
`
		m3uPath := filepath.Join(tmpDir, "playlist.m3u")
		if err := os.WriteFile(m3uPath, []byte(m3uContent), 0600); err != nil {
			t.Fatal(err)
		}

		cfg := config.AppConfig{DataDir: tmpDir}
		s := &Server{
			cfg:  cfg,
			snap: config.BuildSnapshot(cfg, config.ReadOSRuntimeEnvOrDefault()),
		}

		req := httptest.NewRequest(http.MethodGet, "/lineup.json", nil)
		req = req.WithContext(context.Background())
		rr := httptest.NewRecorder()

		s.handleLineupJSON(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
		}

		var lineup []hdhr.LineupEntry
		if err := json.NewDecoder(rr.Body).Decode(&lineup); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if len(lineup) != 3 {
			t.Errorf("expected 3 channels, got %d", len(lineup))
		}

		// Verify first channel
		if lineup[0].GuideNumber != "1" {
			t.Errorf("expected GuideNumber %q, got %q", "1", lineup[0].GuideNumber)
		}
		if lineup[0].GuideName != "Channel One" {
			t.Errorf("expected GuideName %q, got %q", "Channel One", lineup[0].GuideName)
		}
		if lineup[0].URL != "http://example.com/stream1" {
			t.Errorf("expected URL %q, got %q", "http://example.com/stream1", lineup[0].URL)
		}
	})

	t.Run("missing_playlist", func(t *testing.T) {
		tmpDir := t.TempDir()
		// Don't create playlist.m3u

		cfg := config.AppConfig{DataDir: tmpDir}
		s := &Server{
			cfg:  cfg,
			snap: config.BuildSnapshot(cfg, config.ReadOSRuntimeEnvOrDefault()),
		}

		req := httptest.NewRequest(http.MethodGet, "/lineup.json", nil)
		req = req.WithContext(context.Background())
		rr := httptest.NewRecorder()

		s.handleLineupJSON(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("expected status %d, got %d", http.StatusInternalServerError, rr.Code)
		}
	})
}

// TestRespondError tests the structured error response function.
func TestRespondError(t *testing.T) {
	t.Run("basic_error", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req = req.WithContext(context.Background())
		rr := httptest.NewRecorder()

		RespondError(rr, req, http.StatusBadRequest, ErrInvalidInput)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected status %d, got %d", http.StatusBadRequest, rr.Code)
		}

		var apiErr APIError
		if err := json.NewDecoder(rr.Body).Decode(&apiErr); err != nil {
			t.Fatalf("failed to decode error response: %v", err)
		}

		if apiErr.Code != "INVALID_INPUT" {
			t.Errorf("expected code %q, got %q", "INVALID_INPUT", apiErr.Code)
		}
		if apiErr.Message != "Invalid input parameters" {
			t.Errorf("expected message %q, got %q", "Invalid input parameters", apiErr.Message)
		}
	})

	t.Run("error_with_details", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req = req.WithContext(context.Background())
		rr := httptest.NewRecorder()

		details := map[string]string{"field": "bouquet", "reason": "invalid format"}
		RespondError(rr, req, http.StatusBadRequest, ErrInvalidInput, details)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected status %d, got %d", http.StatusBadRequest, rr.Code)
		}

		var apiErr APIError
		if err := json.NewDecoder(rr.Body).Decode(&apiErr); err != nil {
			t.Fatalf("failed to decode error response: %v", err)
		}

		if apiErr.Details == nil {
			t.Error("expected Details to be set")
		}
	})
}

// TestAPIError_Error tests the Error method of APIError.
func TestAPIError_Error(t *testing.T) {
	err := &APIError{
		Code:    "TEST_ERROR",
		Message: "This is a test error",
	}

	if err.Error() != "This is a test error" {
		t.Errorf("expected Error() %q, got %q", "This is a test error", err.Error())
	}
}

// TestHDHomeRunServer tests the HDHomeRunServer getter.
func TestHDHomeRunServer(t *testing.T) {
	s := &Server{
		hdhr: nil,
	}

	if got := s.HDHomeRunServer(); got != nil {
		t.Errorf("expected nil HDHomeRun server, got %v", got)
	}
}

// TestHandler tests the Handler method with and without audit logger.
func TestHandler(t *testing.T) {
	t.Run("without_audit_logger", func(t *testing.T) {
		cfg := config.AppConfig{
			DataDir: t.TempDir(),
			Bouquet: "test",
		}
		s := New(cfg, nil)
		handler := s.Handler()

		if handler == nil {
			t.Fatal("expected handler, got nil")
		}

		// Test basic endpoint
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
		}
	})

	// with_audit_logger case removed
}

// Obsolete tests removed (TestSetConfigHolder, TestSetAuditLogger, TestHandleStatusPlaceholder, TestNewRouter)

// API Routes Tests

func TestRegisterRoutes_StatusEndpoint(t *testing.T) {
	cfg := config.AppConfig{
		APIToken:       "test-token",
		APITokenScopes: []string{string(ScopeV3Read)},
		Version:        "3.0.0",
		DataDir:        t.TempDir(),
	}
	s := New(cfg, nil)
	s.SetStatus(jobs.Status{
		Version:  "3.0.0",
		Channels: 2,
	})

	handler := s.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/v3/system/health", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))

	var resp map[string]interface{}
	err := json.NewDecoder(rr.Body).Decode(&resp)
	require.NoError(t, err)

	assert.Equal(t, "ok", resp["status"])
	assert.Equal(t, "3.0.0", resp["version"])
}

// TestHandleLineupJSON_PlexForceHLS_Disabled tests lineup.json returns direct MPEG-TS URLs when PlexForceHLS=false
func TestHandleLineupJSON_PlexForceHLS_Disabled(t *testing.T) {
	// Create temp directory for test data
	tempDir := t.TempDir()

	// Create test M3U playlist
	m3uContent := `#EXTM3U
#EXTINF:-1 tvg-chno="1" tvg-id="sref-test" tvg-name="Test Channel",Test Channel
http://10.10.55.14:18000/1:0:19:132F:3EF:1:C00000:0:0:0:
#EXTINF:-1 tvg-chno="2" tvg-id="sref-test2" tvg-name="Test Channel 2",Test Channel 2
http://10.10.55.14:18000/1:0:19:1334:3EF:1:C00000:0:0:0:
`
	m3uPath := filepath.Join(tempDir, "playlist.m3u")
	require.NoError(t, os.WriteFile(m3uPath, []byte(m3uContent), 0600))

	// Create server with PlexForceHLS disabled
	cfg := config.AppConfig{
		DataDir: tempDir,
	}
	srv := New(cfg, nil)

	// Initialize HDHomeRun with PlexForceHLS=false
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()
	hdhrCfg := hdhr.Config{
		Enabled:      true,
		PlexForceHLS: false,
		Logger:       logger,
	}
	srv.hdhr = hdhr.NewServer(hdhrCfg, nil)

	// Make request with Host header for URL rewriting
	req := httptest.NewRequest(http.MethodGet, "/lineup.json", nil)
	req.Host = "10.10.55.14:8080"
	w := httptest.NewRecorder()

	srv.handleLineupJSON(w, req)

	// Verify response
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var lineup []hdhr.LineupEntry
	err := json.NewDecoder(w.Body).Decode(&lineup)
	require.NoError(t, err)
	require.Len(t, lineup, 2)

	// Verify URLs do NOT have /hls/ prefix
	assert.Equal(t, "http://10.10.55.14:18000/1:0:19:132F:3EF:1:C00000:0:0:0:", lineup[0].URL)
	assert.Equal(t, "http://10.10.55.14:18000/1:0:19:1334:3EF:1:C00000:0:0:0:", lineup[1].URL)
	assert.NotContains(t, lineup[0].URL, "/hls/")
	assert.NotContains(t, lineup[1].URL, "/hls/")
}

// TestHandleLineupJSON_PlexForceHLS_Enabled tests lineup.json returns HLS URLs when PlexForceHLS=true
func TestHandleLineupJSON_PlexForceHLS_Enabled(t *testing.T) {
	// Create temp directory for test data
	tempDir := t.TempDir()

	// Create test M3U playlist
	m3uContent := `#EXTM3U
#EXTINF:-1 tvg-chno="1" tvg-id="sref-test" tvg-name="Test Channel",Test Channel
http://10.10.55.14:18000/1:0:19:132F:3EF:1:C00000:0:0:0:
#EXTINF:-1 tvg-chno="2" tvg-id="sref-test2" tvg-name="Test Channel 2",Test Channel 2
http://10.10.55.14:18000/1:0:19:1334:3EF:1:C00000:0:0:0:
`
	m3uPath := filepath.Join(tempDir, "playlist.m3u")
	require.NoError(t, os.WriteFile(m3uPath, []byte(m3uContent), 0600))

	// Create server with PlexForceHLS enabled
	cfg := config.AppConfig{
		DataDir: tempDir,
	}
	srv := New(cfg, nil)

	// Initialize HDHomeRun with PlexForceHLS=true
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()
	hdhrCfg := hdhr.Config{
		Enabled:      true,
		PlexForceHLS: true,
		Logger:       logger,
	}
	srv.hdhr = hdhr.NewServer(hdhrCfg, nil)

	// Make request with Host header for URL rewriting
	req := httptest.NewRequest(http.MethodGet, "/lineup.json", nil)
	req.Host = "10.10.55.14:8080"
	w := httptest.NewRecorder()

	srv.handleLineupJSON(w, req)

	// Verify response
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var lineup []hdhr.LineupEntry
	err := json.NewDecoder(w.Body).Decode(&lineup)
	require.NoError(t, err)
	require.Len(t, lineup, 2)

	// Verify URLs have /hls/ prefix added (keeping original port 18000)
	// The /hls/ handler exists on the stream proxy (port 18000), not the API server (port 8080)
	assert.Equal(t, "http://10.10.55.14:18000/hls/1:0:19:132F:3EF:1:C00000:0:0:0:", lineup[0].URL)
	assert.Equal(t, "http://10.10.55.14:18000/hls/1:0:19:1334:3EF:1:C00000:0:0:0:", lineup[1].URL)
	assert.Contains(t, lineup[0].URL, "/hls/")
	assert.Contains(t, lineup[1].URL, "/hls/")
}

// TestHandleLineupJSON_PlexForceHLS_NoDoublePrefix tests that /hls/ is not added twice
func TestHandleLineupJSON_PlexForceHLS_NoDoublePrefix(t *testing.T) {
	// Create temp directory for test data
	tempDir := t.TempDir()

	// Create test M3U playlist with URL that already has /hls/ prefix
	m3uContent := `#EXTM3U
#EXTINF:-1 tvg-chno="1" tvg-id="sref-test" tvg-name="Test Channel",Test Channel
http://10.10.55.14:18000/hls/1:0:19:132F:3EF:1:C00000:0:0:0:
`
	m3uPath := filepath.Join(tempDir, "playlist.m3u")
	require.NoError(t, os.WriteFile(m3uPath, []byte(m3uContent), 0600))

	// Create server with PlexForceHLS enabled
	cfg := config.AppConfig{
		DataDir: tempDir,
	}
	srv := New(cfg, nil)

	// Initialize HDHomeRun with PlexForceHLS=true
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()
	hdhrCfg := hdhr.Config{
		Enabled:      true,
		PlexForceHLS: true,
		Logger:       logger,
	}
	srv.hdhr = hdhr.NewServer(hdhrCfg, nil)

	// Make request with Host header for URL rewriting
	req := httptest.NewRequest(http.MethodGet, "/lineup.json", nil)
	req.Host = "10.10.55.14:8080"
	w := httptest.NewRecorder()

	srv.handleLineupJSON(w, req)

	// Verify response
	assert.Equal(t, http.StatusOK, w.Code)

	var lineup []hdhr.LineupEntry
	err := json.NewDecoder(w.Body).Decode(&lineup)
	require.NoError(t, err)
	require.Len(t, lineup, 1)

	// Verify URL still has only ONE /hls/ prefix (not /hls/hls/)
	assert.Equal(t, "http://10.10.55.14:18000/hls/1:0:19:132F:3EF:1:C00000:0:0:0:", lineup[0].URL)
	assert.NotContains(t, lineup[0].URL, "/hls/hls/")
}
