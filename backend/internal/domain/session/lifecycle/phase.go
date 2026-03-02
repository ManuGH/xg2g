// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package lifecycle

import "github.com/ManuGH/xg2g/internal/domain/session/model"

// Phase describes the lifecycle phase relevant to terminal outcome mapping.
type Phase int

const (
	PhaseUnknown Phase = iota
	PhaseStart
	PhaseRunning
	PhaseTeardown
	PhaseVODComplete
)

func PhaseFromState(state model.SessionState) Phase {
	switch state {
	case model.SessionNew, model.SessionStarting, model.SessionPriming:
		return PhaseStart
	case model.SessionReady, model.SessionDraining:
		return PhaseRunning
	case model.SessionStopping:
		return PhaseTeardown
	default:
		return PhaseUnknown
	}
}
