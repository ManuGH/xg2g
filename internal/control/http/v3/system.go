// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/http/v3/problem"
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
		writeProblem(w, r, http.StatusBadRequest, "system/invalid_input", "Invalid Request Format", "INVALID_INPUT", "The request body could not be decoded as JSON", nil)
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
		writeProblem(w, r, http.StatusInternalServerError, "system/save_failed", "Save Failed", "SAVE_FAILED", "Failed to save configuration change to disk", nil)
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
		// Explicitly flush the 202 Accepted response to the client
		// before initiating shutdown.
		if rc := http.NewResponseController(w); rc != nil {
			_ = rc.Flush()
		}

		go func() {
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
		writeProblem(w, r, http.StatusInternalServerError, "services/read_failed", "Failed to get bouquets", "READ_FAILED", err.Error(), nil)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if bouquets == nil {
		_, _ = w.Write([]byte("null"))
		return
	}
	_ = json.NewEncoder(w).Encode(bouquets)
}

// GetServices implements ServerInterface
func (s *Server) GetServices(w http.ResponseWriter, r *http.Request, params GetServicesParams) {
	s.mu.RLock()
	cfg := s.cfg
	snap := s.snap
	src := s.servicesSource
	s.mu.RUnlock()

	q := read.ServicesQuery{}
	if params.Bouquet != nil {
		q.Bouquet = *params.Bouquet
	}

	res, err := read.GetServices(cfg, snap, src, q)
	if err != nil {
		log.L().Error().Err(err).Msg("failed to get services")
		writeProblem(w, r, http.StatusInternalServerError, "system/internal_error", "Receiver Read Error", "INTERNAL_ERROR", err.Error(), nil)
		return
	}

	// Map to API type to ensure correct JSON casing
	var items []Service
	if res.Items != nil {
		items = make([]Service, 0, len(res.Items))
		for _, s := range res.Items {
			// Scoping
			s := s
			items = append(items, Service{
				Id:         &s.ID,
				Name:       &s.Name,
				Group:      &s.Group,
				LogoUrl:    &s.LogoURL,
				Number:     &s.Number,
				Enabled:    &s.Enabled,
				ServiceRef: &s.ServiceRef,
			})
		}
	} else if res.EmptyEncoding == read.EmptyEncodingArray {
		items = make([]Service, 0)
	}

	w.Header().Set("Content-Type", "application/json")
	if items == nil && res.EmptyEncoding == read.EmptyEncodingNull {
		_, _ = w.Write([]byte("null"))
		return
	}
	if items == nil {
		items = make([]Service, 0) // Fallback for safety
	}
	_ = json.NewEncoder(w).Encode(items)
}

