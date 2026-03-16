// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"net/http"

	v3sessions "github.com/ManuGH/xg2g/internal/control/http/v3/sessions"
)

func writeSessionStateServiceError(w http.ResponseWriter, r *http.Request, hlsRoot string, err *v3sessions.GetSessionError) {
	if err == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrInternalServer, "unknown session state error")
		return
	}

	switch err.Kind {
	case v3sessions.GetSessionErrorUnavailable:
		RespondError(w, r, http.StatusServiceUnavailable, ErrV3StoreNotInitialized)
	case v3sessions.GetSessionErrorInvalidInput:
		RespondError(w, r, http.StatusBadRequest, ErrInvalidInput, "invalid session id")
	case v3sessions.GetSessionErrorNotFound:
		RespondError(w, r, http.StatusNotFound, ErrSessionNotFound)
	case v3sessions.GetSessionErrorTerminal:
		terminal := err.Terminal
		if terminal == nil || terminal.Session == nil {
			RespondError(w, r, http.StatusNotFound, ErrSessionNotFound)
			return
		}

		trace := mapSessionPlaybackTrace(requestID(r.Context()), terminal.Session, hlsRoot)
		problemSpec := mapTerminalProblem(terminal.Outcome)
		state := string(mapSessionState(terminal.Outcome.State))
		reason, ok := mapSessionReason(terminal.Outcome.Reason)
		extra := map[string]any{
			"session":       terminal.Session.SessionID,
			"state":         state,
			"reason_detail": mapDetailCode(terminal.Outcome.DetailCode),
		}
		if ok {
			extra["reason"] = string(reason)
		}
		if trace != nil {
			extra["trace"] = trace
		}

		writeProblem(w, r, http.StatusGone,
			problemSpec.problemType,
			problemSpec.title,
			problemSpec.code,
			problemSpec.detail,
			extra,
		)
	default:
		RespondError(w, r, http.StatusInternalServerError, ErrInternalServer, err.Error())
	}
}
