// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ring

import "testing"

// Live TV is not one audio track with properties. A programme carries 0..N tracks
// at once - the broadcast language, a second language, audio description, the
// original soundtrack - and that whole set changes while the session runs: a
// track's layout moves, a track appears, a track goes away. These tests exist so
// what was built is a multi-track observer rather than an excellent single-track
// one.

const (
	mtGerman  = 101
	mtEnglish = 102
	mtThird   = 103
)

type mtStream struct {
	pid uint16
	es  []byte
}

// pushInterleaved feeds the streams packet by packet in turn, the way a
// transponder delivers them. Feeding one track to completion and then the next
// would let a shared-state bug pass unnoticed.
func pushInterleaved(t *testing.T, r *MasterRing, streams ...mtStream) {
	t.Helper()

	packets := make([][][]byte, len(streams))
	longest := 0
	for i, s := range streams {
		packets[i] = audioPackets(s.pid, s.es, false)
		if len(packets[i]) > longest {
			longest = len(packets[i])
		}
	}

	for i := 0; i < longest; i++ {
		for _, p := range packets {
			if i < len(p) {
				pushAll(t, r, p[i])
			}
		}
	}
}

func audioPIDsOf(r *MasterRing) []uint16 {
	var pids []uint16
	for _, track := range r.ReadinessFacts().AudioTracks {
		pids = append(pids, track.PID)
	}
	return pids
}

func assertPIDs(t *testing.T, r *MasterRing, want ...uint16) {
	t.Helper()

	got := audioPIDsOf(r)
	if len(got) != len(want) {
		t.Fatalf("AudioTracks PIDs = %v, want exactly %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("AudioTracks PIDs = %v, want exactly %v", got, want)
		}
	}
}

func assertObserved(t *testing.T, r *MasterRing, pid uint16, channels int) {
	t.Helper()

	if got := observedTrack(t, r, pid).Observed.Channels; got != channels {
		t.Errorf("PID %d observed %d channels, want %d", pid, got, channels)
	}
}

// Two AC-3 tracks running side by side hold different layouts at the same time,
// and a change on one does not move the other. A single shared observation would
// pass the first half of this and fail the second.
func TestMasterRing_TracksAreObservedIndependently(t *testing.T) {
	r := NewMasterRing(8000 * TSPacketSize)
	defer r.Close()

	pushAll(t, r, obsPAT(), obsPMT(0,
		ac3Track(mtGerman, "deu", 0x85),
		ac3Track(mtEnglish, "eng", 0x82),
	))

	pushInterleaved(t, r,
		mtStream{mtGerman, obsAC3Run(obsByte6Surround51, 4)},
		mtStream{mtEnglish, obsAC3Run(obsByte6Stereo, 4)},
	)

	assertObserved(t, r, mtGerman, 6)
	assertObserved(t, r, mtEnglish, 2)

	// The two swap layouts. Each must follow its own stream.
	pushInterleaved(t, r,
		mtStream{mtGerman, obsAC3Run(obsByte6Stereo, 4)},
		mtStream{mtEnglish, obsAC3Run(obsByte6Surround51, 4)},
	)

	assertObserved(t, r, mtGerman, 2)
	assertObserved(t, r, mtEnglish, 6)

	// And the languages stayed with their own tracks.
	if lang := observedTrack(t, r, mtGerman).Language; lang != "deu" {
		t.Errorf("PID %d language = %q, want deu", mtGerman, lang)
	}
	if lang := observedTrack(t, r, mtEnglish).Language; lang != "eng" {
		t.Errorf("PID %d language = %q, want eng", mtEnglish, lang)
	}
}

// A track that has carried no audio stays unknown, however much its neighbour has
// established. One track's observation is never evidence about another.
func TestMasterRing_ObservationDoesNotLeakToASilentTrack(t *testing.T) {
	r := NewMasterRing(8000 * TSPacketSize)
	defer r.Close()

	pushAll(t, r, obsPAT(), obsPMT(0,
		ac3Track(mtGerman, "deu", 0x85),
		ac3Track(mtEnglish, "eng", 0x85),
	))
	pushAudio(t, r, mtGerman, obsAC3Run(obsByte6Surround51, 4))

	assertObserved(t, r, mtGerman, 6)

	if got := observedTrack(t, r, mtEnglish).Observed; got.Known() {
		t.Errorf("PID %d observed %+v, want nothing - it has carried no audio", mtEnglish, got)
	}

	// The declaration is still there on both. Declared and Observed are separate
	// statements about the same track, and one being empty says nothing about the
	// other.
	for _, pid := range []uint16{mtGerman, mtEnglish} {
		if d := observedTrack(t, r, pid).Declared; !d.Multichannel {
			t.Errorf("PID %d declared %+v, want the multichannel class", pid, d)
		}
	}
}

