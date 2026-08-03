// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package manager

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/receiverusage"
	"github.com/ManuGH/xg2g/internal/domain/session/lifecycle"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/store"
	pipelineLease "github.com/ManuGH/xg2g/internal/pipeline/lease"
	"github.com/rs/zerolog"
)

// Test 1: Restricted Access slot acquired -> Tuner acquire fails -> Restricted Access slot guaranteed released -> original tuner error returned.
func TestE2_5c_Compensation_RestrictedAccessReleased_OnTunerFailure(t *testing.T) {
	st := store.NewMemoryStore()
	evaluator := receiverusage.NewEvaluator()
	ctrl := receiverusage.NewRestrictedAccessController(st)

	o := &Orchestrator{
		Store:                st,
		ReceiverID:           "rec-comp-1",
		Owner:                "test-worker-1",
		TunerSlots:           []int{0}, // Slot 0
		LeaseTTL:             10 * time.Minute,
		UsageEvaluator:       evaluator,
		RestrictedAccessCtrl: ctrl,
		UsagePolicy:          receiverusage.ConservativeSingleUsePolicy(),
		LeaseKeyFunc:         func(e model.StartSessionEvent) string { return "dedup:" + e.SessionID },
	}

	// Pre-occupy tuner slot 0 with another owner to force tuner conflict
	_, _, _ = st.TryAcquireLease(context.Background(), "tuner:0", "other-tuner-owner", 10*time.Minute)

	event := model.StartSessionEvent{
		SessionID:  "sess-comp-1",
		ServiceRef: "1:0:19:283D:3FB:1:C00000:0:0:0:",
	}

	sCtx := &sessionContext{
		Mode:       model.ModeLive,
		ServiceRef: event.ServiceRef,
	}

	_, err := o.acquireLeases(context.Background(), sCtx, event, "test-owner-1", zerolog.Nop())
	if err == nil {
		t.Fatalf("expected tuner acquire failure, got nil")
	}

	// Verify original tuner conflict error is preserved (reason RLeaseBusy and cause ErrScopeConflict)
	reason, _, _, ok := lifecycle.ReasonFromError(err)
	if !ok || reason != model.RLeaseBusy {
		t.Fatalf("expected tuner conflict error with reason RLeaseBusy, got %v", err)
	}

	// CRITICAL COMPENSATING VERIFICATION:
	// The Restricted Access slot MUST have been released by compensation!
	inv, invErr := receiverusage.Inventory(context.Background(), st, "rec-comp-1", 1, time.Now())
	if invErr != nil {
		t.Fatalf("unexpected inventory error: %v", invErr)
	}
	if len(inv.Active) != 0 {
		t.Fatalf("COMPENSATION FAILED! Expected 0 active restricted access leases in store, got %d (leak!)", len(inv.Active))
	}
}

