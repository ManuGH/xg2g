package audiotopology

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiffTopologies_NoChange(t *testing.T) {
	now := time.Now()
	pmt := []PMTTrackObservation{
		{PID: 6120, Codec: "mp2", Channels: 2, Language: "deu"},
		{PID: 6122, Codec: "ac3", Channels: 6, Language: "deu"},
	}
	topo1 := BuildTopology("sref1", pmt, nil, nil, nil, now)
	topo2 := BuildTopology("sref1", pmt, nil, nil, nil, now)

	diff := DiffTopologies(topo1, topo2)
	assert.False(t, diff.HasChange)
	assert.False(t, diff.IsStructural)
	assert.False(t, diff.IsMetadataOnly)
}

func TestDiffTopologies_ChannelLayoutChange_2_0_to_5_1(t *testing.T) {
	now := time.Now()
	// Pre-movie: Deutsch AC-3 2.0
	pmtBefore := []PMTTrackObservation{
		{PID: 6122, Codec: "ac3", Channels: 2, Language: "deu"},
	}
	topoBefore := BuildTopology("sref1", pmtBefore, nil, nil, nil, now)

	// Movie start: Deutsch AC-3 5.1
	pmtAfter := []PMTTrackObservation{
		{PID: 6122, Codec: "ac3", Channels: 6, Language: "deu"},
	}
	topoAfter := BuildTopology("sref1", pmtAfter, nil, nil, nil, now.Add(time.Minute))

	diff := DiffTopologies(topoBefore, topoAfter)
	require.True(t, diff.HasChange)
	assert.True(t, diff.IsStructural, "channel change is structural")
	assert.False(t, diff.IsMetadataOnly)
	require.Len(t, diff.ModifiedTracks, 1)
	assert.Equal(t, uint16(6122), diff.ModifiedTracks[0].PID)
	assert.True(t, diff.ModifiedTracks[0].IsStructural)
	assert.Contains(t, diff.Summary, "channels 2 -> 6")
}

func TestDiffTopologies_TrackAppearedAndDisappeared(t *testing.T) {
	now := time.Now()
	// Pre-game: Only German commentary
	pmt1 := []PMTTrackObservation{
		{PID: 5101, Codec: "mp2", Channels: 2, Language: "deu"},
	}
	topo1 := BuildTopology("sref1", pmt1, nil, nil, nil, now)

	// Game starts: Stadium sound (PID 5102) added
	pmt2 := []PMTTrackObservation{
		{PID: 5101, Codec: "mp2", Channels: 2, Language: "deu"},
		{PID: 5102, Codec: "ac3", Channels: 6, Language: "deu", CleanEffects: true},
	}
	e22 := []Enigma2TrackObservation{
		{PID: 5101, Description: "Kommentar"},
		{PID: 5102, Description: "Stadionton"},
	}
	topo2 := BuildTopology("sref1", pmt2, nil, nil, e22, now.Add(time.Minute))

	diff := DiffTopologies(topo1, topo2)
	require.True(t, diff.HasChange)
	assert.True(t, diff.IsStructural)
	require.Len(t, diff.AddedTracks, 1)
	assert.Equal(t, uint16(5102), diff.AddedTracks[0].PID)

	// Game ends: Stadium sound removed
	diff2 := DiffTopologies(topo2, topo1)
	require.True(t, diff2.HasChange)
	assert.True(t, diff2.IsStructural)
	require.Len(t, diff2.RemovedTracks, 1)
	assert.Equal(t, uint16(5102), diff2.RemovedTracks[0].PID)
}

func TestDiffTopologies_MetadataOnlyChange(t *testing.T) {
	now := time.Now()
	pmt := []PMTTrackObservation{
		{PID: 6120, Codec: "mp2", Channels: 2, Language: "deu"},
	}
	topo1 := BuildTopology("sref1", pmt, nil, nil, nil, now)

	// Receiver provides description without any elementary stream changes
	e2 := []Enigma2TrackObservation{
		{PID: 6120, Description: "Stereo (Standard)"},
	}
	topo2 := BuildTopology("sref1", pmt, nil, nil, e2, now.Add(time.Second))

	diff := DiffTopologies(topo1, topo2)
	require.True(t, diff.HasChange)
	assert.False(t, diff.IsStructural, "label only change must not be structural")
	assert.True(t, diff.IsMetadataOnly)
	require.Len(t, diff.ModifiedTracks, 1)
	assert.False(t, diff.ModifiedTracks[0].IsStructural)
	assert.True(t, diff.ModifiedTracks[0].IsMetadata)
}
