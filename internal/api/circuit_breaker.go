// SPDX-License-Identifier: MIT

// Package api provides HTTP server functionality for the xg2g application.
package api

import (
	"errors"
	"sync"
	"time"
)

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
}

var errCircuitOpen = errors.New("circuit breaker is open")

const (
	circuitStateClosed   = "closed"
	circuitStateOpen     = "open"
	circuitStateHalfOpen = "half-open"
)

// NewCircuitBreaker creates a new circuit breaker with the specified threshold and timeout.
func NewCircuitBreaker(threshold int, timeout time.Duration) *CircuitBreaker {
	if threshold <= 0 {
		threshold = 3
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &CircuitBreaker{threshold: threshold, timeout: timeout, state: circuitStateClosed}
}

// Call executes fn respecting the breaker state. It records failures and panics.
func (cb *CircuitBreaker) Call(fn func() error) (err error) {
	if cb == nil {
		return fn()
	}

	cb.mu.Lock()
	switch cb.state {
	case circuitStateOpen:
		if time.Since(cb.openedAt) >= cb.timeout {
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
	cb.mu.Unlock()

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
	defer cb.mu.Unlock()
	cb.failures++
	// If in half-open, any failure opens immediately
	if cb.state == circuitStateHalfOpen || cb.failures >= cb.threshold {
		cb.state = circuitStateOpen
		cb.openedAt = time.Now()
	}
}

func (cb *CircuitBreaker) recordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures = 0
	cb.state = circuitStateClosed
}

// State returns the current state (for debugging/metrics if needed).
func (cb *CircuitBreaker) State() string {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}
