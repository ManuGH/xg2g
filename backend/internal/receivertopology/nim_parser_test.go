// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package receivertopology

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Real authentic OpenATV 7.x / VTi Enigma2 settings fixtures

const fixtureDualLegacyFBC = `
config.misc.radiopic=/usr/share/enigma2/radio.mvi
config.Nims.0.configMode=simple
config.Nims.0.diseqcmode=single
config.Nims.0.diseqcA=192
config.Nims.1.configMode=simple
config.Nims.1.diseqcmode=single
config.Nims.1.diseqcA=192
config.Nims.2.configMode=auto
config.Nims.3.configMode=auto
config.Nims.4.configMode=auto
config.Nims.5.configMode=auto
config.Nims.6.configMode=auto
config.Nims.7.configMode=auto
config.plugins.epgsearch.encoding=UTF-8
`

const fixtureSingleCableLoopthroughFBC = `
config.misc.firstrun=false
config.Nims.0.configMode=simple
config.Nims.0.diseqcmode=single
config.Nims.0.diseqcA=192
config.Nims.1.configMode=loopthrough
config.Nims.1.connectedTo=0
config.Nims.2.configMode=auto
config.Nims.3.configMode=auto
config.Nims.4.configMode=auto
config.Nims.5.configMode=auto
config.Nims.6.configMode=auto
config.Nims.7.configMode=auto
`

const fixtureUnicableJESS = `
config.Nims.0.configMode=unicable
config.Nims.0.unicableMode=unicable_matrix
config.Nims.0.unicable.manufacturer=Inverto
config.Nims.0.unicable.model=IDLU-UST110-CUO1O-32PP
config.Nims.0.unicable.scr=0
config.Nims.0.unicable.frequency=1210
config.Nims.1.configMode=unicable
config.Nims.1.unicableMode=unicable_matrix
config.Nims.1.unicable.scr=1
config.Nims.1.unicable.frequency=1420
config.Nims.2.configMode=unicable
config.Nims.2.unicableMode=unicable_matrix
config.Nims.2.unicable.scr=2
config.Nims.2.unicable.frequency=1680
config.Nims.3.configMode=unicable
config.Nims.3.unicableMode=unicable_matrix
config.Nims.3.unicable.scr=3
config.Nims.3.unicable.frequency=2040
config.Nims.4.configMode=unicable
config.Nims.4.unicableMode=unicable_matrix
config.Nims.4.unicable.scr=4
config.Nims.4.unicable.frequency=984
config.Nims.5.configMode=unicable
config.Nims.5.unicableMode=unicable_matrix
config.Nims.5.unicable.scr=5
config.Nims.5.unicable.frequency=1020
config.Nims.6.configMode=unicable
config.Nims.6.unicableMode=unicable_matrix
config.Nims.6.unicable.scr=6
config.Nims.6.unicable.frequency=1056
config.Nims.7.configMode=unicable
config.Nims.7.unicableMode=unicable_matrix
config.Nims.7.unicable.scr=7
config.Nims.7.unicable.frequency=1092
`

const fixtureDiscreteDualSat = `
config.Nims.0.configMode=simple
config.Nims.0.diseqcmode=single
config.Nims.0.diseqcA=192
config.Nims.1.configMode=equal
config.Nims.1.equal=0
`

const fixtureHybridCombo = `
config.Nims.0.configMode=simple
config.Nims.0.diseqcmode=single
config.Nims.0.diseqcA=192
config.Nims.1.configMode=enabled
config.Nims.1.dvbType=DVB-C
`

