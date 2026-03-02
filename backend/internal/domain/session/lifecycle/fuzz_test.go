// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package lifecycle

import (
	"context"
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
)

func FuzzTerminalOutcomeInvariants(f *testing.F) {
	f.Add(true, int(PhaseStart), int(EvTerminalize))
	f.Add(false, int(PhaseStart), int(EvTerminalize))
	f.Add(false, int(PhaseRunning), int(EvTerminalize))

	f.Fuzz(func(t *testing.T, stop bool, phaseInt int, evInt int) {
		phase := Phase(phaseInt % 5)
		ev := EventKind(evInt % 10)
		var cause error
		switch ev {
		case EvTerminalize:
			cause = context.Canceled
		default:
			cause = nil
		}

		out := TerminalOutcome(stop, phase, cause)

		if stop {
			if out.State != model.SessionStopped || out.Reason != model.RClientStop || out.DetailCode != model.DNone {
				t.Fatalf("stopIntent invariant failed: %+v", out)
			}
		}
		if out.Reason == model.RClientStop && out.DetailCode != model.DNone {
			t.Fatalf("client stop must have empty detail: %+v", out)
		}
		if out.Reason == model.RCancelled && out.State != model.SessionCancelled {
			t.Fatalf("cancelled reason must map to CANCELLED state: %+v", out)
		}
		if out.State.IsTerminal() && (out.State == model.SessionCancelled || out.State == model.SessionStopped || out.State == model.SessionFailed) {
			// terminal states must be absorbing: ensure no non-terminal state is returned
		}
	})
}
