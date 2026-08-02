// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package lease

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	ErrNotFound       = errors.New("lease not found")
	ErrOwnerMismatch  = errors.New("lease owner mismatch")
	ErrScopeConflict  = errors.New("lease scope conflict")
	ErrInvalidTTL     = errors.New("lease invalid TTL")
	ErrLeaseInactive  = errors.New("lease inactive or expired")
)

// ManagerConfig holds configuration for the Lease Manager.
type ManagerConfig struct {
	SweepInterval time.Duration
}

// Manager manages in-memory lease lifecycles, state transitions, and expirations.
type Manager struct {
	mu     sync.RWMutex
	leases map[ID]*Lease
	scopes map[Scope]ID // maps active scope -> lease ID

	sweepInterval time.Duration
	stopCh        chan struct{}
	wg            sync.WaitGroup
	nowFunc       func() time.Time
}

// NewManager creates a new thread-safe Lease Manager.
func NewManager(cfg ManagerConfig) *Manager {
	if cfg.SweepInterval <= 0 {
		cfg.SweepInterval = 1 * time.Second
	}
	m := &Manager{
		leases:        make(map[ID]*Lease),
		scopes:        make(map[Scope]ID),
		sweepInterval: cfg.SweepInterval,
		stopCh:        make(chan struct{}),
		nowFunc:       time.Now,
	}
	m.startSweeper()
	return m
}

func (m *Manager) now() time.Time {
	if m.nowFunc != nil {
		return m.nowFunc()
	}
	return time.Now()
}

func generateID() ID {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return ID("lse_" + hex.EncodeToString(b))
}

// Acquire creates a new lease for the specified scope if no active lease conflicts.
func (m *Manager) Acquire(ctx context.Context, owner Owner, scope Scope, ttl time.Duration) (*Lease, error) {
	if ctx != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if owner == "" || scope == "" {
		return nil, fmt.Errorf("%w: owner and scope must not be empty", ErrInvalidTTL)
	}
	if ttl <= 0 {
		return nil, fmt.Errorf("%w: TTL must be greater than 0", ErrInvalidTTL)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now()
	m.sweepLocked(now)

	// Check if scope is currently held by an active lease
	if activeID, exists := m.scopes[scope]; exists {
		if activeLease, ok := m.leases[activeID]; ok && activeLease.IsActive(now) {
			return nil, fmt.Errorf("%w: scope %q is held by lease %s", ErrScopeConflict, scope, activeID)
		}
	}

	leaseID := generateID()
	expiresAt := now.Add(ttl)

	l := &Lease{
		ID:         leaseID,
		Owner:      owner,
		Scope:      scope,
		State:      StateAcquired,
		AcquiredAt: now,
		ExpiresAt:  expiresAt,
		ReasonCode: ReasonAcquired,
	}

	m.leases[leaseID] = l
	m.scopes[scope] = leaseID

	// Return a copy to prevent external mutation without locks
	cp := *l
	return &cp, nil
}

// Release idempotently releases a lease held by the given owner.
func (m *Manager) Release(id ID, owner Owner, reason ReasonCode) (*Lease, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	l, ok := m.leases[id]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
	}

	if l.Owner != owner {
		return nil, fmt.Errorf("%w: lease owned by %s, release attempted by %s", ErrOwnerMismatch, l.Owner, owner)
	}

	now := m.now()

	// Idempotency: if already released or expired, return the lease without error
	if l.State == StateReleased || l.State == StateExpired || l.State == StateRevoked {
		cp := *l
		return &cp, nil
	}

	if reason == "" {
		reason = ReasonReleasedByOwner
	}

	l.State = StateReleased
	l.ReleasedAt = &now
	l.ReasonCode = reason

	if activeID, exists := m.scopes[l.Scope]; exists && activeID == id {
		delete(m.scopes, l.Scope)
	}

	cp := *l
	return &cp, nil
}

// Renew extends the expiration time of an active lease.
func (m *Manager) Renew(id ID, owner Owner, ttl time.Duration) (*Lease, error) {
	if ttl <= 0 {
		return nil, fmt.Errorf("%w: TTL must be greater than 0", ErrInvalidTTL)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	l, ok := m.leases[id]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
	}

	if l.Owner != owner {
		return nil, fmt.Errorf("%w: lease owned by %s, renew attempted by %s", ErrOwnerMismatch, l.Owner, owner)
	}

	now := m.now()
	if !l.IsActive(now) {
		return nil, fmt.Errorf("%w: lease %s is %s", ErrLeaseInactive, id, l.State)
	}

	l.ExpiresAt = now.Add(ttl)
	cp := *l
	return &cp, nil
}

// Revoke forcefully invalidates a lease regardless of owner (for admin or preemption).
func (m *Manager) Revoke(id ID, reason ReasonCode) (*Lease, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	l, ok := m.leases[id]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
	}

	now := m.now()
	if l.State == StateReleased || l.State == StateExpired || l.State == StateRevoked {
		cp := *l
		return &cp, nil
	}

	if reason == "" {
		reason = ReasonRevokedByAdmin
	}

	l.State = StateRevoked
	l.ReleasedAt = &now
	l.ReasonCode = reason

	if activeID, exists := m.scopes[l.Scope]; exists && activeID == id {
		delete(m.scopes, l.Scope)
	}

	cp := *l
	return &cp, nil
}

// Get returns a copy of a lease by ID.
func (m *Manager) Get(id ID) (*Lease, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	l, ok := m.leases[id]
	if !ok {
		return nil, false
	}
	cp := *l
	return &cp, true
}

// ActiveLeases returns all currently active leases for a given scope (or all active if scope is empty).
func (m *Manager) ActiveLeases(scope Scope) []*Lease {
	m.mu.RLock()
	defer m.mu.RUnlock()

	now := m.now()
	var result []*Lease

	for _, l := range m.leases {
		if scope != "" && l.Scope != scope {
			continue
		}
		if l.IsActive(now) {
			cp := *l
			result = append(result, &cp)
		}
	}
	return result
}

// Sweep checks for expired leases and updates their state deterministically.
func (m *Manager) Sweep(now time.Time) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sweepLocked(now)
}

func (m *Manager) sweepLocked(now time.Time) int {
	expiredCount := 0
	for id, l := range m.leases {
		if l.State == StateAcquired && !now.Before(l.ExpiresAt) {
			l.State = StateExpired
			l.ReleasedAt = &now
			l.ReasonCode = ReasonExpired
			if activeID, exists := m.scopes[l.Scope]; exists && activeID == id {
				delete(m.scopes, l.Scope)
			}
			expiredCount++
		}
	}
	return expiredCount
}

func (m *Manager) startSweeper() {
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		ticker := time.NewTicker(m.sweepInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				m.Sweep(time.Now())
			case <-m.stopCh:
				return
			}
		}
	}()
}

// Close stops the background sweeper loop cleanly.
func (m *Manager) Close() error {
	select {
	case <-m.stopCh:
		// already closed
		return nil
	default:
		close(m.stopCh)
		m.wg.Wait()
		return nil
	}
}
