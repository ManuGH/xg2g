package audiotopology

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestORF1HD_RealWorldDiscovery(t *testing.T) {
	sref := "1:0:19:132F:3EF:1:C00000:0:0:0:"
	now := time.Now()

	pmt := []PMTTrackObservation{
		{PID: 1921, Codec: "ac3", Channels: 6, Language: "deu", BitrateKbps: 448, IsDefault: true},
		{PID: 1922, Codec: "mp2", Channels: 2, Language: "mis", BitrateKbps: 160, CleanEffects: true},
	}
	e2 := []Enigma2TrackObservation{
		{TrackID: 0, PID: 1921, Description: "AC3 (Sprache 1)", Active: true},
		{TrackID: 1, PID: 1922, Description: "MPEG (Miscellaneous languages)", Active: false},
	}

	topo := BuildTopology(sref, pmt, nil, e2, now)

	require.Len(t, topo.Tracks, 2)
	require.Equal(t, sref, topo.ServiceRef)
	require.Equal(t, PresenceVerified, topo.Presence)
	require.NotZero(t, topo.StructuralRevision)

	t1 := topo.Tracks[0]
	assert.Equal(t, uint16(1921), t1.PID)
	assert.Equal(t, CodecAC3, t1.Codec)
	assert.Equal(t, 6, t1.Channels)
	assert.Equal(t, "deu", t1.Language.ISO639_2)
	assert.True(t, t1.BroadcastDefault)
	assert.True(t, t1.ReceiverSelected)
	assert.Contains(t, t1.Label, "Deutsch")
	assert.Contains(t, t1.Label, "Dolby Digital 5.1")

	t2 := topo.Tracks[1]
	assert.Equal(t, uint16(1922), t2.PID)
	assert.Equal(t, CodecMP2, t2.Codec)
	assert.Equal(t, AudioPurposeAlternate, t2.Purpose)
	assert.False(t, t2.ReceiverSelected)
}

func TestDasErsteHD_RealWorldDiscovery(t *testing.T) {
	sref := "1:0:19:283D:3FB:1:C00000:0:0:0:"
	now := time.Now()

	pmt := []PMTTrackObservation{
		{PID: 5102, Codec: "mp2", Channels: 2, Language: "deu", BitrateKbps: 256, IsDefault: true},
		{PID: 5103, Codec: "mp2", Channels: 2, Language: "deu", BitrateKbps: 256, VisualImpaired: true},
		{PID: 5107, Codec: "mp2", Channels: 2, Language: "deu", BitrateKbps: 192, HearingImpaired: true},
		{PID: 5106, Codec: "ac3", Channels: 2, Language: "deu", BitrateKbps: 448},
	}
	e2 := []Enigma2TrackObservation{
		{TrackID: 0, PID: 5102, Description: "MPEG (stereo)", Active: true},
		{TrackID: 1, PID: 5103, Description: "MPEG (mit Audiodeskription)", Active: false},
		{TrackID: 2, PID: 5107, Description: "MPEG (Klare Sprache)", Active: false},
		{TrackID: 3, PID: 5106, Description: "AC3 (Dolby Digital 2.0)", Active: false},
	}

	topo := BuildTopology(sref, pmt, nil, e2, now)

	require.Len(t, topo.Tracks, 4)

	// PID 5102: Stereo
	assert.Equal(t, uint16(5102), topo.Tracks[0].PID)
	assert.True(t, topo.Tracks[0].ReceiverSelected)
	assert.Equal(t, AudioPurposeMain, topo.Tracks[0].Purpose)

	// PID 5103: Audiodeskription
	assert.Equal(t, uint16(5103), topo.Tracks[1].PID)
	assert.True(t, topo.Tracks[1].Accessibility.AudioDescription)
	assert.Contains(t, topo.Tracks[1].Label, "Audiodeskription")

	// PID 5106: Dolby Digital 2.0
	assert.Equal(t, uint16(5106), topo.Tracks[2].PID)
	assert.Equal(t, CodecAC3, topo.Tracks[2].Codec)
	assert.Contains(t, topo.Tracks[2].Label, "Dolby Digital 2.0")

	// PID 5107: Klare Sprache
	assert.Equal(t, uint16(5107), topo.Tracks[3].PID)
	assert.True(t, topo.Tracks[3].Accessibility.ClearDialogue)
	assert.Contains(t, topo.Tracks[3].Label, "Klare Sprache")
}

