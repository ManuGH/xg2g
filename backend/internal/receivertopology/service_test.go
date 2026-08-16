// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package receivertopology

import (
	"testing"
)

func TestService_StreamLifecycle(t *testing.T) {
	topology := buildVuPlusUno4K_FBC_SingleCable()
	svc, err := NewService(topology, EvaluationModeEnforce)
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	serviceRef1 := "1:0:19:283D:3FB:1:C00000:0:0:0:" // Das Erste HD (TSID 0x03FB, ONID 0x0001, Astra 19.2E)
	session1 := "sess-abc-1"

	// 1. Check can start
	decision, err := svc.CanStartStream(serviceRef1, session1)
	if err != nil || !decision.Allowed {
		t.Fatalf("expected CanStartStream ALLOW, got %v (%s)", err, decision.Reason)
	}

	// 2. Register stream
	allocDecision, err := svc.RegisterStream(serviceRef1, session1)
	if err != nil || !allocDecision.Allowed {
		t.Fatalf("expected RegisterStream ALLOW, got %v (%s)", err, allocDecision.Reason)
	}

	snapshot := svc.RuntimeSnapshot()
	if len(snapshot.ActiveMultiplexes) != 1 {
		t.Fatalf("expected 1 active multiplex, got %d", len(snapshot.ActiveMultiplexes))
	}

	// 3. Register second session on same multiplex (Reuse)
	session2 := "sess-abc-2"
	allocDecision2, err := svc.RegisterStream(serviceRef1, session2)
	if err != nil || !allocDecision2.Allowed || !allocDecision2.ReusedDemod {
		t.Fatalf("expected RegisterStream 2 to reuse demod, got %v (reused=%v)", err, allocDecision2.ReusedDemod)
	}

	// 4. Release session 1 (multiplex should remain active because session 2 is still running)
	svc.ReleaseStream(session1)
	snapshot = svc.RuntimeSnapshot()
	if len(snapshot.ActiveMultiplexes) != 1 {
		t.Fatalf("expected multiplex still active with session 2 running")
	}

	// 5. Release session 2 (multiplex should be completely freed)
	svc.ReleaseStream(session2)
	snapshot = svc.RuntimeSnapshot()
	if len(snapshot.ActiveMultiplexes) != 0 {
		t.Fatalf("expected 0 active multiplexes after releasing session 2, got %d", len(snapshot.ActiveMultiplexes))
	}
}

func TestService_ReconcileActiveSessions(t *testing.T) {
	topology := buildVuPlusUno4K_FBC_SingleCable()
	svc, _ := NewService(topology, EvaluationModeEnforce)

	serviceRef := "1:0:19:283D:3FB:1:C00000:0:0:0:"
	_, _ = svc.RegisterStream(serviceRef, "active-1")
	_, _ = svc.RegisterStream(serviceRef, "abandoned-2")

	if len(svc.RuntimeSnapshot().ActiveMultiplexes) != 1 {
		t.Fatalf("expected 1 active multiplex")
	}

	// Reconcile with only "active-1" remaining
	svc.ReconcileActiveSessions([]ActiveSessionInfo{
		{SessionID: "active-1", ServiceRef: serviceRef},
	})

	snapshot := svc.RuntimeSnapshot()
	for _, alloc := range snapshot.ActiveMultiplexes {
		if len(alloc.SessionIDs) != 1 || alloc.SessionIDs[0] != "active-1" {
			t.Fatalf("expected only active-1 surviving, got %v", alloc.SessionIDs)
		}
	}

	// Reconcile with 0 active sessions
	svc.ReconcileActiveSessions(nil)
	if len(svc.RuntimeSnapshot().ActiveMultiplexes) != 0 {
		t.Fatalf("expected 0 active multiplexes after empty reconciliation")
	}
}