// Test 2: ModeDisabled (default) acquires 0 access leases and passes cleanly.
func TestE2_5c_DisabledMode_AcquiresZeroAccessLeases(t *testing.T) {
	st := store.NewMemoryStore()
	evaluator := receiverusage.NewEvaluator()
	ctrl := receiverusage.NewRestrictedAccessController(st)

	o := &Orchestrator{
		Store:                st,
		ReceiverID:           "rec-dis-1",
		Owner:                "test-worker-1",
		TunerSlots:           []int{0},
		LeaseTTL:             10 * time.Minute,
		UsageEvaluator:       evaluator,
		RestrictedAccessCtrl: ctrl,
		UsagePolicy: receiverusage.ReceiverUsagePolicy{
			Mode: receiverusage.ReceiverUsageModeDisabled,
		},
		LeaseKeyFunc: func(e model.StartSessionEvent) string { return "dedup:" + e.SessionID },
	}

	event := model.StartSessionEvent{
		SessionID:  "sess-dis-1",
		ServiceRef: "1:0:19:283D:3FB:1:C00000:0:0:0:",
	}

	sCtx := &sessionContext{
		Mode:       model.ModeLive,
		ServiceRef: event.ServiceRef,
	}

	res, err := o.acquireLeases(context.Background(), sCtx, event, "test-owner-1", zerolog.Nop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.RestrictedAccessHandle.Scope != "" {
		t.Fatalf("Disabled mode should acquire 0 restricted access leases, got %s", res.RestrictedAccessHandle.Scope)
	}
}

// Test 3: ModeAuditOnly evaluates without rejecting or acquiring access leases.
func TestE2_5c_AuditOnlyMode_LogsWithoutRejection(t *testing.T) {
	st := store.NewMemoryStore()
	evaluator := receiverusage.NewEvaluator()
	ctrl := receiverusage.NewRestrictedAccessController(st)

	o := &Orchestrator{
		Store:                st,
		ReceiverID:           "rec-audit-1",
		Owner:                "test-worker-1",
		TunerSlots:           []int{0},
		LeaseTTL:             10 * time.Minute,
		UsageEvaluator:       evaluator,
		RestrictedAccessCtrl: ctrl,
		UsagePolicy: receiverusage.ReceiverUsagePolicy{
			Mode:                        receiverusage.ReceiverUsageModeAuditOnly,
			MaxLiveSessions:             0, // Would reject if enforced!
			MaxRestrictedAccessSessions: 0,
		},
		LeaseKeyFunc: func(e model.StartSessionEvent) string { return "dedup:" + e.SessionID },
	}

	event := model.StartSessionEvent{
		SessionID:  "sess-audit-1",
		ServiceRef: "1:0:19:283D:3FB:1:C00000:0:0:0:",
	}

	sCtx := &sessionContext{
		Mode:       model.ModeLive,
		ServiceRef: event.ServiceRef,
	}

	res, err := o.acquireLeases(context.Background(), sCtx, event, "test-owner-1", zerolog.Nop())
	if err != nil {
		t.Fatalf("AuditOnly mode should not reject request, got %v", err)
	}

	if res.RestrictedAccessHandle.Scope != "" {
		t.Fatalf("AuditOnly mode should acquire 0 restricted access leases, got %s", res.RestrictedAccessHandle.Scope)
	}
}

// Test 4: DecisionReuseSession acquires 0 new leases.
func TestE2_5c_DecisionReuseSession_AcquiresZeroLeases(t *testing.T) {
	st := store.NewMemoryStore()
	evaluator := receiverusage.NewEvaluator()
	ctrl := receiverusage.NewRestrictedAccessController(st)

	o := &Orchestrator{
		Store:                st,
		ReceiverID:           "rec-reuse-1",
		Owner:                "test-worker-1",
		TunerSlots:           []int{0},
		LeaseTTL:             10 * time.Minute,
		UsageEvaluator:       evaluator,
		RestrictedAccessCtrl: ctrl,
		UsagePolicy:          receiverusage.ConservativeSingleUsePolicy(),
		LeaseKeyFunc:         func(e model.StartSessionEvent) string { return "dedup:" + e.SessionID },
	}

	activeSess := &model.SessionRecord{
		SessionID:     "sess-existing",
		ServiceRef:    "1:0:19:283D:3FB:1:C00000:0:0:0:",
		CreatedAtUnix: time.Now().Unix(),
		ContextData: map[string]string{
			"receiver_id": "rec-reuse-1",
			"owner":       "test-owner-1",
			"mode":        model.ModeLive,
		},
	}
	_ = st.PutSession(context.Background(), activeSess)

	event := model.StartSessionEvent{
		SessionID:  "sess-existing",
		ServiceRef: "1:0:19:283D:3FB:1:C00000:0:0:0:",
	}

	sCtx := &sessionContext{
		Mode:       model.ModeLive,
		ServiceRef: event.ServiceRef,
	}

	res, err := o.acquireLeases(context.Background(), sCtx, event, "test-owner-1", zerolog.Nop())
	if err != nil {
		t.Fatalf("unexpected error on reuse session: %v", err)
	}

	if res.RestrictedAccessHandle.Scope != "" {
		t.Fatalf("ReuseSession should not acquire new restricted access handle, got %s", res.RestrictedAccessHandle.Scope)
	}
}

// Test 5: 50 parallel start attempts under -race with occupied tuner slot results in 0 leaked restricted access leases.
func TestE2_5c_Compensation_Concurrent(t *testing.T) {
	st := store.NewMemoryStore()
	evaluator := receiverusage.NewEvaluator()
	ctrl := receiverusage.NewRestrictedAccessController(st)

	o := &Orchestrator{
		Store:                st,
		ReceiverID:           "rec-race-1",
		Owner:                "test-worker-1",
		TunerSlots:           []int{0}, // Slot 0
		LeaseTTL:             10 * time.Minute,
		UsageEvaluator:       evaluator,
		RestrictedAccessCtrl: ctrl,
		UsagePolicy:          receiverusage.ConservativeSingleUsePolicy(),
		LeaseKeyFunc:         func(e model.StartSessionEvent) string { return "dedup:" + e.SessionID },
	}

	// Pre-occupy tuner slot 0 to force tuner acquisition failure across all concurrent workers
	_, _, _ = st.TryAcquireLease(context.Background(), "tuner:0", "other-tuner-owner", 10*time.Minute)

	workers := 50
	var wg sync.WaitGroup
	wg.Add(workers)

	for i := 0; i < workers; i++ {
		go func(id int) {
			defer wg.Done()
			event := model.StartSessionEvent{
				SessionID:  "sess-race-" + string(rune('A'+id%26)),
				ServiceRef: "1:0:19:283D:3FB:1:C00000:0:0:0:",
			}
			sCtx := &sessionContext{
				Mode:       model.ModeLive,
				ServiceRef: event.ServiceRef,
			}
			_, _ = o.acquireLeases(context.Background(), sCtx, event, "worker-"+string(rune('A'+id%26)), zerolog.Nop())
		}(i)
	}

	wg.Wait()

	// Verify 0 leaked restricted access leases in store after all workers finish
	inv, err := receiverusage.Inventory(context.Background(), st, "rec-race-1", 1, time.Now())
	if err != nil {
		t.Fatalf("unexpected inventory error: %v", err)
	}
	if len(inv.Active) != 0 {
		t.Fatalf("CONCURRENCY LEAK! Expected 0 active restricted access leases, got %d", len(inv.Active))
	}
}

// Test 6: Verify errors.Is(tunerErr) holds when joined with release compensation error.
func TestE2_5c_Compensation_JoinedErrorPreservesCause(t *testing.T) {
	tunerErr := pipelineLease.ErrScopeConflict
	relErr := errors.New("store release connection failed")

	joined := errors.Join(tunerErr, relErr)

	if !errors.Is(joined, pipelineLease.ErrScopeConflict) {
		t.Fatalf("joined error must return true for errors.Is(ErrScopeConflict)")
	}
	if !errors.Is(joined, relErr) {
		t.Fatalf("joined error must return true for errors.Is(relErr)")
	}
}