func TestParseNIMSettings_DualLegacyFBC(t *testing.T) {
	topo, err := ParseNIMSettings(fixtureDualLegacyFBC)
	require.NoError(t, err)
	require.Equal(t, ConfidenceObserved, topo.Confidence)

	// Must discover 2 distinct physical legacy inputs
	require.Len(t, topo.Inputs, 2)
	require.Equal(t, InputID("input_a"), topo.Inputs[0].ID)
	require.Equal(t, DeliveryLegacyUniversal, topo.Inputs[0].DeliveryType)
	require.Equal(t, InputID("input_b"), topo.Inputs[1].ID)
	require.Equal(t, DeliveryLegacyUniversal, topo.Inputs[1].DeliveryType)

	// Must discover 8 demodulators (A..H), with C..H marked as FBC virtual
	require.Len(t, topo.Demodulators, 8)
	require.Equal(t, DemodulatorID("tuner_a"), topo.Demodulators[0].ID)
	require.False(t, topo.Demodulators[0].IsFBCVirtual)
	require.Equal(t, InputID("input_a"), topo.Demodulators[0].InputID)

	require.Equal(t, DemodulatorID("tuner_b"), topo.Demodulators[1].ID)
	require.False(t, topo.Demodulators[1].IsFBCVirtual)
	require.Equal(t, InputID("input_b"), topo.Demodulators[1].InputID)

	for i := 2; i < 8; i++ {
		require.True(t, topo.Demodulators[i].IsFBCVirtual, "demodulator %d must be virtual", i)
	}
}

func TestParseNIMSettings_SingleCableLoopthroughFBC(t *testing.T) {
	topo, err := ParseNIMSettings(fixtureSingleCableLoopthroughFBC)
	require.NoError(t, err)
	require.Equal(t, ConfidenceObserved, topo.Confidence)

	// Must discover strictly 1 physical input (no phantom input_b!)
	require.Len(t, topo.Inputs, 1)
	require.Equal(t, InputID("input_a"), topo.Inputs[0].ID)
	require.Equal(t, DeliveryLegacyUniversal, topo.Inputs[0].DeliveryType)

	// Demod B must be linked to input_a
	require.Len(t, topo.Demodulators, 8)
	require.Equal(t, InputID("input_a"), topo.Demodulators[1].InputID)
}

func TestParseNIMSettings_UnicableJESS(t *testing.T) {
	topo, err := ParseNIMSettings(fixtureUnicableJESS)
	require.NoError(t, err)
	require.Equal(t, ConfidenceObserved, topo.Confidence)

	// Single physical cable delivering Unicable with 8 SCR User Bands
	require.Len(t, topo.Inputs, 1)
	require.Equal(t, InputID("input_a"), topo.Inputs[0].ID)
	require.Equal(t, DeliveryUnicable1, topo.Inputs[0].DeliveryType)
	require.Equal(t, 8, topo.Inputs[0].UserBands)

	require.Len(t, topo.Demodulators, 8)
	for _, d := range topo.Demodulators {
		require.Equal(t, InputID("input_a"), d.InputID)
	}
}

func TestParseNIMSettings_DiscreteDualSat(t *testing.T) {
	topo, err := ParseNIMSettings(fixtureDiscreteDualSat)
	require.NoError(t, err)
	require.Equal(t, ConfidenceObserved, topo.Confidence)

	require.Len(t, topo.Inputs, 2)
	require.Len(t, topo.Demodulators, 2)
	require.False(t, topo.Demodulators[0].IsFBCVirtual)
	require.False(t, topo.Demodulators[1].IsFBCVirtual)
}

func TestParseNIMSettings_HybridCombo(t *testing.T) {
	topo, err := ParseNIMSettings(fixtureHybridCombo)
	require.NoError(t, err)
	require.Equal(t, ConfidenceObserved, topo.Confidence)

	require.Len(t, topo.Inputs, 2)
	require.Len(t, topo.Demodulators, 2)
	require.Equal(t, DVBTypeSat, topo.Demodulators[0].DVBTypes[0])
	require.Equal(t, DVBTypeCable, topo.Demodulators[1].DVBTypes[0])
}

func TestParseNIMSettings_InvalidAndEmpty(t *testing.T) {
	_, err := ParseNIMSettings("")
	require.ErrorIs(t, err, ErrNoNIMConfigFound)

	_, err = ParseNIMSettings("config.misc.firstrun=false\n")
	require.ErrorIs(t, err, ErrNoNIMConfigFound)

	_, err = ParseNIMSettings("config.Nims.0.configMode=nothing\nconfig.Nims.1.configMode=nothing\n")
	require.Error(t, err)
}
