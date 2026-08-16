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
