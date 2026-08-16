// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/receivertopology"
	"github.com/stretchr/testify/require"
)

func TestToDomainTopology_NilOrEmpty(t *testing.T) {
	_, _, configured, err := ToDomainTopology(nil)
	require.NoError(t, err)
	require.False(t, configured)

	_, _, configured, err = ToDomainTopology(&ReceiverTopologyFileConfig{})
	require.NoError(t, err)
	require.False(t, configured)
}

func TestToDomainTopology_DualLegacyVerified(t *testing.T) {
	dto := &ReceiverTopologyFileConfig{
		Mode:  "enforce",
		Model: "Vu+ Uno 4K SE (Dual Legacy)",
		Inputs: []PhysicalInputFileConfig{
			{ID: "input_a", Label: "Tuner A (Port 1)", DeliveryType: "legacy_universal"},
			{ID: "input_b", Label: "Tuner B (Port 2)", DeliveryType: "legacy_universal"},
		},
		Demodulators: []DemodulatorFileConfig{
			{ID: "demod_a", InputID: "input_a", DVBTypes: []string{"dvb-s2"}},
			{ID: "demod_b", InputID: "input_b", DVBTypes: []string{"dvb-s2"}},
			{ID: "demod_c", InputID: "input_a", DVBTypes: []string{"dvb-s2"}, IsFBCVirtual: true},
			{ID: "demod_d", InputID: "input_a", DVBTypes: []string{"dvb-s2"}, IsFBCVirtual: true},
			{ID: "demod_e", InputID: "input_a", DVBTypes: []string{"dvb-s2"}, IsFBCVirtual: true},
			{ID: "demod_f", InputID: "input_a", DVBTypes: []string{"dvb-s2"}, IsFBCVirtual: true},
			{ID: "demod_g", InputID: "input_a", DVBTypes: []string{"dvb-s2"}, IsFBCVirtual: true},
			{ID: "demod_h", InputID: "input_a", DVBTypes: []string{"dvb-s2"}, IsFBCVirtual: true},
		},
	}

	topo, mode, configured, err := ToDomainTopology(dto)
	require.NoError(t, err)
	require.True(t, configured)
	require.Equal(t, receivertopology.ConfidenceVerified, topo.Confidence)
	require.Equal(t, receivertopology.EvaluationModeEnforce, mode)
	require.Equal(t, "Vu+ Uno 4K SE (Dual Legacy)", topo.Model)
	require.Len(t, topo.Inputs, 2)
	require.Len(t, topo.Demodulators, 8)
}

func TestToDomainTopology_UnicableJESSVerified(t *testing.T) {
	dto := &ReceiverTopologyFileConfig{
		Mode:  "audit_only",
		Model: "Vu+ Uno 4K SE (Unicable JESS)",
		Inputs: []PhysicalInputFileConfig{
			{ID: "input_a", Label: "Unicable SCR Feeder", DeliveryType: "unicable_2_jess", UserBands: 16},
		},
		Demodulators: []DemodulatorFileConfig{
			{ID: "demod_a", InputID: "input_a", DVBTypes: []string{"dvb-s2"}},
			{ID: "demod_b", InputID: "input_a", DVBTypes: []string{"dvb-s2"}},
			{ID: "demod_c", InputID: "input_a", DVBTypes: []string{"dvb-s2"}, IsFBCVirtual: true},
			{ID: "demod_d", InputID: "input_a", DVBTypes: []string{"dvb-s2"}, IsFBCVirtual: true},
			{ID: "demod_e", InputID: "input_a", DVBTypes: []string{"dvb-s2"}, IsFBCVirtual: true},
			{ID: "demod_f", InputID: "input_a", DVBTypes: []string{"dvb-s2"}, IsFBCVirtual: true},
			{ID: "demod_g", InputID: "input_a", DVBTypes: []string{"dvb-s2"}, IsFBCVirtual: true},
			{ID: "demod_h", InputID: "input_a", DVBTypes: []string{"dvb-s2"}, IsFBCVirtual: true},
		},
	}

	topo, mode, configured, err := ToDomainTopology(dto)
	require.NoError(t, err)
	require.True(t, configured)
	require.Equal(t, receivertopology.ConfidenceVerified, topo.Confidence)
	require.Equal(t, receivertopology.EvaluationModeAuditOnly, mode)
	require.Len(t, topo.Inputs, 1)
	require.Equal(t, 16, topo.Inputs[0].UserBands)
	require.Len(t, topo.Demodulators, 8)
}

func TestToDomainTopology_MissingDemodulators_HardRejection(t *testing.T) {
	dto := &ReceiverTopologyFileConfig{
		Inputs: []PhysicalInputFileConfig{
			{ID: "input_a", DeliveryType: "legacy_universal"},
		},
		// Missing Demodulators -> Must NOT invent phantom demodulators
	}

	_, _, _, err := ToDomainTopology(dto)
	require.ErrorContains(t, err, "must explicitly define demodulators")
}

func TestToDomainTopology_DeliveryTypeTypo_HardRejection(t *testing.T) {
	dto := &ReceiverTopologyFileConfig{
		Inputs: []PhysicalInputFileConfig{
			{ID: "input_a", DeliveryType: "unicable_2_jes"}, // Typo
		},
		Demodulators: []DemodulatorFileConfig{
			{ID: "demod_a", InputID: "input_a", DVBTypes: []string{"sat"}},
		},
	}

	_, _, _, err := ToDomainTopology(dto)
	require.ErrorContains(t, err, "invalid delivery_type")
}

func TestToDomainTopology_DVBTypeTypo_HardRejection(t *testing.T) {
	dto := &ReceiverTopologyFileConfig{
		Inputs: []PhysicalInputFileConfig{
			{ID: "input_a", DeliveryType: "legacy_universal"},
		},
		Demodulators: []DemodulatorFileConfig{
			{ID: "demod_a", InputID: "input_a", DVBTypes: []string{"dvb_invalid"}},
		},
	}

	_, _, _, err := ToDomainTopology(dto)
	require.ErrorContains(t, err, "invalid dvb_type")
}

func TestToDomainTopology_ModeTypo_HardRejection(t *testing.T) {
	dto := &ReceiverTopologyFileConfig{
		Mode: "strict_mode", // Typo / invalid mode
		Inputs: []PhysicalInputFileConfig{
			{ID: "input_a", DeliveryType: "legacy_universal"},
		},
		Demodulators: []DemodulatorFileConfig{
			{ID: "demod_a", InputID: "input_a", DVBTypes: []string{"sat"}},
		},
	}

	_, _, _, err := ToDomainTopology(dto)
	require.ErrorContains(t, err, "invalid receiver topology mode")
}

func TestToDomainTopology_DemodReferencesNonExistentInput(t *testing.T) {
	dto := &ReceiverTopologyFileConfig{
		Inputs: []PhysicalInputFileConfig{
			{ID: "input_a", DeliveryType: "legacy_universal"},
		},
		Demodulators: []DemodulatorFileConfig{
			{ID: "demod_a", InputID: "input_b", DVBTypes: []string{"sat"}}, // input_b doesn't exist
		},
	}

	_, _, _, err := ToDomainTopology(dto)
	require.ErrorContains(t, err, "references non-existent input_id")
}