func TestService_ConfidenceTransitions(t *testing.T) {
	defaultTopo := DefaultFallbackTopology()
	svc, err := NewService(defaultTopo, EvaluationModeAuditOnly)
	if err != nil {
		t.Fatalf("failed to create default service: %v", err)
	}

	// 1. Initial state: DEFAULT -> Mode: AUDIT_ONLY
	if svc.Topology().Confidence != ConfidenceDefault {
		t.Fatalf("expected initial ConfidenceDefault, got %s", svc.Topology().Confidence)
	}
	if svc.Mode() != EvaluationModeAuditOnly {
		t.Fatalf("expected initial EvaluationModeAuditOnly, got %s", svc.Mode())
	}

	// 2. DEFAULT -> OBSERVED (Allowed, AUDIT_ONLY)
	observedTopo := ReceiverTopology{
		Model:      "Observed Model",
		Confidence: ConfidenceObserved,
		Inputs: []PhysicalInput{
			{ID: "input_a", DeliveryType: DeliveryLegacyUniversal},
		},
		Demodulators: []Demodulator{
			{ID: "demod_a", InputID: "input_a", DVBTypes: []DVBType{DVBTypeSat}},
		},
	}
	err = svc.UpdateTopologyWithPriority(observedTopo, false)
	if err != nil {
		t.Fatalf("expected DEFAULT -> OBSERVED success, got: %v", err)
	}
	if svc.Topology().Confidence != ConfidenceObserved || svc.Topology().Model != "Observed Model" {
		t.Fatalf("failed to update to OBSERVED")
	}
	if svc.Mode() != EvaluationModeAuditOnly {
		t.Fatalf("expected mode AUDIT_ONLY for OBSERVED, got %s", svc.Mode())
	}

	// 3. OBSERVED -> OBSERVED (Allowed, e.g. re-poll with updated info)
	observedTopo2 := observedTopo
	observedTopo2.Model = "Observed Model Updated"
	err = svc.UpdateTopologyWithPriority(observedTopo2, false)
	if err != nil {
		t.Fatalf("expected OBSERVED -> OBSERVED success, got: %v", err)
	}
	if svc.Topology().Model != "Observed Model Updated" {
		t.Fatalf("failed to update OBSERVED -> OBSERVED")
	}

	// 4. OBSERVED -> DEFAULT (Forbidden)
	err = svc.UpdateTopologyWithPriority(defaultTopo, false)
	if err == nil {
		t.Fatalf("expected OBSERVED -> DEFAULT to be rejected")
	}

	// 5. OBSERVED -> VERIFIED (Allowed, ENFORCE)
	verifiedTopo := buildVuPlusUno4K_FBC_SingleCable() // ConfidenceVerified
	err = svc.UpdateTopologyWithPriority(verifiedTopo, true)
	if err != nil {
		t.Fatalf("expected OBSERVED -> VERIFIED success, got: %v", err)
	}
	if svc.Topology().Confidence != ConfidenceVerified {
		t.Fatalf("expected ConfidenceVerified, got %s", svc.Topology().Confidence)
	}
	if svc.Mode() != EvaluationModeEnforce {
		t.Fatalf("expected EvaluationModeEnforce for VERIFIED, got %s", svc.Mode())
	}

	// 6. VERIFIED -> OBSERVED (Forbidden, verified is sticky)
	err = svc.UpdateTopologyWithPriority(observedTopo, false)
	if err == nil {
		t.Fatalf("expected VERIFIED -> OBSERVED to be rejected")
	}

	// 7. VERIFIED -> DEFAULT (Forbidden)
	err = svc.UpdateTopologyWithPriority(defaultTopo, false)
	if err == nil {
		t.Fatalf("expected VERIFIED -> DEFAULT to be rejected")
	}

	// 8. VERIFIED -> VERIFIED on explicit reload (Allowed)
	verifiedTopo2 := buildVuPlusUno4K_FBC_DualCable()
	err = svc.UpdateTopologyWithPriority(verifiedTopo2, true, EvaluationModeEnforce)
	if err != nil {
		t.Fatalf("expected VERIFIED -> VERIFIED with explicit reload success, got: %v", err)
	}
	if len(svc.Topology().Inputs) != 2 {
		t.Fatalf("expected updated dual-cable verified topology with 2 inputs")
	}
}

func TestService_ConfidenceModeInvariants(t *testing.T) {
	observedTopo := ReceiverTopology{
		Model:      "Observed Receiver",
		Confidence: ConfidenceObserved,
		Inputs: []PhysicalInput{
			{ID: "input_a", DeliveryType: DeliveryLegacyUniversal},
		},
		Demodulators: []Demodulator{
			{ID: "demod_a", InputID: "input_a", DVBTypes: []DVBType{DVBTypeSat}},
		},
	}

	defaultTopo := DefaultFallbackTopology()

	// 1. NewService with ENFORCE on OBSERVED -> Must fail
	_, err := NewService(observedTopo, EvaluationModeEnforce)
	if err == nil {
		t.Fatalf("expected NewService(Observed, ENFORCE) to fail")
	}

	// 2. NewService with ENFORCE on DEFAULT -> Must fail
	_, err = NewService(defaultTopo, EvaluationModeEnforce)
	if err == nil {
		t.Fatalf("expected NewService(Default, ENFORCE) to fail")
	}

	// 3. NewService with AUDIT_ONLY on OBSERVED -> Must succeed with AUDIT_ONLY
	svc, err := NewService(observedTopo, EvaluationModeAuditOnly)
	if err != nil {
		t.Fatalf("expected NewService(Observed, AUDIT_ONLY) to succeed: %v", err)
	}
	if svc.Mode() != EvaluationModeAuditOnly {
		t.Fatalf("expected mode AUDIT_ONLY, got %s", svc.Mode())
	}

	// 4. UpdateTopology with ENFORCE on OBSERVED -> Must fail
	err = svc.UpdateTopology(observedTopo, EvaluationModeEnforce)
	if err == nil {
		t.Fatalf("expected UpdateTopology(Observed, ENFORCE) to fail")
	}

	// 5. UpdateTopologyWithPriority with ENFORCE on OBSERVED -> Must fail
	err = svc.UpdateTopologyWithPriority(observedTopo, false, EvaluationModeEnforce)
	if err == nil {
		t.Fatalf("expected UpdateTopologyWithPriority(Observed, ENFORCE) to fail")
	}

	// 6. UpdateTopologyWithPriority with ENFORCE on DEFAULT -> Must fail
	defaultSvc, _ := NewService(defaultTopo, EvaluationModeAuditOnly)
	err = defaultSvc.UpdateTopologyWithPriority(defaultTopo, false, EvaluationModeEnforce)
	if err == nil {
		t.Fatalf("expected UpdateTopologyWithPriority(Default, ENFORCE) to fail")
	}

	// 7. Allocator constructor clamps non-verified topologies to AUDIT_ONLY
	alloc := NewAllocator(observedTopo, EvaluationModeEnforce)
	if alloc.Mode() != EvaluationModeAuditOnly {
		t.Fatalf("expected NewAllocator(Observed, ENFORCE) to clamp to AUDIT_ONLY, got %s", alloc.Mode())
	}
}
