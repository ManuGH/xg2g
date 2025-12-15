package api

import (
	"encoding/json"
	"encoding/xml"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/ManuGH/xg2g/internal/epg"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/m3u"
	"github.com/ManuGH/xg2g/internal/openwebif"
)

// Ensure Server implements ServerInterface
var _ ServerInterface = (*Server)(nil)

type nowNextRequest struct {
	Services []string `json:"services"`
}

type epgEntry struct {
	Title string `json:"title,omitempty"`
	Start int64  `json:"start,omitempty"` // unix seconds
	End   int64  `json:"end,omitempty"`   // unix seconds
}

type nowNextItem struct {
	ServiceRef string    `json:"service_ref"`
	Now        *epgEntry `json:"now,omitempty"`
	Next       *epgEntry `json:"next,omitempty"`
}

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
	snap := s.snap
	s.mu.RUnlock()

	playlistName := snap.Runtime.PlaylistFilename
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
	snap := s.snap
	s.mu.RUnlock()

	playlistName := snap.Runtime.PlaylistFilename
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

		publicURL := snap.Runtime.PublicURL
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

// handleNowNextEPG returns now/next EPG for a list of service references.
func (s *Server) handleNowNextEPG(w http.ResponseWriter, r *http.Request) {
	var req nowNextRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Services) == 0 {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	s.mu.RLock()
	cfg := s.cfg
	snap := s.snap
	s.mu.RUnlock()

	enableHTTP2 := snap.Runtime.OpenWebIF.HTTPEnableHTTP2
	client := openwebif.NewWithPort(cfg.OWIBase, cfg.StreamPort, openwebif.Options{
		Timeout:         cfg.OWITimeout,
		Username:        cfg.OWIUsername,
		Password:        cfg.OWIPassword,
		UseWebIFStreams: cfg.UseWebIFStreams,
		StreamBaseURL:   snap.Runtime.OpenWebIF.StreamBaseURL,

		HTTPMaxIdleConns:        snap.Runtime.OpenWebIF.HTTPMaxIdleConns,
		HTTPMaxIdleConnsPerHost: snap.Runtime.OpenWebIF.HTTPMaxIdleConnsPerHost,
		HTTPMaxConnsPerHost:     snap.Runtime.OpenWebIF.HTTPMaxConnsPerHost,
		HTTPIdleTimeout:         snap.Runtime.OpenWebIF.HTTPIdleTimeout,
		HTTPEnableHTTP2:         &enableHTTP2,
	})

	now := time.Now()
	items := make([]nowNextItem, len(req.Services))

	var wg sync.WaitGroup
	sem := make(chan struct{}, 6) // limit concurrency

	for i, sref := range req.Services {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, ref string) {
			defer wg.Done()
			defer func() { <-sem }()

			events, err := client.GetEPG(r.Context(), ref, 1)
			if err != nil || len(events) == 0 {
				items[idx] = nowNextItem{ServiceRef: ref}
				return
			}

			var current *epgEntry
			var next *epgEntry

			for _, ev := range events {
				start := time.Unix(ev.Begin, 0)
				end := start.Add(time.Duration(ev.Duration) * time.Second)
				if ev.Duration <= 0 {
					end = start.Add(30 * time.Minute)
				}

				entry := epgEntry{
					Title: ev.Title,
					Start: start.Unix(),
					End:   end.Unix(),
				}

				if now.After(start) && now.Before(end) {
					current = &entry
				} else if start.After(now) {
					if next == nil || start.Before(time.Unix(next.Start, 0)) {
						next = &entry
					}
				}
			}

			items[idx] = nowNextItem{
				ServiceRef: ref,
				Now:        current,
				Next:       next,
			}
		}(i, sref)
	}

	wg.Wait()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"items": items,
	})
}

