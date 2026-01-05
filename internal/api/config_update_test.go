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
	"strings"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/stretchr/testify/require"
)

func TestPutSystemConfigRejectsInvalid(t *testing.T) {
	cfg, err := config.NewLoader("", "test").Load()
	require.NoError(t, err)
	cfg.DataDir = t.TempDir()

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	srv := New(cfg, config.NewManager(configPath))

	req := httptest.NewRequest(http.MethodPut, "/api/v3/system/config", strings.NewReader(`{"epg":{"days":0}}`))
	w := httptest.NewRecorder()

	srv.PutSystemConfig(w, req)

	resp := w.Result()
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)

	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.NotNil(t, body["details"])

	_, statErr := os.Stat(configPath)
	require.True(t, os.IsNotExist(statErr), "invalid config must not be persisted")
}

func TestPutSystemConfigTriggersShutdown(t *testing.T) {
	cfg, err := config.NewLoader("", "test").Load()
	require.NoError(t, err)
	cfg.DataDir = t.TempDir()

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	srv := New(cfg, config.NewManager(configPath))

	shutdownCh := make(chan struct{})
	srv.SetShutdownFunc(func(ctx context.Context) error {
		close(shutdownCh)
		return nil
	})

	req := httptest.NewRequest(http.MethodPut, "/api/v3/system/config", strings.NewReader(`{"featureFlags":{"instantTune":true}}`))
	w := httptest.NewRecorder()

	srv.PutSystemConfig(w, req)

	resp := w.Result()
	defer resp.Body.Close()
	require.Equal(t, http.StatusAccepted, resp.StatusCode)

	select {
	case <-shutdownCh:
	case <-time.After(2 * time.Second):
		t.Fatal("expected shutdown to be requested")
	}
}
