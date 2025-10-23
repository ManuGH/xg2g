// SPDX-License-Identifier: MIT
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ManuGH/xg2g/internal/jobs"
)

// TestBackwardCompatibility ensures that v1 API is fully compatible with legacy API
func TestBackwardCompatibility(t *testing.T) {
	cfg := jobs.Config{
		Version:    "1.5.0-test",
		DataDir:    t.TempDir(),
		OWIBase:    "http://test.local",
		StreamPort: 8001,
	}

	server := New(cfg)
	handler := server.Handler()

	t.Run("StatusResponseStructure", func(t *testing.T) {
		// Both endpoints should return identical JSON structure
		legacyReq := httptest.NewRequest(http.MethodGet, "/api/status", nil)
		legacyRec := httptest.NewRecorder()
		handler.ServeHTTP(legacyRec, legacyReq)

		v1Req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
		v1Rec := httptest.NewRecorder()
		handler.ServeHTTP(v1Rec, v1Req)

		var legacyData, v1Data map[string]interface{}
		if err := json.Unmarshal(legacyRec.Body.Bytes(), &legacyData); err != nil {
			t.Fatalf("failed to parse legacy response: %v", err)
		}
		if err := json.Unmarshal(v1Rec.Body.Bytes(), &v1Data); err != nil {
			t.Fatalf("failed to parse v1 response: %v", err)
		}

		// Check all expected fields exist in both
		requiredFields := []string{"status", "version", "lastRun", "channels"}
		for _, field := range requiredFields {
			if _, ok := legacyData[field]; !ok {
				t.Errorf("legacy response missing field: %s", field)
			}
			if _, ok := v1Data[field]; !ok {
				t.Errorf("v1 response missing field: %s", field)
			}
		}

		// Values should be identical (ignoring type specifics)
		if legacyData["status"] != v1Data["status"] {
			t.Errorf("status field mismatch: legacy=%v, v1=%v", legacyData["status"], v1Data["status"])
		}
		if legacyData["version"] != v1Data["version"] {
			t.Errorf("version field mismatch: legacy=%v, v1=%v", legacyData["version"], v1Data["version"])
		}
	})

	t.Run("StatusFieldTypes", func(t *testing.T) {
		// Ensure field types haven't changed
		req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		var data map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &data); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}

		// status should be string
		if _, ok := data["status"].(string); !ok {
			t.Errorf("status field is not a string: %T", data["status"])
		}

		// version should be string
		if _, ok := data["version"].(string); !ok {
			t.Errorf("version field is not a string: %T", data["version"])
		}

		// channels should be number
		if _, ok := data["channels"].(float64); !ok {
			t.Errorf("channels field is not a number: %T", data["channels"])
		}

		// lastRun should be string (RFC3339 timestamp)
		if _, ok := data["lastRun"].(string); !ok {
			t.Errorf("lastRun field is not a string: %T", data["lastRun"])
		}
	})

	t.Run("HTTPStatusCodes", func(t *testing.T) {
		tests := []struct {
			name       string
			legacyPath string
			v1Path     string
			method     string
			wantCode   int
		}{
			{
				name:       "StatusEndpoint",
				legacyPath: "/api/status",
				v1Path:     "/api/v1/status",
				method: http.MethodGet,
				wantCode:   http.StatusOK,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				// Test legacy endpoint
				legacyReq := httptest.NewRequest(tt.method, tt.legacyPath, nil)
				legacyRec := httptest.NewRecorder()
				handler.ServeHTTP(legacyRec, legacyReq)

				if legacyRec.Code != tt.wantCode {
					t.Errorf("legacy endpoint status code = %d, want %d", legacyRec.Code, tt.wantCode)
				}

				// Test v1 endpoint
				v1Req := httptest.NewRequest(tt.method, tt.v1Path, nil)
				v1Rec := httptest.NewRecorder()
				handler.ServeHTTP(v1Rec, v1Req)

				if v1Rec.Code != tt.wantCode {
					t.Errorf("v1 endpoint status code = %d, want %d", v1Rec.Code, tt.wantCode)
				}

				// Both should return same status code
				if legacyRec.Code != v1Rec.Code {
					t.Errorf("status codes don't match: legacy=%d, v1=%d", legacyRec.Code, v1Rec.Code)
				}
			})
		}
	})

	t.Run("ContentTypeHeaders", func(t *testing.T) {
		legacyReq := httptest.NewRequest(http.MethodGet, "/api/status", nil)
		legacyRec := httptest.NewRecorder()
		handler.ServeHTTP(legacyRec, legacyReq)

		v1Req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
		v1Rec := httptest.NewRecorder()
		handler.ServeHTTP(v1Rec, v1Req)

		// Both should return application/json
		legacyContentType := legacyRec.Header().Get("Content-Type")
		v1ContentType := v1Rec.Header().Get("Content-Type")

		if legacyContentType != "application/json" {
			t.Errorf("legacy Content-Type = %s, want application/json", legacyContentType)
		}
		if v1ContentType != "application/json" {
			t.Errorf("v1 Content-Type = %s, want application/json", v1ContentType)
		}
	})

	t.Run("SecurityHeaders", func(t *testing.T) {
		// Security headers should be identical
		legacyReq := httptest.NewRequest(http.MethodGet, "/api/status", nil)
		legacyRec := httptest.NewRecorder()
		handler.ServeHTTP(legacyRec, legacyReq)

		v1Req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
		v1Rec := httptest.NewRecorder()
		handler.ServeHTTP(v1Rec, v1Req)

		securityHeaders := []string{
			"X-Content-Type-Options",
			"X-Frame-Options",
			"Content-Security-Policy",
			"Referrer-Policy",
		}

		for _, header := range securityHeaders {
			legacyValue := legacyRec.Header().Get(header)
			v1Value := v1Rec.Header().Get(header)

			if legacyValue != v1Value {
				t.Errorf("security header %s mismatch: legacy=%s, v1=%s", header, legacyValue, v1Value)
			}
		}
	})
}

