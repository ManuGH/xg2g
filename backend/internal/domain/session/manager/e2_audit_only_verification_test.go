// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package manager

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	domainPolicy "github.com/ManuGH/xg2g/internal/domain/policy"
	"github.com/ManuGH/xg2g/internal/domain/session/lifecycle"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/store"
	pipelineLease "github.com/ManuGH/xg2g/internal/pipeline/lease"
	pipelinePolicy "github.com/ManuGH/xg2g/internal/pipeline/policy"
	"github.com/rs/zerolog"
)

func defaultKeyFunc(e model.StartSessionEvent) string {
	return "dedup:" + e.SessionID
}

// PanicSpyAuditor panics if any audit method is called (used to verify zero calls on success/non-conflict paths).
type PanicSpyAuditor struct{}

func (p *PanicSpyAuditor) AuditTunerConflict(ctx context.Context, req pipelinePolicy.ConflictAuditRequest) error {
	panic("PanicSpyAuditor: AuditTunerConflict must NOT be called on this path")
}

// MockClock provides a deterministic fixed timestamp.
type MockClock struct {
	FixedTime time.Time
}

func (c *MockClock) Now() time.Time {
	return c.FixedTime
}

// MockIDGenerator provides a deterministic event ID.
type MockIDGenerator struct {
	ID  string
	Err error
}

func (g *MockIDGenerator) NewID() (string, error) {
	return g.ID, g.Err
}

// MockAuditLogger captures emitted audit events.
type MockAuditLogger struct {
	mu     sync.Mutex
	Events []pipelinePolicy.EvaluationAuditEvent
	Err    error
}

func (l *MockAuditLogger) Emit(ctx context.Context, event pipelinePolicy.EvaluationAuditEvent) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.Err != nil {
		return l.Err
	}
	l.Events = append(l.Events, event)
	return nil
}

// TestE2_01_FreeTuner_ZeroPolicyCalls verifies that acquiring a free tuner slot executes zero policy evaluations.
func TestE2_01_FreeTuner_ZeroPolicyCalls(t *testing.T) {
	memStore := store.NewMemoryStore()
	orch := &Orchestrator{
		Store:           memStore,
		Owner:           "orch-1",
		LeaseTTL:        5 * time.Minute,
		TunerSlots:      []int{0, 1},
		LeaseKeyFunc:    defaultKeyFunc,
		ConflictAuditor: &PanicSpyAuditor{}, // Panic spy verifies 0 calls
	}

	ctx := context.Background()
	slot, leaseObj, handle, err := orch.acquireTunerLease(ctx, orch.TunerSlots, "session-free")
	if err != nil {
		t.Fatalf("expected successful acquire, got err: %v", err)
	}
	if slot != 0 || leaseObj == nil || handle == nil {
		t.Fatalf("expected slot 0, non-nil lease and handle, got slot=%d", slot)
	}
}

// TestE2_02_NonConflictError_ZeroPolicyCalls verifies that technical errors during acquire return immediately without policy evaluation.
func TestE2_02_NonConflictError_ZeroPolicyCalls(t *testing.T) {
	techErr := errors.New("database connection lost")
	failingCtrl := &mockFailingTunerController{ErrToReturn: techErr}

	orch := &Orchestrator{
		Store:                store.NewMemoryStore(),
		Owner:                "orch-1",
		LeaseTTL:             5 * time.Minute,
		TunerSlots:           []int{0, 1},
		LeaseKeyFunc:         defaultKeyFunc,
		TunerLeaseController: failingCtrl,
		ConflictAuditor:      &PanicSpyAuditor{}, // Panic spy verifies 0 calls
	}

	ctx := context.Background()
	_, _, _, err := orch.acquireTunerLease(ctx, orch.TunerSlots, "session-tech-err")
	if !errors.Is(err, techErr) {
		t.Fatalf("expected techErr %v, got %v", techErr, err)
	}
}

// TestE2_03_AllSlotsConflict_InvokesOneAudit verifies that genuine scope conflicts invoke exactly 1 audit evaluation.
func TestE2_03_AllSlotsConflict_InvokesOneAudit(t *testing.T) {
	memStore := store.NewMemoryStore()
	controller := pipelineLease.NewSessionStoreTunerLeaseController(memStore)
	ctx := context.Background()

	// Occupy both tuner slots
	h0, err := controller.Acquire(ctx, "owner-0", 0, 10*time.Minute)
	if err != nil {
		t.Fatalf("failed setup acquire 0: %v", err)
	}
	h1, err := controller.Acquire(ctx, "owner-1", 1, 10*time.Minute)
	if err != nil {
		t.Fatalf("failed setup acquire 1: %v", err)
	}
	defer func() {
		_ = controller.Release(ctx, h0, pipelineLease.ReasonReleasedByOwner)
		_ = controller.Release(ctx, h1, pipelineLease.ReasonReleasedByOwner)
	}()

	callCount := 0
	auditor := &mockCountingAuditor{
		OnAudit: func(req pipelinePolicy.ConflictAuditRequest) error {
			callCount++
			if req.Consumer != domainPolicy.ConsumerLiveTV {
				t.Errorf("expected ConsumerLiveTV, got %v", req.Consumer)
			}
			return nil
		},
	}

	orch := &Orchestrator{
		Store:                memStore,
		Owner:                "orch-1",
		LeaseTTL:             5 * time.Minute,
		TunerSlots:           []int{0, 1},
		LeaseKeyFunc:         defaultKeyFunc,
		TunerLeaseController: controller,
		ConflictAuditor:      auditor,
	}

	sessionCtx := &sessionContext{Mode: model.ModeLive}
	event := model.StartSessionEvent{SessionID: "session-conflict"}

	_, gotErr := orch.acquireLeases(ctx, sessionCtx, event, "owner-conflict", zerolog.Nop())
	if !errors.Is(gotErr, pipelineLease.ErrScopeConflict) {
		t.Fatalf("expected ErrScopeConflict, got %v", gotErr)
	}
	if callCount != 1 {
		t.Fatalf("expected exactly 1 audit call, got %d", callCount)
	}
}

