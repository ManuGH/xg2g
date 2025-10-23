// SPDX-License-Identifier: MIT

//go:build integration
// +build integration

package test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/api"
	"github.com/ManuGH/xg2g/internal/jobs"
	"github.com/ManuGH/xg2g/internal/openwebif"
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
	cfg := jobs.Config{
		DataDir:           tmpDir,
		OWIBase:           mock.URL(),
		Bouquet:           "Premium",
		StreamPort:        8001,
		XMLTVPath:         "xmltv.xml",
		EPGEnabled:        true,
		EPGDays:           1,
		EPGMaxConcurrency: 2,
	}

	// Execute: Trigger refresh
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	status, err := jobs.Refresh(ctx, cfg)

	// Verify: Refresh succeeded
	require.NoError(t, err, "Refresh should complete successfully")
	require.NotNil(t, status, "Status should not be nil")
	assert.Greater(t, status.Channels, 0, "Should have processed channels")
	assert.NotZero(t, status.LastRun, "LastRun should be set")

	// Verify: M3U playlist was created
	playlistPath := filepath.Join(tmpDir, "playlist.m3u")
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

// TestAPIRefreshEndpoint tests the complete flow through API endpoint
func TestAPIRefreshEndpoint(t *testing.T) {
	// Setup: Temp directory
	tmpDir := t.TempDir()

	// Setup: Mock OpenWebIF
	mock := openwebif.NewMockServer()
	defer mock.Close()

	// Setup: API server
	cfg := jobs.Config{
		DataDir:    tmpDir,
		OWIBase:    mock.URL(),
		StreamPort: 8001,
		Bouquet:    "Premium",
		APIToken:   "test-token",
		XMLTVPath:  "xmltv.xml",
		EPGEnabled: false, // Disable EPG for faster test
	}

	apiServer := api.New(cfg)
	handler := apiServer.Handler()
	testServer := httptest.NewServer(handler)
	defer testServer.Close()

	// Execute: Call refresh endpoint
	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		testServer.URL+"/api/v1/refresh",
		nil,
	)
	require.NoError(t, err, "Should create request")
	req.Header.Set("X-API-Token", "test-token")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err, "Should make request")
	defer resp.Body.Close()

	// Verify: API response
	assert.Equal(t, http.StatusOK, resp.StatusCode, "Should return 200 OK")

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "Should read response body")

	bodyStr := string(body)
	assert.Contains(t, bodyStr, "channels", "Response should contain channels count")
	// API uses camelCase (lastRun), not snake_case (last_run)
	assert.Contains(t, bodyStr, "lastRun", "Response should contain lastRun timestamp")

	// Verify: Files were created
	playlistPath := filepath.Join(tmpDir, "playlist.m3u")
	require.FileExists(t, playlistPath, "Playlist should be generated")

	// Verify: Status endpoint reflects update
	statusReq, _ := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		testServer.URL+"/api/v1/status",
		nil,
	)

	statusResp, err := http.DefaultClient.Do(statusReq)
	require.NoError(t, err, "Status request should succeed")
	defer statusResp.Body.Close()

	statusBody, _ := io.ReadAll(statusResp.Body)
	statusStr := string(statusBody)
	// API uses camelCase (lastRun), not snake_case (last_run)
	assert.Contains(t, statusStr, "lastRun", "Status should show last refresh")

	t.Logf("✅ API refresh endpoint flow completed successfully")
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

	cfg := jobs.Config{
		DataDir:            tmpDir,
		OWIBase:   failingServer.URL,
		StreamPort:        8001,
		Bouquet:           "Premium",
		EPGEnabled:         false,
	}

	// Execute: Refresh should handle error gracefully
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := jobs.Refresh(ctx, cfg)

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

	cfg := jobs.Config{
		DataDir:            tmpDir,
		OWIBase:   slowServer.URL,
		StreamPort:        8001,
		Bouquet:           "Premium",
		EPGEnabled:         false,
	}

	// Execute: Refresh with very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	_, err := jobs.Refresh(ctx, cfg)

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

	cfg := jobs.Config{
		DataDir:            tmpDir,
		OWIBase:   partialFailServer.URL,
		StreamPort:        8001,
		Bouquet:           "Premium",
		EPGEnabled:         false,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Execute: Should handle partial failures
	_, err := jobs.Refresh(ctx, cfg)

	// Verify: May succeed or fail depending on which request failed
	// but should not panic or hang
	t.Logf("Partial failure result: %v (requests made: %d)", err, requestCount)
	assert.Greater(t, requestCount, 0, "Should have made requests")
}

