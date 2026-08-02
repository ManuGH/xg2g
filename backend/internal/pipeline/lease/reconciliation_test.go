// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package lease

import (
	"context"
	"errors"
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

	// Close store1 to release path lock
	if err := store1.Close(); err != nil {
		t.Fatalf("store1 Close failed: %v", err)
	}

	// 2. Simulate process restart: Re-open with store2 on same file
	store2, err := NewFileIntentStore(storePath)
	if err != nil {
		t.Fatalf("failed to re-open store2: %v", err)
	}
	defer store2.Close()

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

	lBackend, err := mgr.Acquire(ctx, "worker-persistent", "res:PERSISTENT", 10*time.Minute)
	if err != nil {
		t.Fatalf("backend acquire failed: %v", err)
	}

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

// TestFileIntentStore_SingleWriterPathRegistry verifies process-wide single-writer protection.
func TestFileIntentStore_SingleWriterPathRegistry(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "intents_single_writer.json")

	store1, err := NewFileIntentStore(storePath)
	if err != nil {
		t.Fatalf("failed to create store1: %v", err)
	}

	// Second open on same path must fail with ErrIntentStoreAlreadyOpen
	_, err = NewFileIntentStore(storePath)
	if !errors.Is(err, ErrIntentStoreAlreadyOpen) {
		t.Errorf("expected ErrIntentStoreAlreadyOpen, got %v", err)
	}

	// Close store1
	_ = store1.Close()

	// Re-open after Close must succeed
	store2, err := NewFileIntentStore(storePath)
	if err != nil {
		t.Fatalf("failed to re-open store2 after Close: %v", err)
	}
	_ = store2.Close()
}

// TestFileIntentStore_CASRevisionProtection verifies CAS revision enforcement.
func TestFileIntentStore_CASRevisionProtection(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "intents_cas.json")
	ctx := context.Background()

	store, err := NewFileIntentStore(storePath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	intent := LeaseIntent{
		IntentID: "intent-cas-1",
		LeaseID:  "lse-100",
		Owner:    "worker-1",
		Scope:    "res:CAS",
		State:    IntentStateActive,
		Revision: 1,
	}

	if err := store.SaveIntent(ctx, intent); err != nil {
		t.Fatalf("initial SaveIntent failed: %v", err)
	}

	// Save with same revision -> ErrIntentConflict
	intent.Revision = 1
	err = store.SaveIntent(ctx, intent)
	if !errors.Is(err, ErrIntentConflict) {
		t.Errorf("expected ErrIntentConflict for same revision update, got %v", err)
	}

	// Save with sequential revision 2 -> Success
	intent.Revision = 2
	if err := store.SaveIntent(ctx, intent); err != nil {
		t.Fatalf("sequential update to revision 2 failed: %v", err)
	}
}

// TestReconciler_MixedStateAuthoritativeSelection verifies that when a TERMINAL intent-a and an ACTIVE intent-b
// exist for the same scope with a matching backend lease-b, ACTIVE intent-b is authoritatively selected.
func TestReconciler_MixedStateAuthoritativeSelection(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	store := NewInMemoryIntentStore()
	ctx := context.Background()

	lB, _ := mgr.Acquire(ctx, "worker-B", "res:MIXED_SCOPE", 10*time.Minute)

	// Historical TERMINAL intent-a
	_ = store.SaveIntent(ctx, LeaseIntent{
		IntentID:  "intent-A-terminal",
		Owner:     "worker-A",
		Scope:     "res:MIXED_SCOPE",
		State:     IntentStateTerminal,
		Revision:  1,
		UpdatedAt: time.Now().Add(-1 * time.Hour),
	})

	// Active intent-b
	_ = store.SaveIntent(ctx, LeaseIntent{
		IntentID:  "intent-B-active",
		LeaseID:   lB.ID,
		Owner:     "worker-B",
		Scope:     "res:MIXED_SCOPE",
		State:     IntentStateActive,
		Revision:  1,
		UpdatedAt: time.Now(),
	})

	reconciler, _ := NewReconciler(ReconcilerConfig{
		IntentStore: store,
		Backend:     mgr,
	})

	report, err := reconciler.Reconcile(ctx)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	if len(report.Items) != 1 {
		t.Fatalf("expected 1 item for scope, got %d", len(report.Items))
	}
	if report.Items[0].IntentID != "intent-B-active" {
		t.Errorf("expected authoritative intent to be intent-B-active, got %s", report.Items[0].IntentID)
	}
	if report.Items[0].ObservedStatus != ReconciliationStatusConfirmed {
		t.Errorf("expected status CONFIRMED, got %s", report.Items[0].ObservedStatus)
	}
}

