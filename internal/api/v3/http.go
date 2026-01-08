// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"encoding/json"
	"net/http"

	"github.com/ManuGH/xg2g/internal/health"
	"github.com/ManuGH/xg2g/internal/log"
)

// GetSystemHealth implements ServerInterface
func (s *Server) GetSystemHealth(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	hm := s.healthManager
	s.mu.RUnlock()

	if hm == nil {
		// Should not happen in production if initialized correctly
		http.Error(w, "health manager not initialized", http.StatusServiceUnavailable)
		return
	}

	respH := hm.Health(r.Context(), true)

	status := SystemHealthStatusOk
	if respH.Status != health.StatusHealthy {
		status = SystemHealthStatusDegraded
	}

	// Map Receiver Status
	receiverStatus := ComponentStatusStatusOk
	if res, ok := respH.Checks["receiver_connection"]; ok {
		if res.Status != health.StatusHealthy {
			receiverStatus = ComponentStatusStatusError
		}
	} else {
		// Fallback if check missing (e.g. not configured)
		receiverStatus = ComponentStatusStatusError
	}

	// Map EPG Status
	// We check for "epg_status" checker
	epgStatus := EPGStatusStatusOk
	if res, ok := respH.Checks["epg_status"]; ok {
		if res.Status != health.StatusHealthy {
			epgStatus = EPGStatusStatusMissing
		}
	} else {
		epgStatus = EPGStatusStatusMissing
	}

	// Helper for safe dereference
	int64Ptr := func(i int64) *int64 { return &i }

	missing := 0
	// Try to parse missing from message? Or just keep it 0 as it's not in HealthResponse detail
	// The current HealthManager doesn't export "Calculated Missing Channels" directly in CheckResult
	// We could improve HealthManager later, for now we keep the simplified view.

	resp := SystemHealth{
		Status: &status,
		Receiver: &ComponentStatus{
			Status:    &receiverStatus,
			LastCheck: &respH.Timestamp,
		},
		Epg: &EPGStatus{
			Status:          &epgStatus,
			MissingChannels: &missing,
		},
		Version:       &respH.Version,
		UptimeSeconds: int64Ptr(respH.Uptime),
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// GetSystemHealthz implements ServerInterface (Simple liveness check)
func (s *Server) GetSystemHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "ok"}); err != nil {
		log.L().Error().Err(err).Msg("failed to encode system healthz response")
	}
}

// GetLogs implements ServerInterface
func (s *Server) GetLogs(w http.ResponseWriter, r *http.Request) {
	logs := log.GetRecentLogs()
	// Reverse to show most recent first
	for i, j := 0, len(logs)-1; i < j; i, j = i+1, j-1 {
		logs[i], logs[j] = logs[j], logs[i]
	}

	var resp []LogEntry
	for _, l := range logs {
		lvl := l.Level
		msg := l.Message
		t := l.Timestamp
		fields := make(map[string]interface{})
		for k, v := range l.Fields {
			fields[k] = v
		}

		resp = append(resp, LogEntry{
			Level:   &lvl,
			Message: &msg,
			Time:    &t,
			Fields:  &fields,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
