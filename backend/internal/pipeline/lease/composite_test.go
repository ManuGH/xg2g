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
	mu               sync.Mutex
	backend          LeaseBackend
	ops              []OperationRecord
	failAcquireScope map[Scope]error
	failRenewScope   map[Scope]error
	failReleaseScope map[Scope]error
	failReleaseID    map[ID]error
}

func NewRecordingLeaseBackend(realBackend LeaseBackend) *RecordingLeaseBackend {
	return &RecordingLeaseBackend{
		backend:          realBackend,
		failAcquireScope: make(map[Scope]error),
		failRenewScope:   make(map[Scope]error),
		failReleaseScope: make(map[Scope]error),
		failReleaseID:    make(map[ID]error),
	}
}

func (r *RecordingLeaseBackend) Acquire(ctx context.Context, owner Owner, scope Scope, ttl time.Duration) (*Lease, error) {
	r.mu.Lock()
	if err, fail := r.failAcquireScope[scope]; fail {
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

func (r *RecordingLeaseBackend) Renew(ctx context.Context, id ID, owner Owner, ttl time.Duration) (*Lease, error) {
	r.mu.Lock()
	lTemp, errFind := r.backend.Renew(ctx, id, owner, ttl)
	var sc Scope
	if lTemp != nil {
		sc = lTemp.Scope
	}
	if err, fail := r.failRenewScope[sc]; fail {
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
	if err, fail := r.failReleaseID[id]; fail {
		r.ops = append(r.ops, OperationRecord{Op: "ReleaseFailed", Owner: owner, ID: id, Reason: reason})
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

// TestCompositeManager_FailureInjection_RollbackReleaseFailure directly tests technical acquire failure
// followed by an injected release failure during LIFO rollback.
func TestCompositeManager_FailureInjection_RollbackReleaseFailure(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	rec := NewRecordingLeaseBackend(mgr)
	cm := NewCompositeManager(CompositeManagerConfig{Backend: rec})

	ctx := context.Background()

	acqErrInj := errors.New("injected technical acquire failure on res:C")
	relErrInj := errors.New("injected release failure on res:B during rollback")

	rec.failAcquireScope["res:C"] = acqErrInj
	rec.failReleaseScope["res:B"] = relErrInj

	reqs := []ResourceRequest{
		{Scope: "res:A", TTL: 5 * time.Minute},
		{Scope: "res:B", TTL: 5 * time.Minute},
		{Scope: "res:C", TTL: 5 * time.Minute},
	}

	_, err := cm.AcquireComposite(ctx, "owner-1", reqs)
	if err == nil {
		t.Fatalf("expected composite acquire error")
	}

	// Verify structured error type
	var compErr *CompositeAcquireError
	if !errors.As(err, &compErr) {
		t.Fatalf("expected error to be *CompositeAcquireError, got %T: %v", err, err)
	}

	if compErr.FailedScope != "res:C" {
		t.Errorf("expected FailedScope res:C, got %s", compErr.FailedScope)
	}
	if !errors.Is(err, acqErrInj) {
		t.Errorf("expected errors.Is to match primary acquire error")
	}
	if !errors.Is(err, relErrInj) {
		t.Errorf("expected errors.Is to match injected rollback release error")
	}

	if len(compErr.Compensated) != 1 || compErr.Compensated[0] != "res:A" {
		t.Errorf("expected compensated scope [res:A], got %v", compErr.Compensated)
	}
	if len(compErr.Failures) != 1 || compErr.Failures[0].Scope != "res:B" {
		t.Errorf("expected compensation failure on res:B, got %v", compErr.Failures)
	}

	// Verify operation sequence is exact LIFO:
	// 0: Acquire res:A (success)
	// 1: Acquire res:B (success)
	// 2: AcquireFailed res:C
	// 3: ReleaseFailed res:B (LIFO step 1)
	// 4: Release res:A (LIFO step 2, success)
	ops := rec.Operations()
	if len(ops) != 5 {
		t.Fatalf("expected 5 recorded ops, got %d: %v", len(ops), ops)
	}
	if ops[3].Op != "ReleaseFailed" || ops[3].Scope != "res:B" {
		t.Errorf("expected LIFO step 1 ReleaseFailed res:B, got %v", ops[3])
	}
	if ops[4].Op != "Release" || ops[4].Scope != "res:A" {
		t.Errorf("expected LIFO step 2 Release res:A, got %v", ops[4])
	}

	// Verify res:A is free for another owner
	lA, err := mgr.Acquire(ctx, "owner-2", "res:A", 5*time.Minute)
	if err != nil {
		t.Fatalf("res:A should be free after rollback, got %v", err)
	}
	_, _ = mgr.Release(ctx, lA.ID, "owner-2", ReasonReleasedByOwner)
}

// TestCompositeManager_FailureInjection_MultiRollbackReleaseFailure tests multiple release failures during rollback.
func TestCompositeManager_FailureInjection_MultiRollbackReleaseFailure(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	rec := NewRecordingLeaseBackend(mgr)
	cm := NewCompositeManager(CompositeManagerConfig{Backend: rec})

	ctx := context.Background()

	rec.failAcquireScope["res:C"] = errors.New("acquire failed on C")
	rec.failReleaseScope["res:A"] = errors.New("release failed on A")
	rec.failReleaseScope["res:B"] = errors.New("release failed on B")

	reqs := []ResourceRequest{
		{Scope: "res:A", TTL: 5 * time.Minute},
		{Scope: "res:B", TTL: 5 * time.Minute},
		{Scope: "res:C", TTL: 5 * time.Minute},
	}

	_, err := cm.AcquireComposite(ctx, "owner-1", reqs)
	if err == nil {
		t.Fatalf("expected composite acquire error")
	}

	var compErr *CompositeAcquireError
	if !errors.As(err, &compErr) {
		t.Fatalf("expected *CompositeAcquireError, got %v", err)
	}
	if len(compErr.Failures) != 2 {
		t.Errorf("expected 2 compensation failures, got %d", len(compErr.Failures))
	}
}

// TestCompositeManager_RenewPartialFailureDegradesState verifies fail-fast renewal and DEGRADED state.
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

	// Inject renew failure on res:A
	rec.failRenewScope["res:A"] = errors.New("injected renew failure on res:A")

	err = cm.RenewComposite(ctx, handle, 10*time.Minute)
	if err == nil {
		t.Fatalf("expected renew error")
	}

	if handle.State() != CompositeStateDegraded {
		t.Errorf("expected state DEGRADED after partial renew failure, got %s", handle.State())
	}

	// Subsequent renew attempt fails fast because handle is DEGRADED
	err2 := cm.RenewComposite(ctx, handle, 10*time.Minute)
	if err2 == nil {
		t.Errorf("expected renew to fail fast when handle is DEGRADED")
	}
}

// TestCompositeManager_ReleaseDegradedHandleRetriesOnlyUnreleased tests retrying ReleaseComposite on a DEGRADED handle.
func TestCompositeManager_ReleaseDegradedHandleRetriesOnlyUnreleased(t *testing.T) {
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
		t.Fatalf("acquire failed: %v", err)
	}

	// Inject release failure on res:A (LIFO step 2, res:B released first)
	rec.failReleaseScope["res:A"] = errors.New("temporary release failure on A")

	err = cm.ReleaseComposite(ctx, handle, ReasonReleasedByOwner)
	if err == nil {
		t.Fatalf("expected release error on DEGRADED handle")
	}
	if handle.State() != CompositeStateDegraded {
		t.Fatalf("expected state DEGRADED, got %s", handle.State())
	}

	// Remove injected failure on res:A and retry ReleaseComposite
	rec.mu.Lock()
	delete(rec.failReleaseScope, "res:A")
	rec.mu.Unlock()

	errRetry := cm.ReleaseComposite(ctx, handle, ReasonReleasedByOwner)
	if errRetry != nil {
		t.Fatalf("retry release failed: %v", errRetry)
	}
	if handle.State() != CompositeStateReleased {
		t.Fatalf("expected state RELEASED after retry, got %s", handle.State())
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
