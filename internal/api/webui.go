package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/m3u"
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

func writeUIError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":  "error",
		"message": message,
	})
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
	s.mu.RLock()
	cfg := s.cfg
	s.mu.RUnlock()

	// Read M3U playlist
	playlistName := os.Getenv("XG2G_PLAYLIST_FILENAME")
	if strings.TrimSpace(playlistName) == "" {
		playlistName = "playlist.m3u"
	}
	path := filepath.Join(cfg.DataDir, playlistName)

	var bouquets []string

	// Try to read and parse M3U
	if data, err := os.ReadFile(path); err == nil {
		channels := m3u.Parse(string(data))
		seen := make(map[string]bool)
		for _, ch := range channels {
			if ch.Group != "" && !seen[ch.Group] {
				bouquets = append(bouquets, ch.Group)
				seen[ch.Group] = true
			}
		}
	}

	// If no bouquets found in M3U (or file missing), fall back to config
	if len(bouquets) == 0 {
		configured := strings.Split(cfg.Bouquet, ",")
		for _, b := range configured {
			if trimmed := strings.TrimSpace(b); trimmed != "" {
				bouquets = append(bouquets, trimmed)
			}
		}
	}

	type BouquetEntry struct {
		Name     string `json:"name"`
		Services int    `json:"services"` // Unknown for now
	}

	var resp []BouquetEntry
	for _, b := range bouquets {
		resp = append(resp, BouquetEntry{Name: b, Services: 0})
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
		writeUIError(w, http.StatusInternalServerError, "Internal server error")
		return
	}

	// Parse channels using shared package
	channels := m3u.Parse(string(data))

	// Enrich with enabled status
	type ChannelWithStatus struct {
		m3u.Channel
		Enabled bool `json:"enabled"`
	}

	var enriched []ChannelWithStatus
	for _, ch := range channels {
		// Use TvgID as stable identifier, fallback to Name
		id := ch.TvgID
		if id == "" {
			id = ch.Name
		}

		enabled := true
		if s.channelManager != nil {
			enabled = s.channelManager.IsEnabled(id)
		}

		enriched = append(enriched, ChannelWithStatus{
			Channel: ch,
			Enabled: enabled,
		})
	}

	// Filter by bouquet if requested
	requestedBouquet := r.URL.Query().Get("bouquet")
	if requestedBouquet != "" {
		var filtered []ChannelWithStatus
		for _, ch := range enriched {
			if ch.Group == requestedBouquet {
				filtered = append(filtered, ch)
			}
		}
		enriched = filtered
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(enriched); err != nil {
		log.L().Error().Err(err).Msg("failed to encode channels response")
	}
}

func (s *Server) handleAPIToggleChannel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID      string `json:"id"`
		Enabled bool   `json:"enabled"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeUIError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.ID == "" {
		writeUIError(w, http.StatusBadRequest, "Channel ID required")
		return
	}

	if s.channelManager == nil {
		writeUIError(w, http.StatusInternalServerError, "Channel manager not initialized")
		return
	}

	if err := s.channelManager.SetEnabled(req.ID, req.Enabled); err != nil {
		log.L().Error().Err(err).Str("channel_id", req.ID).Msg("failed to toggle channel")
		writeUIError(w, http.StatusInternalServerError, "Failed to save channel state")
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleAPIToggleAllChannels(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled bool `json:"enabled"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeUIError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if s.channelManager == nil {
		writeUIError(w, http.StatusInternalServerError, "Channel manager not initialized")
		return
	}

	// We need to get all known channels to toggle them
	// Read M3U playlist
	playlistName := os.Getenv("XG2G_PLAYLIST_FILENAME")
	if strings.TrimSpace(playlistName) == "" {
		playlistName = "playlist.m3u"
	}
	path := filepath.Join(s.cfg.DataDir, playlistName)

	data, err := os.ReadFile(path)
	if err != nil {
		log.L().Error().Err(err).Msg("failed to read playlist for toggle all")
		writeUIError(w, http.StatusInternalServerError, "Internal server error")
		return
	}

	channels := m3u.Parse(string(data))
	for _, ch := range channels {
		id := ch.TvgID
		if id == "" {
			id = ch.Name
		}
		if err := s.channelManager.SetEnabled(id, req.Enabled); err != nil {
			log.L().Error().Err(err).Str("channel_id", id).Msg("failed to toggle channel")
		}
	}

	w.WriteHeader(http.StatusOK)
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
