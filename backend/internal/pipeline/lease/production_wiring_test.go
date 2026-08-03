// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package lease

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sessionstore "github.com/ManuGH/xg2g/internal/domain/session/store"
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

type mockFailIntentStore struct {
	*InMemoryIntentStore
	failSaveStates map[IntentState]error
	failList       error
}

func newMockFailIntentStore() *mockFailIntentStore {
	return &mockFailIntentStore{
		InMemoryIntentStore: NewInMemoryIntentStore(),
		failSaveStates:      make(map[IntentState]error),
	}
}

func (m *mockFailIntentStore) SaveIntent(ctx context.Context, intent LeaseIntent) error {
	if err, ok := m.failSaveStates[intent.State]; ok && err != nil {
		return err
	}
	return m.InMemoryIntentStore.SaveIntent(ctx, intent)
}

func (m *mockFailIntentStore) ListIntents(ctx context.Context) ([]LeaseIntent, error) {
	if m.failList != nil {
		return nil, m.failList
	}
	return m.InMemoryIntentStore.ListIntents(ctx)
}

type spyBackendController struct {
	TunerLeaseController
	releaseCalledCount int
	releaseCalled      bool
	releaseErr         error
	acquireNilHandle   bool
}

func (s *spyBackendController) Acquire(ctx context.Context, owner Owner, slot int, ttl time.Duration) (*TunerLeaseHandle, error) {
	if s.acquireNilHandle {
		return nil, nil
	}
	return s.TunerLeaseController.Acquire(ctx, owner, slot, ttl)
}

func (s *spyBackendController) Release(ctx context.Context, handle *TunerLeaseHandle, reason ReasonCode) error {
	s.releaseCalledCount++
	s.releaseCalled = true
	if s.releaseErr != nil {
		return s.releaseErr
	}
	return s.TunerLeaseController.Release(ctx, handle, reason)
}

// 1. ACTIVE save fails, rollback succeeds, recovery save succeeds.
func TestSaga_ACTIVE_SaveFails_RollbackSucceeds_RecoverySaveSucceeds(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	store := newMockFailIntentStore()
	store.failSaveStates[IntentStateActive] = errors.New("simulated ACTIVE save error")

	tb := NewTunerBinding(mgr)
	baseCtrl := NewTunerBindingController(tb)
	trackedCtrl, _ := NewIntentTrackedTunerLeaseControllerWithConfig(IntentTrackedControllerConfig{
		Controller:  baseCtrl,
		IntentStore: store,
		IDGenerator: func() (ID, error) { return "intent-act-fail", nil },
	})
	_, _ = trackedCtrl.ExecuteStartupReconciliation(context.Background(), ReconcilerConfig{IntentStore: store, Backend: mgr})

	_, err := trackedCtrl.Acquire(context.Background(), "session-1", 0, 10*time.Minute)
	if err == nil {
		t.Fatalf("expected acquire error")
	}
	if !strings.Contains(err.Error(), "failed to persist ACTIVE intent") {
		t.Errorf("expected ACTIVE save error in errors.Join, got %v", err)
	}

	in, _ := store.GetIntent(context.Background(), "intent-act-fail")
	if in.State != IntentStateRecoveryRequired {
		t.Errorf("expected intent state RECOVERY_REQUIRED, got %s", in.State)
	}
}

