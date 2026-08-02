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
	ErrDuplicateScope         = errors.New("duplicate lease scope")
	ErrInvalidBackendResult   = errors.New("invalid lease backend result")
	ErrOperationInProgress    = errors.New("composite operation in progress")
	ErrCompensationIncomplete = errors.New("compensation incomplete - reconciliation required")
)

const (
	DefaultCleanupTimeout = 5 * time.Second
)

// LeaseBackend exposes the contract required by CompositeManager for single-resource lease operations.
type LeaseBackend interface {
	Acquire(ctx context.Context, owner Owner, scope Scope, ttl time.Duration) (*Lease, error)
	Renew(ctx context.Context, id ID, owner Owner, ttl time.Duration) (*Lease, error)
	Release(ctx context.Context, id ID, owner Owner, reason ReasonCode) (*Lease, error)
}

// LeaseToken represents the immutable release token returned by a backend lease operation.
type LeaseToken struct {
	ID    ID
	Owner Owner
	Scope Scope
}

type acquiredResource struct {
	RequestedScope Scope
	Token          LeaseToken
}

// CompositeState represents the lifecycle status of an acquired multi-resource composite lease.
type CompositeState string

const (
	CompositeStateAcquired CompositeState = "ACQUIRED"
	CompositeStateReleased CompositeState = "RELEASED"
	CompositeStateDegraded CompositeState = "DEGRADED"
)

type compositeOperation string

const (
	operationNone    compositeOperation = ""
	operationRenew   compositeOperation = "RENEW"
	operationRelease compositeOperation = "RELEASE"
)

// ResourceRequest defines a single resource scope and TTL to be acquired as part of a composite lease.
type ResourceRequest struct {
	Scope Scope
	TTL   time.Duration
}

// CompositeMember tracks the lease snapshot and cleanup state for a single member of a composite lease.
type CompositeMember struct {
	Lease    Lease
	Released bool
	LastErr  error
}

// CompensationFailure records a single failed release during LIFO rollback compensation.
type CompensationFailure struct {
	Scope Scope
	ID    ID
	Err   error
}

// CompositeAcquireError describes a composite acquisition failure, preserving the primary cause,
// list of successfully compensated scopes, any compensation release failures, and reconciliation flag.
type CompositeAcquireError struct {
	FailedScope            Scope
	Cause                  error
	Compensated            []Scope
	Failures               []CompensationFailure
	ReconciliationRequired bool
}

func (e *CompositeAcquireError) Error() string {
	recStr := ""
	if e.ReconciliationRequired {
		recStr = " [RECONCILIATION REQUIRED]"
	}
	return fmt.Sprintf("composite acquire failed at scope %s: %v (compensated %d scopes, %d rollback failures)%s",
		e.FailedScope, e.Cause, len(e.Compensated), len(e.Failures), recStr)
}

func (e *CompositeAcquireError) Unwrap() []error {
	errs := make([]error, 0, 1+len(e.Failures))
	if e.Cause != nil {
		errs = append(errs, e.Cause)
	}
	if e.ReconciliationRequired {
		errs = append(errs, ErrCompensationIncomplete)
	}
	for _, f := range e.Failures {
		if f.Err != nil {
			errs = append(errs, f.Err)
		}
	}
	return errs
}

// CompositeLeaseHandle encapsulates multiple acquired resource leases for a single owner with encapsulated state.
type CompositeLeaseHandle struct {
	mu        sync.Mutex
	id        ID
	owner     Owner
	state     CompositeState
	members   []*CompositeMember
	operation compositeOperation
}

