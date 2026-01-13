// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/ManuGH/xg2g/internal/control/read"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/epg"
	"github.com/ManuGH/xg2g/internal/log"
)

// Responsibility: Handles Active Streams list and deletion.
// Non-goals: Service management or config.

// GetStreams implements ServerInterface
func (s *Server) GetStreams(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	store := s.v3Store
	cfg := s.cfg
	snap := s.snap
	s.mu.RUnlock()

	if store == nil {
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
	now := time.Now()
	xmltvFormat := "20060102150405 -0700"

	// Pre-index programs for faster lookup
	progMap := make(map[string][]epg.Programme)
	if snap.App.EPGEnabled && s.epgCache != nil {
		for _, p := range s.epgCache.Programs {
			progMap[p.Channel] = append(progMap[p.Channel], p)
		}
	}

	resp := make([]StreamSession, 0, len(streams))
	for _, st := range streams {
		// Pointers for optional fields (capture loop var)
		id := st.ID
		name := st.ChannelName
		ip := st.ClientIP
		start := st.StartedAt

		// Map State
		activeState := Idle
		if st.State == "active" {
			activeState = Active
		}

		var clientIP *string
		if q.IncludeClientIP {
			clientIP = &ip
		}
		dto := StreamSession{
			Id:          &id,
			ChannelName: &name,
			ClientIp:    clientIP,
			StartedAt:   &start,
			State:       &activeState,
		}

		// Enrich with EPG if available
		if progs, ok := progMap[st.ServiceRef]; ok {
			for _, p := range progs {
				pStart, serr := time.Parse(xmltvFormat, p.Start)
				pStop, perr := time.Parse(xmltvFormat, p.Stop)
				if serr == nil && perr == nil && now.After(pStart) && now.Before(pStop) {
					pTitle := p.Title.Text
					pDesc := p.Desc
					pBegin := pStart.Unix()
					pDuration := int(pStop.Sub(pStart).Seconds())

					dto.Program = &struct {
						BeginTimestamp *int64  `json:"begin_timestamp,omitempty"`
						Description    *string `json:"description,omitempty"`
						DurationSec    *int    `json:"duration_sec,omitempty"`
						Title          *string `json:"title,omitempty"`
					}{
						Title:          &pTitle,
						Description:    &pDesc,
						BeginTimestamp: &pBegin,
						DurationSec:    &pDuration,
					}
					break
				}
			}
		}

		resp = append(resp, dto)
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

	if store == nil || bus == nil {
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
