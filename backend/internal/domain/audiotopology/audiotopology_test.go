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
	require.NotZero(t, topo.TopologyRevision)

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
		{PID: 5102, Codec: "mp2", Channels: 2, Language: "deu", BitrateKbps: 256},
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
		{PID: 6120, Codec: "mp2", Channels: 2, Language: "deu", BitrateKbps: 256},
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

	// PID 6123: ZDF Originalton enriched via Enigma2
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
		{PID: 5112, Codec: "mp2", Channels: 2, Language: "deu", BitrateKbps: 192},
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

	// PID 5113: French second language
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
	assert.Equal(t, EvidencePMT, conflictTrack.Conflicts[0].SourceA)
	assert.Equal(t, EvidenceEnigma2, conflictTrack.Conflicts[0].SourceB)
}

func TestTopologyInvariants(t *testing.T) {
	sref := "1:0:19:132F:3EF:1:C00000:0:0:0:"
	now := time.Now()

	pmt := []PMTTrackObservation{
		{PID: 1921, Codec: "ac3", Channels: 6, Language: "deu", BitrateKbps: 448},
		{PID: 1922, Codec: "mp2", Channels: 2, Language: "mis", BitrateKbps: 160},
	}

	// 1. PMT only -> Complete, valid topology without Enigma2
	topoPMTOnly := BuildTopology(sref, pmt, nil, nil, now)
	require.Len(t, topoPMTOnly.Tracks, 2)
	assert.Equal(t, uint16(1921), topoPMTOnly.Tracks[0].PID)
	assert.Equal(t, CodecAC3, topoPMTOnly.Tracks[0].Codec)
	assert.Equal(t, 6, topoPMTOnly.Tracks[0].Channels)
	assert.False(t, topoPMTOnly.Tracks[0].ReceiverSelected) // No Enigma2 active state

	// 2. OpenWebIF empty slice -> No error
	topoEmptyE2 := BuildTopology(sref, pmt, nil, []Enigma2TrackObservation{}, now)
	require.Len(t, topoEmptyE2.Tracks, 2)
	assert.Equal(t, topoPMTOnly.TopologyRevision, topoEmptyE2.TopologyRevision)

	// 3. PMT PID unknown to Enigma2 -> Track still preserved
	e2Partial := []Enigma2TrackObservation{
		{TrackID: 0, PID: 1921, Description: "AC3 (Sprache 1)", Active: true},
	}
	topoPartialE2 := BuildTopology(sref, pmt, nil, e2Partial, now)
	require.Len(t, topoPartialE2.Tracks, 2)
	assert.Equal(t, uint16(1922), topoPartialE2.Tracks[1].PID)

	// 4. Enigma2 has unknown PID not in PMT -> PMT presence truth dominates
	e2Phantom := []Enigma2TrackObservation{
		{TrackID: 0, PID: 1921, Description: "AC3", Active: true},
		{TrackID: 99, PID: 9999, Description: "Phantom PID", Active: false},
	}
	topoPhantom := BuildTopology(sref, pmt, nil, e2Phantom, now)
	require.Len(t, topoPhantom.Tracks, 2) // 9999 is discarded because not in PMT
	assert.Equal(t, uint16(1921), topoPhantom.Tracks[0].PID)
	assert.Equal(t, uint16(1922), topoPhantom.Tracks[1].PID)

	// 5. Topology Revision determinism
	rev1 := topoPMTOnly.TopologyRevision
	topoPMTOnly2 := BuildTopology(sref, pmt, nil, nil, now.Add(time.Hour))
	assert.Equal(t, rev1, topoPMTOnly2.TopologyRevision)

	// 6. Adding a PID changes TopologyRevision
	pmtAdded := append(pmt, PMTTrackObservation{PID: 1923, Codec: "mp2", Channels: 2, Language: "eng"})
	topoAdded := BuildTopology(sref, pmtAdded, nil, nil, now)
	assert.NotEqual(t, rev1, topoAdded.TopologyRevision)
}