// TestE2_04_ConflictThenTechnicalError verifies that if slot 0 is conflict and slot 1 is a technical error, the technical error is returned with zero policy calls.
func TestE2_04_ConflictThenTechnicalError(t *testing.T) {
	techErr := errors.New("tuner:1 hardware error")
	ctrl := &mockMixedSlotController{
		SlotErrors: map[int]error{
			0: pipelineLease.ErrScopeConflict,
			1: techErr,
		},
	}

	orch := &Orchestrator{
		Store:                store.NewMemoryStore(),
		Owner:                "orch-1",
		LeaseTTL:             5 * time.Minute,
		TunerSlots:           []int{0, 1},
		LeaseKeyFunc:         defaultKeyFunc,
		TunerLeaseController: ctrl,
		ConflictAuditor:      &PanicSpyAuditor{}, // Panic spy verifies 0 calls
	}

	ctx := context.Background()
	_, _, _, err := orch.acquireTunerLease(ctx, orch.TunerSlots, "session-mixed")
	if !errors.Is(err, techErr) {
		t.Fatalf("expected technical error %v, got %v", techErr, err)
	}
}

// TestE2_05_ConflictThenSuccess verifies that if slot 0 is conflict and slot 1 succeeds, slot 1 is returned with zero policy calls.
func TestE2_05_ConflictThenSuccess(t *testing.T) {
	ctrl := &mockMixedSlotController{
		SlotErrors: map[int]error{
			0: pipelineLease.ErrScopeConflict,
			1: nil, // Success
		},
	}

	ctx := context.Background()
	memStore := store.NewMemoryStore()
	_, _, _ = memStore.TryAcquireLease(ctx, "tuner:1", "session-mixed-success", 5*time.Minute)

	orch := &Orchestrator{
		Store:                memStore,
		Owner:                "orch-1",
		LeaseTTL:             5 * time.Minute,
		TunerSlots:           []int{0, 1},
		LeaseKeyFunc:         defaultKeyFunc,
		TunerLeaseController: ctrl,
		ConflictAuditor:      &PanicSpyAuditor{}, // Panic spy verifies 0 calls
	}
	slot, _, handle, err := orch.acquireTunerLease(ctx, orch.TunerSlots, "session-mixed-success")
	if err != nil {
		t.Fatalf("expected success on slot 1, got err: %v", err)
	}
	if slot != 1 || handle == nil {
		t.Fatalf("expected slot 1, got slot %d", slot)
	}
}

// TestE2_06_PublicConflictErrorPreservesReasonAndCause verifies that acquireLeases constructs a public conflict error with reason RLeaseBusy and preserves underlying ErrScopeConflict cause.
func TestE2_06_PublicConflictErrorPreservesReasonAndCause(t *testing.T) {
	memStore := store.NewMemoryStore()
	controller := pipelineLease.NewSessionStoreTunerLeaseController(memStore)
	ctx := context.Background()

	// Occupy slot 0
	h0, err := controller.Acquire(ctx, "owner-0", 0, 10*time.Minute)
	if err != nil {
		t.Fatalf("failed setup acquire 0: %v", err)
	}
	defer func() { _ = controller.Release(ctx, h0, pipelineLease.ReasonReleasedByOwner) }()

	auditor := &mockCountingAuditor{
		OnAudit: func(req pipelinePolicy.ConflictAuditRequest) error {
			return nil
		},
	}

	orch := &Orchestrator{
		Store:                memStore,
		Owner:                "orch-1",
		LeaseTTL:             5 * time.Minute,
		TunerSlots:           []int{0},
		LeaseKeyFunc:         defaultKeyFunc,
		TunerLeaseController: controller,
		ConflictAuditor:      auditor,
	}

	sessionCtx := &sessionContext{Mode: model.ModeLive}
	event := model.StartSessionEvent{SessionID: "session-identity-test"}

	_, gotErr := orch.acquireLeases(ctx, sessionCtx, event, "owner-identity-test", zerolog.Nop())
	if gotErr == nil {
		t.Fatalf("expected error from acquireLeases, got nil")
	}

	// Assert gotErr preserves underlying ErrScopeConflict identity
	if !errors.Is(gotErr, pipelineLease.ErrScopeConflict) {
		t.Fatalf("expected errors.Is(gotErr, ErrScopeConflict) to be true, got %v", gotErr)
	}

	// Assert gotErr is a ReasonError with RLeaseBusy
	reason, _, _, ok := lifecycle.ReasonFromError(gotErr)
	if !ok || reason != model.RLeaseBusy {
		t.Fatalf("expected reason code RLeaseBusy, got ok=%v, reason=%v", ok, reason)
	}
}

