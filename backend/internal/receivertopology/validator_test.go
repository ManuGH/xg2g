// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package receivertopology

import "testing"

func TestValidator_ValidTopology(t *testing.T) {
	topology := buildVuPlusUno4K_FBC_SingleCable()
	if err := Validate(topology); err != nil {
		t.Fatalf("expected valid topology, got: %v", err)
	}
}

func TestValidator_MissingInput(t *testing.T) {
	topology := ReceiverTopology{
		Model: "Bad Model",
		Inputs: []PhysicalInput{
			{ID: "input_a", DeliveryType: DeliveryLegacyUniversal},
		},
		Demodulators: []Demodulator{
			{ID: "demod_1", InputID: "input_NON_EXISTENT"},
		},
	}

	err := Validate(topology)
	if err == nil {
		t.Fatalf("expected validation error for non-existent input, got nil")
	}
}

func TestValidator_UnicableZeroUserBands(t *testing.T) {
	topology := ReceiverTopology{
		Model: "Bad Unicable",
		Inputs: []PhysicalInput{
			{ID: "input_unicable", DeliveryType: DeliveryUnicable1, UserBands: 0},
		},
		Demodulators: []Demodulator{
			{ID: "demod_1", InputID: "input_unicable"},
		},
	}

	err := Validate(topology)
	if err == nil {
		t.Fatalf("expected validation error for Unicable with 0 user bands, got nil")
	}
}

func TestValidator_DuplicateIDs(t *testing.T) {
	topology := ReceiverTopology{
		Model: "Duplicate Model",
		Inputs: []PhysicalInput{
			{ID: "input_a", DeliveryType: DeliveryLegacyUniversal},
			{ID: "input_a", DeliveryType: DeliveryLegacyUniversal},
		},
		Demodulators: []Demodulator{
			{ID: "demod_1", InputID: "input_a"},
		},
	}

	err := Validate(topology)
	if err == nil {
		t.Fatalf("expected validation error for duplicate input ID, got nil")
	}
}
