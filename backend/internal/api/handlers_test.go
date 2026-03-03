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
	v3 "github.com/ManuGH/xg2g/internal/control/http/v3"
	"github.com/ManuGH/xg2g/internal/jobs"
	"github.com/ManuGH/xg2g/internal/resilience"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetConfig tests the GetConfig method.
func TestGetConfig(t *testing.T) {
	cfg := config.AppConfig{
		Bouquet: "test-bouquet",
		Enigma2: config.Enigma2Settings{
			Username: "testuser",
			BaseURL:  "http://example.com",
		},
		Streaming: config.StreamingConfig{
			DeliveryPolicy: "universal",
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

func runtimePlaylistPath(dir string) string {
	snap := config.BuildSnapshot(config.AppConfig{DataDir: dir}, config.ReadOSRuntimeEnvOrDefault())
	return filepath.Join(dir, snap.Runtime.PlaylistFilename)
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

	cfg := config.AppConfig{
		Bouquet: "test",
		Enigma2: config.Enigma2Settings{
			BaseURL: "http://example.com",
		},
		Streaming: config.StreamingConfig{
			DeliveryPolicy: "universal",
		},
	}
	s := &Server{
		cfg:       cfg,
		snap:      config.BuildSnapshot(cfg, config.ReadOSRuntimeEnvOrDefault()),
		refreshFn: mockRefreshFn,
		cb:        resilience.NewCircuitBreaker("test", 3, 5, 60*time.Second, 5*time.Second),
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
			Streaming: config.StreamingConfig{
				DeliveryPolicy: "universal",
			},
		}
		s := mustNewServer(t, cfg, config.NewManager(""))
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
	// Create a mock receiver for health checks
	mockReceiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer mockReceiver.Close()

	// Create playlist file
	dataDir := t.TempDir()
	require.NoError(t, os.WriteFile(runtimePlaylistPath(dataDir), []byte("#EXTM3U"), 0600))

	cfg := config.AppConfig{
		APIToken:       "test-token",
		APITokenScopes: []string{string(v3.ScopeV3Read)},
		Version:        "3.0.0",
		DataDir:        dataDir,
		Enigma2: config.Enigma2Settings{
			BaseURL: mockReceiver.URL,
		},
		Streaming: config.StreamingConfig{
			DeliveryPolicy: "universal",
		},
	}
	s := mustNewServer(t, cfg, config.NewManager(""))
	s.SetStatus(jobs.Status{
		Version:       "3.0.0",
		Channels:      2,
		LastRun:       time.Now(),
		EPGProgrammes: 10,
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
