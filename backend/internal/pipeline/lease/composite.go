// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package lease

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

var (
	ErrDuplicateScope = errors.New("duplicate lease scope")
)

// LeaseBackend exposes the minimal contract required by CompositeManager for single-resource lease operations.
type LeaseBackend interface {
	Acquire(ctx context.Context, owner Owner, scope Scope, ttl time.Duration) (*Lease, error)
	Renew(id ID, owner Owner, ttl time.Duration) (*Lease, error)
	Release(id ID, owner Owner, reason ReasonCode) (*Lease, error)
}

// CompositeState represents the lifecycle status of a multi-resource composite lease.
type CompositeState string

const (
	CompositeStateAcquired   CompositeState = "ACQUIRED"
	CompositeStateReleased   CompositeState = "RELEASED"
	CompositeStateRolledBack CompositeState = "ROLLED_BACK"
	CompositeStateDegraded   CompositeState = "DEGRADED"
)

// ResourceRequest defines a single resource scope and TTL to be acquired as part of a composite lease.
type ResourceRequest struct {
	Scope Scope
	TTL   time.Duration
}

// CompositeLeaseHandle encapsulates multiple acquired resource leases for a single owner with explicit state tracking.
type CompositeLeaseHandle struct {
	ID     ID
	Owner  Owner
	State  CompositeState
	Leases []*Lease
	mu     sync.Mutex
}

// GetState returns the current CompositeState thread-safely.
func (h *CompositeLeaseHandle) GetState() CompositeState {
	if h == nil {
		return ""
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.State
}

func (h *CompositeLeaseHandle) setStateLocked(st CompositeState) {
	h.State = st
}

// IDGenerator defines a pluggable generator for unique composite IDs.
type IDGenerator func() (ID, error)

func defaultCompositeIDGen() (ID, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate secure composite ID: %w", err)
	}
	return ID("comp-" + hex.EncodeToString(b)), nil
}

// CompositeManagerConfig configures CompositeManager options.
type CompositeManagerConfig struct {
	Backend     LeaseBackend
	IDGenerator IDGenerator
}

// CompositeManager orchestrates multi-resource acquisitions with deterministic scope ordering,
// LIFO rollback compensation, explicit state tracking, and failure injection support.
//
// Operational Note:
// AcquireComposite is a deterministic sequential composite acquisition with LIFO compensating rollback on failure,
// not a single-phase atomic transaction. Partial visibility may occur briefly prior to rollback completion.
type CompositeManager struct {
	backend LeaseBackend
	idGen   IDGenerator
}

// NewCompositeManager creates a new CompositeManager using the specified configuration.
func NewCompositeManager(config CompositeManagerConfig) *CompositeManager {
	idGen := config.IDGenerator
	if idGen == nil {
		idGen = defaultCompositeIDGen
	}
	return &CompositeManager{
		backend: config.Backend,
		idGen:   idGen,
	}
}

