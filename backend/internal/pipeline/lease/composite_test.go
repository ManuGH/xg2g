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
	failReleaseOwner     map[Owner]error
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
		failReleaseOwner:    make(map[Owner]error),
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
	if err, fail := r.failReleaseOwner[owner]; fail && err != nil {
		r.ops = append(r.ops, OperationRecord{Op: "ReleaseFailedOwner", Owner: owner, ID: id, Reason: reason})
		r.mu.Unlock()
		return nil, err
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

// TestCompositeManager_AcquireResultInconsistentOwnerRollback verifies that when backend returns a lease with valid ID
// but wrong Owner (e.g. returned owner-other instead of owner-1), rollback releases the lease using the returned LeaseToken.Owner,
// successfully releasing the lease!
func TestCompositeManager_AcquireResultInconsistentOwnerRollback(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	rec := NewRecordingLeaseBackend(mgr)
	cm := NewCompositeManager(CompositeManagerConfig{Backend: rec})

	ctx := context.Background()

	// Pre-acquire a real lease under owner-other for res:B
	lOther, err := mgr.Acquire(ctx, "owner-other", "res:B", 5*time.Minute)
	if err != nil {
		t.Fatalf("pre-acquire failed: %v", err)
	}

	// Override res:B acquire for owner-1 to return lOther (valid ID, but owner-other)
	rec.overrideAcquireLease["res:B"] = lOther

	reqs := []ResourceRequest{
		{Scope: "res:A", TTL: 5 * time.Minute},
		{Scope: "res:B", TTL: 5 * time.Minute},
	}

	_, err = cm.AcquireComposite(ctx, "owner-1", reqs)
	if err == nil {
		t.Fatalf("expected composite acquire error on mismatched owner")
	}

	if !errors.Is(err, ErrInvalidBackendResult) {
		t.Errorf("expected ErrInvalidBackendResult, got %v", err)
	}

	var compErr *CompositeAcquireError
	if !errors.As(err, &compErr) {
		t.Fatalf("expected *CompositeAcquireError, got %T", err)
	}

	// Verify both res:B (with returned owner-other) and res:A (with owner-1) were successfully released during rollback!
	if compErr.ReconciliationRequired {
		t.Errorf("expected ReconciliationRequired false because release with returned owner-other succeeded!")
	}

	ops := rec.Operations()
	releaseOps := make([]OperationRecord, 0)
	for _, op := range ops {
		if op.Op == "Release" {
			releaseOps = append(releaseOps, op)
		}
	}
	if len(releaseOps) != 2 {
		t.Fatalf("expected 2 release ops during rollback, got %d: %v", len(releaseOps), releaseOps)
	}

	// First release during LIFO rollback is res:B (with owner-other)
	if releaseOps[0].Owner != "owner-other" || releaseOps[0].ID != lOther.ID {
		t.Errorf("expected rollback release of res:B to use returned owner-other and ID %s, got owner=%s ID=%s",
			lOther.ID, releaseOps[0].Owner, releaseOps[0].ID)
	}

	// Verify res:B (lOther) is now StateReleased in real store
	lCheck, _ := mgr.Get("res:B")
	if lCheck != nil && lCheck.State != StateReleased {
		t.Errorf("expected res:B (lOther) to be RELEASED, got %s", lCheck.State)
	}
}

// TestCompositeManager_AcquireResultInconsistentOwnerRollbackFailure verifies that if release with returned owner fails,
// ReconciliationRequired is set to true and ErrCompensationIncomplete is wrapped.
func TestCompositeManager_AcquireResultInconsistentOwnerRollbackFailure(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	rec := NewRecordingLeaseBackend(mgr)
	cm := NewCompositeManager(CompositeManagerConfig{Backend: rec})

	ctx := context.Background()

	lOther, _ := mgr.Acquire(ctx, "owner-other", "res:B", 5*time.Minute)
	rec.overrideAcquireLease["res:B"] = lOther

	// Inject release failure for owner-other
	rec.failReleaseOwner["owner-other"] = errors.New("injected release failure for owner-other")

	reqs := []ResourceRequest{
		{Scope: "res:A", TTL: 5 * time.Minute},
		{Scope: "res:B", TTL: 5 * time.Minute},
	}

	_, err := cm.AcquireComposite(ctx, "owner-1", reqs)
	if err == nil {
		t.Fatalf("expected composite acquire error")
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
		t.Errorf("expected ReconciliationRequired true when release with returned owner fails")
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

	ops := rec.Operations()
	if len(ops) != 2 {
		t.Fatalf("expected 2 ops (Acquire then Release rollback), got %d: %v", len(ops), ops)
	}
	if ops[1].Op != "Release" || ops[1].Reason != ReasonRollback {
		t.Errorf("expected rollback release on pre-IDGen context cancellation, got %v", ops[1])
	}

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

// TestCompositeManager_PreservesBackendLeaseSnapshot verifies that AcquiredAt, ExpiresAt, ReasonCode,
// and State of the backend-returned Lease are preserved verbatim without synthesizing fields.
func TestCompositeManager_PreservesBackendLeaseSnapshot(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	rec := NewRecordingLeaseBackend(mgr)

	fixedAcquiredAt := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	fixedExpiresAt := fixedAcquiredAt.Add(90 * time.Second)

	customLease := &Lease{
		ID:         "lease-custom-123",
		Owner:      "worker-1",
		Scope:      "res:A",
		State:      StateAcquired,
		ReasonCode: ReasonAcquired,
		AcquiredAt: fixedAcquiredAt,
		ExpiresAt:  fixedExpiresAt,
	}

	rec.overrideAcquireLease["res:A"] = customLease

	cm := NewCompositeManager(CompositeManagerConfig{Backend: rec})
	ctx := context.Background()

	handle, err := cm.AcquireComposite(ctx, "worker-1", []ResourceRequest{{Scope: "res:A", TTL: 90 * time.Second}})
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	members := handle.Members()
	if len(members) != 1 {
		t.Fatalf("expected 1 member, got %d", len(members))
	}

	lSnapshot := members[0].Lease
	if lSnapshot.ID != "lease-custom-123" {
		t.Errorf("expected ID lease-custom-123, got %s", lSnapshot.ID)
	}
	if !lSnapshot.AcquiredAt.Equal(fixedAcquiredAt) {
		t.Errorf("expected AcquiredAt %v, got %v", fixedAcquiredAt, lSnapshot.AcquiredAt)
	}
	if !lSnapshot.ExpiresAt.Equal(fixedExpiresAt) {
		t.Errorf("expected ExpiresAt %v, got %v", fixedExpiresAt, lSnapshot.ExpiresAt)
	}
	if lSnapshot.ReasonCode != ReasonAcquired {
		t.Errorf("expected ReasonCode %s, got %s", ReasonAcquired, lSnapshot.ReasonCode)
	}
	if lSnapshot.State != StateAcquired {
		t.Errorf("expected State %s, got %s", StateAcquired, lSnapshot.State)
	}
}

// TestCompositeManager_MembersAndLeasesDeepCopy verifies that mutating returned slices or elements
// does NOT mutate the internal handle state.
func TestCompositeManager_MembersAndLeasesDeepCopy(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	cm := NewCompositeManager(CompositeManagerConfig{Backend: mgr})

	ctx := context.Background()
	handle, err := cm.AcquireComposite(ctx, "worker-1", []ResourceRequest{{Scope: "res:A", TTL: 5 * time.Minute}})
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	// Mutate Members() result
	members := handle.Members()
	members[0].Lease.Owner = "hacked-owner"
	members[0].Released = true

	// Mutate Leases() result
	leases := handle.Leases()
	leases[0].Scope = "hacked-scope"

	// Verify internal handle state is untouched
	freshMembers := handle.Members()
	if freshMembers[0].Lease.Owner != "worker-1" {
		t.Errorf("internal handle owner was mutated! got %s", freshMembers[0].Lease.Owner)
	}
	if freshMembers[0].Released {
		t.Errorf("internal handle release state was mutated!")
	}
	if freshMembers[0].Lease.Scope != "res:A" {
		t.Errorf("internal handle scope was mutated! got %s", freshMembers[0].Lease.Scope)
	}
}

// TestCompositeManager_RenewUpdatesSnapshotWithBackendLease verifies that RenewComposite replaces
// member Lease snapshot exclusively with the validated backend-returned Lease.
func TestCompositeManager_RenewUpdatesSnapshotWithBackendLease(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	rec := NewRecordingLeaseBackend(mgr)
	cm := NewCompositeManager(CompositeManagerConfig{Backend: rec})

	ctx := context.Background()
	handle, err := cm.AcquireComposite(ctx, "worker-1", []ResourceRequest{{Scope: "res:A", TTL: 5 * time.Minute}})
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	originalLease := handle.Leases()[0]
	renewedExpiresAt := time.Now().Add(20 * time.Minute)

	renewedLease := &Lease{
		ID:         originalLease.ID,
		Owner:      originalLease.Owner,
		Scope:      originalLease.Scope,
		State:      StateAcquired,
		ReasonCode: ReasonAcquired,
		AcquiredAt: originalLease.AcquiredAt,
		ExpiresAt:  renewedExpiresAt,
	}

	rec.overrideRenewLease[originalLease.ID] = renewedLease

	if err := cm.RenewComposite(ctx, handle, 20*time.Minute); err != nil {
		t.Fatalf("renew failed: %v", err)
	}

	updatedLeases := handle.Leases()
	if !updatedLeases[0].ExpiresAt.Equal(renewedExpiresAt) {
		t.Errorf("expected ExpiresAt %v after renew, got %v", renewedExpiresAt, updatedLeases[0].ExpiresAt)
	}
	if updatedLeases[0].ReasonCode != ReasonAcquired {
		t.Errorf("expected ReasonCode %s after renew, got %s", ReasonAcquired, updatedLeases[0].ReasonCode)
	}
}
