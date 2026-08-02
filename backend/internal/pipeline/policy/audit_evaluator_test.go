package policy

import (
	"context"
	"errors"
	"testing"
	"time"

	domainPolicy "github.com/ManuGH/xg2g/internal/domain/policy"
	"github.com/rs/zerolog"
)

type mockAuditLogger struct {
	events []EvaluationAuditEvent
	err    error
}

func (m *mockAuditLogger) Emit(ctx context.Context, event EvaluationAuditEvent) error {
	if m.err != nil {
		return m.err
	}
	m.events = append(m.events, event)
	return nil
}

func TestAuditEvaluator_DisabledMode_BypassesAll(t *testing.T) {
	ctx := context.Background()
	logger := &mockAuditLogger{}

	cfg := Config{Mode: PreemptionModeDisabled}
	evaluator := NewAuditEvaluator(cfg, nil, nil, nil, logger, zerolog.Nop())

	req := domainPolicy.EvaluationRequest{
		Consumer:     domainPolicy.ConsumerLiveTV,
		ResourceKind: domainPolicy.ResourceTuner,
		Owner:        "user-1",
		ScopeMode:    domainPolicy.ScopeSelectionAnyCompatible,
		EvaluatedAt:  time.Now(),
	}

	origErr := errors.New("original conflict")
	event, err := evaluator.EvaluateConflict(ctx, "req-123", req, origErr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.EventID != "" {
		t.Errorf("expected empty event in disabled mode, got %+v", event)
	}
	if len(logger.events) != 0 {
		t.Errorf("expected 0 emitted audit events in disabled mode, got %d", len(logger.events))
	}
}

func TestAuditEvaluator_AuditOnlyMode_EmitsAuditEvent(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 2, 22, 0, 0, 0, time.UTC)
	logger := &mockAuditLogger{}

	cands := []domainPolicy.ResourceCandidate{
		{Scope: "tuner:0", Compatible: true, Available: true},
	}
	candProv := &mockCandidateProvider{cands: cands, obsAt: now}
	allocProv := &mockAllocationProvider{allocs: nil, obsAt: now}
	builder := NewSnapshotBuilder(candProv, allocProv)
	engine := domainPolicy.NewPolicyEngine()

	idGen := func() (string, error) {
		return "evt-999", nil
	}

	cfg := Config{Mode: PreemptionModeAuditOnly}
	evaluator := NewAuditEvaluator(cfg, builder, engine, idGen, logger, zerolog.Nop())

	req := domainPolicy.EvaluationRequest{
		Consumer:     domainPolicy.ConsumerLiveTV,
		ResourceKind: domainPolicy.ResourceTuner,
		Owner:        "user-1",
		ScopeMode:    domainPolicy.ScopeSelectionAnyCompatible,
		EvaluatedAt:  now,
	}

	origErr := errors.New("original conflict")
	event, err := evaluator.EvaluateConflict(ctx, "req-555", req, origErr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if event.EventID != "evt-999" {
		t.Errorf("expected EventID evt-999, got %s", event.EventID)
	}
	if event.RequestID != "req-555" {
		t.Errorf("expected RequestID req-555, got %s", event.RequestID)
	}
	if event.Decision != domainPolicy.DecisionGrant {
		t.Errorf("expected DecisionGrant, got %s", event.Decision)
	}
	if event.EnforcementMode != PreemptionModeAuditOnly {
		t.Errorf("expected enforcement mode audit-only, got %s", event.EnforcementMode)
	}
	if !event.EvaluationSucceeded {
		t.Error("expected EvaluationSucceeded == true")
	}
	if len(logger.events) != 1 {
		t.Fatalf("expected 1 emitted event, got %d", len(logger.events))
	}
}

func TestAuditEvaluator_SnapshotBuildFailure_EmitsFailureEvent(t *testing.T) {
	ctx := context.Background()
	logger := &mockAuditLogger{}

	// Failing snapshot builder
	failingCandProv := &mockCandidateProvider{err: errors.New("hardware provider down")}
	allocProv := &mockAllocationProvider{obsAt: time.Now()}
	builder := NewSnapshotBuilder(failingCandProv, allocProv)
	engine := domainPolicy.NewPolicyEngine()

	idGen := func() (string, error) {
		return "evt-fail-1", nil
	}

	cfg := Config{Mode: PreemptionModeAuditOnly}
	evaluator := NewAuditEvaluator(cfg, builder, engine, idGen, logger, zerolog.Nop())

	req := domainPolicy.EvaluationRequest{
		Consumer:     domainPolicy.ConsumerLiveTV,
		ResourceKind: domainPolicy.ResourceTuner,
		Owner:        "user-1",
		ScopeMode:    domainPolicy.ScopeSelectionAnyCompatible,
		EvaluatedAt:  time.Now(),
	}

	origErr := errors.New("original conflict")
	_, err := evaluator.EvaluateConflict(ctx, "req-fail", req, origErr)
	if !errors.Is(err, ErrSnapshotBuildFailed) {
		t.Fatalf("expected ErrSnapshotBuildFailed, got %v", err)
	}

	if len(logger.events) != 1 {
		t.Fatalf("expected 1 failure event emitted, got %d", len(logger.events))
	}
	failEvt := logger.events[0]
	if failEvt.EvaluationSucceeded {
		t.Error("expected EvaluationSucceeded == false")
	}
	if failEvt.ReasonCode != domainPolicy.ReasonCode("POLICY_EVALUATION_FAILED") {
		t.Errorf("expected ReasonCode POLICY_EVALUATION_FAILED, got %s", failEvt.ReasonCode)
	}
}