// AcquireComposite validates requests up-front, sorts requests deterministically by Scope (lexicographically),
// and sequentially acquires each resource.
//
// If any resource acquisition fails, all previously acquired leases in the batch are released in LIFO (reverse acquisition)
// order with ReasonRollback before returning a joined error.
func (cm *CompositeManager) AcquireComposite(ctx context.Context, owner Owner, requests []ResourceRequest) (*CompositeLeaseHandle, error) {
	if cm == nil || cm.backend == nil {
		return nil, fmt.Errorf("%w: composite manager backend is nil", ErrBindingUnavailable)
	}
	if owner == "" {
		return nil, fmt.Errorf("%w: owner cannot be empty", ErrInvalidOwner)
	}
	if len(requests) == 0 {
		return nil, fmt.Errorf("%w: at least one resource request is required", ErrInvalidScope)
	}

	// 1. Complete Pre-validation (Zero acquisitions performed if validation fails)
	seenScopes := make(map[Scope]bool, len(requests))
	for _, req := range requests {
		if req.Scope == "" {
			return nil, fmt.Errorf("%w: scope cannot be empty", ErrInvalidScope)
		}
		if req.TTL <= 0 {
			return nil, fmt.Errorf("%w: TTL must be greater than 0 for scope %s", ErrInvalidTTL, req.Scope)
		}
		if seenScopes[req.Scope] {
			return nil, fmt.Errorf("%w: %s requested more than once in composite batch", ErrDuplicateScope, req.Scope)
		}
		seenScopes[req.Scope] = true
	}

	// 2. Copy and deterministically sort requests by Scope (lexicographically)
	sortedReqs := make([]ResourceRequest, len(requests))
	copy(sortedReqs, requests)
	sort.SliceStable(sortedReqs, func(i, j int) bool {
		return sortedReqs[i].Scope < sortedReqs[j].Scope
	})

	acquired := make([]*Lease, 0, len(sortedReqs))

	// 3. Sequential acquisition loop
	for _, req := range sortedReqs {
		l, err := cm.backend.Acquire(ctx, owner, req.Scope, req.TTL)
		if err != nil {
			// Failure at step i: Rollback acquired leases (0 .. i-1) in LIFO (reverse) order
			var rollbackErrs []error
			for j := len(acquired) - 1; j >= 0; j-- {
				_, relErr := cm.backend.Release(acquired[j].ID, owner, ReasonRollback)
				if relErr != nil {
					rollbackErrs = append(rollbackErrs, fmt.Errorf("rollback release failed for scope %s (id %s): %w", acquired[j].Scope, acquired[j].ID, relErr))
				}
			}
			acqErr := fmt.Errorf("composite acquire failed at scope %s: %w", req.Scope, err)
			allErrs := append([]error{acqErr}, rollbackErrs...)
			return nil, errors.Join(allErrs...)
		}
		acquired = append(acquired, l)
	}

	compID, err := cm.idGen()
	if err != nil {
		// ID generation failure after successful acquisition: Rollback all acquired leases
		var rollbackErrs []error
		for j := len(acquired) - 1; j >= 0; j-- {
			_, relErr := cm.backend.Release(acquired[j].ID, owner, ReasonRollback)
			if relErr != nil {
				rollbackErrs = append(rollbackErrs, fmt.Errorf("rollback release failed for scope %s: %w", acquired[j].Scope, relErr))
			}
		}
		return nil, errors.Join(append([]error{err}, rollbackErrs...)...)
	}

	return &CompositeLeaseHandle{
		ID:     compID,
		Owner:  owner,
		State:  CompositeStateAcquired,
		Leases: acquired,
	}, nil
}

// ReleaseComposite releases all leases in a composite handle in LIFO (reverse acquisition) order.
// Updates handle state to CompositeStateReleased on clean completion, or CompositeStateDegraded if any release fails.
func (cm *CompositeManager) ReleaseComposite(handle *CompositeLeaseHandle, reason ReasonCode) error {
	if cm == nil || cm.backend == nil {
		return fmt.Errorf("%w: composite manager backend is nil", ErrBindingUnavailable)
	}
	if handle == nil {
		return nil
	}

	handle.mu.Lock()
	defer handle.mu.Unlock()

	if len(handle.Leases) == 0 || handle.State == CompositeStateReleased {
		handle.State = CompositeStateReleased
		return nil
	}
	if reason == "" {
		reason = ReasonReleasedByOwner
	}

	var errs []error
	for i := len(handle.Leases) - 1; i >= 0; i-- {
		l := handle.Leases[i]
		_, err := cm.backend.Release(l.ID, handle.Owner, reason)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to release composite lease scope %s (id %s): %w", l.Scope, l.ID, err))
		}
	}

	if len(errs) > 0 {
		handle.State = CompositeStateDegraded
		return errors.Join(errs...)
	}

	handle.State = CompositeStateReleased
	return nil
}

// RenewComposite extends the expiration time of all leases held in a composite handle.
// Only allowed when handle is in CompositeStateAcquired.
// If any renewal fails, stops further renewals, marks handle as CompositeStateDegraded, and returns joined errors.
func (cm *CompositeManager) RenewComposite(handle *CompositeLeaseHandle, ttl time.Duration) error {
	if cm == nil || cm.backend == nil {
		return fmt.Errorf("%w: composite manager backend is nil", ErrBindingUnavailable)
	}
	if handle == nil {
		return nil
	}
	if ttl <= 0 {
		return fmt.Errorf("%w: TTL must be greater than 0", ErrInvalidTTL)
	}

	handle.mu.Lock()
	defer handle.mu.Unlock()

	if handle.State != CompositeStateAcquired {
		return fmt.Errorf("cannot renew composite in state %s", handle.State)
	}

	var errs []error
	for _, l := range handle.Leases {
		renewed, err := cm.backend.Renew(l.ID, handle.Owner, ttl)
		if err != nil {
			handle.State = CompositeStateDegraded
			errs = append(errs, fmt.Errorf("failed to renew composite lease scope %s (id %s): %w", l.Scope, l.ID, err))
			break
		}
		*l = *renewed
	}

	return errors.Join(errs...)
}
