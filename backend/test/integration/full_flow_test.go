// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

//go:build integration

package test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/api"
	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/jobs"
	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/ManuGH/xg2g/test/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFullRefreshFlow tests the complete refresh flow from API call to file generation
func TestFullRefreshFlow(t *testing.T) {
	// Setup: Create temp directory for output
	tmpDir := t.TempDir()

	// Setup: Start mock OpenWebIF server
	mock := openwebif.NewMockServer()
	defer mock.Close()

	// Setup: Configure jobs
	cfg := config.AppConfig{
		DataDir:           tmpDir,
		Bouquet:           "Premium",
		XMLTVPath:         "xmltv.xml",
		EPGEnabled:        true,
		EPGDays:           1,
		EPGMaxConcurrency: 2,
		Enigma2: config.Enigma2Settings{
			BaseURL:    mock.URL(),
			StreamPort: 8001,
		},
	}

	// Execute: Trigger refresh
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	status, err := jobs.Refresh(ctx, config.BuildSnapshot(cfg, config.ReadOSRuntimeEnvOrDefault()))

	// Verify: Refresh succeeded
	require.NoError(t, err, "Refresh should complete successfully")
	require.NotNil(t, status, "Status should not be nil")
	assert.Greater(t, status.Channels, 0, "Should have processed channels")
	assert.NotZero(t, status.LastRun, "LastRun should be set")

	// Verify: M3U playlist was created
	playlistPath := filepath.Join(tmpDir, "playlist.m3u8")
	require.FileExists(t, playlistPath, "Playlist file should exist")

	playlistContent, err := os.ReadFile(playlistPath)
	require.NoError(t, err, "Should read playlist file")

	// Verify: M3U content is valid
	playlistStr := string(playlistContent)
	assert.Contains(t, playlistStr, "#EXTM3U", "Should have M3U header")
	assert.Contains(t, playlistStr, "#EXTINF", "Should have at least one channel entry")
	assert.NotContains(t, playlistStr, "FROM BOUQUET", "Should not contain raw bouquet references")

	// Verify: XMLTV was created
	xmltvPath := filepath.Join(tmpDir, "xmltv.xml")
	require.FileExists(t, xmltvPath, "XMLTV file should exist")

	xmltvContent, err := os.ReadFile(xmltvPath)
	require.NoError(t, err, "Should read XMLTV file")

	// Verify: XMLTV content is valid
	xmltvStr := string(xmltvContent)
	assert.Contains(t, xmltvStr, "<?xml version", "Should have XML declaration")
	assert.Contains(t, xmltvStr, "<tv", "Should have tv root element")
	assert.Contains(t, xmltvStr, "<channel", "Should have channel elements")

	t.Logf("✅ Full refresh flow completed successfully")
	t.Logf("   Channels: %d", status.Channels)
	t.Logf("   Playlist size: %d bytes", len(playlistContent))
	t.Logf("   XMLTV size: %d bytes", len(xmltvContent))
}

// TestRefreshWithBackendError tests error handling when backend fails
func TestRefreshWithBackendError(t *testing.T) {
	tmpDir := t.TempDir()

	// Setup: Mock server that fails
	failingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("Backend error"))
	}))
	defer failingServer.Close()

	cfg := config.AppConfig{
		DataDir:    tmpDir,
		Bouquet:    "Premium",
		EPGEnabled: false,
		Enigma2: config.Enigma2Settings{
			BaseURL:    failingServer.URL,
			StreamPort: 8001,
		},
	}

	// Execute: Refresh should handle error gracefully
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := jobs.Refresh(ctx, config.BuildSnapshot(cfg, config.ReadOSRuntimeEnvOrDefault()))

	// Verify: Error is returned but doesn't panic
	assert.Error(t, err, "Should return error when backend fails")
	assert.Contains(t, err.Error(), "500", "Error should mention status code")

	t.Logf("✅ Backend error handled gracefully: %v", err)
}

