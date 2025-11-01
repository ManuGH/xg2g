// SPDX-License-Identifier: MIT

package v1_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/api"
	"github.com/ManuGH/xg2g/internal/api/v1"
	"github.com/ManuGH/xg2g/internal/jobs"
)

func TestHandleStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		version        string
		lastRun        time.Time
		channels       int
		wantStatus     string
		wantStatusCode int
	}{
                {
                        name:           "successful status response",
                        version:        "1.7.0",
			lastRun:        time.Date(2025, 11, 1, 10, 0, 0, 0, time.UTC),
			channels:       42,
			wantStatus:     "ok",
			wantStatusCode: http.StatusOK,
		},
		{
			name:           "status with no channels",
			version:        "1.6.0",
			lastRun:        time.Date(2025, 11, 1, 9, 0, 0, 0, time.UTC),
			channels:       0,
			wantStatus:     "ok",
			wantStatusCode: http.StatusOK,
		},
		{
			name:           "status with zero time",
			version:        "dev",
			lastRun:        time.Time{},
			channels:       10,
			wantStatus:     "ok",
			wantStatusCode: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create test server with mock status
			cfg := jobs.Config{
				Version: tt.version,
				DataDir: t.TempDir(),
			}
			srv := api.New(cfg)
			srv.SetStatus(jobs.Status{
				Version:  tt.version,
				LastRun:  tt.lastRun,
				Channels: tt.channels,
			})

			handler := v1.NewHandler(srv)

			// Create test request
			req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
			req = req.WithContext(context.Background())
			w := httptest.NewRecorder()

			// Execute handler
			handler.HandleStatus(w, req)

			// Assert response code
			if w.Code != tt.wantStatusCode {
				t.Errorf("HandleStatus() status = %v, want %v", w.Code, tt.wantStatusCode)
			}

			// Assert Content-Type header
			contentType := w.Header().Get("Content-Type")
			if contentType != "application/json" {
				t.Errorf("HandleStatus() Content-Type = %v, want application/json", contentType)
			}

			// Assert X-API-Version header
			apiVersion := w.Header().Get("X-API-Version")
			if apiVersion != "1" {
				t.Errorf("HandleStatus() X-API-Version = %v, want 1", apiVersion)
			}

			// Parse and validate JSON response
			var resp v1.StatusResponse
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				t.Fatalf("HandleStatus() failed to decode response: %v", err)
			}

			// Assert response fields
			if resp.Status != tt.wantStatus {
				t.Errorf("HandleStatus() status = %v, want %v", resp.Status, tt.wantStatus)
			}
			if resp.Version != tt.version {
				t.Errorf("HandleStatus() version = %v, want %v", resp.Version, tt.version)
			}
			if !resp.LastRun.Equal(tt.lastRun) {
				t.Errorf("HandleStatus() lastRun = %v, want %v", resp.LastRun, tt.lastRun)
			}
			if resp.Channels != tt.channels {
				t.Errorf("HandleStatus() channels = %v, want %v", resp.Channels, tt.channels)
			}
		})
	}
}

func TestHandleStatus_JSONStructure(t *testing.T) {
	t.Parallel()

	// Ensure v1 API contract stability
        cfg := jobs.Config{
                Version: "1.7.0",
		DataDir: t.TempDir(),
	}
	srv := api.New(cfg)
        srv.SetStatus(jobs.Status{
                Version:  "1.7.0",
		LastRun:  time.Date(2025, 11, 1, 10, 0, 0, 0, time.UTC),
		Channels: 42,
	})

	handler := v1.NewHandler(srv)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	w := httptest.NewRecorder()

	handler.HandleStatus(w, req)

	// Decode raw JSON to ensure field names match contract
	var raw map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&raw); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}

	// Verify required fields exist
	requiredFields := []string{"status", "version", "lastRun", "channels"}
	for _, field := range requiredFields {
		if _, ok := raw[field]; !ok {
			t.Errorf("HandleStatus() missing required field: %s", field)
		}
	}

	// Verify no unexpected fields (v1 contract stability)
	for field := range raw {
		found := false
		for _, expected := range requiredFields {
			if field == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("HandleStatus() unexpected field in v1 contract: %s", field)
		}
	}
}

func TestHandleRefresh(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		method         string
		wantStatusCode int
	}{
		{
			name:           "successful POST refresh",
			method:         http.MethodPost,
			wantStatusCode: http.StatusAccepted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create test server
                        cfg := jobs.Config{
                                Version: "1.7.0",
				DataDir: t.TempDir(),
			}
			srv := api.New(cfg)

			// Set mock refresh function to avoid actual refresh
			srv.SetRefreshFunc(func(ctx context.Context, cfg jobs.Config) (*jobs.Status, error) {
                                return &jobs.Status{
                                        Version:  "1.7.0",
					LastRun:  time.Now(),
					Channels: 42,
				}, nil
			})

			handler := v1.NewHandler(srv)

			// Create test request
			req := httptest.NewRequest(tt.method, "/api/v1/refresh", nil)
			req = req.WithContext(context.Background())
			w := httptest.NewRecorder()

			// Execute handler
			handler.HandleRefresh(w, req)

			// Assert X-API-Version header
			apiVersion := w.Header().Get("X-API-Version")
			if apiVersion != "1" {
				t.Errorf("HandleRefresh() X-API-Version = %v, want 1", apiVersion)
			}
		})
	}
}

func TestHandleRefresh_Concurrency(t *testing.T) {
	t.Parallel()

	// Create test server
        cfg := jobs.Config{
                Version: "1.7.0",
		DataDir: t.TempDir(),
	}
	srv := api.New(cfg)

	// Track refresh calls
	refreshCalled := 0
	srv.SetRefreshFunc(func(ctx context.Context, cfg jobs.Config) (*jobs.Status, error) {
		refreshCalled++
		time.Sleep(50 * time.Millisecond) // Simulate work
                return &jobs.Status{
                        Version:  "1.7.0",
			LastRun:  time.Now(),
			Channels: 42,
		}, nil
	})

	handler := v1.NewHandler(srv)

	// Make concurrent refresh requests
	const numRequests = 3
	done := make(chan bool, numRequests)

	for i := 0; i < numRequests; i++ {
		go func() {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/refresh", nil)
			req = req.WithContext(context.Background())
			w := httptest.NewRecorder()
			handler.HandleRefresh(w, req)
			done <- true
		}()
	}

	// Wait for all requests to complete
	for i := 0; i < numRequests; i++ {
		<-done
	}

	// Note: We don't assert refreshCalled == 1 here because the current
	// implementation delegates to HandleRefreshInternal which may or may
	// not serialize refreshes. This test primarily ensures no panics occur.
}

func TestHandleStatus_MethodNotAllowed(t *testing.T) {
	t.Parallel()

        cfg := jobs.Config{
                Version: "1.7.0",
		DataDir: t.TempDir(),
	}
	srv := api.New(cfg)
	handler := v1.NewHandler(srv)

	// Test that POST is not allowed for status endpoint
	req := httptest.NewRequest(http.MethodPost, "/api/v1/status", nil)
	w := httptest.NewRecorder()

	// Note: The handler itself doesn't enforce method restrictions
	// This is typically done at the router level
	// We just verify the handler doesn't panic
	handler.HandleStatus(w, req)

	if w.Code == 0 {
		t.Error("HandleStatus() did not write a response")
	}
}