// TestConcurrentRefreshRequests tests handling of concurrent refresh calls
func TestConcurrentRefreshRequests(t *testing.T) {
	tmpDir := t.TempDir()

	mock := openwebif.NewMockServer()
	defer mock.Close()

	cfg := jobs.Config{
		DataDir:    tmpDir,
		OWIBase:    mock.URL(),
		StreamPort: 8001,
		Bouquet:    "Premium",
		APIToken:   "test-token",
		EPGEnabled: false,
	}

	apiServer := api.New(cfg)
	handler := apiServer.Handler()
	testServer := httptest.NewServer(handler)
	defer testServer.Close()

	// Execute: Make multiple concurrent refresh requests
	const numRequests = 5
	results := make(chan error, numRequests)

	for i := 0; i < numRequests; i++ {
		go func(id int) {
			req, _ := http.NewRequestWithContext(
				context.Background(),
				http.MethodPost,
				testServer.URL+"/api/v1/refresh",
				nil,
			)
			req.Header.Set("X-API-Token", "test-token")

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				results <- err
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusTooManyRequests {
				results <- fmt.Errorf("unexpected status: %d", resp.StatusCode)
				return
			}

			results <- nil
		}(i)
	}

	// Verify: All requests complete without hanging
	successCount := 0
	for i := 0; i < numRequests; i++ {
		select {
		case err := <-results:
			if err == nil {
				successCount++
			} else {
				t.Logf("Request failed: %v", err)
			}
		case <-time.After(30 * time.Second):
			t.Fatal("Concurrent requests timed out")
		}
	}

	assert.Greater(t, successCount, 0, "At least one request should succeed")
	t.Logf("✅ Concurrent requests handled: %d/%d succeeded", successCount, numRequests)
}

// TestHealthCheckFlow tests complete health check flow
func TestHealthCheckFlow(t *testing.T) {
	tmpDir := t.TempDir()

	mock := openwebif.NewMockServer()
	defer mock.Close()

	cfg := jobs.Config{
		DataDir:            tmpDir,
		OWIBase:   mock.URL(),
		StreamPort:        8001,
		Bouquet:           "Premium",
	}

	apiServer := api.New(cfg)
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
			shouldContain:  "ok",
		},
		{
			name:           "readiness check before refresh",
			endpoint:       "/readyz",
			expectedStatus: http.StatusServiceUnavailable, // Not ready before first refresh - correct behavior
			shouldContain:  "not-ready",
		},
		{
			name:           "status before refresh",
			endpoint:       "/api/v1/status",
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

// TestFileServingFlow tests serving generated files
func TestFileServingFlow(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test files
	playlistPath := filepath.Join(tmpDir, "playlist.m3u")
	playlistContent := `#EXTM3U
#EXTINF:-1,Test Channel
http://example.com/stream`

	err := os.WriteFile(playlistPath, []byte(playlistContent), 0644)
	require.NoError(t, err)

	mock := openwebif.NewMockServer()
	defer mock.Close()

	cfg := jobs.Config{
		DataDir:            tmpDir,
		OWIBase:   mock.URL(),
		StreamPort:        8001,
		Bouquet:           "Premium",
	}

	apiServer := api.New(cfg)
	handler := apiServer.Handler()
	testServer := httptest.NewServer(handler)
	defer testServer.Close()

	// Test: Fetch playlist
	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		testServer.URL+"/files/playlist.m3u",
		nil,
	)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "audio/x-mpegurl")

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	assert.Equal(t, playlistContent, string(body), "Served content should match file")

	t.Logf("✅ File serving flow completed successfully")
}
