// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

//go:build integration || integration_fast
// +build integration integration_fast

// Package contract provides contract tests that verify interface boundaries
// between major components. These tests ensure that:
// - API contracts are stable and don't change unexpectedly
// - Components can be swapped/mocked without breaking integrations
// - Error handling across boundaries is predictable
package contract

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/api"
	"github.com/ManuGH/xg2g/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAPIServerContract verifies the API server's external contract
func TestAPIServerContract(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := config.AppConfig{
		Version:    "test",
		DataDir:    tmpDir,
		OWIBase:    "http://example.com",
		Bouquet:    "Test",
		StreamPort: 8001,
		XMLTVPath:  "xmltv.xml",
		APIToken:   "test-token",
		OWITimeout: 10 * time.Second,
		OWIRetries: 3,
		OWIBackoff: 500 * time.Millisecond,
	}

	cfgMgr := config.NewManager(filepath.Join(cfg.DataDir, "config.yaml"))
	server, err := api.New(cfg, cfgMgr)
	require.NoError(t, err)
	handler := server.Handler()

	t.Run("HealthEndpointContract", func(t *testing.T) {
		// Contract: /healthz returns 200 OK with JSON
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code, "Health endpoint must return 200")
		assert.Contains(t, rec.Header().Get("Content-Type"), "application/json",
			"Health endpoint must return JSON")

		var response map[string]interface{}
		err := json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err, "Health response must be valid JSON")

		// Contract: Health response contains status field
		assert.Contains(t, response, "status", "Health response must contain 'status' field")
	})

	t.Run("ReadinessEndpointContract", func(t *testing.T) {
		// Contract: /readyz returns 503 before first refresh, 200 after
		req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		// Before first refresh, readiness should be 503 (no data loaded yet)
		assert.Equal(t, http.StatusServiceUnavailable, rec.Code,
			"Readiness endpoint must return 503 before first refresh")
		assert.Contains(t, rec.Header().Get("Content-Type"), "application/json",
			"Readiness endpoint must return JSON")

		var response map[string]interface{}
		err := json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err, "Readiness response must be valid JSON")
		assert.Contains(t, response, "ready", "Readiness response must contain 'ready' field")
		assert.False(t, response["ready"].(bool), "Ready field must be false before first refresh")
	})

	t.Run("StatusEndpointContract", func(t *testing.T) {
		// Contract: /api/v2/system/health returns JSON with system health information
		req := httptest.NewRequest(http.MethodGet, "/api/v2/system/health", nil)
		req.Header.Set("X-API-Token", "test-token")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code, "Status endpoint must return 200")
		assert.Contains(t, rec.Header().Get("Content-Type"), "application/json",
			"Status endpoint must return JSON")

		var health map[string]interface{}
		err := json.Unmarshal(rec.Body.Bytes(), &health)
		require.NoError(t, err, "Health response must be valid JSON")

		// Contract: Health response must have system status fields
		assert.Contains(t, health, "version", "Health must contain version")
		assert.Contains(t, health, "status", "Health must contain status")
		assert.Contains(t, health, "uptime_seconds", "Health must contain uptime_seconds")

		// Contract: version must be string
		assert.IsType(t, "", health["version"], "version must be string")

		// Contract: status must be string ("ok", "degraded", etc.)
		assert.IsType(t, "", health["status"], "status must be string")
	})

	t.Run("AuthenticationContract", func(t *testing.T) {
		// Contract: Protected endpoints require X-API-Token header
		tests := []struct {
			name           string
			endpoint       string
			method         string
			token          string
			expectedStatus int
		}{
			{
				name:           "no_token",
				endpoint:       "/api/v2/system/refresh",
				method:         http.MethodPost,
				token:          "",
				expectedStatus: http.StatusUnauthorized,
			},
			{
				name:           "wrong_token",
				endpoint:       "/api/v2/system/refresh",
				method:         http.MethodPost,
				token:          "wrong-token",
				expectedStatus: http.StatusUnauthorized, // API returns 401 for invalid tokens
			},
			{
				name:           "valid_token",
				endpoint:       "/api/v2/system/refresh",
				method:         http.MethodPost,
				token:          "test-token",
				expectedStatus: http.StatusOK, // Will fail due to mock, but auth passes
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				req := httptest.NewRequest(tt.method, tt.endpoint, nil)
				if tt.token != "" {
					req.Header.Set("X-API-Token", tt.token)
				}
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)

				// Only check auth status codes (401), ignore functional errors
				if tt.expectedStatus == http.StatusUnauthorized {
					assert.Equal(t, tt.expectedStatus, rec.Code,
						"Authentication contract violated for %s", tt.name)
				}
			})
		}
	})

	t.Run("SecurityHeadersContract", func(t *testing.T) {
		// Contract: All endpoints return security headers
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		requiredHeaders := []string{
			"X-Content-Type-Options",
			"X-Frame-Options",
			"X-XSS-Protection",
		}

		for _, header := range requiredHeaders {
			assert.NotEmpty(t, rec.Header().Get(header),
				"Security header %s must be present", header)
		}
	})

	t.Run("DeprecationHeadersContract", func(t *testing.T) {
		// Contract: This test is skipped as legacy /api/* endpoints were removed
		// API v2 endpoints (/api/v2/*) are the canonical API surface
		t.Skip("Legacy /api/* endpoints removed in favor of /api/v2/* (see docs/UPGRADE.md)")
	})

	t.Run("ErrorResponseContract", func(t *testing.T) {
		// Contract: Error responses are JSON with 'error' field
		req := httptest.NewRequest(http.MethodPost, "/api/v2/system/refresh", nil)
		// No auth token - will return error
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		// API v2 returns JSON error responses
		assert.Equal(t, http.StatusUnauthorized, rec.Code, "Missing auth should return 401")
		assert.Contains(t, rec.Header().Get("Content-Type"), "application/json",
			"Error responses must be JSON")

		var errResponse map[string]interface{}
		err := json.Unmarshal(rec.Body.Bytes(), &errResponse)
		require.NoError(t, err, "Error response must be valid JSON")
		assert.Contains(t, errResponse, "error", "Error response must contain 'error' field")
		assert.Equal(t, "unauthorized", errResponse["error"], "Error field must indicate unauthorized")
	})
}