// ID returns the composite handle's unique identifier.
func (h *CompositeLeaseHandle) ID() ID {
	if h == nil {
		return ""
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.id
}

// Owner returns the handle owner.
func (h *CompositeLeaseHandle) Owner() Owner {
	if h == nil {
		return ""
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.owner
}

// State returns the current CompositeState thread-safely.
func (h *CompositeLeaseHandle) State() CompositeState {
	if h == nil {
		return ""
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.state
}

// Leases returns deep-copied snapshots of active (non-released) member leases.
func (h *CompositeLeaseHandle) Leases() []Lease {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	res := make([]Lease, 0, len(h.members))
	for _, m := range h.members {
		if !m.Released {
			res = append(res, m.Lease)
		}
	}
	return res
}

// Members returns deep-copied snapshots of all composite members and their cleanup status.
func (h *CompositeLeaseHandle) Members() []CompositeMember {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	res := make([]CompositeMember, len(h.members))
	for i, m := range h.members {
		res[i] = *m
	}
	return res
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
	Backend        LeaseBackend
	IDGenerator    IDGenerator
	CleanupTimeout time.Duration
}

// CompositeManager orchestrates multi-resource acquisitions with deterministic scope ordering,
// LIFO rollback compensation, explicit state tracking, and failure injection support.
//
// All identifiable partial resources are compensated using a detached, bounded cleanup context.
// Incomplete or impossible compensation is surfaced through CompositeAcquireError.ReconciliationRequired
// and ErrCompensationIncomplete.
type CompositeManager struct {
	backend        LeaseBackend
	idGen          IDGenerator
	cleanupTimeout time.Duration
}

// NewCompositeManager creates a new CompositeManager using the specified configuration.
func NewCompositeManager(config CompositeManagerConfig) *CompositeManager {
	idGen := config.IDGenerator
	if idGen == nil {
		idGen = defaultCompositeIDGen
	}
	cleanupTimeout := config.CleanupTimeout
	if cleanupTimeout <= 0 {
		cleanupTimeout = DefaultCleanupTimeout
	}
	return &CompositeManager{
		backend:        config.Backend,
		idGen:          idGen,
		cleanupTimeout: cleanupTimeout,
	}
}

func nonNilContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

// AcquireComposite validates requests up-front, sorts requests deterministically by Scope (lexicographically),
// and sequentially acquires each resource.
//
// If any resource acquisition fails, context is cancelled, or invalid lease is returned, all previously acquired leases
// in the batch are released in LIFO (reverse acquisition) order with ReasonRollback using a detached cleanup context.
func (cm *CompositeManager) AcquireComposite(ctx context.Context, owner Owner, requests []ResourceRequest) (*CompositeLeaseHandle, error) {
	if cm == nil || cm.backend == nil {
		return nil, fmt.Errorf("%w: composite manager backend is nil", ErrBindingUnavailable)
	}
	if ctx != nil && ctx.Err() != nil {
		return nil, ctx.Err()
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

	acquired := make([]acquiredResource, 0, len(sortedReqs))

	getCleanupCtx := func() (context.Context, context.CancelFunc) {
		return context.WithTimeout(context.WithoutCancel(nonNilContext(ctx)), cm.cleanupTimeout)
	}

	// 3. Sequential acquisition loop
	for _, req := range sortedReqs {
		if ctx != nil && ctx.Err() != nil {
			cCtx, cancel := getCleanupCtx()
			defer cancel()
			return nil, cm.rollbackAcquired(cCtx, owner, req.Scope, ctx.Err(), acquired, false)
		}

		l, err := cm.backend.Acquire(ctx, owner, req.Scope, req.TTL)
		if err != nil {
			cCtx, cancel := getCleanupCtx()
			defer cancel()
			return nil, cm.rollbackAcquired(cCtx, owner, req.Scope, err, acquired, false)
		}

		// Validate Backend Result
		if l == nil || l.ID == "" {
			backendErr := fmt.Errorf("%w: missing lease identity for scope %s", ErrInvalidBackendResult, req.Scope)
			cCtx, cancel := getCleanupCtx()
			defer cancel()
			return nil, cm.rollbackAcquired(cCtx, owner, req.Scope, backendErr, acquired, true)
		}

		tok := LeaseToken{ID: l.ID, Owner: l.Owner, Scope: l.Scope}

		if l.Owner != owner || l.Scope != req.Scope {
			backendErr := fmt.Errorf("%w: requested owner=%q scope=%q, got owner=%q scope=%q",
				ErrInvalidBackendResult, owner, req.Scope, l.Owner, l.Scope)
			// Include returned token with actual returned Owner/Scope in rollback set!
			acquiredWithReturned := append(acquired, acquiredResource{
				RequestedScope: req.Scope,
				Token:          tok,
			})
			cCtx, cancel := getCleanupCtx()
			defer cancel()
			return nil, cm.rollbackAcquired(cCtx, owner, req.Scope, backendErr, acquiredWithReturned, false)
		}

		acquired = append(acquired, acquiredResource{
			RequestedScope: req.Scope,
			Token:          tok,
		})
	}

	// 4. Pre-IDGen Context Check
	if ctx != nil && ctx.Err() != nil {
		cCtx, cancel := getCleanupCtx()
		defer cancel()
		return nil, cm.rollbackAcquired(cCtx, owner, "Pre-IDGen", ctx.Err(), acquired, false)
	}

	// 5. ID Generation
	compID, err := cm.idGen()
	if err != nil {
		idErr := fmt.Errorf("ID generation failed: %w", err)
		cCtx, cancel := getCleanupCtx()
		defer cancel()
		return nil, cm.rollbackAcquired(cCtx, owner, "IDGen", idErr, acquired, false)
	}

	// 6. Post-IDGen / Pre-Commit Context Check
	if ctx != nil && ctx.Err() != nil {
		cCtx, cancel := getCleanupCtx()
		defer cancel()
		return nil, cm.rollbackAcquired(cCtx, owner, "Commit", ctx.Err(), acquired, false)
	}

	members := make([]*CompositeMember, len(acquired))
	for i, res := range acquired {
		members[i] = &CompositeMember{
			Lease: Lease{
				ID:        res.Token.ID,
				Owner:     res.Token.Owner,
				Scope:     res.Token.Scope,
				State:     StateAcquired,
				AcquiredAt: time.Now(),
			},
			Released: false,
		}
	}

	return &CompositeLeaseHandle{
		id:      compID,
		owner:   owner,
		state:   CompositeStateAcquired,
		members: members,
	}, nil
}

func (cm *CompositeManager) rollbackAcquired(cleanupCtx context.Context, requestedOwner Owner, failedScope Scope, cause error, acquired []acquiredResource, unidentifiableLeak bool) error {
	compensated := make([]Scope, 0, len(acquired))
	var compFailures []CompensationFailure

	// LIFO rollback loop (reverse acquisition order using actual returned LeaseToken identity)
	for j := len(acquired) - 1; j >= 0; j-- {
		res := acquired[j]
		tok := res.Token
		_, relErr := cm.backend.Release(cleanupCtx, tok.ID, tok.Owner, ReasonRollback)
		if relErr != nil {
			compFailures = append(compFailures, CompensationFailure{
				Scope: tok.Scope,
				ID:    tok.ID,
				Err:   relErr,
			})
		} else {
			compensated = append(compensated, tok.Scope)
		}
	}

	reconRequired := unidentifiableLeak || len(compFailures) > 0

	return &CompositeAcquireError{
		FailedScope:            failedScope,
		Cause:                  cause,
		Compensated:            compensated,
		Failures:               compFailures,
		ReconciliationRequired: reconRequired,
	}
}

// ReleaseComposite releases all un-released member leases in a composite handle in LIFO (reverse acquisition) order.
// Backend I/O operations are performed OUTSIDE the handle lock.
func (cm *CompositeManager) ReleaseComposite(ctx context.Context, handle *CompositeLeaseHandle, reason ReasonCode) error {
	if cm == nil || cm.backend == nil {
		return fmt.Errorf("%w: composite manager backend is nil", ErrBindingUnavailable)
	}
	if handle == nil {
		return nil
	}

	if reason == "" {
		reason = ReasonReleasedByOwner
	}

	// 1. Lock handle, check state & operation, extract LIFO snapshot of unreleased members
	handle.mu.Lock()
	if handle.state == CompositeStateReleased {
		handle.mu.Unlock()
		return nil
	}
	if handle.operation != operationNone {
		handle.mu.Unlock()
		return ErrOperationInProgress
	}
	handle.operation = operationRelease

	type releaseWork struct {
		index int
		lease Lease
	}
	workList := make([]releaseWork, 0, len(handle.members))
	for i := len(handle.members) - 1; i >= 0; i-- {
		m := handle.members[i]
		if !m.Released {
			workList = append(workList, releaseWork{index: i, lease: m.Lease})
		}
	}
	handle.mu.Unlock()

	// 2. Perform backend I/O OUTSIDE handle lock
	type workResult struct {
		index int
		err   error
	}
	results := make([]workResult, 0, len(workList))
	for _, w := range workList {
		if ctx != nil && ctx.Err() != nil {
			results = append(results, workResult{index: w.index, err: ctx.Err()})
			continue
		}
		_, err := cm.backend.Release(ctx, w.lease.ID, w.lease.Owner, reason)
		results = append(results, workResult{index: w.index, err: err})
	}

	// 3. Re-lock handle to apply results and update state
	handle.mu.Lock()
	defer handle.mu.Unlock()
	handle.operation = operationNone

	var errs []error
	for _, res := range results {
		m := handle.members[res.index]
		if res.err == nil {
			m.Released = true
			m.LastErr = nil
		} else {
			m.LastErr = res.err
			errs = append(errs, fmt.Errorf("failed to release scope %s (id %s): %w", m.Lease.Scope, m.Lease.ID, res.err))
		}
	}

	allReleased := true
	for _, m := range handle.members {
		if !m.Released {
			allReleased = false
			break
		}
	}

	if allReleased {
		handle.state = CompositeStateReleased
	} else {
		handle.state = CompositeStateDegraded
	}

	return errors.Join(errs...)
}

// RenewComposite extends the expiration time of all active member leases in a composite handle.
// Backend I/O operations are performed OUTSIDE the handle lock.
// Only allowed when handle is in CompositeStateAcquired.
// Validates renewed lease identity and scope; if invalid or failed, sets handle state to CompositeStateDegraded
// without overwriting member lease snapshot.
func (cm *CompositeManager) RenewComposite(ctx context.Context, handle *CompositeLeaseHandle, ttl time.Duration) error {
	if cm == nil || cm.backend == nil {
		return fmt.Errorf("%w: composite manager backend is nil", ErrBindingUnavailable)
	}
	if handle == nil {
		return nil
	}
	if ttl <= 0 {
		return fmt.Errorf("%w: TTL must be greater than 0", ErrInvalidTTL)
	}

	// 1. Lock handle, check state & operation, extract snapshot of members to renew
	handle.mu.Lock()
	if handle.state != CompositeStateAcquired {
		handle.mu.Unlock()
		return fmt.Errorf("cannot renew composite in state %s", handle.state)
	}
	if handle.operation != operationNone {
		handle.mu.Unlock()
		return ErrOperationInProgress
	}
	handle.operation = operationRenew

	type renewWork struct {
		index int
		lease Lease
	}
	workList := make([]renewWork, 0, len(handle.members))
	for i, m := range handle.members {
		if !m.Released {
			workList = append(workList, renewWork{index: i, lease: m.Lease})
		}
	}
	handle.mu.Unlock()

	// 2. Perform backend I/O OUTSIDE handle lock
	type renewResult struct {
		index   int
		renewed *Lease
		err     error
	}
	results := make([]renewResult, 0, len(workList))
	for _, w := range workList {
		if ctx != nil && ctx.Err() != nil {
			results = append(results, renewResult{index: w.index, err: ctx.Err()})
			break
		}
		rLease, err := cm.backend.Renew(ctx, w.lease.ID, w.lease.Owner, ttl)
		if err == nil {
			if rLease == nil || rLease.ID != w.lease.ID || rLease.Owner != w.lease.Owner || rLease.Scope != w.lease.Scope {
				err = fmt.Errorf("%w: renewed lease scope/owner mismatch (expected scope=%s owner=%s)", ErrInvalidBackendResult, w.lease.Scope, w.lease.Owner)
			}
		}
		results = append(results, renewResult{index: w.index, renewed: rLease, err: err})
		if err != nil {
			break // Fail-fast on first renewal failure
		}
	}

	// 3. Re-lock handle to apply results and update state
	handle.mu.Lock()
	defer handle.mu.Unlock()
	handle.operation = operationNone

	var errs []error
	for _, res := range results {
		m := handle.members[res.index]
		if res.err == nil && res.renewed != nil {
			m.Lease = *res.renewed
			m.LastErr = nil
		} else {
			m.LastErr = res.err
			handle.state = CompositeStateDegraded
			errs = append(errs, fmt.Errorf("failed to renew scope %s (id %s): %w", m.Lease.Scope, m.Lease.ID, res.err))
			break
		}
	}

	return errors.Join(errs...)
}
