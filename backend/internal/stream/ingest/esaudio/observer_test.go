// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package esaudio

import (
	"bytes"
	"testing"
)

// frames concatenates n identical syncframes into one elementary stream run.
func frames(byte6 byte, n int) []byte {
	var out []byte
	for i := 0; i < n; i++ {
		out = append(out, ac3Frame(byte6)...)
	}
	return out
}

func TestObserver_NothingSeenIsNotStereo(t *testing.T) {
	o := NewObserver()

	if got := o.Current(); got.Known() || got.Channels != 0 {
		t.Errorf("Current() = %+v, want the zero observation", got)
	}

	o.Feed(bytes.Repeat([]byte{0x00, 0x11, 0x22}, 200))

	if got := o.Current(); got.Known() {
		t.Errorf("Current() = %+v, want nothing established from payload that says nothing", got)
	}
}

// One frame is a guess. The layout is only reported once the stream has repeated
// it, so a byte pair that happened to look like a syncword cannot become a fact.
func TestObserver_EstablishesOnlyAfterAgreement(t *testing.T) {
	o := NewObserver()

	for i := 1; i < stableFrames; i++ {
		o.Feed(ac3Frame(ac3Byte6Surround51))
		if got := o.Current(); got.Known() {
			t.Fatalf("established after %d frame(s): %+v, want to wait for %d", i, got, stableFrames)
		}
	}

	o.Feed(ac3Frame(ac3Byte6Surround51))

	got := o.Current()
	if got.Channels != 6 || !got.LFE || got.Acmod != 7 {
		t.Errorf("Current() = %+v, want 6 channels with LFE from acmod 7", got)
	}
	if got.Frames != uint64(stableFrames) {
		t.Errorf("Frames = %d, want %d", got.Frames, stableFrames)
	}
}

// The case the whole observer exists for: a service that runs stereo between
// programmes and 5.1 during the film, on one PID, with no PMT change to announce
// it. A reading taken once at session start is wrong for the rest of the session.
func TestObserver_FollowsALayoutChangeOnTheSameStream(t *testing.T) {
	o := NewObserver()

	o.Feed(frames(ac3Byte6Stereo, stableFrames))
	if got := o.Current(); got.Channels != 2 {
		t.Fatalf("Channels = %d, want 2 to start", got.Channels)
	}

	o.Feed(frames(ac3Byte6Surround51, stableFrames))
	if got := o.Current(); got.Channels != 6 || !got.LFE {
		t.Errorf("Current() = %+v, want the stream's move to 5.1 to be followed", got)
	}

	o.Feed(frames(ac3Byte6Stereo, stableFrames))
	if got := o.Current(); got.Channels != 2 || got.LFE {
		t.Errorf("Current() = %+v, want the move back to stereo to be followed", got)
	}
}

// Until the new layout has proved itself the old one stands. It never falls back
// to unknown and never falls back to stereo in between.
func TestObserver_HoldsTheProvenLayoutWhileANewOneIsStillArriving(t *testing.T) {
	o := NewObserver()
	o.Feed(frames(ac3Byte6Surround51, stableFrames))

	for i := 1; i < stableFrames; i++ {
		o.Feed(ac3Frame(ac3Byte6Stereo))
		if got := o.Current(); got.Channels != 6 {
			t.Fatalf("Channels = %d after %d stereo frame(s), want 6 until the change is proved", got.Channels, i)
		}
	}
}

// A run broken by noise starts over rather than counting toward a change.
func TestObserver_InterruptedRunDoesNotEstablish(t *testing.T) {
	o := NewObserver()
	o.Feed(frames(ac3Byte6Surround51, stableFrames))

	for i := 0; i < stableFrames*3; i++ {
		o.Feed(ac3Frame(ac3Byte6Stereo))
		o.Feed(ac3Frame(ac3Byte6Mono))
	}

	if got := o.Current(); got.Channels != 6 {
		t.Errorf("Channels = %d, want 6 - neither alternating layout ever agreed with itself", got.Channels)
	}
}

// Elementary stream bytes arrive in transport-sized pieces that cut frames
// anywhere, including inside a header.
func TestObserver_FrameSplitAcrossChunks(t *testing.T) {
	es := frames(ac3Byte6Surround51, stableFrames+1)

	for _, chunk := range []int{1, 3, 7, 64, 127} {
		t.Run("chunk size", func(t *testing.T) {
			o := NewObserver()
			for i := 0; i < len(es); i += chunk {
				end := i + chunk
				if end > len(es) {
					end = len(es)
				}
				o.Feed(es[i:end])
			}
			if got := o.Current(); got.Channels != 6 {
				t.Errorf("chunk %d: Channels = %d, want 6", chunk, got.Channels)
			}
		})
	}
}

// A frame cut short at the end of the stream contributes nothing and does not
// disturb what was already established.
func TestObserver_TruncatedTrailingFrame(t *testing.T) {
	o := NewObserver()
	es := frames(ac3Byte6Surround51, stableFrames)
	o.Feed(append(es, ac3Frame(ac3Byte6Stereo)[:4]...))

	got := o.Current()
	if got.Channels != 6 {
		t.Errorf("Channels = %d, want 6", got.Channels)
	}
	if got.Frames != uint64(stableFrames) {
		t.Errorf("Frames = %d, want %d - a partial frame is not a frame", got.Frames, stableFrames)
	}
}

// Frame payload is compressed audio and contains the sync pattern by chance. The
// scan steps over a parsed frame rather than through it, so bytes inside one are
// never read as a header.
func TestObserver_SyncPatternInsideFramePayloadIsNotAFrame(t *testing.T) {
	o := NewObserver()

	for i := 0; i < stableFrames; i++ {
		f := ac3Frame(ac3Byte6Surround51)
		// A complete, well-formed stereo header buried in the payload.
		copy(f[64:], ac3Frame(ac3Byte6Stereo)[:headerBytes])
		o.Feed(f)
	}

	got := o.Current()
	if got.Channels != 6 {
		t.Errorf("Channels = %d, want 6", got.Channels)
	}
	if got.Frames != uint64(stableFrames) {
		t.Errorf("Frames = %d, want %d - payload was scanned as headers", got.Frames, stableFrames)
	}
}

// Garbage between frames is skipped without disturbing the run, because the scan
// resumes looking for a syncword rather than giving up.
func TestObserver_ResumesAfterGarbage(t *testing.T) {
	o := NewObserver()
	junk := bytes.Repeat([]byte{0x0B, 0x00, 0xFF}, 40)

	for i := 0; i < stableFrames; i++ {
		o.Feed(junk)
		o.Feed(ac3Frame(ac3Byte6Surround51))
	}

	if got := o.Current(); got.Channels != 6 {
		t.Errorf("Channels = %d, want 6", got.Channels)
	}
}

// A dependent substream is recorded as a fact about the stream's shape without
// becoming a count, so a consumer can tell that the independent substream's
// number may not be the whole programme.
func TestObserver_DependentSubstreamIsFlaggedNotCounted(t *testing.T) {
	o := NewObserver()

	for i := 0; i < stableFrames; i++ {
		o.Feed(eac3Frame(0, 0, 7, true))
		o.Feed(eac3Frame(1, 0, 2, false))
	}

	got := o.Current()
	if got.Channels != 6 {
		t.Errorf("Channels = %d, want 6 from the independent substream", got.Channels)
	}
	if !got.DependentSubstream {
		t.Error("DependentSubstream = false, want true")
	}
}
