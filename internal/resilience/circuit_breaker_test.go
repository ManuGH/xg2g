// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package resilience

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// fakeClock abstracts time for deterministic testing
type fakeClock struct {
	now time.Time
}

func (f *fakeClock) Now() time.Time { return f.now }

func (f *fakeClock) Advance(d time.Duration) { f.now = f.now.Add(d) }

func TestCircuitBreaker_StateTransitions(t *testing.T) {
	clk := &fakeClock{now: time.Now()}
	cb := NewCircuitBreaker("test_cb", 2, 100*time.Millisecond, WithClock(clk))

	// Initial state: Closed
	assert.Equal(t, "closed", cb.State())

	// 1st Failure: Should remain Closed
	err := cb.Execute(func() error { return errors.New("fail") })
	assert.Error(t, err)
	assert.Equal(t, "closed", cb.State())

	// 2nd Failure: Should switch to Open
	err = cb.Execute(func() error { return errors.New("fail") })
	assert.Error(t, err)
	assert.Equal(t, "open", cb.State())

	// Request while Open: Should return ErrCircuitOpen immediately
	err = cb.Execute(func() error { return nil })
	assert.True(t, errors.Is(err, ErrCircuitOpen))

	// Advance time past timeout
	clk.Advance(150 * time.Millisecond)

	// Next request: Should be allowed (HalfOpen) -> Success -> Closed
	err = cb.Execute(func() error { return nil })
	assert.NoError(t, err)
	assert.Equal(t, "closed", cb.State())
}

func TestCircuitBreaker_HalfOpenFailure(t *testing.T) {
	clk := &fakeClock{now: time.Now()}
	cb := NewCircuitBreaker("test_cb", 1, 100*time.Millisecond, WithClock(clk))

	// Open the circuit
	_ = cb.Execute(func() error { return errors.New("fail") })
	assert.Equal(t, "open", cb.State())

	// Wait for reset
	clk.Advance(150 * time.Millisecond)

	// HalfOpen failure: Should go back to Open
	err := cb.Execute(func() error { return errors.New("fail") })
	assert.Error(t, err)
	assert.Equal(t, "open", cb.State())
}

func TestCircuitBreaker_PanicRecovery(t *testing.T) {
	cb := NewCircuitBreaker("panic_cb", 1, time.Minute, WithPanicRecovery(true))

	// Execute function that panics
	assert.Panics(t, func() {
		_ = cb.Execute(func() error {
			panic("oops")
		})
	})

	// Should have counted as a failure and opened the circuit
	assert.Equal(t, "open", cb.State())
}

func TestCircuitBreaker_NoPanicRecovery(t *testing.T) {
	cb := NewCircuitBreaker("no_panic_cb", 1, time.Minute, WithPanicRecovery(false))

	// Execute function that panics
	assert.Panics(t, func() {
		_ = cb.Execute(func() error {
			panic("oops")
		})
	})

	// Should NOT have counted as a failure (failures incremented manually)
	// Actually, if it panics and we don't recover, we don't hit recordFailure line 113.
	// So state remains closed.
	assert.Equal(t, "closed", cb.State())
}
