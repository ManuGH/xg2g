// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package lease

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestFileIntentStore_RestartPersistenceRecovery verifies that LeaseIntents persisted to a FileIntentStore
// survive closing and reopening in a new FileIntentStore instance, proving restart recovery!
func TestFileIntentStore_RestartPersistenceRecovery(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "intents.json")
	ctx := context.Background()

	// 1. First instance: Save intent
	store1, err := NewFileIntentStore(storePath)
	if err != nil {
		t.Fatalf("failed to create store1: %v", err)
	}

	intent1 := LeaseIntent{
		IntentID:    "intent-persist-1",
		LeaseID:     "lse_backend_100",
		Owner:       "worker-persistent",
		Scope:       "res:PERSISTENT",
		CompositeID: "comp-123",
		State:       IntentStateActive,
		Revision:    1,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if err := store1.SaveIntent(ctx, intent1); err != nil {
		t.Fatalf("SaveIntent failed: %v", err)
	}

	// 2. Simulate process restart: Re-open with store2 on same file
	store2, err := NewFileIntentStore(storePath)
	if err != nil {
		t.Fatalf("failed to re-open store2: %v", err)
	}

	got, err := store2.GetIntent(ctx, "intent-persist-1")
	if err != nil {
		t.Fatalf("GetIntent on reopened store failed: %v", err)
	}

	if got.Owner != "worker-persistent" || got.LeaseID != "lse_backend_100" || got.Scope != "res:PERSISTENT" {
		t.Errorf("reopened store returned invalid data: %+v", got)
	}

	// 3. Reconcile with reopened store
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()

	// Re-acquire exact lease in backend
	lBackend, err := mgr.Acquire(ctx, "worker-persistent", "res:PERSISTENT", 10*time.Minute)
	if err != nil {
		t.Fatalf("backend acquire failed: %v", err)
	}

	// Update intent with actual backend lease ID
	intent1.LeaseID = lBackend.ID
	intent1.Revision = 2
	_ = store2.SaveIntent(ctx, intent1)

	reconciler, err := NewReconciler(ReconcilerConfig{
		IntentStore: store2,
		Backend:     mgr,
	})
	if err != nil {
		t.Fatalf("NewReconciler failed: %v", err)
	}

	report, err := reconciler.Reconcile(ctx)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	if report.Summary.Confirmed != 1 {
		t.Errorf("expected 1 confirmed lease after restart recovery, got summary: %+v", report.Summary)
	}
}

// TestReconciler_DuplicateIntents verifies that multiple active intents for the same scope
// produce ReconciliationStatusManualInterventionRequired with ReasonReconciliationDuplicateIntents.
func TestReconciler_DuplicateIntents(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	store := NewInMemoryIntentStore()
	ctx := context.Background()

	_ = store.SaveIntent(ctx, LeaseIntent{
		IntentID: "intent-dup-1",
		LeaseID:  "lse-1",
		Owner:    "worker-1",
		Scope:    "res:DUPLICATE",
		State:    IntentStateActive,
	})
	_ = store.SaveIntent(ctx, LeaseIntent{
		IntentID: "intent-dup-2",
		LeaseID:  "lse-2",
		Owner:    "worker-2",
		Scope:    "res:DUPLICATE",
		State:    IntentStateActive,
	})

	reconciler, _ := NewReconciler(ReconcilerConfig{
		IntentStore: store,
		Backend:     mgr,
	})

	report, err := reconciler.Reconcile(ctx)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	if report.Summary.ManualInterventionRequired != 1 {
		t.Fatalf("expected 1 manual intervention item, got %+v", report.Summary)
	}
	if report.Items[0].ReasonCode != ReasonReconciliationDuplicateIntents {
		t.Errorf("expected reason %s, got %s", ReasonReconciliationDuplicateIntents, report.Items[0].ReasonCode)
	}
}

