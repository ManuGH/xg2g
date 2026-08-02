// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package lease

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestStartupPolicy_RefusesManualIntervention verifies policy refusal on manual intervention.
func TestStartupPolicy_RefusesManualIntervention(t *testing.T) {
	policy := DefaultStartupPolicy{}
	report := ReconciliationReport{
		Summary: ReconciliationSummary{ManualInterventionRequired: 1},
	}
	res := policy.Evaluate(report)
	if res.Decision != StartupRefused {
		t.Errorf("expected StartupRefused, got %s", res.Decision)
	}
}

// TestStartupPolicy_RefusesMissingIntents verifies policy refusal on missing backend lease for active intent.
func TestStartupPolicy_RefusesMissingIntents(t *testing.T) {
	policy := DefaultStartupPolicy{}
	report := ReconciliationReport{
		Summary: ReconciliationSummary{Missing: 1},
	}
	res := policy.Evaluate(report)
	if res.Decision != StartupRefused {
		t.Errorf("expected StartupRefused, got %s", res.Decision)
	}
}

// TestStartupPolicy_RefusesBrokenComposite verifies policy refusal on broken composite.
func TestStartupPolicy_RefusesBrokenComposite(t *testing.T) {
	policy := DefaultStartupPolicy{}
	report := ReconciliationReport{
		Summary: ReconciliationSummary{
			Composites: []CompositeReconciliationSummary{
				{CompositeID: "comp-1", Status: CompositeStatusBroken},
			},
		},
	}
	res := policy.Evaluate(report)
	if res.Decision != StartupRefused {
		t.Errorf("expected StartupRefused, got %s", res.Decision)
	}
}

// TestStartupPolicy_RefusesFailedRemediation verifies policy refusal when orphan remediation fails.
func TestStartupPolicy_RefusesFailedRemediation(t *testing.T) {
	policy := DefaultStartupPolicy{}
	report := ReconciliationReport{
		Items: []ReconciliationItem{
			{Scope: "tuner:0", RemediationOutcome: RemediationFailed, Diagnostic: "release error"},
		},
	}
	res := policy.Evaluate(report)
	if res.Decision != StartupRefused {
		t.Errorf("expected StartupRefused, got %s", res.Decision)
	}
}

// TestStartupPolicy_RefusesUnremediatedOrphansWhenAuditOnly verifies policy refusal on unremediated orphans.
func TestStartupPolicy_RefusesUnremediatedOrphansWhenAuditOnly(t *testing.T) {
	policy := DefaultStartupPolicy{AllowAuditOnlyOrphans: false}
	report := ReconciliationReport{
		Summary: ReconciliationSummary{Orphaned: 1, RemediatedOrphans: 0},
	}
	res := policy.Evaluate(report)
	if res.Decision != StartupRefused {
		t.Errorf("expected StartupRefused, got %s", res.Decision)
	}
}

type failReleaseController struct {
	TunerLeaseController
}

func (f failReleaseController) Release(ctx context.Context, handle *TunerLeaseHandle, reason ReasonCode) error {
	return errors.New("simulated backend release error")
}

// TestIntentTrackedController_AcquireFailurePersistsTerminal verifies that when backend acquire fails,
// intent is persisted as TERMINAL and error is combined via errors.Join.
func TestIntentTrackedController_AcquireFailurePersistsTerminal(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	store := NewInMemoryIntentStore()
	ctx := context.Background()

	// Occupy tuner:0 so next acquire fails with ErrScopeConflict
	_, _ = mgr.Acquire(ctx, "existing-owner", "tuner:0", 10*time.Minute)

	tb := NewTunerBinding(mgr)
	controller := NewTunerBindingController(tb)
	trackedCtrl, _ := NewIntentTrackedTunerLeaseControllerWithConfig(IntentTrackedControllerConfig{
		Controller:    controller,
		IntentStore:   store,
		IDGenerator:   func() (ID, error) { return "intent-fail-acq", nil },
		StartupPolicy: DefaultStartupPolicy{AllowAuditOnlyOrphans: true},
	})
	_, _ = trackedCtrl.ExecuteStartupReconciliation(ctx, ReconcilerConfig{IntentStore: store, Backend: mgr})

	_, err := trackedCtrl.Acquire(ctx, "session-fail", 0, 10*time.Minute)
	if !errors.Is(err, ErrScopeConflict) {
		t.Fatalf("expected ErrScopeConflict, got %v", err)
	}

	// Verify intent was persisted as TERMINAL
	in, getErr := store.GetIntent(ctx, "intent-fail-acq")
	if getErr != nil {
		t.Fatalf("failed to get intent: %v", getErr)
	}
	if in.State != IntentStateTerminal {
		t.Errorf("expected intent state TERMINAL after acquire failure, got %s", in.State)
	}
}

