// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package lifecycle

import "github.com/ManuGH/xg2g/internal/domain/session/model"

const (
	ForbiddenTerminalAbsorbing = "terminal_absorbing"
	ForbiddenOutOfOrder        = "out_of_order"
	ForbiddenAlreadyInState    = "already_in_state"
	ForbiddenRequiresStart     = "requires_start"
	ForbiddenRequiresReady     = "requires_ready"
)

func allowed() Decision        { return Decision{Allowed: true} }
func forbid(r string) Decision { return Decision{Allowed: false, Reason: r} }

// decisionTable defines an explicit decision for every State×Event combination.
var decisionTable = map[model.SessionState]map[EventKind]Decision{
	model.SessionUnknown: {
		EvStartRequested:    allowed(),
		EvPrimingStarted:    forbid(ForbiddenOutOfOrder),
		EvReady:             forbid(ForbiddenOutOfOrder),
		EvDrainRequested:    forbid(ForbiddenRequiresReady),
		EvStopRequested:     forbid(ForbiddenRequiresStart),
		EvLeaseExpired:      forbid(ForbiddenRequiresStart),
		EvSweeperForcedStop: forbid(ForbiddenRequiresStart),
		EvRecoveryReset:     forbid(ForbiddenOutOfOrder),
		EvRecoveryFail:      forbid(ForbiddenOutOfOrder),
		EvTerminalize:       allowed(),
	},
	model.SessionNew: {
		EvStartRequested:    allowed(),
		EvPrimingStarted:    forbid(ForbiddenOutOfOrder),
		EvReady:             forbid(ForbiddenOutOfOrder),
		EvDrainRequested:    forbid(ForbiddenRequiresReady),
		EvStopRequested:     allowed(),
		EvLeaseExpired:      allowed(),
		EvSweeperForcedStop: allowed(),
		EvRecoveryReset:     forbid(ForbiddenAlreadyInState),
		EvRecoveryFail:      forbid(ForbiddenOutOfOrder),
		EvTerminalize:       allowed(),
	},
	model.SessionStarting: {
		EvStartRequested:    forbid(ForbiddenAlreadyInState),
		EvPrimingStarted:    allowed(),
		EvReady:             forbid(ForbiddenOutOfOrder),
		EvDrainRequested:    forbid(ForbiddenRequiresReady),
		EvStopRequested:     allowed(),
		EvLeaseExpired:      allowed(),
		EvSweeperForcedStop: allowed(),
		EvRecoveryReset:     allowed(),
		EvRecoveryFail:      forbid(ForbiddenOutOfOrder),
		EvTerminalize:       allowed(),
	},
	model.SessionPriming: {
		EvStartRequested:    forbid(ForbiddenAlreadyInState),
		EvPrimingStarted:    forbid(ForbiddenAlreadyInState),
		EvReady:             allowed(),
		EvDrainRequested:    forbid(ForbiddenRequiresReady),
		EvStopRequested:     allowed(),
		EvLeaseExpired:      allowed(),
		EvSweeperForcedStop: allowed(),
		EvRecoveryReset:     forbid(ForbiddenOutOfOrder),
		EvRecoveryFail:      allowed(),
		EvTerminalize:       allowed(),
	},
	model.SessionReady: {
		EvStartRequested:    forbid(ForbiddenAlreadyInState),
		EvPrimingStarted:    forbid(ForbiddenOutOfOrder),
		EvReady:             forbid(ForbiddenAlreadyInState),
		EvDrainRequested:    allowed(),
		EvStopRequested:     allowed(),
		EvLeaseExpired:      allowed(),
		EvSweeperForcedStop: allowed(),
		EvRecoveryReset:     forbid(ForbiddenOutOfOrder),
		EvRecoveryFail:      allowed(),
		EvTerminalize:       allowed(),
	},
	model.SessionDraining: {
		EvStartRequested:    forbid(ForbiddenAlreadyInState),
		EvPrimingStarted:    forbid(ForbiddenOutOfOrder),
		EvReady:             forbid(ForbiddenOutOfOrder),
		EvDrainRequested:    forbid(ForbiddenAlreadyInState),
		EvStopRequested:     allowed(),
		EvLeaseExpired:      allowed(),
		EvSweeperForcedStop: allowed(),
		EvRecoveryReset:     forbid(ForbiddenOutOfOrder),
		EvRecoveryFail:      allowed(),
		EvTerminalize:       allowed(),
	},
	model.SessionStopping: {
		EvStartRequested:    forbid(ForbiddenAlreadyInState),
		EvPrimingStarted:    forbid(ForbiddenOutOfOrder),
		EvReady:             forbid(ForbiddenOutOfOrder),
		EvDrainRequested:    forbid(ForbiddenOutOfOrder),
		EvStopRequested:     forbid(ForbiddenAlreadyInState),
		EvLeaseExpired:      allowed(),
		EvSweeperForcedStop: allowed(),
		EvRecoveryReset:     forbid(ForbiddenOutOfOrder),
		EvRecoveryFail:      allowed(),
		EvTerminalize:       allowed(),
	},
	model.SessionFailed: {
		EvStartRequested:    forbid(ForbiddenTerminalAbsorbing),
		EvPrimingStarted:    forbid(ForbiddenTerminalAbsorbing),
		EvReady:             forbid(ForbiddenTerminalAbsorbing),
		EvDrainRequested:    forbid(ForbiddenTerminalAbsorbing),
		EvStopRequested:     forbid(ForbiddenTerminalAbsorbing),
		EvLeaseExpired:      forbid(ForbiddenTerminalAbsorbing),
		EvSweeperForcedStop: forbid(ForbiddenTerminalAbsorbing),
		EvRecoveryReset:     forbid(ForbiddenTerminalAbsorbing),
		EvRecoveryFail:      forbid(ForbiddenTerminalAbsorbing),
		EvTerminalize:       forbid(ForbiddenTerminalAbsorbing),
	},
	model.SessionCancelled: {
		EvStartRequested:    forbid(ForbiddenTerminalAbsorbing),
		EvPrimingStarted:    forbid(ForbiddenTerminalAbsorbing),
		EvReady:             forbid(ForbiddenTerminalAbsorbing),
		EvDrainRequested:    forbid(ForbiddenTerminalAbsorbing),
		EvStopRequested:     forbid(ForbiddenTerminalAbsorbing),
		EvLeaseExpired:      forbid(ForbiddenTerminalAbsorbing),
		EvSweeperForcedStop: forbid(ForbiddenTerminalAbsorbing),
		EvRecoveryReset:     forbid(ForbiddenTerminalAbsorbing),
		EvRecoveryFail:      forbid(ForbiddenTerminalAbsorbing),
		EvTerminalize:       forbid(ForbiddenTerminalAbsorbing),
	},
	model.SessionStopped: {
		EvStartRequested:    forbid(ForbiddenTerminalAbsorbing),
		EvPrimingStarted:    forbid(ForbiddenTerminalAbsorbing),
		EvReady:             forbid(ForbiddenTerminalAbsorbing),
		EvDrainRequested:    forbid(ForbiddenTerminalAbsorbing),
		EvStopRequested:     forbid(ForbiddenTerminalAbsorbing),
		EvLeaseExpired:      forbid(ForbiddenTerminalAbsorbing),
		EvSweeperForcedStop: forbid(ForbiddenTerminalAbsorbing),
		EvRecoveryReset:     forbid(ForbiddenTerminalAbsorbing),
		EvRecoveryFail:      forbid(ForbiddenTerminalAbsorbing),
		EvTerminalize:       forbid(ForbiddenTerminalAbsorbing),
	},
}

// DecisionFor returns the explicit decision for state×event.
func DecisionFor(from model.SessionState, ev EventKind) (Decision, bool) {
	m, ok := decisionTable[from]
	if !ok {
		return Decision{}, false
	}
	d, ok := m[ev]
	return d, ok
}