func TestZDFHD_RealWorldDiscovery(t *testing.T) {
	sref := "1:0:19:2B66:3F3:1:C00000:0:0:0:"
	now := time.Now()

	pmt := []PMTTrackObservation{
		{PID: 6120, Codec: "mp2", Channels: 2, Language: "deu", BitrateKbps: 256, IsDefault: true},
		{PID: 6121, Codec: "mp2", Channels: 2, Language: "deu", BitrateKbps: 192, VisualImpaired: true},
		{PID: 6122, Codec: "ac3", Channels: 6, Language: "deu", BitrateKbps: 448},
		{PID: 6123, Codec: "mp2", Channels: 2, Language: "mul", BitrateKbps: 192, HearingImpaired: true},
	}
	e2 := []Enigma2TrackObservation{
		{TrackID: 0, PID: 6120, Description: "MPEG (Stereo)", Active: true},
		{TrackID: 1, PID: 6121, Description: "MPEG (Audiodeskription)", Active: false},
		{TrackID: 2, PID: 6123, Description: "MPEG (Originalton)", Active: false},
		{TrackID: 3, PID: 6122, Description: "AC3 (Dolby Digital 5.1)", Active: false},
	}

	topo := BuildTopology(sref, pmt, nil, e2, now)

	require.Len(t, topo.Tracks, 4)

	var oton *AudioTrack
	for i := range topo.Tracks {
		if topo.Tracks[i].PID == 6123 {
			oton = &topo.Tracks[i]
			break
		}
	}
	require.NotNil(t, oton)
	assert.Equal(t, AudioPurposeAlternate, oton.Purpose)
	assert.Equal(t, ConfidenceHigh, oton.Confidence)
	assert.Contains(t, oton.Label, "Originalton")
}

func TestArteHD_ConflictPreservation(t *testing.T) {
	sref := "1:0:19:283E:3FB:1:C00000:0:0:0:"
	now := time.Now()

	pmt := []PMTTrackObservation{
		{PID: 5112, Codec: "mp2", Channels: 2, Language: "deu", BitrateKbps: 192, IsDefault: true},
		{PID: 5113, Codec: "mp2", Channels: 2, Language: "fra", BitrateKbps: 192},
		{PID: 5116, Codec: "mp2", Channels: 2, Language: "deu", BitrateKbps: 192, HearingImpaired: true},
		{PID: 5117, Codec: "mp2", Channels: 2, Language: "mis", BitrateKbps: 192, VisualImpaired: true},
	}
	e2 := []Enigma2TrackObservation{
		{TrackID: 0, PID: 5112, Description: "MPEG (stereo)", Active: true},
		{TrackID: 1, PID: 5113, Description: "MPEG (französisch)", Active: false},
		{TrackID: 2, PID: 5116, Description: "MPEG (Klare Sprache (wenn kein Originalton))", Active: false},
		{TrackID: 3, PID: 5117, Description: "MPEG (ohne Audiodeskription)", Active: false},
	}

	topo := BuildTopology(sref, pmt, nil, e2, now)
	require.Len(t, topo.Tracks, 4)

	var frenchTrack *AudioTrack
	var conflictTrack *AudioTrack
	for i := range topo.Tracks {
		if topo.Tracks[i].PID == 5113 {
			frenchTrack = &topo.Tracks[i]
		}
		if topo.Tracks[i].PID == 5117 {
			conflictTrack = &topo.Tracks[i]
		}
	}

	require.NotNil(t, frenchTrack)
	assert.Equal(t, "fra", frenchTrack.Language.ISO639_2)
	assert.Equal(t, AudioPurposeAlternate, frenchTrack.Purpose)
	assert.Contains(t, frenchTrack.Label, "Französisch")

	// PID 5117: Contradiction between PMT VisualImpaired and E2 "ohne Audiodeskription"
	require.NotNil(t, conflictTrack)
	require.NotEmpty(t, conflictTrack.Conflicts)
	assert.Equal(t, "accessibility.audioDescription", conflictTrack.Conflicts[0].Field)
}

