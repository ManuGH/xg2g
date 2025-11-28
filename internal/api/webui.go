package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// HealthResponse represents the overall system health
type HealthResponse struct {
	Status        string          `json:"status"`
	Receiver      ComponentStatus `json:"receiver"`
	EPG           EPGStatus       `json:"epg"`
	Version       string          `json:"version"`
	UptimeSeconds int64           `json:"uptime_seconds"`
}

type ComponentStatus struct {
	Status    string    `json:"status"`
	LastCheck time.Time `json:"last_check,omitempty"`
}

type EPGStatus struct {
	Status          string `json:"status"`
	MissingChannels int    `json:"missing_channels,omitempty"`
}

func (s *Server) handleAPIHealth(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	status := "ok"
	if s.status.Error != "" {
		status = "degraded"
	}

	receiverStatus := "ok"
	// TODO: Add real receiver check or use last refresh status
	if s.status.Error != "" {
		receiverStatus = "error"
	}

	epgStatus := "ok"
	if s.status.EPGProgrammes == 0 && s.status.Channels > 0 {
		epgStatus = "missing"
	}

	resp := HealthResponse{
		Status: status,
		Receiver: ComponentStatus{
			Status:    receiverStatus,
			LastCheck: s.status.LastRun,
		},
		EPG: EPGStatus{
			Status:          epgStatus,
			MissingChannels: 0, // TODO: Calculate this
		},
		Version:       s.status.Version,
		UptimeSeconds: int64(time.Since(s.startTime).Seconds()),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleAPIConfig(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	cfg := s.cfg
	s.mu.RUnlock()

	// Sanitize sensitive info
	cfg.OWIPassword = "***"
	cfg.APIToken = "***"

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cfg)
}

func (s *Server) handleAPIBouquets(w http.ResponseWriter, r *http.Request) {
	// For now, return configured bouquets
	// Ideally, we should fetch from receiver or cache
	s.mu.RLock()
	bouquets := strings.Split(s.cfg.Bouquet, ",")
	s.mu.RUnlock()

	type BouquetEntry struct {
		Name     string `json:"name"`
		Services int    `json:"services"` // Unknown for now
	}

	var resp []BouquetEntry
	for _, b := range bouquets {
		if trimmed := strings.TrimSpace(b); trimmed != "" {
			resp = append(resp, BouquetEntry{Name: trimmed, Services: 0})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleAPIChannels(w http.ResponseWriter, r *http.Request) {
	// This requires parsing the M3U or having an in-memory channel list
	// For Phase 1, we can return empty or implement M3U parsing
	// Since handleLineupJSON already parses M3U, we can extract that logic later.
	// For now, return empty list to satisfy the endpoint existence.
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode([]interface{}{})
}

func (s *Server) handleAPILogs(w http.ResponseWriter, r *http.Request) {
	// Phase 1: Empty logs
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode([]interface{}{})
}
