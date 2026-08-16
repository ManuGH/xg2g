// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package receivertopology

import (
	"testing"
)

// Helper to build standard Vu+ Uno 4K FBC Single-Cable Topology (1 Legacy Cable -> Tuner A root + 7 virtual FBC demods B-H)
func buildVuPlusUno4K_FBC_SingleCable() ReceiverTopology {
	inputA := PhysicalInput{
		ID:           "input_a",
		Label:        "Tuner A (LNB 1 In)",
		DeliveryType: DeliveryLegacyUniversal,
		Satellites:   []SatellitePosition{192}, // Astra 19.2E
	}

	demods := []Demodulator{
		{ID: "demod_a", InputID: "input_a", DVBTypes: []DVBType{DVBTypeSat}, IsFBCVirtual: false},
		{ID: "demod_b", InputID: "input_a", DVBTypes: []DVBType{DVBTypeSat}, IsFBCVirtual: true},
		{ID: "demod_c", InputID: "input_a", DVBTypes: []DVBType{DVBTypeSat}, IsFBCVirtual: true},
		{ID: "demod_d", InputID: "input_a", DVBTypes: []DVBType{DVBTypeSat}, IsFBCVirtual: true},
		{ID: "demod_e", InputID: "input_a", DVBTypes: []DVBType{DVBTypeSat}, IsFBCVirtual: true},
		{ID: "demod_f", InputID: "input_a", DVBTypes: []DVBType{DVBTypeSat}, IsFBCVirtual: true},
		{ID: "demod_g", InputID: "input_a", DVBTypes: []DVBType{DVBTypeSat}, IsFBCVirtual: true},
		{ID: "demod_h", InputID: "input_a", DVBTypes: []DVBType{DVBTypeSat}, IsFBCVirtual: true},
	}

	return ReceiverTopology{
		Model:        "Vu+ Uno 4K",
		Confidence:   ConfidenceVerified,
		Inputs:       []PhysicalInput{inputA},
		Demodulators: demods,
	}
}

// Helper to build Vu+ Uno 4K SE Dual-Cable Topology (2 Legacy Cables -> Tuner A [A,C,D,E] and Tuner B [B,F,G,H])
func buildVuPlusUno4K_FBC_DualCable() ReceiverTopology {
	inputA := PhysicalInput{
		ID:           "input_a",
		Label:        "Tuner A (LNB 1 In)",
		DeliveryType: DeliveryLegacyUniversal,
		Satellites:   []SatellitePosition{192},
	}
	inputB := PhysicalInput{
		ID:           "input_b",
		Label:        "Tuner B (LNB 2 In)",
		DeliveryType: DeliveryLegacyUniversal,
		Satellites:   []SatellitePosition{192},
	}

	demods := []Demodulator{
		{ID: "demod_a", InputID: "input_a", DVBTypes: []DVBType{DVBTypeSat}, IsFBCVirtual: false},
		{ID: "demod_c", InputID: "input_a", DVBTypes: []DVBType{DVBTypeSat}, IsFBCVirtual: true},
		{ID: "demod_d", InputID: "input_a", DVBTypes: []DVBType{DVBTypeSat}, IsFBCVirtual: true},
		{ID: "demod_e", InputID: "input_a", DVBTypes: []DVBType{DVBTypeSat}, IsFBCVirtual: true},

		{ID: "demod_b", InputID: "input_b", DVBTypes: []DVBType{DVBTypeSat}, IsFBCVirtual: false},
		{ID: "demod_f", InputID: "input_b", DVBTypes: []DVBType{DVBTypeSat}, IsFBCVirtual: true},
		{ID: "demod_g", InputID: "input_b", DVBTypes: []DVBType{DVBTypeSat}, IsFBCVirtual: true},
		{ID: "demod_h", InputID: "input_b", DVBTypes: []DVBType{DVBTypeSat}, IsFBCVirtual: true},
	}

	return ReceiverTopology{
		Model:        "Vu+ Uno 4K SE",
		Confidence:   ConfidenceVerified,
		Inputs:       []PhysicalInput{inputA, inputB},
		Demodulators: demods,
	}
}