func TestPresenceStates_VerifiedVsProvisionalVsEmpty(t *testing.T) {
	sref := "1:0:19:132F:3EF:1:C00000:0:0:0:"
	now := time.Now()

	// 1. PMT only -> Verified Presence
	pmt := []PMTTrackObservation{{PID: 100, Codec: "mp2", Language: "deu"}}
	topoVerified := BuildTopology(sref, pmt, nil, nil, now)
	assert.Equal(t, PresenceVerified, topoVerified.Presence)
	require.Len(t, topoVerified.Tracks, 1)

	// 2. Enigma2 only (unprobed stream) -> Provisional Presence
	e2 := []Enigma2TrackObservation{{PID: 200, Description: "MPEG (stereo)"}}
	topoProvisional := BuildTopology(sref, nil, nil, e2, now)
	assert.Equal(t, PresenceProvisional, topoProvisional.Presence)
	require.Len(t, topoProvisional.Tracks, 1)

	// 3. No observations -> Empty Presence
	topoEmpty := BuildTopology(sref, nil, nil, nil, now)
	assert.Equal(t, PresenceEmpty, topoEmpty.Presence)
	assert.Empty(t, topoEmpty.Tracks)
}

func TestPrimaryLanguage_NotDependentOnPIDOrdering(t *testing.T) {
	sref := "1:0:19:132F:3EF:1:C00000:0:0:0:"
	now := time.Now()

	// PID 200 is French (Alternate), PID 800 is German Broadcast Default
	pmt := []PMTTrackObservation{
		{PID: 200, Codec: "mp2", Language: "fra"},
		{PID: 800, Codec: "mp2", Language: "deu", IsDefault: true},
	}

	topo := BuildTopology(sref, pmt, nil, nil, now)
	require.Len(t, topo.Tracks, 2)

	// PID 200 (numerically smaller) MUST NOT be classified as Main Anchor
	assert.Equal(t, uint16(200), topo.Tracks[0].PID)
	assert.Equal(t, AudioPurposeAlternate, topo.Tracks[0].Purpose)

	// PID 800 (Broadcast Default) is Main Anchor
	assert.Equal(t, uint16(800), topo.Tracks[1].PID)
	assert.Equal(t, AudioPurposeMain, topo.Tracks[1].Purpose)
	assert.True(t, topo.Tracks[1].BroadcastDefault)
}

func TestStreamType0x06_IsUnknownWithoutDescriptorConfirmation(t *testing.T) {
	assert.Equal(t, CodecUnknown, CodecFromDVBStreamType(0x06))
	assert.Equal(t, CodecMP2, CodecFromDVBStreamType(0x03))
	assert.Equal(t, CodecAAC, CodecFromDVBStreamType(0x0F))
	assert.Equal(t, CodecAC3, CodecFromDVBStreamType(0x81))
}

func TestLanguageQAA_IsPrivateUseNotGuaranteedOriginal(t *testing.T) {
	lang := NormalizeLanguage("qaa")
	assert.Equal(t, "qaa", lang.ISO639_2)
	assert.False(t, lang.IsOriginal)
	assert.True(t, lang.IsUndefined)
}

func TestDualRevisions_StructuralVsMetadata(t *testing.T) {
	sref := "1:0:19:132F:3EF:1:C00000:0:0:0:"
	now := time.Now()

	pmt := []PMTTrackObservation{
		{PID: 100, Codec: "mp2", Channels: 2, Language: "deu"},
	}
	e2Active := []Enigma2TrackObservation{
		{PID: 100, Description: "Stereo", Active: true},
	}
	e2Inactive := []Enigma2TrackObservation{
		{PID: 100, Description: "Stereo", Active: false},
	}

	topo1 := BuildTopology(sref, pmt, nil, e2Active, now)
	topo2 := BuildTopology(sref, pmt, nil, e2Inactive, now)

	// Structural revision is IDENTICAL (same PID, codec, channels)
	assert.Equal(t, topo1.StructuralRevision, topo2.StructuralRevision)

	// Metadata revision is DIFFERENT (active selection changed)
	assert.NotEqual(t, topo1.MetadataRevision, topo2.MetadataRevision)
}
