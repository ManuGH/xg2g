// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package lifecycle

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/stretchr/testify/require"
)

func TestTransitionTable_Coverage(t *testing.T) {
	states := []model.SessionState{
		model.SessionUnknown,
		model.SessionNew,
		model.SessionStarting,
		model.SessionPriming,
		model.SessionReady,
		model.SessionDraining,
		model.SessionStopping,
		model.SessionFailed,
		model.SessionCancelled,
		model.SessionStopped,
	}
	events := []EventKind{
		EvStartRequested,
		EvPrimingStarted,
		EvReady,
		EvDrainRequested,
		EvStopRequested,
		EvLeaseExpired,
		EvSweeperForcedStop,
		EvRecoveryReset,
		EvRecoveryFail,
		EvTerminalize,
	}

	allowed := map[model.SessionState]map[EventKind]struct{}{}
	for _, tr := range transitionsTable {
		if _, ok := allowed[tr.From]; !ok {
			allowed[tr.From] = map[EventKind]struct{}{}
		}
		if _, exists := allowed[tr.From][tr.Event]; exists {
			t.Fatalf("duplicate transition: %s + %v", tr.From, tr.Event)
		}
		allowed[tr.From][tr.Event] = struct{}{}
	}

	for _, state := range states {
		for _, ev := range events {
			if _, ok := allowed[state][ev]; ok {
				decision, ok := DecisionFor(state, ev)
				require.True(t, ok, "missing decision for %s + %v", state, ev)
				require.True(t, decision.Allowed, "allowed transition must be marked allowed for %s + %v", state, ev)
				continue
			}
			decision, ok := DecisionFor(state, ev)
			require.True(t, ok, "missing decision for %s + %v", state, ev)
			if ev == EvTerminalize {
				// Terminalize is handled outside the transition table.
				continue
			}
			require.False(t, decision.Allowed, "forbidden transition must be marked forbidden for %s + %v", state, ev)
			require.NotEmpty(t, decision.Reason, "forbidden transition must have reason for %s + %v", state, ev)
		}
	}
}
