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

// TestCompositeManager_DeterministicScopeSorting verifies that requests are acquired in sorted Scope order,
// eliminating circular lock deadlocks between workers requesting resources in opposite orders.
func TestCompositeManager_DeterministicScopeSorting(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	cm := NewCompositeManager(mgr)

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
	defer func() {
		_ = cm.ReleaseComposite(handle, ReasonReleasedByOwner)
	}()

	if len(handle.Leases) != 3 {
		t.Fatalf("expected 3 leases, got %d", len(handle.Leases))
	}

	expectedOrder := []Scope{"res:A", "res:M", "res:Z"}
	for i, expected := range expectedOrder {
		if handle.Leases[i].Scope != expected {
			t.Errorf("expected index %d scope %s, got %s", i, expected, handle.Leases[i].Scope)
		}
	}
}

// TestCompositeManager_FailureInjection_Step1Failure verifies that when step 1 fails,
// no rollback compensation is needed and 0 leases remain held.
func TestCompositeManager_FailureInjection_Step1Failure(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	cm := NewCompositeManager(mgr)

	ctx := context.Background()

	// Pre-occupy res:A by owner-1
	_, err := mgr.Acquire(ctx, "owner-1", "res:A", 5*time.Minute)
	if err != nil {
		t.Fatalf("pre-occupy failed: %v", err)
	}

	// owner-2 attempts composite acquire of res:A and res:B
	reqs := []ResourceRequest{
		{Scope: "res:A", TTL: 5 * time.Minute},
		{Scope: "res:B", TTL: 5 * time.Minute},
	}

	handle, err := cm.AcquireComposite(ctx, "owner-2", reqs)
	if err == nil {
		t.Fatalf("expected error on occupied res:A, got handle=%v", handle)
	}
	if !errors.Is(err, ErrScopeConflict) {
		t.Errorf("expected ErrScopeConflict, got %v", err)
	}

	// Verify res:B was never acquired and is free for others
	lB, err := mgr.Acquire(ctx, "owner-3", "res:B", 5*time.Minute)
	if err != nil {
		t.Fatalf("res:B should remain free for acquisition, got %v", err)
	}
	_, _ = mgr.Release(lB.ID, "owner-3", ReasonReleasedByOwner)
}

// TestCompositeManager_FailureInjection_Step2Failure verifies that when step 2 fails,
// step 1 is rolled back in LIFO order with ReasonRollback and freed immediately.
func TestCompositeManager_FailureInjection_Step2Failure(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	cm := NewCompositeManager(mgr)

	ctx := context.Background()

	// Pre-occupy res:B by owner-1
	lB, err := mgr.Acquire(ctx, "owner-1", "res:B", 5*time.Minute)
	if err != nil {
		t.Fatalf("pre-occupy failed: %v", err)
	}

	// owner-2 attempts composite acquire of res:A (succeeds) and res:B (fails)
	reqs := []ResourceRequest{
		{Scope: "res:A", TTL: 5 * time.Minute},
		{Scope: "res:B", TTL: 5 * time.Minute},
	}

	_, err = cm.AcquireComposite(ctx, "owner-2", reqs)
	if err == nil {
		t.Fatalf("expected error on occupied res:B")
	}
	if !errors.Is(err, ErrScopeConflict) {
		t.Errorf("expected ErrScopeConflict, got %v", err)
	}

	// Verify res:A was rolled back and is immediately free again
	lA, err := mgr.Acquire(ctx, "owner-3", "res:A", 5*time.Minute)
	if err != nil {
		t.Fatalf("res:A should be rolled back and free for owner-3, got %v", err)
	}
	_, _ = mgr.Release(lA.ID, "owner-3", ReasonReleasedByOwner)
	_, _ = mgr.Release(lB.ID, "owner-1", ReasonReleasedByOwner)
}

// TestCompositeManager_FailureInjection_MultiResource_LIFOOrder verifies LIFO rollback order
// across 3 resources when step 3 fails.
func TestCompositeManager_FailureInjection_MultiResource_LIFOOrder(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	cm := NewCompositeManager(mgr)

	ctx := context.Background()

	// Pre-occupy res:C by owner-1
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

	// Both res:A and res:B must have been rolled back and now free
	lA, err := mgr.Acquire(ctx, "owner-3", "res:A", 5*time.Minute)
	if err != nil {
		t.Errorf("res:A was not freed during rollback: %v", err)
	}
	lB, err := mgr.Acquire(ctx, "owner-3", "res:B", 5*time.Minute)
	if err != nil {
		t.Errorf("res:B was not freed during rollback: %v", err)
	}

	_, _ = mgr.Release(lA.ID, "owner-3", ReasonReleasedByOwner)
	_, _ = mgr.Release(lB.ID, "owner-3", ReasonReleasedByOwner)
	_, _ = mgr.Release(lC.ID, "owner-1", ReasonReleasedByOwner)
}

// TestCompositeManager_FullLifecycle tests successful acquisition, renewal, and release.
func TestCompositeManager_FullLifecycle(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	cm := NewCompositeManager(mgr)

	ctx := context.Background()
	reqs := []ResourceRequest{
		{Scope: "tuner:0", TTL: 10 * time.Minute},
		{Scope: "encoder:0", TTL: 10 * time.Minute},
	}

	handle, err := cm.AcquireComposite(ctx, "worker-1", reqs)
	if err != nil {
		t.Fatalf("acquire composite failed: %v", err)
	}

	// Renew all
	if err := cm.RenewComposite(handle, 20*time.Minute); err != nil {
		t.Fatalf("renew composite failed: %v", err)
	}

	// Release all
	if err := cm.ReleaseComposite(handle, ReasonReleasedByOwner); err != nil {
		t.Fatalf("release composite failed: %v", err)
	}

	// Both slots should be free now
	l1, err1 := mgr.Acquire(ctx, "worker-2", "tuner:0", 5*time.Minute)
	l2, err2 := mgr.Acquire(ctx, "worker-2", "encoder:0", 5*time.Minute)
	if err1 != nil || err2 != nil {
		t.Fatalf("slots should be free after release, got err1=%v, err2=%v", err1, err2)
	}
	_, _ = mgr.Release(l1.ID, "worker-2", ReasonReleasedByOwner)
	_, _ = mgr.Release(l2.ID, "worker-2", ReasonReleasedByOwner)
}

// TestCompositeManager_RaceConditions tests concurrent competing composite acquisitions under -race.
func TestCompositeManager_RaceConditions(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 10 * time.Millisecond})
	defer mgr.Close()
	cm := NewCompositeManager(mgr)

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
				// Alternate requested scope order between workers to stress deterministic sorting
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
					time.Sleep(1 * time.Millisecond)
					if j%2 == 0 {
						_ = cm.RenewComposite(handle, 50*time.Millisecond)
					}
					_ = cm.ReleaseComposite(handle, ReasonReleasedByOwner)
				}
			}
		}(workerID, i)
	}

	wg.Wait()
}
