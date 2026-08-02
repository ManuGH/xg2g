// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package lease

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

type OperationRecord struct {
	Op     string // "Acquire", "AcquireFailed", "Renew", "RenewFailed", "Release", "ReleaseFailed"
	Owner  Owner
	ID     ID
	Scope  Scope
	Reason ReasonCode
}

type RecordingLeaseBackend struct {
	mu                   sync.Mutex
	backend              LeaseBackend
	ops                  []OperationRecord
	failAcquireScope     map[Scope]error
	failRenewScope       map[Scope]error
	failReleaseScope     map[Scope]error
	overrideAcquireLease map[Scope]*Lease
	overrideRenewLease   map[ID]*Lease
	onAcquire            func(scope Scope)
}

func NewRecordingLeaseBackend(realBackend LeaseBackend) *RecordingLeaseBackend {
	return &RecordingLeaseBackend{
		backend:              realBackend,
		failAcquireScope:    make(map[Scope]error),
		failRenewScope:      make(map[Scope]error),
		failReleaseScope:    make(map[Scope]error),
		overrideAcquireLease: make(map[Scope]*Lease),
		overrideRenewLease:   make(map[ID]*Lease),
	}
}

func (r *RecordingLeaseBackend) Acquire(ctx context.Context, owner Owner, scope Scope, ttl time.Duration) (*Lease, error) {
	r.mu.Lock()
	if r.onAcquire != nil {
		r.onAcquire(scope)
	}
	if err, fail := r.failAcquireScope[scope]; fail && err != nil {
		r.ops = append(r.ops, OperationRecord{Op: "AcquireFailed", Owner: owner, Scope: scope})
		r.mu.Unlock()
		return nil, err
	}
	if override, ok := r.overrideAcquireLease[scope]; ok {
		r.ops = append(r.ops, OperationRecord{Op: "AcquireOverride", Owner: owner, Scope: scope})
		r.mu.Unlock()
		return override, nil
	}
	r.mu.Unlock()

	l, err := r.backend.Acquire(ctx, owner, scope, ttl)
	r.mu.Lock()
	defer r.mu.Unlock()
	if err == nil {
		r.ops = append(r.ops, OperationRecord{Op: "Acquire", Owner: owner, ID: l.ID, Scope: scope})
	} else {
		r.ops = append(r.ops, OperationRecord{Op: "AcquireFailed", Owner: owner, Scope: scope})
	}
	return l, err
}

func (r *RecordingLeaseBackend) Renew(ctx context.Context, id ID, owner Owner, ttl time.Duration) (*Lease, error) {
	r.mu.Lock()
	if override, ok := r.overrideRenewLease[id]; ok {
		r.ops = append(r.ops, OperationRecord{Op: "RenewOverride", Owner: owner, ID: id})
		r.mu.Unlock()
		return override, nil
	}

	lTemp, errFind := r.backend.Renew(ctx, id, owner, ttl)
	var sc Scope
	if lTemp != nil {
		sc = lTemp.Scope
	}
	if err, fail := r.failRenewScope[sc]; fail && err != nil {
		r.ops = append(r.ops, OperationRecord{Op: "RenewFailed", Owner: owner, ID: id, Scope: sc})
		r.mu.Unlock()
		return nil, err
	}
	r.mu.Unlock()

	r.mu.Lock()
	defer r.mu.Unlock()
	if errFind == nil {
		r.ops = append(r.ops, OperationRecord{Op: "Renew", Owner: owner, ID: id, Scope: sc})
	} else {
		r.ops = append(r.ops, OperationRecord{Op: "RenewFailed", Owner: owner, ID: id, Scope: sc})
	}
	return lTemp, errFind
}

func (r *RecordingLeaseBackend) Release(ctx context.Context, id ID, owner Owner, reason ReasonCode) (*Lease, error) {
	r.mu.Lock()
	if ctx != nil && ctx.Err() != nil {
		r.ops = append(r.ops, OperationRecord{Op: "ReleaseFailedCtx", Owner: owner, ID: id, Reason: reason})
		r.mu.Unlock()
		return nil, ctx.Err()
	}
	r.mu.Unlock()

	l, err := r.backend.Release(ctx, id, owner, reason)
	r.mu.Lock()
	defer r.mu.Unlock()

	var sc Scope
	if l != nil {
		sc = l.Scope
	}

	if err, fail := r.failReleaseScope[sc]; fail && err != nil {
		r.ops = append(r.ops, OperationRecord{Op: "ReleaseFailed", Owner: owner, ID: id, Scope: sc, Reason: reason})
		return l, err
	}

	if err == nil {
		r.ops = append(r.ops, OperationRecord{Op: "Release", Owner: owner, ID: id, Scope: sc, Reason: reason})
	} else {
		r.ops = append(r.ops, OperationRecord{Op: "ReleaseFailed", Owner: owner, ID: id, Scope: sc, Reason: reason})
	}
	return l, err
}

