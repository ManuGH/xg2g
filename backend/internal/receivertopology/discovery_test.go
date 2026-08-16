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

	// Active xg2g session owns Tuner C's serviceRef
	activeXG2G := map[string]bool{
		"1:0:19:283F:3FB:1:C00000:0:0:0:": true,
	}

	external := ExtractExternalAllocations(about, activeXG2G)

	if len(external) != 2 {
		t.Fatalf("expected 2 external allocations (ignoring xg2g session on Tuner C), got %d", len(external))
	}

	if external[0].Source != "hdmi_live_tv" {
		t.Fatalf("expected hdmi_live_tv on first external allocation, got %s", external[0].Source)
	}
	if external[1].Source != "local_timer_dvr" {
		t.Fatalf("expected local_timer_dvr on second external allocation, got %s", external[1].Source)
	}
}