// 1. Golden Test: Multiplex Reuse (Channels on same transponder share 1 physical demod)
func TestGolden_FBC_LegacySingleCable_MultiplexReuse(t *testing.T) {
	topology := buildVuPlusUno4K_FBC_SingleCable()
	allocator := NewAllocator(topology, EvaluationModeEnforce)
	runtime := NewRuntimeAllocation()

	// Transponder ZDF HD on Astra 19.2E (TSID 0x03F3, ONID 0x0001, High-H)
	zdfMux := BuildSatMultiplexID(192, 0x00C00000, 0x03F3, 0x0001, BandHigh, PolarizationHorizontal)

	// Session 1 tunes ZDF HD
	dec1, err := allocator.Allocate(runtime, zdfMux, "session-1", AllocationOwnerXG2G)
	if err != nil || !dec1.Allowed {
		t.Fatalf("Session 1 expected ALLOW, got: %v (%s)", err, dec1.Reason)
	}
	if dec1.ReusedDemod {
		t.Fatalf("Session 1 should be fresh demod, not reused")
	}

	// Sessions 2, 3, 4, 5 tune other channels on the EXACT same multiplex (KiKA HD, 3sat HD, ZDFinfo HD)
	for i := 2; i <= 5; i++ {
		sessID := "session-" + string(rune('0'+i))
		dec, err := allocator.Allocate(runtime, zdfMux, sessID, AllocationOwnerXG2G)
		if err != nil || !dec.Allowed {
			t.Fatalf("Session %d expected ALLOW via multiplex reuse, got: %v", i, err)
		}
		if !dec.ReusedDemod {
			t.Fatalf("Session %d expected ReusedDemod=true", i)
		}
		if dec.DemodID != dec1.DemodID {
			t.Fatalf("Session %d demod %s != session 1 demod %s", i, dec.DemodID, dec1.DemodID)
		}
	}

	// Verify only 1 physical demodulator is occupied
	if len(runtime.ActiveMultiplexes) != 1 {
		t.Fatalf("expected 1 active multiplex, got %d", len(runtime.ActiveMultiplexes))
	}
	alloc := runtime.ActiveMultiplexes[zdfMux.String()]
	if len(alloc.SessionIDs) != 5 {
		t.Fatalf("expected 5 sessions sharing multiplex, got %d", len(alloc.SessionIDs))
	}

	// Release session 2
	released := allocator.Release(runtime, "session-2")
	if !released || len(alloc.SessionIDs) != 4 {
		t.Fatalf("expected release of session-2, remaining %d", len(alloc.SessionIDs))
	}

	// Release all remaining sessions
	allocator.Release(runtime, "session-1")
	allocator.Release(runtime, "session-3")
	allocator.Release(runtime, "session-4")
	allocator.Release(runtime, "session-5")

	if len(runtime.ActiveMultiplexes) != 0 {
		t.Fatalf("expected 0 active multiplexes after full release, got %d", len(runtime.ActiveMultiplexes))
	}
	if len(runtime.ActiveInputPlanes) != 0 {
		t.Fatalf("expected input plane freed after all sessions closed")
	}
}