// TestE2_07_ZeroSideEffects verifies that audit evaluation produces ZERO lease releases, ZERO session stops, and ZERO state mutations.
func TestE2_07_ZeroSideEffects(t *testing.T) {
	memStore := store.NewMemoryStore()
	controller := pipelineLease.NewSessionStoreTunerLeaseController(memStore)
	ctx := context.Background()

	// Active session 1 holds slot 0
	h0, err := controller.Acquire(ctx, "session-1", 0, 10*time.Minute)
	if err != nil {
		t.Fatalf("failed setup acquire 0: %v", err)
	}
	defer func() { _ = controller.Release(ctx, h0, pipelineLease.ReasonReleasedByOwner) }()

	sessRecord := &model.SessionRecord{
		SessionID: "session-1",
		State:     model.SessionReady,
	}
	if err := memStore.PutSession(ctx, sessRecord); err != nil {
		t.Fatalf("failed setup save session: %v", err)
	}

	// Policy Engine returns PREEMPTION_REQUIRED
	mockEngine := domainPolicy.NewPolicyEngine()
	logger := &MockAuditLogger{}
	clock := &MockClock{FixedTime: time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)}
	idGen := &MockIDGenerator{ID: "audit-evt-123"}

	candProv := pipelinePolicy.NewTunerCandidateProvider(1, &mockAllocProvider{
		Allocs: []pipelinePolicy.AllocationMetadata{
			{AllocationID: "tuner:0", Consumer: domainPolicy.ConsumerLiveTV, Owner: "session-1", Scope: "tuner:0", AcquiredAt: clock.Now()},
		},
	})
	snapBuilder := pipelinePolicy.NewSnapshotBuilder(candProv, &mockAllocProvider{
		Allocs: []pipelinePolicy.AllocationMetadata{
			{AllocationID: "tuner:0", Consumer: domainPolicy.ConsumerLiveTV, Owner: "session-1", Scope: "tuner:0", AcquiredAt: clock.Now()},
		},
	})

	evaluator := pipelinePolicy.NewAuditEvaluator(
		pipelinePolicy.Config{Mode: pipelinePolicy.PreemptionModeAuditOnly},
		clock,
		snapBuilder,
		mockEngine,
		idGen,
		logger,
		zerolog.Nop(),
	)

	orch := &Orchestrator{
		Store:                memStore,
		Owner:                "orch-1",
		LeaseTTL:             5 * time.Minute,
		TunerSlots:           []int{0},
		LeaseKeyFunc:         defaultKeyFunc,
		TunerLeaseController: controller,
		ConflictAuditor:      evaluator,
	}

	sessionCtx := &sessionContext{Mode: model.ModeLive}
	event := model.StartSessionEvent{SessionID: "session-2"}

	// Conflicting session 2 attempts acquire
	_, gotErr := orch.acquireLeases(ctx, sessionCtx, event, "session-2", zerolog.Nop())
	if gotErr == nil {
		t.Fatalf("expected error for session 2, got nil")
	}

	// Verify session 1 is STILL in SessionReady
	s1Check, err := memStore.GetSession(ctx, "session-1")
	if err != nil || s1Check == nil {
		t.Fatalf("failed to retrieve session 1: %v", err)
	}
	if s1Check.State != model.SessionReady {
		t.Fatalf("session 1 state mutated to %v, expected SessionReady", s1Check.State)
	}

	// Verify tuner lease for slot 0 is STILL held by session-1
	lCheck, ok, err := memStore.GetLease(ctx, "tuner:0")
	if err != nil || !ok || lCheck.Owner() != "session-1" {
		t.Fatalf("tuner:0 lease mutated or lost: ok=%v, owner=%v", ok, lCheck.Owner())
	}
}

// TestE2_08_SnapshotFailure verifies that a snapshot build error emits a failure audit event while preserving original public error.
func TestE2_08_SnapshotFailure(t *testing.T) {
	memStore := store.NewMemoryStore()
	controller := pipelineLease.NewSessionStoreTunerLeaseController(memStore)
	ctx := context.Background()

	h0, _ := controller.Acquire(ctx, "owner-0", 0, 10*time.Minute)
	defer func() { _ = controller.Release(ctx, h0, pipelineLease.ReasonReleasedByOwner) }()

	logger := &MockAuditLogger{}
	clock := &MockClock{FixedTime: time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)}
	idGen := &MockIDGenerator{ID: "audit-fail-123"}

	// Failing snapshot builder
	failingBuilder := &mockFailingSnapshotBuilder{ErrToReturn: errors.New("snapshot DB unavailable")}

	evaluator := pipelinePolicy.NewAuditEvaluator(
		pipelinePolicy.Config{Mode: pipelinePolicy.PreemptionModeAuditOnly},
		clock,
		failingBuilder,
		domainPolicy.NewPolicyEngine(),
		idGen,
		logger,
		zerolog.Nop(),
	)

	orch := &Orchestrator{
		Store:                memStore,
		Owner:                "orch-1",
		LeaseTTL:             5 * time.Minute,
		TunerSlots:           []int{0},
		LeaseKeyFunc:         defaultKeyFunc,
		TunerLeaseController: controller,
		ConflictAuditor:      evaluator,
	}

	sessionCtx := &sessionContext{Mode: model.ModeLive}
	event := model.StartSessionEvent{SessionID: "session-snap-fail"}

	_, gotErr := orch.acquireLeases(ctx, sessionCtx, event, "owner-snap-fail", zerolog.Nop())
	if !errors.Is(gotErr, pipelineLease.ErrScopeConflict) {
		t.Fatalf("expected ErrScopeConflict, got %v", gotErr)
	}

	// Verify failure audit event was emitted
	if len(logger.Events) != 1 {
		t.Fatalf("expected 1 audit failure event, got %d", len(logger.Events))
	}
	evt := logger.Events[0]
	if evt.EvaluationSucceeded {
		t.Fatalf("expected EvaluationSucceeded == false")
	}
	if evt.FailureStage != "SNAPSHOT_BUILD" {
		t.Fatalf("expected FailureStage SNAPSHOT_BUILD, got %s", evt.FailureStage)
	}
	if evt.ErrorCode != "POLICY_EVALUATION_FAILED" {
		t.Fatalf("expected ErrorCode POLICY_EVALUATION_FAILED, got %s", evt.ErrorCode)
	}
}

