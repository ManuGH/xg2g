package sessions

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/lifecycle"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/problemcode"
)

// GetSession resolves the canonical session read model for GET /sessions/{sessionID}.
func (s *Service) GetSession(ctx context.Context, req GetSessionRequest) (GetSessionResult, *GetSessionError) {
	store := s.deps.SessionStore()
	if store == nil {
		return GetSessionResult{}, &GetSessionError{
			Kind:    GetSessionErrorUnavailable,
			Message: "session store is not initialized",
		}
	}

	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" || !model.IsSafeSessionID(sessionID) {
		return GetSessionResult{}, &GetSessionError{
			Kind:    GetSessionErrorInvalidInput,
			Message: "invalid session id",
		}
	}

	session, err := store.GetSession(ctx, sessionID)
	if err != nil || session == nil {
		return GetSessionResult{}, &GetSessionError{
			Kind:    GetSessionErrorNotFound,
			Message: "session not found",
			Cause:   err,
		}
	}

	outcome := lifecycle.PublicOutcomeFromRecord(session)
	if session.State.IsTerminal() {
		return GetSessionResult{}, &GetSessionError{
			Kind: GetSessionErrorTerminal,
			Terminal: &GetSessionTerminal{
				Session:      session,
				Outcome:      outcome,
				State:        outcome.State,
				Reason:       outcome.Reason,
				ReasonDetail: mapReasonDetailCode(outcome.DetailCode),
				Code:         terminalProblemCode(outcome),
				Detail:       terminalProblemDetail(outcome),
			},
		}
	}

	return GetSessionResult{
		Session:      session,
		Outcome:      outcome,
		PlaybackInfo: sessionPlaybackInfo(session, req.Now),
	}, nil
}

func sessionPlaybackInfo(session *model.SessionRecord, now time.Time) SessionPlaybackInfo {
	mode := model.ModeLive
	if session.ContextData != nil {
		if raw := strings.TrimSpace(session.ContextData[model.CtxKeyMode]); raw != "" {
			mode = strings.ToUpper(raw)
		}
	}
	if mode != model.ModeLive && mode != model.ModeRecording {
		mode = model.ModeLive
	}

	if mode == model.ModeRecording {
		durationSeconds := parseContextSeconds(session.ContextData, model.CtxKeyDurationSeconds)
		if durationSeconds == nil {
			return SessionPlaybackInfo{Mode: mode}
		}
		zero := 0.0
		return SessionPlaybackInfo{
			Mode:                 mode,
			DurationSeconds:      durationSeconds,
			SeekableStartSeconds: &zero,
			SeekableEndSeconds:   durationSeconds,
		}
	}

	var durationSeconds *float64
	if session.Profile.DVRWindowSec > 0 {
		value := float64(session.Profile.DVRWindowSec)
		durationSeconds = &value
	}

	nowUnix := session.LastAccessUnix
	if nowUnix == 0 {
		nowUnix = session.UpdatedAtUnix
	}
	if nowUnix == 0 {
		if now.IsZero() {
			now = time.Now()
		}
		nowUnix = now.Unix()
	}

	startUnix := session.CreatedAtUnix
	if startUnix == 0 {
		startUnix = session.UpdatedAtUnix
	}
	if startUnix == 0 {
		startUnix = nowUnix
	}

	liveEdge := float64(nowUnix - startUnix)
	if liveEdge < 0 {
		liveEdge = 0
	}

	seekableStart := liveEdge
	if durationSeconds != nil && *durationSeconds > 0 {
		seekableStart = liveEdge - *durationSeconds
		if seekableStart < 0 {
			seekableStart = 0
		}
	}

	return SessionPlaybackInfo{
		Mode:                 mode,
		DurationSeconds:      durationSeconds,
		SeekableStartSeconds: &seekableStart,
		SeekableEndSeconds:   &liveEdge,
		LiveEdgeSeconds:      &liveEdge,
	}
}

func parseContextSeconds(ctx map[string]string, key string) *float64 {
	if ctx == nil {
		return nil
	}
	raw := strings.TrimSpace(ctx[key])
	if raw == "" {
		return nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || value <= 0 {
		return nil
	}
	return &value
}

func mapReasonDetailCode(code model.ReasonDetailCode) string {
	switch code {
	case model.DContextCanceled:
		return "context canceled"
	case model.DDeadlineExceeded:
		return "deadline exceeded"
	case model.DRecordingComplete:
		return "recording completed"
	case model.DSweeperForcedStopStuck:
		return "sweeper_forced_stop_stuck"
	case model.DInternalInvariantBreach:
		return "internal invariant breach"
	case model.DProcessEndedStartup:
		return "process ended during startup"
	case model.DProcessExitedUnexpectedly:
		return "process exited unexpectedly"
	case model.DTranscodeStalled:
		return "transcode stalled - no progress detected"
	case model.DUpstreamEndedPrematurely:
		return "upstream stream ended prematurely"
	case model.DUpstreamInputOpenFailed:
		return "failed to open upstream input"
	case model.DInvalidUpstreamInput:
		return "invalid upstream input data"
	default:
		return ""
	}
}

func terminalProblemCode(outcome lifecycle.PublicOutcome) string {
	if outcome.State == model.SessionFailed && outcome.DetailCode == model.DTranscodeStalled {
		return problemcode.CodeTranscodeStalled
	}
	return problemcode.CodeSessionGone
}

func terminalProblemDetail(outcome lifecycle.PublicOutcome) string {
	if outcome.State == model.SessionFailed && outcome.DetailCode == model.DTranscodeStalled {
		return "The session failed because the transcode process stopped producing progress."
	}
	return "Session is in a terminal state (stopped, failed, or cancelled)."
}
