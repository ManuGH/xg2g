// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/ManuGH/xg2g/internal/control/read"
	"github.com/ManuGH/xg2g/internal/health"
	"github.com/ManuGH/xg2g/internal/problemcode"
)

// GetErrors implements ServerInterface.
func (s *Server) GetErrors(w http.ResponseWriter, r *http.Request) {
	entries := problemcode.PublicEntries()
	items := make([]ErrorCatalogEntry, 0, len(entries))
	for _, entry := range entries {
		items = append(items, ErrorCatalogEntry{
			Code:         ProblemCode(entry.Code),
			Description:  entry.Description,
			OperatorHint: entry.OperatorHint,
			ProblemType:  entry.ProblemType,
			Retryable:    entry.Retryable,
			RunbookUrl:   stringPtrOrNil(entry.RunbookURL),
			Severity:     ErrorSeverity(entry.Severity),
			Title:        entry.DefaultTitle,
		})
	}

	writeJSON(w, http.StatusOK, ErrorCatalogResponse{Items: items})
}

func stringPtrOrNil(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// GetSystemHealth implements ServerInterface
func (s *Server) GetSystemHealth(w http.ResponseWriter, r *http.Request) {
	deps := s.systemModuleDeps()
	hm := deps.healthManager

	if hm == nil {
		writeRegisteredProblem(w, r, http.StatusServiceUnavailable, "system/unavailable", "Subsystem Unavailable", problemcode.CodeUnavailable, "Health manager not initialized", nil)
		return
	}

	// Use short timeout to prevent blocking the dashboard if receiver is offline
	// The health check upstream (receiver) can hang for 20s+ if unreachable.
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	info, err := read.GetHealthInfo(ctx, hm)
	if err != nil {
		writeRegisteredProblem(w, r, http.StatusInternalServerError, "system/internal_error", "Health Read Error", problemcode.CodeInternalError, err.Error(), nil)
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
	// EPG wall-clock display must follow the server/XMLTV timezone rather than UTC,
	// otherwise clients with a skewed or foreign device clock show the right events
	// with the wrong visible times.
	serverNow := time.Now()

	resp := SystemHealth{
		Status:     &status,
		ServerTime: &serverNow,
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
func (s *Server) GetLogs(w http.ResponseWriter, r *http.Request, params GetLogsParams) {
	deps := s.systemModuleDeps()
	ls := deps.logSource
	entries, err := read.GetRecentLogs(ls)
	if err != nil {
		writeRegisteredProblem(w, r, http.StatusInternalServerError, "system/internal_error", "Log Read Error", problemcode.CodeInternalError, err.Error(), nil)
		return
	}

	// Reverse to show most recent first (Presentation Logic)
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}

	if params.Limit != nil && *params.Limit > 0 && *params.Limit < len(entries) {
		entries = entries[:*params.Limit]
	} else if rawLimit := r.URL.Query().Get("limit"); rawLimit != "" {
		if limit, parseErr := strconv.Atoi(rawLimit); parseErr == nil && limit > 0 && limit < len(entries) {
			entries = entries[:limit]
		}
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