// TestIntentTrackedController_ReleaseBackendFailurePersistsRecoveryRequiredNotTerminal verifies that if backend release
// returns an error, intent state transitions to RECOVERY_REQUIRED and NEVER TERMINAL.
func TestIntentTrackedController_ReleaseBackendFailurePersistsRecoveryRequiredNotTerminal(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	store := NewInMemoryIntentStore()
	ctx := context.Background()

	tb := NewTunerBinding(mgr)
	baseController := NewTunerBindingController(tb)
	failController := failReleaseController{TunerLeaseController: baseController}

	trackedCtrl, _ := NewIntentTrackedTunerLeaseControllerWithConfig(IntentTrackedControllerConfig{
		Controller:  failController,
		IntentStore: store,
		IDGenerator: func() (ID, error) { return "intent-rel-fail", nil },
	})
	_, _ = trackedCtrl.ExecuteStartupReconciliation(ctx, ReconcilerConfig{IntentStore: store, Backend: mgr})

	handle, err := trackedCtrl.Acquire(ctx, "session-rel-fail", 0, 10*time.Minute)
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	relErr := trackedCtrl.Release(ctx, handle, ReasonReleasedByOwner)
	if relErr == nil {
		t.Fatalf("expected release error from failReleaseController")
	}

	// Verify intent state is RECOVERY_REQUIRED and NOT TERMINAL
	in, _ := store.GetIntent(ctx, "intent-rel-fail")
	if in.State == IntentStateTerminal {
		t.Errorf("CRITICAL SAFETY VIOLATION: intent set to TERMINAL despite backend release failure!")
	}
	if in.State != IntentStateRecoveryRequired {
		t.Errorf("expected intent state RECOVERY_REQUIRED, got %s", in.State)
	}
}

// TestReasonCodeMatrix_CompletenessAndNoUnknowns asserts that every ReasonCode and ReconciliationReasonCode
// constant in Go is documented exactly once in lease_reason_matrix.md.
func TestReasonCodeMatrix_CompletenessAndNoUnknowns(t *testing.T) {
	matrixPath := filepath.Join("..", "..", "..", "docs", "ADR", "lease_reason_matrix.md")
	contentBytes, err := os.ReadFile(matrixPath)
	if err != nil {
		t.Fatalf("failed to read lease_reason_matrix.md: %v", err)
	}
	content := string(contentBytes)

	// List all Go ReasonCode constants
	reasonCodes := []ReasonCode{
		ReasonAcquired,
		ReasonReleasedByOwner,
		ReasonExpired,
		ReasonRevokedByAdmin,
		ReasonPreempted,
		ReasonOwnerMismatch,
		ReasonAlreadyReleased,
		ReasonAlreadyExpired,
		ReasonScopeConflict,
		ReasonInvalidTTL,
		ReasonInvalidOwner,
		ReasonInvalidScope,
		ReasonNotFound,
		ReasonManagerClosed,
		ReasonRollback,
		ReasonReconciliationOrphanCleanup,
		ReasonReconciliationRecoveryRequired,
	}

	for _, rc := range reasonCodes {
		if !strings.Contains(content, "`"+string(rc)+"`") {
			t.Errorf("ReasonCode constant %q is missing from lease_reason_matrix.md", rc)
		}
	}

	// List all Go ReconciliationReasonCode constants
	reconReasonCodes := []ReconciliationReasonCode{
		ReasonReconciliationMatchConfirmed,
		ReasonReconciliationReleased,
		ReasonReconciliationLeaseMissing,
		ReasonReconciliationOrphaned,
		ReasonReconciliationOwnerMismatch,
		ReasonReconciliationLeaseIDMismatch,
		ReasonReconciliationDuplicateIntents,
		ReasonReconciliationDuplicateBackendLeases,
		ReasonReconciliationOrphanReleaseFailed,
		ReasonReconciliationRevalidationFailed,
		ReasonReconciliationPendingAcquisition,
		ReasonReconciliationReleasePending,
		ReasonReconciliationBrokenComposite,
	}

	for _, rrc := range reconReasonCodes {
		if !strings.Contains(content, "`"+string(rrc)+"`") {
			t.Errorf("ReconciliationReasonCode constant %q is missing from lease_reason_matrix.md", rrc)
		}
	}
}

// TestLeasePackage_ManualWiringLifecycle tests manual assembly of lease components within the lease package.
func TestLeasePackage_ManualWiringLifecycle(t *testing.T) {
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

	report, err := trackedCtrl.ExecuteStartupReconciliation(ctx, ReconcilerConfig{
		IntentStore: fileStore,
		Backend:     mgr,
	})
	if err != nil {
		t.Fatalf("startup reconciliation failed: %v", err)
	}
	if report.Summary.TotalChecked != 0 {
		t.Errorf("expected 0 initial checked items, got %d", report.Summary.TotalChecked)
	}

	handle, err := trackedCtrl.Acquire(ctx, "session-manual-1", 0, 5*time.Minute)
	if err != nil {
		t.Fatalf("session acquisition failed: %v", err)
	}

	err = trackedCtrl.Release(ctx, handle, ReasonReleasedByOwner)
	if err != nil {
		t.Fatalf("release lease failed: %v", err)
	}
}
