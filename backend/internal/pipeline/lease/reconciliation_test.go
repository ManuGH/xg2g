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

// TestIntentStore_Operations tests SaveIntent, GetIntent, ListIntents, and DeleteIntent.
func TestIntentStore_Operations(t *testing.T) {
	store := NewInMemoryIntentStore()
	ctx := context.Background()

	intentA := LeaseIntent{
		ID:        "intent-1",
		Owner:     "worker-1",
		Scope:     "res:A",
		State:     StateAcquired,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// Save
	if err := store.SaveIntent(ctx, intentA); err != nil {
		t.Fatalf("SaveIntent failed: %v", err)
	}

	// Get
	got, err := store.GetIntent(ctx, "intent-1")
	if err != nil {
		t.Fatalf("GetIntent failed: %v", err)
	}
	if got.Owner != "worker-1" || got.Scope != "res:A" {
		t.Errorf("GetIntent returned incorrect data: %v", got)
	}

	// List
	list, err := store.ListIntents(ctx)
	if err != nil {
		t.Fatalf("ListIntents failed: %v", err)
	}
	if len(list) != 1 || list[0].ID != "intent-1" {
		t.Errorf("ListIntents returned incorrect slice: %v", list)
	}

	// Delete
	if err := store.DeleteIntent(ctx, "intent-1"); err != nil {
		t.Fatalf("DeleteIntent failed: %v", err)
	}
	_, err = store.GetIntent(ctx, "intent-1")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound after DeleteIntent, got %v", err)
	}
}

// TestReconciler_ConfirmedStatus verifies that matching intent and backend lease produce ReconciliationStatusConfirmed.
func TestReconciler_ConfirmedStatus(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	store := NewInMemoryIntentStore()
	ctx := context.Background()

	l, err := mgr.Acquire(ctx, "worker-1", "res:A", 10*time.Minute)
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	intent := LeaseIntent{
		ID:        l.ID,
		Owner:     "worker-1",
		Scope:     "res:A",
		State:     StateAcquired,
		CreatedAt: l.AcquiredAt,
		UpdatedAt: l.AcquiredAt,
	}
	_ = store.SaveIntent(ctx, intent)

	reconciler, err := NewReconciler(ReconcilerConfig{
		IntentStore: store,
		Backend:     mgr,
	})
	if err != nil {
		t.Fatalf("NewReconciler failed: %v", err)
	}

	report, err := reconciler.Reconcile(ctx)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	if report.Summary.Confirmed != 1 || len(report.Items) != 1 {
		t.Fatalf("expected 1 confirmed item, got summary: %+v", report.Summary)
	}
	if report.Items[0].Status != ReconciliationStatusConfirmed {
		t.Errorf("expected status %s, got %s", ReconciliationStatusConfirmed, report.Items[0].Status)
	}
}

// TestReconciler_ReleasedStatus verifies that when both intent and backend agree on released/expired state,
// status is ReconciliationStatusReleased.
func TestReconciler_ReleasedStatus(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	store := NewInMemoryIntentStore()
	ctx := context.Background()

	l, _ := mgr.Acquire(ctx, "worker-1", "res:A", 10*time.Minute)
	_, _ = mgr.Release(ctx, l.ID, "worker-1", ReasonReleasedByOwner)

	intent := LeaseIntent{
		ID:        l.ID,
		Owner:     "worker-1",
		Scope:     "res:A",
		State:     StateReleased,
		CreatedAt: l.AcquiredAt,
		UpdatedAt: time.Now(),
	}
	_ = store.SaveIntent(ctx, intent)

	reconciler, _ := NewReconciler(ReconcilerConfig{
		IntentStore: store,
		Backend:     mgr,
	})

	report, err := reconciler.Reconcile(ctx)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	if report.Summary.Released != 1 {
		t.Fatalf("expected 1 released item, got summary: %+v", report.Summary)
	}
	if report.Items[0].Status != ReconciliationStatusReleased {
		t.Errorf("expected status %s, got %s", ReconciliationStatusReleased, report.Items[0].Status)
	}
}

// TestReconciler_OrphanedStatus verifies that active backend leases without intents produce ReconciliationStatusOrphaned.
// Also tests auto-remediation when AutoRemediateOrphans = true.
func TestReconciler_OrphanedStatus(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	store := NewInMemoryIntentStore()
	ctx := context.Background()

	// Acquire lease directly in backend without intent
	lOrphan, err := mgr.Acquire(ctx, "worker-orphan", "res:ORPHAN", 10*time.Minute)
	if err != nil {
		t.Fatalf("acquire orphan failed: %v", err)
	}

	// Audit mode (AutoRemediateOrphans = false)
	reconcilerAudit, _ := NewReconciler(ReconcilerConfig{
		IntentStore:          store,
		Backend:              mgr,
		AutoRemediateOrphans: false,
	})

	reportAudit, err := reconcilerAudit.Reconcile(ctx)
	if err != nil {
		t.Fatalf("Reconcile audit failed: %v", err)
	}
	if reportAudit.Summary.Orphaned != 1 || reportAudit.Summary.RemediatedOrphans != 0 {
		t.Fatalf("expected 1 orphaned item, 0 remediated, got %+v", reportAudit.Summary)
	}
	if reportAudit.Items[0].Status != ReconciliationStatusOrphaned {
		t.Errorf("expected status %s, got %s", ReconciliationStatusOrphaned, reportAudit.Items[0].Status)
	}

	// Auto-remediation mode (AutoRemediateOrphans = true)
	reconcilerAuto, _ := NewReconciler(ReconcilerConfig{
		IntentStore:          store,
		Backend:              mgr,
		AutoRemediateOrphans: true,
	})

	reportAuto, err := reconcilerAuto.Reconcile(ctx)
	if err != nil {
		t.Fatalf("Reconcile auto failed: %v", err)
	}
	if reportAuto.Summary.Orphaned != 1 || reportAuto.Summary.RemediatedOrphans != 1 {
		t.Fatalf("expected 1 orphaned item, 1 remediated, got %+v", reportAuto.Summary)
	}
	if !reportAuto.Items[0].Remediated {
		t.Errorf("expected item to be remediated")
	}

	// Verify lease is now released in backend
	lCheck, _ := mgr.Get(lOrphan.ID)
	if lCheck != nil && lCheck.State != StateReleased {
		t.Errorf("expected orphaned lease to be RELEASED in backend, got %s", lCheck.State)
	}
}

