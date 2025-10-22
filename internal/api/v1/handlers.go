// SPDX-License-Identifier: MIT
package v1

import (
	"encoding/json"
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
	Status   string    `json:"status"`
	Version  string    `json:"version"`
	LastRun  time.Time `json:"lastRun"`
	Channels int       `json:"channels"`
}

// HandleStatus implements GET /api/v1/status
// This endpoint returns the current status of the xg2g service
func (h *Handler) HandleStatus(w http.ResponseWriter, r *http.Request) {
	logger := log.WithComponentFromContext(r.Context(), "api.v1")

	status := h.server.GetStatus()

	resp := StatusResponse{
		Status:   "ok",
		Version:  status.Version,
		LastRun:  status.LastRun,
		Channels: status.Channels,
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
		Msg("v1 status request handled")
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