// TestE2_09_DisabledMode verifies that mode "disabled" produces zero evaluations and zero audit events.
func TestE2_09_DisabledMode(t *testing.T) {
	logger := &MockAuditLogger{}
	clock := &MockClock{FixedTime: time.Now()}
	idGen := &MockIDGenerator{ID: "evt-disabled"}

	evaluator := pipelinePolicy.NewAuditEvaluator(
		pipelinePolicy.Config{Mode: pipelinePolicy.PreemptionModeDisabled},
		clock,
		&PanicSpySnapshotBuilder{}, // Panic spy verifies 0 snapshot calls
		domainPolicy.NewPolicyEngine(),
		idGen,
		logger,
		zerolog.Nop(),
	)

	ctx := context.Background()
	req := pipelinePolicy.ConflictAuditRequest{
		RequestID:    "req-disabled",
		Consumer:     domainPolicy.ConsumerLiveTV,
		ResourceKind: domainPolicy.ResourceTuner,
		Owner:        "owner-disabled",
		ScopeMode:    domainPolicy.ScopeSelectionAnyCompatible,
	}

	err := evaluator.AuditTunerConflict(ctx, req)
	if err != nil {
		t.Fatalf("expected nil error in disabled mode, got %v", err)
	}
	if len(logger.Events) != 0 {
		t.Fatalf("expected 0 audit events in disabled mode, got %d", len(logger.Events))
	}
}

// TestE2_10_AuditOnlyMode verifies that mode "audit-only" emits an audit event while existing session state remains unchanged.
func TestE2_10_AuditOnlyMode(t *testing.T) {
	memStore := store.NewMemoryStore()
	controller := pipelineLease.NewSessionStoreTunerLeaseController(memStore)
	ctx := context.Background()

	h0, _ := controller.Acquire(ctx, "active-owner", 0, 10*time.Minute)
	defer func() { _ = controller.Release(ctx, h0, pipelineLease.ReasonReleasedByOwner) }()

	logger := &MockAuditLogger{}
	clock := &MockClock{FixedTime: time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)}
	idGen := &MockIDGenerator{ID: "audit-success-123"}

	allocProv := &mockAllocProvider{
		Allocs: []pipelinePolicy.AllocationMetadata{
			{AllocationID: "tuner:0", Consumer: domainPolicy.ConsumerLiveTV, Owner: "active-owner", Scope: "tuner:0", AcquiredAt: clock.Now()},
		},
	}
	candProv := pipelinePolicy.NewTunerCandidateProvider(1, allocProv)
	snapBuilder := pipelinePolicy.NewSnapshotBuilder(candProv, allocProv)

	evaluator := pipelinePolicy.NewAuditEvaluator(
		pipelinePolicy.Config{Mode: pipelinePolicy.PreemptionModeAuditOnly},
		clock,
		snapBuilder,
		domainPolicy.NewPolicyEngine(),
		idGen,
		logger,
		zerolog.Nop(),
	)

	req := pipelinePolicy.ConflictAuditRequest{
		RequestID:    "req-audit-only",
		Consumer:     domainPolicy.ConsumerLiveTV,
		ResourceKind: domainPolicy.ResourceTuner,
		Owner:        "req-owner",
		ScopeMode:    domainPolicy.ScopeSelectionAnyCompatible,
	}

	err := evaluator.AuditTunerConflict(ctx, req)
	if err != nil {
		t.Fatalf("expected nil error from AuditTunerConflict, got %v", err)
	}
	if len(logger.Events) != 1 {
		t.Fatalf("expected 1 audit event, got %d", len(logger.Events))
	}
	evt := logger.Events[0]
	if !evt.EvaluationSucceeded {
		t.Fatalf("expected EvaluationSucceeded == true")
	}
	if evt.EventID != "audit-success-123" {
		t.Fatalf("expected EventID audit-success-123, got %s", evt.EventID)
	}
	if evt.EnforcementMode != pipelinePolicy.PreemptionModeAuditOnly {
		t.Fatalf("expected EnforcementMode audit-only, got %s", evt.EnforcementMode)
	}
}

