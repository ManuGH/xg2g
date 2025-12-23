// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.
package openwebif

import (
	"errors"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/metrics"
)

// State represents the circuit breaker state.
type State int

const (
	StateClosed   State = iota // Normal operation, requests allowed
	StateOpen                  // Circuit open, requests blocked
	StateHalfOpen              // Testing if service recovered
)

var (
	ErrCircuitOpen = errors.New("circuit breaker is open")
)

// CircuitBreaker implements a state machine to prevent cascading failures.
type CircuitBreaker struct {
	mu               sync.RWMutex
	state            State
	failures         int
	failureThreshold int
	resetTimeout     time.Duration
	lastFailure      time.Time
}

// NewCircuitBreaker creates a new circuit breaker.
func NewCircuitBreaker(threshold int, resetTimeout time.Duration) *CircuitBreaker {
	cb := &CircuitBreaker{
		state:            StateClosed,
		failureThreshold: threshold,
		resetTimeout:     resetTimeout,
	}
	metrics.SetCircuitBreakerState("openwebif", stateLabel(cb.state))
	return cb
}

// Execute runs the given function if the circuit is closed or half-open.
func (cb *CircuitBreaker) Execute(fn func() error) error {
	if !cb.allowRequest() {
		return ErrCircuitOpen
	}

	err := fn()

	if err != nil {
		cb.recordFailure()
		return err
	}

	cb.recordSuccess()
	return nil
}

// allowRequest checks if a request should be allowed based on current state.
func (cb *CircuitBreaker) allowRequest() bool {
	cb.mu.Lock()
	prevState := cb.state

	if cb.state == StateClosed {
		cb.mu.Unlock()
		return true
	}

	if cb.state == StateOpen {
		if time.Since(cb.lastFailure) > cb.resetTimeout {
			cb.state = StateHalfOpen
			state := cb.state
			cb.mu.Unlock()
			if state != prevState {
				metrics.SetCircuitBreakerState("openwebif", stateLabel(state))
			}
			return true
		}
		cb.mu.Unlock()
		return false
	}

	// StateHalfOpen: allow 1 request to test connectivity
	// In a real implementation we might want to limit concurrent half-open requests,
	// but for simplicity we assume the caller handles concurrency or we allow all
	// until one fails or succeeds.
	cb.mu.Unlock()
	return true
}

// recordFailure records a failure and potentially opens the circuit.
func (cb *CircuitBreaker) recordFailure() {
	cb.mu.Lock()
	prevState := cb.state

	cb.failures++
	cb.lastFailure = time.Now()

	if cb.state == StateHalfOpen {
		cb.state = StateOpen
		state := cb.state
		cb.mu.Unlock()
		if state != prevState {
			metrics.SetCircuitBreakerState("openwebif", stateLabel(state))
		}
		return
	}

	if cb.failures >= cb.failureThreshold {
		cb.state = StateOpen
	}
	state := cb.state
	cb.mu.Unlock()
	if state != prevState {
		metrics.SetCircuitBreakerState("openwebif", stateLabel(state))
	}
}

// recordSuccess records a success and closes the circuit.
func (cb *CircuitBreaker) recordSuccess() {
	cb.mu.Lock()
	prevState := cb.state

	cb.failures = 0
	cb.state = StateClosed
	state := cb.state
	cb.mu.Unlock()
	if state != prevState {
		metrics.SetCircuitBreakerState("openwebif", stateLabel(state))
	}
}

// State returns the current state (thread-safe).
func (cb *CircuitBreaker) State() State {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

func stateLabel(state State) string {
	switch state {
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "closed"
	}
}
