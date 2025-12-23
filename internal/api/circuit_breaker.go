// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

// Package api provides HTTP server functionality for the xg2g application.
package api

import (
	"errors"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/metrics"
)

// clock abstracts time operations for testability.
type clock interface {
	Now() time.Time
}

// realClock uses actual system time.
type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// CircuitBreaker is a minimal circuit breaker with three states: closed, open, half-open.
// It opens after 'threshold' consecutive failures and remains open for 'timeout'.
// After timeout, it transitions to half-open and allows a single trial.
// On success, it closes; on failure/panic, it opens again.
type CircuitBreaker struct {
	mu        sync.Mutex
	failures  int
	threshold int
	timeout   time.Duration
	state     string // circuitStateClosed, circuitStateOpen, circuitStateHalfOpen
	openedAt  time.Time
	clock     clock // Time source for testing
}

var errCircuitOpen = errors.New("circuit breaker is open")

const (
	circuitStateClosed   = "closed"
	circuitStateOpen     = "open"
	circuitStateHalfOpen = "half-open"
)

// CircuitBreakerOption is a functional option for CircuitBreaker configuration.
type CircuitBreakerOption func(*CircuitBreaker)

// WithClock sets a custom clock for testing.
func WithClock(c clock) CircuitBreakerOption {
	return func(cb *CircuitBreaker) {
		cb.clock = c
	}
}

// NewCircuitBreaker creates a new circuit breaker with the specified threshold and timeout.
// Accepts optional configuration via CircuitBreakerOption.
func NewCircuitBreaker(threshold int, timeout time.Duration, opts ...CircuitBreakerOption) *CircuitBreaker {
	if threshold <= 0 {
		threshold = 3
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	cb := &CircuitBreaker{
		threshold: threshold,
		timeout:   timeout,
		state:     circuitStateClosed,
		clock:     realClock{}, // Default to real time
	}
	for _, opt := range opts {
		opt(cb)
	}
	metrics.SetCircuitBreakerState("refresh", cb.state)
	return cb
}

// Call executes fn respecting the breaker state. It records failures and panics.
func (cb *CircuitBreaker) Call(fn func() error) (err error) {
	if cb == nil {
		return fn()
	}

	cb.mu.Lock()
	prevState := cb.state
	switch cb.state {
	case circuitStateOpen:
		if cb.clock.Now().Sub(cb.openedAt) >= cb.timeout {
			cb.state = circuitStateHalfOpen
		} else {
			cb.mu.Unlock()
			return errCircuitOpen
		}
	case circuitStateHalfOpen, circuitStateClosed:
		// proceed
	default:
		cb.state = circuitStateClosed
	}
	state := cb.state
	cb.mu.Unlock()
	if state != prevState {
		metrics.SetCircuitBreakerState("refresh", state)
	}

	defer func() {
		if r := recover(); r != nil {
			// On panic, open circuit and re-panic for outer recovery
			cb.recordFailure()
			panic(r)
		}
	}()

	err = fn()
	if err != nil {
		cb.recordFailure()
		return err
	}
	cb.recordSuccess()
	return nil
}

func (cb *CircuitBreaker) recordFailure() {
	cb.mu.Lock()
	prevState := cb.state
	cb.failures++
	// If in half-open, any failure opens immediately
	if cb.state == circuitStateHalfOpen || cb.failures >= cb.threshold {
		cb.state = circuitStateOpen
		cb.openedAt = cb.clock.Now()
	}
	state := cb.state
	cb.mu.Unlock()
	if state != prevState {
		metrics.SetCircuitBreakerState("refresh", state)
		if state == circuitStateOpen {
			reason := "threshold_exceeded"
			if prevState == circuitStateHalfOpen {
				reason = "half_open_failure"
			}
			metrics.RecordCircuitBreakerTrip("refresh", reason)
		}
	}
}

func (cb *CircuitBreaker) recordSuccess() {
	cb.mu.Lock()
	prevState := cb.state
	cb.failures = 0
	cb.state = circuitStateClosed
	state := cb.state
	cb.mu.Unlock()
	if state != prevState {
		metrics.SetCircuitBreakerState("refresh", state)
	}
}

// State returns the current state (for debugging/metrics if needed).
func (cb *CircuitBreaker) State() string {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}
