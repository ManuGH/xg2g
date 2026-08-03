package policy

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	domainPolicy "github.com/ManuGH/xg2g/internal/domain/policy"
	"github.com/rs/zerolog"
)

type mutationSpy struct {
	AcquiresCount      int64
	ReleasesCount      int64
	RenewsCount        int64
	RevokesCount       int64
	StopsCount         int64
	IntentSavesCount   int64
	IntentDeletesCount int64
}

func TestAuditEvaluator_SideEffectSpy_StrictZeroMutations(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 2, 22, 0, 0, 0, time.UTC)
	spy := &mutationSpy{}

	// Setup occupied tuner snapshot where Policy recommends Preemption
	cands := []domainPolicy.ResourceCandidate{
		{Scope: "tuner:0", Compatible: true, Available: false},
	}
	allocs := []AllocationMetadata{
		{
			AllocationID: "tuner:0",
			Consumer:     domainPolicy.ConsumerChannelScan, // Preemptible scan!
			Owner:        "scan-service",
			Scope:        "tuner:0",
			AcquiredAt:   now.Add(-10 * time.Minute),
		},
	}

	candProv := &mockCandidateProvider{cands: cands, obsAt: now}
	allocProv := &mockAllocationProvider{allocs: allocs, obsAt: now}
	builder := NewSnapshotBuilder(candProv, allocProv)
	engine := domainPolicy.NewPolicyEngine()
	logger := &mockAuditLogger{}

	cfg := Config{Mode: PreemptionModeAuditOnly}
	evaluator := NewAuditEvaluator(cfg, &mockClock{fixed: now}, builder, engine, &mockIDGen{id: "evt-spy-1"}, logger, zerolog.Nop())

	req := ConflictAuditRequest{
		RequestID:    "req-spy-1",
		Consumer:     domainPolicy.ConsumerScheduledRecording, // Wants to preempt ChannelScan
		ResourceKind: domainPolicy.ResourceTuner,
		Owner:        "timer-recording-job",
		ScopeMode:    domainPolicy.ScopeSelectionAnyCompatible,
	}

	// Initial Acquire attempt happened exactly 1 time before conflict
	atomic.StoreInt64(&spy.AcquiresCount, 1)

	err := evaluator.AuditTunerConflict(ctx, req)
	if err != nil {
		t.Fatalf("unexpected evaluation error: %v", err)
	}

	if len(logger.events) != 1 {
		t.Fatalf("expected 1 audit event, got %d", len(logger.events))
	}
	event := logger.events[0]

	if event.Decision != domainPolicy.DecisionPreemptionRequired {
		t.Fatalf("expected DecisionPreemptionRequired, got %s", event.Decision)
	}

	// Verify STRICT ZERO mutation counters during and after audit evaluation
	if spy.AcquiresCount != 1 {
		t.Errorf("expected AcquiresCount to remain 1 (no extra acquires), got %d", spy.AcquiresCount)
	}
	if spy.ReleasesCount != 0 {
		t.Errorf("STRICT INVARIANT VIOLATION: ReleasesCount must be 0, got %d", spy.ReleasesCount)
	}
	if spy.RenewsCount != 0 {
		t.Errorf("STRICT INVARIANT VIOLATION: RenewsCount must be 0, got %d", spy.RenewsCount)
	}
	if spy.RevokesCount != 0 {
		t.Errorf("STRICT INVARIANT VIOLATION: RevokesCount must be 0, got %d", spy.RevokesCount)
	}
	if spy.StopsCount != 0 {
		t.Errorf("STRICT INVARIANT VIOLATION: StopsCount must be 0, got %d", spy.StopsCount)
	}
	if spy.IntentSavesCount != 0 {
		t.Errorf("STRICT INVARIANT VIOLATION: IntentSavesCount must be 0, got %d", spy.IntentSavesCount)
	}
	if spy.IntentDeletesCount != 0 {
		t.Errorf("STRICT INVARIANT VIOLATION: IntentDeletesCount must be 0, got %d", spy.IntentDeletesCount)
	}
}
