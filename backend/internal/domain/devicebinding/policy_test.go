// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package devicebinding

import "testing"

func TestBoundDeviceIsAllowed(t *testing.T) {
	decision := Evaluate(StateBound)

	if decision.Outcome != OutcomeAllow {
		t.Errorf("outcome = %s, want %s", decision.Outcome, OutcomeAllow)
	}
	if decision.Denied() || decision.RequiresRePairing() {
		t.Error("a bound device must not be refused or asked to re-pair")
	}
}

// Hard cutover: an unbound credential is refused immediately, with no window.
func TestLegacyDeviceIsRefused(t *testing.T) {
	decision := Evaluate(StateLegacyUnbound)

	if decision.Outcome != OutcomeDenyRepairRequired {
		t.Fatalf("outcome = %s, want %s", decision.Outcome, OutcomeDenyRepairRequired)
	}
	if !decision.Denied() {
		t.Error("an unbound credential must be refused")
	}
	if !decision.RequiresRePairing() {
		t.Error("the refusal must state the required user action")
	}
}

// An unrecognised state must never be promoted to bound. Assuming security that
// was never established is the one direction this must not fail in.
func TestUnknownStateIsRefused(t *testing.T) {
	decision := Evaluate(State("something-new"))

	if decision.Outcome != OutcomeDenyRepairRequired {
		t.Errorf("outcome = %s, want %s", decision.Outcome, OutcomeDenyRepairRequired)
	}
	if decision.State != StateLegacyUnbound {
		t.Errorf("state = %s, want %s", decision.State, StateLegacyUnbound)
	}
}
