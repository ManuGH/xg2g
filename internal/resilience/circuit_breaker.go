// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package resilience

import (
	"errors"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/metrics"
)

// State represents the circuit breaker state.
type State string

const (
	StateClosed   State = "closed"
	StateOpen     State = "open"
	StateHalfOpen State = "half-open"
)

var (
	ErrCircuitOpen = errors.New("circuit breaker is open")
)

// clock abstracts time operations for testability.
type clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// CircuitBreaker implements a state machine to prevent cascading failures.
// It combines the safety features of previous implementations (panic recovery)
// with the robust state management of the OpenWebIF implementation.
type CircuitBreaker struct {
	mu           sync.Mutex
	name         string // Component name for metrics
	state        State
	failures     int
	threshold    int
	resetTimeout time.Duration
	openedAt     time.Time
	clock        clock

	// Optional: Panic recovery handler
	// If set, panics in the executed function will be caught, recorded as failure, and then re-panicked.
	recoverPanic bool
}

// Option configuration pattern
type Option func(*CircuitBreaker)

func WithClock(c clock) Option {
	return func(cb *CircuitBreaker) { cb.clock = c }
}

func WithPanicRecovery(enabled bool) Option {
	return func(cb *CircuitBreaker) { cb.recoverPanic = enabled }
}

// NewCircuitBreaker creates a new circuit breaker.
func NewCircuitBreaker(name string, threshold int, resetTimeout time.Duration, opts ...Option) *CircuitBreaker {
	if threshold <= 0 {
		threshold = 3
	}
	if resetTimeout <= 0 {
		resetTimeout = 30 * time.Second
	}

	cb := &CircuitBreaker{
		name:         name,
		state:        StateClosed,
		threshold:    threshold,
		resetTimeout: resetTimeout,
		clock:        realClock{},
		recoverPanic: false, // Default to false (explicit opt-in)
	}

	for _, opt := range opts {
		opt(cb)
	}

	// Initialize metric
	metrics.SetCircuitBreakerState(cb.name, string(cb.state))
	return cb
}

// Execute runs the given function respecting the breaker state.
func (cb *CircuitBreaker) Execute(fn func() error) (err error) {
	if !cb.allowRequest() {
		return ErrCircuitOpen
	}

	// Panic recovery handling
	if cb.recoverPanic {
		defer func() {
			if r := recover(); r != nil {
				cb.recordFailure()
				panic(r) // Re-throw to let caller handle if they wish, or crash
			}
		}()
	}

	// Run function
	err = fn()

	if err != nil {
		cb.recordFailure()
		return err
	}

	cb.recordSuccess()
	return nil
}

func (cb *CircuitBreaker) allowRequest() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state == StateClosed {
		return true
	}

	if cb.state == StateOpen {
		if cb.clock.Now().Sub(cb.openedAt) > cb.resetTimeout {
			cb.transitionTo(StateHalfOpen)
			return true
		}
		return false
	}

	// StateHalfOpen
	// In strict implementations we might allow only 1 concurrent request.
	// For simplicity and matching previous behavior, we allow it.
	return true
}

func (cb *CircuitBreaker) recordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures++

	if cb.state == StateHalfOpen {
		// Failed probe
		metrics.RecordCircuitBreakerTrip(cb.name, "half_open_failure")
		cb.transitionTo(StateOpen)
		return
	}

	if cb.state == StateClosed && cb.failures >= cb.threshold {
		metrics.RecordCircuitBreakerTrip(cb.name, "threshold_exceeded")
		cb.transitionTo(StateOpen)
	}
}

func (cb *CircuitBreaker) recordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures = 0
	if cb.state != StateClosed {
		cb.transitionTo(StateClosed)
	}
}

// transitionTo handles state transitions and updates metrics.
// Caller must hold lock.
func (cb *CircuitBreaker) transitionTo(newState State) {
	if cb.state == newState {
		return
	}
	cb.state = newState
	if newState == StateOpen {
		cb.openedAt = cb.clock.Now()
	}
	metrics.SetCircuitBreakerState(cb.name, string(newState))
}

// State returns the current state.
func (cb *CircuitBreaker) State() string {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return string(cb.state)
}