// TestE2_11_TechnicalAcquireError verifies that non-conflict store errors return immediately without policy evaluation.
func TestE2_11_TechnicalAcquireError(t *testing.T) {
	techErr := errors.New("disk I/O error on lease store")
	ctrl := &mockFailingTunerController{ErrToReturn: techErr}

	orch := &Orchestrator{
		Store:                store.NewMemoryStore(),
		Owner:                "orch-1",
		LeaseTTL:             5 * time.Minute,
		TunerSlots:           []int{0},
		LeaseKeyFunc:         defaultKeyFunc,
		TunerLeaseController: ctrl,
		ConflictAuditor:      &PanicSpyAuditor{}, // Panic spy verifies 0 calls
	}

	sessionCtx := &sessionContext{Mode: model.ModeLive}
	event := model.StartSessionEvent{SessionID: "session-tech"}

	ctx := context.Background()
	_, gotErr := orch.acquireLeases(ctx, sessionCtx, event, "owner-tech", zerolog.Nop())
	if !errors.Is(gotErr, techErr) {
		t.Fatalf("expected techErr %v, got %v", techErr, gotErr)
	}
}

// TestE2_12_LoggerFailure verifies that if AuditLogger.Emit fails, the public conflict error is returned unchanged.
func TestE2_12_LoggerFailure(t *testing.T) {
	memStore := store.NewMemoryStore()
	controller := pipelineLease.NewSessionStoreTunerLeaseController(memStore)
	ctx := context.Background()

	h0, _ := controller.Acquire(ctx, "owner-0", 0, 10*time.Minute)
	defer func() { _ = controller.Release(ctx, h0, pipelineLease.ReasonReleasedByOwner) }()

	failingLogger := &MockAuditLogger{Err: errors.New("syslog disk full")}
	clock := &MockClock{FixedTime: time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)}
	idGen := &MockIDGenerator{ID: "audit-logger-fail-123"}

	allocProv := &mockAllocProvider{
		Allocs: []pipelinePolicy.AllocationMetadata{
			{AllocationID: "tuner:0", Consumer: domainPolicy.ConsumerLiveTV, Owner: "owner-0", Scope: "tuner:0", AcquiredAt: clock.Now()},
		},
	}
	candProv := pipelinePolicy.NewTunerCandidateProvider(1, allocProv)
	snapBuilder := pipelinePolicy.NewSnapshotBuilder(candProv, allocProv)

	evaluator := pipelinePolicy.NewAuditEvaluator(
		pipelinePolicy.Config{Mode: pipelinePolicy.PreemptionModeAuditOnly},
		clock,
		snapBuilder,
		domainPolicy.NewPolicyEngine(),
		idGen,
		failingLogger,
		zerolog.Nop(),
	)

	orch := &Orchestrator{
		Store:                memStore,
		Owner:                "orch-1",
		LeaseTTL:             5 * time.Minute,
		TunerSlots:           []int{0},
		LeaseKeyFunc:         defaultKeyFunc,
		TunerLeaseController: controller,
		ConflictAuditor:      evaluator,
	}

	sessionCtx := &sessionContext{Mode: model.ModeLive}
	event := model.StartSessionEvent{SessionID: "session-logger-fail"}

	_, gotErr := orch.acquireLeases(ctx, sessionCtx, event, "owner-logger-fail", zerolog.Nop())
	if !errors.Is(gotErr, pipelineLease.ErrScopeConflict) {
		t.Fatalf("expected ErrScopeConflict, got %v", gotErr)
	}

	reason, _, _, ok := lifecycle.ReasonFromError(gotErr)
	if !ok || reason != model.RLeaseBusy {
		t.Fatalf("expected reason code RLeaseBusy, got ok=%v, reason=%v", ok, reason)
	}
}

// TestE2_13_DeterminismCanonicalJSONBytes verifies that repeated runs with fixed clock/ID produce 100% identical JSON bytes.
func TestE2_13_DeterminismCanonicalJSONBytes(t *testing.T) {
	clock := &MockClock{FixedTime: time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)}
	idGen := &MockIDGenerator{ID: "deterministic-id-777"}

	allocProv := &mockAllocProvider{
		Allocs: []pipelinePolicy.AllocationMetadata{
			{AllocationID: "tuner:0", Consumer: domainPolicy.ConsumerLiveTV, Owner: "owner-a", Scope: "tuner:0", AcquiredAt: clock.Now()},
		},
	}
	candProv := pipelinePolicy.NewTunerCandidateProvider(1, allocProv)
	snapBuilder := pipelinePolicy.NewSnapshotBuilder(candProv, allocProv)

	var runJSONs [][]byte
	for i := 0; i < 5; i++ {
		logger := &MockAuditLogger{}
		evaluator := pipelinePolicy.NewAuditEvaluator(
			pipelinePolicy.Config{Mode: pipelinePolicy.PreemptionModeAuditOnly},
			clock,
			snapBuilder,
			domainPolicy.NewPolicyEngine(),
			idGen,
			logger,
			zerolog.Nop(),
		)

		ctx := context.Background()
		req := pipelinePolicy.ConflictAuditRequest{
			RequestID:    "det-req-1",
			Consumer:     domainPolicy.ConsumerLiveTV,
			ResourceKind: domainPolicy.ResourceTuner,
			Owner:        "owner-b",
			ScopeMode:    domainPolicy.ScopeSelectionAnyCompatible,
		}

		err := evaluator.AuditTunerConflict(ctx, req)
		if err != nil {
			t.Fatalf("run %d failed: %v", i, err)
		}
		if len(logger.Events) != 1 {
			t.Fatalf("run %d expected 1 event, got %d", i, len(logger.Events))
		}

		b, err := json.Marshal(logger.Events[0])
		if err != nil {
			t.Fatalf("run %d json marshal failed: %v", i, err)
		}
		runJSONs = append(runJSONs, b)
	}

	for i := 1; i < len(runJSONs); i++ {
		if !bytes.Equal(runJSONs[0], runJSONs[i]) {
			t.Fatalf("JSON byte mismatch between run 0 (%s) and run %d (%s)", string(runJSONs[0]), i, string(runJSONs[i]))
		}
	}
}

