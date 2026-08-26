// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ffmpeg

import (
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/audiotopology"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
)

func TestPMTTrackObservations_CarriesTheDeclaration(t *testing.T) {
	got := pmtTrackObservations([]ports.LiveAudioTrack{
		{PID: 257, StreamType: 0x81, Codec: "ac3", Language: "deu", Channels: 2, ComponentType: 0x82, HasComponentType: true},
		{PID: 258, StreamType: 0x0F, Codec: "aac", Language: "eng", Channels: 1},
	})

	if len(got) != 2 {
		t.Fatalf("got %d observations, want 2", len(got))
	}
	if got[0].PID != 257 || got[0].Codec != "ac3" || got[0].Language != "deu" || got[0].Channels != 2 {
		t.Errorf("first observation = %+v, want the AC-3 stereo declaration", got[0])
	}
	if got[1].StreamType != 0x0F || got[1].Channels != 1 {
		t.Errorf("second observation = %+v, want the AAC mono declaration", got[1])
	}
}

// The rule that survives every layer: a multichannel declaration names no count,
// so Channels stays 0 and the merge falls through to whatever measured it. Turning
// it into 6 here would put a number in front of the encoder that the stream never
// gave.
func TestPMTTrackObservations_MultichannelDeclaresNoCount(t *testing.T) {
	got := pmtTrackObservations([]ports.LiveAudioTrack{
		{PID: 257, Codec: "ac3", Language: "deu", Multichannel: true, ComponentType: 0x84, HasComponentType: true},
	})

	if len(got) != 1 {
		t.Fatalf("got %d observations, want 1", len(got))
	}
	if got[0].Channels != 0 {
		t.Errorf("Channels = %d, want 0: the descriptor said \"more than two\", not a number", got[0].Channels)
	}
	// The parts that are exact still come through.
	if got[0].Codec != "ac3" || got[0].Language != "deu" {
		t.Errorf("observation = %+v, want codec and language preserved", got[0])
	}
}

func TestPMTTrackObservations_EmptyStaysNil(t *testing.T) {
	if got := pmtTrackObservations(nil); got != nil {
		t.Errorf("got %+v, want nil so BuildTopology sees what it always saw", got)
	}
}

// The point of the whole chain: BuildTopology has taken pmtTracks since it was
// written and been handed nil for as long. Feeding it real declarations must make
// the merge use them, and must record where each fact came from.
func TestBuildTopology_UsesThePMTDeclaration(t *testing.T) {
	pmt := pmtTrackObservations([]ports.LiveAudioTrack{
		{PID: 257, StreamType: 0x81, Codec: "ac3", Language: "deu", Channels: 2},
	})

	topo := audiotopology.BuildTopology("1:0:19:83:6:85:C00000:0:0:0:", pmt, nil, nil, time.Now())

	if len(topo.Tracks) != 1 {
		t.Fatalf("topology has %d tracks, want 1 from the PMT alone", len(topo.Tracks))
	}
	track := topo.Tracks[0]
	if track.PID != 257 {
		t.Errorf("PID = %d, want 257", track.PID)
	}
	if track.Channels != 2 {
		t.Errorf("Channels = %d, want the declared 2", track.Channels)
	}
}

// A probe still overrides a declaration for the same PID - the measurement is the
// stronger fact - and the disagreement has to be visible rather than silently
// resolved.
func TestBuildTopology_ProbeOverridesTheDeclaration(t *testing.T) {
	pmt := pmtTrackObservations([]ports.LiveAudioTrack{
		{PID: 257, StreamType: 0x81, Codec: "ac3", Language: "deu", Channels: 2},
	})
	probe := []audiotopology.ProbeTrackObservation{
		{PID: 257, StreamIndex: 0, Codec: "ac3", Channels: 6, Language: "deu"},
	}

	topo := audiotopology.BuildTopology("1:0:19:83:6:85:C00000:0:0:0:", pmt, probe, nil, time.Now())

	if len(topo.Tracks) != 1 {
		t.Fatalf("topology has %d tracks, want the two observations merged into 1", len(topo.Tracks))
	}
	if topo.Tracks[0].Channels != 6 {
		t.Errorf("Channels = %d, want the probed 6 to win over the declared 2", topo.Tracks[0].Channels)
	}
}

