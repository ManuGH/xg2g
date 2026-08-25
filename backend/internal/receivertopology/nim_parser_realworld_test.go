// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package receivertopology

import "testing"

// Verbatim tuner section of /etc/enigma2/settings from a Vu+ Uno4K running
// OpenATV 8.0.0-beta with OpenWebif 2.4.0, captured 2026-08-17.
//
// Two properties of this dump broke the parser and are the point of the test:
// keys are namespaced under the delivery system ("dvbs."), and every value
// still at its default — dvbType and configMode among them — is simply absent,
// because Enigma2 only persists what was changed.
const realVuUno4KSettings = `
config.misc.epgcache_filename=/media/hdd/epg.dat
config.Nims.0.dvbs.diseqcA=192
config.Nims.0.dvbs.diseqcB=3601
config.Nims.0.dvbs.diseqcC=3601
config.Nims.0.dvbs.diseqcD=3601
config.Nims.1.dvbs.diseqcA=192
config.Nims.1.dvbs.diseqcB=3601
config.Nims.1.dvbs.diseqcC=3601
config.Nims.1.dvbs.diseqcD=3601
config.channelSelection.screenStyle=ChannelSelectionDefault
`

func TestParseNIMSettings_RealVuUno4KNamespacedKeys(t *testing.T) {
	topo, err := ParseNIMSettings(realVuUno4KSettings)
	if err != nil {
		t.Fatalf("real receiver settings must parse, got: %v", err)
	}

	if len(topo.Demodulators) != 2 {
		t.Fatalf("expected 2 demodulators for Nims 0 and 1, got %d", len(topo.Demodulators))
	}
	if len(topo.Inputs) != 2 {
		t.Fatalf("expected 2 physical inputs, got %d", len(topo.Inputs))
	}

	for _, demod := range topo.Demodulators {
		if len(demod.DVBTypes) != 1 || demod.DVBTypes[0] != DVBTypeSat {
			t.Errorf("demod %s: expected DVB-S inferred from the dvbs key namespace, got %v",
				demod.ID, demod.DVBTypes)
		}
	}

	for _, input := range topo.Inputs {
		if input.DeliveryType != DeliveryLegacyUniversal {
			t.Errorf("input %s: no unicable keys present, expected legacy universal, got %s",
				input.ID, input.DeliveryType)
		}
	}
}

// The flat layout older images write must keep parsing identically.
func TestParseNIMSettings_FlatKeysStillParse(t *testing.T) {
	const flat = `
config.Nims.0.configMode=simple
config.Nims.0.diseqcA=192
config.Nims.1.configMode=simple
config.Nims.1.diseqcA=192
`
	topo, err := ParseNIMSettings(flat)
	if err != nil {
		t.Fatalf("flat settings must parse, got: %v", err)
	}
	if len(topo.Demodulators) != 2 {
		t.Fatalf("expected 2 demodulators, got %d", len(topo.Demodulators))
	}
	for _, demod := range topo.Demodulators {
		if len(demod.DVBTypes) != 1 || demod.DVBTypes[0] != DVBTypeSat {
			t.Errorf("demod %s: expected DVB-S, got %v", demod.ID, demod.DVBTypes)
		}
	}
}

// An absent configMode means "default", not "unconfigured": the parser used to
// reject the whole file over it.
func TestParseNIMSettings_MissingConfigModeDefaultsToSimple(t *testing.T) {
	const minimal = `config.Nims.0.dvbs.diseqcA=192`

	topo, err := ParseNIMSettings(minimal)
	if err != nil {
		t.Fatalf("a slot carrying only a diseqc key must still parse, got: %v", err)
	}
	if len(topo.Demodulators) != 1 {
		t.Fatalf("expected 1 demodulator, got %d", len(topo.Demodulators))
	}
}