// TestE2_14_ConcurrentConflicts verifies concurrent conflicting requests operate safely under -race detector without data races.
func TestE2_14_ConcurrentConflicts(t *testing.T) {
	memStore := store.NewMemoryStore()
	controller := pipelineLease.NewSessionStoreTunerLeaseController(memStore)
	ctx := context.Background()

	// Occupy tuner slot 0 with owner-occupied
	h0, err := controller.Acquire(ctx, "owner-occupied", 0, 10*time.Minute)
	if err != nil {
		t.Fatalf("failed setup acquire: %v", err)
	}
	defer func() { _ = controller.Release(ctx, h0, pipelineLease.ReasonReleasedByOwner) }()

	logger := &MockAuditLogger{}
	clock := &MockClock{FixedTime: time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)}

	var idCounter uint64
	var idMu sync.Mutex

	allocProv := &mockAllocProvider{
		Allocs: []pipelinePolicy.AllocationMetadata{
			{AllocationID: "tuner:0", Consumer: domainPolicy.ConsumerLiveTV, Owner: "owner-occupied", Scope: "tuner:0", AcquiredAt: clock.Now()},
		},
	}
	candProv := pipelinePolicy.NewTunerCandidateProvider(1, allocProv)
	snapBuilder := pipelinePolicy.NewSnapshotBuilder(candProv, allocProv)

	// Thread-safe IDGenerator
	safeIDGen := &mockSafeIDGen{
		Gen: func() (string, error) {
			idMu.Lock()
			defer idMu.Unlock()
			idCounter++
			return fmt.Sprintf("race-id-%d", idCounter), nil
		},
	}

	evaluator := pipelinePolicy.NewAuditEvaluator(
		pipelinePolicy.Config{Mode: pipelinePolicy.PreemptionModeAuditOnly},
		clock,
		snapBuilder,
		domainPolicy.NewPolicyEngine(),
		safeIDGen,
		logger,
		zerolog.Nop(),
	)

	orch := &Orchestrator{
		Store:                memStore,
		Owner:                "orch-1",
		LeaseTTL:             5 * time.Minute,
		TunerSlots:           []int{0},
		LeaseKeyFunc:         defaultKeyFunc,
		TunerLeaseController: controller,
		ConflictAuditor:      evaluator,
	}

	var wg sync.WaitGroup
	const goroutines = 10
	errs := make([]error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sessionCtx := &sessionContext{Mode: model.ModeLive}
			event := model.StartSessionEvent{SessionID: fmt.Sprintf("race-session-%d", idx)}
			_, errs[idx] = orch.acquireLeases(ctx, sessionCtx, event, fmt.Sprintf("conflicting-owner-%d", idx), zerolog.Nop())
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if !errors.Is(err, pipelineLease.ErrScopeConflict) {
			t.Fatalf("goroutine %d expected ErrScopeConflict, got %v", i, err)
		}
	}
}

// TestE2_15_BootstrapModeValidation verifies that LoadConfig validates preemption modes strictly at bootstrap.
func TestE2_15_BootstrapModeValidation(t *testing.T) {
	tests := []struct {
		envVal    string
		wantMode  pipelinePolicy.PreemptionMode
		expectErr bool
	}{
		{"", pipelinePolicy.PreemptionModeDisabled, false},
		{"disabled", pipelinePolicy.PreemptionModeDisabled, false},
		{"audit-only", pipelinePolicy.PreemptionModeAuditOnly, false},
		{"enforce", "", true},
		{"foo", "", true},
		{"  audit-only  ", pipelinePolicy.PreemptionModeAuditOnly, false},
	}

	for _, tt := range tests {
		cfg, err := pipelinePolicy.LoadConfig(tt.envVal)
		if tt.expectErr {
			if err == nil {
				t.Errorf("LoadConfig(%q) expected error, got nil", tt.envVal)
			}
			if !errors.Is(err, pipelinePolicy.ErrInvalidPreemptionMode) {
				t.Errorf("LoadConfig(%q) expected ErrInvalidPreemptionMode, got %v", tt.envVal, err)
			}
		} else {
			if err != nil {
				t.Errorf("LoadConfig(%q) unexpected error: %v", tt.envVal, err)
			}
			if cfg.Mode != tt.wantMode {
				t.Errorf("LoadConfig(%q) got mode %q, want %q", tt.envVal, cfg.Mode, tt.wantMode)
			}
		}
	}
}