// TestReconciler_OrphanConfirmationFailure verifies that if release returns nil error
// but confirmation ListLeases returns an error, RemediationOutcome is set to RemediationFailed.
func TestReconciler_OrphanConfirmationFailure(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	store := NewInMemoryIntentStore()
	ctx := context.Background()

	lOrphan, _ := mgr.Acquire(ctx, "worker-orphan", "res:CONFIRM_FAIL", 10*time.Minute)

	failBackend := &confirmFailBackend{
		mgr:        mgr,
		orphanID:   lOrphan.ID,
		failOnCall: 3,
	}

	reconciler, _ := NewReconciler(ReconcilerConfig{
		IntentStore:          store,
		Backend:              failBackend,
		AutoRemediateOrphans: true,
	})

	report, err := reconciler.Reconcile(ctx)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	if len(report.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(report.Items))
	}
	if report.Items[0].RemediationOutcome != RemediationFailed {
		t.Errorf("expected RemediationOutcome %s on confirmation failure, got %s", RemediationFailed, report.Items[0].RemediationOutcome)
	}
	if report.Items[0].ReasonCode != ReasonReconciliationOrphanReleaseFailed {
		t.Errorf("expected reason %s, got %s", ReasonReconciliationOrphanReleaseFailed, report.Items[0].ReasonCode)
	}
}

type confirmFailBackend struct {
	mgr        *Manager
	orphanID   ID
	calls      int
	failOnCall int
}

func (c *confirmFailBackend) Acquire(ctx context.Context, owner Owner, scope Scope, ttl time.Duration) (*Lease, error) {
	return c.mgr.Acquire(ctx, owner, scope, ttl)
}

func (c *confirmFailBackend) Renew(ctx context.Context, id ID, owner Owner, ttl time.Duration) (*Lease, error) {
	return c.mgr.Renew(ctx, id, owner, ttl)
}

func (c *confirmFailBackend) Release(ctx context.Context, id ID, owner Owner, reason ReasonCode) (*Lease, error) {
	return c.mgr.Release(ctx, id, owner, reason)
}

func (c *confirmFailBackend) ListLeases(ctx context.Context) ([]Lease, error) {
	c.calls++
	if c.calls == c.failOnCall {
		return nil, errors.New("injected confirmation backend error")
	}
	return c.mgr.ListLeases(ctx)
}

// TestReconciler_ParentCanceledDetachedRemediationConfirmation verifies that when parent context is canceled,
// detached remediation context allows release AND confirmation to succeed cleanly.
func TestReconciler_ParentCanceledDetachedRemediationConfirmation(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	store := NewInMemoryIntentStore()

	parentCtx, cancelParent := context.WithCancel(context.Background())

	lOrphan, _ := mgr.Acquire(context.Background(), "worker-orphan", "res:DETACHED_CANCEL", 10*time.Minute)

	cancelBackend := &cancelHookBackend{
		mgr: mgr,
		onRelease: func() {
			cancelParent()
		},
	}

	reconciler, _ := NewReconciler(ReconcilerConfig{
		IntentStore:          store,
		Backend:              cancelBackend,
		AutoRemediateOrphans: true,
	})

	report, err := reconciler.Reconcile(parentCtx)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	if len(report.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(report.Items))
	}
	if report.Items[0].RemediationOutcome != RemediationSucceeded {
		t.Errorf("expected RemediationOutcome SUCCEEDED despite parent context cancellation, got %s (err=%s)",
			report.Items[0].RemediationOutcome, report.Items[0].Diagnostic)
	}
	if report.Items[0].ObservedStatus != ReconciliationStatusOrphaned || report.Items[0].FinalStatus != ReconciliationStatusReleased {
		t.Errorf("expected ObservedStatus=ORPHANED, FinalStatus=RELEASED, got Observed=%s, Final=%s",
			report.Items[0].ObservedStatus, report.Items[0].FinalStatus)
	}

	lCheck, _ := mgr.Get(lOrphan.ID)
	if lCheck.State != StateReleased {
		t.Errorf("expected orphan lease to be RELEASED, got %s", lCheck.State)
	}
}

type cancelHookBackend struct {
	mgr       *Manager
	onRelease func()
}

func (c *cancelHookBackend) Acquire(ctx context.Context, owner Owner, scope Scope, ttl time.Duration) (*Lease, error) {
	return c.mgr.Acquire(ctx, owner, scope, ttl)
}

func (c *cancelHookBackend) Renew(ctx context.Context, id ID, owner Owner, ttl time.Duration) (*Lease, error) {
	return c.mgr.Renew(ctx, id, owner, ttl)
}

