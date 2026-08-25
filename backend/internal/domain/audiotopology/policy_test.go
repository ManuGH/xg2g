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

	e2 := []Enigma2TrackObservation{{PID: 200, Description: "Stereo"}}
	topoProvisional := BuildTopology(sref, nil, nil, e2, now)
	assert.Equal(t, PresenceProvisional, topoProvisional.Presence)

	planProvisional := PlanAudioOutput(topoProvisional, appleCaps)
	assert.False(t, planProvisional.IsExecutable)
	assert.Empty(t, planProvisional.Tracks)

	planMultiProvisional := PlanMultiAudioOutput(topoProvisional, appleCaps)
	assert.False(t, planMultiProvisional.IsExecutable)
	assert.Empty(t, planMultiProvisional.Tracks)
}

func TestPlanMultiAudioOutput_ZDFHD(t *testing.T) {
	sref := "1:0:19:2B66:3F3:1:C00000:0:0:0:" // ZDF HD
	now := time.Now()

	pmt := []PMTTrackObservation{
		{PID: 6120, Codec: "mp2", Channels: 2, Language: "deu", BitrateKbps: 256, IsDefault: true},
		{PID: 6121, Codec: "mp2", Channels: 2, Language: "deu", BitrateKbps: 192, VisualImpaired: true},
		{PID: 6122, Codec: "ac3", Channels: 6, Language: "deu", BitrateKbps: 448},
		{PID: 6123, Codec: "mp2", Channels: 2, Language: "mul", BitrateKbps: 192},
	}
	e2 := []Enigma2TrackObservation{
		{TrackID: 0, PID: 6120, Description: "MPEG (Stereo)"},
		{TrackID: 1, PID: 6121, Description: "MPEG (Audiodeskription)"},
		{TrackID: 2, PID: 6123, Description: "MPEG (Originalton)"},
		{TrackID: 3, PID: 6122, Description: "AC3 (Dolby Digital 5.1)"},
	}

	topo := BuildTopology(sref, pmt, nil, e2, now)
	require.Len(t, topo.Tracks, 4)

	appleCaps := ClientAudioCapabilities{
		SupportsAC3:        true,
		SupportsAAC:        true,
		SupportsSpatial51:  true,
		PrefersPassthrough: true,
	}

	multiPlan := PlanMultiAudioOutput(topo, appleCaps)
	require.True(t, multiPlan.IsExecutable)

	// Deduplication: PID 6120 (MP2 stereo duplicate) is omitted; we get 3 distinct renditions
	require.Len(t, multiPlan.Tracks, 3)

	// 1. Main track: Dolby Digital 5.1 (PID 6122)
	assert.Equal(t, uint16(6122), multiPlan.Tracks[0].PID)
	assert.True(t, multiPlan.Tracks[0].IsDefault)
	assert.Equal(t, CodecStrategyPassthrough, multiPlan.Tracks[0].Strategy)
	assert.Equal(t, "6", multiPlan.Tracks[0].HLSChannels)
	assert.Contains(t, multiPlan.Tracks[0].Name, "Dolby Digital 5.1")

	// 2. Alternate track: Originalton (PID 6123)
	assert.Equal(t, uint16(6123), multiPlan.Tracks[1].PID)
	assert.False(t, multiPlan.Tracks[1].IsDefault)
	assert.Contains(t, multiPlan.Tracks[1].Name, "Originalton")

	// 3. Accessibility track: Audiodeskription (PID 6121)
	assert.Equal(t, uint16(6121), multiPlan.Tracks[2].PID)
	assert.False(t, multiPlan.Tracks[2].IsDefault)
	assert.Equal(t, "public.accessibility.describes-video", multiPlan.Tracks[2].Characteristics)
	assert.Contains(t, multiPlan.Tracks[2].Name, "Audiodeskription")

	// Verify -var_stream_map generation
	vsm := multiPlan.BuildVarStreamMap()
	assert.Contains(t, vsm, "v:0,agroup:audio,default:yes")
	assert.Contains(t, vsm, "a:0,agroup:audio")
	assert.Contains(t, vsm, "default:yes")
	assert.Contains(t, vsm, "a:1,agroup:audio")
	assert.Contains(t, vsm, "default:no")
	assert.Contains(t, vsm, "a:2,agroup:audio")
}

func TestPlanMultiAudioOutput_DasErsteHD(t *testing.T) {
	sref := "1:0:19:283D:3FB:1:C00000:0:0:0:" // Das Erste HD
	now := time.Now()

	pmt := []PMTTrackObservation{
		{PID: 5102, Codec: "mp2", Channels: 2, Language: "deu", BitrateKbps: 256, IsDefault: true},
		{PID: 5103, Codec: "mp2", Channels: 2, Language: "deu", BitrateKbps: 256, VisualImpaired: true},
		{PID: 5107, Codec: "mp2", Channels: 2, Language: "deu", BitrateKbps: 192, HearingImpaired: true},
		{PID: 5106, Codec: "ac3", Channels: 2, Language: "deu", BitrateKbps: 448},
	}
	e2 := []Enigma2TrackObservation{
		{TrackID: 0, PID: 5102, Description: "MPEG (stereo)"},
		{TrackID: 1, PID: 5103, Description: "MPEG (mit Audiodeskription)"},
		{TrackID: 2, PID: 5107, Description: "MPEG (Klare Sprache)"},
		{TrackID: 3, PID: 5106, Description: "AC3 (Dolby Digital 2.0)"},
	}

	topo := BuildTopology(sref, pmt, nil, e2, now)
	appleCaps := ClientAudioCapabilities{SupportsAC3: true, SupportsAAC: true, PrefersPassthrough: true}

	multiPlan := PlanMultiAudioOutput(topo, appleCaps)
	require.True(t, multiPlan.IsExecutable)
	require.Len(t, multiPlan.Tracks, 3)

	// 1. Main: Dolby Digital 2.0 (PID 5106)
	assert.Equal(t, uint16(5106), multiPlan.Tracks[0].PID)
	assert.True(t, multiPlan.Tracks[0].IsDefault)

	// 2. Audiodeskription (PID 5103)
	assert.Equal(t, uint16(5103), multiPlan.Tracks[1].PID)
	assert.Equal(t, "public.accessibility.describes-video", multiPlan.Tracks[1].Characteristics)

	// 3. Klare Sprache (PID 5107)
	assert.Equal(t, uint16(5107), multiPlan.Tracks[2].PID)
	assert.Equal(t, "public.accessibility.transcribes-spoken-dialog", multiPlan.Tracks[2].Characteristics)
}
