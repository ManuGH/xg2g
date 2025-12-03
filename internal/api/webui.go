package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/log"
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
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.L().Error().Err(err).Msg("failed to encode health response")
	}
}

func (s *Server) handleAPIConfig(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	cfg := s.cfg
	s.mu.RUnlock()

	// Sanitize sensitive info
	cfg.OWIPassword = "***"
	cfg.APIToken = "***"

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(cfg); err != nil {
		log.L().Error().Err(err).Msg("failed to encode config response")
	}
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
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.L().Error().Err(err).Msg("failed to encode bouquets response")
	}
}

func (s *Server) handleAPIChannels(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	cfg := s.cfg
	s.mu.RUnlock()

	// Read M3U playlist
	playlistName := os.Getenv("XG2G_PLAYLIST_FILENAME")
	if strings.TrimSpace(playlistName) == "" {
		playlistName = "playlist.m3u"
	}
	path := filepath.Join(cfg.DataDir, playlistName)

	// #nosec G304 -- path is constructed from safe config
	data, err := os.ReadFile(path)
	if err != nil {
		// If file doesn't exist yet, return empty list instead of error
		if os.IsNotExist(err) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]interface{}{})
			return
		}
		log.L().Error().Err(err).Msg("failed to read playlist for channels API")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Simple M3U parser
	type Channel struct {
		Number string `json:"number"`
		Name   string `json:"name"`
		TvgID  string `json:"tvg_id"`
		Logo   string `json:"logo"`
		Group  string `json:"group"`
		URL    string `json:"url"`
		HasEPG bool   `json:"has_epg"`
	}

	var channels []Channel
	lines := strings.Split(string(data), "\n")
	var current Channel

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#EXTINF:") {
			// Parse EXTINF
			// #EXTINF:-1 tvg-id="..." tvg-name="..." tvg-logo="..." group-title="..." tvg-chno="...",Display Name
			current = Channel{}

			// Extract attributes
			if idx := strings.Index(line, `tvg-chno="`); idx != -1 {
				end := strings.Index(line[idx+10:], `"`)
				if end != -1 {
					current.Number = line[idx+10 : idx+10+end]
				}
			}
			if idx := strings.Index(line, `tvg-id="`); idx != -1 {
				end := strings.Index(line[idx+8:], `"`)
				if end != -1 {
					current.TvgID = line[idx+8 : idx+8+end]
				}
			}
			if idx := strings.Index(line, `tvg-logo="`); idx != -1 {
				end := strings.Index(line[idx+10:], `"`)
				if end != -1 {
					current.Logo = line[idx+10 : idx+10+end]
				}
			}
			if idx := strings.Index(line, `group-title="`); idx != -1 {
				end := strings.Index(line[idx+13:], `"`)
				if end != -1 {
					current.Group = line[idx+13 : idx+13+end]
				}
			}

			// Name is after the last comma
			if idx := strings.LastIndex(line, ","); idx != -1 {
				current.Name = strings.TrimSpace(line[idx+1:])
			}
		} else if len(line) > 0 && !strings.HasPrefix(line, "#") {
			// URL line
			current.URL = line
			// Check if we have EPG data for this channel (simple check based on TvgID presence)
			current.HasEPG = current.TvgID != ""
			channels = append(channels, current)
		}
	}

	// Filter by bouquet if requested
	requestedBouquet := r.URL.Query().Get("bouquet")
	if requestedBouquet != "" {
		var filtered []Channel
		for _, ch := range channels {
			if ch.Group == requestedBouquet {
				filtered = append(filtered, ch)
			}
		}
		channels = filtered
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(channels); err != nil {
		log.L().Error().Err(err).Msg("failed to encode channels response")
	}
}

func (s *Server) handleAPILogs(w http.ResponseWriter, r *http.Request) {
	logs := log.GetRecentLogs()

	// Reverse order (newest first)
	for i, j := 0, len(logs)-1; i < j; i, j = i+1, j-1 {
		logs[i], logs[j] = logs[j], logs[i]
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(logs); err != nil {
		log.L().Error().Err(err).Msg("failed to encode logs response")
	}
}
