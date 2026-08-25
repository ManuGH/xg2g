// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package receivertopology

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/openwebif"
)

func TestDiscovery_VuPlusUno4K_FBC(t *testing.T) {
	about := &openwebif.AboutInfo{}
	about.Info.Model = "Vu+ Uno 4K"
	about.Info.Tuners = []openwebif.AboutTuner{
		{Name: "Tuner A", Type: "Vuplus DVB-S NIM(45208 FBC) (DVB-S2)"},
		{Name: "Tuner B", Type: "Vuplus DVB-S NIM(45208 FBC) (DVB-S2)"},
		{Name: "Tuner C", Type: "Vuplus DVB-S NIM(45208 FBC) (DVB-S2)"},
		{Name: "Tuner D", Type: "Vuplus DVB-S NIM(45208 FBC) (DVB-S2)"},
		{Name: "Tuner E", Type: "Vuplus DVB-S NIM(45208 FBC) (DVB-S2)"},
		{Name: "Tuner F", Type: "Vuplus DVB-S NIM(45208 FBC) (DVB-S2)"},
		{Name: "Tuner G", Type: "Vuplus DVB-S NIM(45208 FBC) (DVB-S2)"},
		{Name: "Tuner H", Type: "Vuplus DVB-S NIM(45208 FBC) (DVB-S2)"},
	}

	topology := DiscoverTopology(about)

	if topology.Confidence != ConfidenceObserved {
		t.Fatalf("expected ConfidenceObserved for discovered topology, got %s", topology.Confidence)
	}
	if len(topology.Demodulators) != 8 {
		t.Fatalf("expected 8 demods discovered for FBC, got %d", len(topology.Demodulators))
	}
	if topology.Model != "Vu+ Uno 4K" {
		t.Fatalf("expected model Vu+ Uno 4K, got %s", topology.Model)
	}
	// Verify generic discovery leaves Satellites unrestricted (nil), not hardcoded to 192
	if len(topology.Inputs) != 1 || topology.Inputs[0].Satellites != nil {
		t.Fatalf("expected unrestricted satellite list on generic discovery, got %v", topology.Inputs[0].Satellites)
	}
}

func TestDiscovery_ExternalAllocations(t *testing.T) {
	about := &openwebif.AboutInfo{}
	about.Info.Tuners = []openwebif.AboutTuner{
		{
			Name: "Tuner A",
			Type: "Vuplus DVB-S NIM(45208 FBC) (DVB-S2)",
			Live: "1:0:19:283D:3FB:1:C00000:0:0:0:", // Das Erste HD on HDMI
		},
		{
			Name: "Tuner B",
			Type: "Vuplus DVB-S NIM(45208 FBC) (DVB-S2)",
			Rec:  "1:0:19:283E:3FB:1:C00000:0:0:0:", // ZDF HD Recording locally
		},
		{
			Name:   "Tuner C",
			Type:   "Vuplus DVB-S NIM(45208 FBC) (DVB-S2)",
			Stream: "1:0:19:283F:3FB:1:C00000:0:0:0:", // xg2g stream
		},
	}

	topology := DiscoverTopology(about)

	// Demod C is occupied by our own active xg2g session
	activeDemods := map[DemodulatorID]bool{
		"tuner_c": true,
	}

	external := ExtractExternalAllocations(about, topology, activeDemods)

	if len(external) != 2 {
		t.Fatalf("expected 2 external allocations (ignoring xg2g session on Tuner C), got %d", len(external))
	}

	if external[0].Source != "hdmi_live_tv" {
		t.Fatalf("expected hdmi_live_tv on first external allocation, got %s", external[0].Source)
	}
	if external[0].InputID == nil || *external[0].InputID != "input_a" {
		t.Fatalf("expected InputID input_a resolved for external allocation 0, got %v", external[0].InputID)
	}
	if external[1].Source != "local_timer_dvr" {
		t.Fatalf("expected local_timer_dvr on second external allocation, got %s", external[1].Source)
	}
}

