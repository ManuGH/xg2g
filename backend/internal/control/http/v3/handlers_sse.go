// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	v3sessions "github.com/ManuGH/xg2g/internal/control/http/v3/sessions"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/go-chi/chi/v5"
)

// handleV3SessionEvents handles GET /sessions/{sessionID}/events.
// It subscribes to real-time session telemetry and state events and streams them via SSE.
func (s *Server) handleV3SessionEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeProblem(w, r, http.StatusInternalServerError, "sessions/events/streaming_unsupported", "Streaming Unsupported", "STREAMING_UNSUPPORTED", "HTTP streaming is unsupported by this server configuration.", nil)
		return
	}

	deps := s.sessionsModuleDeps()
	sessionID := chi.URLParam(r, "sessionID")

	result, err := s.sessionsProcessor().GetSession(r.Context(), v3sessions.GetSessionRequest{
		SessionID: sessionID,
		RequestID: requestID(r.Context()),
		Now:       time.Now(),
		HLSRoot:   deps.cfg.HLS.Root,
	})
	if err != nil {
		writeSessionStateServiceError(w, r, deps.cfg.HLS.Root, err)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// Send initial state event
	initialState := model.SessionStateChangedEvent{
		Type:        model.EventSessionStateChanged,
		SessionID:   sessionID,
		State:       result.Outcome.State,
		Reason:      result.Outcome.Reason,
		UpdatedAtUN: time.Now().Unix(),
	}
	_ = writeSSEEvent(w, flusher, string(initialState.Type), initialState)

	if result.Session != nil && result.Session.PlaybackTrace != nil && result.Session.PlaybackTrace.RuntimeDiagnostics != nil {
		initialTelem := model.SessionTelemetryEvent{
			Type:        model.EventSessionTelemetry,
			SessionID:   sessionID,
			Diagnostics: *result.Session.PlaybackTrace.RuntimeDiagnostics,
			UpdatedAtUN: time.Now().Unix(),
		}
		_ = writeSSEEvent(w, flusher, string(initialTelem.Type), initialTelem)
	}

	if deps.bus == nil {
		<-r.Context().Done()
		return
	}

	stateSub, subErr := deps.bus.Subscribe(r.Context(), string(model.EventSessionStateChanged))
	if subErr != nil {
		return
	}
	defer func() { _ = stateSub.Close() }()

	telemSub, subErr2 := deps.bus.Subscribe(r.Context(), string(model.EventSessionTelemetry))
	if subErr2 != nil {
		return
	}
	defer func() { _ = telemSub.Close() }()

	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-stateSub.C():
			if event, ok := msg.(model.SessionStateChangedEvent); ok && event.SessionID == sessionID {
				if err := writeSSEEvent(w, flusher, string(event.Type), event); err != nil {
					return
				}
			}
		case msg := <-telemSub.C():
			if event, ok := msg.(model.SessionTelemetryEvent); ok && event.SessionID == sessionID {
				if err := writeSSEEvent(w, flusher, string(event.Type), event); err != nil {
					return
				}
			}
		}
	}
}

func writeSSEEvent(w http.ResponseWriter, flusher http.Flusher, eventType string, data any) error {
	bytes, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, string(bytes))
	if err != nil {
		return err
	}
	flusher.Flush()
	return nil
}
