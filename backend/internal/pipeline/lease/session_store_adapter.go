// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package lease

import (
	"context"
	"fmt"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/store"
)

// SessionStoreTunerLeaseController adapts the production store.LeaseStore
// to the TunerLeaseController interface, maintaining a single source of truth.
//
// Operational Note:
// store.LeaseStore tracks ownership, scope keys, and TTL liveness.
// Higher-level session lifecycle management and audit logs record specific terminal domain ReasonCodes.
type SessionStoreTunerLeaseController struct {
	Store store.LeaseStore
}

// NewSessionStoreTunerLeaseController creates a new adapter wrapping a production LeaseStore.
func NewSessionStoreTunerLeaseController(st store.LeaseStore) *SessionStoreTunerLeaseController {
	return &SessionStoreTunerLeaseController{Store: st}
}

// AcquireWithLease acquires a tuner slot lease and returns both the TunerLeaseHandle and store.Lease atomically.
func (c *SessionStoreTunerLeaseController) AcquireWithLease(ctx context.Context, owner Owner, slot int, ttl time.Duration) (*TunerLeaseHandle, store.Lease, error) {
	if c == nil || c.Store == nil {
		return nil, nil, ErrBindingUnavailable
	}
	if slot < 0 {
		return nil, nil, fmt.Errorf("%w: tuner slot must be non-negative", ErrInvalidScope)
	}
	key := string(ScopeForTunerSlot(slot))
	l, ok, err := c.Store.TryAcquireLease(ctx, key, string(owner), ttl)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return nil, nil, ErrScopeConflict
	}
	handle := &TunerLeaseHandle{
		LeaseID: ID(l.Key()),
		Owner:   Owner(l.Owner()),
		Slot:    slot,
		Scope:   Scope(l.Key()),
	}
	return handle, l, nil
}

func (c *SessionStoreTunerLeaseController) Acquire(ctx context.Context, owner Owner, slot int, ttl time.Duration) (*TunerLeaseHandle, error) {
	handle, _, err := c.AcquireWithLease(ctx, owner, slot, ttl)
	return handle, err
}

func (c *SessionStoreTunerLeaseController) Renew(ctx context.Context, handle *TunerLeaseHandle, ttl time.Duration) error {
	if c == nil || c.Store == nil {
		return ErrBindingUnavailable
	}
	if handle == nil || handle.LeaseID == "" {
		return ErrNotFound
	}
	_, ok, err := c.Store.RenewLease(ctx, string(handle.Scope), string(handle.Owner), ttl)
	if err != nil {
		return err
	}
	if !ok {
		return ErrLeaseInactive
	}
	return nil
}

func (c *SessionStoreTunerLeaseController) Release(ctx context.Context, handle *TunerLeaseHandle, reason ReasonCode) error {
	if c == nil || c.Store == nil {
		return ErrBindingUnavailable
	}
	if handle == nil || handle.LeaseID == "" {
		return nil
	}
	return c.Store.ReleaseLease(ctx, string(handle.Scope), string(handle.Owner))
}

func (c *SessionStoreTunerLeaseController) ListLeases(ctx context.Context) ([]Lease, error) {
	if c == nil || c.Store == nil {
		return nil, nil
	}
	sls, err := c.Store.ListLeases(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	res := make([]Lease, 0, len(sls))
	for _, sl := range sls {
		if now.After(sl.ExpiresAt()) {
			continue
		}
		res = append(res, Lease{
			ID:         ID(sl.Key()),
			Owner:      Owner(sl.Owner()),
			Scope:      Scope(sl.Key()),
			State:      StateAcquired,
			AcquiredAt: now,
			ExpiresAt:  sl.ExpiresAt(),
			ReasonCode: ReasonAcquired,
		})
	}
	return res, nil
}

func (c *SessionStoreTunerLeaseController) ReleaseBackendLease(ctx context.Context, id ID, owner Owner, reason ReasonCode) (*Lease, error) {
	if c == nil || c.Store == nil {
		return nil, ErrBindingUnavailable
	}
	err := c.Store.ReleaseLease(ctx, string(id), string(owner))
	if err != nil {
		return nil, err
	}
	return &Lease{
		ID:         id,
		Owner:      owner,
		Scope:      Scope(id),
		State:      StateReleased,
		ReasonCode: reason,
	}, nil
}
