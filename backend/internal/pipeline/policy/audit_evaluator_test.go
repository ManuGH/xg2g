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

type mockClock struct {
	fixed time.Time
}

func (c *mockClock) Now() time.Time {
	return c.fixed
}

type mockIDGen struct {
	id  string
	err error
}

func (g *mockIDGen) NewID() (string, error) {
	return g.id, g.err
}

func TestAuditEvaluator_DisabledMode_BypassesAll(t *testing.T) {
	ctx := context.Background()
	logger := &mockAuditLogger{}

	cfg := Config{Mode: PreemptionModeDisabled}
	evaluator := NewAuditEvaluator(cfg, &mockClock{fixed: time.Now()}, nil, nil, &mockIDGen{}, logger, zerolog.Nop())

	req := ConflictAuditRequest{
		RequestID:    "req-123",
		Consumer:     domainPolicy.ConsumerLiveTV,
		ResourceKind: domainPolicy.ResourceTuner,
		Owner:        "user-1",
		ScopeMode:    domainPolicy.ScopeSelectionAnyCompatible,
	}

	err := evaluator.AuditTunerConflict(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
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

	cfg := Config{Mode: PreemptionModeAuditOnly}
	evaluator := NewAuditEvaluator(cfg, &mockClock{fixed: now}, builder, engine, &mockIDGen{id: "evt-999"}, logger, zerolog.Nop())

	req := ConflictAuditRequest{
		RequestID:    "req-555",
		Consumer:     domainPolicy.ConsumerLiveTV,
		ResourceKind: domainPolicy.ResourceTuner,
		Owner:        "user-1",
		ScopeMode:    domainPolicy.ScopeSelectionAnyCompatible,
	}

	err := evaluator.AuditTunerConflict(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(logger.events) != 1 {
		t.Fatalf("expected 1 emitted event, got %d", len(logger.events))
	}
	event := logger.events[0]

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
}

func TestAuditEvaluator_SnapshotBuildFailure_EmitsFailureEvent(t *testing.T) {
	ctx := context.Background()
	logger := &mockAuditLogger{}

	failingCandProv := &mockCandidateProvider{err: errors.New("hardware provider down")}
	allocProv := &mockAllocationProvider{obsAt: time.Now()}
	builder := NewSnapshotBuilder(failingCandProv, allocProv)
	engine := domainPolicy.NewPolicyEngine()

	cfg := Config{Mode: PreemptionModeAuditOnly}
	evaluator := NewAuditEvaluator(cfg, &mockClock{fixed: time.Now()}, builder, engine, &mockIDGen{id: "evt-fail-1"}, logger, zerolog.Nop())

	req := ConflictAuditRequest{
		RequestID:    "req-fail",
		Consumer:     domainPolicy.ConsumerLiveTV,
		ResourceKind: domainPolicy.ResourceTuner,
		Owner:        "user-1",
		ScopeMode:    domainPolicy.ScopeSelectionAnyCompatible,
	}

	err := evaluator.AuditTunerConflict(ctx, req)
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
	if failEvt.FailureStage != "SNAPSHOT_BUILD" {
		t.Errorf("expected FailureStage SNAPSHOT_BUILD, got %s", failEvt.FailureStage)
	}
	if failEvt.ErrorCode != "POLICY_EVALUATION_FAILED" {
		t.Errorf("expected ErrorCode POLICY_EVALUATION_FAILED, got %s", failEvt.ErrorCode)
	}
}

func TestAuditEvaluator_JoinedErrorsWhenAuditLoggerFails(t *testing.T) {
	ctx := context.Background()
	failingLogger := &mockAuditLogger{err: errors.New("disk full / logger stream closed")}

	failingCandProv := &mockCandidateProvider{err: errors.New("hardware provider down")}
	allocProv := &mockAllocationProvider{obsAt: time.Now()}
	builder := NewSnapshotBuilder(failingCandProv, allocProv)
	engine := domainPolicy.NewPolicyEngine()

	cfg := Config{Mode: PreemptionModeAuditOnly}
	evaluator := NewAuditEvaluator(cfg, &mockClock{fixed: time.Now()}, builder, engine, &mockIDGen{id: "evt-fail-2"}, failingLogger, zerolog.Nop())

	req := ConflictAuditRequest{
		RequestID:    "req-fail-join",
		Consumer:     domainPolicy.ConsumerLiveTV,
		ResourceKind: domainPolicy.ResourceTuner,
		Owner:        "user-1",
		ScopeMode:    domainPolicy.ScopeSelectionAnyCompatible,
	}

	err := evaluator.AuditTunerConflict(ctx, req)
	if !errors.Is(err, ErrSnapshotBuildFailed) {
		t.Fatalf("expected primary error ErrSnapshotBuildFailed in joined error chain, got %v", err)
	}
	if !errors.Is(err, ErrAuditEmissionFailed) {
		t.Fatalf("expected audit emission error ErrAuditEmissionFailed in joined error chain, got %v", err)
	}
}