// 2. Golden Test: 8 Unique Multiplexes on the Active RF Plane (FBC 8 Demods Full Utilization)
func TestGolden_FBC_LegacySingleCable_8UniqueMultiplexesOnActivePlane(t *testing.T) {
	topology := buildVuPlusUno4K_FBC_SingleCable()
	allocator := NewAllocator(topology, EvaluationModeEnforce)
	runtime := NewRuntimeAllocation()

	// 8 distinct transponders all located on Astra 19.2E High-H (TSID 0x0400 through 0x0407)
	for i := 0; i < 8; i++ {
		mux := BuildSatMultiplexID(192, 0x00C00000, uint16(0x0400+i), 0x0001, BandHigh, PolarizationHorizontal)
		sessID := "session-" + string(rune('1'+i))

		dec, err := allocator.Allocate(runtime, mux, sessID, AllocationOwnerXG2G)
		if err != nil || !dec.Allowed {
			t.Fatalf("Session %d on unique multiplex %d expected ALLOW, got: %v (%s)", i+1, i, err, dec.Reason)
		}
		if dec.ReusedDemod {
			t.Fatalf("Session %d should allocate fresh demod", i+1)
		}
	}

	if len(runtime.ActiveMultiplexes) != 8 {
		t.Fatalf("expected 8 active multiplexes, got %d", len(runtime.ActiveMultiplexes))
	}

	// Session 9 on a 9th UNIQUE multiplex on the active plane -> MUST REJECT (Hardware Demod Exhaustion)
	mux9 := BuildSatMultiplexID(192, 0x00C00000, 0x0408, 0x0001, BandHigh, PolarizationHorizontal)
	dec9 := allocator.CanAllocate(runtime, mux9, "session-9")
	if dec9.Allowed {
		t.Fatalf("Session 9 on 9th unique multiplex MUST BE REJECTED when all 8 demods occupied")
	}
	if dec9.Reason != "All 8 hardware demodulators occupied" {
		t.Fatalf("unexpected reject reason: %s", dec9.Reason)
	}

	// However: Session 10 tuning an EXISTING multiplex (e.g. Mux 0) -> MUST BE ALLOWED (Multiplex Reuse!)
	muxExisting := BuildSatMultiplexID(192, 0x00C00000, 0x0400, 0x0001, BandHigh, PolarizationHorizontal)
	dec10 := allocator.CanAllocate(runtime, muxExisting, "session-10")
	if !dec10.Allowed || !dec10.ReusedDemod {
		t.Fatalf("Session 10 on existing multiplex MUST BE ALLOWED via multiplex reuse even when all demods full")
	}
}

// 3. Golden Test: Single Cable RF Plane Conflict (High-H vs High-V on 1 Legacy Cable)
func TestGolden_FBC_LegacySingleCable_PlaneConflict(t *testing.T) {
	topology := buildVuPlusUno4K_FBC_SingleCable()
	allocator := NewAllocator(topology, EvaluationModeEnforce)
	runtime := NewRuntimeAllocation()

	// Session 1 tunes High-H (locks Input A to High-H)
	muxHighH := BuildSatMultiplexID(192, 0x00C00000, 0x0453, 0x0001, BandHigh, PolarizationHorizontal)
	dec1, err := allocator.Allocate(runtime, muxHighH, "session-1", AllocationOwnerXG2G)
	if err != nil || !dec1.Allowed {
		t.Fatalf("Session 1 expected ALLOW, got: %v", err)
	}

	// Session 2 tunes High-V (different polarization on single cable)
	muxHighV := BuildSatMultiplexID(192, 0x00C00000, 0x0421, 0x0001, BandHigh, PolarizationVertical)
	dec2 := allocator.CanAllocate(runtime, muxHighV, "session-2")
	if dec2.Allowed {
		t.Fatalf("Session 2 on High-V MUST BE REJECTED due to single cable plane conflict with active High-H")
	}

	// Session 3 tunes Low-H (different band on single cable)
	muxLowH := BuildSatMultiplexID(192, 0x00C00000, 0x0411, 0x0001, BandLow, PolarizationHorizontal)
	dec3 := allocator.CanAllocate(runtime, muxLowH, "session-3")
	if dec3.Allowed {
		t.Fatalf("Session 3 on Low-H MUST BE REJECTED due to single cable plane conflict with active High-H")
	}
}

