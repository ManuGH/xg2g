// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package lifecycle

import "github.com/ManuGH/xg2g/internal/domain/session/model"

// EventKind is a domain event in the session lifecycle.
type EventKind int

const (
	EvUnknown EventKind = iota
	EvStartRequested
	EvPrimingStarted
	EvReady
	EvDrainRequested
	EvStopRequested
	EvLeaseExpired
	EvSweeperForcedStop
	EvRecoveryReset
	EvRecoveryFail
	EvTerminalize // Derived from cause/phase/stopIntent
)

// Event carries optional domain metadata for a transition.
type Event struct {
	Kind   EventKind
	Reason model.ReasonCode
	DetailCode model.ReasonDetailCode
}