// A declaration for a PID the probe never saw must not vanish: that is the case
// this whole change exists for, where the PMT knows something the probe missed.
func TestBuildTopology_KeepsADeclarationTheProbeMissed(t *testing.T) {
	pmt := pmtTrackObservations([]ports.LiveAudioTrack{
		{PID: 257, Codec: "ac3", Language: "deu", Channels: 2},
		{PID: 258, Codec: "mp2", Language: "eng", Channels: 2},
	})
	probe := []audiotopology.ProbeTrackObservation{
		{PID: 257, StreamIndex: 0, Codec: "ac3", Channels: 2, Language: "deu"},
	}

	topo := audiotopology.BuildTopology("1:0:19:83:6:85:C00000:0:0:0:", pmt, probe, nil, time.Now())

	if len(topo.Tracks) != 2 {
		t.Fatalf("topology has %d tracks, want both PIDs", len(topo.Tracks))
	}
	found := false
	for _, tr := range topo.Tracks {
		if tr.PID == 258 && tr.Language.Code == "eng" {
			found = true
		}
	}
	if !found {
		t.Error("the PMT-only track was dropped; a declaration the probe missed is exactly what this feeds in for")
	}
}

// The scenario review found once the declarations started reaching the plan: the
// PMT names two languages, the startup snapshot only shows one. Before the filter
// the missing track fell back to the first audio stream, so the German stream was
// published twice - once correctly, once advertised as English.
func TestSplitMappableTracks_DropsATrackWithNoInputStream(t *testing.T) {
	streams := map[uint16]liveAudioStream{
		101: {Index: 0, ID: "0x65", CodecName: "ac3", Channels: 2},
	}
	tracks := []audiotopology.TrackPlan{
		{PID: 101, Name: "Deutsch"},
		{PID: 102, Name: "English"},
	}

	mappable, unmappable := splitMappableTracks(tracks, streams)

	if len(mappable) != 1 || mappable[0].PID != 101 {
		t.Errorf("mappable = %+v, want only the probed PID 101", mappable)
	}
	if len(unmappable) != 1 || unmappable[0].PID != 102 {
		t.Errorf("unmappable = %+v, want the PMT-only PID 102", unmappable)
	}
}

// The input must survive the split intact, because the caller logs one half after
// reading the other. An in-place filter would rewrite the array under it.
func TestSplitMappableTracks_LeavesTheInputIntact(t *testing.T) {
	streams := map[uint16]liveAudioStream{101: {Index: 0}}
	tracks := []audiotopology.TrackPlan{
		{PID: 101, Name: "Deutsch"},
		{PID: 102, Name: "English"},
		{PID: 103, Name: "Français"},
	}

	_, unmappable := splitMappableTracks(tracks, streams)

	if len(tracks) != 3 || tracks[1].PID != 102 || tracks[2].PID != 103 {
		t.Errorf("input was mutated: %+v", tracks)
	}
	if len(unmappable) != 2 {
		t.Fatalf("unmappable has %d entries, want 2", len(unmappable))
	}
	if unmappable[0].Name != "English" || unmappable[1].Name != "Français" {
		t.Errorf("unmappable = %+v, want the two names preserved for the log", unmappable)
	}
}

// Nothing to drop is the ordinary case and must stay untouched.
func TestSplitMappableTracks_KeepsEverythingWhenAllProbed(t *testing.T) {
	streams := map[uint16]liveAudioStream{101: {Index: 0}, 102: {Index: 1}}
	tracks := []audiotopology.TrackPlan{{PID: 101}, {PID: 102}}

	mappable, unmappable := splitMappableTracks(tracks, streams)

	if len(mappable) != 2 {
		t.Errorf("mappable has %d entries, want 2", len(mappable))
	}
	if len(unmappable) != 0 {
		t.Errorf("unmappable = %+v, want none", unmappable)
	}
}
