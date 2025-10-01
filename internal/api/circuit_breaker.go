// SPDX-License-Identifier: MIT
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
    state     string // "closed", "open", "half-open"
    openedAt  time.Time
}

var errCircuitOpen = errors.New("circuit breaker is open")

func NewCircuitBreaker(threshold int, timeout time.Duration) *CircuitBreaker {
    if threshold <= 0 {
        threshold = 3
    }
    if timeout <= 0 {
        timeout = 30 * time.Second
    }
    return &CircuitBreaker{threshold: threshold, timeout: timeout, state: "closed"}
}

// Call executes fn respecting the breaker state. It records failures and panics.
func (cb *CircuitBreaker) Call(fn func() error) (err error) {
    if cb == nil {
        return fn()
    }

    cb.mu.Lock()
    switch cb.state {
    case "open":
        if time.Since(cb.openedAt) >= cb.timeout {
            cb.state = "half-open"
        } else {
            cb.mu.Unlock()
            return errCircuitOpen
        }
    case "half-open", "closed":
        // proceed
    default:
        cb.state = "closed"
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
    if cb.state == "half-open" || cb.failures >= cb.threshold {
        cb.state = "open"
        cb.openedAt = time.Now()
    }
}

func (cb *CircuitBreaker) recordSuccess() {
    cb.mu.Lock()
    defer cb.mu.Unlock()
    cb.failures = 0
    cb.state = "closed"
}

// State returns the current state (for debugging/metrics if needed).
func (cb *CircuitBreaker) State() string {
    cb.mu.Lock()
    defer cb.mu.Unlock()
    return cb.state
}
