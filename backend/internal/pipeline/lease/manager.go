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

	"github.com/ManuGH/xg2g/internal/pipeline/policy"
)

var (
	ErrNotFound           = errors.New("lease not found")
	ErrOwnerMismatch      = errors.New("lease owner mismatch")
	ErrScopeConflict      = errors.New("lease scope conflict")
	ErrInvalidTTL         = errors.New("lease invalid TTL")
	ErrInvalidOwner       = errors.New("lease invalid owner")
	ErrInvalidScope       = errors.New("lease invalid scope")
	ErrLeaseInactive      = errors.New("lease inactive or expired")
	ErrManagerClosed      = errors.New("lease manager closed")
	ErrBindingUnavailable = errors.New("tuner lease binding unavailable")
)

// ManagerConfig holds configuration for the Lease Manager.
type ManagerConfig struct {
	SweepInterval time.Duration
	Clock         func() time.Time
}

// Manager manages in-memory lease lifecycles, state transitions, and expirations.
type Manager struct {
	mu     sync.RWMutex
	leases map[ID]*Lease
	scopes map[Scope]ID // maps active scope -> lease ID

	sweepInterval time.Duration
	nowFunc       func() time.Time
	closed        bool
	stopCh        chan struct{}
	closeOnce     sync.Once
	wg            sync.WaitGroup
}

// NewManager creates a new thread-safe Lease Manager.
func NewManager(cfg ManagerConfig) *Manager {
	if cfg.SweepInterval <= 0 {
		cfg.SweepInterval = 1 * time.Second
	}
	clock := cfg.Clock
	if clock == nil {
		clock = time.Now
	}

	m := &Manager{
		leases:        make(map[ID]*Lease),
		scopes:        make(map[Scope]ID),
		sweepInterval: cfg.SweepInterval,
		nowFunc:       clock,
		stopCh:        make(chan struct{}),
	}
	m.startSweeper()
	return m
}

func (m *Manager) now() time.Time {
	return m.nowFunc()
}

func generateID() (ID, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate lease ID: %w", err)
	}
	return ID("lse_" + hex.EncodeToString(b)), nil
}

// resolveStateLocked evaluates whether an ACQUIRED lease has expired according to now.
// If expired, it transitions the lease to EXPIRED deterministically and cleans up scope index.
func (m *Manager) resolveStateLocked(l *Lease, now time.Time) {
	if l == nil {
		return
	}
	if l.State == StateAcquired && !now.Before(l.ExpiresAt) {
		l.State = StateExpired
		expiryCopy := l.ExpiresAt
		l.ReleasedAt = &expiryCopy
		l.ReasonCode = ReasonExpired
		if activeID, exists := m.scopes[l.Scope]; exists && activeID == l.ID {
			delete(m.scopes, l.Scope)
		}
	}
}

// AcquireWithBoundTicket enforces that a valid, bound AdmissionTicket in TicketStatusConsumed state is provided prior to acquiring a tuner lease.
func (m *Manager) AcquireWithBoundTicket(ctx context.Context, ticket *policy.AdmissionTicket, sessionID, userID, profileID string, owner Owner, scope Scope, ttl time.Duration) (*Lease, error) {
	if err := policy.ValidateBoundTicket(ticket, sessionID, userID, profileID, "tuner"); err != nil {
		return nil, fmt.Errorf("tuner lease ticket validation failed: %w", err)
	}
	return m.Acquire(ctx, owner, scope, ttl)
}

// Acquire creates a new lease for the specified scope if no active lease conflicts.
func (m *Manager) Acquire(ctx context.Context, owner Owner, scope Scope, ttl time.Duration) (*Lease, error) {
	if ctx != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if owner == "" {
		return nil, fmt.Errorf("%w: owner must not be empty", ErrInvalidOwner)
	}
	if scope == "" {
		return nil, fmt.Errorf("%w: scope must not be empty", ErrInvalidScope)
	}
	if ttl <= 0 {
		return nil, fmt.Errorf("%w: TTL must be greater than 0", ErrInvalidTTL)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil, fmt.Errorf("%w: cannot acquire lease on closed manager", ErrManagerClosed)
	}

	now := m.now()
	m.sweepLocked(now)

	// Check if scope is currently held by an active lease
	if activeID, exists := m.scopes[scope]; exists {
		if activeLease, ok := m.leases[activeID]; ok {
			m.resolveStateLocked(activeLease, now)
			if activeLease.IsActive(now) {
				return nil, fmt.Errorf("%w: scope %q is held by lease %s", ErrScopeConflict, scope, activeID)
			}
		}
	}

	leaseID, err := generateID()
	if err != nil {
		return nil, err
	}

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

	cp := *l
	return &cp, nil
}

func (m *Manager) ReleaseBackendLease(ctx context.Context, id ID, owner Owner, reason ReasonCode) (*Lease, error) {
	return m.Release(ctx, id, owner, reason)
}

// Release idempotently releases a lease held by the given owner.
func (m *Manager) Release(ctx context.Context, id ID, owner Owner, reason ReasonCode) (*Lease, error) {
	if ctx != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
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
	m.resolveStateLocked(l, now)

	// Idempotency: if already released, expired, or revoked, return current state cleanly
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
func (m *Manager) Renew(ctx context.Context, id ID, owner Owner, ttl time.Duration) (*Lease, error) {
	if ctx != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if ttl <= 0 {
		return nil, fmt.Errorf("%w: TTL must be greater than 0", ErrInvalidTTL)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil, fmt.Errorf("%w: cannot renew lease on closed manager", ErrManagerClosed)
	}

	l, ok := m.leases[id]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
	}

	if l.Owner != owner {
		return nil, fmt.Errorf("%w: lease owned by %s, renew attempted by %s", ErrOwnerMismatch, l.Owner, owner)
	}

	now := m.now()
	m.resolveStateLocked(l, now)

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
	m.resolveStateLocked(l, now)

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

// Get returns a copy of a lease by ID with deterministic state resolution.
func (m *Manager) Get(id ID) (*Lease, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	l, ok := m.leases[id]
	if !ok {
		return nil, false
	}
	now := m.now()
	m.resolveStateLocked(l, now)

	cp := *l
	return &cp, true
}

// ListLeases returns copies of all leases in the manager with deterministic state resolution.
func (m *Manager) ListLeases(ctx context.Context) ([]Lease, error) {
	if ctx != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now()
	result := make([]Lease, 0, len(m.leases))
	for _, l := range m.leases {
		m.resolveStateLocked(l, now)
		result = append(result, *l)
	}
	return result, nil
}

// ActiveLeases returns all currently active leases for a given scope (or all active if scope is empty).
func (m *Manager) ActiveLeases(scope Scope) []*Lease {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now()
	var result []*Lease

	for _, l := range m.leases {
		m.resolveStateLocked(l, now)
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
	for _, l := range m.leases {
		if l.State == StateAcquired {
			prevState := l.State
			m.resolveStateLocked(l, now)
			if prevState == StateAcquired && l.State == StateExpired {
				expiredCount++
			}
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
				m.Sweep(m.now())
			case <-m.stopCh:
				return
			}
		}
	}()
}

// Close cleanly and idempotently stops the background sweeper loop and marks the manager closed.
func (m *Manager) Close() error {
	m.closeOnce.Do(func() {
		m.mu.Lock()
		m.closed = true
		m.mu.Unlock()
		close(m.stopCh)
	})
	m.wg.Wait()
	return nil
}