func (r *RecordingLeaseBackend) Operations() []OperationRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := make([]OperationRecord, len(r.ops))
	copy(cp, r.ops)
	return cp
}

// TestCompositeManager_ParentContextCancellationWithDetachedRollback verifies that when parent context is cancelled,
// detached cleanup context successfully rolls back acquired leases in LIFO order without being affected by parent cancellation.
func TestCompositeManager_ParentContextCancellationWithDetachedRollback(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	rec := NewRecordingLeaseBackend(mgr)
	cm := NewCompositeManager(CompositeManagerConfig{
		Backend:        rec,
		CleanupTimeout: 2 * time.Second,
	})

	parentCtx, cancelParent := context.WithCancel(context.Background())

	reqs := []ResourceRequest{
		{Scope: "res:A", TTL: 5 * time.Minute},
		{Scope: "res:B", TTL: 5 * time.Minute},
	}

	// Deterministic hook: cancel parentCtx exactly when res:B acquire is attempted
	rec.onAcquire = func(scope Scope) {
		if scope == "res:B" {
			cancelParent()
		}
	}

	_, err := cm.AcquireComposite(parentCtx, "owner-1", reqs)
	if err == nil {
		t.Fatalf("expected error on cancelled parent context")
	}

	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected primary error to be context.Canceled, got %v", err)
	}

	var compErr *CompositeAcquireError
	if !errors.As(err, &compErr) {
		t.Fatalf("expected *CompositeAcquireError, got %T", err)
	}

	// Verify res:A was successfully compensated using detached context
	if len(compErr.Compensated) != 1 || compErr.Compensated[0] != "res:A" {
		t.Errorf("expected compensated scope [res:A], got %v", compErr.Compensated)
	}
	if len(compErr.Failures) != 0 {
		t.Errorf("expected 0 compensation failures, got %d", len(compErr.Failures))
	}
	if compErr.ReconciliationRequired {
		t.Errorf("expected ReconciliationRequired false for clean rollback")
	}

	// Verify res:A is free again
	lA, err := mgr.Acquire(context.Background(), "owner-2", "res:A", 5*time.Minute)
	if err != nil {
		t.Fatalf("res:A should be free after rollback, got %v", err)
	}
	_, _ = mgr.Release(context.Background(), lA.ID, "owner-2", ReasonReleasedByOwner)
}

// TestCompositeManager_AcquireResultInconsistentTokenRollback verifies that when backend returns a lease with valid ID
// but wrong Scope/Owner, the returned lease token is included in the rollback set and released!
func TestCompositeManager_AcquireResultInconsistentTokenRollback(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	rec := NewRecordingLeaseBackend(mgr)
	cm := NewCompositeManager(CompositeManagerConfig{Backend: rec})

	ctx := context.Background()

	// Pre-acquire a real lease to get a valid ID with wrong scope
	lWrong, _ := mgr.Acquire(ctx, "owner-1", "res:WRONG", 5*time.Minute)
	badLeaseToken := &Lease{
		ID:        lWrong.ID,
		Owner:     "owner-1",
		Scope:     "res:BAD_SCOPE", // Mismatched scope
		State:     StateAcquired,
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}

	// Inject returning badLeaseToken when res:B is acquired
	rec.overrideAcquireLease["res:B"] = badLeaseToken

	reqs := []ResourceRequest{
		{Scope: "res:A", TTL: 5 * time.Minute},
		{Scope: "res:B", TTL: 5 * time.Minute},
	}

	_, err := cm.AcquireComposite(ctx, "owner-1", reqs)
	if err == nil {
		t.Fatalf("expected composite acquire error on mismatched lease result")
	}

	if !errors.Is(err, ErrInvalidBackendResult) {
		t.Errorf("expected ErrInvalidBackendResult, got %v", err)
	}

	var compErr *CompositeAcquireError
	if !errors.As(err, &compErr) {
		t.Fatalf("expected *CompositeAcquireError, got %T", err)
	}

	// Verify both res:B (bad lease token) and res:A were included in LIFO rollback and released!
	ops := rec.Operations()
	relOps := make([]OperationRecord, 0)
	for _, op := range ops {
		if op.Op == "Release" {
			relOps = append(relOps, op)
		}
	}
	if len(relOps) != 2 {
		t.Fatalf("expected 2 release ops during rollback, got %d: %v", len(relOps), relOps)
	}

	// First release during LIFO rollback is res:B (lWrong.ID), second is res:A
	if relOps[0].ID != lWrong.ID {
		t.Errorf("expected first rollback release to be returned lease token ID %s, got %s", lWrong.ID, relOps[0].ID)
	}

	// Verify res:WRONG (lWrong.ID) is now RELEASED in real store
	lCheck, _ := mgr.Get("res:WRONG")
	if lCheck != nil && lCheck.State != StateReleased {
		t.Errorf("expected lWrong to be released by rollback, got state %s", lCheck.State)
	}
}