// handleEPGList serves EPG entries from the local XMLTV file for a time window.
// Query params: from (unix seconds, optional), to (unix seconds, optional), bouquet (optional)
func (s *Server) handleEPGList(w http.ResponseWriter, r *http.Request) {
	logger := log.WithComponentFromContext(r.Context(), "api")

	s.mu.RLock()
	cfg := s.cfg
	s.mu.RUnlock()

	if strings.TrimSpace(cfg.XMLTVPath) == "" {
		http.Error(w, "XMLTV not configured", http.StatusNotFound)
		return
	}

	xmltvPath, err := s.dataFilePath(cfg.XMLTVPath)
	if err != nil {
		http.Error(w, "XMLTV not available", http.StatusNotFound)
		return
	}

	xmltvData, err := os.ReadFile(xmltvPath) // #nosec G304
	if err != nil {
		logger.Error().Err(err).Msg("failed to read xmltv for epg list")
		http.Error(w, "XMLTV not available", http.StatusInternalServerError)
		return
	}

	var tv epg.TV
	if err := xml.Unmarshal(xmltvData, &tv); err != nil {
		logger.Error().Err(err).Msg("failed to parse xmltv for epg list")
		http.Error(w, "XMLTV parse error", http.StatusInternalServerError)
		return
	}

	fromParam := r.URL.Query().Get("from")
	toParam := r.URL.Query().Get("to")
	bouquetFilter := strings.TrimSpace(r.URL.Query().Get("bouquet"))
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	qLower := strings.ToLower(q)
	var fromTime time.Time
	var toTime time.Time
	now := time.Now()

	if fromParam == "" {
		fromTime = now.Add(-30 * time.Minute)
	} else {
		if sec, err := strconv.ParseInt(fromParam, 10, 64); err == nil {
			fromTime = time.Unix(sec, 0)
		} else {
			fromTime = now.Add(-30 * time.Minute)
		}
	}
	if toParam == "" {
		// default: show up to 7 days for non-search; search ignores window anyway
		toTime = now.Add(7 * 24 * time.Hour)
	} else {
		if sec, err := strconv.ParseInt(toParam, 10, 64); err == nil {
			toTime = time.Unix(sec, 0)
		} else {
			toTime = now.Add(7 * 24 * time.Hour)
		}
	}

	allowedRefs := make(map[string]struct{})
	if bouquetFilter != "" {
		s.mu.RLock()
		snap := s.snap
		s.mu.RUnlock()
		playlistName := snap.Runtime.PlaylistFilename
		playlistPath := filepath.Clean(filepath.Join(cfg.DataDir, playlistName))
		if data, err := os.ReadFile(playlistPath); err == nil { // #nosec G304
			channels := m3u.Parse(string(data))
			for _, ch := range channels {
				if ch.Group != bouquetFilter {
					continue
				}
				// Add tvg-id directly
				if ch.TvgID != "" {
					allowedRefs[ch.TvgID] = struct{}{}
				}
				ref := ""
				if ch.URL != "" {
					if parsed, err := url.Parse(ch.URL); err == nil {
						if q := parsed.Query().Get("ref"); q != "" {
							ref = q
						}
						if ref == "" {
							parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
							if len(parts) > 0 {
								ref = parts[len(parts)-1]
							}
						}
					} else {
						parts := strings.Split(ch.URL, "/")
						if len(parts) > 0 {
							ref = parts[len(parts)-1]
						}
					}
				}
				if ref != "" {
					allowedRefs[ref] = struct{}{}
				}
			}
		}
	}

	// If search requested and bouquet filter yields nothing, relax filter
	if qLower != "" && bouquetFilter != "" && len(allowedRefs) == 0 {
		allowedRefs = nil
	}

	// Filter programmes in window
	items := make([]map[string]any, 0, 512)
	for _, p := range tv.Programs {
		start, errStart := parseXMLTVTime(p.Start)
		end, errEnd := parseXMLTVTime(p.Stop)
		if errStart != nil || errEnd != nil {
			continue
		}
		if bouquetFilter != "" && len(allowedRefs) > 0 {
			if _, ok := allowedRefs[p.Channel]; !ok {
				continue
			}
		}

		// Search: ignore time window, match on title/description
		if qLower != "" {
			title := strings.ToLower(p.Title.Text)
			desc := strings.ToLower(p.Desc)
			if !strings.Contains(title, qLower) && !strings.Contains(desc, qLower) {
				continue
			}
			// Optional: also match channel name if encoded in XMLTV channel id (tvg-id)
			if !utf8.ValidString(p.Channel) {
				continue
			}
			if !strings.Contains(strings.ToLower(p.Channel), qLower) && !strings.Contains(title, qLower) && !strings.Contains(desc, qLower) {
				// already checked title/desc above; channel check is extra guard
			}
		} else {
			if end.Before(fromTime) || start.After(toTime) {
				continue
			}
		}

		items = append(items, map[string]any{
			"service_ref": p.Channel,
			"title":       p.Title.Text,
			"start":       start.Unix(),
			"end":         end.Unix(),
			"desc":        p.Desc,
		})
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i]["start"].(int64) < items[j]["start"].(int64)
	})

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"items": items,
	})
}

func parseXMLTVTime(value string) (time.Time, error) {
	layout := "20060102150405 -0700"
	return time.Parse(layout, strings.TrimSpace(value))
}