// 4. Golden Test: Dual Cable Legacy FBC (2 RF Planes simultaneously active across Tuner A & B)
func TestGolden_FBC_LegacyDualCable_TwoPlanes(t *testing.T) {
	topology := buildVuPlusUno4K_FBC_DualCable()
	allocator := NewAllocator(topology, EvaluationModeEnforce)
	runtime := NewRuntimeAllocation()

	// Session 1 on High-H -> takes Input A
	muxHighH := BuildSatMultiplexID(192, 0x00C00000, 0x0453, 0x0001, BandHigh, PolarizationHorizontal)
	dec1, err := allocator.Allocate(runtime, muxHighH, "session-1", AllocationOwnerXG2G)
	if err != nil || !dec1.Allowed || dec1.InputID != "input_a" {
		t.Fatalf("Session 1 expected ALLOW on input_a, got input %s", dec1.InputID)
	}

	// Session 2 on High-V -> takes Input B (different physical cable!)
	muxHighV := BuildSatMultiplexID(192, 0x00C00000, 0x0421, 0x0001, BandHigh, PolarizationVertical)
	dec2, err := allocator.Allocate(runtime, muxHighV, "session-2", AllocationOwnerXG2G)
	if err != nil || !dec2.Allowed || dec2.InputID != "input_b" {
		t.Fatalf("Session 2 expected ALLOW on input_b, got input %s", dec2.InputID)
	}

	// Session 3 on another High-H transponder -> joins Input A's active plane
	muxHighH_2 := BuildSatMultiplexID(192, 0x00C00000, 0x0454, 0x0001, BandHigh, PolarizationHorizontal)
	dec3, err := allocator.Allocate(runtime, muxHighH_2, "session-3", AllocationOwnerXG2G)
	if err != nil || !dec3.Allowed || dec3.InputID != "input_a" {
		t.Fatalf("Session 3 expected ALLOW on input_a sharing High-H plane, got %s", dec3.InputID)
	}

	// Session 4 on another High-V transponder -> joins Input B's active plane
	muxHighV_2 := BuildSatMultiplexID(192, 0x00C00000, 0x0422, 0x0001, BandHigh, PolarizationVertical)
	dec4, err := allocator.Allocate(runtime, muxHighV_2, "session-4", AllocationOwnerXG2G)
	if err != nil || !dec4.Allowed || dec4.InputID != "input_b" {
		t.Fatalf("Session 4 expected ALLOW on input_b sharing High-V plane, got %s", dec4.InputID)
	}
}

// 5. Golden Test: Unicable SCR (8 Independent transponders across all 4 quadrants on 1 cable)
func TestGolden_FBC_Unicable8_FullAgility(t *testing.T) {
	inputUnicable := PhysicalInput{
		ID:           "input_unicable",
		Label:        "Unicable SCR LNB",
		DeliveryType: DeliveryUnicable1,
		UserBands:    8,
		Satellites:   []SatellitePosition{192},
	}
	demods := []Demodulator{
		{ID: "demod_a", InputID: "input_unicable", DVBTypes: []DVBType{DVBTypeSat}},
		{ID: "demod_b", InputID: "input_unicable", DVBTypes: []DVBType{DVBTypeSat}},
		{ID: "demod_c", InputID: "input_unicable", DVBTypes: []DVBType{DVBTypeSat}},
		{ID: "demod_d", InputID: "input_unicable", DVBTypes: []DVBType{DVBTypeSat}},
		{ID: "demod_e", InputID: "input_unicable", DVBTypes: []DVBType{DVBTypeSat}},
		{ID: "demod_f", InputID: "input_unicable", DVBTypes: []DVBType{DVBTypeSat}},
		{ID: "demod_g", InputID: "input_unicable", DVBTypes: []DVBType{DVBTypeSat}},
		{ID: "demod_h", InputID: "input_unicable", DVBTypes: []DVBType{DVBTypeSat}},
	}
	topology := ReceiverTopology{
		Model:        "Vu+ Uno 4K Unicable",
		Confidence:   ConfidenceVerified,
		Inputs:       []PhysicalInput{inputUnicable},
		Demodulators: demods,
	}

	allocator := NewAllocator(topology, EvaluationModeEnforce)
	runtime := NewRuntimeAllocation()

	// 8 transponders randomly distributed across all 4 quadrants (High-H, High-V, Low-H, Low-V)
	planes := []RFPlane{
		{SatPosition: 192, Band: BandHigh, Polarization: PolarizationHorizontal},
		{SatPosition: 192, Band: BandHigh, Polarization: PolarizationVertical},
		{SatPosition: 192, Band: BandLow, Polarization: PolarizationHorizontal},
		{SatPosition: 192, Band: BandLow, Polarization: PolarizationVertical},
		{SatPosition: 192, Band: BandHigh, Polarization: PolarizationHorizontal},
		{SatPosition: 192, Band: BandHigh, Polarization: PolarizationVertical},
		{SatPosition: 192, Band: BandLow, Polarization: PolarizationHorizontal},
		{SatPosition: 192, Band: BandLow, Polarization: PolarizationVertical},
	}

	for i, plane := range planes {
		mux := BuildSatMultiplexID(192, 0x00C00000, uint16(0x0500+i), 0x0001, plane.Band, plane.Polarization)
		sessID := "unicable-session-" + string(rune('1'+i))
		dec, err := allocator.Allocate(runtime, mux, sessID, AllocationOwnerXG2G)
		if err != nil || !dec.Allowed {
			t.Fatalf("Unicable session %d on plane %s expected ALLOW, got: %v", i+1, plane.String(), err)
		}
	}

	// 9th Unicable request exceeds 8 user bands -> REJECT
	mux9 := BuildSatMultiplexID(192, 0x00C00000, 0x0599, 0x0001, BandHigh, PolarizationHorizontal)
	dec9 := allocator.CanAllocate(runtime, mux9, "unicable-session-9")
	if dec9.Allowed {
		t.Fatalf("9th Unicable session must be rejected when all 8 SCR bands are occupied")
	}
}

