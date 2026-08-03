package policy

import (
	"context"
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

	cfg := Config{Mode: PreemptionModeAuditOnly}
	evaluator := NewAuditEvaluator(cfg, &mockClock{fixed: now}, builder, engine, &mockIDGen{id: "evt-prod-123"}, auditLogger, zerolog.Nop())

	// 3. Execute AuditEvaluator on conflict
	req := ConflictAuditRequest{
		RequestID:    "sess-prod-777",
		Consumer:     domainPolicy.ConsumerScheduledRecording,
		ResourceKind: domainPolicy.ResourceTuner,
		Owner:        "new-recording-job",
		ScopeMode:    domainPolicy.ScopeSelectionAnyCompatible,
	}

	auditErr := evaluator.AuditTunerConflict(ctx, req)
	if auditErr != nil {
		t.Fatalf("unexpected audit error: %v", auditErr)
	}

	// 4. Assert Audit Event content
	if len(auditLogger.events) != 1 {
		t.Fatalf("expected 1 audit event, got %d", len(auditLogger.events))
	}
	event := auditLogger.events[0]

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
	evaluator := NewAuditEvaluator(cfg, &mockClock{fixed: time.Now()}, nil, nil, &mockIDGen{}, auditLogger, zerolog.Nop())

	req := ConflictAuditRequest{
		RequestID:    "sess-disabled",
		Consumer:     domainPolicy.ConsumerLiveTV,
		ResourceKind: domainPolicy.ResourceTuner,
		Owner:        "user-2",
		ScopeMode:    domainPolicy.ScopeSelectionAnyCompatible,
	}

	err := evaluator.AuditTunerConflict(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(auditLogger.events) != 0 {
		t.Errorf("expected 0 audit events emitted in disabled mode, got %d", len(auditLogger.events))
	}
}
