// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package resilience

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type mockClock struct {
	now time.Time
}

func (m *mockClock) Now() time.Time { return m.now }

func TestCircuitBreaker_SlidingWindowPruning(t *testing.T) {
	clock := &mockClock{now: time.Now()}
	cb := NewCircuitBreaker("test", 3, 5, 10*time.Second, 30*time.Second, WithClock(clock))

	for i := 0; i < 10; i++ {
		cb.RecordAttempt()
		clock.now = clock.now.Add(1 * time.Second)
	}

	assert.Equal(t, 10, len(cb.events))

	// Move time beyond some events
	clock.now = clock.now.Add(5 * time.Second)
	cb.AllowRequest() // Triggers prune

	// Cutoff is now - 10s. events were [T0, T1, ... T9].
	// now is T0+15. Cutoff is T0+5.
	// events [T0..T4] should be pruned.
	assert.Equal(t, 5, len(cb.events))
}

func TestCircuitBreaker_TechnicalFailureTrigger(t *testing.T) {
	clock := &mockClock{now: time.Now()}
	// Threshold 3, MinAttempts 5
	cb := NewCircuitBreaker("test", 3, 5, 60*time.Second, 30*time.Second, WithClock(clock))

	// 1. Record 2 failures; should stays CLOSED (not enough failures, not enough attempts)
	cb.RecordAttempt()
	cb.RecordTechnicalFailure()
	cb.RecordAttempt()
	cb.RecordTechnicalFailure()
	assert.Equal(t, StateClosed, cb.GetState())

	// 2. Record more attempts to reach minAttempts=5
	cb.RecordAttempt()
	cb.RecordAttempt()
	cb.RecordAttempt() // total 5 attempts
	assert.Equal(t, StateClosed, cb.GetState())

	// 3. Record 3rd failure -> trips to OPEN
	cb.RecordTechnicalFailure()
	assert.Equal(t, StateOpen, cb.GetState())
}

func TestCircuitBreaker_Exclusions(t *testing.T) {
	clock := &mockClock{now: time.Now()}
	cb := NewCircuitBreaker("test", 2, 2, 60*time.Second, 30*time.Second, WithClock(clock))

	// Capacity FULL or Canceled should not be recorded as failure.
	// Only Success or TechFailure are recorded in our model.
	// In the real system, we just DON'T call RecordTechnicalFailure for these.

	cb.RecordAttempt()
	// Simulate success for capacity exceeded/canceled (per plan: no breaker penalty)
	cb.RecordSuccess()
	assert.Equal(t, StateClosed, cb.GetState())

	cb.RecordAttempt()
	cb.RecordTechnicalFailure()
	assert.Equal(t, StateClosed, cb.GetState(), "Only 1 failure; threshold is 2")
}

func TestCircuitBreaker_HalfOpenBehavior(t *testing.T) {
	clock := &mockClock{now: time.Now()}
	cb := NewCircuitBreaker("test", 1, 1, 60*time.Second, 10*time.Second, WithClock(clock), WithHalfOpenSuccessThreshold(2))

	// 1. Trip it
	cb.RecordAttempt()
	cb.RecordTechnicalFailure()
	assert.Equal(t, StateOpen, cb.GetState())

	// 2. Wait for reset timeout
	clock.now = clock.now.Add(11 * time.Second)
	assert.True(t, cb.AllowRequest(), "Should allow in half-open")
	assert.Equal(t, StateHalfOpen, cb.GetState())

	// 3. One success; stays HALF_OPEN (need 2)
	cb.RecordSuccess()
	assert.Equal(t, StateHalfOpen, cb.GetState())

	// 4. One tech failure in HALF_OPEN -> immediately OPEN
	cb.RecordTechnicalFailure()
	assert.Equal(t, StateOpen, cb.GetState())

	// 5. Recover again
	clock.now = clock.now.Add(11 * time.Second)
	cb.AllowRequest()
	assert.Equal(t, StateHalfOpen, cb.GetState())

	// 6. Two successes -> CLOSED
	cb.RecordSuccess()
	cb.RecordSuccess()
	assert.Equal(t, StateClosed, cb.GetState())
}

func TestCircuitBreaker_BoundedMemory(t *testing.T) {
	clock := &mockClock{now: time.Now()}
	cb := NewCircuitBreaker("test", 3, 5, 60*time.Second, 30*time.Second, WithClock(clock))

	// Fill with 10k events over 10 minutes
	for i := 0; i < 600; i++ {
		cb.RecordAttempt()
		clock.now = clock.now.Add(1 * time.Second)
	}

	// Should have approx 60 samples (last 60s)
	// On every RecordAttempt it prunes.
	assert.LessOrEqual(t, len(cb.events), 61)
}
