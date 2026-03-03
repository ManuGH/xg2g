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
	v3 "github.com/ManuGH/xg2g/internal/control/http/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAPIServerContract verifies the API server's external contract
func TestAPIServerContract(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := config.AppConfig{
		Version:        "test",
		DataDir:        tmpDir,
		Bouquet:        "Test",
		XMLTVPath:      "xmltv.xml",
		APIToken:       "test-token",
		APITokenScopes: []string{string(v3.ScopeV3Read), string(v3.ScopeV3Write), string(v3.ScopeV3Status)},
		Enigma2: config.Enigma2Settings{
			BaseURL:    "http://example.com",
			StreamPort: 8001,
			Timeout:    10 * time.Second,
			Retries:    3,
			Backoff:    500 * time.Millisecond,
		},
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
		// Contract: /api/v3/system/health returns JSON with system health information
		req := httptest.NewRequest(http.MethodGet, "/api/v3/system/health", nil)
		req.Header.Set("Authorization", "Bearer test-token")
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
		_, hasUptimeCamel := health["uptimeSeconds"]
		_, hasUptimeSnake := health["uptime_seconds"]
		assert.True(t, hasUptimeCamel || hasUptimeSnake, "Health must contain uptime field")

		// Contract: version must be string
		assert.IsType(t, "", health["version"], "version must be string")

		// Contract: status must be string ("ok", "degraded", etc.)
		assert.IsType(t, "", health["status"], "status must be string")
	})

	t.Run("AuthenticationContract", func(t *testing.T) {
		// Contract: Protected v3 endpoints require valid auth token
		tests := []struct {
			name           string
			endpoint       string
			method         string
			token          string
			expectedStatus int
		}{
			{
				name:           "no_token",
				endpoint:       "/api/v3/system/health",
				method:         http.MethodGet,
				token:          "",
				expectedStatus: http.StatusUnauthorized,
			},
			{
				name:           "wrong_token",
				endpoint:       "/api/v3/system/health",
				method:         http.MethodGet,
				token:          "wrong-token",
				expectedStatus: http.StatusUnauthorized,
			},
			{
				name:           "valid_token",
				endpoint:       "/api/v3/system/health",
				method:         http.MethodGet,
				token:          "test-token",
				expectedStatus: http.StatusOK,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				req := httptest.NewRequest(tt.method, tt.endpoint, nil)
				if tt.token != "" {
					req.Header.Set("Authorization", "Bearer "+tt.token)
				}
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)

				assert.Equal(t, tt.expectedStatus, rec.Code,
					"Authentication contract violated for %s", tt.name)
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
			"Referrer-Policy",
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
		// Contract: Unauthorized responses use RFC7807 problem details
		req := httptest.NewRequest(http.MethodGet, "/api/v3/system/health", nil)
		// No auth token - will return error
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		// API returns RFC7807 problem+json for auth failures
		assert.Equal(t, http.StatusUnauthorized, rec.Code, "Missing auth should return 401")
		assert.Contains(t, rec.Header().Get("Content-Type"), "application/problem+json",
			"Error responses must use RFC7807 content type")

		var errResponse map[string]interface{}
		err := json.Unmarshal(rec.Body.Bytes(), &errResponse)
		require.NoError(t, err, "Error response must be valid JSON")
		assert.Equal(t, "UNAUTHORIZED", errResponse["code"], "Problem code must indicate unauthorized")
		assert.Equal(t, float64(http.StatusUnauthorized), errResponse["status"], "Problem status must match HTTP status")
		assert.NotEmpty(t, errResponse["type"], "Problem response must include type")
		assert.NotEmpty(t, errResponse["title"], "Problem response must include title")
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
		Version:        "test",
		DataDir:        tmpDir,
		Bouquet:        "Test",
		XMLTVPath:      "xmltv.xml",
		APIToken:       "test-token",
		APITokenScopes: []string{string(v3.ScopeV3Read)},
		Enigma2: config.Enigma2Settings{
			BaseURL:    "http://example.com",
			StreamPort: 8001,
			Timeout:    10 * time.Second,
			Retries:    3,
			Backoff:    500 * time.Millisecond,
		},
	}

	cfgMgr := config.NewManager(filepath.Join(cfg.DataDir, "config.yaml"))
	server, err := api.New(cfg, cfgMgr)
	require.NoError(t, err)
	handler := server.Handler()

	t.Run("ValidFileAccess", func(t *testing.T) {
		// Contract: Files within data dir are accessible via /files/ prefix
		req := httptest.NewRequest(http.MethodGet, "/files/playlist.m3u", nil)
		req.RemoteAddr = "127.0.0.1:1234" // LAN guard allows localhost
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
			req.RemoteAddr = "127.0.0.1:1234" // LAN guard allows localhost
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
		Version:        "1.2.3",
		DataDir:        tmpDir,
		Bouquet:        "Test",
		XMLTVPath:      "xmltv.xml",
		APIToken:       "test-token",
		APITokenScopes: []string{string(v3.ScopeV3Read)},
		Enigma2: config.Enigma2Settings{
			BaseURL:    "http://example.com",
			StreamPort: 8001,
			Timeout:    10 * time.Second,
			Retries:    3,
			Backoff:    500 * time.Millisecond,
		},
	}

	cfgMgr := config.NewManager(filepath.Join(cfg.DataDir, "config.yaml"))
	server, err := api.New(cfg, cfgMgr)
	require.NoError(t, err)
	handler := server.Handler()

	t.Run("V3EndpointsExist", func(t *testing.T) {
		// Contract: Canonical v3 API endpoints are available
		v3Endpoints := []string{
			"/api/v3/system/health",
			"/api/v3/dvr/status",
		}

		for _, endpoint := range v3Endpoints {
			req := httptest.NewRequest(http.MethodGet, endpoint, nil)
			req.Header.Set("Authorization", "Bearer test-token")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			assert.NotEqual(t, http.StatusNotFound, rec.Code,
				"V3 endpoint must exist: %s", endpoint)
		}
	})

	t.Run("LegacyEndpointsRemoved", func(t *testing.T) {
		// Contract: legacy v2 and pre-v2 endpoints are removed
		req := httptest.NewRequest(http.MethodGet, "/api/v2/system/health", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code,
			"Legacy v2 endpoint should be removed (use /api/v3/*)")

		reqLegacy := httptest.NewRequest(http.MethodGet, "/api/status", nil)
		recLegacy := httptest.NewRecorder()
		handler.ServeHTTP(recLegacy, reqLegacy)
		assert.Equal(t, http.StatusNotFound, recLegacy.Code,
			"Legacy /api/status endpoint should be removed")
	})
}

// TestAPICircuitBreakerContract verifies circuit breaker behavior
func TestAPICircuitBreakerContract(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping circuit breaker contract test in short mode")
	}

	tmpDir := t.TempDir()

	cfg := config.AppConfig{
		Version:        "test",
		DataDir:        tmpDir,
		Bouquet:        "Test",
		XMLTVPath:      "xmltv.xml",
		APIToken:       "test-token",
		APITokenScopes: []string{string(v3.ScopeV3Write)},
		Enigma2: config.Enigma2Settings{
			BaseURL:    "http://invalid-backend-that-will-fail.local",
			StreamPort: 8001,
			Timeout:    1 * time.Second,
			Retries:    0, // No retries for faster test
			Backoff:    100 * time.Millisecond,
		},
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

			req := httptest.NewRequest(http.MethodPost, "/api/v3/system/refresh", nil)
			req.Header.Set("Authorization", "Bearer test-token")
			req.Host = "example.com"
			req.Header.Set("Origin", "http://example.com") // Pass CSRF to reach business logic
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
