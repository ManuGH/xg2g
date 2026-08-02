// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package lease

import (
	"context"
	"fmt"
	"time"
)

// TunerBinding provides a typed adapter for managing hardware tuner slot leases.
type TunerBinding struct {
	mgr *Manager
}

// NewTunerBinding creates a new TunerBinding adapter wrapping a Lease Manager.
func NewTunerBinding(mgr *Manager) *TunerBinding {
	return &TunerBinding{mgr: mgr}
}

// ScopeForTunerSlot formats a standardized lease scope key for a tuner slot index.
// Format: "tuner:<slot>"
func ScopeForTunerSlot(slot int) Scope {
	return Scope(LeaseKeyTunerSlot(slot))
}

// AcquireTunerSlot attempts to acquire an exclusive lease for the given tuner slot.
func (tb *TunerBinding) AcquireTunerSlot(ctx context.Context, owner Owner, slot int, ttl time.Duration) (*Lease, error) {
	if slot < 0 {
		return nil, fmt.Errorf("%w: tuner slot must be non-negative", ErrInvalidScope)
	}
	if tb == nil || tb.mgr == nil {
		return nil, fmt.Errorf("%w: tuner binding manager is nil", ErrBindingUnavailable)
	}
	scope := ScopeForTunerSlot(slot)
	return tb.mgr.Acquire(ctx, owner, scope, ttl)
}

// ReleaseTunerSlot idempotently releases a tuner slot lease held by the owner with an explicit reason code.
func (tb *TunerBinding) ReleaseTunerSlot(id ID, owner Owner, reason ReasonCode) (*Lease, error) {
	if tb == nil || tb.mgr == nil {
		return nil, fmt.Errorf("%w: tuner binding manager is nil", ErrBindingUnavailable)
	}
	if reason == "" {
		reason = ReasonReleasedByOwner
	}
	return tb.mgr.Release(id, owner, reason)
}

// RenewTunerSlot extends the expiration time of an active tuner slot lease.
func (tb *TunerBinding) RenewTunerSlot(id ID, owner Owner, ttl time.Duration) (*Lease, error) {
	if tb == nil || tb.mgr == nil {
		return nil, fmt.Errorf("%w: tuner binding manager is nil", ErrBindingUnavailable)
	}
	return tb.mgr.Renew(id, owner, ttl)
}

// GetTunerLease retrieves the active lease for a tuner slot, if present.
func (tb *TunerBinding) GetTunerLease(slot int) (*Lease, bool) {
	if tb == nil || tb.mgr == nil || slot < 0 {
		return nil, false
	}
	scope := ScopeForTunerSlot(slot)
	leases := tb.mgr.ActiveLeases(scope)
	if len(leases) == 0 {
		return nil, false
	}
	return leases[0], true
}