// 2. ACTIVE save fails, rollback fails.
func TestSaga_ACTIVE_SaveFails_RollbackFails(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	store := newMockFailIntentStore()
	store.failSaveStates[IntentStateActive] = errors.New("simulated ACTIVE save error")

	tb := NewTunerBinding(mgr)
	baseCtrl := NewTunerBindingController(tb)
	failCtrl := &spyBackendController{TunerLeaseController: baseCtrl, releaseErr: errors.New("rollback release failed")}

	trackedCtrl, _ := NewIntentTrackedTunerLeaseControllerWithConfig(IntentTrackedControllerConfig{
		Controller:  failCtrl,
		IntentStore: store,
	})
	_, _ = trackedCtrl.ExecuteStartupReconciliation(context.Background(), ReconcilerConfig{IntentStore: store, Backend: mgr})

	_, err := trackedCtrl.Acquire(context.Background(), "session-2", 0, 10*time.Minute)
	if err == nil {
		t.Fatalf("expected acquire error")
	}
	if !strings.Contains(err.Error(), "failed to persist ACTIVE intent") || !strings.Contains(err.Error(), "rollback release") {
		t.Errorf("expected both errors in errors.Join, got %v", err)
	}
}

// 3. ACTIVE save fails, recovery save fails.
func TestSaga_ACTIVE_SaveFails_RecoverySaveFails(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	store := newMockFailIntentStore()
	store.failSaveStates[IntentStateActive] = errors.New("simulated ACTIVE save error")
	store.failSaveStates[IntentStateRecoveryRequired] = errors.New("simulated RECOVERY_REQUIRED save error")

	tb := NewTunerBinding(mgr)
	baseCtrl := NewTunerBindingController(tb)

	trackedCtrl, _ := NewIntentTrackedTunerLeaseControllerWithConfig(IntentTrackedControllerConfig{
		Controller:  baseCtrl,
		IntentStore: store,
	})
	_, _ = trackedCtrl.ExecuteStartupReconciliation(context.Background(), ReconcilerConfig{IntentStore: store, Backend: mgr})

	_, err := trackedCtrl.Acquire(context.Background(), "session-3", 0, 10*time.Minute)
	if err == nil {
		t.Fatalf("expected acquire error")
	}
	if !strings.Contains(err.Error(), "persist RECOVERY_REQUIRED") {
		t.Errorf("expected recovery save error in errors.Join, got %v", err)
	}
}

// 4. Renew fails and recovery save fails.
func TestSaga_RenewFails_RecoverySaveFails(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	store := newMockFailIntentStore()

	tb := NewTunerBinding(mgr)
	baseCtrl := NewTunerBindingController(tb)
	trackedCtrl, _ := NewIntentTrackedTunerLeaseControllerWithConfig(IntentTrackedControllerConfig{
		Controller:  baseCtrl,
		IntentStore: store,
	})
	_, _ = trackedCtrl.ExecuteStartupReconciliation(context.Background(), ReconcilerConfig{IntentStore: store, Backend: mgr})

	handle, err := trackedCtrl.Acquire(context.Background(), "session-4", 0, 10*time.Minute)
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	// Trigger renew failure with invalid handle ID + fail RECOVERY_REQUIRED save
	badHandle := &TunerLeaseHandle{LeaseID: "nonexistent-lease", Scope: "tuner:0", Owner: "session-4"}
	store.failSaveStates[IntentStateRecoveryRequired] = errors.New("recovery save error")

	renewErr := trackedCtrl.Renew(context.Background(), badHandle, 10*time.Minute)
	if renewErr == nil {
		t.Fatalf("expected renew error")
	}
	_ = handle
}

// 5. RELEASING save fails -> Backend release is NOT called.
func TestSaga_RELEASING_SaveFails_BackendReleaseNotCalled(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	store := newMockFailIntentStore()

	tb := NewTunerBinding(mgr)
	baseCtrl := NewTunerBindingController(tb)
	spyCtrl := &spyBackendController{TunerLeaseController: baseCtrl}

	trackedCtrl, _ := NewIntentTrackedTunerLeaseControllerWithConfig(IntentTrackedControllerConfig{
		Controller:  spyCtrl,
		IntentStore: store,
		IDGenerator: func() (ID, error) { return "intent-rel-gate", nil },
	})
	_, _ = trackedCtrl.ExecuteStartupReconciliation(context.Background(), ReconcilerConfig{IntentStore: store, Backend: mgr})

	handle, err := trackedCtrl.Acquire(context.Background(), "session-5", 0, 10*time.Minute)
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	spyCtrl.releaseCalled = false
	store.failSaveStates[IntentStateReleasing] = errors.New("simulated RELEASING save failure")

	relErr := trackedCtrl.Release(context.Background(), handle, ReasonReleasedByOwner)
	if relErr == nil {
		t.Fatalf("expected release error")
	}
	if !strings.Contains(relErr.Error(), "failed to persist RELEASING intent prior to backend mutation") {
		t.Errorf("expected RELEASING save error, got %v", relErr)
	}
	if spyCtrl.releaseCalled {
		t.Errorf("CRITICAL SAFETY VIOLATION: backend release called despite RELEASING save failure!")
	}
}