// TestReconciler_MissingStatus verifies that active intents missing in backend produce ReconciliationStatusMissing.
func TestReconciler_MissingStatus(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	store := NewInMemoryIntentStore()
	ctx := context.Background()

	// Save active intent for res:A, but backend has no lease for res:A
	intent := LeaseIntent{
		ID:        "intent-missing-1",
		Owner:     "worker-1",
		Scope:     "res:A",
		State:     StateAcquired,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	_ = store.SaveIntent(ctx, intent)

	reconciler, _ := NewReconciler(ReconcilerConfig{
		IntentStore: store,
		Backend:     mgr,
	})

	report, err := reconciler.Reconcile(ctx)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	if report.Summary.Missing != 1 {
		t.Fatalf("expected 1 missing item, got %+v", report.Summary)
	}
	if report.Items[0].Status != ReconciliationStatusMissing {
		t.Errorf("expected status %s, got %s", ReconciliationStatusMissing, report.Items[0].Status)
	}
}

// TestReconciler_ManualInterventionRequiredStatus verifies that owner or ID mismatches between active intent
// and backend lease produce ReconciliationStatusManualInterventionRequired.
func TestReconciler_ManualInterventionRequiredStatus(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	store := NewInMemoryIntentStore()
	ctx := context.Background()

	// Backend holds res:A for owner-backend
	lBackend, err := mgr.Acquire(ctx, "owner-backend", "res:A", 10*time.Minute)
	if err != nil {
		t.Fatalf("acquire backend failed: %v", err)
	}

	// Intent expects res:A for owner-intent with different ID
	intent := LeaseIntent{
		ID:        "intent-different-id",
		Owner:     "owner-intent",
		Scope:     "res:A",
		State:     StateAcquired,
		CreatedAt: lBackend.AcquiredAt,
		UpdatedAt: lBackend.AcquiredAt,
	}
	_ = store.SaveIntent(ctx, intent)

	reconciler, _ := NewReconciler(ReconcilerConfig{
		IntentStore: store,
		Backend:     mgr,
	})

	report, err := reconciler.Reconcile(ctx)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	if report.Summary.ManualInterventionRequired != 1 {
		t.Fatalf("expected 1 manual-intervention-required item, got %+v", report.Summary)
	}
	if report.Items[0].Status != ReconciliationStatusManualInterventionRequired {
		t.Errorf("expected status %s, got %s", ReconciliationStatusManualInterventionRequired, report.Items[0].Status)
	}
}

// TestReconciler_RaceConditions tests concurrent intent mutations and reconciliation executions under -race.
func TestReconciler_RaceConditions(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 10 * time.Millisecond})
	defer mgr.Close()
	store := NewInMemoryIntentStore()
	reconciler, _ := NewReconciler(ReconcilerConfig{
		IntentStore:          store,
		Backend:              mgr,
		AutoRemediateOrphans: true,
	})

	ctx := context.Background()
	const numWorkers = 15
	const opsPerWorker = 30
	var wg sync.WaitGroup

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		wOwner := Owner(fmt.Sprintf("recon-worker-%d", i))
		wScope := Scope(fmt.Sprintf("res:race:%d", i%5))

		go func(owner Owner, scope Scope, idx int) {
			defer wg.Done()
			for j := 0; j < opsPerWorker; j++ {
				intentID := ID(fmt.Sprintf("intent-%s-%d", owner, j))

				_ = store.SaveIntent(ctx, LeaseIntent{
					ID:        intentID,
					Owner:     owner,
					Scope:     scope,
					State:     StateAcquired,
					CreatedAt: time.Now(),
					UpdatedAt: time.Now(),
				})

				l, err := mgr.Acquire(ctx, owner, scope, 50*time.Millisecond)
				if err == nil {
					time.Sleep(1 * time.Millisecond)
					_, _ = mgr.Release(ctx, l.ID, owner, ReasonReleasedByOwner)
				}

				if idx%2 == 0 {
					_, _ = reconciler.Reconcile(ctx)
				}

				_ = store.DeleteIntent(ctx, intentID)
			}
		}(wOwner, wScope, i)
	}

	wg.Wait()
}
