package v3

import (
	"context"
	"errors"
	"net/http"
	"syscall"

	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/metrics"
)

func isClientDisconnect(err error, reqCtx context.Context) bool {
	if err == nil {
		return false
	}
	if reqCtx != nil && errors.Is(reqCtx.Err(), context.Canceled) {
		return true
	}
	return errors.Is(err, context.Canceled) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.EPIPE)
}

func (s *Server) handleRecordingCopyError(
	w http.ResponseWriter,
	r *http.Request,
	deps recordingsModuleDeps,
	stage string,
	mode string,
	sessionID string,
	recordingID string,
	err error,
) {
	if err == nil {
		return
	}

	if isClientDisconnect(err, r.Context()) {
		if deps.playbackSLO != nil {
			outcome := deps.playbackSLO.MarkOutcome(playbackSessionMeta{
				SessionID:   sessionID,
				Schema:      playbackSchemaRecordingLabel,
				Mode:        mode,
				RecordingID: recordingID,
			}, "aborted")
			if outcome.TTFFObserved {
				log.L().Info().
					Str("event", "playback.slo.ttff").
					Str("request_id", requestID(r.Context())).
					Str("session_id", sessionID).
					Str("schema", outcome.Schema).
					Str("mode", outcome.Mode).
					Str("outcome", outcome.Outcome).
					Str("recording_id", recordingID).
					Float64("ttff_seconds", outcome.TTFFSeconds).
					Msg("recording playback ttff outcome observed")
			}
		}
		return
	}

	metrics.IncPlaybackError(playbackSchemaRecordingLabel, stage, "INTERNAL_ERROR")
	if deps.playbackSLO != nil {
		outcome := deps.playbackSLO.MarkOutcome(playbackSessionMeta{
			SessionID:   sessionID,
			Schema:      playbackSchemaRecordingLabel,
			Mode:        mode,
			RecordingID: recordingID,
		}, "failed")
		if outcome.TTFFObserved {
			log.L().Info().
				Str("event", "playback.slo.ttff").
				Str("request_id", requestID(r.Context())).
				Str("session_id", sessionID).
				Str("schema", outcome.Schema).
				Str("mode", outcome.Mode).
				Str("outcome", outcome.Outcome).
				Str("recording_id", recordingID).
				Float64("ttff_seconds", outcome.TTFFSeconds).
				Msg("recording playback ttff outcome observed")
		}
	}
	if w != nil {
		// Response might already be committed by stream writes; best effort only.
		RespondError(w, r, http.StatusInternalServerError, ErrInternalServer, "stream write failed")
	}
}
