package audiotopology

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAudioOutputPolicy_51PassthroughForAppleClients(t *testing.T) {
	sref := "1:0:19:2B66:3F3:1:C00000:0:0:0:" // ZDF HD
	now := time.Now()

	pmt := []PMTTrackObservation{
		{PID: 6120, Codec: "mp2", Channels: 2, Language: "deu", BitrateKbps: 256},
		{PID: 6122, Codec: "ac3", Channels: 6, Language: "deu", BitrateKbps: 448},
	}

	topo := BuildTopology(sref, pmt, nil, nil, now)
	require.Len(t, topo.Tracks, 2)

	appleCaps := ClientAudioCapabilities{
		SupportsAC3:        true,
		SupportsSpatial51:  true,
		PrefersPassthrough: true,
	}

	plan := PlanAudioOutput(topo, appleCaps)
	require.Len(t, plan.Tracks, 2)

	// Stereo track -> Transcode to 192k AAC
	assert.Equal(t, uint16(6120), plan.Tracks[0].PID)
	assert.Equal(t, CodecStrategyTranscode, plan.Tracks[0].Strategy)
	assert.Equal(t, "2", plan.Tracks[0].HLSChannels)
	assert.Equal(t, 192, plan.Tracks[0].BitrateKbps)

	// 5.1 Surround track -> AC-3 Passthrough
	assert.Equal(t, uint16(6122), plan.Tracks[1].PID)
	assert.Equal(t, CodecStrategyPassthrough, plan.Tracks[1].Strategy)
	assert.Equal(t, "6", plan.Tracks[1].HLSChannels)
	assert.Equal(t, 448, plan.Tracks[1].BitrateKbps)
	assert.True(t, plan.Tracks[1].IsDefault) // 5.1 preferred as default
}

func TestAudioOutputPolicy_Dynamic20To51To20Transitions(t *testing.T) {
	sref := "1:0:19:132F:3EF:1:C00000:0:0:0:" // ORF 1 HD
	now := time.Now()

	// Show A: Daytime show broadcasting AC-3 in 2.0 Stereo
	pmtStereo := []PMTTrackObservation{
		{PID: 1921, Codec: "ac3", Channels: 2, Language: "deu", BitrateKbps: 448},
	}
	topoStereo := BuildTopology(sref, pmtStereo, nil, nil, now)

	// Show B: Primetime movie broadcasting AC-3 in 5.1 Surround
	pmtSurround := []PMTTrackObservation{
		{PID: 1921, Codec: "ac3", Channels: 6, Language: "deu", BitrateKbps: 448},
	}
	topoSurround := BuildTopology(sref, pmtSurround, nil, nil, now.Add(time.Hour))

	// Show C: Late night news broadcasting AC-3 in 2.0 Stereo again
	pmtLate := []PMTTrackObservation{
		{PID: 1921, Codec: "ac3", Channels: 2, Language: "deu", BitrateKbps: 448},
	}
	topoLateness := BuildTopology(sref, pmtLate, nil, nil, now.Add(2*time.Hour))

	// 1. Structural revisions MUST differ between Stereo and Surround
	assert.NotEqual(t, topoStereo.StructuralRevision, topoSurround.StructuralRevision)
	assert.Equal(t, topoStereo.StructuralRevision, topoLateness.StructuralRevision)

	// 2. Output plans adapt correctly
	appleCaps := ClientAudioCapabilities{SupportsAC3: true, SupportsSpatial51: true}
	planA := PlanAudioOutput(topoStereo, appleCaps)
	planB := PlanAudioOutput(topoSurround, appleCaps)
	planC := PlanAudioOutput(topoLateness, appleCaps)

	assert.Equal(t, "2", planA.Tracks[0].HLSChannels)
	assert.Equal(t, RenditionStereo, planA.Tracks[0].Rendition)

	assert.Equal(t, "6", planB.Tracks[0].HLSChannels)
	assert.Equal(t, RenditionSurround, planB.Tracks[0].Rendition)

	assert.Equal(t, "2", planC.Tracks[0].HLSChannels)
	assert.Equal(t, RenditionStereo, planC.Tracks[0].Rendition)
}
