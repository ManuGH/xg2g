package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/m3u"
)

// Ensure Server implements ServerInterface
var _ ServerInterface = (*Server)(nil)

// GetSystemHealth implements ServerInterface
func (s *Server) GetSystemHealth(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	status := Ok
	if s.status.Error != "" {
		status = Degraded
	}

	receiverStatus := ComponentStatusStatusOk
	if s.status.Error != "" {
		receiverStatus = ComponentStatusStatusError
	}

	epgStatus := EPGStatusStatusOk
	if s.status.EPGProgrammes == 0 && s.status.Channels > 0 {
		epgStatus = EPGStatusStatusMissing
	}

	missing := 0
	uptime := int64(time.Since(s.startTime).Seconds())

	resp := SystemHealth{
		Status: &status,
		Receiver: &ComponentStatus{
			Status:    &receiverStatus,
			LastCheck: &s.status.LastRun,
		},
		Epg: &EPGStatus{
			Status:          &epgStatus,
			MissingChannels: &missing,
		},
		Version:       &s.status.Version,
		UptimeSeconds: &uptime,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// GetSystemConfig implements ServerInterface
func (s *Server) GetSystemConfig(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	cfg := s.cfg
	s.mu.RUnlock()

	// Sanitize sensitive info is done by returning a new struct mostly
	// But let's map it to the generated AppConfig

	epgSource := EPGConfigSource(cfg.EPGSource)
	resp := AppConfig{
		Version:  &cfg.Version,
		DataDir:  &cfg.DataDir,
		LogLevel: &cfg.LogLevel,
		Epg: &EPGConfig{
			Days:    &cfg.EPGDays,
			Enabled: &cfg.EPGEnabled,
			Source:  &epgSource,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// PutSystemConfig implements ServerInterface
func (s *Server) PutSystemConfig(w http.ResponseWriter, r *http.Request) {
	var req ConfigUpdate
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request format", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if req.OpenWebIF != nil {
		if req.OpenWebIF.BaseUrl != nil {
			s.cfg.OWIBase = *req.OpenWebIF.BaseUrl
		}
		if req.OpenWebIF.Username != nil {
			s.cfg.OWIUsername = *req.OpenWebIF.Username
		}
		if req.OpenWebIF.Password != nil {
			s.cfg.OWIPassword = *req.OpenWebIF.Password
		}
		if req.OpenWebIF.StreamPort != nil {
			s.cfg.StreamPort = *req.OpenWebIF.StreamPort
		}
	}

	if req.Bouquets != nil {
		// Join array to comma-separated string
		s.cfg.Bouquet = strings.Join(*req.Bouquets, ",")
	}

	if req.Epg != nil {
		if req.Epg.Enabled != nil {
			s.cfg.EPGEnabled = *req.Epg.Enabled
		}
		if req.Epg.Days != nil {
			s.cfg.EPGDays = *req.Epg.Days
		}
		if req.Epg.Source != nil {
			s.cfg.EPGSource = string(*req.Epg.Source)
		}
	}

	if req.Picons != nil {
		if req.Picons.BaseUrl != nil {
			s.cfg.PiconBase = *req.Picons.BaseUrl
		}
	}

	if req.FeatureFlags != nil {
		if req.FeatureFlags.InstantTune != nil {
			s.cfg.InstantTuneEnabled = *req.FeatureFlags.InstantTune
		}
	}

	// Persist to disk
	if err := s.configManager.Save(&s.cfg); err != nil {
		log.L().Error().Err(err).Msg("failed to save configuration")
		http.Error(w, "Failed to save configuration", http.StatusInternalServerError)
		return
	}

	// Determine if restart is required
	restartRequired := true // Conservative default

	respObj := struct {
		RestartRequired bool `json:"restart_required"`
	}{
		RestartRequired: restartRequired,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(respObj)
}

// PostSystemRefresh implements ServerInterface
func (s *Server) PostSystemRefresh(w http.ResponseWriter, r *http.Request) {
	s.handleRefresh(w, r)
}

// GetServicesBouquets implements ServerInterface
func (s *Server) GetServicesBouquets(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	cfg := s.cfg
	s.mu.RUnlock()

	playlistName := os.Getenv("XG2G_PLAYLIST_FILENAME")
	if strings.TrimSpace(playlistName) == "" {
		playlistName = "playlist.m3u"
	}
	path := filepath.Clean(filepath.Join(cfg.DataDir, playlistName))

	var bouquetNames []string

	if data, err := os.ReadFile(path); err == nil {
		channels := m3u.Parse(string(data))
		seen := make(map[string]bool)
		for _, ch := range channels {
			if ch.Group != "" && !seen[ch.Group] {
				bouquetNames = append(bouquetNames, ch.Group)
				seen[ch.Group] = true
			}
		}
	}

	if len(bouquetNames) == 0 {
		configured := strings.Split(cfg.Bouquet, ",")
		for _, b := range configured {
			if trimmed := strings.TrimSpace(b); trimmed != "" {
				bouquetNames = append(bouquetNames, trimmed)
			}
		}
	}
	sort.Strings(bouquetNames)

	var resp []Bouquet
	for _, b := range bouquetNames {
		name := b
		count := 0
		resp = append(resp, Bouquet{Name: &name, Services: &count})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// GetServices implements ServerInterface
func (s *Server) GetServices(w http.ResponseWriter, r *http.Request, params GetServicesParams) {
	s.mu.RLock()
	cfg := s.cfg
	s.mu.RUnlock()

	playlistName := os.Getenv("XG2G_PLAYLIST_FILENAME")
	if strings.TrimSpace(playlistName) == "" {
		playlistName = "playlist.m3u"
	}
	path := filepath.Clean(filepath.Join(cfg.DataDir, playlistName))

	data, err := os.ReadFile(path)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]Service{})
		return
	}

	channels := m3u.Parse(string(data))
	var resp []Service

	for _, ch := range channels {
		id := ch.TvgID
		if id == "" {
			id = ch.Name
		}

		if params.Bouquet != nil && ch.Group != *params.Bouquet {
			continue
		}

		enabled := true
		if s.channelManager != nil {
			enabled = s.channelManager.IsEnabled(id)
		}

		name := ch.Name
		group := ch.Group
		logo := ch.Logo

		publicURL := os.Getenv("XG2G_PUBLIC_URL")
		if publicURL != "" && strings.HasPrefix(logo, publicURL) {
			logo = strings.TrimPrefix(logo, publicURL)
		}
		number := ch.Number

		// Extract service_ref from URL for streaming
		serviceRef := ""
		if ch.URL != "" {
			// URL format is typically: http://receiver:8001/sref (where sref is the service reference)
			parts := strings.Split(ch.URL, "/")
			if len(parts) > 0 {
				serviceRef = parts[len(parts)-1]
			}
		}
		// Fallback to TvgID if service_ref not found
		if serviceRef == "" {
			serviceRef = id
		}

		resp = append(resp, Service{
			Id:         &id,
			Name:       &name,
			Group:      &group,
			LogoUrl:    &logo,
			Number:     &number,
			Enabled:    &enabled,
			ServiceRef: &serviceRef,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// PostServicesIdToggle implements ServerInterface
func (s *Server) PostServicesIdToggle(w http.ResponseWriter, r *http.Request, id string) {
	var req PostServicesIdToggleJSONBody
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if s.channelManager == nil {
		http.Error(w, "Channel manager not initialized", http.StatusInternalServerError)
		return
	}

	enabled := false
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	if err := s.channelManager.SetEnabled(id, enabled); err != nil {
		log.L().Error().Err(err).Str("channel_id", id).Msg("failed to toggle channel")
		http.Error(w, "Failed to save channel state", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// GetLogs implements ServerInterface
func (s *Server) GetLogs(w http.ResponseWriter, r *http.Request) {
	logs := log.GetRecentLogs()
	// Reverse
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

// GetStreams implements ServerInterface
func (s *Server) GetStreams(w http.ResponseWriter, r *http.Request) {
	// TODO: Integrate with Proxy/StreamManager to get real active streams
	// For now return empty list
	resp := []StreamSession{}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// DeleteStreamsId implements ServerInterface
func (s *Server) DeleteStreamsId(w http.ResponseWriter, r *http.Request, id string) {
	// TODO: Implement kill logic
	w.WriteHeader(http.StatusNotFound)
}
