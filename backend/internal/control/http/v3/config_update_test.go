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
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/stretchr/testify/require"
)

func TestPutSystemConfigRejectsInvalid(t *testing.T) {
	t.Setenv("XG2G_E2_HOST", "http://example.com")
	t.Setenv("XG2G_STORE_PATH", t.TempDir())
	t.Setenv("XG2G_RECORDINGS_TARGET_SIGNING_KEY", "12345678901234567890123456789012")
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
	t.Setenv("XG2G_E2_HOST", "http://example.com")
	t.Setenv("XG2G_STORE_PATH", t.TempDir())
	t.Setenv("XG2G_RECORDINGS_TARGET_SIGNING_KEY", "12345678901234567890123456789012")
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
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not found in PATH, skipping preflight-dependent test")
	}
	t.Setenv("XG2G_E2_HOST", "http://example.com")
	t.Setenv("XG2G_STORE_PATH", t.TempDir())
	t.Setenv("XG2G_RECORDINGS_TARGET_SIGNING_KEY", "12345678901234567890123456789012")
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

func TestReceiverUsagePolicyConfig_Roundtrip(t *testing.T) {
	t.Setenv("XG2G_E2_HOST", "http://example.com")
	t.Setenv("XG2G_STORE_PATH", t.TempDir())
	t.Setenv("XG2G_RECORDINGS_TARGET_SIGNING_KEY", "12345678901234567890123456789012")
	cfg, err := config.NewLoader("", "test").Load()
	require.NoError(t, err)
	cfg.DataDir = t.TempDir()

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	srv := NewServer(cfg, config.NewManager(configPath), nil)

	// 1. Initial state: mode is empty/disabled, ReceiverUsagePolicy omitted in GetSystemConfig
	reqGet1 := httptest.NewRequest(http.MethodGet, "/api/v3/system/config", nil)
	wGet1 := httptest.NewRecorder()
	srv.GetSystemConfig(wGet1, reqGet1)
	require.Equal(t, http.StatusOK, wGet1.Code)

	var resp1 AppConfig
	require.NoError(t, json.NewDecoder(wGet1.Body).Decode(&resp1))
	require.NotNil(t, resp1.ReceiverUsagePolicy)
	require.Equal(t, ReceiverUsagePolicyConfigMode("disabled"), *resp1.ReceiverUsagePolicy.Mode)

	// 2. PutSystemConfig update with enforce mode and limits
	policyUpdate := `{"receiverUsagePolicy":{"mode":"enforce","maxLiveSessions":2,"maxRecordingSessions":1,"maxRestrictedAccessSessions":1,"allowLiveWithRecording":true}}`
	reqPut := httptest.NewRequest(http.MethodPut, "/api/v3/system/config", strings.NewReader(policyUpdate))
	wPut := httptest.NewRecorder()
	srv.requestShutdown = func(ctx context.Context) error { return nil }
	srv.PutSystemConfig(wPut, reqPut)
	require.Contains(t, []int{http.StatusOK, http.StatusAccepted}, wPut.Code)

	// 3. GetSystemConfig after update returns the configured ReceiverUsagePolicy
	reqGet2 := httptest.NewRequest(http.MethodGet, "/api/v3/system/config", nil)
	wGet2 := httptest.NewRecorder()
	srv.GetSystemConfig(wGet2, reqGet2)
	require.Equal(t, http.StatusOK, wGet2.Code)

	var resp2 AppConfig
	require.NoError(t, json.NewDecoder(wGet2.Body).Decode(&resp2))
	require.NotNil(t, resp2.ReceiverUsagePolicy)
	require.Equal(t, ReceiverUsagePolicyConfigMode("enforce"), *resp2.ReceiverUsagePolicy.Mode)
	require.Equal(t, 2, *resp2.ReceiverUsagePolicy.MaxLiveSessions)
	require.Equal(t, true, *resp2.ReceiverUsagePolicy.AllowLiveWithRecording)
}
