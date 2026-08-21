// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package receivertopology

import (
	"testing"
	"time"
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

func TestReconciliation_EvidenceUnknownPreservesClaims(t *testing.T) {
	topo := buildVuPlusUno4K_FBC_SingleCable()
	svc, err := NewService(topo, EvaluationModeEnforce)
	if err != nil {
		t.Fatalf("unexpected NewService error: %v", err)
	}

	// 1. Snapshot with EvidenceUnknown / missing OpenWebIF data
	svc.UpdateEvidentiarySnapshot(ReceiverRuntimeSnapshot{
		ObservedAt:      time.Now().UTC().Add(-1 * time.Hour), // Stale
		StandbyEvidence: EvidenceUnknown,
	})

	// 2. Active lease running in memory
	_, _, err = svc.ReserveStreamLease("1:0:19:2B90:3F3:1:C00000:0:0:0:", "active-sess-1", PriorityLive, 10*time.Minute)
	if err != nil {
		t.Fatalf("unexpected ReserveStreamLease error: %v", err)
	}

	// 3. BuildReconciliationPlan when session is active in session store
	plan := svc.BuildReconciliationPlan([]string{"active-sess-1"}, nil, time.Now().UTC())

	// INVARIANT: Active session MUST NOT be flagged for reap even if OpenWebIF snapshot is unknown/stale
	if len(plan.SessionsToReap) > 0 {
		t.Fatalf("expected 0 sessions to reap, got %v", plan.SessionsToReap)
	}
}

func TestTransponderKey_IdenticalRF_SharesDemod(t *testing.T) {
	topo := buildVuPlusUno4K_FBC_SingleCable()
	svc, err := NewService(topo, EvaluationModeEnforce)
	if err != nil {
		t.Fatalf("unexpected NewService error: %v", err)
	}

	tpKey := TransponderKey{
		DeliverySystem:  DeliverySystemDVBS2,
		OrbitalPosition: 192,
		FrequencyHz:     11493000000,
		Polarization:    PolarizationHorizontal,
		StreamID:        1,
		PLSMode:         PLSModeGold,
		PLSCode:         12345,
	}

	mux1 := BuildMultiplexWithTransponder(DVBTypeSat, 0x00C00000, 0x03EF, 0x0001, tpKey)
	mux2 := BuildMultiplexWithTransponder(DVBTypeSat, 0x00C00000, 0x03EF, 0x0001, tpKey)

	// 1. First stream reserves lease
	lease1, dec1, err := svc.ReserveMultiplexLeaseAtomic(mux1, "sess-1", PriorityLive, time.Minute)
	if err != nil || !dec1.Allowed {
		t.Fatalf("first stream reservation failed: %v", err)
	}
	defer svc.ReleaseStream("sess-1")

	// 2. Second stream with identical RF identity MUST share the demod
	lease2, dec2, err := svc.ReserveMultiplexLeaseAtomic(mux2, "sess-2", PriorityLive, time.Minute)
	if err != nil || !dec2.Allowed {
		t.Fatalf("second stream reservation failed: %v", err)
	}
	defer svc.ReleaseStream("sess-2")

	if !dec2.ReusedDemod {
		t.Fatalf("expected second stream to reuse demod for identical RF transponder")
	}
	if lease1.DemodID != lease2.DemodID {
		t.Fatalf("expected same demod ID %s, got %s", lease1.DemodID, lease2.DemodID)
	}
}

func TestTransponderKey_Multistream_DifferentStreamID_NoSharing(t *testing.T) {
	topo := buildVuPlusUno4K_FBC_SingleCable()
	svc, err := NewService(topo, EvaluationModeEnforce)
	if err != nil {
		t.Fatalf("unexpected NewService error: %v", err)
	}

	tpKey1 := TransponderKey{
		DeliverySystem:  DeliverySystemDVBS2,
		OrbitalPosition: 192,
		FrequencyHz:     11493000000,
		Polarization:    PolarizationHorizontal,
		StreamID:        1, // Stream 1 (MIS)
	}
	tpKey2 := TransponderKey{
		DeliverySystem:  DeliverySystemDVBS2,
		OrbitalPosition: 192,
		FrequencyHz:     11493000000,
		Polarization:    PolarizationHorizontal,
		StreamID:        2, // Stream 2 (MIS)
	}

	mux1 := BuildMultiplexWithTransponder(DVBTypeSat, 0x00C00000, 0x03EF, 0x0001, tpKey1)
	mux2 := BuildMultiplexWithTransponder(DVBTypeSat, 0x00C00000, 0x03EF, 0x0001, tpKey2)

	// Stream 1
	lease1, dec1, err := svc.ReserveMultiplexLeaseAtomic(mux1, "sess-mis-1", PriorityLive, time.Minute)
	if err != nil || !dec1.Allowed {
		t.Fatalf("stream 1 reservation failed: %v", err)
	}
	defer svc.ReleaseStream("sess-mis-1")

	// Stream 2 on different stream ID MUST allocate a separate demod (or not reuse)
	lease2, dec2, err := svc.ReserveMultiplexLeaseAtomic(mux2, "sess-mis-2", PriorityLive, time.Minute)
	if err != nil || !dec2.Allowed {
		t.Fatalf("stream 2 reservation failed: %v", err)
	}
	defer svc.ReleaseStream("sess-mis-2")

	if dec2.ReusedDemod {
		t.Fatalf("expected NO false demod sharing across different Multistream StreamIDs (MIS)")
	}
	if lease1.DemodID == lease2.DemodID {
		t.Fatalf("expected different demod IDs for distinct Multistream StreamIDs")
	}
}

func TestTransponderKey_PLS_DifferentCode_NoSharing(t *testing.T) {
	topo := buildVuPlusUno4K_FBC_SingleCable()
	svc, err := NewService(topo, EvaluationModeEnforce)
	if err != nil {
		t.Fatalf("unexpected NewService error: %v", err)
	}

	tpKey1 := TransponderKey{
		DeliverySystem:  DeliverySystemDVBS2,
		OrbitalPosition: 192,
		FrequencyHz:     11493000000,
		Polarization:    PolarizationHorizontal,
		PLSMode:         PLSModeGold,
		PLSCode:         11111,
	}
	tpKey2 := TransponderKey{
		DeliverySystem:  DeliverySystemDVBS2,
		OrbitalPosition: 192,
		FrequencyHz:     11493000000,
		Polarization:    PolarizationHorizontal,
		PLSMode:         PLSModeGold,
		PLSCode:         99999,
	}

	mux1 := BuildMultiplexWithTransponder(DVBTypeSat, 0x00C00000, 0x03EF, 0x0001, tpKey1)
	mux2 := BuildMultiplexWithTransponder(DVBTypeSat, 0x00C00000, 0x03EF, 0x0001, tpKey2)

	lease1, dec1, err := svc.ReserveMultiplexLeaseAtomic(mux1, "sess-pls-1", PriorityLive, time.Minute)
	if err != nil || !dec1.Allowed {
		t.Fatalf("stream 1 reservation failed: %v", err)
	}
	defer svc.ReleaseStream("sess-pls-1")

	lease2, dec2, err := svc.ReserveMultiplexLeaseAtomic(mux2, "sess-pls-2", PriorityLive, time.Minute)
	if err != nil || !dec2.Allowed {
		t.Fatalf("stream 2 reservation failed: %v", err)
	}
	defer svc.ReleaseStream("sess-pls-2")

	if dec2.ReusedDemod {
		t.Fatalf("expected NO false demod sharing across different PLS codes")
	}
	if lease1.DemodID == lease2.DemodID {
		t.Fatalf("expected different demod IDs for distinct PLS codes")
	}
}

func TestTransponderKey_DVBT2_DifferentPLP_CorrectDecision(t *testing.T) {
	dvbtTopo := ReceiverTopology{
		Model:      "DVB-T2 Receiver",
		Confidence: ConfidenceVerified,
		Inputs: []PhysicalInput{
			{ID: "in_t2", DeliveryType: DeliveryTerrestrial},
		},
		Demodulators: []Demodulator{
			{ID: "demod_t0", InputID: "in_t2", DVBTypes: []DVBType{DVBTypeTerrestrial}},
			{ID: "demod_t1", InputID: "in_t2", DVBTypes: []DVBType{DVBTypeTerrestrial}},
		},
	}
	svc, err := NewService(dvbtTopo, EvaluationModeEnforce)
	if err != nil {
		t.Fatalf("unexpected NewService error: %v", err)
	}

	tpKey1 := TransponderKey{
		DeliverySystem: DeliverySystemDVBT2,
		FrequencyHz:    506000000,
		StreamID:       0, // PLP 0
	}
	tpKey2 := TransponderKey{
		DeliverySystem: DeliverySystemDVBT2,
		FrequencyHz:    506000000,
		StreamID:       1, // PLP 1
	}

	mux1 := BuildMultiplexWithTransponder(DVBTypeTerrestrial, 0xEEEE0000, 0x1111, 0x2222, tpKey1)
	mux2 := BuildMultiplexWithTransponder(DVBTypeTerrestrial, 0xEEEE0000, 0x1111, 0x2222, tpKey2)

	lease1, dec1, err := svc.ReserveMultiplexLeaseAtomic(mux1, "sess-plp-0", PriorityLive, time.Minute)
	if err != nil || !dec1.Allowed {
		t.Fatalf("stream 1 reservation failed: %v", err)
	}
	defer svc.ReleaseStream("sess-plp-0")

	lease2, dec2, err := svc.ReserveMultiplexLeaseAtomic(mux2, "sess-plp-1", PriorityLive, time.Minute)
	if err != nil || !dec2.Allowed {
		t.Fatalf("stream 2 reservation failed: %v", err)
	}
	defer svc.ReleaseStream("sess-plp-1")

	if dec2.ReusedDemod {
		t.Fatalf("expected NO false demod sharing across different DVB-T2 PLPs")
	}
	if lease1.DemodID == lease2.DemodID {
		t.Fatalf("expected different demod IDs for distinct DVB-T2 PLPs")
	}
}

func TestTransponderRegistry_ResolvesAuthoritativeRFFromRawServiceRef(t *testing.T) {
	topo := buildVuPlusUno4K_FBC_SingleCable()
	svc, err := NewService(topo, EvaluationModeEnforce)
	if err != nil {
		t.Fatalf("unexpected NewService error: %v", err)
	}

	// 1. Create authoritative registry
	registry := NewTransponderRegistry()

	// Register ORF 1 HD transponder (Astra 19.2E, 11273 MHz, Horizontal, Low Band, TSID 0x03EF, ONID 0x0001, DVB-S2)
	registry.RegisterTransponder(0x03EF, 0x0001, 0x00C00000, TransponderKey{
		DeliverySystem:  DeliverySystemDVBS2,
		OrbitalPosition: 192,
		FrequencyHz:     11273000000,
		Polarization:    PolarizationHorizontal,
	})

	svc.SetResolver(registry)

	// 2. Call ReserveStreamLeaseAtomic with RAW Enigma2 Service Reference (no query params!)
	rawServiceRef1 := "1:0:19:132F:3EF:1:C00000:0:0:0:" // ORF 1 HD
	lease1, dec1, err := svc.ReserveStreamLeaseAtomic(rawServiceRef1, "sess-raw-orf1", PriorityLive, time.Minute)
	if err != nil || !dec1.Allowed {
		t.Fatalf("raw serviceRef1 reservation failed: %v", err)
	}
	defer svc.ReleaseStream("sess-raw-orf1")

	// Verify authoritative RF plane was applied: Low Band, Horizontal, 11273 MHz
	allocs := svc.RuntimeSnapshot().ActiveMultiplexes
	if len(allocs) != 1 {
		t.Fatalf("expected 1 active multiplex, got %d", len(allocs))
	}
	var activeMux MultiplexID
	for _, alloc := range allocs {
		activeMux = alloc.MultiplexID
	}
	if activeMux.RFPlane == nil || activeMux.RFPlane.Band != BandLow || activeMux.RFPlane.Polarization != PolarizationHorizontal {
		t.Fatalf("expected authoritative RF plane Low/Horizontal, got %+v", activeMux.RFPlane)
	}
	if activeMux.TransponderKey == nil || activeMux.TransponderKey.FrequencyHz != 11273000000 {
		t.Fatalf("expected authoritative FrequencyHz 11273000000, got %+v", activeMux.TransponderKey)
	}

	// 3. Second channel on same raw TSID 0x03EF (ORF 2 HD) MUST reuse the demod
	rawServiceRef2 := "1:0:19:1330:3EF:1:C00000:0:0:0:" // ORF 2 HD
	lease2, dec2, err := svc.ReserveStreamLeaseAtomic(rawServiceRef2, "sess-raw-orf2", PriorityLive, time.Minute)
	if err != nil || !dec2.Allowed {
		t.Fatalf("raw serviceRef2 reservation failed: %v", err)
	}
	defer svc.ReleaseStream("sess-raw-orf2")

	if !dec2.ReusedDemod {
		t.Fatalf("expected ORF 2 HD to reuse demod on same authoritative transponder")
	}
	if lease1.DemodID != lease2.DemodID {
		t.Fatalf("expected same demod ID %s, got %s", lease1.DemodID, lease2.DemodID)
	}
}

func TestTransponderRegistry_PlaneConflict_DetectedViaAuthoritativeRF(t *testing.T) {
	// Single legacy cable: can only tune 1 RF plane at a time
	singleCableTopo := ReceiverTopology{
		Model:      "Single Cable Legacy LNB Receiver",
		Confidence: ConfidenceVerified,
		Inputs: []PhysicalInput{
			{ID: "in_a", DeliveryType: DeliveryLegacyUniversal, Satellites: []SatellitePosition{192}},
		},
		Demodulators: []Demodulator{
			{ID: "demod_0", InputID: "in_a", DVBTypes: []DVBType{DVBTypeSat}},
			{ID: "demod_1", InputID: "in_a", DVBTypes: []DVBType{DVBTypeSat}},
		},
	}
	svc, err := NewService(singleCableTopo, EvaluationModeEnforce)
	if err != nil {
		t.Fatalf("unexpected NewService error: %v", err)
	}

	registry := NewTransponderRegistry()
	// Channel 1: High Band Horizontal (12544 MHz H)
	registry.RegisterTransponder(0x0400, 0x0001, 0x00C00000, TransponderKey{
		DeliverySystem:  DeliverySystemDVBS2,
		OrbitalPosition: 192,
		FrequencyHz:     12544000000,
		Polarization:    PolarizationHorizontal,
	})
	// Channel 2: Low Band Vertical (11494 MHz V)
	registry.RegisterTransponder(0x0401, 0x0001, 0x00C00000, TransponderKey{
		DeliverySystem:  DeliverySystemDVBS2,
		OrbitalPosition: 192,
		FrequencyHz:     11494000000,
		Polarization:    PolarizationVertical,
	})

	svc.SetResolver(registry)

	// Stream 1 on raw serviceRef (High Band Horizontal)
	rawRef1 := "1:0:19:1000:400:1:C00000:0:0:0:"
	_, dec1, err := svc.ReserveStreamLeaseAtomic(rawRef1, "sess-plane-h", PriorityLive, time.Minute)
	if err != nil || !dec1.Allowed {
		t.Fatalf("stream 1 reservation failed: %v", err)
	}
	defer svc.ReleaseStream("sess-plane-h")

	// Stream 2 on raw serviceRef (Low Band Vertical) -> Single cable MUST detect plane conflict and reject!
	rawRef2 := "1:0:19:2000:401:1:C00000:0:0:0:"
	_, dec2, err := svc.ReserveStreamLeaseAtomic(rawRef2, "sess-plane-v", PriorityLive, time.Minute)
	if err == nil && dec2.Allowed {
		t.Fatalf("expected plane conflict rejection for conflicting authoritative RF planes on single cable")
	}
	if dec2.ProblemCode != ProblemCodePlaneConflict {
		t.Fatalf("expected ProblemCodePlaneConflict, got %s", dec2.ProblemCode)
	}
}
