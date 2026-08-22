package receivertopology

import (
	"os"
	"path/filepath"
	"testing"
)

func BuildVuPlusUno4K_FBC_SingleCable() ReceiverTopology {
	return ReceiverTopology{
		Model:      "Vu+ Uno 4K SE (FBC Legacy)",
		Confidence: ConfidenceVerified,
		Inputs: []PhysicalInput{
			{
				ID:           "input_a",
				DeliveryType: DeliveryLegacyUniversal,
				Satellites:   []SatellitePosition{192},
			},
		},
		Demodulators: []Demodulator{
			{ID: "demod_0", InputID: "input_a", DVBTypes: []DVBType{DVBTypeSat}},
			{ID: "demod_1", InputID: "input_a", DVBTypes: []DVBType{DVBTypeSat}},
			{ID: "demod_2", InputID: "input_a", DVBTypes: []DVBType{DVBTypeSat}},
			{ID: "demod_3", InputID: "input_a", DVBTypes: []DVBType{DVBTypeSat}},
			{ID: "demod_4", InputID: "input_a", DVBTypes: []DVBType{DVBTypeSat}},
			{ID: "demod_5", InputID: "input_a", DVBTypes: []DVBType{DVBTypeSat}},
			{ID: "demod_6", InputID: "input_a", DVBTypes: []DVBType{DVBTypeSat}},
			{ID: "demod_7", InputID: "input_a", DVBTypes: []DVBType{DVBTypeSat}},
		},
	}
}

func BuildVuPlusUno4K_FBC_DualCable() ReceiverTopology {
	return ReceiverTopology{
		Model:      "Vu+ Uno 4K SE (FBC Dual-Cable)",
		Confidence: ConfidenceVerified,
		Inputs: []PhysicalInput{
			{
				ID:           "input_a",
				DeliveryType: DeliveryLegacyUniversal,
				Satellites:   []SatellitePosition{192},
			},
			{
				ID:           "input_b",
				DeliveryType: DeliveryLegacyUniversal,
				Satellites:   []SatellitePosition{192},
			},
		},
		Demodulators: []Demodulator{
			{ID: "demod_0", InputID: "input_a", DVBTypes: []DVBType{DVBTypeSat}},
			{ID: "demod_1", InputID: "input_a", DVBTypes: []DVBType{DVBTypeSat}},
			{ID: "demod_2", InputID: "input_a", DVBTypes: []DVBType{DVBTypeSat}},
			{ID: "demod_3", InputID: "input_a", DVBTypes: []DVBType{DVBTypeSat}},
			{ID: "demod_4", InputID: "input_b", DVBTypes: []DVBType{DVBTypeSat}},
			{ID: "demod_5", InputID: "input_b", DVBTypes: []DVBType{DVBTypeSat}},
			{ID: "demod_6", InputID: "input_b", DVBTypes: []DVBType{DVBTypeSat}},
			{ID: "demod_7", InputID: "input_b", DVBTypes: []DVBType{DVBTypeSat}},
		},
	}
}

func buildVuPlusUno4K_FBC_SingleCable() ReceiverTopology {
	return BuildVuPlusUno4K_FBC_SingleCable()
}

func buildVuPlusUno4K_FBC_DualCable() ReceiverTopology {
	return BuildVuPlusUno4K_FBC_DualCable()
}

// seedReceiverTransponders gives a service the transponder facts of the real
// Vu+ Uno 4K this project is developed against, read from a capture of that
// receiver's own service database.
//
// Tests that assert on RF planes need carriers that actually exist: a service
// created with no facts resolves nothing, and one seeded from invented frequencies
// only proves the test agrees with the fiction rather than with the hardware.
func seedReceiverTransponders(t *testing.T, svc *Service) {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("topologytest", "lamedb_v4_vuuno4k.txt"))
	if err != nil {
		t.Fatalf("read captured service database: %v", err)
	}

	registry := NewTransponderRegistry()
	snap, err := registry.LoadLamedbBytes(data)
	if err != nil {
		t.Fatalf("load captured service database: %v", err)
	}
	if len(snap.Transponders) == 0 {
		t.Fatal("captured service database yielded no transponders")
	}

	svc.SetResolver(registry)
}
