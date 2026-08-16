package receivertopology

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
