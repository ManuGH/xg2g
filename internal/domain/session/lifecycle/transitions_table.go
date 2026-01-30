// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package lifecycle

import "github.com/ManuGH/xg2g/internal/domain/session/model"

// Transition is a single allowed edge in the lifecycle state machine.
type Transition struct {
	From       model.SessionState
	To         model.SessionState
	Event      EventKind
	Reason     model.ReasonCode
	DetailCode model.ReasonDetailCode
	DetailDebug string
}

// Decision records whether a transition is allowed and why it is forbidden.
type Decision struct {
	Allowed bool
	Reason  string
}

var transitionsTable = []Transition{
	// Start path
	{From: model.SessionUnknown, To: model.SessionStarting, Event: EvStartRequested},
	{From: model.SessionNew, To: model.SessionStarting, Event: EvStartRequested},
	{From: model.SessionStarting, To: model.SessionPriming, Event: EvPrimingStarted},
	{From: model.SessionPriming, To: model.SessionReady, Event: EvReady},

	// Drain path
	{From: model.SessionReady, To: model.SessionDraining, Event: EvDrainRequested},

	// Stop intent (non-terminal; terminalization handled by EvTerminalize)
	{From: model.SessionNew, To: model.SessionStopping, Event: EvStopRequested},
	{From: model.SessionStarting, To: model.SessionStopping, Event: EvStopRequested},
	{From: model.SessionPriming, To: model.SessionStopping, Event: EvStopRequested},
	{From: model.SessionReady, To: model.SessionStopping, Event: EvStopRequested},
	{From: model.SessionDraining, To: model.SessionStopping, Event: EvStopRequested},

	// Recovery transitions
	{From: model.SessionStarting, To: model.SessionNew, Event: EvRecoveryReset},
	{From: model.SessionPriming, To: model.SessionFailed, Event: EvRecoveryFail},
	{From: model.SessionStopping, To: model.SessionFailed, Event: EvRecoveryFail},
	{From: model.SessionDraining, To: model.SessionFailed, Event: EvRecoveryFail},
	{From: model.SessionReady, To: model.SessionFailed, Event: EvRecoveryFail},

	// Lease/sweeper forced stops
	{From: model.SessionNew, To: model.SessionStopped, Event: EvLeaseExpired, Reason: model.RLeaseExpired},
	{From: model.SessionStarting, To: model.SessionStopped, Event: EvLeaseExpired, Reason: model.RLeaseExpired},
	{From: model.SessionPriming, To: model.SessionStopped, Event: EvLeaseExpired, Reason: model.RLeaseExpired},
	{From: model.SessionReady, To: model.SessionStopped, Event: EvLeaseExpired, Reason: model.RLeaseExpired},
	{From: model.SessionDraining, To: model.SessionStopped, Event: EvLeaseExpired, Reason: model.RLeaseExpired},
	{From: model.SessionStopping, To: model.SessionStopped, Event: EvLeaseExpired, Reason: model.RLeaseExpired},

	{From: model.SessionNew, To: model.SessionStopped, Event: EvSweeperForcedStop, Reason: model.RIdleTimeout, DetailCode: model.DSweeperForcedStopStuck},
	{From: model.SessionStarting, To: model.SessionStopped, Event: EvSweeperForcedStop, Reason: model.RIdleTimeout, DetailCode: model.DSweeperForcedStopStuck},
	{From: model.SessionPriming, To: model.SessionStopped, Event: EvSweeperForcedStop, Reason: model.RIdleTimeout, DetailCode: model.DSweeperForcedStopStuck},
	{From: model.SessionReady, To: model.SessionStopped, Event: EvSweeperForcedStop, Reason: model.RIdleTimeout, DetailCode: model.DSweeperForcedStopStuck},
	{From: model.SessionDraining, To: model.SessionStopped, Event: EvSweeperForcedStop, Reason: model.RIdleTimeout, DetailCode: model.DSweeperForcedStopStuck},
	{From: model.SessionStopping, To: model.SessionStopped, Event: EvSweeperForcedStop, Reason: model.RIdleTimeout, DetailCode: model.DSweeperForcedStopStuck},
}

// TransitionFor returns the allowed transition for a given state+event.
func TransitionFor(from model.SessionState, ev EventKind) (Transition, bool) {
	for _, tr := range transitionsTable {
		if tr.From == from && tr.Event == ev {
			return tr, true
		}
	}
	return Transition{}, false
}