// 6. Backend release fails -> Intent is NOT TERMINAL.
func TestSaga_BackendReleaseFails_IntentNotTerminal(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	store := newMockFailIntentStore()

	tb := NewTunerBinding(mgr)
	baseCtrl := NewTunerBindingController(tb)
	spyCtrl := &spyBackendController{TunerLeaseController: baseCtrl, releaseErr: errors.New("backend release error")}

	trackedCtrl, _ := NewIntentTrackedTunerLeaseControllerWithConfig(IntentTrackedControllerConfig{
		Controller:  spyCtrl,
		IntentStore: store,
		IDGenerator: func() (ID, error) { return "intent-rel-backend-fail", nil },
	})
	_, _ = trackedCtrl.ExecuteStartupReconciliation(context.Background(), ReconcilerConfig{IntentStore: store, Backend: mgr})

	handle, err := trackedCtrl.Acquire(context.Background(), "session-6", 0, 10*time.Minute)
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	relErr := trackedCtrl.Release(context.Background(), handle, ReasonReleasedByOwner)
	if relErr == nil {
		t.Fatalf("expected release error")
	}

	in, _ := store.GetIntent(context.Background(), "intent-rel-backend-fail")
	if in.State == IntentStateTerminal {
		t.Errorf("CRITICAL SAFETY VIOLATION: intent marked TERMINAL despite backend release failure!")
	}
	if in.State != IntentStateRecoveryRequired {
		t.Errorf("expected intent state RECOVERY_REQUIRED, got %s", in.State)
	}
}

// 7. TERMINAL save fails -> Error is returned and recovery is visible.
func TestSaga_TERMINAL_SaveFails_RecoveryVisible(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	store := newMockFailIntentStore()

	tb := NewTunerBinding(mgr)
	baseCtrl := NewTunerBindingController(tb)

	trackedCtrl, _ := NewIntentTrackedTunerLeaseControllerWithConfig(IntentTrackedControllerConfig{
		Controller:  baseCtrl,
		IntentStore: store,
		IDGenerator: func() (ID, error) { return "intent-term-save-fail", nil },
	})
	_, _ = trackedCtrl.ExecuteStartupReconciliation(context.Background(), ReconcilerConfig{IntentStore: store, Backend: mgr})

	handle, err := trackedCtrl.Acquire(context.Background(), "session-7", 0, 10*time.Minute)
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	store.failSaveStates[IntentStateTerminal] = errors.New("simulated TERMINAL save failure")

	relErr := trackedCtrl.Release(context.Background(), handle, ReasonReleasedByOwner)
	if relErr == nil {
		t.Fatalf("expected release error")
	}
	if !strings.Contains(relErr.Error(), "failed to persist TERMINAL intent") {
		t.Errorf("expected TERMINAL save error, got %v", relErr)
	}

	in, _ := store.GetIntent(context.Background(), "intent-term-save-fail")
	if in.State != IntentStateRecoveryRequired {
		t.Errorf("expected intent state RECOVERY_REQUIRED after terminal save failure, got %s", in.State)
	}
}

