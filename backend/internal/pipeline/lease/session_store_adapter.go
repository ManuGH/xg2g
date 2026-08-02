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
type SessionStoreTunerLeaseController struct {
	Store store.LeaseStore
}

// NewSessionStoreTunerLeaseController creates a new adapter wrapping a production LeaseStore.
func NewSessionStoreTunerLeaseController(st store.LeaseStore) *SessionStoreTunerLeaseController {
	return &SessionStoreTunerLeaseController{Store: st}
}

func (c *SessionStoreTunerLeaseController) Acquire(ctx context.Context, owner Owner, slot int, ttl time.Duration) (*TunerLeaseHandle, error) {
	if c == nil || c.Store == nil {
		return nil, ErrBindingUnavailable
	}
	if slot < 0 {
		return nil, fmt.Errorf("%w: tuner slot must be non-negative", ErrInvalidScope)
	}
	key := string(ScopeForTunerSlot(slot))
	l, ok, err := c.Store.TryAcquireLease(ctx, key, string(owner), ttl)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrScopeConflict
	}
	return &TunerLeaseHandle{
		LeaseID: ID(l.Key()),
		Owner:   Owner(l.Owner()),
		Slot:    slot,
		Scope:   Scope(l.Key()),
	}, nil
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