func (c *cancelHookBackend) Release(ctx context.Context, id ID, owner Owner, reason ReasonCode) (*Lease, error) {
	if c.onRelease != nil {
		c.onRelease()
	}
	return c.mgr.Release(ctx, id, owner, reason)
}

func (c *cancelHookBackend) ListLeases(ctx context.Context) ([]Lease, error) {
	return c.mgr.ListLeases(ctx)
}

// TestReconciler_RecheckErrorRevalidationFailed verifies technical errors during compare-before-act
// produce RemediationOutcome = RemediationFailed and ReasonCode = RECONCILIATION_REVALIDATION_FAILED.
func TestReconciler_RecheckErrorRevalidationFailed(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	store := NewInMemoryIntentStore()
	ctx := context.Background()

	lOrphan, _ := mgr.Acquire(ctx, "worker-orphan", "res:RECHECK_FAIL", 10*time.Minute)

	recheckFailBackend := &recheckFailBackend{
		mgr:      mgr,
		orphanID: lOrphan.ID,
	}

	reconciler, _ := NewReconciler(ReconcilerConfig{
		IntentStore:          store,
		Backend:              recheckFailBackend,
		AutoRemediateOrphans: true,
	})

	report, err := reconciler.Reconcile(ctx)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	if len(report.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(report.Items))
	}
	if report.Items[0].RemediationOutcome != RemediationFailed {
		t.Errorf("expected RemediationOutcome %s on revalidation error, got %s", RemediationFailed, report.Items[0].RemediationOutcome)
	}
	if report.Items[0].ReasonCode != ReasonReconciliationRevalidationFailed {
		t.Errorf("expected reason %s, got %s", ReasonReconciliationRevalidationFailed, report.Items[0].ReasonCode)
	}
}

type recheckFailBackend struct {
	mgr      *Manager
	orphanID ID
	calls    int
}

func (r *recheckFailBackend) Acquire(ctx context.Context, owner Owner, scope Scope, ttl time.Duration) (*Lease, error) {
	return r.mgr.Acquire(ctx, owner, scope, ttl)
}

func (r *recheckFailBackend) Renew(ctx context.Context, id ID, owner Owner, ttl time.Duration) (*Lease, error) {
	return r.mgr.Renew(ctx, id, owner, ttl)
}

func (r *recheckFailBackend) Release(ctx context.Context, id ID, owner Owner, reason ReasonCode) (*Lease, error) {
	return r.mgr.Release(ctx, id, owner, reason)
}

func (r *recheckFailBackend) ListLeases(ctx context.Context) ([]Lease, error) {
	r.calls++
	if r.calls == 2 {
		return nil, errors.New("injected compare-before-act recheck error")
	}
	return r.mgr.ListLeases(ctx)
}

// TestReconciler_IntentStateMatrix verifies PENDING, ACTIVE, RELEASING, and TERMINAL classifications.
func TestReconciler_IntentStateMatrix(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	store := NewInMemoryIntentStore()
	ctx := context.Background()

	// 1. PENDING intent without backend lease -> Confirmed / PendingAcquisition
	_ = store.SaveIntent(ctx, LeaseIntent{
		IntentID: "intent-pending-1",
		Owner:    "worker-1",
		Scope:    "res:PENDING_FREE",
		State:    IntentStatePending,
		Revision: 1,
	})

	// 2. RELEASING intent with active backend lease -> Confirmed / ReleasePending
	lReleasing, _ := mgr.Acquire(ctx, "worker-1", "res:RELEASING_ACTIVE", 10*time.Minute)
	_ = store.SaveIntent(ctx, LeaseIntent{
		IntentID: "intent-releasing-1",
		LeaseID:  lReleasing.ID,
		Owner:    "worker-1",
		Scope:    "res:RELEASING_ACTIVE",
		State:    IntentStateReleasing,
		Revision: 1,
	})

	// 3. TERMINAL intent without backend lease -> Released
	_ = store.SaveIntent(ctx, LeaseIntent{
		IntentID: "intent-terminal-1",
		Owner:    "worker-1",
		Scope:    "res:TERMINAL_FREE",
		State:    IntentStateTerminal,
		Revision: 1,
	})

	reconciler, _ := NewReconciler(ReconcilerConfig{
		IntentStore: store,
		Backend:     mgr,
	})

	report, err := reconciler.Reconcile(ctx)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	if len(report.Items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(report.Items))
	}

	itemPending := report.Items[0]
	if itemPending.Scope != "res:PENDING_FREE" || itemPending.ReasonCode != ReasonReconciliationPendingAcquisition {
		t.Errorf("unexpected itemPending: %+v", itemPending)
	}

	itemReleasing := report.Items[1]
	if itemReleasing.Scope != "res:RELEASING_ACTIVE" || itemReleasing.ReasonCode != ReasonReconciliationReleasePending {
		t.Errorf("unexpected itemReleasing: %+v", itemReleasing)
	}

	itemTerminal := report.Items[2]
	if itemTerminal.Scope != "res:TERMINAL_FREE" || itemTerminal.ReasonCode != ReasonReconciliationReleased {
		t.Errorf("unexpected itemTerminal: %+v", itemTerminal)
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
		Revision: 1,
	})
	_ = store.SaveIntent(ctx, LeaseIntent{
		IntentID: "intent-dup-2",
		LeaseID:  "lse-2",
		Owner:    "worker-2",
		Scope:    "res:DUPLICATE",
		State:    IntentStateActive,
		Revision: 1,
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
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	store := NewInMemoryIntentStore()
	ctx := context.Background()

	l1, _ := mgr.Acquire(ctx, "worker-1", "res:DUP_BACKEND", 10*time.Minute)

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
		Revision: 1,
	})

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

