// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

//go:build integration || integration_fast
// +build integration integration_fast

package contract

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ManuGH/xg2g/internal/channels"
	"github.com/ManuGH/xg2g/internal/hdhr"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHDHomeRunDiscoveryContract verifies the HDHomeRun discovery endpoint contract
func TestHDHomeRunDiscoveryContract(t *testing.T) {
	logger := log.WithComponent("test")

	cfg := hdhr.Config{
		Enabled:      true,
		DeviceID:     "TESTHDR1",
		FriendlyName: "Test HDHomeRun",
		ModelName:    "HDHR-TEST",
		FirmwareName: "test-1.0.0",
		BaseURL:      "http://test.local:8080",
		TunerCount:   4,
		Logger:       logger,
	}

	server := hdhr.NewServer(cfg, channels.NewManager(t.TempDir()))

	t.Run("DiscoveryEndpointContract", func(t *testing.T) {
		// Contract: /discover.json returns JSON with required fields
		req := httptest.NewRequest(http.MethodGet, "/discover.json", nil)
		rec := httptest.NewRecorder()

		server.HandleDiscover(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code, "Discovery endpoint must return 200")
		assert.Contains(t, rec.Header().Get("Content-Type"), "application/json",
			"Discovery response must be JSON")

		var response map[string]interface{}
		err := json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err, "Discovery response must be valid JSON")

		// Contract: Required HDHomeRun discovery fields
		requiredFields := []string{
			"FriendlyName",
			"ModelNumber",
			"FirmwareName",
			"FirmwareVersion",
			"DeviceID",
			"DeviceAuth",
			"BaseURL",
			"LineupURL",
			"TunerCount",
		}

		for _, field := range requiredFields {
			assert.Contains(t, response, field,
				"Discovery response must contain %s", field)
		}

		// Contract: DeviceID must match config
		assert.Equal(t, cfg.DeviceID, response["DeviceID"],
			"DeviceID must match configuration")

		// Contract: TunerCount must be numeric
		assert.IsType(t, float64(0), response["TunerCount"],
			"TunerCount must be numeric")

		// Contract: LineupURL must be properly formatted
		lineupURL, ok := response["LineupURL"].(string)
		require.True(t, ok, "LineupURL must be string")
		assert.Contains(t, lineupURL, "/lineup.json",
			"LineupURL must point to /lineup.json")
	})

	t.Run("DiscoveryDefaultValues", func(t *testing.T) {
		// Contract: Server provides defaults for missing config values
		minimalCfg := hdhr.Config{
			Enabled: true,
			Logger:  logger,
		}

		minimalServer := hdhr.NewServer(minimalCfg, channels.NewManager(t.TempDir()))

		req := httptest.NewRequest(http.MethodGet, "/discover.json", nil)
		rec := httptest.NewRecorder()

		minimalServer.HandleDiscover(rec, req)

		var response map[string]interface{}
		err := json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)

		// Contract: Default values are provided
		assert.NotEmpty(t, response["DeviceID"], "DeviceID must have default")
		assert.NotEmpty(t, response["FriendlyName"], "FriendlyName must have default")
		assert.NotEmpty(t, response["ModelNumber"], "ModelNumber must have default")
		assert.NotZero(t, response["TunerCount"], "TunerCount must have default")
	})
}