// The whole set moves while the session runs: layouts change, a track appears, a
// track goes away. At every point AudioTracks is exactly the current set and each
// PID carries its own observation.
func TestMasterRing_TrackSetFollowsThePMTThroughAProgramme(t *testing.T) {
	r := NewMasterRing(8000 * TSPacketSize)
	defer r.Close()

	// Advertisements: German 5.1, English stereo.
	pushAll(t, r, obsPAT(), obsPMT(0,
		ac3Track(mtGerman, "deu", 0x85),
		ac3Track(mtEnglish, "eng", 0x82),
	))
	pushInterleaved(t, r,
		mtStream{mtGerman, obsAC3Run(obsByte6Surround51, 4)},
		mtStream{mtEnglish, obsAC3Run(obsByte6Stereo, 4)},
	)

	assertPIDs(t, r, mtGerman, mtEnglish)
	assertObserved(t, r, mtGerman, 6)
	assertObserved(t, r, mtEnglish, 2)

	// The film starts: a third track appears, and the two existing ones swap
	// layouts. No session restart.
	pushAll(t, r, obsPMT(1,
		ac3Track(mtGerman, "deu", 0x82),
		ac3Track(mtEnglish, "eng", 0x85),
		ac3Track(mtThird, "deu", 0x85),
	))
	pushInterleaved(t, r,
		mtStream{mtGerman, obsAC3Run(obsByte6Stereo, 4)},
		mtStream{mtEnglish, obsAC3Run(obsByte6Surround51, 4)},
		mtStream{mtThird, obsAC3Run(obsByte6Surround51, 4)},
	)

	assertPIDs(t, r, mtGerman, mtEnglish, mtThird)
	assertObserved(t, r, mtGerman, 2)
	assertObserved(t, r, mtEnglish, 6)
	assertObserved(t, r, mtThird, 6)

	// The English track goes away.
	pushAll(t, r, obsPMT(2,
		ac3Track(mtGerman, "deu", 0x82),
		ac3Track(mtThird, "deu", 0x85),
	))

	assertPIDs(t, r, mtGerman, mtThird)

	// Payload still arriving on the PID the table dropped reaches nothing.
	pushAudio(t, r, mtEnglish, obsAC3Run(obsByte6Surround51, 8))
	assertPIDs(t, r, mtGerman, mtThird)

	pushInterleaved(t, r,
		mtStream{mtGerman, obsAC3Run(obsByte6Stereo, 4)},
		mtStream{mtThird, obsAC3Run(obsByte6Surround51, 4)},
	)
	assertObserved(t, r, mtGerman, 2)
	assertObserved(t, r, mtThird, 6)
}

// A PID reused for a different codec gets a fresh observer, not the previous
// stream's history. The codec here is one this path does read, so an observer is
// built either way - what must not survive is what the old one had established.
func TestMasterRing_PIDReusedForAnotherReadCodecStartsClean(t *testing.T) {
	r := NewMasterRing(8000 * TSPacketSize)
	defer r.Close()

	pushAll(t, r, obsPAT(), obsPMT(0, ac3Track(mtGerman, "deu", 0x85)))
	pushAudio(t, r, mtGerman, obsAC3Run(obsByte6Surround51, 4))
	assertObserved(t, r, mtGerman, 6)

	// Same PID, now E-AC-3.
	pushAll(t, r, obsPMT(1, obsTrack{mtGerman, 0x06, append([]byte{0x7A, 0x02, 0x80, 0x85}, langDescriptor("deu")...)}))

	track := observedTrack(t, r, mtGerman)
	if track.Codec != "eac3" {
		t.Fatalf("Codec = %q, want eac3", track.Codec)
	}
	if track.Observed.Known() {
		t.Errorf("Observed = %+v, want nothing - that is the previous stream's layout", track.Observed)
	}
}

// An audio layout change is a new fact about a track, not a new stream. Faking a
// generation would tell every subscriber to re-attach because a film started.
func TestMasterRing_LayoutChangeAcrossTracksDoesNotMoveTheGeneration(t *testing.T) {
	r := NewMasterRing(8000 * TSPacketSize)
	defer r.Close()

	pushAll(t, r, obsPAT(), obsPMT(0,
		ac3Track(mtGerman, "deu", 0x85),
		ac3Track(mtEnglish, "eng", 0x85),
	))
	pushInterleaved(t, r,
		mtStream{mtGerman, obsAC3Run(obsByte6Stereo, 4)},
		mtStream{mtEnglish, obsAC3Run(obsByte6Stereo, 4)},
	)

	before := r.ReadinessFacts().Generation

	pushInterleaved(t, r,
		mtStream{mtGerman, obsAC3Run(obsByte6Surround51, 4)},
		mtStream{mtEnglish, obsAC3Run(obsByte6Surround51, 4)},
	)

	assertObserved(t, r, mtGerman, 6)
	assertObserved(t, r, mtEnglish, 6)

	if now := r.ReadinessFacts().Generation; now != before {
		t.Errorf("Generation moved from %d to %d - an observed layout change is not a new stream", before, now)
	}
}