func TestDiscovery_StreamPresence_Matrix(t *testing.T) {
	topology := ReceiverTopology{
		Inputs: []PhysicalInput{
			{ID: "input_a", DeliveryType: DeliveryLegacyUniversal},
		},
		Demodulators: []Demodulator{
			{ID: "tuner_a", InputID: "input_a", DVBTypes: []DVBType{DVBTypeSat}},
		},
	}

	// 1. Unknown streams payload (nil) -> fail-safe retains t.Stream
	aboutUnknown := &openwebif.AboutInfo{}
	aboutUnknown.Info.Streams = nil
	aboutUnknown.Info.Tuners = []openwebif.AboutTuner{
		{Name: "Tuner A", Type: "Vuplus DVB-S", Stream: "1:0:19:283F:3FB:1:C00000:0:0:0:"},
	}
	allocsUnknown := ExtractExternalAllocations(aboutUnknown, topology, nil)
	if len(allocsUnknown) != 1 || allocsUnknown[0].Source != "external_stream_client" {
		t.Fatalf("expected fail-safe external allocation on unknown streams payload, got %v", allocsUnknown)
	}

	// 2. Explicitly empty streams payload ([]any{}) -> suppresses stale t.Stream
	aboutEmpty := &openwebif.AboutInfo{}
	aboutEmpty.Info.Streams = []any{}
	aboutEmpty.Info.Tuners = []openwebif.AboutTuner{
		{Name: "Tuner A", Type: "Vuplus DVB-S", Stream: "1:0:19:283F:3FB:1:C00000:0:0:0:"},
	}
	allocsEmpty := ExtractExternalAllocations(aboutEmpty, topology, nil)
	if len(allocsEmpty) != 0 {
		t.Fatalf("expected 0 external allocations when streams payload is explicitly empty, got %d", len(allocsEmpty))
	}

	// 3. Active streams payload -> confirms t.Stream
	aboutActive := &openwebif.AboutInfo{}
	aboutActive.Info.Streams = []any{"1:0:19:283F:3FB:1:C00000:0:0:0:"}
	aboutActive.Info.Tuners = []openwebif.AboutTuner{
		{Name: "Tuner A", Type: "Vuplus DVB-S", Stream: "1:0:19:283F:3FB:1:C00000:0:0:0:"},
	}
	allocsActive := ExtractExternalAllocations(aboutActive, topology, nil)
	if len(allocsActive) != 1 || allocsActive[0].Source != "external_stream_client" {
		t.Fatalf("expected active external stream client allocation, got %v", allocsActive)
	}
}

func TestTopology_EffectiveTunerCapacity(t *testing.T) {
	// 1. Single Legacy Cable with 8 FBC demods -> Capacity = 1
	fbcSingle := ReceiverTopology{
		Inputs: []PhysicalInput{
			{ID: "input_a", DeliveryType: DeliveryLegacyUniversal},
		},
		Demodulators: []Demodulator{
			{ID: "tuner_a", InputID: "input_a"},
			{ID: "tuner_b", InputID: "input_a"},
			{ID: "tuner_c", InputID: "input_a"},
			{ID: "tuner_d", InputID: "input_a"},
			{ID: "tuner_e", InputID: "input_a"},
			{ID: "tuner_f", InputID: "input_a"},
			{ID: "tuner_g", InputID: "input_a"},
			{ID: "tuner_h", InputID: "input_a"},
		},
	}
	if cap := fbcSingle.EffectiveTunerCapacity(); cap != 1 {
		t.Fatalf("expected single legacy FBC capacity 1, got %d", cap)
	}

	// 2. Dual Legacy Cables with 8 FBC demods -> Capacity = 2
	fbcDual := ReceiverTopology{
		Inputs: []PhysicalInput{
			{ID: "input_a", DeliveryType: DeliveryLegacyUniversal},
			{ID: "input_b", DeliveryType: DeliveryLegacyUniversal},
		},
		Demodulators: []Demodulator{
			{ID: "tuner_a", InputID: "input_a"},
			{ID: "tuner_b", InputID: "input_b"},
			{ID: "tuner_c", InputID: "input_a"},
			{ID: "tuner_d", InputID: "input_a"},
			{ID: "tuner_e", InputID: "input_b"},
			{ID: "tuner_f", InputID: "input_b"},
			{ID: "tuner_g", InputID: "input_a"},
			{ID: "tuner_h", InputID: "input_b"},
		},
	}
	if cap := fbcDual.EffectiveTunerCapacity(); cap != 2 {
		t.Fatalf("expected dual legacy FBC capacity 2, got %d", cap)
	}

	// 3. Unicable with 8 User Bands -> Capacity = 8
	unicable := ReceiverTopology{
		Inputs: []PhysicalInput{
			{ID: "input_a", DeliveryType: DeliveryUnicable1, UserBands: 8},
		},
		Demodulators: []Demodulator{
			{ID: "tuner_a", InputID: "input_a"},
			{ID: "tuner_b", InputID: "input_a"},
			{ID: "tuner_c", InputID: "input_a"},
			{ID: "tuner_d", InputID: "input_a"},
			{ID: "tuner_e", InputID: "input_a"},
			{ID: "tuner_f", InputID: "input_a"},
			{ID: "tuner_g", InputID: "input_a"},
			{ID: "tuner_h", InputID: "input_a"},
		},
	}
	if cap := unicable.EffectiveTunerCapacity(); cap != 8 {
		t.Fatalf("expected Unicable 8 user bands capacity 8, got %d", cap)
	}

	// 4. Quad DVB-C Tuners -> Capacity = 4
	cable := ReceiverTopology{
		Inputs: []PhysicalInput{
			{ID: "input_cable", DeliveryType: DeliveryCable},
		},
		Demodulators: []Demodulator{
			{ID: "tuner_a", InputID: "input_cable"},
			{ID: "tuner_b", InputID: "input_cable"},
			{ID: "tuner_c", InputID: "input_cable"},
			{ID: "tuner_d", InputID: "input_cable"},
		},
	}
	if cap := cable.EffectiveTunerCapacity(); cap != 4 {
		t.Fatalf("expected Quad DVB-C capacity 4, got %d", cap)
	}
}
