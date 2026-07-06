// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"net/http"
	"regexp"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/metrics"
	v3api "github.com/ManuGH/xg2g/internal/pipeline/api"
	"github.com/ManuGH/xg2g/internal/problemcode"
)

var safeHLSSessionIDRouteRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
var safeHLSFilenameRouteRe = regexp.MustCompile(`^(?:index\.m3u8|stream\.m3u8|init\.mp4|seg_[A-Za-z0-9_-]+\.(?:ts|m4s)|stream[A-Za-z0-9_-]*\.ts)$`)

// Responsibility: Serves HLS playlists and segments from disk.
// Non-goals: Playback lifecycle or session management.

// handleV3HLS serves HLS playlists and segments.
// Authorization: Requires v3:read scope (enforced by route middleware).
func (s *Server) handleV3HLS(w http.ResponseWriter, r *http.Request) {
	deps := s.sessionsModuleDeps()
	store := deps.store

	if store == nil {
		RespondError(w, r, http.StatusServiceUnavailable, ErrV3NotAvailable)
		return
	}

	// 2. Extract Params
	sessionID := chi.URLParam(r, "sessionID")
	filename := chi.URLParam(r, "filename")
	if !safeHLSSessionIDRouteRe.MatchString(sessionID) {
		writeRegisteredProblem(w, r, http.StatusBadRequest, "sessions/invalid_id", "Invalid Session ID", problemcode.CodeInvalidSessionID, "The provided session ID contains unsafe characters", nil)
		return
	}
	// DVR scrub preview: rides on this route (same session auth/scope) instead of
	// a separate endpoint, so it inherits ServeHLS's access model and adds no new
	// contract surface. Intercept before the segment/playlist allowlist, but after
	// session validation — expired/terminated sessions must not serve previews even
	// if their directory lingers on disk after muxer cleanup.
	if filename == livePreviewFilename {
		rec, storeErr := store.GetSession(r.Context(), sessionID)
		if storeErr != nil || rec == nil {
			writeRegisteredProblem(w, r, http.StatusNotFound, "sessions/not_found", "Session Not Found", problemcode.CodeSessionNotFound, "The session could not be located.", nil)
			return
		}
		if rec.ExpiresAtUnix > 0 && time.Now().Unix() > rec.ExpiresAtUnix {
			writeRegisteredProblem(w, r, http.StatusGone, "sessions/expired", "Session Expired", problemcode.CodeSessionGone, "This session has expired.", nil)
			return
		}
		validState := rec.State == model.SessionReady ||
			rec.State == model.SessionDraining ||
			rec.State == model.SessionStarting ||
			rec.State == model.SessionNew ||
			rec.State == model.SessionPriming
		if !validState {
			if rec.State.IsTerminal() {
				writeRegisteredProblem(w, r, http.StatusGone, "sessions/expired", "Session Ended", problemcode.CodeSessionGone, "stream ended", nil)
				return
			}
			writeRegisteredProblem(w, r, http.StatusNotFound, "sessions/not_found", "Session Not Found", problemcode.CodeSessionNotFound, "session not ready", nil)
			return
		}
		s.serveLivePreviewFrame(w, r, deps.cfg.HLS.Root, deps.cfg.HLS.SegmentSeconds, deps.cfg.FFmpeg.Bin, sessionID)
		return
	}
	if !safeHLSFilenameRouteRe.MatchString(filename) {
		writeRegisteredProblem(w, r, http.StatusForbidden, "sessions/hls_forbidden_artifact", "Access Denied", problemcode.CodeForbidden, "The requested HLS artifact is not allowed", nil)
		return
	}
	// Playlist fetches renew the session lease: an attached player refetches
	// the playlist every target duration, so consumption keeps the session
	// alive even when the client's heartbeat loop is throttled or dead.
	if filename == "index.m3u8" || filename == "stream.m3u8" {
		s.renewLeaseFromConsumption(r.Context(), sessionID)
	}
	// Low-latency HLS: serve the part-augmented playlist with blocking
	// reload when enabled; any not-ready condition falls back to the plain
	// file path below so startup semantics stay identical.
	if deps.cfg.HLS.LowLatency && filename == "index.m3u8" {
		if s.serveLLHLSPlaylist(w, r, deps, sessionID) {
			return
		}
	}

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