// 8. Parent context canceled -> Terminal cleanup continues detached.
func TestSaga_ParentContextCanceled_DetachedCleanupRuns(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	store := NewInMemoryIntentStore()

	tb := NewTunerBinding(mgr)
	baseCtrl := NewTunerBindingController(tb)

	trackedCtrl, _ := NewIntentTrackedTunerLeaseControllerWithConfig(IntentTrackedControllerConfig{
		Controller:  baseCtrl,
		IntentStore: store,
		IDGenerator: func() (ID, error) { return "intent-ctx-cancel", nil },
	})
	_, _ = trackedCtrl.ExecuteStartupReconciliation(context.Background(), ReconcilerConfig{IntentStore: store, Backend: mgr})

	handle, err := trackedCtrl.Acquire(context.Background(), "session-8", 0, 10*time.Minute)
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel parent context immediately!

	relErr := trackedCtrl.Release(canceledCtx, handle, ReasonReleasedByOwner)
	if relErr != nil {
		t.Fatalf("expected release to succeed using detached cleanup context, got %v", relErr)
	}

	in, _ := store.GetIntent(context.Background(), "intent-ctx-cancel")
	if in.State != IntentStateTerminal {
		t.Errorf("expected intent state TERMINAL after detached cleanup, got %s", in.State)
	}
}

// 9. Second ExecuteStartupReconciliation call is deterministically rejected with ErrStartupGateAlreadyResolved.
func TestGate_SecondExecutionRejected(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	store := NewInMemoryIntentStore()

	tb := NewTunerBinding(mgr)
	baseCtrl := NewTunerBindingController(tb)
	trackedCtrl, _ := NewIntentTrackedTunerLeaseController(baseCtrl, store)

	_, err := trackedCtrl.ExecuteStartupReconciliation(context.Background(), ReconcilerConfig{IntentStore: store, Backend: mgr})
	if err != nil {
		t.Fatalf("first startup reconciliation failed: %v", err)
	}

	_, err2 := trackedCtrl.ExecuteStartupReconciliation(context.Background(), ReconcilerConfig{IntentStore: store, Backend: mgr})
	if !errors.Is(err2, ErrStartupGateAlreadyResolved) {
		t.Fatalf("expected ErrStartupGateAlreadyResolved on second call, got %v", err2)
	}
}

// 10. Bootstrap reconciler and production controller see exact same backend (ObservableSessionStoreLeaseBackend).
func TestBackendIdentity_ReconcilerAndProductionControllerShareBackend(t *testing.T) {
	memStore := sessionstore.NewMemoryStore()
	ctx := context.Background()

	// Seed memStore directly with tuner:0 lease
	_, ok, err := memStore.TryAcquireLease(ctx, "tuner:0", "existing-backend-owner", 10*time.Minute)
	if err != nil || !ok {
		t.Fatalf("failed to seed memStore lease: %v", err)
	}

	sessionStoreController := NewSessionStoreTunerLeaseController(memStore)
	intentStore := NewInMemoryIntentStore()

	trackedCtrl, _ := NewIntentTrackedTunerLeaseControllerWithConfig(IntentTrackedControllerConfig{
		Controller:    sessionStoreController,
		IntentStore:   intentStore,
		StartupPolicy: DefaultStartupPolicy{AllowAuditOnlyOrphans: true},
	})

	report, err := trackedCtrl.ExecuteStartupReconciliation(ctx, ReconcilerConfig{
		IntentStore: intentStore,
		Backend:     SessionStoreObservableBackend{SessionStoreTunerLeaseController: sessionStoreController},
	})
	if err != nil {
		t.Fatalf("reconciliation failed: %v", err)
	}

	if report.Summary.Orphaned != 1 {
		t.Errorf("expected reconciler to observe 1 orphaned lease in shared v3Store backend, got %d", report.Summary.Orphaned)
	}
}

