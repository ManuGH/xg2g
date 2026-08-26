package audiotopology

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const chSref = "1:0:19:83:6:85:C00000:0:0:0:"

func evidenceFor(track AudioTrack, field string) map[EvidenceSource]string {
	out := map[EvidenceSource]string{}
	for _, o := range track.Evidence {
		if o.Field == field {
			out[o.Source] = o.Value
		}
	}
	return out
}

func conflictsFor(track AudioTrack, field string) []Conflict {
	var out []Conflict
	for _, c := range track.Conflicts {
		if c.Field == field {
			out = append(out, c)
		}
	}
	return out
}

// The precedence is stated once, in one place, and the table is the contract.
func TestResolveChannels_Precedence(t *testing.T) {
	tests := []struct {
		name       string
		pmt        *PMTTrackObservation
		probe      *ProbeTrackObservation
		es         *ESTrackObservation
		want       int
		wantSource EvidenceSource
		wantOK     bool
	}{
		{
			name:       "the stream is read over both others",
			pmt:        &PMTTrackObservation{Channels: 2},
			probe:      &ProbeTrackObservation{Channels: 2},
			es:         &ESTrackObservation{Channels: 6},
			want:       6,
			wantSource: EvidenceObserved,
			wantOK:     true,
		},
		{
			name:       "the probe is read over the declaration",
			pmt:        &PMTTrackObservation{Channels: 2},
			probe:      &ProbeTrackObservation{Channels: 6},
			want:       6,
			wantSource: EvidenceProbe,
			wantOK:     true,
		},
		{
			name:       "the declaration answers when nothing read the frames",
			pmt:        &PMTTrackObservation{Channels: 2},
			want:       2,
			wantSource: EvidencePMT,
			wantOK:     true,
		},
		{
			name:       "a source that named nothing does not displace one that did",
			pmt:        &PMTTrackObservation{Channels: 2},
			probe:      &ProbeTrackObservation{Channels: 0},
			es:         &ESTrackObservation{Channels: 0},
			want:       2,
			wantSource: EvidencePMT,
			wantOK:     true,
		},
		{
			name:   "nobody named a count",
			pmt:    &PMTTrackObservation{},
			probe:  &ProbeTrackObservation{},
			es:     &ESTrackObservation{},
			wantOK: false,
		},
		{
			name:   "no sources at all",
			wantOK: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, source, ok := resolveChannels(tc.pmt, tc.probe, tc.es)
			assert.Equal(t, tc.wantOK, ok)
			if !tc.wantOK {
				return
			}
			assert.Equal(t, tc.want, got)
			assert.Equal(t, tc.wantSource, source)
		})
	}
}

// The case the observer was built for. The probe read stereo at session start and
// the film has since started; the continuous reading is the current one, and the
// disagreement stays on the record instead of being silently dropped.
func TestBuildTopology_ObservedChannelsWinAndTheDisagreementStays(t *testing.T) {
	topo := BuildTopology(chSref,
		[]PMTTrackObservation{{PID: 101, Codec: "ac3", Language: "deu"}},
		[]ProbeTrackObservation{{PID: 101, Codec: "ac3", Channels: 2}},
		[]ESTrackObservation{{PID: 101, Channels: 6, LFE: true, Frames: 300}},
		nil, time.Now())

	require.Len(t, topo.Tracks, 1)
	track := topo.Tracks[0]

	assert.Equal(t, 6, track.Channels)

	// Both statements are on the record, whichever one was used.
	ev := evidenceFor(track, "channels")
	assert.Equal(t, "2", ev[EvidenceProbe])
	assert.Equal(t, "6", ev[EvidenceObserved])

	conflicts := conflictsFor(track, "channels")
	require.Len(t, conflicts, 1)
	assert.Equal(t, EvidenceProbe, conflicts[0].SourceA)
	assert.Equal(t, "2", conflicts[0].ValueA)
	assert.Equal(t, EvidenceObserved, conflicts[0].SourceB)
	assert.Equal(t, "6", conflicts[0].ValueB)
	assert.Equal(t, "preferred_continuous_stream_observation", conflicts[0].Resolution)
}

