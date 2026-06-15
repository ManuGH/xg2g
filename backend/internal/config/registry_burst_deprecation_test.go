// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package config

import "testing"

// TestRateLimitBurst_IsDeprecatedNotActive is the drift guard for M21. The API rate limiter
// is window-based and has no burst capacity, so api.rateLimit.burst is inert and must NOT be
// advertised as Active on the config surface (that mislabel is the bug — it tells an operator
// the knob works when it does nothing). This goes RED if someone flips it back to Active
// without actually wiring a burst-capable limiter — i.e. it guards the recurrence of exactly
// the lie M21 corrected. The sibling rateLimit.global/auth stay Active (they ARE wired).
func TestRateLimitBurst_IsDeprecatedNotActive(t *testing.T) {
	reg, err := GetRegistry()
	if err != nil {
		t.Fatalf("GetRegistry: %v", err)
	}

	entry, ok := reg.ByPath["rateLimit.burst"]
	if !ok {
		t.Fatal("rateLimit.burst missing from registry — it must stay registered (parseable) so existing configs don't break under KnownFields(true)")
	}
	if entry.Status != StatusDeprecated {
		t.Errorf("rateLimit.burst Status = %q, want %q — the API limiter has no burst capacity; advertising it as active mislabels an inert knob", entry.Status, StatusDeprecated)
	}

	// Guard the separation: the wired sibling knobs must remain Active.
	for _, p := range []string{"rateLimit.global", "rateLimit.auth"} {
		if e, ok := reg.ByPath[p]; ok && e.Status != StatusActive {
			t.Errorf("%s Status = %q, want Active (it is wired and functional)", p, e.Status)
		}
	}
}
