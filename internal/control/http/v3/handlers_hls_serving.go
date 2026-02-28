// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/metrics"
	v3api "github.com/ManuGH/xg2g/internal/pipeline/api"
)

// Responsibility: Serves HLS playlists and segments from disk.
// Non-goals: Playback lifecycle or session management.

// handleV3HLS serves HLS playlists and segments.
// Authorization: Requires v3:read scope (enforced by route middleware).
func (s *Server) handleV3HLS(w http.ResponseWriter, r *http.Request) {
	deps := s.sessionsModuleDeps()
	store := deps.store

	if store == nil {
		RespondError(w, r, http.StatusServiceUnavailable, &APIError{
			Code:    "V3_UNAVAILABLE",
			Message: "v3 not available",
		})
		return
	}

	// 2. Extract Params
	sessionID := chi.URLParam(r, "sessionID")
	filename := chi.URLParam(r, "filename")
	stage := playbackStageLabelFromLiveFilename(filename)

	// 3. Serve via HLS helper
	wrapped, tracker := wrapResponseWriter(w)
	v3api.ServeHLS(wrapped, r, store, deps.cfg.HLS.Root, sessionID, filename)

	status := http.StatusOK
	if st, ok := tracker.(StatusTracker); ok {
		status = st.StatusCode()
	}

	if status >= 400 {
		code := playbackErrorCodeFromStatus(status)
		metrics.IncPlaybackError(playbackSchemaLiveLabel, stage, code)
		if deps.playbackSLO != nil {
			outcome := deps.playbackSLO.MarkOutcome(playbackSessionMeta{
				SessionID: sessionID,
				Schema:    playbackSchemaLiveLabel,
				Mode:      playbackModeHLSLabel,
			}, "failed")
			if outcome.TTFFObserved {
				evt := log.L().Info().
					Str("event", "playback.slo.ttff").
					Str("request_id", requestID(r.Context())).
					Str("session_id", sessionID).
					Str("schema", outcome.Schema).
					Str("mode", outcome.Mode).
					Str("outcome", outcome.Outcome).
					Float64("ttff_seconds", outcome.TTFFSeconds)
				if outcome.ServiceRef != "" {
					evt = evt.Str("service_ref", outcome.ServiceRef)
				}
				evt.Msg("live playback ttff outcome observed")
			}
		}
		return
	}

	if deps.playbackSLO == nil {
		return
	}
	obs := deps.playbackSLO.MarkMediaSuccess(playbackSessionMeta{
		SessionID: sessionID,
		Schema:    playbackSchemaLiveLabel,
		Mode:      playbackModeHLSLabel,
	})
	if obs.TTFFObserved {
		evt := log.L().Info().
			Str("event", "playback.slo.ttff").
			Str("request_id", requestID(r.Context())).
			Str("session_id", sessionID).
			Str("schema", obs.Schema).
			Str("mode", obs.Mode).
			Str("outcome", "ok").
			Float64("ttff_seconds", obs.TTFFSeconds)
		if obs.ServiceRef != "" {
			evt = evt.Str("service_ref", obs.ServiceRef)
		}
		evt.Msg("live playback ttff observed")
	}
	if obs.RebufferSeverity != "" {
		evt := log.L().Warn().
			Str("event", "playback.slo.rebuffer").
			Str("request_id", requestID(r.Context())).
			Str("session_id", sessionID).
			Str("schema", obs.Schema).
			Str("mode", obs.Mode).
			Str("severity", obs.RebufferSeverity)
		if obs.ServiceRef != "" {
			evt = evt.Str("service_ref", obs.ServiceRef)
		}
		evt.Msg("live playback rebuffer proxy event observed")
	}
}