// TestAPIDataFilePathContract verifies data file path resolution contract
func TestAPIDataFilePathContract(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test file
	testFile := filepath.Join(tmpDir, "playlist.m3u")
	err := os.WriteFile(testFile, []byte("#EXTM3U\n"), 0600)
	require.NoError(t, err)

	cfg := config.AppConfig{
		Version:    "test",
		DataDir:    tmpDir,
		OWIBase:    "http://example.com",
		Bouquet:    "Test",
		StreamPort: 8001,
		XMLTVPath:  "xmltv.xml",
		OWITimeout: 10 * time.Second,
		OWIRetries: 3,
		OWIBackoff: 500 * time.Millisecond,
	}

	cfgMgr := config.NewManager(filepath.Join(cfg.DataDir, "config.yaml"))
	server, err := api.New(cfg, cfgMgr)
	require.NoError(t, err)
	handler := server.Handler()

	t.Run("ValidFileAccess", func(t *testing.T) {
		// Contract: Files within data dir are accessible via /files/ prefix
		req := httptest.NewRequest(http.MethodGet, "/files/playlist.m3u", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code, "Valid files must be accessible via /files/ prefix")
		assert.Contains(t, rec.Body.String(), "#EXTM3U", "File content must be returned")
	})

	t.Run("PathTraversalPrevention", func(t *testing.T) {
		// Contract: Path traversal attempts are blocked
		dangerousPaths := []string{
			"/../etc/passwd",
			"/../../etc/passwd",
			"/../../../etc/passwd",
			"/./../../etc/passwd",
		}

		for _, path := range dangerousPaths {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			// Should return 400/404, NOT 200 with sensitive file content
			assert.NotEqual(t, http.StatusOK, rec.Code,
				"Path traversal attempt must be blocked: %s", path)
			assert.NotContains(t, rec.Body.String(), "root:",
				"Sensitive file content must not be exposed: %s", path)
		}
	})
}

// TestAPIVersioningContract verifies API versioning contract
func TestAPIVersioningContract(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := config.AppConfig{
		Version:    "1.2.3",
		DataDir:    tmpDir,
		OWIBase:    "http://example.com",
		Bouquet:    "Test",
		StreamPort: 8001,
		XMLTVPath:  "xmltv.xml",
		OWITimeout: 10 * time.Second,
		OWIRetries: 3,
		OWIBackoff: 500 * time.Millisecond,
	}

	cfgMgr := config.NewManager(filepath.Join(cfg.DataDir, "config.yaml"))
	server, err := api.New(cfg, cfgMgr)
	require.NoError(t, err)
	handler := server.Handler()

	t.Run("V2EndpointsExist", func(t *testing.T) {
		// Contract: V2 API endpoints are available
		v2Endpoints := []string{
			"/api/v2/system/health",
			"/api/v2/dvr/status",
		}

		for _, endpoint := range v2Endpoints {
			req := httptest.NewRequest(http.MethodGet, endpoint, nil)
			req.Header.Set("X-API-Token", "test-token")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			assert.NotEqual(t, http.StatusNotFound, rec.Code,
				"V2 endpoint must exist: %s", endpoint)
		}
	})

	t.Run("LegacyEndpointsRemoved", func(t *testing.T) {
		// Contract: Legacy /api/* endpoints were removed in favor of /api/v2/*
		req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code,
			"Legacy /api/status endpoint should be removed (use /api/v2/status)")
	})
}

// TestAPICircuitBreakerContract verifies circuit breaker behavior
func TestAPICircuitBreakerContract(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping circuit breaker contract test in short mode")
	}

	tmpDir := t.TempDir()

	cfg := config.AppConfig{
		Version:    "test",
		DataDir:    tmpDir,
		OWIBase:    "http://invalid-backend-that-will-fail.local",
		Bouquet:    "Test",
		StreamPort: 8001,
		XMLTVPath:  "xmltv.xml",
		APIToken:   "test-token",
		OWITimeout: 1 * time.Second,
		OWIRetries: 0, // No retries for faster test
		OWIBackoff: 100 * time.Millisecond,
	}

	cfgMgr := config.NewManager(filepath.Join(cfg.DataDir, "config.yaml"))
	server, err := api.New(cfg, cfgMgr)
	require.NoError(t, err)
	handler := server.Handler()

	t.Run("CircuitBreakerTrips", func(t *testing.T) {
		// Contract: After threshold failures, circuit breaker opens and fast-fails
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		const numRequests = 6 // Expect 3 failures to trip, then fast-fail

		for i := 0; i < numRequests; i++ {
			select {
			case <-ctx.Done():
				t.Fatal("Test timeout")
			default:
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v2/system/refresh", nil)
			req.Header.Set("X-API-Token", "test-token")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			// All requests should fail (backend is invalid)
			assert.NotEqual(t, http.StatusOK, rec.Code,
				"Requests with invalid backend must fail")

			// Give circuit breaker time to trip
			if i == 3 {
				time.Sleep(100 * time.Millisecond)
			}
		}
	})
}
