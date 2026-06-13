// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package resilience

import (
	"errors"
	"testing"
	"time"
)

// TestExecuteTripsOnFailureThreshold ensures the breaker actually opens when used
// through Execute(). Before the fix, Execute() never recorded an attempt, so the
// `attempts >= minAttempts` gate in evaluate() was never satisfied and the breaker
// stayed closed forever regardless of how many calls failed.
func TestExecuteTripsOnFailureThreshold(t *testing.T) {
	// threshold=2 failures, minAttempts=3, large window.
	cb := NewCircuitBreaker("test", 2, 3, time.Minute, time.Minute)
	failing := func() error { return errors.New("boom") }

	for i := 0; i < 3; i++ {
		if err := cb.Execute(failing); err == nil {
			t.Fatalf("call %d: expected the wrapped error, got nil", i)
		}
	}

	if got := cb.GetState(); got != StateOpen {
		t.Fatalf("breaker state after 3 failing Execute calls: got %v, want StateOpen", got)
	}
	// Once open, further calls are short-circuited.
	if err := cb.Execute(failing); !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("expected ErrCircuitOpen once tripped, got %v", err)
	}
}
