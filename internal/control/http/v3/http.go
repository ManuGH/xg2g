// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/ManuGH/xg2g/internal/control/read"
	"github.com/ManuGH/xg2g/internal/health"
)

// GetSystemHealth implements ServerInterface
func (s *Server) GetSystemHealth(w http.ResponseWriter, r *http.Request) {
	deps := s.systemModuleDeps()
	hm := deps.healthManager

	if hm == nil {
		writeProblem(w, r, http.StatusServiceUnavailable, "system/unavailable", "Subsystem Unavailable", "UNAVAILABLE", "Health manager not initialized", nil)
		return
	}

	// Use short timeout to prevent blocking the dashboard if receiver is offline
	// The health check upstream (receiver) can hang for 20s+ if unreachable.
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	info, err := read.GetHealthInfo(ctx, hm)
	if err != nil {
		writeProblem(w, r, http.StatusInternalServerError, "system/internal_error", "Health Read Error", "INTERNAL_ERROR", err.Error(), nil)
		return
	}

	// Map to generated types
	status := Ok
	if info.OverallStatus != string(health.StatusHealthy) {
		status = Degraded
	}

	receiverStatus := ComponentStatusStatusOk
	if info.ReceiverStatus != string(health.StatusHealthy) {
		receiverStatus = ComponentStatusStatusError
	}

	epgStatus := EPGStatusStatusOk
	if info.EpgStatus != string(health.StatusHealthy) {
		epgStatus = EPGStatusStatusMissing
	}

	resp := SystemHealth{
		Status: &status,
		Receiver: &ComponentStatus{
			Status:    &receiverStatus,
			LastCheck: &info.Timestamp,
		},
		Epg: &EPGStatus{
			Status:          &epgStatus,
			MissingChannels: &info.MissingChannels,
		},
		Version:       &info.Version,
		UptimeSeconds: &info.UptimeSeconds,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// GetSystemHealthz implements ServerInterface (Simple liveness check)
func (s *Server) GetSystemHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// GetLogs implements ServerInterface
func (s *Server) GetLogs(w http.ResponseWriter, r *http.Request) {
	deps := s.systemModuleDeps()
	ls := deps.logSource
	entries, err := read.GetRecentLogs(ls)
	if err != nil {
		writeProblem(w, r, http.StatusInternalServerError, "system/internal_error", "Log Read Error", "INTERNAL_ERROR", err.Error(), nil)
		return
	}

	// Reverse to show most recent first (Presentation Logic)
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}

	var resp []LogEntry
	for _, e := range entries {
		// Scoping for pointer safety in loop
		e := e
		resp = append(resp, LogEntry{
			Level:   &e.Level,
			Message: &e.Message,
			Time:    &e.Time,
			Fields:  &e.Fields,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
