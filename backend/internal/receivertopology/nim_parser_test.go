// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package receivertopology

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Representative Enigma2 Settings Fixtures (derived from OpenATV 7.x / VTi Enigma2 environments)

const fixtureRepresentativeDualLegacyFBC = `
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

const fixtureRepresentativeSingleCableLoopthroughFBC = `
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

const fixtureRepresentativeUnicableJESS = `
config.Nims.0.configMode=unicable
config.Nims.0.unicableMode=unicable_matrix
config.Nims.0.unicable.manufacturer=Inverto
config.Nims.0.unicable.model=IDLU-UST110-CUO1O-32PP (JESS/EN50607)
config.Nims.0.unicable.unicableFormat=jess
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

const fixtureRepresentativeUnicable1 = `
config.Nims.0.configMode=unicable
config.Nims.0.unicableMode=unicable_matrix
config.Nims.0.unicable.manufacturer=Inverto
config.Nims.0.unicable.model=IDLP-UST110-CUO1O-08PP (EN50494)
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
`

const fixtureRepresentativeDiscreteDualSat = `
config.Nims.0.configMode=simple
config.Nims.0.diseqcmode=single
config.Nims.0.diseqcA=192
config.Nims.1.configMode=equal
config.Nims.1.equal=0
`

const fixtureRepresentativeHybridCombo = `
config.Nims.0.configMode=simple
config.Nims.0.diseqcmode=single
config.Nims.0.diseqcA=192
config.Nims.1.configMode=enabled
config.Nims.1.dvbType=DVB-C
`

func TestParseNIMSettings_DualLegacyFBC(t *testing.T) {
	topo, err := ParseNIMSettings(fixtureRepresentativeDualLegacyFBC)
	require.NoError(t, err)
	require.Equal(t, ConfidenceObserved, topo.Confidence)

	// Must discover 2 distinct physical legacy inputs
	require.Len(t, topo.Inputs, 2)
	require.Equal(t, InputID("input_a"), topo.Inputs[0].ID)
	require.Equal(t, DeliveryLegacyUniversal, topo.Inputs[0].DeliveryType)
	require.Equal(t, InputID("input_b"), topo.Inputs[1].ID)
	require.Equal(t, DeliveryLegacyUniversal, topo.Inputs[1].DeliveryType)

	// Must discover 8 demodulators (A..H), with C..H marked as FBC virtual (configMode == auto)
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
	topo, err := ParseNIMSettings(fixtureRepresentativeSingleCableLoopthroughFBC)
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

func TestParseNIMSettings_UnicableJESS_ExplicitProtocol(t *testing.T) {
	topo, err := ParseNIMSettings(fixtureRepresentativeUnicableJESS)
	require.NoError(t, err)
	require.Equal(t, ConfidenceObserved, topo.Confidence)

	// Explicit JESS protocol detected via format/model evidence, exactly 8 SCRs counted
	require.Len(t, topo.Inputs, 1)
	require.Equal(t, InputID("input_a"), topo.Inputs[0].ID)
	require.Equal(t, DeliveryUnicable2JESS, topo.Inputs[0].DeliveryType)
	require.Equal(t, 8, topo.Inputs[0].UserBands)

	require.Len(t, topo.Demodulators, 8)
	for _, d := range topo.Demodulators {
		require.Equal(t, InputID("input_a"), d.InputID)
	}
}

func TestParseNIMSettings_Unicable1_ExplicitProtocol(t *testing.T) {
	topo, err := ParseNIMSettings(fixtureRepresentativeUnicable1)
	require.NoError(t, err)
	require.Equal(t, ConfidenceObserved, topo.Confidence)

	// Standard Unicable 1 protocol, exactly 4 SCRs counted
	require.Len(t, topo.Inputs, 1)
	require.Equal(t, InputID("input_a"), topo.Inputs[0].ID)
	require.Equal(t, DeliveryUnicable1, topo.Inputs[0].DeliveryType)
	require.Equal(t, 4, topo.Inputs[0].UserBands)

	require.Len(t, topo.Demodulators, 4)
}

func TestParseNIMSettings_DiscreteDualSat(t *testing.T) {
	topo, err := ParseNIMSettings(fixtureRepresentativeDiscreteDualSat)
	require.NoError(t, err)
	require.Equal(t, ConfidenceObserved, topo.Confidence)

	require.Len(t, topo.Inputs, 2)
	require.Len(t, topo.Demodulators, 2)
	require.False(t, topo.Demodulators[0].IsFBCVirtual)
	require.False(t, topo.Demodulators[1].IsFBCVirtual)
}

func TestParseNIMSettings_HybridCombo(t *testing.T) {
	topo, err := ParseNIMSettings(fixtureRepresentativeHybridCombo)
	require.NoError(t, err)
	require.Equal(t, ConfidenceObserved, topo.Confidence)

	require.Len(t, topo.Inputs, 2)
	require.Len(t, topo.Demodulators, 2)
	require.Equal(t, DVBTypeSat, topo.Demodulators[0].DVBTypes[0])
	require.Equal(t, DVBTypeCable, topo.Demodulators[1].DVBTypes[0])
}

func TestParseNIMSettings_InvalidAndBrokenCases(t *testing.T) {
	// 1. Empty string
	_, err := ParseNIMSettings("")
	require.ErrorIs(t, err, ErrNoNIMConfigFound)

	// 2. Settings without NIM keys
	_, err = ParseNIMSettings("config.misc.firstrun=false\n")
	require.ErrorIs(t, err, ErrNoNIMConfigFound)

	// 3. All slots disabled
	_, err = ParseNIMSettings("config.Nims.0.configMode=nothing\nconfig.Nims.1.configMode=nothing\n")
	require.Error(t, err)

	// 4. Broken loopthrough reference (connectedTo points to non-existent slot)
	brokenLoopthrough := `
config.Nims.0.configMode=loopthrough
config.Nims.0.connectedTo=99
`
	_, err = ParseNIMSettings(brokenLoopthrough)
	require.ErrorIs(t, err, ErrInvalidNIMConfig)

	// 5. Unicable with zero configured SCR user bands
	zeroSCRUnicable := `
config.Nims.0.configMode=unicable
config.Nims.0.unicableMode=unicable_matrix
`
	_, err = ParseNIMSettings(zeroSCRUnicable)
	require.ErrorIs(t, err, ErrInvalidNIMConfig)
}