// TestHDHomeRunLineupStatusContract verifies the lineup status endpoint contract
func TestHDHomeRunLineupStatusContract(t *testing.T) {
	logger := log.WithComponent("test")

	cfg := hdhr.Config{
		Enabled:      true,
		DeviceID:     "TESTHDR1",
		FriendlyName: "Test HDHomeRun",
		Logger:       logger,
	}

	server := hdhr.NewServer(cfg, channels.NewManager(t.TempDir()))

	t.Run("LineupStatusContract", func(t *testing.T) {
		// Contract: /lineup_status.json returns status information
		req := httptest.NewRequest(http.MethodGet, "/lineup_status.json", nil)
		rec := httptest.NewRecorder()

		server.HandleLineupStatus(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code,
			"Lineup status endpoint must return 200")
		assert.Contains(t, rec.Header().Get("Content-Type"), "application/json",
			"Lineup status response must be JSON")

		var response map[string]interface{}
		err := json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err, "Lineup status response must be valid JSON")

		// Contract: Required lineup status fields
		requiredFields := []string{
			"ScanInProgress",
			"ScanPossible",
			"Source",
			"SourceList",
		}

		for _, field := range requiredFields {
			assert.Contains(t, response, field,
				"Lineup status must contain %s", field)
		}

		// Contract: ScanInProgress and ScanPossible are numeric
		assert.IsType(t, float64(0), response["ScanInProgress"],
			"ScanInProgress must be numeric")
		assert.IsType(t, float64(0), response["ScanPossible"],
			"ScanPossible must be numeric")

		// Contract: SourceList is array
		_, ok := response["SourceList"].([]interface{})
		assert.True(t, ok, "SourceList must be array")
	})
}

// TestHDHomeRunLineupContract verifies the lineup endpoint contract
func TestHDHomeRunLineupContract(t *testing.T) {
	logger := log.WithComponent("test")
	tmpDir := t.TempDir()

	// Create minimal playlist.m3u file for lineup endpoint
	playlistContent := `#EXTM3U
#EXTINF:-1 tvg-chno="1" tvg-id="test1" tvg-name="Test Channel 1",Test Channel 1
http://example.com/stream/1
`
	err := os.WriteFile(filepath.Join(tmpDir, "playlist.m3u"), []byte(playlistContent), 0600)
	require.NoError(t, err, "Failed to create test playlist")

	cfg := hdhr.Config{
		Enabled:      true,
		DeviceID:     "TESTHDR1",
		FriendlyName: "Test HDHomeRun",
		Logger:       logger,
		DataDir:      tmpDir,
	}

	server := hdhr.NewServer(cfg, channels.NewManager(tmpDir))

	t.Run("LineupEndpointContract", func(t *testing.T) {
		// Contract: /lineup.json returns channel array
		req := httptest.NewRequest(http.MethodGet, "/lineup.json", nil)
		rec := httptest.NewRecorder()

		server.HandleLineup(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code,
			"Lineup endpoint must return 200")
		assert.Contains(t, rec.Header().Get("Content-Type"), "application/json",
			"Lineup response must be JSON")

		var response []interface{}
		err := json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err, "Lineup response must be valid JSON array")

		// Contract: Response is array (may be empty before channels are loaded)
		assert.IsType(t, []interface{}{}, response,
			"Lineup must return array")
	})

	t.Run("LineupPostContract", func(t *testing.T) {
		// Contract: POST /lineup.json is supported for scanning
		req := httptest.NewRequest(http.MethodPost, "/lineup.json?scan=start", nil)
		rec := httptest.NewRecorder()

		server.HandleLineup(rec, req)

		// POST is supported (may return empty array or specific response)
		assert.NotEqual(t, http.StatusMethodNotAllowed, rec.Code,
			"POST to lineup must be supported")
	})
}

