// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import "github.com/ManuGH/xg2g/internal/domain/session/model"

// mapSessionState converts the domain session state to the API enum.
func mapSessionState(s model.SessionState) SessionResponseState {
	switch s {
	case model.SessionStarting:
		return STARTING
	case model.SessionPriming:
		return PRIMING
	case model.SessionReady:
		return READY
	case model.SessionDraining:
		return DRAINING
	case model.SessionStopping:
		return STOPPING
	case model.SessionStopped:
		return STOPPED
	case model.SessionFailed:
		return FAILED
	case model.SessionCancelled:
		return CANCELLED
	default:
		return IDLE
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
func mapDetailCode(code model.ReasonDetailCode) string {
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
	default:
		return ""
	}
}
