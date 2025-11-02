// SPDX-License-Identifier: MIT

// Package v1 provides version 1 API handlers.
package v1

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/ManuGH/xg2g/internal/api"
	"github.com/ManuGH/xg2g/internal/log"
)

// Handler holds v1 API dependencies
type Handler struct {
	server *api.Server
}

// NewHandler creates a new v1 API handler
func NewHandler(srv *api.Server) *Handler {
	return &Handler{server: srv}
}

// StatusResponse defines the v1 status contract
// This structure is STABLE and must not change in backwards-incompatible ways
type StatusResponse struct {
	Status   string           `json:"status"`
	Version  string           `json:"version"`
	LastRun  time.Time        `json:"lastRun"`
	Channels int              `json:"channels"`
	Receiver *ReceiverStatus  `json:"receiver,omitempty"` // Optional receiver health check
}

// ReceiverStatus provides receiver connectivity information
type ReceiverStatus struct {
	Reachable    bool   `json:"reachable"`
	ResponseTime int64  `json:"responseTimeMs,omitempty"` // Response time in milliseconds
	Error        string `json:"error,omitempty"`          // Error message if unreachable
}

// HandleStatus implements GET /api/v1/status
// This endpoint returns the current status of the xg2g service
// Supports optional ?check_receiver=true query parameter for receiver health check
func (h *Handler) HandleStatus(w http.ResponseWriter, r *http.Request) {
	logger := log.WithComponentFromContext(r.Context(), "api.v1")

	status := h.server.GetStatus()

	resp := StatusResponse{
		Status:   "ok",
		Version:  status.Version,
		LastRun:  status.LastRun,
		Channels: status.Channels,
	}

	// Optional receiver health check
	if r.URL.Query().Get("check_receiver") == "true" {
		receiverStatus := h.checkReceiverHealth(r)
		resp.Receiver = receiverStatus

		// If receiver is unreachable, downgrade overall status to degraded
		if !receiverStatus.Reachable {
			resp.Status = "degraded"
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-API-Version", "1")

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		logger.Error().Err(err).Msg("failed to encode v1 status response")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	logger.Debug().
		Str("event", "v1.status.success").
		Str("version", status.Version).
		Time("lastRun", status.LastRun).
		Int("channels", status.Channels).
		Bool("receiver_checked", resp.Receiver != nil).
		Msg("v1 status request handled")
}

// checkReceiverHealth performs a lightweight health check on the OpenWebIF receiver
func (h *Handler) checkReceiverHealth(r *http.Request) *ReceiverStatus {
	logger := log.WithComponentFromContext(r.Context(), "api.v1")

	// Get server config for receiver connection details
	cfg := h.server.GetConfig()
	if cfg.OWIBase == "" {
		return &ReceiverStatus{
			Reachable: false,
			Error:     "receiver not configured",
		}
	}

	// Create short timeout context for health check (5 seconds max)
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	// Create a minimal HTTP client for health check
	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	start := time.Now()

	// Lightweight check: GET /api/statusinfo (minimal endpoint)
	checkURL := cfg.OWIBase + "/api/statusinfo"
	req, err := http.NewRequestWithContext(ctx, "GET", checkURL, nil)
	if err != nil {
		return &ReceiverStatus{
			Reachable: false,
			Error:     "failed to create request: " + err.Error(),
		}
	}

	// Add basic auth if configured
	if cfg.OWIUsername != "" && cfg.OWIPassword != "" {
		req.SetBasicAuth(cfg.OWIUsername, cfg.OWIPassword)
	}

	resp, err := client.Do(req)
	responseTime := time.Since(start).Milliseconds()

	if err != nil {
		logger.Debug().Err(err).Int64("responseMs", responseTime).Msg("receiver health check failed")
		return &ReceiverStatus{
			Reachable:    false,
			ResponseTime: responseTime,
			Error:        err.Error(),
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return &ReceiverStatus{
			Reachable:    false,
			ResponseTime: responseTime,
			Error:        fmt.Sprintf("unexpected status code: %d", resp.StatusCode),
		}
	}

	logger.Debug().Int64("responseMs", responseTime).Msg("receiver health check successful")
	return &ReceiverStatus{
		Reachable:    true,
		ResponseTime: responseTime,
	}
}

// RefreshResponse defines the v1 refresh response contract
type RefreshResponse struct {
	Version  string    `json:"version"`
	LastRun  time.Time `json:"lastRun"`
	Channels int       `json:"channels"`
}

// HandleRefresh implements POST /api/v1/refresh
// This endpoint triggers a manual refresh of the playlist and EPG data
// Requires authentication via X-API-Token header
func (h *Handler) HandleRefresh(w http.ResponseWriter, r *http.Request) {
	logger := log.WithComponentFromContext(r.Context(), "api.v1")

	// Delegate to the main server's refresh handler
	// We wrap the response to ensure v1 contract compliance
	h.server.HandleRefreshInternal(w, r)

	// Add v1 version header
	w.Header().Set("X-API-Version", "1")

	logger.Debug().
		Str("event", "v1.refresh.delegated").
		Msg("v1 refresh request delegated to main handler")
}
