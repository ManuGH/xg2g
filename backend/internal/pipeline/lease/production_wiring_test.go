// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package lease

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// TestProductionWiring_MandatoryStartupGateEnforcement verifies that Acquire is blocked
// until ExecuteStartupReconciliation completes successfully.
func TestProductionWiring_MandatoryStartupGateEnforcement(t *testing.T) {
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

	// 1. Initial state is GatePending -> Acquire MUST fail with ErrStartupReconciliationRequired
	if trackedCtrl.GateState() != GatePending {
		t.Errorf("expected initial gate state GatePending, got %s", trackedCtrl.GateState())
	}
	_, err = trackedCtrl.Acquire(ctx, "session-1", 0, 10*time.Minute)
	if !errors.Is(err, ErrStartupReconciliationRequired) {
		t.Errorf("expected ErrStartupReconciliationRequired when gate is pending, got %v", err)
	}

	// 2. Execute startup reconciliation -> GateAccepted
	report, err := trackedCtrl.ExecuteStartupReconciliation(ctx, ReconcilerConfig{
		IntentStore: store,
		Backend:     mgr,
	})
	if err != nil {
		t.Fatalf("ExecuteStartupReconciliation failed: %v", err)
	}
	if report == nil {
		t.Fatalf("expected non-nil report")
	}
	if trackedCtrl.GateState() != GateAccepted {
		t.Errorf("expected gate state GateAccepted after clean reconciliation, got %s", trackedCtrl.GateState())
	}

	// 3. Acquire after GateAccepted MUST succeed
	handle, err := trackedCtrl.Acquire(ctx, "session-1", 0, 10*time.Minute)
	if err != nil {
		t.Fatalf("Acquire failed after GateAccepted: %v", err)
	}
	if handle == nil || handle.LeaseID == "" {
		t.Fatalf("expected valid handle with LeaseID")
	}
}

// TestProductionWiring_StartupGateRefusedOnConflict verifies that if startup reconciliation detects
// a conflict requiring manual intervention, the gate transitions to GateRefused and blocks Acquire.
func TestProductionWiring_StartupGateRefusedOnConflict(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	store := NewInMemoryIntentStore()
	ctx := context.Background()

	// Seed conflicting active intents for the same scope
	_ = store.SaveIntent(ctx, LeaseIntent{
		IntentID:  "intent-conflict-1",
		LeaseID:   "lse-conflict-1",
		Owner:     "owner-A",
		Scope:     "tuner:0",
		State:     IntentStateActive,
		Revision:  1,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})
	_ = store.SaveIntent(ctx, LeaseIntent{
		IntentID:  "intent-conflict-2",
		LeaseID:   "lse-conflict-2",
		Owner:     "owner-B",
		Scope:     "tuner:0",
		State:     IntentStateActive,
		Revision:  1,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})

	tb := NewTunerBinding(mgr)
	controller := NewTunerBindingController(tb)
	trackedCtrl, _ := NewIntentTrackedTunerLeaseController(controller, store)

	// Execute startup reconciliation -> MUST return error and set gate to GateRefused
	_, err := trackedCtrl.ExecuteStartupReconciliation(ctx, ReconcilerConfig{
		IntentStore: store,
		Backend:     mgr,
	})
	if !errors.Is(err, ErrStartupReconciliationFailed) {
		t.Errorf("expected ErrStartupReconciliationFailed, got %v", err)
	}
	if trackedCtrl.GateState() != GateRefused {
		t.Errorf("expected gate state GateRefused, got %s", trackedCtrl.GateState())
	}

	// Subsequent Acquire MUST fail with ErrStartupReconciliationFailed
	_, err = trackedCtrl.Acquire(ctx, "session-2", 0, 10*time.Minute)
	if !errors.Is(err, ErrStartupReconciliationFailed) {
		t.Errorf("expected ErrStartupReconciliationFailed on Acquire, got %v", err)
	}
}

// TestIntegration_FullBootstrapLifecycle performs an end-to-end integration test
// verifying clean startup, mandatory gate check, session acquisition, renewal, release, and zero-leak intent alignment.
func TestIntegration_FullBootstrapLifecycle(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "intents.json")

	fileStore, err := NewFileIntentStore(storePath)
	if err != nil {
		t.Fatalf("failed to create FileIntentStore: %v", err)
	}
	defer fileStore.Close()

	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	ctx := context.Background()

	tb := NewTunerBinding(mgr)
	controller := NewTunerBindingController(tb)
	trackedCtrl, err := NewIntentTrackedTunerLeaseController(controller, fileStore)
	if err != nil {
		t.Fatalf("failed to create IntentTrackedTunerLeaseController: %v", err)
	}

	// Step 1: Execute mandatory startup reconciliation
	report, err := trackedCtrl.ExecuteStartupReconciliation(ctx, ReconcilerConfig{
		IntentStore: fileStore,
		Backend:     mgr,
	})
	if err != nil {
		t.Fatalf("startup reconciliation failed: %v", err)
	}
	if report.Summary.TotalChecked != 0 {
		t.Errorf("expected 0 initial checked items on clean store, got %d", report.Summary.TotalChecked)
	}

	// Step 2: First session acquires tuner lease
	handle, err := trackedCtrl.Acquire(ctx, "session-bootstrap-1", 0, 5*time.Minute)
	if err != nil {
		t.Fatalf("session acquisition failed: %v", err)
	}

	// Step 3: Verify intent is ACTIVE in FileIntentStore
	intents, err := fileStore.ListIntents(ctx)
	if err != nil || len(intents) != 1 {
		t.Fatalf("expected 1 intent in FileIntentStore, got %d (err=%v)", len(intents), err)
	}
	if intents[0].State != IntentStateActive || intents[0].LeaseID != handle.LeaseID {
		t.Errorf("unexpected intent state in store: %+v", intents[0])
	}

	// Step 4: Renew lease
	err = trackedCtrl.Renew(ctx, handle, 10*time.Minute)
	if err != nil {
		t.Fatalf("renew lease failed: %v", err)
	}

	// Step 5: Release lease cleanly
	err = trackedCtrl.Release(ctx, handle, ReasonReleasedByOwner)
	if err != nil {
		t.Fatalf("release lease failed: %v", err)
	}

	// Step 6: Verify intent is TERMINAL in FileIntentStore
	intentsAfter, err := fileStore.ListIntents(ctx)
	if err != nil || len(intentsAfter) != 1 {
		t.Fatalf("expected 1 intent after release, got %d", len(intentsAfter))
	}
	if intentsAfter[0].State != IntentStateTerminal {
		t.Errorf("expected intent state TERMINAL after release, got %s", intentsAfter[0].State)
	}

	// Step 7: Run post-release audit reconciliation -> MUST be 100% confirmed released
	postReport, err := RunStartupReconciliation(ctx, ReconcilerConfig{
		IntentStore: fileStore,
		Backend:     mgr,
	})
	if err != nil {
		t.Fatalf("post-release reconciliation failed: %v", err)
	}
	if postReport.Summary.Released != 1 {
		t.Errorf("expected 1 released item in post-release reconciliation report, got summary: %+v", postReport.Summary)
	}
}
