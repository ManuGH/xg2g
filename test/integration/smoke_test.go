// SPDX-License-Identifier: MIT

//go:build integration_fast || integration
// +build integration_fast integration

package test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/api"
	"github.com/ManuGH/xg2g/internal/jobs"
	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSmoke_HealthEndpoints tests critical health check endpoints (< 100ms)
// Tag: critical, fast
// Risk Level: HIGH - production health checks depend on this
func TestSmoke_HealthEndpoints(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := jobs.Config{
		DataDir:    tmpDir,
		OWIBase:    "http://test.local",
		StreamPort: 8001,
	}

	apiServer := api.New(cfg)
	handler := apiServer.Handler()
	testServer := httptest.NewServer(handler)
	defer testServer.Close()

	tests := []struct {
		name           string
		endpoint       string
		expectedStatus int
	}{
		{"liveness", "/healthz", http.StatusOK},
		{"readiness_not_ready", "/readyz", http.StatusServiceUnavailable},
		{"status", "/api/v1/status", http.StatusOK},
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

			assert.Equal(t, tt.expectedStatus, resp.StatusCode,
				"Health endpoint %s returned unexpected status", tt.endpoint)
		})
	}
}

// TestSmoke_RefreshEndpointAuth tests API authentication (< 50ms)
// Tag: critical, fast, security
// Risk Level: HIGH - authentication bypass would be critical
func TestSmoke_RefreshEndpointAuth(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := jobs.Config{
		DataDir:    tmpDir,
		OWIBase:    "http://test.local",
		StreamPort: 8001,
		APIToken:   "test-token",
	}

	apiServer := api.New(cfg)
	handler := apiServer.Handler()
	testServer := httptest.NewServer(handler)
	defer testServer.Close()

	tests := []struct {
		name           string
		token          string
		expectedStatus int
	}{
		{"no_token", "", http.StatusUnauthorized},
		{"wrong_token", "wrong", http.StatusForbidden}, // 403 for wrong token (not 401)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequestWithContext(
				context.Background(),
				http.MethodPost,
				testServer.URL+"/api/v1/refresh",
				nil,
			)
			require.NoError(t, err)

			if tt.token != "" {
				req.Header.Set("X-API-Token", tt.token)
			}

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, tt.expectedStatus, resp.StatusCode,
				"Auth test '%s' returned unexpected status", tt.name)
		})
	}
}

// TestSmoke_BasicRefreshFlow tests minimal refresh cycle (< 500ms)
// Tag: critical, fast
// Risk Level: HIGH - core business logic
func TestSmoke_BasicRefreshFlow(t *testing.T) {
	tmpDir := t.TempDir()

	mock := openwebif.NewMockServer()
	defer mock.Close()

	cfg := jobs.Config{
		DataDir:    tmpDir,
		OWIBase:    mock.URL(),
		Bouquet:    "Premium",
		StreamPort: 8001,
		XMLTVPath:  "xmltv.xml",
		EPGEnabled: false, // Disable EPG for speed
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	status, err := jobs.Refresh(ctx, cfg)

	require.NoError(t, err, "Basic refresh should succeed")
	require.NotNil(t, status)
	assert.Greater(t, status.Channels, 0, "Should find channels")
}

// TestSmoke_ConcurrentAPIRequests tests basic concurrency safety (< 1s)
// Tag: critical, fast, concurrency
// Risk Level: HIGH - race conditions can cause production issues
func TestSmoke_ConcurrentAPIRequests(t *testing.T) {
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

	// Make 3 concurrent requests (fast smoke test, not load test)
	const numRequests = 3
	results := make(chan int, numRequests)

	for i := 0; i < numRequests; i++ {
		go func() {
			req, _ := http.NewRequestWithContext(
				context.Background(),
				http.MethodPost,
				testServer.URL+"/api/v1/refresh",
				nil,
			)
			req.Header.Set("X-API-Token", "test-token")

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				results <- 0
				return
			}
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
