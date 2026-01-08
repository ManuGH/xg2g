// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"encoding/json"
	"net/http"

	"github.com/ManuGH/xg2g/internal/control/read"
	"github.com/ManuGH/xg2g/internal/health"
)

// GetSystemHealth implements ServerInterface
func (s *Server) GetSystemHealth(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	hm := s.healthManager
	s.mu.RUnlock()

	if hm == nil {
		http.Error(w, "health manager not initialized", http.StatusServiceUnavailable)
		return
	}

	info, err := read.GetHealthInfo(r.Context(), hm)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Map to generated types
	status := SystemHealthStatusOk
	if info.OverallStatus != string(health.StatusHealthy) {
		status = SystemHealthStatusDegraded
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
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// GetLogs implements ServerInterface
func (s *Server) GetLogs(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	ls := s.logSource
	s.mu.RUnlock()
	entries, err := read.GetRecentLogs(ls)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
