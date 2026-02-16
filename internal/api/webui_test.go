package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/jobs"
	"github.com/ManuGH/xg2g/internal/openwebif"
)

func TestWebUIEndpoints(t *testing.T) {
	cfg := config.AppConfig{
		Version:     "v2.2.0",
		DataDir:     t.TempDir(),
		Bouquet:     "TestBouquet",
		OWIPassword: "secret_password",
		APIToken:    "secret_token",
	}
	s := New(cfg, nil)
	s.refreshFn = func(_ context.Context, _ config.AppConfig, _ *openwebif.StreamDetector) (*jobs.Status, error) {
		return &jobs.Status{
			Version:  "v2.2.0",
			LastRun:  time.Now(),
			Channels: 101,
			Bouquets: 1,
		}, nil
	}
	s.status = jobs.Status{
		Version:       "v2.2.0",
		LastRun:       time.Date(2025, 11, 28, 12, 0, 0, 0, time.UTC),
		Channels:      100,
		EPGProgrammes: 500,
	}
	// Manually set startTime to simulate uptime
	s.startTime = time.Now().Add(-1 * time.Hour)

	router := s.routes()
	handler := s.Handler()

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

	t.Run("POST /api/channels/toggle requires csrf", func(t *testing.T) {
		body := bytes.NewBufferString(`{"id":"chan-1","enabled":true}`)
		req := httptest.NewRequest(http.MethodPost, "/api/channels/toggle", body)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Errorf("Expected 403 Forbidden, got %d", w.Code)
		}
	})

	t.Run("POST /api/channels/toggle token without csrf is forbidden", func(t *testing.T) {
		body := bytes.NewBufferString(`{"id":"chan-1","enabled":true}`)
		req := httptest.NewRequest(http.MethodPost, "/api/channels/toggle", body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Token", "secret_token")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Errorf("Expected 403 Forbidden, got %d", w.Code)
		}
	})

	t.Run("POST /api/channels/toggle requires token", func(t *testing.T) {
		body := bytes.NewBufferString(`{"id":"chan-1","enabled":true}`)
		req := httptest.NewRequest(http.MethodPost, "/api/channels/toggle", body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://example.com")
		req.Host = "example.com"
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("Expected 401 Unauthorized, got %d", w.Code)
		}
	})

	t.Run("POST /api/channels/toggle accepts csrf+token", func(t *testing.T) {
		body := bytes.NewBufferString(`{"id":"chan-1","enabled":true}`)
		req := httptest.NewRequest(http.MethodPost, "/api/channels/toggle", body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://example.com")
		req.Host = "example.com"
		req.Header.Set("X-API-Token", "secret_token")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("Expected 200 OK, got %d", w.Code)
		}
	})

	t.Run("POST /api/channels/toggle invalid body returns sanitized json error", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/channels/toggle", bytes.NewBufferString("{invalid"))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://example.com")
		req.Host = "example.com"
		req.Header.Set("X-API-Token", "secret_token")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected 400, got %d", w.Code)
		}
		var body map[string]any
		if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
			t.Fatalf("expected JSON error response: %v", err)
		}
		if body["status"] != "error" {
			t.Fatalf("expected status=error, got %v", body["status"])
		}
		if _, ok := body["message"].(string); !ok {
			t.Fatalf("expected message string in error response")
		}
	})

	t.Run("POST /api/m3u/regenerate requires csrf", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/m3u/regenerate", nil)
		req.Header.Set("X-API-Token", "secret_token")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Errorf("Expected 403 Forbidden, got %d", w.Code)
		}
	})

	t.Run("POST /api/m3u/regenerate requires token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/m3u/regenerate", nil)
		req.Header.Set("Origin", "http://example.com")
		req.Host = "example.com"
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("Expected 401 Unauthorized, got %d", w.Code)
		}
	})

	t.Run("POST /api/m3u/regenerate accepts csrf+token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/m3u/regenerate", nil)
		req.Header.Set("Origin", "http://example.com")
		req.Host = "example.com"
		req.Header.Set("X-API-Token", "secret_token")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("Expected 200 OK, got %d", w.Code)
		}
	})

	t.Run("HEAD/OPTIONS do not trigger csrf rejection", func(t *testing.T) {
		headReq := httptest.NewRequest(http.MethodHead, "/api/health", nil)
		headW := httptest.NewRecorder()
		handler.ServeHTTP(headW, headReq)
		if headW.Code == http.StatusForbidden {
			t.Fatalf("HEAD /api/health should not be rejected by CSRF")
		}

		optionsReq := httptest.NewRequest(http.MethodOptions, "/api/channels/toggle", nil)
		optionsW := httptest.NewRecorder()
		handler.ServeHTTP(optionsW, optionsReq)
		if optionsW.Code == http.StatusForbidden {
			t.Fatalf("OPTIONS /api/channels/toggle should not be rejected by CSRF")
		}
	})

	t.Run("POST /api/v1/ui/refresh sanitizes internal errors", func(t *testing.T) {
		s.refreshFn = func(_ context.Context, _ config.AppConfig, _ *openwebif.StreamDetector) (*jobs.Status, error) {
			return nil, errors.New("dial tcp 10.1.2.3:5432: open /var/lib/xg2g/secrets/token")
		}
		req := httptest.NewRequest(http.MethodPost, "/api/v1/ui/refresh", nil)
		req.Header.Set("Origin", "http://example.com")
		req.Host = "example.com"
		req.Header.Set("X-API-Token", "secret_token")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Expected 500, got %d", w.Code)
		}
		body := w.Body.String()
		if strings.Contains(body, "10.1.2.3") || strings.Contains(body, "/var/lib/xg2g/secrets") {
			t.Fatalf("response leaked internal error details: %s", body)
		}
	})
}
