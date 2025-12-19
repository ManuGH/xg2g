// SPDX-License-Identifier: MIT

//go:build integration_fast || integration

package test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/jobs"
	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/ManuGH/xg2g/test/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAPIFast_HealthEndpoints tests critical health check endpoints (< 100ms)
// Tag: critical, fast
// Risk Level: HIGH - production health checks depend on this
func TestAPIFast_HealthEndpoints(t *testing.T) {
	ts := helpers.NewTestServer(t, helpers.TestServerOptions{
		DataDir:  t.TempDir(),
		APIToken: "test-token",
	})
	defer ts.Close()

	tests := []struct {
		name           string
		endpoint       string
		token          string
		expectedStatus int
	}{
		{"liveness", "/healthz", "", http.StatusOK},
		{"readiness_not_ready", "/readyz", "", http.StatusServiceUnavailable},
		{"status", "/api/v2/system/health", "test-token", http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := helpers.DoRequest(t, ts.Server.URL, helpers.RequestOptions{
				Method: http.MethodGet,
				Path:   tt.endpoint,
				Token:  tt.token,
			})
			defer resp.Body.Close()

			assert.Equal(t, tt.expectedStatus, resp.StatusCode,
				"Health endpoint %s returned unexpected status", tt.endpoint)
		})
	}
}

// TestAPIFast_RefreshEndpointAuth tests API authentication (< 50ms)
// Tag: critical, fast, security
// Risk Level: HIGH - authentication bypass would be critical
func TestAPIFast_RefreshEndpointAuth(t *testing.T) {
	ts := helpers.NewTestServer(t, helpers.TestServerOptions{
		DataDir:  t.TempDir(),
		APIToken: "test-token",
	})
	defer ts.Close()

	tests := []struct {
		name           string
		token          string
		expectedStatus int
	}{
		{"no_token", "", http.StatusUnauthorized},
		{"wrong_token", "wrong", http.StatusUnauthorized}, // API returns 401 for both missing and invalid tokens
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := helpers.DoRequest(t, ts.Server.URL, helpers.RequestOptions{
				Method: http.MethodPost,
				Path:   "/api/v2/system/refresh",
				Token:  tt.token,
			})
			defer resp.Body.Close()

			assert.Equal(t, tt.expectedStatus, resp.StatusCode,
				"Auth test '%s' returned unexpected status", tt.name)
		})
	}
}

// TestAPIFast_BasicRefreshFlow tests minimal refresh cycle (< 500ms)
// Tag: critical, fast
// Risk Level: HIGH - core business logic
func TestAPIFast_BasicRefreshFlow(t *testing.T) {
	tmpDir := t.TempDir()

	mock := openwebif.NewMockServer()
	defer mock.Close()

	cfg := config.AppConfig{
		DataDir:    tmpDir,
		OWIBase:    mock.URL(),
		Bouquet:    "Premium",
		StreamPort: 8001,
		XMLTVPath:  "xmltv.xml",
		EPGEnabled: false, // Disable EPG for speed
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	status, err := jobs.Refresh(ctx, config.BuildSnapshot(cfg))

	require.NoError(t, err, "Basic refresh should succeed")
	require.NotNil(t, status)
	assert.Greater(t, status.Channels, 0, "Should find channels")
}

// TestAPIFast_ConcurrentAPIRequests tests basic concurrency safety (< 1s)
// Tag: critical, fast, concurrency
// Risk Level: HIGH - race conditions can cause production issues
func TestAPIFast_ConcurrentAPIRequests(t *testing.T) {
	mock := openwebif.NewMockServer()
	defer mock.Close()

	ts := helpers.NewTestServer(t, helpers.TestServerOptions{
		DataDir:  t.TempDir(),
		OWIBase:  mock.URL(),
		APIToken: "test-token",
	})
	defer ts.Close()

	// Make 3 concurrent requests (fast smoke test, not load test)
	const numRequests = 3
	results := make(chan int, numRequests)

	for i := 0; i < numRequests; i++ {
		go func() {
			resp := helpers.DoRequest(t, ts.Server.URL, helpers.RequestOptions{
				Method: http.MethodPost,
				Path:   "/api/v2/system/refresh",
				Token:  "test-token",
			})
			defer resp.Body.Close()
			results <- resp.StatusCode
		}()
	}

	// Collect results
	var successCount int
	for i := 0; i < numRequests; i++ {
		status := <-results
		if status == http.StatusOK || status == http.StatusConflict {
			successCount++
		}
	}

	// At least one should succeed, others may get 409 Conflict (expected for concurrent refresh)
	assert.Greater(t, successCount, 0, "At least one concurrent request should succeed or conflict gracefully")
}