// TestRefreshWithTimeout tests timeout handling
func TestRefreshWithTimeout(t *testing.T) {
	tmpDir := t.TempDir()

	// Setup: Mock server with slow responses
	slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second) // Slower than timeout
		w.WriteHeader(http.StatusOK)
	}))
	defer slowServer.Close()

	cfg := config.AppConfig{
		DataDir:    tmpDir,
		Bouquet:    "Premium",
		EPGEnabled: false,
		Enigma2: config.Enigma2Settings{
			BaseURL:    slowServer.URL,
			StreamPort: 8001,
		},
	}

	// Execute: Refresh with very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	_, err := jobs.Refresh(ctx, config.BuildSnapshot(cfg, config.ReadOSRuntimeEnvOrDefault()))

	// Verify: Timeout error
	assert.Error(t, err, "Should timeout")
	assert.True(t,
		strings.Contains(err.Error(), "context deadline exceeded") ||
			strings.Contains(err.Error(), "timeout"),
		"Error should indicate timeout: %v", err)

	t.Logf("✅ Timeout handled correctly: %v", err)
}

// TestRefreshWithPartialFailure tests resilience to partial failures
func TestRefreshWithPartialFailure(t *testing.T) {
	tmpDir := t.TempDir()

	// Setup: Mock server that fails some requests
	requestCount := 0
	partialFailServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++

		// Fail every 3rd request
		if requestCount%3 == 0 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}

		// Handle different endpoints
		if strings.Contains(r.URL.Path, "bouquets") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"bouquets": [["Premium", "1:7:1:0:0:0:0:0:0:0:FROM BOUQUET"]]}`))
		} else {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"services": []}`))
		}
	}))
	defer partialFailServer.Close()

	cfg := config.AppConfig{
		DataDir:    tmpDir,
		Bouquet:    "Premium",
		EPGEnabled: false,
		Enigma2: config.Enigma2Settings{
			BaseURL:    partialFailServer.URL,
			StreamPort: 8001,
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Execute: Should handle partial failures
	_, err := jobs.Refresh(ctx, config.BuildSnapshot(cfg, config.ReadOSRuntimeEnvOrDefault()))

	// Verify: May succeed or fail depending on which request failed
	// but should not panic or hang
	t.Logf("Partial failure result: %v (requests made: %d)", err, requestCount)
	assert.Greater(t, requestCount, 0, "Should have made requests")
}

// TestHealthCheckFlow tests complete health check flow
func TestHealthCheckFlow(t *testing.T) {
	tmpDir := t.TempDir()

	mock := openwebif.NewMockServer()
	defer mock.Close()

	cfg := config.AppConfig{
		DataDir:        tmpDir,
		Bouquet:        "Premium",
		APIToken:       "test-token",
		APITokenScopes: []string{"v3:read"},
		Enigma2: config.Enigma2Settings{
			BaseURL:    mock.URL(),
			StreamPort: 8001,
		},
	}

	helpers.EnsureDecisionSecret(t)
	cfgMgr := config.NewManager(filepath.Join(cfg.DataDir, "config.yaml"))
	apiServer, err := api.New(cfg, cfgMgr)
	require.NoError(t, err)
	handler := apiServer.Handler()
	testServer := httptest.NewServer(handler)
	defer testServer.Close()

	tests := []struct {
		name           string
		endpoint       string
		expectedStatus int
		shouldContain  string
	}{
		{
			name:           "health check",
			endpoint:       "/healthz",
			expectedStatus: http.StatusOK,
			shouldContain:  "healthy", // JSON response: {"status":"healthy",...}
		},
		{
			name:           "readiness check before refresh",
			endpoint:       "/readyz",
			expectedStatus: http.StatusServiceUnavailable, // Not ready before first refresh - correct behavior
			shouldContain:  "unhealthy",                   // JSON response: {"ready":false,"status":"unhealthy",...}
		},
		{
			name:           "status before refresh",
			endpoint:       "/api/v3/system/health",
			expectedStatus: http.StatusOK,
			shouldContain:  "channels",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequestWithContext(
				context.Background(),
				http.MethodGet,
				testServer.URL+tt.endpoint,
				nil,
			)
			require.NoError(t, err)
			if strings.HasPrefix(tt.endpoint, "/api/v3/") {
				req.Header.Set("Authorization", "Bearer test-token")
			}

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, tt.expectedStatus, resp.StatusCode, "Status code mismatch")

			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)

			bodyStr := string(body)
			if tt.shouldContain != "" {
				assert.Contains(t, strings.ToLower(bodyStr), tt.shouldContain,
					"Response should contain expected content")
			}

			t.Logf("✅ %s: %d - %s", tt.name, resp.StatusCode, bodyStr)
		})
	}
}
