// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package session

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrSessionClosed = errors.New("ingest session closed")
	ErrSessionFailed = errors.New("ingest session startup failed")
	ErrManagerClosed = errors.New("ingest session manager closed")
)

// State represents the lifecycle state of a shared ingest session.
type State string

const (
	StateStarting State = "starting"
	StateActive   State = "active"
	StateHolding  State = "holding"
	StateStopped  State = "stopped"
	StateFailed   State = "failed"
)

// PipelineHolder is an optional interface implemented by upstream readers to expose pipeline payloads.
type PipelineHolder interface {
	Pipeline() any
}

// Session manages a single shared upstream broadcast stream.
// Note: Session owns the upstream io.ReadCloser exclusively.
type Session struct {
	key          SessionKey
	holdDuration time.Duration
	onTeardown   func(s *Session)

	mu           sync.Mutex
	state        State
	refCount     int
	waitersCount int
	startErr     error
	readyChan    chan struct{}
	readyOnce    sync.Once

	upstream   io.ReadCloser
	payload    any
	cancelFunc context.CancelFunc
	holdTimer  *time.Timer
}

// NewSession creates an unstarted session in StateStarting.
func NewSession(key SessionKey, holdDuration time.Duration, onTeardown func(s *Session)) *Session {
	return &Session{
		key:          key.Canonicalize(),
		holdDuration: holdDuration,
		onTeardown:   onTeardown,
		state:        StateStarting,
		readyChan:    make(chan struct{}),
	}
}

// Key returns the session's canonical key.
func (s *Session) Key() SessionKey {
	return s.key
}

// State returns the current session lifecycle state.
func (s *Session) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// RefCount returns the current active subscriber count.
func (s *Session) RefCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.refCount
}

func (s *Session) closeReadyChanLocked() {
	s.readyOnce.Do(func() {
		close(s.readyChan)
	})
}

// SetStarted activates the session with its connected upstream.
// If the session was cancelled/abandoned in the meantime, the upstream is immediately closed.
func (s *Session) SetStarted(upstream io.ReadCloser, cancelFunc context.CancelFunc) {
	s.mu.Lock()
	if s.state != StateStarting {
		s.mu.Unlock()
		if upstream != nil {
			_ = upstream.Close()
		}
		if cancelFunc != nil {
			cancelFunc()
		}
		return
	}

	s.upstream = upstream
	if ph, ok := upstream.(PipelineHolder); ok {
		s.payload = ph.Pipeline()
	}
	s.cancelFunc = cancelFunc
	s.state = StateActive
	s.closeReadyChanLocked()
	s.mu.Unlock()
}

// Payload returns the associated pipeline payload if any.
func (s *Session) Payload() any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.payload
}

// SetPayload stores an arbitrary pipeline payload onto the session.
func (s *Session) SetPayload(p any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.payload = p
}

// SetFailed marks session startup as failed and propagates the error.
func (s *Session) SetFailed(err error) {
	s.mu.Lock()
	if s.state != StateStarting {
		s.mu.Unlock()
		return
	}

	s.startErr = err
	s.state = StateFailed
	s.closeReadyChanLocked()
	s.mu.Unlock()

	if s.onTeardown != nil {
		s.onTeardown(s)
	}
}

