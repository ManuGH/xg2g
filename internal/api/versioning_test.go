// SPDX-License-Identifier: MIT
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/jobs"
)

const headerValueTrue = "true"

func TestAPIVersioning(t *testing.T) {
	cfg := jobs.Config{
		Version:    "1.4.0-test",
		DataDir:    t.TempDir(),
		OWIBase:    "http://test.local",
		StreamPort: 8001,
	}

	server := New(cfg)
	handler := server.Handler()

	t.Run("V1StatusEndpoint", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rec.Code)
		}

		if apiVersion := rec.Header().Get("X-API-Version"); apiVersion != "1" {
			t.Errorf("expected X-API-Version: 1, got %s", apiVersion)
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}

		if resp["status"] != "ok" {
			t.Errorf("expected status ok, got %v", resp["status"])
		}

		if resp["version"] != "1.4.0-test" {
			t.Errorf("expected version 1.4.0-test, got %v", resp["version"])
		}
	})

	t.Run("LegacyStatusEndpoint", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rec.Code)
		}

		// Check for deprecation headers
		if deprecation := rec.Header().Get("Deprecation"); deprecation != headerValueTrue {
			t.Errorf("expected Deprecation header, got %s", deprecation)
		}

		if sunset := rec.Header().Get("Sunset"); sunset == "" {
			t.Error("expected Sunset header to be set")
		}

		if link := rec.Header().Get("Link"); link == "" {
			t.Error("expected Link header to be set")
		}

		if warning := rec.Header().Get("Warning"); warning == "" {
			t.Error("expected Warning header to be set")
		}
	})

	t.Run("LegacyAndV1ResponseCompatibility", func(t *testing.T) {
		// Legacy endpoint
		legacyReq := httptest.NewRequest(http.MethodGet, "/api/status", nil)
		legacyRec := httptest.NewRecorder()
		handler.ServeHTTP(legacyRec, legacyReq)

		// V1 endpoint
		v1Req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
		v1Rec := httptest.NewRecorder()
		handler.ServeHTTP(v1Rec, v1Req)

		var legacyResp, v1Resp map[string]interface{}
		if err := json.Unmarshal(legacyRec.Body.Bytes(), &legacyResp); err != nil {
			t.Fatalf("failed to parse legacy response: %v", err)
		}
		if err := json.Unmarshal(v1Rec.Body.Bytes(), &v1Resp); err != nil {
			t.Fatalf("failed to parse v1 response: %v", err)
		}

		// Compare response structures (ignoring deprecation headers)
		if legacyResp["status"] != v1Resp["status"] {
			t.Error("status fields don't match between legacy and v1")
		}
		if legacyResp["version"] != v1Resp["version"] {
			t.Error("version fields don't match between legacy and v1")
		}
		if legacyResp["channels"] != v1Resp["channels"] {
			t.Error("channels fields don't match between legacy and v1")
		}
	})

	t.Run("V2PreviewDisabledByDefault", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v2/status", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		// Should return 404 since v2 is not enabled by default
		if rec.Code != http.StatusNotFound {
			t.Errorf("expected status 404, got %d", rec.Code)
		}
	})
}

func TestDeprecationMiddleware(t *testing.T) {
	cfg := DeprecationConfig{
		SunsetVersion: "2.0.0",
		SunsetDate:    "2025-12-31T23:59:59Z",
		SuccessorPath: "/api/v1",
	}

	middleware := deprecationMiddleware(cfg)

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if deprecation := rec.Header().Get("Deprecation"); deprecation != headerValueTrue {
		t.Errorf("expected Deprecation: true, got %s", deprecation)
	}

	if sunset := rec.Header().Get("Sunset"); sunset != "2025-12-31T23:59:59Z" {
		t.Errorf("expected Sunset: 2025-12-31T23:59:59Z, got %s", sunset)
	}

	expectedLink := `</api/v1>; rel="successor-version"`
	if link := rec.Header().Get("Link"); link != expectedLink {
		t.Errorf("expected Link: %s, got %s", expectedLink, link)
	}

	warning := rec.Header().Get("Warning")
	if warning == "" {
		t.Error("expected Warning header to be set")
	}
	if warning != `299 - "This API is deprecated. Use /api/v1 instead. Will be removed in version 2.0.0"` {
		t.Errorf("unexpected warning message: %s", warning)
	}
}

func TestGetStatus(t *testing.T) {
	cfg := jobs.Config{
		Version:    "1.4.0-test",
		DataDir:    t.TempDir(),
		OWIBase:    "http://test.local",
		StreamPort: 8001,
	}

	server := New(cfg)

	// Set some test status
	server.mu.Lock()
	server.status = jobs.Status{
		Version:  "1.4.0-test",
		LastRun:  time.Now(),
		Channels: 42,
	}
	server.mu.Unlock()

	// Get status should return the same value
	status := server.GetStatus()

	if status.Version != "1.4.0-test" {
		t.Errorf("expected version 1.4.0-test, got %s", status.Version)
	}

	if status.Channels != 42 {
		t.Errorf("expected 42 channels, got %d", status.Channels)
	}
}

func TestFeatureEnabled(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		want     bool
	}{
		{"EnabledWithTrue", "true", true},
		{"EnabledWith1", "1", true},
		{"EnabledWithTRUE", "TRUE", true},
		{"DisabledWithFalse", "false", false},
		{"DisabledWith0", "0", false},
		{"DisabledWithEmpty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XG2G_FEATURE_TEST", tt.envValue)
			got := featureEnabled("TEST")
			if got != tt.want {
				t.Errorf("featureEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}