// 11. 20 parallel ExecuteStartupReconciliation calls: exactly 1 executes reconciliation, 19 return ErrStartupGateAlreadyResolved.
func TestGate_ParallelExecution20(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	store := NewInMemoryIntentStore()

	tb := NewTunerBinding(mgr)
	baseCtrl := NewTunerBindingController(tb)
	trackedCtrl, _ := NewIntentTrackedTunerLeaseController(baseCtrl, store)

	var wg sync.WaitGroup
	var resolvedCount atomic.Int32
	var successCount atomic.Int32

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := trackedCtrl.ExecuteStartupReconciliation(context.Background(), ReconcilerConfig{IntentStore: store, Backend: mgr})
			if err == nil {
				successCount.Add(1)
			} else if errors.Is(err, ErrStartupGateAlreadyResolved) {
				resolvedCount.Add(1)
			}
		}()
	}
	wg.Wait()

	if successCount.Load() != 1 {
		t.Errorf("expected exactly 1 successful reconciliation, got %d", successCount.Load())
	}
	if resolvedCount.Load() != 19 {
		t.Errorf("expected exactly 19 ErrStartupGateAlreadyResolved responses, got %d", resolvedCount.Load())
	}
}

// 12. Gate state can never transition from GateAccepted to GateRefused.
func TestGate_StateNeverTransitionsAcceptedToRefused(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	store := NewInMemoryIntentStore()

	tb := NewTunerBinding(mgr)
	baseCtrl := NewTunerBindingController(tb)
	trackedCtrl, _ := NewIntentTrackedTunerLeaseController(baseCtrl, store)

	_, err := trackedCtrl.ExecuteStartupReconciliation(context.Background(), ReconcilerConfig{IntentStore: store, Backend: mgr})
	if err != nil {
		t.Fatalf("first startup reconciliation failed: %v", err)
	}

	if trackedCtrl.GateState() != GateAccepted {
		t.Fatalf("expected GateState GateAccepted, got %s", trackedCtrl.GateState())
	}

	// Attempt second reconciliation call which fails
	_, err2 := trackedCtrl.ExecuteStartupReconciliation(context.Background(), ReconcilerConfig{IntentStore: store, Backend: nil})
	if !errors.Is(err2, ErrStartupGateAlreadyResolved) {
		t.Fatalf("expected ErrStartupGateAlreadyResolved, got %v", err2)
	}

	if trackedCtrl.GateState() != GateAccepted {
		t.Errorf("expected GateState to remain GateAccepted, but transitioned to %s", trackedCtrl.GateState())
	}
}

// 13. Release lookup error aborts immediately and does NOT mutate backend.
func TestSaga_Release_LookupFails_BackendNotMutated(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	tb := NewTunerBinding(mgr)
	baseCtrl := NewTunerBindingController(tb)
	spy := &spyBackendController{TunerLeaseController: baseCtrl}

	store := newMockFailIntentStore()
	trackedCtrl, _ := NewIntentTrackedTunerLeaseController(spy, store)
	_, _ = trackedCtrl.ExecuteStartupReconciliation(context.Background(), ReconcilerConfig{IntentStore: store, Backend: mgr})

	handle, err := trackedCtrl.Acquire(context.Background(), "session-lookup-fail", 0, 10*time.Minute)
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	// Set ListIntents to fail
	store.failList = errors.New("simulated ListIntents lookup error")

	relErr := trackedCtrl.Release(context.Background(), handle, ReasonReleasedByOwner)
	if relErr == nil {
		t.Fatalf("expected error from Release when lookup fails, got nil")
	}

	if spy.releaseCalledCount > 0 {
		t.Errorf("backend Release MUST NOT be called when intent lookup fails")
	}
}

