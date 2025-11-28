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

func TestWebUIEndpoints(t *testing.T) {
	cfg := config.AppConfig{
		Version:     "v2.2.0",
		Bouquet:     "TestBouquet",
		OWIPassword: "secret_password",
		APIToken:    "secret_token",
	}
	s := New(cfg)
	s.status = jobs.Status{
		Version:       "v2.2.0",
		LastRun:       time.Date(2025, 11, 28, 12, 0, 0, 0, time.UTC),
		Channels:      100,
		EPGProgrammes: 500,
	}
	// Manually set startTime to simulate uptime
	s.startTime = time.Now().Add(-1 * time.Hour)

	router := s.routes()

	t.Run("GET /api/health", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/health", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected 200 OK, got %d", w.Code)
		}

		var resp HealthResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if resp.Status != "ok" {
			t.Errorf("Expected status ok, got %s", resp.Status)
		}
		if resp.Version != "v2.2.0" {
			t.Errorf("Expected version v2.2.0, got %s", resp.Version)
		}
		if resp.UptimeSeconds < 3500 {
			t.Errorf("Expected uptime > 3500, got %d", resp.UptimeSeconds)
		}
	})

	t.Run("GET /api/config", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/config", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected 200 OK, got %d", w.Code)
		}

		var resp config.AppConfig
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if resp.OWIPassword != "***" {
			t.Error("Password was not sanitized")
		}
		if resp.APIToken != "***" {
			t.Error("APIToken was not sanitized")
		}
	})

	t.Run("GET /api/bouquets", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/bouquets", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected 200 OK, got %d", w.Code)
		}

		var resp []struct {
			Name     string `json:"name"`
			Services int    `json:"services"`
		}
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if len(resp) != 1 || resp[0].Name != "TestBouquet" {
			t.Errorf("Unexpected bouquets: %v", resp)
		}
	})
}
