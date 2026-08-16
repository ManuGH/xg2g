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
	assert.Equal(t, PresenceVerified, topo.Presence)

	appleCaps := ClientAudioCapabilities{
		SupportsAC3:        true,
		SupportsAAC:        true,
		SupportsSpatial51:  true,
		PrefersPassthrough: true,
	}

	plan := PlanAudioOutput(topo, appleCaps)
	require.True(t, plan.IsExecutable)
	require.Len(t, plan.Tracks, 2)

	// Stereo track -> Transcode to 192k AAC
	assert.Equal(t, uint16(6120), plan.Tracks[0].PID)
	assert.Equal(t, CodecStrategyTranscode, plan.Tracks[0].Strategy)
	assert.Equal(t, "aac", plan.Tracks[0].EncoderCodec)
	assert.Equal(t, "mp4a.40.2", plan.Tracks[0].HLSCodec)
	assert.Equal(t, "2", plan.Tracks[0].HLSChannels)
	assert.Equal(t, 192, plan.Tracks[0].BitrateKbps)
	assert.False(t, plan.Tracks[0].IsDefault)

	// 5.1 Surround track -> AC-3 Passthrough
	assert.Equal(t, uint16(6122), plan.Tracks[1].PID)
	assert.Equal(t, CodecStrategyPassthrough, plan.Tracks[1].Strategy)
	assert.Equal(t, "copy", plan.Tracks[1].EncoderCodec)
	assert.Equal(t, "ac-3", plan.Tracks[1].HLSCodec)
	assert.Equal(t, "6", plan.Tracks[1].HLSChannels)
	assert.Equal(t, 448, plan.Tracks[1].BitrateKbps)
	assert.True(t, plan.Tracks[1].IsDefault) // Exactly this 5.1 track is default
}

func TestAudioOutputPolicy_ExactlyOneDefaultAcrossMultipleSurroundTracks(t *testing.T) {
	sref := "1:0:19:2B66:3F3:1:C00000:0:0:0:"
	now := time.Now()

	// Broadcaster provides Deutsch 5.1 AND Englisch 5.1
	pmt := []PMTTrackObservation{
		{PID: 101, Codec: "ac3", Channels: 6, Language: "deu", BitrateKbps: 448, IsDefault: true},
		{PID: 102, Codec: "ac3", Channels: 6, Language: "eng", BitrateKbps: 448},
		{PID: 103, Codec: "mp2", Channels: 2, Language: "deu", BitrateKbps: 192, VisualImpaired: true},
	}

	topo := BuildTopology(sref, pmt, nil, nil, now)
	require.Len(t, topo.Tracks, 3)

	appleCaps := ClientAudioCapabilities{SupportsAC3: true, SupportsAAC: true}
	plan := PlanAudioOutput(topo, appleCaps)

	defaultCount := 0
	for _, tr := range plan.Tracks {
		if tr.IsDefault {
			defaultCount++
		}
	}

	assert.Equal(t, 1, defaultCount, "Exactly ONE track must be marked as default")
	assert.True(t, plan.Tracks[0].IsDefault, "Deutsch 5.1 should be the single default")
	assert.False(t, plan.Tracks[1].IsDefault, "Englisch 5.1 must NOT be default")
	assert.False(t, plan.Tracks[2].IsDefault, "Audiodeskription must NOT be default")
}

func TestAudioOutputPolicy_PresenceGate(t *testing.T) {
	sref := "1:0:19:132F:3EF:1:C00000:0:0:0:"
	now := time.Now()
	appleCaps := ClientAudioCapabilities{SupportsAC3: true, SupportsAAC: true}

	// 1. Provisional topology (Enigma2-only, unverified TS)
	e2 := []Enigma2TrackObservation{{PID: 200, Description: "Stereo"}}
	topoProvisional := BuildTopology(sref, nil, nil, e2, now)
	assert.Equal(t, PresenceProvisional, topoProvisional.Presence)

	planProvisional := PlanAudioOutput(topoProvisional, appleCaps)
	assert.False(t, planProvisional.IsExecutable, "Provisional topology must NOT produce executable media plans")
	assert.Empty(t, planProvisional.Tracks)

	// 2. Empty topology
	topoEmpty := BuildTopology(sref, nil, nil, nil, now)
	planEmpty := PlanAudioOutput(topoEmpty, appleCaps)
	assert.False(t, planEmpty.IsExecutable)
	assert.Empty(t, planEmpty.Tracks)
}

func TestAudioOutputPolicy_Dynamic20To51To20Transitions(t *testing.T) {
	sref := "1:0:19:132F:3EF:1:C00000:0:0:0:"
	now := time.Now()

	pmtStereo := []PMTTrackObservation{
		{PID: 1921, Codec: "ac3", Channels: 2, Language: "deu", BitrateKbps: 448},
	}
	topoStereo := BuildTopology(sref, pmtStereo, nil, nil, now)

	pmtSurround := []PMTTrackObservation{
		{PID: 1921, Codec: "ac3", Channels: 6, Language: "deu", BitrateKbps: 448},
	}
	topoSurround := BuildTopology(sref, pmtSurround, nil, nil, now.Add(time.Hour))

	pmtLate := []PMTTrackObservation{
		{PID: 1921, Codec: "ac3", Channels: 2, Language: "deu", BitrateKbps: 448},
	}
	topoLateness := BuildTopology(sref, pmtLate, nil, nil, now.Add(2*time.Hour))

	assert.NotEqual(t, topoStereo.StructuralRevision, topoSurround.StructuralRevision)
	assert.Equal(t, topoStereo.StructuralRevision, topoLateness.StructuralRevision)

	appleCaps := ClientAudioCapabilities{SupportsAC3: true, SupportsAAC: true, SupportsSpatial51: true}
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
