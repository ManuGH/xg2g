// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/receivertopology"
	"gopkg.in/yaml.v3"
)

// Topology of the reference receiver: Vu+ Uno4K, FBC front-end, two coaxial
// runs from a quad LNB, both pointed at 19.2°E (Enigma2 diseqcA=192).
//
// A quad LNB output behaves as a universal LNB: it carries exactly one
// band/polarisation quadrant at a time. FBC then demodulates several
// transponders out of whichever quadrant that cable currently delivers, so the
// binding limit is two independent quadrants, not the demodulator count.
//
// The 4+4 demodulator split is deliberately conservative. The FBC driver
// assigns demodulators dynamically and the receiver exposes no mapping over
// OpenWebIF (/proc/bus/nim_sockets is not served, /api/settings carries only
// the eight diseqc keys), so this under-promises rather than over-promises:
// blocking a stream that would have worked is recoverable, admitting one that
// cannot be tuned is not.
const vuUno4KTopologyYAML = `
mode: enforce
model: "Vu+ Uno4K"
inputs:
  - id: input_a
    label: "Tuner A (FBC In) — Quad-LNB output 1"
    delivery_type: legacy_universal
    satellites: [192]
  - id: input_b
    label: "Tuner B (FBC In) — Quad-LNB output 2"
    delivery_type: legacy_universal
    satellites: [192]
demodulators:
  - {id: tuner_a, input_id: input_a, dvb_types: [DVB_S]}
  - {id: tuner_b, input_id: input_a, dvb_types: [DVB_S], is_fbc_virtual: true}
  - {id: tuner_c, input_id: input_a, dvb_types: [DVB_S], is_fbc_virtual: true}
  - {id: tuner_d, input_id: input_a, dvb_types: [DVB_S], is_fbc_virtual: true}
  - {id: tuner_e, input_id: input_b, dvb_types: [DVB_S]}
  - {id: tuner_f, input_id: input_b, dvb_types: [DVB_S], is_fbc_virtual: true}
  - {id: tuner_g, input_id: input_b, dvb_types: [DVB_S], is_fbc_virtual: true}
  - {id: tuner_h, input_id: input_b, dvb_types: [DVB_S], is_fbc_virtual: true}
`

func TestVuUno4KTopology_ReachesVerifiedEnforce(t *testing.T) {
	var dto ReceiverTopologyFileConfig
	if err := yaml.Unmarshal([]byte(vuUno4KTopologyYAML), &dto); err != nil {
		t.Fatalf("topology YAML must unmarshal: %v", err)
	}

	topo, mode, configured, err := ToDomainTopology(&dto)
	if err != nil {
		t.Fatalf("ToDomainTopology: %v", err)
	}
	if !configured {
		t.Fatal("topology must register as configured")
	}
	if mode != receivertopology.EvaluationModeEnforce {
		t.Errorf("mode = %s, want ENFORCE", mode)
	}
	if topo.Confidence != receivertopology.ConfidenceVerified {
		t.Errorf("confidence = %s, want VERIFIED — ENFORCE is refused below it",
			topo.Confidence)
	}

	if got := len(topo.Demodulators); got != 8 {
		t.Errorf("demodulators = %d, want 8 (FBC front-end)", got)
	}

	// Two cables, each carrying one quadrant at a time.
	if got := topo.EffectiveTunerCapacity(); got != 2 {
		t.Errorf("EffectiveTunerCapacity = %d, want 2 independent RF planes", got)
	}

	// The whole point of the config: ENFORCE must actually be accepted.
	svc, err := receivertopology.NewService(topo, mode)
	if err != nil {
		t.Fatalf("NewService(ENFORCE) refused a verified topology: %v", err)
	}
	if svc.Mode() != receivertopology.EvaluationModeEnforce {
		t.Errorf("active mode = %s, want ENFORCE (not silently downgraded)", svc.Mode())
	}
}