// A multichannel declaration names no count, so it cannot contradict one. The
// descriptor saying "more than two" beside an observed 6 is two sources agreeing,
// and recording a conflict there would bury the real ones.
func TestBuildTopology_MultichannelDeclarationIsNotAConflict(t *testing.T) {
	topo := BuildTopology(chSref,
		[]PMTTrackObservation{{PID: 101, Codec: "ac3", Language: "deu", Channels: 0}},
		nil,
		[]ESTrackObservation{{PID: 101, Channels: 6, LFE: true}},
		nil, time.Now())

	require.Len(t, topo.Tracks, 1)
	track := topo.Tracks[0]

	assert.Equal(t, 6, track.Channels)
	assert.Empty(t, conflictsFor(track, "channels"))
	assert.Equal(t, "6", evidenceFor(track, "channels")[EvidenceObserved])
}

// Precedence is per field, not a ranking of sources. The observation supplies the
// channel count and says nothing about language, so the descriptor keeps it - and
// there is no way for one to leak into the other, because an audio frame header
// carries no language for ESTrackObservation to hold.
func TestBuildTopology_ObservedChannelsDoNotDisplaceTheDeclaredLanguage(t *testing.T) {
	topo := BuildTopology(chSref,
		[]PMTTrackObservation{{PID: 101, Codec: "ac3", Language: "deu", VisualImpaired: true}},
		[]ProbeTrackObservation{{PID: 101, Codec: "ac3", Channels: 2, Language: "eng"}},
		[]ESTrackObservation{{PID: 101, Channels: 6, LFE: true}},
		nil, time.Now())

	require.Len(t, topo.Tracks, 1)
	track := topo.Tracks[0]

	assert.Equal(t, 6, track.Channels, "the stream answered the channel question")
	assert.Equal(t, "deu", track.Language.ISO639_2, "the descriptor still answers the language question")
	assert.True(t, track.Accessibility.AudioDescription, "and the accessibility one")
}

// Live TV is 0..N tracks. An observation belongs to one PID and must not reach
// another, whatever the neighbouring track established.
func TestBuildTopology_ObservationsStayWithTheirTrack(t *testing.T) {
	topo := BuildTopology(chSref,
		[]PMTTrackObservation{
			{PID: 101, Codec: "ac3", Language: "deu"},
			{PID: 102, Codec: "ac3", Language: "eng"},
			{PID: 103, Codec: "mp2", Language: "deu"},
		},
		nil,
		[]ESTrackObservation{
			{PID: 101, Channels: 6, LFE: true},
			{PID: 102, Channels: 2},
		},
		nil, time.Now())

	require.Len(t, topo.Tracks, 3)
	byPID := map[uint16]AudioTrack{}
	for _, tr := range topo.Tracks {
		byPID[tr.PID] = tr
	}

	assert.Equal(t, 6, byPID[101].Channels)
	assert.Equal(t, 2, byPID[102].Channels)

	// PID 103 has no observation and no declared count. It must not inherit its
	// neighbours' 6, and the evidence must not claim the stream answered for it.
	assert.Empty(t, evidenceFor(byPID[103], "channels"))
	assert.NotEqual(t, 6, byPID[103].Channels)
}

// An observed layout change moves the topology's own revision, which is what a
// downstream consumer watches. It is derived from the resolved facts, so it needs
// no counter of its own and never borrows the PMT generation.
func TestBuildTopology_ObservedChangeMovesTheStructuralRevision(t *testing.T) {
	pmt := []PMTTrackObservation{{PID: 101, Codec: "ac3", Language: "deu"}}
	now := time.Now()

	stereo := BuildTopology(chSref, pmt, nil, []ESTrackObservation{{PID: 101, Channels: 2}}, nil, now)
	surround := BuildTopology(chSref, pmt, nil, []ESTrackObservation{{PID: 101, Channels: 6, LFE: true}}, nil, now)

	assert.NotEqual(t, stereo.StructuralRevision, surround.StructuralRevision,
		"the same PMT with a different observed layout is a different topology")
}