// TestCompositeManager_AcquireResultNilLeaseReconciliationRequired verifies that returning a nil lease with nil error
// sets ReconciliationRequired to true.
func TestCompositeManager_AcquireResultNilLeaseReconciliationRequired(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	rec := NewRecordingLeaseBackend(mgr)
	cm := NewCompositeManager(CompositeManagerConfig{Backend: rec})

	ctx := context.Background()
	// Override res:A to return nil lease with nil error
	rec.overrideAcquireLease["res:A"] = nil

	reqs := []ResourceRequest{{Scope: "res:A", TTL: 5 * time.Minute}}

	_, err := cm.AcquireComposite(ctx, "owner-1", reqs)
	if err == nil {
		t.Fatalf("expected error on nil lease result")
	}

	if !errors.Is(err, ErrInvalidBackendResult) {
		t.Errorf("expected ErrInvalidBackendResult, got %v", err)
	}
	if !errors.Is(err, ErrCompensationIncomplete) {
		t.Errorf("expected ErrCompensationIncomplete via errors.Is, got %v", err)
	}

	var compErr *CompositeAcquireError
	if !errors.As(err, &compErr) {
		t.Fatalf("expected *CompositeAcquireError, got %T", err)
	}
	if !compErr.ReconciliationRequired {
		t.Errorf("expected ReconciliationRequired true for unidentifiable nil lease result")
	}
}

// TestCompositeManager_RenewScopeMismatchDegradesState verifies that if backend returns a renewed lease
// with mismatched scope/owner, handle state transitions to DEGRADED without corrupting member lease snapshot.
func TestCompositeManager_RenewScopeMismatchDegradesState(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	rec := NewRecordingLeaseBackend(mgr)
	cm := NewCompositeManager(CompositeManagerConfig{Backend: rec})

	ctx := context.Background()
	reqs := []ResourceRequest{
		{Scope: "res:A", TTL: 5 * time.Minute},
		{Scope: "res:B", TTL: 5 * time.Minute},
	}

	handle, err := cm.AcquireComposite(ctx, "worker-1", reqs)
	if err != nil {
		t.Fatalf("acquire composite failed: %v", err)
	}

	originalLeaseA := handle.Leases()[0]

	// Inject returning mismatched scope lease on renew of res:A
	badRenewLease := &Lease{
		ID:        originalLeaseA.ID,
		Owner:     "worker-1",
		Scope:     "res:WRONG_SCOPE",
		State:     StateAcquired,
		ExpiresAt: time.Now().Add(20 * time.Minute),
	}
	rec.overrideRenewLease[originalLeaseA.ID] = badRenewLease

	err = cm.RenewComposite(ctx, handle, 10*time.Minute)
	if err == nil {
		t.Fatalf("expected renew error on scope mismatch")
	}
	if !errors.Is(err, ErrInvalidBackendResult) {
		t.Errorf("expected ErrInvalidBackendResult, got %v", err)
	}

	if handle.State() != CompositeStateDegraded {
		t.Errorf("expected state DEGRADED after invalid renew result, got %s", handle.State())
	}

	// Verify member snapshot for res:A was NOT overwritten with badRenewLease scope
	currentLeases := handle.Leases()
	if currentLeases[0].Scope != "res:A" {
		t.Errorf("member snapshot scope was corrupted! expected res:A, got %s", currentLeases[0].Scope)
	}
}

