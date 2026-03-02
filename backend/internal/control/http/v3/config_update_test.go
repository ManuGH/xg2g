// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/stretchr/testify/require"
)

func TestPutSystemConfigRejectsInvalid(t *testing.T) {
	t.Setenv("XG2G_OWI_BASE", "http://example.com")
	t.Setenv("XG2G_STORE_PATH", t.TempDir())
	cfg, err := config.NewLoader("", "test").Load()
	require.NoError(t, err)
	cfg.DataDir = t.TempDir()

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	srv := NewServer(cfg, config.NewManager(configPath), nil)

	req := httptest.NewRequest(http.MethodPut, "/api/v3/system/config", strings.NewReader(`{"epg":{"days":0}}`))
	w := httptest.NewRecorder()

	srv.PutSystemConfig(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)

	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.NotNil(t, body["details"])

	_, statErr := os.Stat(configPath)
	require.True(t, os.IsNotExist(statErr), "invalid config must not be persisted")
}

func TestPutSystemConfigTriggersShutdown(t *testing.T) {
	t.Setenv("XG2G_OWI_BASE", "http://example.com")
	t.Setenv("XG2G_STORE_PATH", t.TempDir())
	cfg, err := config.NewLoader("", "test").Load()
	require.NoError(t, err)
	cfg.DataDir = t.TempDir()
	cfg.Engine.Enabled = false // Skip ffmpeg/curl checks in test environment

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	srv := NewServer(cfg, config.NewManager(configPath), nil)

	shutdownCh := make(chan struct{})
	srv.requestShutdown = func(ctx context.Context) error {
		close(shutdownCh)
		return nil
	}

	// 'bouquets' is not hot-reloadable, should trigger shutdown
	req := httptest.NewRequest(http.MethodPut, "/api/v3/system/config", strings.NewReader(`{"bouquets":["A","B"]}`))
	w := httptest.NewRecorder()

	srv.PutSystemConfig(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusAccepted, resp.StatusCode)

	select {
	case <-shutdownCh:
	case <-time.After(2 * time.Second):
		t.Fatal("expected shutdown to be requested")
	}
}

func TestPutSystemConfigDoesNotAliasCurrent(t *testing.T) {
	t.Setenv("XG2G_OWI_BASE", "http://example.com")
	t.Setenv("XG2G_STORE_PATH", t.TempDir())
	cfg, err := config.NewLoader("", "test").Load()
	require.NoError(t, err)

	cfg.DataDir = t.TempDir()

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	srv := NewServer(cfg, config.NewManager(configPath), nil)

	// Mock shutdown to avoid panic on restart-required updates
	srv.requestShutdown = func(ctx context.Context) error { return nil }

	// Snapshot before
	before := srv.GetConfig()

	// Update that changes the string
	req := httptest.NewRequest(http.MethodPut, "/api/v3/system/config", strings.NewReader(`{"bouquets":["A","B"]}`))
	w := httptest.NewRecorder()

	srv.PutSystemConfig(w, req)
	require.Equal(t, http.StatusAccepted, w.Code)

	// Check after
	after := srv.GetConfig()
	require.Equal(t, "A,B", after.Bouquet)

	// Assert "before" was NOT mutated (alias safety)
	require.Empty(t, before.Bouquet, "original config must not be mutated by update (aliasing)")
}