// TestE2_EmptySlotList_ReturnsTypedError verifies that calling acquireTunerLease with an empty slot list returns ErrNoSlotsConfigured.
func TestE2_EmptySlotList_ReturnsTypedError(t *testing.T) {
	orch := &Orchestrator{
		Store:                store.NewMemoryStore(),
		Owner:                "orch-1",
		LeaseTTL:             5 * time.Minute,
		TunerSlots:           []int{},
		LeaseKeyFunc:         defaultKeyFunc,
		TunerLeaseController: &mockFailingTunerController{},
	}

	ctx := context.Background()
	_, _, _, err := orch.acquireTunerLease(ctx, []int{}, "owner-empty")
	if !errors.Is(err, ErrNoSlotsConfigured) {
		t.Fatalf("expected ErrNoSlotsConfigured, got %v", err)
	}
}

// TestE2_AcquireSucceededButLeaseMissing_CompensatesAndReturnsTypedError verifies that if Acquire succeeds but GetLease returns ok=false, compensation Release is called and ErrAuthoritativeLeaseMissing is returned.
func TestE2_AcquireSucceededButLeaseMissing_CompensatesAndReturnsTypedError(t *testing.T) {
	released := false
	ctrl := &mockCustomController{
		AcquireFunc: func(ctx context.Context, owner pipelineLease.Owner, slot int, ttl time.Duration) (*pipelineLease.TunerLeaseHandle, error) {
			return &pipelineLease.TunerLeaseHandle{Owner: owner, Slot: slot, Scope: "tuner:0"}, nil
		},
		ReleaseFunc: func(ctx context.Context, handle *pipelineLease.TunerLeaseHandle, reason pipelineLease.ReasonCode) error {
			released = true
			return nil
		},
	}

	emptyStore := store.NewMemoryStore() // Store has no lease for tuner:0
	orch := &Orchestrator{
		Store:                emptyStore,
		Owner:                "orch-1",
		LeaseTTL:             5 * time.Minute,
		TunerSlots:           []int{0},
		LeaseKeyFunc:         defaultKeyFunc,
		TunerLeaseController: ctrl,
	}

	ctx := context.Background()
	_, _, _, err := orch.acquireTunerLease(ctx, []int{0}, "owner-missing-lease")
	if !errors.Is(err, ErrAuthoritativeLeaseMissing) {
		t.Fatalf("expected ErrAuthoritativeLeaseMissing, got %v", err)
	}
	if !released {
		t.Fatalf("expected compensation release to be executed")
	}
}

// TestE2_AcquireSucceededLeaseReadFails_ReturnsReadAndCompensationErrors verifies that store read error and compensation release error are properly preserved.
func TestE2_AcquireSucceededLeaseReadFails_ReturnsReadAndCompensationErrors(t *testing.T) {
	readErr := errors.New("read error from DB")
	releaseErr := errors.New("release error from controller")

	ctrl := &mockCustomController{
		AcquireFunc: func(ctx context.Context, owner pipelineLease.Owner, slot int, ttl time.Duration) (*pipelineLease.TunerLeaseHandle, error) {
			return &pipelineLease.TunerLeaseHandle{Owner: owner, Slot: slot, Scope: "tuner:0"}, nil
		},
		ReleaseFunc: func(ctx context.Context, handle *pipelineLease.TunerLeaseHandle, reason pipelineLease.ReasonCode) error {
			return releaseErr
		},
	}

	failingStore := &mockFailingLeaseStore{ReadErr: readErr}
	orch := &Orchestrator{
		Store:                failingStore,
		Owner:                "orch-1",
		LeaseTTL:             5 * time.Minute,
		TunerSlots:           []int{0},
		LeaseKeyFunc:         defaultKeyFunc,
		TunerLeaseController: ctrl,
	}

	ctx := context.Background()
	_, _, _, err := orch.acquireTunerLease(ctx, []int{0}, "owner-read-fail")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !errors.Is(err, readErr) {
		t.Fatalf("expected errors.Is(err, readErr) to be true, got %v", err)
	}
	if !errors.Is(err, releaseErr) {
		t.Fatalf("expected errors.Is(err, releaseErr) to be true, got %v", err)
	}
	if !errors.Is(err, ErrLeaseReadAfterAcquire) {
		t.Fatalf("expected errors.Is(err, ErrLeaseReadAfterAcquire) to be true, got %v", err)
	}
	if !errors.Is(err, ErrCompensationReleaseFailed) {
		t.Fatalf("expected errors.Is(err, ErrCompensationReleaseFailed) to be true, got %v", err)
	}
}

// TestE2_OnlyOneConflictAuditorSurfaceExists verifies that Orchestrator contains ONLY ConflictAuditor and NO AuditEvaluator field.
func TestE2_OnlyOneConflictAuditorSurfaceExists(t *testing.T) {
	typ := reflect.TypeOf(Orchestrator{})
	_, hasConflictAuditor := typ.FieldByName("ConflictAuditor")
	if !hasConflictAuditor {
		t.Fatalf("expected Orchestrator to contain ConflictAuditor field")
	}

	_, hasAuditEvaluator := typ.FieldByName("AuditEvaluator")
	if hasAuditEvaluator {
		t.Fatalf("STRICT ARCHITECTURAL VIOLATION: Orchestrator still contains legacy AuditEvaluator field")
	}
}

// --- Helper Test Doubles ---

type mockFailingTunerController struct {
	ErrToReturn error
}

func (m *mockFailingTunerController) Acquire(ctx context.Context, owner pipelineLease.Owner, slot int, ttl time.Duration) (*pipelineLease.TunerLeaseHandle, error) {
	return nil, m.ErrToReturn
}

