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
type State int

const (
	StateClosed State = iota
	StateOpen
	StateHalfOpen
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

var (
	ErrCircuitOpen = errors.New("circuit breaker is open")
)

type EventKind int

const (
	EventAttempt EventKind = iota
	EventSuccess
	EventTechFailure
)

type event struct {
	ts   time.Time
	kind EventKind
}

// clock abstracts time operations for testability.
type clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// CircuitBreaker implements a sliding-window state machine to prevent cascading failures.
type CircuitBreaker struct {
	mu sync.Mutex

	name string

	state    State
	openedAt time.Time

	// Sliding window events
	events []event
	window time.Duration

	// Thresholds
	threshold        int           // Max failures in window
	minAttempts      int           // Min attempts in window before tripping
	successes        int           // Successes in HALF_OPEN
	successThreshold int           // Successes required to close from HALF_OPEN
	resetTimeout     time.Duration // Cooldown before HALF_OPEN

	clock         clock
	panicRecovery bool
}

// Option configuration pattern
type Option func(*CircuitBreaker)

func WithClock(c clock) Option {
	return func(cb *CircuitBreaker) { cb.clock = c }
}

func WithHalfOpenSuccessThreshold(n int) Option {
	return func(cb *CircuitBreaker) { cb.successThreshold = n }
}

func WithPanicRecovery(enabled bool) Option {
	return func(cb *CircuitBreaker) { cb.panicRecovery = enabled }
}

// NewCircuitBreaker creates a new sliding-window circuit breaker.
func NewCircuitBreaker(name string, threshold int, minAttempts int, window time.Duration, resetTimeout time.Duration, opts ...Option) *CircuitBreaker {
	if threshold <= 0 {
		threshold = 3
	}
	if minAttempts <= 0 {
		minAttempts = 5
	}
	if window <= 0 {
		window = 60 * time.Second
	}
	if resetTimeout <= 0 {
		resetTimeout = 30 * time.Second
	}

	cb := &CircuitBreaker{
		name:             name,
		state:            StateClosed,
		threshold:        threshold,
		minAttempts:      minAttempts,
		window:           window,
		resetTimeout:     resetTimeout,
		successThreshold: 3, // Default N=3 successes to close
		clock:            realClock{},
	}

	for _, opt := range opts {
		opt(cb)
	}

	metrics.SetCircuitBreakerState(cb.name, cb.state.String())
	metrics.SetCircuitBreakerStatus(cb.name, int(cb.state))
	return cb
}

// Execute wraps a function call with circuit breaker logic and optional panic recovery.
func (cb *CircuitBreaker) Execute(fn func() error) error {
	if !cb.AllowRequest() {
		return ErrCircuitOpen
	}

	if cb.panicRecovery {
		defer func() {
			if r := recover(); r != nil {
				cb.RecordTechnicalFailure()
				// We don't swallow the panic, just record it as a failure
				panic(r)
			}
		}()
	}

	err := fn()
	if err != nil {
		// Note: We don't know if this is a technical failure here
		// so we assume any error returned by the function is a failure
		// for the sake of backward compatibility with the old Execute()
		cb.RecordTechnicalFailure()
		return err
	}

	cb.RecordSuccess()
	return nil
}

// AllowRequest checks if a request is permitted and handles transitions to HALF_OPEN.
func (cb *CircuitBreaker) AllowRequest() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.prune()

	if cb.state == StateClosed {
		return true
	}

	if cb.state == StateOpen {
		if cb.clock.Now().Sub(cb.openedAt) >= cb.resetTimeout {
			cb.transitionInto(StateHalfOpen)
			return true
		}
		return false
	}

	// HALF_OPEN
	return true
}

// RecordAttempt marks a transcode spawn commit.
func (cb *CircuitBreaker) RecordAttempt() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.events = append(cb.events, event{ts: cb.clock.Now(), kind: EventAttempt})
	cb.prune()
	cb.evaluate()
}

// RecordSuccess marks a successful completion or intentional cancel.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.events = append(cb.events, event{ts: cb.clock.Now(), kind: EventSuccess})
	cb.prune()

	if cb.state == StateHalfOpen {
		cb.successes++
		if cb.successes >= cb.successThreshold {
			cb.transitionInto(StateClosed)
		}
	}
}

// RecordTechnicalFailure marks a crash, start-timeout, or stall.
func (cb *CircuitBreaker) RecordTechnicalFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.events = append(cb.events, event{ts: cb.clock.Now(), kind: EventTechFailure})
	cb.prune()

	if cb.state == StateHalfOpen {
		// First technical failure in HALF_OPEN trips it back to OPEN
		cb.transitionInto(StateOpen)
		return
	}

	cb.evaluate()
}

func (cb *CircuitBreaker) prune() {
	cutoff := cb.clock.Now().Add(-cb.window)
	n := 0
	for i := range cb.events {
		if !cb.events[i].ts.Before(cutoff) {
			cb.events = cb.events[i:]
			n = 1
			break
		}
	}
	if n == 0 {
		cb.events = nil
	}
}

func (cb *CircuitBreaker) evaluate() {
	if cb.state != StateClosed {
		return
	}

	var attempts, failures int
	for _, e := range cb.events {
		switch e.kind {
		case EventAttempt:
			attempts++
		case EventTechFailure:
			failures++
		}
	}

	if attempts >= cb.minAttempts && failures >= cb.threshold {
		cb.transitionInto(StateOpen)
	}
}

func (cb *CircuitBreaker) transitionInto(s State) {
	if cb.state == s {
		return
	}

	cb.state = s
	switch s {
	case StateOpen:
		cb.openedAt = cb.clock.Now()
		metrics.RecordCircuitBreakerTrip(cb.name, "tech_failure_threshold")
	case StateHalfOpen:
		cb.successes = 0
	case StateClosed:
		cb.events = nil // Reset window on recovery? Usually good to clear old noise
	}

	metrics.SetCircuitBreakerState(cb.name, s.String())
	metrics.SetCircuitBreakerStatus(cb.name, int(s))
}

// GetState returns current state for metrics.
func (cb *CircuitBreaker) GetState() State {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}
