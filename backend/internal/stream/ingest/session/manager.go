// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package session

import (
	"context"
	"io"
	"sync"
	"time"
)

// UpstreamConnector is responsible for establishing an upstream broadcast connection.
type UpstreamConnector interface {
	Connect(ctx context.Context, key SessionKey) (io.ReadCloser, error)
}

// ManagerConfig configures the Shared Ingest Session Manager.
type ManagerConfig struct {
	WarmHoldDuration time.Duration // Time to hold upstream open after last subscriber disconnects
	ConnectTimeout   time.Duration // Maximum timeout for establishing upstream connection
}

// DefaultManagerConfig returns production baseline configuration.
func DefaultManagerConfig() ManagerConfig {
	return ManagerConfig{
		WarmHoldDuration: 5 * time.Second,
		ConnectTimeout:   5 * time.Second,
	}
}

// Manager coordinates shared live-ingest broadcast sessions across multiple subscribers.
type Manager struct {
	mu        sync.Mutex
	cfg       ManagerConfig
	connector UpstreamConnector
	sessions  map[SessionKey]*Session
	isClosed  bool
}

// NewManager creates a new Shared Ingest Session Manager.
func NewManager(cfg ManagerConfig, connector UpstreamConnector) *Manager {
	if cfg.ConnectTimeout <= 0 {
		cfg.ConnectTimeout = 5 * time.Second
	}
	return &Manager{
		cfg:       cfg,
		connector: connector,
		sessions:  make(map[SessionKey]*Session),
	}
}

// Acquire requests a lease for a live session matching the given SessionKey.
// Multiple simultaneous Acquire calls for the same key are coalesced to a single upstream connection.
func (m *Manager) Acquire(ctx context.Context, key SessionKey) (*Lease, error) {
	if err := key.Validate(); err != nil {
		return nil, err
	}
	key = key.Canonicalize()

	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		m.mu.Lock()
		if m.isClosed {
			m.mu.Unlock()
			return nil, ErrManagerClosed
		}

		s, exists := m.sessions[key]
		if exists {
			// Fast-path: try acquiring active or holding session
			if lease, ok := s.TryAcquireActive(); ok {
				m.mu.Unlock()
				return lease, nil
			}

			// If starting, wait for start result outside mutex
			if s.State() == StateStarting {
				m.mu.Unlock()
				lease, err := s.AwaitStart(ctx)
				if err != nil {
					return nil, err
				}
				return lease, nil
			}

			// If session is stopped or failed, purge from map and recreate
			delete(m.sessions, key)
		}

		// Create fresh starting session
		s = NewSession(key, m.cfg.WarmHoldDuration, m.removeSession)
		m.sessions[key] = s

		connectCtx, cancelConnect := context.WithTimeout(context.Background(), m.cfg.ConnectTimeout)
		s.cancelFunc = cancelConnect

		go m.startUpstream(connectCtx, cancelConnect, s)

		m.mu.Unlock()

		lease, err := s.AwaitStart(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			// Startup failed: retry fresh if another attempt is viable
			return nil, err
		}
		return lease, nil
	}
}

func (m *Manager) startUpstream(ctx context.Context, cancel context.CancelFunc, s *Session) {
	reader, err := m.connector.Connect(ctx, s.Key())
	if err != nil {
		cancel()
		s.SetFailed(err)
		return
	}

	s.SetStarted(reader, cancel)
}

func (m *Manager) removeSession(s *Session) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if cur, ok := m.sessions[s.Key()]; ok && cur == s {
		delete(m.sessions, s.Key())
	}
}

// ActiveCount returns the number of active, starting, or holding sessions currently registered.
func (m *Manager) ActiveCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sessions)
}

// Close gracefully stops all active ingest sessions and rejects further Acquire calls.
func (m *Manager) Close() error {
	m.mu.Lock()
	if m.isClosed {
		m.mu.Unlock()
		return nil
	}
	m.isClosed = true

	toClose := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		toClose = append(toClose, s)
	}
	m.sessions = make(map[SessionKey]*Session)
	m.mu.Unlock()

	for _, s := range toClose {
		s.Stop()
	}
	return nil
}