// TestHDHomeRunConfigContract verifies the configuration contract
func TestHDHomeRunConfigContract(t *testing.T) {
	t.Run("ConfigFromEnv", func(t *testing.T) {
		// Contract: GetConfigFromEnv reads from environment
		logger := log.WithComponent("test")

		// Set environment variables
		os.Setenv("XG2G_HDHR_ENABLED", "true")
		os.Setenv("XG2G_HDHR_DEVICE_ID", "ENVTEST1")
		os.Setenv("XG2G_HDHR_FRIENDLY_NAME", "Env Test Device")
		defer func() {
			os.Unsetenv("XG2G_HDHR_ENABLED")
			os.Unsetenv("XG2G_HDHR_DEVICE_ID")
			os.Unsetenv("XG2G_HDHR_FRIENDLY_NAME")
		}()

		cfg := hdhr.GetConfigFromEnv(logger, t.TempDir())

		// Contract: Config is populated from environment
		assert.True(t, cfg.Enabled, "Enabled must be read from XG2G_HDHR_ENABLED")
		assert.Equal(t, "ENVTEST1", cfg.DeviceID,
			"DeviceID must be read from XG2G_HDHR_DEVICE_ID")
		assert.Equal(t, "Env Test Device", cfg.FriendlyName,
			"FriendlyName must be read from XG2G_HDHR_FRIENDLY_NAME")
	})

	t.Run("ConfigDefaults", func(t *testing.T) {
		// Contract: Config provides sensible defaults
		logger := log.WithComponent("test")

		// Clear any env vars
		os.Unsetenv("XG2G_HDHR_ENABLED")
		os.Unsetenv("XG2G_HDHR_DEVICE_ID")

		cfg := hdhr.GetConfigFromEnv(logger, t.TempDir())

		// Contract: Defaults are provided when env vars are not set
		assert.False(t, cfg.Enabled, "Enabled defaults to false")
		// DeviceID will be empty from env, but NewServer generates one
	})

	t.Run("ServerGeneratesDefaults", func(t *testing.T) {
		// Contract: NewServer generates defaults for missing fields
		logger := log.WithComponent("test")

		emptyCfg := hdhr.Config{
			Enabled: true,
			Logger:  logger,
		}

		server := hdhr.NewServer(emptyCfg, channels.NewManager(t.TempDir()))

		// Verify server has defaults by checking discovery response
		req := httptest.NewRequest(http.MethodGet, "/discover.json", nil)
		rec := httptest.NewRecorder()
		server.HandleDiscover(rec, req)

		var response map[string]interface{}
		err := json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)

		// Contract: Server generates defaults
		assert.NotEmpty(t, response["DeviceID"], "Server must generate DeviceID")
		assert.NotEmpty(t, response["FriendlyName"], "Server must generate FriendlyName")
		assert.NotEmpty(t, response["ModelNumber"], "Server must generate ModelNumber")
	})
}

// TestHDHomeRunSSDPContract verifies SSDP announcer contract
func TestHDHomeRunSSDPContract(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping SSDP test in short mode")
	}

	logger := log.WithComponent("test")

	cfg := hdhr.Config{
		Enabled:      true,
		DeviceID:     "TESTHDR1",
		FriendlyName: "Test HDHomeRun",
		Logger:       logger,
	}

	server := hdhr.NewServer(cfg, channels.NewManager(t.TempDir()))

	t.Run("SSDPAnnouncerContract", func(t *testing.T) {
		// Contract: StartSSDPAnnouncer can be called without panic
		// Note: Actual SSDP testing requires network access
		// This test verifies the API contract only

		// Create a context that cancels immediately
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		// Contract: StartSSDPAnnouncer respects context cancellation
		err := server.StartSSDPAnnouncer(ctx)

		// May return error (context cancelled) or nil, but must not panic
		_ = err // We just verify it doesn't panic
	})
}

// TestHDHomeRunIntegrationContract verifies integration with main API
func TestHDHomeRunIntegrationContract(t *testing.T) {
	t.Run("DisabledByDefault", func(t *testing.T) {
		// Contract: HDHomeRun is disabled by default
		logger := log.WithComponent("test")

		os.Unsetenv("XG2G_HDHR_ENABLED")

		cfg := hdhr.GetConfigFromEnv(logger, t.TempDir())

		assert.False(t, cfg.Enabled,
			"HDHomeRun must be disabled by default")
	})

	t.Run("EnabledViaEnv", func(t *testing.T) {
		// Contract: HDHomeRun can be enabled via XG2G_HDHR_ENABLED
		logger := log.WithComponent("test")

		os.Setenv("XG2G_HDHR_ENABLED", "true")
		defer os.Unsetenv("XG2G_HDHR_ENABLED")

		cfg := hdhr.GetConfigFromEnv(logger, t.TempDir())

		assert.True(t, cfg.Enabled,
			"XG2G_HDHR_ENABLED=true must enable HDHomeRun")
	})
}
