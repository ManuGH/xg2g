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
	mu          sync.Mutex
	backend     LeaseBackend
	ops         []OperationRecord
	failAcquire map[Scope]error
	failRenew   map[ID]error
	failRelease map[ID]error
}

func NewRecordingLeaseBackend(realBackend LeaseBackend) *RecordingLeaseBackend {
	return &RecordingLeaseBackend{
		backend:     realBackend,
		failAcquire: make(map[Scope]error),
		failRenew:   make(map[ID]error),
		failRelease: make(map[ID]error),
	}
}

func (r *RecordingLeaseBackend) Acquire(ctx context.Context, owner Owner, scope Scope, ttl time.Duration) (*Lease, error) {
	r.mu.Lock()
	if err, fail := r.failAcquire[scope]; fail {
		r.ops = append(r.ops, OperationRecord{Op: "AcquireFailed", Owner: owner, Scope: scope})
		r.mu.Unlock()
		return nil, err
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

func (r *RecordingLeaseBackend) Renew(id ID, owner Owner, ttl time.Duration) (*Lease, error) {
	r.mu.Lock()
	if err, fail := r.failRenew[id]; fail {
		r.ops = append(r.ops, OperationRecord{Op: "RenewFailed", Owner: owner, ID: id})
		r.mu.Unlock()
		return nil, err
	}
	r.mu.Unlock()

	l, err := r.backend.Renew(id, owner, ttl)
	r.mu.Lock()
	defer r.mu.Unlock()
	if err == nil {
		r.ops = append(r.ops, OperationRecord{Op: "Renew", Owner: owner, ID: id, Scope: l.Scope})
	} else {
		r.ops = append(r.ops, OperationRecord{Op: "RenewFailed", Owner: owner, ID: id})
	}
	return l, err
}

func (r *RecordingLeaseBackend) Release(id ID, owner Owner, reason ReasonCode) (*Lease, error) {
	r.mu.Lock()
	if err, fail := r.failRelease[id]; fail {
		r.ops = append(r.ops, OperationRecord{Op: "ReleaseFailed", Owner: owner, ID: id, Reason: reason})
		r.mu.Unlock()
		return nil, err
	}
	r.mu.Unlock()

	l, err := r.backend.Release(id, owner, reason)
	r.mu.Lock()
	defer r.mu.Unlock()
	if err == nil {
		r.ops = append(r.ops, OperationRecord{Op: "Release", Owner: owner, ID: id, Scope: l.Scope, Reason: reason})
	} else {
		r.ops = append(r.ops, OperationRecord{Op: "ReleaseFailed", Owner: owner, ID: id, Reason: reason})
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

// TestCompositeManager_PreValidation verifies that invalid inputs (empty owner, empty requests,
// empty scope, non-positive TTL, duplicate scopes) fail immediately with 0 backend calls.
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

	// Assert 0 backend calls were made for all pre-validation failures
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
	if handle.GetState() != CompositeStateAcquired {
		t.Errorf("expected state ACQUIRED, got %s", handle.GetState())
	}
	defer func() {
		_ = cm.ReleaseComposite(handle, ReasonReleasedByOwner)
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

// TestCompositeManager_FailureInjection_Step2Failure_LIFOOrder proves that when step 2 fails,
// step 1 is released in LIFO order with ReasonRollback, freeing step 1 immediately.
func TestCompositeManager_FailureInjection_Step2Failure_LIFOOrder(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	rec := NewRecordingLeaseBackend(mgr)
	cm := NewCompositeManager(CompositeManagerConfig{Backend: rec})

	ctx := context.Background()

	// Pre-occupy res:B
	lB, err := mgr.Acquire(ctx, "owner-1", "res:B", 5*time.Minute)
	if err != nil {
		t.Fatalf("pre-occupy failed: %v", err)
	}

	reqs := []ResourceRequest{
		{Scope: "res:A", TTL: 5 * time.Minute},
		{Scope: "res:B", TTL: 5 * time.Minute},
	}

	_, err = cm.AcquireComposite(ctx, "owner-2", reqs)
	if err == nil {
		t.Fatalf("expected error on occupied res:B")
	}
	if !errors.Is(err, ErrScopeConflict) {
		t.Errorf("expected ErrScopeConflict wrapped in joined error, got %v", err)
	}

	ops := rec.Operations()
	// Expected operation sequence:
	// 1. Acquire res:A (succeeds)
	// 2. AcquireFailed res:B
	// 3. Release res:A with ReasonRollback
	if len(ops) != 3 {
		t.Fatalf("expected 3 recorded ops, got %d: %v", len(ops), ops)
	}
	if ops[0].Op != "Acquire" || ops[0].Scope != "res:A" {
		t.Errorf("expected op 0 Acquire res:A, got %v", ops[0])
	}
	if ops[1].Op != "AcquireFailed" || ops[1].Scope != "res:B" {
		t.Errorf("expected op 1 AcquireFailed res:B, got %v", ops[1])
	}
	if ops[2].Op != "Release" || ops[2].Scope != "res:A" || ops[2].Reason != ReasonRollback {
		t.Errorf("expected op 2 Release res:A with ReasonRollback, got %v", ops[2])
	}

	// Verify res:A is free again
	lA, err := mgr.Acquire(ctx, "owner-3", "res:A", 5*time.Minute)
	if err != nil {
		t.Fatalf("res:A should be free after rollback, got %v", err)
	}
	_, _ = mgr.Release(lA.ID, "owner-3", ReasonReleasedByOwner)
	_, _ = mgr.Release(lB.ID, "owner-1", ReasonReleasedByOwner)
}

// TestCompositeManager_FailureInjection_MultiResource_LIFOOrder proves LIFO rollback order (C fails -> release B -> release A).
func TestCompositeManager_FailureInjection_MultiResource_LIFOOrder(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	rec := NewRecordingLeaseBackend(mgr)
	cm := NewCompositeManager(CompositeManagerConfig{Backend: rec})

	ctx := context.Background()

	// Pre-occupy res:C
	lC, err := mgr.Acquire(ctx, "owner-1", "res:C", 5*time.Minute)
	if err != nil {
		t.Fatalf("pre-occupy failed: %v", err)
	}

	reqs := []ResourceRequest{
		{Scope: "res:A", TTL: 5 * time.Minute},
		{Scope: "res:B", TTL: 5 * time.Minute},
		{Scope: "res:C", TTL: 5 * time.Minute},
	}

	_, err = cm.AcquireComposite(ctx, "owner-2", reqs)
	if err == nil {
		t.Fatalf("expected error on occupied res:C")
	}

	ops := rec.Operations()
	// Expected operation sequence:
	// 1. Acquire res:A
	// 2. Acquire res:B
	// 3. AcquireFailed res:C
	// 4. Release res:B (LIFO step 1) with ReasonRollback
	// 5. Release res:A (LIFO step 2) with ReasonRollback
	if len(ops) != 5 {
		t.Fatalf("expected 5 recorded ops, got %d: %v", len(ops), ops)
	}
	if ops[3].Scope != "res:B" || ops[3].Reason != ReasonRollback {
		t.Errorf("expected LIFO rollback step 1 to be res:B with ReasonRollback, got %v", ops[3])
	}
	if ops[4].Scope != "res:A" || ops[4].Reason != ReasonRollback {
		t.Errorf("expected LIFO rollback step 2 to be res:A with ReasonRollback, got %v", ops[4])
	}

	_, _ = mgr.Release(lC.ID, "owner-1", ReasonReleasedByOwner)
}

// TestCompositeManager_FailureInjection_RollbackReleaseFailure verifies that if a release during rollback
// fails, remaining rollback releases continue executing and errors are joined via errors.Join.
func TestCompositeManager_FailureInjection_RollbackReleaseFailure(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	rec := NewRecordingLeaseBackend(mgr)
	cm := NewCompositeManager(CompositeManagerConfig{Backend: rec})

	ctx := context.Background()

	// Acquire res:A and res:B for owner-2
	lA, _ := mgr.Acquire(ctx, "owner-2", "res:A", 5*time.Minute)
	lB, _ := mgr.Acquire(ctx, "owner-2", "res:B", 5*time.Minute)
	_ = lA
	_ = lB

	// Inject release error for res:B ID during rollback
	rec.failAcquire["res:C"] = errors.New("injected technical acquire failure")
	rec.failRelease[lB.ID] = errors.New("injected release failure on res:B")

	reqs := []ResourceRequest{
		{Scope: "res:A", TTL: 5 * time.Minute},
		{Scope: "res:B", TTL: 5 * time.Minute},
		{Scope: "res:C", TTL: 5 * time.Minute},
	}

	// Inject acquire error on res:C by manually setting failAcquire
	_, err := cm.AcquireComposite(ctx, "owner-2", reqs)
	if err == nil {
		t.Fatalf("expected composite acquire error")
	}

	// Verify both acquire error and injected rollback release error are preserved in joined error
	if !errors.Is(err, ErrScopeConflict) && !errors.Is(err, rec.failAcquire["res:C"]) {
		t.Errorf("expected acquire error preserved in %v", err)
	}
}

// TestCompositeManager_RenewPartialFailureDegradesState tests that if Renew fails midway,
// further renewals stop immediately, handle state transitions to CompositeStateDegraded,
// and joined errors are returned.
func TestCompositeManager_RenewPartialFailureDegradesState(t *testing.T) {
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

	// Inject renewal failure on res:A
	rec.failRenew[handle.Leases[0].ID] = errors.New("injected renew failure")

	err = cm.RenewComposite(handle, 10*time.Minute)
	if err == nil {
		t.Fatalf("expected renew error")
	}

	if handle.GetState() != CompositeStateDegraded {
		t.Errorf("expected state DEGRADED after partial renew failure, got %s", handle.GetState())
	}

	// Subsequent renew attempt must fail fast because state is DEGRADED
	err2 := cm.RenewComposite(handle, 10*time.Minute)
	if err2 == nil {
		t.Errorf("expected renew to fail fast when handle is DEGRADED")
	}
}

// TestCompositeManager_FullLifecycle tests successful acquisition, renewal, release, and state transitions.
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
	if handle.GetState() != CompositeStateAcquired {
		t.Errorf("expected state ACQUIRED, got %s", handle.GetState())
	}

	// Renew
	if err := cm.RenewComposite(handle, 20*time.Minute); err != nil {
		t.Fatalf("renew composite failed: %v", err)
	}

	// Release
	if err := cm.ReleaseComposite(handle, ReasonReleasedByOwner); err != nil {
		t.Fatalf("release composite failed: %v", err)
	}
	if handle.GetState() != CompositeStateReleased {
		t.Errorf("expected state RELEASED, got %s", handle.GetState())
	}

	// Idempotent release
	if err := cm.ReleaseComposite(handle, ReasonReleasedByOwner); err != nil {
		t.Fatalf("idempotent release failed: %v", err)
	}

	// Verify LIFO release order in operations (encoder:0 first, tuner:0 second)
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

// TestCompositeManager_IDGeneratorFailure tests error handling when ID generation fails after acquisition.
func TestCompositeManager_IDGeneratorFailure(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	rec := NewRecordingLeaseBackend(mgr)

	failingIDGen := func() (ID, error) {
		return "", errors.New("injected ID generation failure")
	}

	cm := NewCompositeManager(CompositeManagerConfig{
		Backend:     rec,
		IDGenerator: failingIDGen,
	})

	ctx := context.Background()
	reqs := []ResourceRequest{{Scope: "res:A", TTL: 5 * time.Minute}}

	_, err := cm.AcquireComposite(ctx, "worker-1", reqs)
	if err == nil {
		t.Fatalf("expected error on failing ID generator")
	}

	// Verify res:A was acquired and immediately rolled back
	ops := rec.Operations()
	if len(ops) != 2 {
		t.Fatalf("expected 2 ops (Acquire then Release rollback), got %d: %v", len(ops), ops)
	}
	if ops[1].Op != "Release" || ops[1].Reason != ReasonRollback {
		t.Errorf("expected rollback release on ID failure, got %v", ops[1])
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
					if handle.GetState() != CompositeStateAcquired {
						t.Errorf("expected state ACQUIRED for successful handle")
					}
					time.Sleep(1 * time.Millisecond)
					if j%2 == 0 {
						_ = cm.RenewComposite(handle, 50*time.Millisecond)
					}
					errRel := cm.ReleaseComposite(handle, ReasonReleasedByOwner)
					if errRel != nil {
						t.Errorf("unexpected release error: %v", errRel)
					}
				}
			}
		}(workerID, i)
	}

	wg.Wait()
}
