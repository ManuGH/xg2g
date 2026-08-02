// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package lease

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"
)

// ResourceRequest defines a single resource scope and TTL to be acquired as part of a composite lease.
type ResourceRequest struct {
	Scope Scope
	TTL   time.Duration
}

// CompositeLeaseHandle encapsulates multiple acquired resource leases for a single owner.
type CompositeLeaseHandle struct {
	ID     ID
	Owner  Owner
	Leases []*Lease
}

// CompositeManager orchestrates multi-resource acquisitions with deterministic scope ordering,
// LIFO rollback compensation, and failure injection support.
type CompositeManager struct {
	mgr *Manager
}

// NewCompositeManager creates a new CompositeManager wrapping a lease Manager.
func NewCompositeManager(mgr *Manager) *CompositeManager {
	return &CompositeManager{mgr: mgr}
}

// AcquireComposite attempts to acquire multiple resource leases atomically.
// Requests are deterministically sorted by Scope (lexicographically) to prevent deadlocks across concurrent calls.
// If any single resource acquisition fails, all previously acquired leases in the batch are released
// in LIFO (reverse acquisition) order with ReasonRollback before returning a joined error.
func (cm *CompositeManager) AcquireComposite(ctx context.Context, owner Owner, requests []ResourceRequest) (*CompositeLeaseHandle, error) {
	if cm == nil || cm.mgr == nil {
		return nil, fmt.Errorf("%w: composite manager is nil", ErrBindingUnavailable)
	}
	if len(requests) == 0 {
		return nil, fmt.Errorf("%w: at least one resource request is required", ErrInvalidScope)
	}
	if owner == "" {
		return nil, fmt.Errorf("%w: owner cannot be empty", ErrInvalidOwner)
	}

	// 1. Copy and deterministically sort requests by Scope lexicographically to prevent deadlocks
	sortedReqs := make([]ResourceRequest, len(requests))
	copy(sortedReqs, requests)
	sort.SliceStable(sortedReqs, func(i, j int) bool {
		return sortedReqs[i].Scope < sortedReqs[j].Scope
	})

	acquired := make([]*Lease, 0, len(sortedReqs))

	// 2. Sequential acquisition loop
	for _, req := range sortedReqs {
		l, err := cm.mgr.Acquire(ctx, owner, req.Scope, req.TTL)
		if err != nil {
			// Failure at step i: Rollback acquired leases (0 .. i-1) in LIFO (reverse) order
			rollbackErrs := make([]error, 0, len(acquired))
			for j := len(acquired) - 1; j >= 0; j-- {
				_, relErr := cm.mgr.Release(acquired[j].ID, owner, ReasonRollback)
				if relErr != nil {
					rollbackErrs = append(rollbackErrs, fmt.Errorf("rollback release failed for scope %s (id %s): %w", acquired[j].Scope, acquired[j].ID, relErr))
				}
			}
			allErrs := append([]error{fmt.Errorf("composite acquire failed at scope %s: %w", req.Scope, err)}, rollbackErrs...)
			return nil, errors.Join(allErrs...)
		}
		acquired = append(acquired, l)
	}

	compositeID := ID(fmt.Sprintf("composite-%s-%d", owner, time.Now().UnixNano()))
	return &CompositeLeaseHandle{
		ID:     compositeID,
		Owner:  owner,
		Leases: acquired,
	}, nil
}

// ReleaseComposite releases all leases in a composite handle in LIFO (reverse acquisition) order.
func (cm *CompositeManager) ReleaseComposite(handle *CompositeLeaseHandle, reason ReasonCode) error {
	if cm == nil || cm.mgr == nil {
		return fmt.Errorf("%w: composite manager is nil", ErrBindingUnavailable)
	}
	if handle == nil || len(handle.Leases) == 0 {
		return nil
	}
	if reason == "" {
		reason = ReasonReleasedByOwner
	}

	var errs []error
	for i := len(handle.Leases) - 1; i >= 0; i-- {
		l := handle.Leases[i]
		_, err := cm.mgr.Release(l.ID, handle.Owner, reason)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to release composite lease scope %s: %w", l.Scope, err))
		}
	}
	return errors.Join(errs...)
}

// RenewComposite extends the expiration time of all leases held in a composite handle.
func (cm *CompositeManager) RenewComposite(handle *CompositeLeaseHandle, ttl time.Duration) error {
	if cm == nil || cm.mgr == nil {
		return fmt.Errorf("%w: composite manager is nil", ErrBindingUnavailable)
	}
	if handle == nil || len(handle.Leases) == 0 {
		return nil
	}

	var errs []error
	for _, l := range handle.Leases {
		_, err := cm.mgr.Renew(l.ID, handle.Owner, ttl)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to renew composite lease scope %s: %w", l.Scope, err))
		}
	}
	return errors.Join(errs...)
}