// 14. Release without active intent aborts immediately and does NOT mutate backend.
func TestSaga_Release_NoActiveIntent_BackendNotMutated(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	tb := NewTunerBinding(mgr)
	baseCtrl := NewTunerBindingController(tb)
	spy := &spyBackendController{TunerLeaseController: baseCtrl}

	store := NewInMemoryIntentStore()
	trackedCtrl, _ := NewIntentTrackedTunerLeaseController(spy, store)
	_, _ = trackedCtrl.ExecuteStartupReconciliation(context.Background(), ReconcilerConfig{IntentStore: store, Backend: mgr})

	fakeHandle := &TunerLeaseHandle{
		LeaseID: "non-existent-lease-id",
		Scope:   ScopeForTunerSlot(0),
		Owner:   "owner-unknown",
	}

	relErr := trackedCtrl.Release(context.Background(), fakeHandle, ReasonReleasedByOwner)
	if !errors.Is(relErr, ErrTrackedIntentNotFound) {
		t.Fatalf("expected ErrTrackedIntentNotFound, got %v", relErr)
	}

	if spy.releaseCalledCount > 0 {
		t.Errorf("backend Release MUST NOT be called when no active intent is found")
	}
}

// 15. Renew without active intent returns ErrTrackedIntentNotFound.
func TestSaga_Renew_NoActiveIntent_ReturnsErrTrackedIntentNotFound(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	tb := NewTunerBinding(mgr)
	baseCtrl := NewTunerBindingController(tb)

	store := NewInMemoryIntentStore()
	trackedCtrl, _ := NewIntentTrackedTunerLeaseController(baseCtrl, store)
	_, _ = trackedCtrl.ExecuteStartupReconciliation(context.Background(), ReconcilerConfig{IntentStore: store, Backend: mgr})

	fakeHandle := &TunerLeaseHandle{
		LeaseID: "non-existent-lease-id",
		Scope:   ScopeForTunerSlot(0),
		Owner:   "owner-unknown",
	}

	renewErr := trackedCtrl.Renew(context.Background(), fakeHandle, 10*time.Minute)
	if !errors.Is(renewErr, ErrTrackedIntentNotFound) {
		t.Fatalf("expected ErrTrackedIntentNotFound, got %v", renewErr)
	}
}

// 16. Acquire returns nil handle without error -> returns ErrInvalidBackendResult + ErrReconciliationRequired and marks intent RECOVERY_REQUIRED.
func TestSaga_Acquire_NilHandleWithoutError_IntentRecoveryRequired(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	tb := NewTunerBinding(mgr)
	baseCtrl := NewTunerBindingController(tb)
	spy := &spyBackendController{
		TunerLeaseController: baseCtrl,
		acquireNilHandle:     true,
	}

	store := NewInMemoryIntentStore()
	trackedCtrl, _ := NewIntentTrackedTunerLeaseController(spy, store)
	_, _ = trackedCtrl.ExecuteStartupReconciliation(context.Background(), ReconcilerConfig{IntentStore: store, Backend: mgr})

	h, err := trackedCtrl.Acquire(context.Background(), "session-nil-handle", 0, 10*time.Minute)
	if !errors.Is(err, ErrInvalidBackendResult) {
		t.Fatalf("expected ErrInvalidBackendResult, got %v", err)
	}
	if !errors.Is(err, ErrReconciliationRequired) {
		t.Fatalf("expected ErrReconciliationRequired, got %v", err)
	}
	if h != nil {
		t.Fatalf("expected nil handle, got %v", h)
	}

	intents, _ := store.ListIntents(context.Background())
	if len(intents) != 1 {
		t.Fatalf("expected 1 intent, got %d", len(intents))
	}
	if intents[0].State != IntentStateRecoveryRequired {
		t.Errorf("expected intent state RECOVERY_REQUIRED, got %s", intents[0].State)
	}
}