// PostServicesIdToggle implements ServerInterface
func (s *Server) PostServicesIdToggle(w http.ResponseWriter, r *http.Request, id string) {
	var req PostServicesIdToggleJSONBody
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "system/invalid_input", "Invalid Request Body", "INVALID_INPUT", "The request body is malformed", nil)
		return
	}

	if s.channelManager == nil {
		writeProblem(w, r, http.StatusInternalServerError, "system/unavailable", "Subsystem Unavailable", "UNAVAILABLE", "Channel manager not initialized", nil)
		return
	}

	enabled := false
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	if err := s.channelManager.SetEnabled(id, enabled); err != nil {
		log.L().Error().Err(err).Str("channel_id", id).Msg("failed to toggle channel")
		writeProblem(w, r, http.StatusInternalServerError, "system/save_failed", "Save Failed", "SAVE_FAILED", "Failed to save channel state", nil)
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
	s.mu.RUnlock()

	if s.v3Store == nil {
		log.L().Error().Msg("v3 store not initialized")
		writeProblem(w, r, http.StatusServiceUnavailable, "streams/unavailable", "V3 control plane not enabled", "UNAVAILABLE", "The V3 control plane is not enabled", nil)
		return
	}

	// 1. Parse Query (IP Gating)
	q := read.StreamsQuery{
		IncludeClientIP: r.URL.Query().Get("include_client_ip") == "true",
	}

	// 2. Call Control Provider
	streams, err := read.GetStreams(r.Context(), cfg, snap, store, q)
	if err != nil {
		log.L().Error().Err(err).Msg("failed to get streams")
		writeProblem(w, r, http.StatusInternalServerError, "streams/read_failed", "Failed to get streams", "READ_FAILED", "Failed to fetch active stream list", nil)
		return
	}

	// 3. Map to API DTOs
	resp := make([]StreamSession, 0, len(streams))
	for _, st := range streams {
		// Pointers for optional fields (capture loop var)
		id := st.ID
		name := st.ChannelName
		ip := st.ClientIP
		start := st.StartedAt

		// Map State (Note B: Strict "active" only)
		state := st.State
		activeState := Active
		if state == "active" {
			activeState = Active
		}

		item := StreamSession{
			Id:    &id,
			State: &activeState,
		}

		if name != "" {
			item.ChannelName = &name
		}
		if ip != "" {
			item.ClientIp = &ip
		}
		if !start.IsZero() {
			item.StartedAt = &start
		}

		resp = append(resp, item)
	}

	// 4. Send Response (Always [] for empty)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// DeleteStreamsId implements ServerInterface
func (s *Server) DeleteStreamsId(w http.ResponseWriter, r *http.Request, id string) {
	// Single source of truth for validation + contract shape
	if id == "" || !model.IsSafeSessionID(id) {
		writeProblem(w, r, http.StatusBadRequest, "streams/invalid_id", "Invalid Session ID", "INVALID_SESSION_ID", "The provided session ID contains unsafe characters", nil)
		return
	}

	s.mu.RLock()
	bus := s.v3Bus
	store := s.v3Store
	s.mu.RUnlock()

	if s.v3Store == nil || s.v3Bus == nil {
		writeProblem(w, r, http.StatusServiceUnavailable, "streams/unavailable", "Control Plane Unavailable", "UNAVAILABLE", "V3 control plane is not enabled", nil)
		return
	}

	session, err := store.GetSession(r.Context(), id)
	if err != nil {
		writeProblem(w, r, http.StatusInternalServerError, "streams/stop_failed", "Stop Failed", "STOP_FAILED", "Failed to retrieve session state", nil)
		return
	}
	if session == nil {
		writeProblem(w, r, http.StatusNotFound, "streams/not_found", "Session Not Found", "NOT_FOUND", "The session does not exist", nil)
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
		writeProblem(w, r, http.StatusInternalServerError, "streams/stop_failed", "Stop Failed", "STOP_FAILED", "Failed to publish stop event", nil)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleNowNextEPG returns now/next EPG for a list of service references.
func (s *Server) handleNowNextEPG(w http.ResponseWriter, r *http.Request) {
	var req nowNextRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Services) == 0 {
		writeProblem(w, r, http.StatusBadRequest, "epg/invalid_input", "Invalid Request", "INVALID_INPUT", "Request body must contain non-empty services list", nil)
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
		log.L().Error().Err(err).Msg("failed to get DVR status")
		writeProblem(w, r, http.StatusBadGateway, "system/receiver_error", "Status Failed", "RECEIVER_ERROR", "Failed to get receiver status", nil)
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

// epgSourceWrapper adapts the server infrastructure to the control-layer EpgSource interface.
type epgSourceWrapper struct {
	s *Server
}

func (w *epgSourceWrapper) GetPrograms(ctx context.Context) ([]epg.Programme, error) {
	s := w.s
	s.mu.RLock()
	cfg := s.cfg
	s.mu.RUnlock()

	if strings.TrimSpace(cfg.XMLTVPath) == "" {
		return nil, os.ErrNotExist
	}

	xmltvPath, err := s.dataFilePath(cfg.XMLTVPath)
	if err != nil {
		return nil, err
	}

	// Singleflight for Concurrency Protection
	result, err, _ := s.epgSfg.Do("epg-load", func() (interface{}, error) {
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
		data, err := os.ReadFile(xmltvPath) // #nosec G304
		if err != nil {
			return nil, err
		}

		var parsedTU epg.TV
		if err := xml.Unmarshal(data, &parsedTU); err != nil {
			s.mu.RLock()
			stale := s.epgCache
			s.mu.RUnlock()
			if stale != nil {
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
		return nil, err
	}

	tv := result.(*epg.TV)
	return tv.Programs, nil
}

func (w *epgSourceWrapper) GetBouquetServiceRefs(ctx context.Context, bouquet string) (map[string]struct{}, error) {
	s := w.s
	s.mu.RLock()
	snap := s.snap
	cfg := s.cfg
	s.mu.RUnlock()

	playlistName := snap.Runtime.PlaylistFilename
	// Parity Hardening: If playlist not configured, strict filter (empty result)
	if playlistName == "" {
		return make(map[string]struct{}), nil
	}
	playlistPath := filepath.Clean(filepath.Join(cfg.DataDir, playlistName))

	data, err := os.ReadFile(playlistPath) // #nosec G304
	if err != nil {
		// Parity: Legacy ignores filter if playlist read fails (effectively strict filter = empty results)
		// unless search is active (handled by caller).
		// Legacy behavior: "allowedRefs" remains initialized but empty.
		// So we return empty map, NO error.
		return make(map[string]struct{}), nil
	}

	allowedRefs := make(map[string]struct{})
	channels := m3u.Parse(string(data))

	for _, ch := range channels {
		if ch.Group != bouquet {
			continue
		}
		if ch.TvgID != "" {
			allowedRefs[ch.TvgID] = struct{}{}
		}
	}

	return allowedRefs, nil
}

// GetEpg implements ServerInterface
// GetEpg implements ServerInterface
func (s *Server) GetEpg(w http.ResponseWriter, r *http.Request, params GetEpgParams) {
	s.mu.RLock()
	src := s.epgSource
	s.mu.RUnlock()

	q := read.EpgQuery{}
	if params.From != nil {
		q.From = int64(*params.From)
	}
	if params.To != nil {
		q.To = int64(*params.To)
	}
	if params.Bouquet != nil {
		q.Bouquet = *params.Bouquet
	}
	if params.Q != nil {
		q.Q = *params.Q
	}

	entries, err := read.GetEpg(r.Context(), src, q, read.RealClock{})
	if err != nil {
		log.L().Error().Err(err).Msg("failed to load EPG")
		writeProblem(w, r, http.StatusInternalServerError, "system/internal_error", "Internal Server Error", "INTERNAL_ERROR", "Failed to parse system config", nil)
		return
	}

	resp := make([]EpgItem, 0, len(entries))
	for _, e := range entries {
		// Capture variables for pointer assignment
		id := e.ID // Legacy used channel ID but ServiceRef is safer?
		// Legacy: allowedRefs[p.Channel]. But output EpgItem.Id *string.
		// Usually Id is just channel ID.
		sRef := e.ServiceRef
		title := e.Title
		desc := e.Desc
		start := int(e.Start)
		end := int(e.End)
		dur := int(e.Duration)

		resp = append(resp, EpgItem{
			Id:         &id,
			ServiceRef: sRef,
			Title:      title,
			Desc:       &desc,
			Start:      start,
			End:        end,
			Duration:   &dur,
		})
	}

	if len(resp) == 0 {
		resp = nil
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// GetTimers implements ServerInterface
func (s *Server) GetTimers(w http.ResponseWriter, r *http.Request, params GetTimersParams) {
	s.mu.RLock()
	src := s.timersSource
	s.mu.RUnlock()

	if s.timersSource == nil {
		writeProblem(w, r, http.StatusServiceUnavailable, "dvr/unavailable", "DVR Unavailable", "UNAVAILABLE", "Timers source not initialized", nil)
		return
	}

	q := read.TimersQuery{}
	if params.State != nil {
		q.State = read.TimerState(*params.State)
	}
	if params.From != nil {
		q.From = int64(*params.From)
	}

	timers, err := read.GetTimers(r.Context(), src, q, read.RealClock{})
	if err != nil {
		log.L().Error().Err(err).Msg("failed to communicate with receiver")
		writeProblem(w, r, http.StatusBadGateway, "dvr/receiver_unreachable", "Receiver Unreachable", "RECEIVER_UNREACHABLE", "Failed to communicate with receiver", nil)
		return
	}

	mapped := make([]Timer, 0, len(timers))
	for _, t := range timers {
		// New read.Timer DTO already has correct types, just map to API struct
		mapped = append(mapped, Timer{
			TimerId:     t.TimerID,
			ServiceRef:  t.ServiceRef,
			ServiceName: &t.ServiceName,
			Name:        t.Name,
			Description: &t.Description,
			Begin:       t.Begin,
			End:         t.End,
			State:       TimerState(t.State),
		})
	}

	// Legacy Parity check for Timers (TimerList{Items: ...})
	// Legacy returned TimerList with Items: [] (not null) because it was initialized as make([]Timer, 0, count)
	// and wrapped in a struct. Struct field []Timer would be [] in JSON if initialized.
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(TimerList{Items: mapped})
}

// AddTimer implements ServerInterface
func (s *Server) AddTimer(w http.ResponseWriter, r *http.Request) {
	var req TimerCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "dvr/invalid_input", "Invalid Request Body", "INVALID_INPUT", "The request body is malformed or empty", nil)
		return
	}

	if req.Begin >= req.End {
		writeProblem(w, r, http.StatusUnprocessableEntity, "dvr/invalid_time", "Invalid Timer Time", "INVALID_TIME", "Begin time must be before end time", nil)
		return
	}

	s.mu.RLock()
	cfg := s.cfg
	snap := s.snap
	s.mu.RUnlock()

	client := s.owi(cfg, snap)
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

	// Calculate Canonical Times ONCE (with padding)
	realBegin := req.Begin
	if req.PaddingBeforeSec != nil {
		realBegin -= int64(*req.PaddingBeforeSec)
	}
	realEnd := req.End
	if req.PaddingAfterSec != nil {
		realEnd += int64(*req.PaddingAfterSec)
	}

	// 1. Duplicate/Conflict Check
	existingTimers, err := client.GetTimers(ctx)
	if err != nil {
		log.L().Error().Err(err).Msg("failed to fetch timers for duplicate check")
		writeProblem(w, r, http.StatusBadGateway, "dvr/receiver_unreachable", "Receiver Unreachable", "RECEIVER_UNREACHABLE", "Failed to verify existing timers", nil)
		return
	}

	// Normalize ServiceRef for comparisons (Enigma2 often differs by trailing colon)
	realSRefNorm := strings.TrimSuffix(realSRef, ":")

	for _, t := range existingTimers {
		// Strict Duplicate: Normalized ServiceRef + Exact Canonical Begin/End
		if strings.TrimSuffix(t.ServiceRef, ":") == realSRefNorm && t.Begin == realBegin && t.End == realEnd {
			// 409 Conflict (Duplicate)
			writeProblem(w, r, http.StatusConflict, "dvr/duplicate", "Timer Conflict", "CONFLICT", "A timer with the same parameters already exists", nil)
			return
		}
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
		writeProblem(w, r, status, "dvr/add_failed", "Add Timer Failed", "ADD_FAILED", err.Error(), nil)
		return
	}

	// 3. Read-back Verification
	verified := false
	var createdTimer *openwebif.Timer

verifyAddLoop:
	for i := 0; i < 5; i++ {
		checkTimers, err := client.GetTimers(r.Context())
		if err == nil {
			targetNormalized := strings.TrimSuffix(realSRef, ":")
			for _, t := range checkTimers {
				candidateRef := strings.TrimSuffix(t.ServiceRef, ":")
				if candidateRef == targetNormalized &&
					(t.Begin >= realBegin-5 && t.Begin <= realBegin+5) { // Increased tolerance to +/- 5s
					verified = true
					createdTimer = &t
					break verifyAddLoop
				}
			}
		}
		if verified {
			break verifyAddLoop
		}

		select {
		case <-r.Context().Done():
			break verifyAddLoop
		case <-time.After(100 * time.Millisecond):
			// continue
		}
	}

	if !verified {
		log.L().Warn().Str("sref", req.ServiceRef).Msg("timer add verified failed")
		writeProblem(w, r, http.StatusBadGateway, "dvr/receiver_inconsistent", "Receiver Inconsistent", "RECEIVER_INCONSISTENT", "Timer added but failed verification", nil)
		return
	}

	// Map response using Truth Table
	dto := read.MapOpenWebIFTimerToDTO(*createdTimer, read.RealClock{}.Now())

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(Timer{
		TimerId:     dto.TimerID,
		ServiceRef:  dto.ServiceRef,
		ServiceName: &dto.ServiceName,
		Name:        dto.Name,
		Description: &dto.Description,
		Begin:       dto.Begin,
		End:         dto.End,
		State:       TimerState(dto.State),
	})
}

// DeleteTimer implements ServerInterface
func (s *Server) DeleteTimer(w http.ResponseWriter, r *http.Request, timerId string) {
	sRef, begin, end, err := read.ParseTimerID(timerId)
	if err != nil {
		writeProblem(w, r, http.StatusBadRequest, "dvr/invalid_id", "Invalid Timer ID", "INVALID_ID", "The provided timer ID is invalid", nil)
		return
	}

	s.mu.RLock()
	cfg := s.cfg
	snap := s.snap
	s.mu.RUnlock()

	client := s.owi(cfg, snap)
	err = client.DeleteTimer(r.Context(), sRef, begin, end)
	if err != nil {
		log.L().Error().Err(err).Str("timerId", timerId).Msg("failed to delete timer")

		status := http.StatusBadGateway
		errCode := "dvr/delete_failed"
		title := "Delete Failed"
		errStr := err.Error()

		if strings.Contains(errStr, "not found") || strings.Contains(errStr, "nicht gefunden") || strings.Contains(errStr, "404") {
			status = http.StatusNotFound
			errCode = "dvr/not_found"
			title = "Timer Not Found"
		} else if strings.Contains(errStr, "connection") || strings.Contains(errStr, "refused") || strings.Contains(errStr, "timeout") {
			status = http.StatusBadGateway
			errCode = "dvr/receiver_unreachable"
			title = "Receiver Unreachable"
		}

		writeProblem(w, r, status, errCode, title, "DELETE_FAILED", errStr, nil)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// UpdateTimer implements ServerInterface (Edit)
func (s *Server) UpdateTimer(w http.ResponseWriter, r *http.Request, timerId string) {
	var req TimerPatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "dvr/invalid_input", "Invalid Request Body", "INVALID_INPUT", "The request body is malformed or empty", nil)
		return
	}

	oldSRef, oldBegin, oldEnd, err := read.ParseTimerID(timerId)
	if err != nil {
		writeProblem(w, r, http.StatusBadRequest, "dvr/invalid_id", "Invalid Timer ID", "INVALID_ID", "The provided timer ID is invalid", nil)
		return
	}

	s.mu.RLock()
	cfg := s.cfg
	snap := s.snap
	s.mu.RUnlock()
	client := s.owi(cfg, snap)
	ctx := r.Context()

	// Resolve Old Timer (ensure it exists)
	timers, err := client.GetTimers(ctx)
	if err != nil {
		log.L().Error().Err(err).Msg("failed to fetch timers during update")
		writeProblem(w, r, http.StatusBadGateway, "dvr/receiver_unreachable", "Receiver Unreachable", "RECEIVER_UNREACHABLE", "Failed to verify existing timer", nil)
		return
	}
	var existing *openwebif.Timer
	// Match strictly on ID derived components
	for _, t := range timers {
		if strings.TrimSuffix(t.ServiceRef, ":") == strings.TrimSuffix(oldSRef, ":") && t.Begin == oldBegin && t.End == oldEnd {
			existing = &t
			break
		}
	}
	if existing == nil {
		writeProblem(w, r, http.StatusNotFound, "dvr/not_found", "Timer Not Found", "NOT_FOUND", "The requested timer does not exist on the receiver", nil)
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

	// Hardening: Reject padding in PATCH to prevent timer drift (Option A)
	if req.PaddingBeforeSec != nil || req.PaddingAfterSec != nil {
		writeProblem(w, r, http.StatusBadRequest, "dvr/unsupported_field", "Padding Update Not Supported", "INVALID_INPUT", "Updating padding via PATCH is not supported to avoid drift. Please update 'begin' and 'end' directly with absolute values.", nil)
		return
	}

	if newBegin >= newEnd {
		writeProblem(w, r, http.StatusUnprocessableEntity, "dvr/invalid_time", "Invalid Timer Order", "INVALID_TIME", "Begin time must be before end time", nil)
		return
	}

	// Check Supported Features
	supportsNative := client.HasTimerChange(ctx)

	log.L().Info().Str("timerId", timerId).Bool("native", supportsNative).Msg("Updating timer")

	if supportsNative {
		err = client.UpdateTimer(ctx, oldSRef, oldBegin, oldEnd, newSRef, newBegin, newEnd, newName, newDesc, true)
		if err != nil {
			log.L().Error().Err(err).Str("timerId", timerId).Msg("native update failed")
			// Map to conflict if message suggests it
			status := http.StatusBadGateway
			errCode := "dvr/update_failed"
			errStr := err.Error()
			if strings.Contains(err.Error(), "Konflikt") || strings.Contains(err.Error(), "Conflict") || strings.Contains(err.Error(), "overlap") {
				status = http.StatusConflict
			}
			writeProblem(w, r, status, errCode, "Update Failed", "UPDATE_FAILED", errStr, nil)
			return
		}
	} else {
		// Fallback: Delete + Add (Atomic-ish)
		log.L().Info().Str("timerId", timerId).Msg("performing delete+add fallback for update")
		err = client.DeleteTimer(ctx, oldSRef, oldBegin, oldEnd)
		if err != nil {
			log.L().Error().Err(err).Str("timerId", timerId).Msg("delete failed")
			writeProblem(w, r, http.StatusInternalServerError, "dvr/receiver_error", "Delete Failed", "DELETE_FAILED", "Failed to delete timer from receiver during update", nil)
			return
		}

		err = client.AddTimer(ctx, newSRef, newBegin, newEnd, newName, newDesc)
		if err != nil {
			log.L().Error().Err(err).Str("timerId", timerId).Msg("fallback add failed, attempting rollback")
			// Rollback: try to re-add the old one
			_ = client.AddTimer(ctx, oldSRef, oldBegin, oldEnd, existing.Name, existing.Description)

			status := http.StatusBadGateway
			if strings.Contains(err.Error(), "Konflikt") || strings.Contains(err.Error(), "Conflict") {
				status = http.StatusConflict
			}
			writeProblem(w, r, status, "dvr/receiver_inconsistent", "Update Failed (Add Step)", "RECEIVER_INCONSISTENT", err.Error(), nil)
			return
		}
	}

	// Verify Existence of NEW timer
	verified := false
	var updatedTimer *openwebif.Timer
verifyUpdateLoop:
	for i := 0; i < 5; i++ {
		checkTimers, err := client.GetTimers(ctx)
		if err == nil {
			targetNormalized := strings.TrimSuffix(newSRef, ":")
			for _, t := range checkTimers {
				candidateRef := strings.TrimSuffix(t.ServiceRef, ":")
				if candidateRef == targetNormalized && (t.Begin >= newBegin-5 && t.Begin <= newBegin+5) {
					verified = true
					updatedTimer = &t
					break verifyUpdateLoop
				}
			}
		}
		if verified {
			break verifyUpdateLoop
		}

		select {
		case <-ctx.Done():
			// Context canceled/deadline exceeded
			break verifyUpdateLoop
		case <-time.After(100 * time.Millisecond):
			// continue
		}
	}

	if !verified || updatedTimer == nil {
		log.L().Warn().Str("timerId", timerId).Msg("timer update verification failed")
		writeProblem(w, r, http.StatusBadGateway, "dvr/receiver_inconsistent", "Inconsistent Receiver State", "RECEIVER_INCONSISTENT", "Timer updated but failed verification", nil)
		return
	}

	// Success
	dto := read.MapOpenWebIFTimerToDTO(*updatedTimer, read.RealClock{}.Now())

	// Return the updated timer
	// ... encode response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(Timer{
		TimerId:     dto.TimerID,
		ServiceRef:  dto.ServiceRef,
		ServiceName: &dto.ServiceName,
		Name:        dto.Name,
		Description: &dto.Description,
		Begin:       dto.Begin,
		End:         dto.End,
		State:       TimerState(dto.State),
	})
}

// PreviewConflicts implements ServerInterface
func (s *Server) PreviewConflicts(w http.ResponseWriter, r *http.Request) {
	var req TimerConflictPreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "dvr/invalid_input", "Invalid Request Body", "INVALID_INPUT", "The request body is malformed or empty", nil)
		return
	}

	// Validate request
	if req.Proposed.Begin >= req.Proposed.End {
		writeProblem(w, r, http.StatusUnprocessableEntity, "dvr/validation", "Invalid Timer Order", "INVALID_TIME", "Begin time must be before end time", nil)
		return
	}

	s.mu.RLock()
	cfg := s.cfg
	snap := s.snap
	s.mu.RUnlock()

	client := s.owi(cfg, snap)

	// Fetch existing timers
	timers, err := client.GetTimers(r.Context())
	if err != nil {
		log.L().Error().Err(err).Msg("failed to fetch timers for conflict preview")
		// "Do not return canSchedule:true when you cannot fetch timers—fail closed."
		writeProblem(w, r, http.StatusBadGateway, "dvr/receiver_unreachable", "Receiver Unreachable", "RECEIVER_UNREACHABLE", "Could not fetch existing timers for conflict check", nil)
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
		log.L().Error().Err(err).Msg("failed to fetch dvr capabilities")
		writeProblem(w, r, http.StatusInternalServerError, "dvr/provider_error", "Failed to Fetch Capabilities", "PROVIDER_ERROR", err.Error(), nil)
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
	sRef, begin, end, err := read.ParseTimerID(timerId)
	if err != nil {
		writeProblem(w, r, http.StatusBadRequest, "dvr/invalid_id", "Invalid Timer ID", "INVALID_ID", "The provided timer ID is invalid", nil)
		return
	}

	s.mu.RLock()
	cfg := s.cfg
	snap := s.snap
	s.mu.RUnlock()
	client := s.owi(cfg, snap)
	ctx := r.Context()

	timers, err := client.GetTimers(ctx)
	if err != nil {
		log.L().Error().Err(err).Msg("failed to fetch timers for individual get")
		writeProblem(w, r, http.StatusBadGateway, "dvr/receiver_unreachable", "Receiver Unreachable", "RECEIVER_UNREACHABLE", "Failed to fetch timers from receiver", nil)
		return
	}

	// Normalize sRef for comparison (remove trailing colon if present)
	sRefNorm := strings.TrimSuffix(sRef, ":")

	for _, t := range timers {
		// DTO map first to get canonical ID matching logic if needed,
		// but here we match raw fields to robustly find the source timer.
		candRef := strings.TrimSuffix(t.ServiceRef, ":")

		// Match logic:
		// 1. ServiceRef matches
		// 2. Begin matches exactly OR is within verification window (since ID has specific Begin)
		//    Actually ID has specific Begin/End. We should match closely.
		if candRef == sRefNorm && t.Begin == begin && t.End == end {
			// Found - Map to DTO using Truth Table
			dto := read.MapOpenWebIFTimerToDTO(t, read.RealClock{}.Now())

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(Timer{
				TimerId:     dto.TimerID,
				ServiceRef:  dto.ServiceRef,
				ServiceName: &dto.ServiceName,
				Name:        dto.Name,
				Description: &dto.Description,
				Begin:       dto.Begin,
				End:         dto.End,
				State:       TimerState(dto.State), // Cast string to enum
			})
			return
		}
	}
	writeProblem(w, r, http.StatusNotFound, "dvr/not_found", "Timer Not Found", "NOT_FOUND", "The requested timer does not exist on the receiver", nil)
}

// problemDetailsResponse defines the structure for RFC 7807 responses.
// Note: This shadows the generated ProblemDetails to strictly enforce the "details" extension point
func writeProblem(w http.ResponseWriter, r *http.Request, status int, problemType, title, code, detail string, extra map[string]any) {
	problem.Write(w, r, status, problemType, title, code, detail, extra)
}

// PostServicesNowNext implements POST /services/now-next.
func (s *Server) PostServicesNowNext(w http.ResponseWriter, r *http.Request) {
	s.handleNowNextEPG(w, r)
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	scan := s.v3Scan
	s.mu.RUnlock()

	if scan == nil {
		writeProblem(w, r, http.StatusServiceUnavailable, "system/unavailable", "Subsystem Unavailable", "UNAVAILABLE", "Scanner not enabled", nil)
		return
	}

	go func() {
		start := time.Now()
		scan.RunBackground()
		log.L().Info().Dur("duration", time.Since(start)).Msg("manual refresh triggered via API")
	}()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "refresh triggered"})
}