// TestReconciler_DuplicateBackendLeases verifies that multiple active backend leases for the same scope
// produce ReconciliationStatusManualInterventionRequired with ReasonReconciliationDuplicateBackendLeases.
func TestReconciler_DuplicateBackendLeases(t *testing.T) {
	// Custom backend returning 2 active leases for same scope
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	store := NewInMemoryIntentStore()
	ctx := context.Background()

	l1, _ := mgr.Acquire(ctx, "worker-1", "res:DUP_BACKEND", 10*time.Minute)

	// Create fake lease directly in manager leases map
	l2 := &Lease{
		ID:         "lse-dup-backend-2",
		Owner:      "worker-2",
		Scope:      "res:DUP_BACKEND",
		State:      StateAcquired,
		AcquiredAt: time.Now(),
		ExpiresAt:  time.Now().Add(10 * time.Minute),
	}

	_ = store.SaveIntent(ctx, LeaseIntent{
		IntentID: "intent-1",
		LeaseID:  l1.ID,
		Owner:    "worker-1",
		Scope:    "res:DUP_BACKEND",
		State:    IntentStateActive,
	})

	// Wrap mgr to return both l1 and l2
	obsBackend := &fakeDuplicateBackend{mgr: mgr, extraLease: l2}

	reconciler, _ := NewReconciler(ReconcilerConfig{
		IntentStore: store,
		Backend:     obsBackend,
	})

	report, err := reconciler.Reconcile(ctx)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	if report.Summary.ManualInterventionRequired != 1 {
		t.Fatalf("expected 1 manual intervention item for duplicate backend leases, got %+v", report.Summary)
	}
	if report.Items[0].ReasonCode != ReasonReconciliationDuplicateBackendLeases {
		t.Errorf("expected reason %s, got %s", ReasonReconciliationDuplicateBackendLeases, report.Items[0].ReasonCode)
	}
}

type fakeDuplicateBackend struct {
	mgr        *Manager
	extraLease *Lease
}

func (f *fakeDuplicateBackend) Acquire(ctx context.Context, owner Owner, scope Scope, ttl time.Duration) (*Lease, error) {
	return f.mgr.Acquire(ctx, owner, scope, ttl)
}

func (f *fakeDuplicateBackend) Renew(ctx context.Context, id ID, owner Owner, ttl time.Duration) (*Lease, error) {
	return f.mgr.Renew(ctx, id, owner, ttl)
}

func (f *fakeDuplicateBackend) Release(ctx context.Context, id ID, owner Owner, reason ReasonCode) (*Lease, error) {
	if id == f.extraLease.ID {
		f.extraLease.State = StateReleased
		return f.extraLease, nil
	}
	return f.mgr.Release(ctx, id, owner, reason)
}

func (f *fakeDuplicateBackend) ListLeases(ctx context.Context) ([]Lease, error) {
	list, err := f.mgr.ListLeases(ctx)
	if err != nil {
		return nil, err
	}
	return append(list, *f.extraLease), nil
}

// TestReconciler_StaleOrphanSkipped verifies that compare-before-act skips orphan remediation
// if an intent appears right before execution.
func TestReconciler_StaleOrphanSkipped(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	store := NewInMemoryIntentStore()
	ctx := context.Background()

	lOrphan, _ := mgr.Acquire(ctx, "worker-orphan", "res:STALE_ORPHAN", 10*time.Minute)

	// Wrap backend so that when ListLeases is called during compare-before-act, an intent is inserted!
	staleBackend := &staleHookBackend{
		mgr: mgr,
		onList: func() {
			_ = store.SaveIntent(ctx, LeaseIntent{
				IntentID: "intent-fresh-appeared",
				LeaseID:  lOrphan.ID,
				Owner:    "worker-orphan",
				Scope:    "res:STALE_ORPHAN",
				State:    IntentStateActive,
			})
		},
	}

	reconciler, _ := NewReconciler(ReconcilerConfig{
		IntentStore:          store,
		Backend:              staleBackend,
		AutoRemediateOrphans: true,
	})

	report, err := reconciler.Reconcile(ctx)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	if len(report.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(report.Items))
	}
	if report.Items[0].RemediationOutcome != RemediationSkippedStale {
		t.Errorf("expected RemediationOutcome %s, got %s", RemediationSkippedStale, report.Items[0].RemediationOutcome)
	}

	// Verify lease in backend was NOT released!
	lCheck, _ := mgr.Get(lOrphan.ID)
	if lCheck.State != StateAcquired {
		t.Errorf("stale orphan should NOT have been released! got state %s", lCheck.State)
	}
}

type staleHookBackend struct {
	mgr    *Manager
	onList func()
	calls  int
}

func (s *staleHookBackend) Acquire(ctx context.Context, owner Owner, scope Scope, ttl time.Duration) (*Lease, error) {
	return s.mgr.Acquire(ctx, owner, scope, ttl)
}

