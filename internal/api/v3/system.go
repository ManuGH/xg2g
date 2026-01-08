// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/read"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/epg"
	"github.com/ManuGH/xg2g/internal/health"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/m3u"
	"github.com/ManuGH/xg2g/internal/openwebif"
	cfgvalidate "github.com/ManuGH/xg2g/internal/validate"
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

// GetSystemConfig implements ServerInterface
func (s *Server) GetSystemConfig(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	cfg := s.cfg
	s.mu.RUnlock()

	info := read.GetConfigInfo(cfg)

	epgSource := EPGConfigSource(info.EPGSource)
	deliveryPolicy := StreamingConfigDeliveryPolicy(info.DeliveryPolicy)

	openWebIF := &OpenWebIFConfig{
		BaseUrl:    &info.Enigma2BaseURL,
		StreamPort: &info.Enigma2StreamPort,
	}
	if info.Enigma2Username != "" {
		openWebIF.Username = &info.Enigma2Username
	}

	resp := AppConfig{
		Version:   &info.Version,
		DataDir:   &info.DataDir,
		LogLevel:  &info.LogLevel,
		OpenWebIF: openWebIF,
		Bouquets:  &info.Bouquets,
		Epg: &EPGConfig{
			Days:    &info.EPGDays,
			Enabled: &info.EPGEnabled,
			Source:  &epgSource,
		},
		Picons: &PiconsConfig{
			BaseUrl: &info.PiconBase,
		},
		Streaming: &StreamingConfig{
			DeliveryPolicy: &deliveryPolicy,
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

	s.mu.RLock()
	current := s.cfg
	s.mu.RUnlock()

	next := current

	if req.OpenWebIF != nil {
		if req.OpenWebIF.BaseUrl != nil {
			val := *req.OpenWebIF.BaseUrl
			if val != "" && !strings.HasPrefix(val, "http://") && !strings.HasPrefix(val, "https://") {
				val = "http://" + val
			}
			next.Enigma2.BaseURL = val
		}
		if req.OpenWebIF.Username != nil {
			next.Enigma2.Username = *req.OpenWebIF.Username
		}
		if req.OpenWebIF.Password != nil {
			next.Enigma2.Password = *req.OpenWebIF.Password
		}
		if req.OpenWebIF.StreamPort != nil {
			next.Enigma2.StreamPort = *req.OpenWebIF.StreamPort
		}
	}

	if req.Bouquets != nil {
		// Join array to comma-separated string
		next.Bouquet = strings.Join(*req.Bouquets, ",")
	}

	if req.Epg != nil {
		if req.Epg.Enabled != nil {
			next.EPGEnabled = *req.Epg.Enabled
		}
		if req.Epg.Days != nil {
			next.EPGDays = *req.Epg.Days
		}
		if req.Epg.Source != nil {
			next.EPGSource = string(*req.Epg.Source)
		}
	}

	if req.Picons != nil {
		if req.Picons.BaseUrl != nil {
			next.PiconBase = *req.Picons.BaseUrl
		}
	}

	if err := config.Validate(next); err != nil {
		respondConfigValidationError(w, r, err)
		return
	}
	if err := health.PerformStartupChecks(r.Context(), next); err != nil {
		respondConfigValidationError(w, r, err)
		return
	}

	// Persist to disk
	if err := s.configManager.Save(&next); err != nil {
		log.L().Error().Err(err).Msg("failed to save configuration")
		http.Error(w, "Failed to save configuration", http.StatusInternalServerError)
		return
	}
	s.mu.Lock()
	s.cfg = next
	s.mu.Unlock()

	// Determine if restart is required
	restartRequired := true // Conservative default

	respObj := struct {
		RestartRequired bool `json:"restart_required"`
	}{
		RestartRequired: restartRequired,
	}

	status := http.StatusOK
	if restartRequired {
		status = http.StatusAccepted
	}
	writeJSON(w, status, respObj)

	if restartRequired {
		go func() {
			time.Sleep(100 * time.Millisecond) // Allow response to flush
			log.L().Info().Msg("configuration updated, triggering graceful shutdown")
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := s.requestShutdown(ctx); err != nil {
				log.L().Error().Err(err).Msg("graceful shutdown request failed")
			}
		}()
	}
}

type configValidationIssue struct {
	Field   string `json:"field"`
	Message string `json:"message"`
	Value   any    `json:"value,omitempty"`
}

func respondConfigValidationError(w http.ResponseWriter, r *http.Request, err error) {
	var details []configValidationIssue

	var vErr cfgvalidate.ValidationError
	if errors.As(err, &vErr) {
		for _, item := range vErr.Errors() {
			details = append(details, configValidationIssue{
				Field:   item.Field,
				Message: item.Message,
				Value:   item.Value,
			})
		}
	} else {
		details = append(details, configValidationIssue{
			Field:   "preflight",
			Message: err.Error(),
		})
	}

	RespondError(w, r, http.StatusBadRequest, ErrInvalidInput, details)
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

	bouquets, err := read.GetBouquets(cfg, snap)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(bouquets)
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
			if u, err := url.Parse(ch.URL); err == nil {
				// Check query params first (e.g. stream.m3u?ref=...)
				if ref := u.Query().Get("ref"); ref != "" {
					serviceRef = ref
				} else {
					// Fallback to path logic
					parts := strings.Split(u.Path, "/")
					if len(parts) > 0 {
						serviceRef = parts[len(parts)-1]
					}
				}
			} else {
				// Fallback for non-parseable URLs
				parts := strings.Split(ch.URL, "/")
				if len(parts) > 0 {
					serviceRef = parts[len(parts)-1]
				}
			}
		}
		// Fallback to TvgID if service_ref not found
		if serviceRef == "" {
			serviceRef = id
		}

		// Rewrite Logo to use local proxy (avoids mixed content & external reachability issues)
		if serviceRef != "" {
			piconRef := strings.ReplaceAll(serviceRef, ":", "_")
			piconRef = strings.TrimSuffix(piconRef, "_")
			// Use relative path so frontend resolves to correct host
			logo = fmt.Sprintf("/logos/%s.png", piconRef)
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

// GetStreams implements ServerInterface
func (s *Server) GetStreams(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	store := s.v3Store
	cfg := s.cfg
	snap := s.snap
	epgCache := s.epgCache
	s.mu.RUnlock()

	if store == nil {
		http.Error(w, "v3 store not initialized", http.StatusServiceUnavailable)
		return
	}

	sessions, err := store.ListSessions(r.Context())
	if err != nil {
		http.Error(w, "failed to list sessions", http.StatusInternalServerError)
		return
	}

	if len(sessions) == 0 {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]StreamSession{})
		return
	}

	now := time.Now()
	idleTimeout := cfg.Engine.IdleTimeout
	channelNames := make(map[string]string)

	if epgCache != nil {
		for _, ch := range epgCache.Channels {
			if ch.ID == "" || len(ch.DisplayName) == 0 {
				continue
			}
			if _, ok := channelNames[ch.ID]; !ok {
				channelNames[ch.ID] = ch.DisplayName[0]
			}
		}
	}

	activeSessions := make([]*model.SessionRecord, 0, len(sessions))
	missingRefs := make(map[string]struct{})

	for _, sess := range sessions {
		if sess == nil {
			continue
		}
		if sess.State.IsTerminal() {
			continue
		}

		lastAccess := sess.LastAccessUnix
		if lastAccess == 0 {
			lastAccess = sess.UpdatedAtUnix
		}
		if lastAccess == 0 {
			lastAccess = sess.CreatedAtUnix
		}
		if idleTimeout > 0 && lastAccess > 0 {
			if now.Sub(time.Unix(lastAccess, 0)) > idleTimeout {
				continue
			}
		}

		activeSessions = append(activeSessions, sess)
		if sess.ServiceRef != "" {
			if _, ok := channelNames[sess.ServiceRef]; !ok {
				missingRefs[sess.ServiceRef] = struct{}{}
			}
		}
	}

	if len(activeSessions) == 0 {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]StreamSession{})
		return
	}

	if len(missingRefs) > 0 {
		for ref, name := range mapPlaylistNames(cfg, snap, missingRefs) {
			if _, ok := channelNames[ref]; !ok && name != "" {
				channelNames[ref] = name
			}
		}
	}

	resp := make([]StreamSession, 0, len(activeSessions))
	for _, sess := range activeSessions {
		if sess == nil {
			continue
		}
		id := sess.SessionID
		if id == "" {
			continue
		}

		var clientIP *string
		if sess.ContextData != nil {
			if ip := sess.ContextData["client_ip"]; ip != "" {
				clientIP = &ip
			}
		}

		var channelName *string
		if sess.ServiceRef != "" {
			name := channelNames[sess.ServiceRef]
			if name == "" {
				name = sess.ServiceRef
			}
			if name != "" {
				channelName = &name
			}
		}

		var startedAt *time.Time
		if sess.CreatedAtUnix > 0 {
			t := time.Unix(sess.CreatedAtUnix, 0)
			startedAt = &t
		}

		state := Active
		resp = append(resp, StreamSession{
			Id:          &id,
			ClientIp:    clientIP,
			ChannelName: channelName,
			StartedAt:   startedAt,
			State:       &state,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// DeleteStreamsId implements ServerInterface
func (s *Server) DeleteStreamsId(w http.ResponseWriter, r *http.Request, id string) {
	if id == "" || !model.IsSafeSessionID(id) {
		http.Error(w, "invalid session id", http.StatusBadRequest)
		return
	}

	s.mu.RLock()
	bus := s.v3Bus
	store := s.v3Store
	s.mu.RUnlock()

	if bus == nil || store == nil {
		http.Error(w, "v3 control plane not enabled", http.StatusServiceUnavailable)
		return
	}

	session, err := store.GetSession(r.Context(), id)
	if err != nil {
		http.Error(w, "failed to load session", http.StatusInternalServerError)
		return
	}
	if session == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	event := model.StopSessionEvent{
		Type:          model.EventStopSession,
		SessionID:     id,
		Reason:        model.RClientStop,
		CorrelationID: session.CorrelationID,
		RequestedAtUN: time.Now().Unix(),
	}
	if err := bus.Publish(r.Context(), string(model.EventStopSession), event); err != nil {
		log.L().Error().Err(err).Str("session", id).Msg("failed to publish stop event")
		http.Error(w, "failed to stop session", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func mapPlaylistNames(cfg config.AppConfig, snap config.Snapshot, wanted map[string]struct{}) map[string]string {
	if len(wanted) == 0 || snap.Runtime.PlaylistFilename == "" {
		return nil
	}

	path := filepath.Clean(filepath.Join(cfg.DataDir, snap.Runtime.PlaylistFilename))
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	channels := m3u.Parse(string(data))
	names := make(map[string]string, len(wanted))

	for _, ch := range channels {
		if len(names) == len(wanted) {
			break
		}

		id := ch.TvgID
		if id == "" {
			id = ch.Name
		}

		serviceRef := ""
		if ch.URL != "" {
			if u, err := url.Parse(ch.URL); err == nil {
				if ref := u.Query().Get("ref"); ref != "" {
					serviceRef = ref
				} else {
					parts := strings.Split(u.Path, "/")
					if len(parts) > 0 {
						serviceRef = parts[len(parts)-1]
					}
				}
			} else {
				parts := strings.Split(ch.URL, "/")
				if len(parts) > 0 {
					serviceRef = parts[len(parts)-1]
				}
			}
		}

		if serviceRef == "" {
			serviceRef = id
		}
		if serviceRef == "" {
			continue
		}
		if _, ok := wanted[serviceRef]; !ok {
			continue
		}

		name := ch.Name
		if name == "" {
			name = id
		}
		if name == "" {
			continue
		}

		names[serviceRef] = name
	}

	return names
}

// handleNowNextEPG returns now/next EPG for a list of service references.
func (s *Server) handleNowNextEPG(w http.ResponseWriter, r *http.Request) {
	var req nowNextRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Services) == 0 {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	s.mu.RLock()
	epgCache := s.epgCache
	s.mu.RUnlock()

	if epgCache == nil {
		items := make([]nowNextItem, len(req.Services))
		for i, sref := range req.Services {
			items[i] = nowNextItem{ServiceRef: sref}
		}
		writeNowNextResponse(w, items)
		return
	}

	// Phase 9-5: Pre-index programs by channel for faster lookup
	progMap := make(map[string][]epg.Programme)
	for _, p := range epgCache.Programs {
		progMap[p.Channel] = append(progMap[p.Channel], p)
	}

	now := time.Now()
	xmltvFormat := "20060102150405 -0700"
	items := make([]nowNextItem, 0, len(req.Services))

	for _, sref := range req.Services {
		progs, ok := progMap[sref]
		if !ok {
			items = append(items, nowNextItem{ServiceRef: sref})
			continue
		}

		var current *epgEntry
		var next *epgEntry

		for _, p := range progs {
			start, serr := time.Parse(xmltvFormat, p.Start)
			stop, perr := time.Parse(xmltvFormat, p.Stop)
			if serr != nil || perr != nil {
				continue
			}

			entry := &epgEntry{
				Title: p.Title.Text,
				Start: start.Unix(),
				End:   stop.Unix(),
			}

			if now.After(start) && now.Before(stop) {
				current = entry
			} else if start.After(now) {
				if next == nil || start.Before(time.Unix(next.Start, 0)) {
					next = entry
				}
			}
		}

		items = append(items, nowNextItem{
			ServiceRef: sref,
			Now:        current,
			Next:       next,
		})
	}

	writeNowNextResponse(w, items)
}

func writeNowNextResponse(w http.ResponseWriter, items []nowNextItem) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"items": items,
	})
}

// GetDvrStatus implements ServerInterface
func (s *Server) GetDvrStatus(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	ds := s.dvrSource
	s.mu.RUnlock()

	st, err := read.GetDvrStatus(r.Context(), ds)

	if err != nil {
		log.L().Warn().Err(err).Msg("failed to get dvr status")
		http.Error(w, "Failed to get status", http.StatusBadGateway)
		return
	}

	resp := RecordingStatus{
		IsRecording: st.IsRecording,
		ServiceName: &st.ServiceName,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// EpgItem defines the JSON response structure for an EPG program
type EpgItem struct {
	Id         *string `json:"id,omitempty"`
	ServiceRef string  `json:"service_ref,omitempty"`
	Title      string  `json:"title,omitempty"`
	Desc       *string `json:"desc,omitempty"`
	Start      int     `json:"start,omitempty"`
	End        int     `json:"end,omitempty"`
	Duration   *int    `json:"duration,omitempty"`
}

// Helper to parse XMLTV dates "20080715003000 +0200"
func parseXMLTVTime(s string) (time.Time, error) {
	// Format: YYYYMMDDhhmmss ZZZZ
	const layout = "20060102150405 -0700"
	return time.Parse(layout, s)
}

// GetEpg implements ServerInterface
func (s *Server) GetEpg(w http.ResponseWriter, r *http.Request, params GetEpgParams) {
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

	// 1. Singleflight for Concurrency Protection
	result, err, shared := s.epgSfg.Do("epg-load", func() (interface{}, error) {
		fileInfo, err := os.Stat(xmltvPath)
		if err != nil {
			return nil, err
		}

		s.mu.Lock()
		if s.epgCache != nil && !fileInfo.ModTime().After(s.epgCacheMTime) {
			defer s.mu.Unlock()
			return s.epgCache, nil
		}
		s.mu.Unlock()

		// Parse
		logger.Info().Str("path", xmltvPath).Msg("EPG cache miss/stale, parsing xmltv...")
		xmltvData, err := os.ReadFile(xmltvPath) // #nosec G304
		if err != nil {
			return nil, err
		}

		var parsedTU epg.TV
		if err := xml.Unmarshal(xmltvData, &parsedTU); err != nil {
			s.mu.RLock()
			stale := s.epgCache
			s.mu.RUnlock()
			if stale != nil {
				logger.Warn().Err(err).Msg("EPG parse failed, serving STALE cache")
				return stale, nil
			}
			return nil, err
		}

		// Update Cache
		s.mu.Lock()
		s.epgCache = &parsedTU
		s.epgCacheMTime = fileInfo.ModTime()
		s.epgCacheTime = time.Now()
		tvVal := s.epgCache
		s.mu.Unlock()

		return tvVal, nil
	})

	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "XMLTV not available", http.StatusNotFound)
			return
		}
		logger.Error().Err(err).Msg("failed to load EPG")
		http.Error(w, "EPG Load Error", http.StatusInternalServerError)
		return
	}

	if shared {
		logger.Debug().Msg("served EPG from shared singleflight")
	}

	tv := result.(*epg.TV)

	// Extract parameters
	var fromTime, toTime time.Time
	now := time.Now()

	if params.From != nil {
		fromTime = time.Unix(int64(*params.From), 0)
	} else {
		fromTime = now.Add(-30 * time.Minute)
	}

	if params.To != nil {
		toTime = time.Unix(int64(*params.To), 0)
	} else {
		toTime = now.Add(7 * 24 * time.Hour)
	}

	// Requirement: Max 7 days server-side
	maxEnd := now.Add(7 * 24 * time.Hour)
	if toTime.After(maxEnd) {
		toTime = maxEnd
	}

	bouquetFilter := ""
	if params.Bouquet != nil {
		bouquetFilter = strings.TrimSpace(*params.Bouquet)
	}

	qLower := ""
	if params.Q != nil {
		qLower = strings.ToLower(strings.TrimSpace(*params.Q))
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
				if ch.TvgID != "" {
					allowedRefs[ch.TvgID] = struct{}{}
				}
			}
		}
	}

	// If search requested and bouquet filter yields nothing, relax filter
	if qLower != "" && bouquetFilter != "" && len(allowedRefs) == 0 {
		allowedRefs = nil
	}

	var items []EpgItem
	for _, p := range tv.Programs {
		if bouquetFilter != "" {
			_, ok1 := allowedRefs[p.Channel]
			if !ok1 {
				continue
			}
		}

		startTime, errStart := parseXMLTVTime(p.Start)
		endTime, errEnd := parseXMLTVTime(p.Stop)
		if errStart != nil || errEnd != nil {
			continue
		}

		if !startTime.Before(toTime) || !endTime.After(fromTime) {
			continue
		}

		if qLower != "" {
			match := false
			if strings.Contains(strings.ToLower(p.Title.Text), qLower) {
				match = true
			} else if strings.Contains(strings.ToLower(p.Desc), qLower) {
				match = true
			}
			if !match {
				continue
			}
		} else {
			if endTime.Before(fromTime) || startTime.After(toTime) {
				continue
			}
		}

		startUnix := startTime.Unix()
		endUnix := endTime.Unix()
		dur := int(endUnix - startUnix)
		if dur < 0 {
			dur = 0
		}

		// Unescape HTML entities and replace literal \n with real newlines
		desc := html.UnescapeString(p.Desc)
		desc = strings.ReplaceAll(desc, "\\n", "\n")
		pID := p.Channel

		items = append(items, EpgItem{
			Id:         &pID,
			ServiceRef: p.Channel,
			Title:      html.UnescapeString(p.Title.Text),
			Desc:       &desc,
			Start:      int(startUnix),
			End:        int(endUnix),
			Duration:   &dur,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"items": items,
	})
}

// GetTimers implements ServerInterface
func (s *Server) GetTimers(w http.ResponseWriter, r *http.Request, params GetTimersParams) {
	s.mu.RLock()
	cfg := s.cfg
	snap := s.snap
	s.mu.RUnlock()

	client := s.newOpenWebIFClient(cfg, snap)
	timers, err := client.GetTimers(r.Context())
	if err != nil {
		log.L().Error().Err(err).Msg("failed to get timers")
		http.Error(w, "Failed to fetch timers", http.StatusBadGateway)
		return
	}

	// Filter by state?
	// params.State (default "scheduled")
	// OpenWebIF returns all usually. We might need to filter manually if OWI doesn't support state param.
	// For now, return all mapped to our Timer struct.

	count := len(timers)
	mapped := make([]Timer, 0, count)

	for _, t := range timers {
		// Map OWI Timer to API Timer
		// state: 0=waiting, 2=running, 3=finished (example, verify OWI codes)
		// We map to "scheduled", "recording", "completed", "disabled"

		stateStr := TimerStateUnknown
		switch t.State {
		case 0:
			stateStr = TimerStateScheduled
			if t.Disabled != 0 {
				stateStr = TimerStateDisabled
			}
		case 2:
			stateStr = TimerStateRecording
		case 3:
			stateStr = TimerStateCompleted
		default:
			if t.Disabled != 0 {
				stateStr = TimerStateDisabled
			}
		}

		// Filter if requested
		if params.State != nil && string(*params.State) != "all" {
			if string(stateStr) != *params.State {
				continue
			}
		}

		timerId := MakeTimerID(t.ServiceRef, t.Begin, t.End)

		mapped = append(mapped, Timer{
			TimerId:     timerId,
			ServiceRef:  t.ServiceRef,
			ServiceName: &t.ServiceName,
			Name:        t.Name,
			Description: &t.Description,
			Begin:       t.Begin,
			End:         t.End,
			State:       stateStr,
			// ReceiverState: raw map?
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(TimerList{Items: mapped})
}

// AddTimer implements ServerInterface
func (s *Server) AddTimer(w http.ResponseWriter, r *http.Request) {
	var req TimerCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Begin >= req.End {
		http.Error(w, "Begin time must be before End time", http.StatusUnprocessableEntity) // 422
		return
	}

	s.mu.RLock()
	cfg := s.cfg
	snap := s.snap
	s.mu.RUnlock()

	client := s.newOpenWebIFClient(cfg, snap)
	ctx := r.Context()

	// 0. Resolve ServiceRef if it's an M3U Channel ID
	realSRef := req.ServiceRef
	if !strings.Contains(realSRef, ":") {
		// Doesn't look like Enigma2 Ref (1:0:1...), try to resolve from Playlist
		playlistPath := filepath.Clean(filepath.Join(cfg.DataDir, snap.Runtime.PlaylistFilename))
		log.L().Info().Str("path", playlistPath).Str("search_id", req.ServiceRef).Msg("attempting to resolve service ref from playlist")

		if data, err := os.ReadFile(playlistPath); err == nil { // #nosec G304
			channels := m3u.Parse(string(data))
			log.L().Info().Int("channels", len(channels)).Msg("parsed playlist for resolution")

			for _, ch := range channels {
				if ch.TvgID == req.ServiceRef {
					// Found it! Extract Ref from URL
					u, err := url.Parse(ch.URL)
					if err == nil {
						// OpenWebIF stream URL analysis
						// URL can be like: http://host/web/stream.m3u?ref=1:0:1...
						// OR like: http://host:8001/1:0:1...

						// Check for 'ref' query parameter first (OpenWebIF 80 port style)
						ref := u.Query().Get("ref")
						if ref != "" {
							// Found in query, use it
							if strings.Contains(ref, ":") {
								realSRef = strings.TrimSuffix(ref, ":")
								log.L().Info().Str("id", req.ServiceRef).Str("resolved", realSRef).Msg("resolved channel id to service ref (via query)")
							}
						} else {
							// Check path (OpenWebIF 8001 port style)
							parts := strings.Split(u.Path, "/")
							if len(parts) > 0 {
								candidate := parts[len(parts)-1]
								if strings.Contains(candidate, ":") {
									realSRef = strings.TrimSuffix(candidate, ":")
									log.L().Info().Str("id", req.ServiceRef).Str("resolved", realSRef).Msg("resolved channel id to service ref (via path)")
								}
							}
						}
					} else {
						log.L().Warn().Err(err).Str("url", ch.URL).Msg("failed to parse channel url during resolution")
					}
					break
				}
			}
		} else {
			log.L().Warn().Err(err).Str("path", playlistPath).Msg("failed to read playlist for resolution")
		}
	}

	// 1. Duplicate/Conflict Check
	existingTimers, err := client.GetTimers(ctx)
	if err != nil {
		log.L().Error().Err(err).Msg("failed to fetch timers for duplicate check")
		http.Error(w, "Failed to verify existing timers", http.StatusBadGateway)
		return
	}

	for _, t := range existingTimers {
		// Strict Duplicate: Same ServiceRef + Exact Begin/End
		if t.ServiceRef == realSRef && t.Begin == req.Begin && t.End == req.End {
			// 409 Conflict (Duplicate)
			writeProblem(w, http.StatusConflict, "dvr/duplicate", "Timer already exists", nil)
			return
		}
	}

	// Apply Padding? Request has PaddingBeforeSec/AfterSec.
	// If OWI supports padding natively, pass it? `AddTimer` in client assumes pre-calculated times?
	// Actually `TimerCreateRequest` has `Begin`/`End`. Padding is optional metadata or applied?
	// User checklist: "Apply padding on server side consistently".
	// begin = begin - paddingBefore
	// end = end + paddingAfter

	realBegin := req.Begin
	if req.PaddingBeforeSec != nil {
		realBegin -= int64(*req.PaddingBeforeSec)
	}
	realEnd := req.End
	if req.PaddingAfterSec != nil {
		realEnd += int64(*req.PaddingAfterSec)
	}

	desc := ""
	if req.Description != nil {
		desc = *req.Description
	}

	// 2. Add Timer
	err = client.AddTimer(ctx, realSRef, realBegin, realEnd, req.Name, desc)
	if err != nil {
		log.L().Error().Err(err).Str("sref", req.ServiceRef).Msg("failed to add timer")

		status := http.StatusInternalServerError
		errStr := err.Error()
		if strings.Contains(errStr, "Konflikt") || strings.Contains(errStr, "Conflict") || strings.Contains(errStr, "overlap") {
			status = http.StatusConflict
		}

		// Use writeProblem to return JSON error (RFC 7807) so frontend can parse the message
		writeProblem(w, status, "dvr/add_failed", err.Error(), nil)
		return
	}

	// 3. Read-back Verification
	verified := false
	var createdTimer *openwebif.Timer
	for i := 0; i < 3; i++ {
		checkTimers, err := client.GetTimers(ctx)
		if err == nil {
			for _, t := range checkTimers {
				// Log candidates to debug matching failure
				// Normalize candidate ref (OpenWebIF often returns trailing colon)
				candidateRef := strings.TrimSuffix(t.ServiceRef, ":")

				// Log candidates to debug matching failure
				if strings.Contains(t.ServiceRef, "1:0:1") { // Log only relevant looking timers to reduce noise
					log.L().Info().
						Str("candidate_ref", t.ServiceRef).
						Str("normalized_ref", candidateRef).
						Int64("candidate_begin", t.Begin).
						Str("target_ref", realSRef).
						Int64("target_begin", realBegin).
						Int64("diff_sec", t.Begin-realBegin).
						Msg("verification candidate")
				}

				// Flexible match (Enigma might adjust seconds slightly)
				if candidateRef == realSRef &&
					(t.Begin >= realBegin-5 && t.Begin <= realBegin+5) { // Increased tolerance to +/- 5s
					verified = true
					createdTimer = &t
					break
				}
			}
		}
		if verified {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if !verified {
		log.L().Warn().Str("sref", req.ServiceRef).Msg("timer added but not verified in read-back")
		writeProblem(w, http.StatusBadGateway, "dvr/receiver_inconsistent", "Timer added but failed verification", nil)
		return
	}

	// Map response
	timerId := MakeTimerID(createdTimer.ServiceRef, createdTimer.Begin, createdTimer.End)
	stateStr := TimerStateScheduled // Default

	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(Timer{
		TimerId:     timerId,
		ServiceRef:  createdTimer.ServiceRef,
		ServiceName: &createdTimer.ServiceName,
		Name:        createdTimer.Name,
		Description: &createdTimer.Description,
		Begin:       createdTimer.Begin,
		End:         createdTimer.End,
		State:       stateStr,
	})
}

// DeleteTimer implements ServerInterface
func (s *Server) DeleteTimer(w http.ResponseWriter, r *http.Request, timerId string) {
	sRef, begin, end, err := ParseTimerID(timerId)
	if err != nil {
		http.Error(w, "Invalid Timer ID", http.StatusBadRequest)
		return
	}

	s.mu.RLock()
	cfg := s.cfg
	snap := s.snap
	s.mu.RUnlock()

	client := s.newOpenWebIFClient(cfg, snap)
	err = client.DeleteTimer(r.Context(), sRef, begin, end)
	if err != nil {
		// If 404 from receiver? client.DeleteTimer returns error.
		// If it's "not found", we should return 404?
		// For now return 500 or 404 based on error?
		log.L().Error().Err(err).Str("timerId", timerId).Msg("failed to delete timer")
		http.Error(w, fmt.Sprintf("Failed to delete timer: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// UpdateTimer implements ServerInterface (Edit)
func (s *Server) UpdateTimer(w http.ResponseWriter, r *http.Request, timerId string) {
	var req TimerPatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	oldSRef, oldBegin, oldEnd, err := ParseTimerID(timerId)
	if err != nil {
		http.Error(w, "Invalid Timer ID", http.StatusBadRequest)
		return
	}

	s.mu.RLock()
	cfg := s.cfg
	snap := s.snap
	s.mu.RUnlock()
	client := s.newOpenWebIFClient(cfg, snap)
	ctx := r.Context()

	// Resolve Old Timer (ensure it exists)
	timers, err := client.GetTimers(ctx)
	if err != nil {
		http.Error(w, "Failed to fetch timers", http.StatusBadGateway)
		return
	}
	var existing *openwebif.Timer
	for _, t := range timers {
		if t.ServiceRef == oldSRef && t.Begin == oldBegin && t.End == oldEnd {
			existing = &t
			break
		}
	}
	if existing == nil {
		http.Error(w, "Timer not found", http.StatusNotFound)
		return
	}

	// Compute New Values
	newSRef := oldSRef
	newBegin := oldBegin
	newEnd := oldEnd
	newName := existing.Name
	newDesc := existing.Description

	if req.Begin != nil {
		newBegin = *req.Begin
	}
	if req.End != nil {
		newEnd = *req.End
	}
	if req.Name != nil {
		newName = *req.Name
	}
	if req.Description != nil {
		newDesc = *req.Description
	}

	// Apply Padding changes if provided
	if req.PaddingBeforeSec != nil {
		newBegin -= int64(*req.PaddingBeforeSec)
	}
	if req.PaddingAfterSec != nil {
		newEnd += int64(*req.PaddingAfterSec)
	}

	if newBegin >= newEnd {
		writeProblem(w, http.StatusUnprocessableEntity, "dvr/invalid_time", "Begin must be before End", nil)
		return
	}

	// Check Supported Features
	supportsNative := client.HasTimerChange(ctx)

	log.L().Info().Str("timerId", timerId).Bool("native", supportsNative).Msg("Updating timer")

	if supportsNative {
		err = client.UpdateTimer(ctx, oldSRef, oldBegin, oldEnd, newSRef, newBegin, newEnd, newName, newDesc, true)
		if err != nil {
			log.L().Error().Err(err).Msg("Native update failed, trying fallback?")
			// Fallback or Error? User said: "Attempt receiver-native edit if available; else delete+add"
			// If native fails, we might want to try fallback or just error.
			// Let's error for now to be safe, or fallback?
			// Fallback involves deleting the timer that might exist.
			writeProblem(w, http.StatusBadGateway, "dvr/update_failed", err.Error(), nil)
			return
		}
	} else {
		// Fallback: Delete + Add (Atomic-ish)
		// 1. Delete
		err = client.DeleteTimer(ctx, oldSRef, oldBegin, oldEnd)
		if err != nil {
			writeProblem(w, http.StatusInternalServerError, "dvr/delete_failed", "Failed to delete old timer during update", nil)
			return
		}

		// 2. Add
		err = client.AddTimer(ctx, newSRef, newBegin, newEnd, newName, newDesc)
		if err != nil {
			// CRITICAL: Rollback! Try to re-add old timer
			log.L().Error().Err(err).Msg("Add failed during update, attempting rollback")
			_ = client.AddTimer(ctx, oldSRef, oldBegin, oldEnd, existing.Name, existing.Description)

			writeProblem(w, http.StatusBadGateway, "dvr/receiver_inconsistent", "Failed to add new timer, attempted rollback of old timer", nil)
			return
		}
	}

	// Verify Existence of NEW timer
	verified := false
	var updatedTimer *openwebif.Timer
	for i := 0; i < 3; i++ {
		checkTimers, err := client.GetTimers(ctx)
		if err == nil {
			for _, t := range checkTimers {
				if t.ServiceRef == newSRef && (t.Begin == newBegin || t.Begin == newBegin-1 || t.Begin == newBegin+1) {
					verified = true
					updatedTimer = &t
					break
				}
			}
		}
		if verified {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if !verified || updatedTimer == nil {
		writeProblem(w, http.StatusBadGateway, "dvr/receiver_inconsistent", "Timer updated/added but verification failed", nil)
		return
	}

	// Success
	newId := MakeTimerID(updatedTimer.ServiceRef, updatedTimer.Begin, updatedTimer.End)
	// Return the updated timer
	// ... encode response
	_ = json.NewEncoder(w).Encode(Timer{
		TimerId:     newId,
		ServiceRef:  updatedTimer.ServiceRef,
		ServiceName: &updatedTimer.ServiceName,
		Name:        updatedTimer.Name,
		Description: &updatedTimer.Description,
		Begin:       updatedTimer.Begin,
		End:         updatedTimer.End,
		State:       TimerStateScheduled, // Simplified
	})
}

// PreviewConflicts implements ServerInterface
func (s *Server) PreviewConflicts(w http.ResponseWriter, r *http.Request) {
	var req TimerConflictPreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate request
	if req.Proposed.Begin >= req.Proposed.End {
		writeProblem(w, http.StatusUnprocessableEntity, "dvr/validation", "Begin must be before End", nil)
		return
	}

	s.mu.RLock()
	cfg := s.cfg
	snap := s.snap
	s.mu.RUnlock()

	client := s.newOpenWebIFClient(cfg, snap)

	// Fetch existing timers
	timers, err := client.GetTimers(r.Context())
	if err != nil {
		log.L().Error().Err(err).Msg("failed to fetch timers for conflict preview")
		// "Do not return canSchedule:true when you cannot fetch timers—fail closed."
		writeProblem(w, http.StatusBadGateway, "dvr/receiver_unreachable", "Could not fetch existing timers", nil)
		return
	}

	// Detect Conflicts
	conflicts := DetectConflicts(req.Proposed, timers)

	// Generate Suggestions if conflicts exist
	var suggestions *[]struct {
		Kind          *TimerConflictPreviewResponseSuggestionsKind `json:"kind,omitempty"`
		Note          *string                                      `json:"note,omitempty"`
		ProposedBegin *int64                                       `json:"proposedBegin,omitempty"`
		ProposedEnd   *int64                                       `json:"proposedEnd,omitempty"`
	}

	if len(conflicts) > 0 {
		sugs := GenerateSuggestions(req.Proposed, conflicts)
		if len(sugs) > 0 {
			suggestions = &sugs
		}
	}

	resp := TimerConflictPreviewResponse{
		CanSchedule: len(conflicts) == 0,
		Conflicts:   conflicts,
		Suggestions: suggestions,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// GetDvrCapabilities implements ServerInterface
func (s *Server) GetDvrCapabilities(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	ds := s.dvrSource
	s.mu.RUnlock()

	caps, err := read.GetDvrCapabilities(r.Context(), ds)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	ptr := func(b bool) *bool { return &b }
	str := func(s string) *string { return &s }

	resp := DvrCapabilities{
		Timers: struct {
			Delete         *bool `json:"delete,omitempty"`
			Edit           *bool `json:"edit,omitempty"`
			ReadBackVerify *bool `json:"readBackVerify,omitempty"`
		}{
			Delete:         ptr(caps.CanDelete),
			Edit:           ptr(caps.CanEdit),
			ReadBackVerify: ptr(caps.ReadBackVerify),
		},
		Conflicts: struct {
			Preview       *bool `json:"preview,omitempty"`
			ReceiverAware *bool `json:"receiverAware,omitempty"`
		}{
			Preview:       ptr(caps.ConflictsPreview),
			ReceiverAware: ptr(caps.ReceiverAware),
		},
		Series: struct {
			DelegatedProvider *string                    `json:"delegatedProvider,omitempty"`
			Mode              *DvrCapabilitiesSeriesMode `json:"mode,omitempty"`
			Supported         *bool                      `json:"supported,omitempty"`
		}{
			Supported: ptr(caps.SeriesSupported),
			Mode:      (*DvrCapabilitiesSeriesMode)(str(caps.SeriesMode)),
		},
	}
	if caps.DelegatedProvider != "" {
		resp.Series.DelegatedProvider = &caps.DelegatedProvider
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// GetTimer implements ServerInterface
func (s *Server) GetTimer(w http.ResponseWriter, r *http.Request, timerId string) {
	sRef, begin, end, err := ParseTimerID(timerId)
	if err != nil {
		http.Error(w, "Invalid Timer ID", http.StatusBadRequest)
		return
	}

	s.mu.RLock()
	cfg := s.cfg
	snap := s.snap
	s.mu.RUnlock()
	client := s.newOpenWebIFClient(cfg, snap)
	ctx := r.Context()

	timers, err := client.GetTimers(ctx)
	if err != nil {
		http.Error(w, "Failed to fetch timers", http.StatusBadGateway)
		return
	}

	for _, t := range timers {
		if t.ServiceRef == sRef && t.Begin == begin && t.End == end {
			// Found
			stateStr := TimerStateScheduled // map properly

			_ = json.NewEncoder(w).Encode(Timer{
				TimerId:     timerId,
				ServiceRef:  t.ServiceRef,
				ServiceName: &t.ServiceName,
				Name:        t.Name,
				Description: &t.Description,
				Begin:       t.Begin,
				End:         t.End,
				State:       stateStr,
			})
			return
		}
	}
	http.Error(w, "Timer not found", http.StatusNotFound)
}

func writeProblem(w http.ResponseWriter, status int, problemType, title string, conflicts []TimerConflict) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(ProblemDetails{
		Status:    status,
		Type:      problemType,
		Title:     title,
		Conflicts: &conflicts,
	})
}

// PostServicesNowNext implements POST /services/now-next.
func (s *Server) PostServicesNowNext(w http.ResponseWriter, r *http.Request) {
	s.handleNowNextEPG(w, r)
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Rate limit or check if already running?
	// For now, just trigger.

	go func() {
		start := time.Now()
		// Trigger refresh via config manager if needed, or scan?
		// In V3, refresh usually means scanning providers.
		if s.v3Scan != nil {
			s.v3Scan.RunBackground()
		}
		log.L().Info().Dur("duration", time.Since(start)).Msg("manual refresh triggered via API")
	}()

	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "refresh triggered"})
}
