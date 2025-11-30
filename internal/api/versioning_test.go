// SPDX-License-Identifier: MIT
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/jobs"
)

func TestAPIVersioning(t *testing.T) {
	cfg := config.AppConfig{
		Version:    "1.4.0-test",
		DataDir:    t.TempDir(),
		OWIBase:    "http://test.local",
		StreamPort: 8001,
	}

	server := New(cfg, nil)
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
}

func TestGetStatus(t *testing.T) {
	cfg := config.AppConfig{
		Version:    "1.4.0-test",
		DataDir:    t.TempDir(),
		OWIBase:    "http://test.local",
		StreamPort: 8001,
	}

	server := New(cfg, nil)

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