func (s *staleHookBackend) Renew(ctx context.Context, id ID, owner Owner, ttl time.Duration) (*Lease, error) {
	return s.mgr.Renew(ctx, id, owner, ttl)
}

func (s *staleHookBackend) Release(ctx context.Context, id ID, owner Owner, reason ReasonCode) (*Lease, error) {
	return s.mgr.Release(ctx, id, owner, reason)
}

func (s *staleHookBackend) ListLeases(ctx context.Context) ([]Lease, error) {
	s.calls++
	if s.calls == 2 && s.onList != nil {
		s.onList()
	}
	return s.mgr.ListLeases(ctx)
}

// TestReconciler_DeterministicReportOrdering verifies that report items are deterministically sorted.
func TestReconciler_DeterministicReportOrdering(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	store := NewInMemoryIntentStore()
	ctx := context.Background()

	// Populate scopes Z, A, M
	scopes := []Scope{"res:Z", "res:A", "res:M"}
	for _, sc := range scopes {
		l, _ := mgr.Acquire(ctx, "worker-1", sc, 10*time.Minute)
		_ = store.SaveIntent(ctx, LeaseIntent{
			IntentID: ID("intent-" + sc),
			LeaseID:  l.ID,
			Owner:    "worker-1",
			Scope:    sc,
			State:    IntentStateActive,
		})
	}

	reconciler, _ := NewReconciler(ReconcilerConfig{
		IntentStore: store,
		Backend:     mgr,
	})

	report1, _ := reconciler.Reconcile(ctx)
	report2, _ := reconciler.Reconcile(ctx)

	if len(report1.Items) != 3 || len(report2.Items) != 3 {
		t.Fatalf("expected 3 items in reports")
	}

	// Verify order is res:A -> res:M -> res:Z
	expectedScopes := []Scope{"res:A", "res:M", "res:Z"}
	for i, sc := range expectedScopes {
		if report1.Items[i].Scope != sc {
			t.Errorf("report1 index %d expected %s, got %s", i, sc, report1.Items[i].Scope)
		}
		if report2.Items[i].Scope != sc {
			t.Errorf("report2 index %d expected %s, got %s", i, sc, report2.Items[i].Scope)
		}
	}
}

// TestReconciler_CompositeGrouping verifies that member lease statuses are aggregated into composite summaries.
func TestReconciler_CompositeGrouping(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	store := NewInMemoryIntentStore()
	ctx := context.Background()

	lA, _ := mgr.Acquire(ctx, "worker-1", "res:A", 10*time.Minute)

	// Intent for res:A confirmed, intent for res:B missing
	_ = store.SaveIntent(ctx, LeaseIntent{
		IntentID:    "intent-comp-A",
		LeaseID:     lA.ID,
		Owner:       "worker-1",
		Scope:       "res:A",
		CompositeID: "comp-batch-100",
		State:       IntentStateActive,
	})

	_ = store.SaveIntent(ctx, LeaseIntent{
		IntentID:    "intent-comp-B",
		LeaseID:     "lse-missing-B",
		Owner:       "worker-1",
		Scope:       "res:B",
		CompositeID: "comp-batch-100",
		State:       IntentStateActive,
	})

	reconciler, _ := NewReconciler(ReconcilerConfig{
		IntentStore: store,
		Backend:     mgr,
	})

	report, err := reconciler.Reconcile(ctx)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	if len(report.Summary.Composites) != 1 {
		t.Fatalf("expected 1 composite summary, got %d", len(report.Summary.Composites))
	}

	compSum := report.Summary.Composites[0]
	if compSum.CompositeID != "comp-batch-100" {
		t.Errorf("expected CompositeID comp-batch-100, got %s", compSum.CompositeID)
	}
	if compSum.Confirmed != 1 || compSum.Missing != 1 {
		t.Errorf("expected 1 confirmed and 1 missing member in composite summary, got %+v", compSum)
	}
	if compSum.Status != ReconciliationStatusManualInterventionRequired {
		t.Errorf("expected composite status manual-intervention-required due to missing member, got %s", compSum.Status)
	}
}

// TestReconciler_RaceConditions tests concurrent intent store operations and reconciliations under -race.
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
					IntentID:  intentID,
					Owner:     owner,
					Scope:     scope,
					State:     IntentStateActive,
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