// TestCompositeManager_PreIDGenContextCancellation verifies that context cancellation after last acquire
// but before ID generation triggers LIFO rollback.
func TestCompositeManager_PreIDGenContextCancellation(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	rec := NewRecordingLeaseBackend(mgr)

	cancelCtx, cancel := context.WithCancel(context.Background())

	// ID Generator that cancels context before generating ID
	cancellingIDGen := func() (ID, error) {
		cancel()
		return defaultCompositeIDGen()
	}

	cm := NewCompositeManager(CompositeManagerConfig{
		Backend:     rec,
		IDGenerator: cancellingIDGen,
	})

	reqs := []ResourceRequest{{Scope: "res:A", TTL: 5 * time.Minute}}

	_, err := cm.AcquireComposite(cancelCtx, "worker-1", reqs)
	if err == nil {
		t.Fatalf("expected error on cancelled context pre IDGen")
	}

	// Verify res:A was rolled back
	ops := rec.Operations()
	if len(ops) != 2 {
		t.Fatalf("expected 2 ops (Acquire then Release rollback), got %d: %v", len(ops), ops)
	}
	if ops[1].Op != "Release" || ops[1].Reason != ReasonRollback {
		t.Errorf("expected rollback release on pre-IDGen context cancellation, got %v", ops[1])
	}

	// Verify res:A is free again
	lA, err := mgr.Acquire(context.Background(), "worker-2", "res:A", 5*time.Minute)
	if err != nil {
		t.Fatalf("res:A should be free after rollback, got %v", err)
	}
	_, _ = mgr.Release(context.Background(), lA.ID, "worker-2", ReasonReleasedByOwner)
}

// TestCompositeManager_PreValidation verifies up-front validation with 0 backend calls.
func TestCompositeManager_PreValidation(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	rec := NewRecordingLeaseBackend(mgr)
	cm := NewCompositeManager(CompositeManagerConfig{Backend: rec})

	ctx := context.Background()

	// 1. Empty Owner
	_, err := cm.AcquireComposite(ctx, "", []ResourceRequest{{Scope: "res:A", TTL: 5 * time.Minute}})
	if !errors.Is(err, ErrInvalidOwner) {
		t.Errorf("expected ErrInvalidOwner, got %v", err)
	}

	// 2. Empty Requests
	_, err = cm.AcquireComposite(ctx, "owner-1", nil)
	if !errors.Is(err, ErrInvalidScope) {
		t.Errorf("expected ErrInvalidScope for nil requests, got %v", err)
	}

	// 3. Empty Scope
	_, err = cm.AcquireComposite(ctx, "owner-1", []ResourceRequest{{Scope: "", TTL: 5 * time.Minute}})
	if !errors.Is(err, ErrInvalidScope) {
		t.Errorf("expected ErrInvalidScope for empty scope, got %v", err)
	}

	// 4. Invalid TTL
	_, err = cm.AcquireComposite(ctx, "owner-1", []ResourceRequest{{Scope: "res:A", TTL: 0}})
	if !errors.Is(err, ErrInvalidTTL) {
		t.Errorf("expected ErrInvalidTTL for zero TTL, got %v", err)
	}

	// 5. Duplicate Scope
	reqs := []ResourceRequest{
		{Scope: "tuner:0", TTL: 5 * time.Minute},
		{Scope: "tuner:0", TTL: 5 * time.Minute},
	}
	_, err = cm.AcquireComposite(ctx, "owner-1", reqs)
	if !errors.Is(err, ErrDuplicateScope) {
		t.Errorf("expected ErrDuplicateScope, got %v", err)
	}

	// Assert 0 backend calls performed
	if len(rec.Operations()) != 0 {
		t.Errorf("expected 0 backend calls during pre-validation failures, got %d", len(rec.Operations()))
	}
}

// TestCompositeManager_DeterministicScopeSorting verifies deterministic scope sorting.
func TestCompositeManager_DeterministicScopeSorting(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	rec := NewRecordingLeaseBackend(mgr)
	cm := NewCompositeManager(CompositeManagerConfig{Backend: rec})

	ctx := context.Background()
	reqs := []ResourceRequest{
		{Scope: "res:Z", TTL: 5 * time.Minute},
		{Scope: "res:A", TTL: 5 * time.Minute},
		{Scope: "res:M", TTL: 5 * time.Minute},
	}

	handle, err := cm.AcquireComposite(ctx, "owner-1", reqs)
	if err != nil {
		t.Fatalf("unexpected acquire error: %v", err)
	}
	if handle.State() != CompositeStateAcquired {
		t.Errorf("expected state ACQUIRED, got %s", handle.State())
	}
	defer func() {
		_ = cm.ReleaseComposite(ctx, handle, ReasonReleasedByOwner)
	}()

	ops := rec.Operations()
	if len(ops) != 3 {
		t.Fatalf("expected 3 acquire operations, got %d", len(ops))
	}
	expectedOrder := []Scope{"res:A", "res:M", "res:Z"}
	for i, expected := range expectedOrder {
		if ops[i].Scope != expected {
			t.Errorf("expected op index %d scope %s, got %s", i, expected, ops[i].Scope)
		}
	}
}

