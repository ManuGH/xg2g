// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package esaudio

import "testing"

// The invariants below are the ones that hold for any bytes at all, which is
// what makes them worth fuzzing. A finding here is promoted into the shared
// corpus rather than fixed only in the fuzzer: the corpus is what the Rust
// observer is held to, and a case only this fuzzer knows about would be a case
// the second implementation never has to satisfy.

// checkObservation states what must be true of an observation whatever was fed.
func checkObservation(t *testing.T, o Observation) {
	t.Helper()
	if o.Known() != (o.Channels > 0) {
		t.Fatalf("Known() disagrees with the channel count: %+v", o)
	}
	if o.Channels > 0 && !o.HasAcmod {
		t.Fatalf("a channel count without the acmod it came from: %+v", o)
	}
	// acmod 7 plus LFE is the widest layout either syntax can name.
	if o.Channels > 6 {
		t.Fatalf("no layout in A/52 has %d channels: %+v", o.Channels, o)
	}
	if o.Acmod > 7 {
		t.Fatalf("acmod is three bits: %+v", o)
	}
}

func FuzzParseSyncFrame(f *testing.F) {
	f.Add(ac3(7, true))
	f.Add(ac3(2, false))
	f.Add(eac3(0x00, 0, 7, true, 128))
	f.Add(eac3(0x01, 0, 7, true, 128))
	f.Add(ac3FrameWith(0x03, 0x00, 8, 0xEB, 128))
	f.Add([]byte{0x0B, 0x77})
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, b []byte) {
		h, ok := ParseSyncFrame(b)
		if !ok {
			return
		}
		if h.SizeBytes < 0 {
			t.Fatalf("negative frame size: %+v", h)
		}
		if h.Channels < 0 || h.Channels > 6 {
			t.Fatalf("impossible channel count: %+v", h)
		}
		if h.Dependent && h.Channels != 0 {
			t.Fatalf("a dependent substream reported a count: %+v", h)
		}
	})
}

func FuzzObserverFeed(f *testing.F) {
	f.Add(join(ac3(7, true), ac3(7, true), ac3(7, true)), 64)
	f.Add(join(ac3(2, false), ac3(7, true)), 7)
	f.Add(join(eac3(0x01, 0, 7, true, 128), eac3(0x00, 0, 2, false, 128)), 3)
	f.Add([]byte{0x0B, 0x77, 0x0B, 0x77, 0x0B, 0x77, 0x0B, 0x77}, 1)

	f.Fuzz(func(t *testing.T, stream []byte, chunk int) {
		if chunk <= 0 {
			chunk = 1
		}
		if chunk > 4096 {
			chunk = 4096
		}
		o := NewObserver()
		var lastFrames uint64
		for i := 0; i < len(stream); i += chunk {
			end := i + chunk
			if end > len(stream) {
				end = len(stream)
			}
			o.Feed(stream[i:end])
			got := o.Current()
			checkObservation(t, got)
			if got.Frames < lastFrames {
				t.Fatalf("the frame count went backwards: %d then %d", lastFrames, got.Frames)
			}
			lastFrames = got.Frames
		}
	})
}
