package policy

import (
	"context"
	"errors"
	"testing"
	"time"

	domainPolicy "github.com/ManuGH/xg2g/internal/domain/policy"
	"github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/rs/zerolog"
)

func TestProductionIntegration_TunerConflictTriggersAudit_PreservesErrorIdentity(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 2, 22, 0, 0, 0, time.UTC)
	st := store.NewMemoryStore()

	// 1. Occupy tuner:0 in authoritative LeaseStore
	_, ok, err := st.TryAcquireLease(ctx, "tuner:0", "existing-owner-live", 10*time.Second)
	if err != nil || !ok {
		t.Fatalf("failed to acquire lease in store: %v", err)
	}

	// 2. Configure AuditEvaluator in audit-only mode
	meta := map[string]AllocationMetadata{
		"tuner:0": {
			AllocationID: "tuner:0",
			Consumer:     domainPolicy.ConsumerLiveTV,
			Owner:        "existing-owner-live",
			Scope:        "tuner:0",
			AcquiredAt:   now,
		},
	}
	allocProv := NewSessionStoreAllocationProvider(st, meta)
	candProv := NewTunerCandidateProvider(1, allocProv) // 1 total slot (tuner:0)
	builder := NewSnapshotBuilder(candProv, allocProv)
	engine := domainPolicy.NewPolicyEngine()
	auditLogger := &mockAuditLogger{}

	idGen := func() (string, error) {
		return "evt-prod-123", nil
	}

	cfg := Config{Mode: PreemptionModeAuditOnly}
	evaluator := NewAuditEvaluator(cfg, builder, engine, idGen, auditLogger, zerolog.Nop())

	// 3. Execute AuditEvaluator on conflict
	req := domainPolicy.EvaluationRequest{
		Consumer:     domainPolicy.ConsumerScheduledRecording,
		ResourceKind: domainPolicy.ResourceTuner,
		Owner:        "new-recording-job",
		ScopeMode:    domainPolicy.ScopeSelectionAnyCompatible,
		EvaluatedAt:  now,
	}

	origConflictErr := errors.New("authoritative lease store conflict: no slots available")
	event, auditErr := evaluator.EvaluateConflict(ctx, "sess-prod-777", req, origConflictErr)

	if auditErr != nil {
		t.Fatalf("unexpected audit error: %v", auditErr)
	}

	// 4. Assert Audit Event content
	if event.EventID != "evt-prod-123" {
		t.Errorf("expected EventID evt-prod-123, got %s", event.EventID)
	}
	if event.RequestID != "sess-prod-777" {
		t.Errorf("expected RequestID sess-prod-777, got %s", event.RequestID)
	}
	if event.Decision != domainPolicy.DecisionPreemptionRequired {
		t.Errorf("expected DecisionPreemptionRequired, got %s", event.Decision)
	}
	if event.EnforcementMode != PreemptionModeAuditOnly {
		t.Errorf("expected enforcement mode audit-only, got %s", event.EnforcementMode)
	}

	// 5. Assert authoritative lease store state remains 100% active and untouched
	lease, leaseOk, getErr := st.GetLease(ctx, "tuner:0")
	if getErr != nil || !leaseOk {
		t.Fatalf("STRICT INVARIANT VIOLATION: active lease tuner:0 was modified or released! getErr=%v", getErr)
	}
	if lease.Owner() != "existing-owner-live" {
		t.Errorf("STRICT INVARIANT VIOLATION: lease owner changed from existing-owner-live to %s", lease.Owner())
	}
}

func TestProductionIntegration_DisabledMode_LeavesBehaviorUntouched(t *testing.T) {
	ctx := context.Background()

	auditLogger := &mockAuditLogger{}
	cfg := Config{Mode: PreemptionModeDisabled}
	evaluator := NewAuditEvaluator(cfg, nil, nil, nil, auditLogger, zerolog.Nop())

	req := domainPolicy.EvaluationRequest{
		Consumer:     domainPolicy.ConsumerLiveTV,
		ResourceKind: domainPolicy.ResourceTuner,
		Owner:        "user-2",
		ScopeMode:    domainPolicy.ScopeSelectionAnyCompatible,
		EvaluatedAt:  time.Now(),
	}

	origConflictErr := errors.New("original conflict error")
	event, err := evaluator.EvaluateConflict(ctx, "sess-disabled", req, origConflictErr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if event.EventID != "" {
		t.Errorf("expected empty event in disabled mode, got %+v", event)
	}
	if len(auditLogger.events) != 0 {
		t.Errorf("expected 0 audit events emitted in disabled mode, got %d", len(auditLogger.events))
	}
}