// TestReconciler_DeterministicReportOrdering verifies that report items are deterministically sorted.
func TestReconciler_DeterministicReportOrdering(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	store := NewInMemoryIntentStore()
	ctx := context.Background()

	scopes := []Scope{"res:Z", "res:A", "res:M"}
	for _, sc := range scopes {
		l, _ := mgr.Acquire(ctx, "worker-1", sc, 10*time.Minute)
		_ = store.SaveIntent(ctx, LeaseIntent{
			IntentID: ID("intent-" + sc),
			LeaseID:  l.ID,
			Owner:    "worker-1",
			Scope:    sc,
			State:    IntentStateActive,
			Revision: 1,
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

	_ = store.SaveIntent(ctx, LeaseIntent{
		IntentID:    "intent-comp-A",
		LeaseID:     lA.ID,
		Owner:       "worker-1",
		Scope:       "res:A",
		CompositeID: "comp-batch-100",
		State:       IntentStateActive,
		Revision:    1,
	})

	_ = store.SaveIntent(ctx, LeaseIntent{
		IntentID:    "intent-comp-B",
		Owner:       "worker-1",
		Scope:       "res:B",
		CompositeID: "comp-batch-100",
		State:       IntentStatePending,
		Revision:    1,
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
	if compSum.Confirmed != 1 || compSum.Pending != 1 {
		t.Errorf("expected 1 confirmed and 1 pending member in composite summary, got %+v", compSum)
	}
}

// TestProductionWiring_IntentTrackedTunerLeaseController verifies production wiring lifecycle and startup reconciliation.
func TestProductionWiring_IntentTrackedTunerLeaseController(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	store := NewInMemoryIntentStore()
	ctx := context.Background()

	tb := NewTunerBinding(mgr)
	controller := NewTunerBindingController(tb)
	trackedCtrl, err := NewIntentTrackedTunerLeaseController(controller, store)
	if err != nil {
		t.Fatalf("failed to create IntentTrackedTunerLeaseController: %v", err)
	}

	// 1. Acquire lease via production-wired tracked controller
	handle, err := trackedCtrl.Acquire(ctx, "session-wired-1", 0, 10*time.Minute)
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	if handle == nil || handle.LeaseID == "" {
		t.Fatalf("expected handle to have non-empty LeaseID")
	}

	// 2. Verify intent is persisted in store as ACTIVE with LeaseID
	intents, _ := store.ListIntents(ctx)
	if len(intents) != 1 {
		t.Fatalf("expected 1 intent in store, got %d", len(intents))
	}
	if intents[0].State != IntentStateActive || intents[0].LeaseID != handle.LeaseID {
		t.Errorf("unexpected intent state: %+v", intents[0])
	}

	// 3. Run startup reconciliation check
	report, err := RunStartupReconciliation(ctx, ReconcilerConfig{
		IntentStore: store,
		Backend:     mgr,
	})
	if err != nil {
		t.Fatalf("RunStartupReconciliation failed: %v", err)
	}

	if report.Summary.Confirmed != 1 {
		t.Errorf("expected 1 confirmed item in startup reconciliation report, got summary: %+v", report.Summary)
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
					IntentID: intentID,
					Owner:    owner,
					Scope:    scope,
					State:    IntentStateActive,
					LeaseID:  ID(fmt.Sprintf("lse-%s-%d", owner, j)),
					Revision: 1,
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