// 16b. Acquire returns nil handle + RECOVERY_REQUIRED save fails -> all errors transparently preserved in errors.Join.
func TestSaga_Acquire_NilHandleWithoutError_RecoverySaveFails(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	tb := NewTunerBinding(mgr)
	baseCtrl := NewTunerBindingController(tb)
	spy := &spyBackendController{
		TunerLeaseController: baseCtrl,
		acquireNilHandle:     true,
	}

	mockStore := newMockFailIntentStore()
	mockStore.failSaveStates[IntentStateRecoveryRequired] = errors.New("simulated RECOVERY_REQUIRED save failure")

	trackedCtrl, _ := NewIntentTrackedTunerLeaseController(spy, mockStore)
	_, _ = trackedCtrl.ExecuteStartupReconciliation(context.Background(), ReconcilerConfig{IntentStore: mockStore, Backend: mgr})

	h, err := trackedCtrl.Acquire(context.Background(), "session-nil-handle-fail-save", 0, 10*time.Minute)
	if !errors.Is(err, ErrInvalidBackendResult) {
		t.Fatalf("expected ErrInvalidBackendResult, got %v", err)
	}
	if !errors.Is(err, ErrReconciliationRequired) {
		t.Fatalf("expected ErrReconciliationRequired, got %v", err)
	}
	if !strings.Contains(err.Error(), "simulated RECOVERY_REQUIRED save failure") {
		t.Errorf("expected save error to be preserved in error chain, got %v", err)
	}
	if h != nil {
		t.Fatalf("expected nil handle, got %v", h)
	}
}

// 17. Nil observable backend returns ErrBindingUnavailable and refuses startup.
func TestBackend_NilObservableBackend_ReturnsErrBindingUnavailable(t *testing.T) {
	nilBackend := SessionStoreObservableBackend{SessionStoreTunerLeaseController: nil}
	ctx := context.Background()

	_, err := nilBackend.ListLeases(ctx)
	if !errors.Is(err, ErrBindingUnavailable) {
		t.Errorf("expected ErrBindingUnavailable from nil ListLeases, got %v", err)
	}

	store := NewInMemoryIntentStore()
	trackedCtrl, _ := NewIntentTrackedTunerLeaseController(NewSessionStoreTunerLeaseController(sessionstore.NewMemoryStore()), store)

	_, err = trackedCtrl.ExecuteStartupReconciliation(ctx, ReconcilerConfig{
		IntentStore: store,
		Backend:     nilBackend,
	})
	if err == nil {
		t.Fatalf("expected startup reconciliation failure with nil observable backend")
	}
}

// 18. Observable Acquire does not invent synthetic AcquiredAt timestamp.
func TestBackend_ObservableAcquire_DoesNotInventAcquiredAt(t *testing.T) {
	memStore := sessionstore.NewMemoryStore()
	ctrl := NewSessionStoreTunerLeaseController(memStore)
	obs := SessionStoreObservableBackend{SessionStoreTunerLeaseController: ctrl}

	l, err := obs.Acquire(context.Background(), "owner-1", "tuner:0", 10*time.Minute)
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	if !l.AcquiredAt.IsZero() {
		t.Errorf("expected AcquiredAt to remain zero value (un-synthesized), got %v", l.AcquiredAt)
	}
}

// 19. Observable Renew does not invent or overwrite synthetic AcquiredAt timestamp.
func TestBackend_ObservableRenew_DoesNotInventAcquiredAt(t *testing.T) {
	memStore := sessionstore.NewMemoryStore()
	ctrl := NewSessionStoreTunerLeaseController(memStore)
	obs := SessionStoreObservableBackend{SessionStoreTunerLeaseController: ctrl}

	_, err := obs.Acquire(context.Background(), "owner-1", "tuner:0", 10*time.Minute)
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	renewed, err := obs.Renew(context.Background(), "tuner:0", "owner-1", 10*time.Minute)
	if err != nil {
		t.Fatalf("Renew failed: %v", err)
	}

	if !renewed.AcquiredAt.IsZero() {
		t.Errorf("expected Renew AcquiredAt to remain zero value (un-synthesized), got %v", renewed.AcquiredAt)
	}
}
