// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"github.com/ManuGH/xg2g/internal/domain/session/lifecycle"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/problemcode"
)

type terminalProblemSpec struct {
	problemType string
	title       string
	code        string
	detail      string
}

// mapSessionState converts the domain session state to the API enum.
func mapSessionState(s model.SessionState) SessionResponseState {
	switch s {
	case model.SessionStarting:
		return SessionResponseStateSTARTING
	case model.SessionPriming:
		return SessionResponseStatePRIMING
	case model.SessionReady:
		return SessionResponseStateREADY
	case model.SessionDraining:
		return SessionResponseStateDRAINING
	case model.SessionStopping:
		return SessionResponseStateSTOPPING
	case model.SessionStopped:
		return SessionResponseStateSTOPPED
	case model.SessionFailed:
		return SessionResponseStateFAILED
	case model.SessionCancelled:
		return SessionResponseStateCANCELLED
	default:
		return SessionResponseStateIDLE
	}
}

// mapSessionReason converts the domain reason to the API enum.
// Returns ok=false when no reason should be exposed.
func mapSessionReason(r model.ReasonCode) (SessionResponseReason, bool) {
	if r == "" {
		return "", false
	}
	return SessionResponseReason(r), true
}

// mapDetailCode converts the domain reason detail code to the public API string.
// mapDetailCode renders a detail code as public text. Both HTTP surfaces delegate
// to the single table on the model so they cannot drift apart again — the copy
// serving this endpoint had already lost the scrambled case, returning an empty
// reason_detail for a failure that carried its text everywhere else.
func mapDetailCode(code model.ReasonDetailCode) string {
	return code.Text()
}

// mapTerminalProblem is the SSOT for terminal session ProblemDetails mapping.
func mapTerminalProblem(out lifecycle.PublicOutcome) terminalProblemSpec {
	if out.State == model.SessionFailed {
		switch out.DetailCode {
		case model.DTranscodeStalled:
			return mapAPIErrorProblem(ErrTranscodeStalled, "The session failed because the transcode process stopped producing progress.")
		}
	}

	return problemSpecForCode(problemcode.CodeSessionGone, "", "Session is in a terminal state (stopped, failed, or cancelled).")
}

func mapAPIErrorProblem(apiErr *APIError, detail string) terminalProblemSpec {
	return problemSpecForAPIError(apiErr, detail)
}