// 6. Golden Test: External Receiver Allocations (HDMI Local TV + Background Timer Recording)
func TestGolden_ExternalReceiverUsage_ReclaimsDemod(t *testing.T) {
	topology := buildVuPlusUno4K_FBC_SingleCable()
	allocator := NewAllocator(topology, EvaluationModeEnforce)
	runtime := NewRuntimeAllocation()

	demodA := DemodulatorID("demod_a")
	demodB := DemodulatorID("demod_b")
	inputA := InputID("input_a")

	// Receiver is recording a movie locally on HDMI TV using Demod A and Demod B
	runtime.ExternalAllocations = []ExternalAllocation{
		{Source: "local_hdmi_viewing", DemodID: &demodA, InputID: &inputA},
		{Source: "enigma2_timer_dvr", DemodID: &demodB, InputID: &inputA},
	}

	// xg2g can now allocate at most 6 demods (demods C through H)
	for i := 0; i < 6; i++ {
		mux := BuildSatMultiplexID(192, 0x00C00000, uint16(0x0600+i), 0x0001, BandHigh, PolarizationHorizontal)
		sessID := "xg2g-session-" + string(rune('1'+i))
		dec, err := allocator.Allocate(runtime, mux, sessID, AllocationOwnerXG2G)
		if err != nil || !dec.Allowed {
			t.Fatalf("xg2g session %d expected ALLOW (slot %d/6), got: %v", i+1, i+1, err)
		}
	}

	// 7th unique mux exceeds remaining capacity -> REJECT
	mux7 := BuildSatMultiplexID(192, 0x00C00000, 0x0607, 0x0001, BandHigh, PolarizationHorizontal)
	dec7 := allocator.CanAllocate(runtime, mux7, "xg2g-session-7")
	if dec7.Allowed {
		t.Fatalf("xg2g session 7 must be rejected because 2 demods are occupied by external receiver usage")
	}
}

// 7. Golden Test: Confidence Observed in Audit-Only Mode (Fail-Open)
func TestGolden_Confidence_AuditOnly_Permissive(t *testing.T) {
	topology := buildVuPlusUno4K_FBC_SingleCable()
	topology.Confidence = ConfidenceObserved

	allocator := NewAllocator(topology, EvaluationModeAuditOnly)
	runtime := NewRuntimeAllocation()

	// Fill all 8 demods
	for i := 0; i < 8; i++ {
		mux := BuildSatMultiplexID(192, 0x00C00000, uint16(0x0700+i), 0x0001, BandHigh, PolarizationHorizontal)
		_, _ = allocator.Allocate(runtime, mux, "sess-"+string(rune('1'+i)), AllocationOwnerXG2G)
	}

	// 9th session on 9th unique mux in Audit-Only mode: Returns ALLOW with Audit-Only warning
	mux9 := BuildSatMultiplexID(192, 0x00C00000, 0x0709, 0x0001, BandHigh, PolarizationHorizontal)
	dec9 := allocator.CanAllocate(runtime, mux9, "sess-9")
	if !dec9.Allowed {
		t.Fatalf("Audit-Only mode must allow stream even under overload")
	}
	if dec9.EvaluationMode != EvaluationModeAuditOnly {
		t.Fatalf("expected EvaluationModeAuditOnly, got %s", dec9.EvaluationMode)
	}
}