// TestCompositeManager_FullLifecycle tests successful acquisition, renewal, release, and encapsulated accessors.
func TestCompositeManager_FullLifecycle(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	rec := NewRecordingLeaseBackend(mgr)
	cm := NewCompositeManager(CompositeManagerConfig{Backend: rec})

	ctx := context.Background()
	reqs := []ResourceRequest{
		{Scope: "tuner:0", TTL: 10 * time.Minute},
		{Scope: "encoder:0", TTL: 10 * time.Minute},
	}

	handle, err := cm.AcquireComposite(ctx, "worker-1", reqs)
	if err != nil {
		t.Fatalf("acquire composite failed: %v", err)
	}
	if handle.State() != CompositeStateAcquired {
		t.Errorf("expected state ACQUIRED, got %s", handle.State())
	}
	if handle.ID() == "" || handle.Owner() != "worker-1" {
		t.Errorf("expected valid ID and owner, got ID=%s, owner=%s", handle.ID(), handle.Owner())
	}

	// Verify deep-copied leases snapshot
	leases := handle.Leases()
	if len(leases) != 2 {
		t.Fatalf("expected 2 active leases in snapshot, got %d", len(leases))
	}

	// Renew
	if err := cm.RenewComposite(ctx, handle, 20*time.Minute); err != nil {
		t.Fatalf("renew composite failed: %v", err)
	}

	// Release
	if err := cm.ReleaseComposite(ctx, handle, ReasonReleasedByOwner); err != nil {
		t.Fatalf("release composite failed: %v", err)
	}
	if handle.State() != CompositeStateReleased {
		t.Errorf("expected state RELEASED, got %s", handle.State())
	}

	// Idempotent release
	if err := cm.ReleaseComposite(ctx, handle, ReasonReleasedByOwner); err != nil {
		t.Fatalf("idempotent release failed: %v", err)
	}

	// Verify LIFO release order in operations (tuner:0 first [index 1], encoder:0 second [index 0])
	ops := rec.Operations()
	releaseOps := make([]OperationRecord, 0)
	for _, op := range ops {
		if op.Op == "Release" {
			releaseOps = append(releaseOps, op)
		}
	}
	if len(releaseOps) != 2 {
		t.Fatalf("expected 2 release ops, got %d", len(releaseOps))
	}
	if releaseOps[0].Scope != "tuner:0" || releaseOps[1].Scope != "encoder:0" {
		t.Errorf("expected LIFO release order (tuner:0 then encoder:0), got %v then %v", releaseOps[0].Scope, releaseOps[1].Scope)
	}
}

// TestCompositeManager_RaceConditions tests concurrent competing composite acquisitions under -race.
func TestCompositeManager_RaceConditions(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 10 * time.Millisecond})
	defer mgr.Close()
	cm := NewCompositeManager(CompositeManagerConfig{Backend: mgr})

	const numWorkers = 30
	const opsPerWorker = 50
	var wg sync.WaitGroup

	ctx := context.Background()

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		workerID := Owner(fmt.Sprintf("comp-worker-%d", i))

		go func(wOwner Owner, wIdx int) {
			defer wg.Done()
			for j := 0; j < opsPerWorker; j++ {
				var reqs []ResourceRequest
				if wIdx%2 == 0 {
					reqs = []ResourceRequest{
						{Scope: "res:B", TTL: 20 * time.Millisecond},
						{Scope: "res:A", TTL: 20 * time.Millisecond},
					}
				} else {
					reqs = []ResourceRequest{
						{Scope: "res:A", TTL: 20 * time.Millisecond},
						{Scope: "res:C", TTL: 20 * time.Millisecond},
					}
				}

				handle, err := cm.AcquireComposite(ctx, wOwner, reqs)
				if err == nil && handle != nil {
					if handle.State() != CompositeStateAcquired {
						t.Errorf("expected state ACQUIRED for successful handle")
					}
					time.Sleep(1 * time.Millisecond)
					if j%2 == 0 {
						_ = cm.RenewComposite(ctx, handle, 50*time.Millisecond)
					}
					errRel := cm.ReleaseComposite(ctx, handle, ReasonReleasedByOwner)
					if errRel != nil && !errors.Is(errRel, ErrOperationInProgress) {
						t.Errorf("unexpected release error: %v", errRel)
					}
				}
			}
		}(workerID, i)
	}

	wg.Wait()
}