// AwaitStart waits for session startup to complete, handling cancellation cleanly.
func (s *Session) AwaitStart(ctx context.Context) (*Lease, error) {
	s.mu.Lock()
	if s.state == StateActive {
		s.refCount++
		s.mu.Unlock()
		return newLease(s), nil
	}
	if s.state == StateFailed {
		err := s.startErr
		s.mu.Unlock()
		if err == nil {
			err = ErrSessionFailed
		}
		return nil, err
	}
	if s.state == StateStopped {
		s.mu.Unlock()
		return nil, ErrSessionClosed
	}

	s.waitersCount++
	ready := s.readyChan
	s.mu.Unlock()

	select {
	case <-ctx.Done():
		s.mu.Lock()
		s.waitersCount--
		allAborted := s.waitersCount == 0 && s.state == StateStarting && s.refCount == 0
		if allAborted {
			s.state = StateStopped
			s.closeUpstreamLocked()
			s.closeReadyChanLocked()
			s.mu.Unlock()
			if s.onTeardown != nil {
				s.onTeardown(s)
			}
			return nil, ctx.Err()
		}
		s.mu.Unlock()
		return nil, ctx.Err()

	case <-ready:
		s.mu.Lock()
		defer s.mu.Unlock()
		s.waitersCount--

		if s.state == StateActive {
			s.refCount++
			return newLease(s), nil
		}
		if s.state == StateStopped {
			return nil, ErrSessionClosed
		}
		if s.startErr != nil {
			return nil, s.startErr
		}
		return nil, ErrSessionFailed
	}
}

// TryAcquireActive attempts to acquire an active or holding session without waiting.
// Returns true and a Lease if acquired, false if session is starting, stopped, or failed.
func (s *Session) TryAcquireActive() (*Lease, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state == StateHolding {
		if s.holdTimer != nil {
			s.holdTimer.Stop()
			s.holdTimer = nil
		}
		s.state = StateActive
	}

	if s.state == StateActive {
		s.refCount++
		return newLease(s), true
	}

	return nil, false
}

func (s *Session) releaseSubscriber() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.refCount > 0 {
		s.refCount--
	}

	if s.refCount == 0 && s.state == StateActive {
		if s.holdDuration > 0 {
			s.state = StateHolding
			if s.holdTimer != nil {
				s.holdTimer.Stop()
			}
			s.holdTimer = time.AfterFunc(s.holdDuration, s.onHoldExpired)
		} else {
			s.state = StateStopped
			s.closeUpstreamLocked()
			if s.onTeardown != nil {
				go s.onTeardown(s)
			}
		}
	}
}

func (s *Session) onHoldExpired() {
	s.mu.Lock()
	if s.state != StateHolding || s.refCount > 0 {
		s.mu.Unlock()
		return
	}

	s.state = StateStopped
	s.closeUpstreamLocked()
	s.mu.Unlock()

	if s.onTeardown != nil {
		s.onTeardown(s)
	}
}

func (s *Session) closeUpstreamLocked() {
	if s.cancelFunc != nil {
		s.cancelFunc()
		s.cancelFunc = nil
	}
	if s.upstream != nil {
		_ = s.upstream.Close()
		s.upstream = nil
	}
	if closer, ok := s.payload.(io.Closer); ok {
		_ = closer.Close()
	}
}

// Stop forcefully closes the session and tears down upstream, immediately waking any waiters.
func (s *Session) Stop() {
	s.mu.Lock()
	if s.holdTimer != nil {
		s.holdTimer.Stop()
		s.holdTimer = nil
	}
	wasStarting := s.state == StateStarting
	s.state = StateStopped
	s.closeUpstreamLocked()
	if wasStarting {
		s.closeReadyChanLocked()
	}
	s.mu.Unlock()

	if s.onTeardown != nil {
		s.onTeardown(s)
	}
}

// Lease represents a subscriber's reference to an active Shared Ingest Session.
type Lease struct {
	session  *Session
	released atomic.Bool
}

func newLease(s *Session) *Lease {
	return &Lease{session: s}
}

// Key returns the session key.
func (l *Lease) Key() SessionKey {
	return l.session.Key()
}

// Session returns the underlying Session.
func (l *Lease) Session() *Session {
	return l.session
}

// State returns the current session state.
func (l *Lease) State() State {
	return l.session.State()
}

// Release decrements the subscriber count. It is safe and idempotent to call multiple times.
func (l *Lease) Release() {
	if l.released.CompareAndSwap(false, true) {
		l.session.releaseSubscriber()
	}
}
