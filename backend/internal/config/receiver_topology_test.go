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
	}

	topo, mode, configured, err := ToDomainTopology(dto)
	require.NoError(t, err)
	require.True(t, configured)
	require.Equal(t, receivertopology.ConfidenceVerified, topo.Confidence)
	require.Equal(t, receivertopology.EvaluationModeEnforce, mode)
	require.Equal(t, "Vu+ Uno 4K SE (Dual Legacy)", topo.Model)
	require.Len(t, topo.Inputs, 2)
	require.Len(t, topo.Demodulators, 8) // Auto-expanded for multi-input FBC
}

func TestToDomainTopology_UnicableJESSVerified(t *testing.T) {
	dto := &ReceiverTopologyFileConfig{
		Mode:  "audit_only",
		Model: "Vu+ Uno 4K SE (Unicable JESS)",
		Inputs: []PhysicalInputFileConfig{
			{ID: "input_a", Label: "Unicable SCR Feeder", DeliveryType: "unicable_2_jess", UserBands: 16},
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

func TestToDomainTopology_InvalidInputs(t *testing.T) {
	dto := &ReceiverTopologyFileConfig{
		Inputs: []PhysicalInputFileConfig{
			{ID: "input_a", DeliveryType: "legacy_universal"},
			{ID: "input_a", DeliveryType: "legacy_universal"}, // Duplicate ID
		},
	}

	_, _, _, err := ToDomainTopology(dto)
	require.Error(t, err)
}