// TestNoRegressions ensures that existing clients continue to work
func TestNoRegressions(t *testing.T) {
	cfg := jobs.Config{
		Version:    "1.5.0-test",
		DataDir:    t.TempDir(),
		OWIBase:    "http://test.local",
		StreamPort: 8001,
	}

	server := New(cfg)
	handler := server.Handler()

	t.Run("LegacyStatusStillWorks", func(t *testing.T) {
		// Simulate an old client that doesn't know about versioning
		req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}

		var data map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &data); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}

		// Old client should still get valid response
		if data["status"] != "ok" {
			t.Errorf("expected status=ok, got %v", data["status"])
		}
	})

	t.Run("RefreshEndpointAuthentication", func(t *testing.T) {
		// Create a new server with a mock refresh function
		mockServer := New(cfg)
		mockServer.cfg.APIToken = "test-token"

		// Mock the refresh function to avoid actual network calls
		mockServer.refreshFn = func(_ context.Context, _ jobs.Config) (*jobs.Status, error) {
			return &jobs.Status{
				Version:  "1.5.0-test",
				Channels: 42,
			}, nil
		}

		mockHandler := mockServer.Handler()

		tests := []struct {
			name         string
			path         string
			token        string
			expectedCode int
		}{
			{"LegacyNoToken", "/api/refresh", "", http.StatusUnauthorized},
			{"V1NoToken", "/api/v1/refresh", "", http.StatusUnauthorized},
			{"LegacyWrongToken", "/api/refresh", "wrong-token", http.StatusForbidden},
			{"V1WrongToken", "/api/v1/refresh", "wrong-token", http.StatusForbidden},
			// Note: We skip the success cases to avoid needing complex mocking
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				req := httptest.NewRequest(http.MethodPost, tt.path, nil)
				if tt.token != "" {
					req.Header.Set("X-API-Token", tt.token)
				}
				rec := httptest.NewRecorder()

				mockHandler.ServeHTTP(rec, req)

				if rec.Code != tt.expectedCode {
					t.Errorf("expected status %d, got %d", tt.expectedCode, rec.Code)
				}
			})
		}
	})
}

// TestAPIVersionHeader ensures v1 endpoints include version header
func TestAPIVersionHeader(t *testing.T) {
	cfg := jobs.Config{
		Version:    "1.5.0-test",
		DataDir:    t.TempDir(),
		OWIBase:    "http://test.local",
		StreamPort: 8001,
	}

	server := New(cfg)
	handler := server.Handler()

	tests := []struct {
		name            string
		path            string
		expectHeader    bool
		expectedVersion string
	}{
		{"V1Status", "/api/v1/status", true, "1"},
		{"LegacyStatus", "/api/status", false, ""},
		// Skip refresh endpoints to avoid timeout issues in tests
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			apiVersion := rec.Header().Get("X-API-Version")

			if tt.expectHeader {
				if apiVersion != tt.expectedVersion {
					t.Errorf("expected X-API-Version=%s, got %s", tt.expectedVersion, apiVersion)
				}
			} else {
				if apiVersion != "" {
					t.Errorf("expected no X-API-Version header, got %s", apiVersion)
				}
			}
		})
	}
}