func (m *mockFailingTunerController) AcquireWithBoundTicket(ctx context.Context, ticket *pipelinePolicy.AdmissionTicket, sessionID, userID, profileID string, owner pipelineLease.Owner, slot int, ttl time.Duration) (*pipelineLease.TunerLeaseHandle, error) {
	return nil, m.ErrToReturn
}

func (m *mockFailingTunerController) Renew(ctx context.Context, handle *pipelineLease.TunerLeaseHandle, ttl time.Duration) error {
	return nil
}

func (m *mockFailingTunerController) Release(ctx context.Context, handle *pipelineLease.TunerLeaseHandle, reason pipelineLease.ReasonCode) error {
	return nil
}

type mockMixedSlotController struct {
	SlotErrors map[int]error
}

func (m *mockMixedSlotController) Acquire(ctx context.Context, owner pipelineLease.Owner, slot int, ttl time.Duration) (*pipelineLease.TunerLeaseHandle, error) {
	err := m.SlotErrors[slot]
	if err == nil {
		return &pipelineLease.TunerLeaseHandle{Owner: owner, Slot: slot, Scope: pipelineLease.Scope(fmt.Sprintf("tuner:%d", slot))}, nil
	}
	return nil, err
}

func (m *mockMixedSlotController) AcquireWithBoundTicket(ctx context.Context, ticket *pipelinePolicy.AdmissionTicket, sessionID, userID, profileID string, owner pipelineLease.Owner, slot int, ttl time.Duration) (*pipelineLease.TunerLeaseHandle, error) {
	return m.Acquire(ctx, owner, slot, ttl)
}

func (m *mockMixedSlotController) Renew(ctx context.Context, handle *pipelineLease.TunerLeaseHandle, ttl time.Duration) error {
	return nil
}

func (m *mockMixedSlotController) Release(ctx context.Context, handle *pipelineLease.TunerLeaseHandle, reason pipelineLease.ReasonCode) error {
	return nil
}

type mockCustomController struct {
	AcquireFunc func(ctx context.Context, owner pipelineLease.Owner, slot int, ttl time.Duration) (*pipelineLease.TunerLeaseHandle, error)
	ReleaseFunc func(ctx context.Context, handle *pipelineLease.TunerLeaseHandle, reason pipelineLease.ReasonCode) error
}

func (m *mockCustomController) Acquire(ctx context.Context, owner pipelineLease.Owner, slot int, ttl time.Duration) (*pipelineLease.TunerLeaseHandle, error) {
	if m.AcquireFunc != nil {
		return m.AcquireFunc(ctx, owner, slot, ttl)
	}
	return nil, nil
}

func (m *mockCustomController) AcquireWithBoundTicket(ctx context.Context, ticket *pipelinePolicy.AdmissionTicket, sessionID, userID, profileID string, owner pipelineLease.Owner, slot int, ttl time.Duration) (*pipelineLease.TunerLeaseHandle, error) {
	return m.Acquire(ctx, owner, slot, ttl)
}

func (m *mockCustomController) Renew(ctx context.Context, handle *pipelineLease.TunerLeaseHandle, ttl time.Duration) error {
	return nil
}

func (m *mockCustomController) Release(ctx context.Context, handle *pipelineLease.TunerLeaseHandle, reason pipelineLease.ReasonCode) error {
	if m.ReleaseFunc != nil {
		return m.ReleaseFunc(ctx, handle, reason)
	}
	return nil
}

type mockFailingLeaseStore struct {
	store.StateStore
	ReadErr error
}

func (m *mockFailingLeaseStore) GetLease(ctx context.Context, key string) (store.Lease, bool, error) {
	return nil, false, m.ReadErr
}

type mockCountingAuditor struct {
	OnAudit func(req pipelinePolicy.ConflictAuditRequest) error
}

func (m *mockCountingAuditor) AuditTunerConflict(ctx context.Context, req pipelinePolicy.ConflictAuditRequest) error {
	if m.OnAudit != nil {
		return m.OnAudit(req)
	}
	return nil
}

type mockAllocProvider struct {
	Allocs     []pipelinePolicy.AllocationMetadata
	ObservedAt time.Time
}

func (m *mockAllocProvider) ActiveAllocations(ctx context.Context, kind domainPolicy.ResourceKind) ([]pipelinePolicy.AllocationMetadata, time.Time, error) {
	if m.ObservedAt.IsZero() {
		return m.Allocs, time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC), nil
	}
	return m.Allocs, m.ObservedAt, nil
}

type mockFailingSnapshotBuilder struct {
	ErrToReturn error
}

func (m *mockFailingSnapshotBuilder) BuildSnapshot(ctx context.Context, req domainPolicy.EvaluationRequest) (domainPolicy.ResourceSnapshot, string, error) {
	return domainPolicy.ResourceSnapshot{}, "", m.ErrToReturn
}

type PanicSpySnapshotBuilder struct{}

func (p *PanicSpySnapshotBuilder) BuildSnapshot(ctx context.Context, req domainPolicy.EvaluationRequest) (domainPolicy.ResourceSnapshot, string, error) {
	panic("PanicSpySnapshotBuilder: BuildSnapshot must NOT be called on disabled path")
}

type mockSafeIDGen struct {
	Gen func() (string, error)
}

func (g *mockSafeIDGen) NewID() (string, error) {
	return g.Gen()
}
