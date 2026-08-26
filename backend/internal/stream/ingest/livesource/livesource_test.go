// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package livesource

import (
	"context"
	"testing"

	"github.com/ManuGH/xg2g/internal/stream/ingest/ring"
)

// The program number decides which program of a multi-program transport stream
// the ingest follows. Reading it wrong points a session at the wrong service on
// exactly the transponders where it matters.
func TestTargetProgramFromServiceRef(t *testing.T) {
	tests := []struct {
		name       string
		serviceRef string
		want       uint16
	}{
		{"hex service id", "1:0:19:83:6:85:C00000:0:0:0:", 0x83},
		{"lowercase hex", "1:0:19:2b5c:3ef:1:c00000:0:0:0:", 0x2b5c},
		{"zero service id", "1:0:19:0:6:85:C00000:0:0:0:", 0},
		{"too few fields", "1:0:19", 0},
		{"empty", "", 0},
		{"non-hex service id", "1:0:19:zzzz:6:85:C00000:0:0:0:", 0},
		{"service id out of range", "1:0:19:1FFFF:6:85:C00000:0:0:0:", 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := targetProgramFromServiceRef(tc.serviceRef); got != tc.want {
				t.Errorf("targetProgramFromServiceRef(%q) = %#x, want %#x", tc.serviceRef, got, tc.want)
			}
		})
	}
}

// A provider with no manager must refuse rather than build one of its own: a
// second manager would open a second connection to the receiver for a service
// that already has one, which is the outcome shared ingest exists to prevent.
func TestAcquireLiveSource_RefusesWithoutAManager(t *testing.T) {
	p := NewLazyProvider(nil, "receiver.local", 8001)
	if _, err := p.AcquireLiveSource(context.Background(), "1:0:19:83:6:85:C00000:0:0:0:"); err == nil {
		t.Error("AcquireLiveSource succeeded with a nil manager getter, want an error")
	}

	p = NewProvider(nil, "receiver.local", 8001)
	if _, err := p.AcquireLiveSource(context.Background(), "1:0:19:83:6:85:C00000:0:0:0:"); err == nil {
		t.Error("AcquireLiveSource succeeded with a nil manager, want an error")
	}
}

func TestAcquireLiveSource_RejectsAnUnusableServiceRef(t *testing.T) {
	p := NewLazyProvider(nil, "receiver.local", 8001)
	if _, err := p.AcquireLiveSource(context.Background(), ""); err == nil {
		t.Error("AcquireLiveSource accepted an empty service reference")
	}
}

// The facts handed to the media path have to mean what the ring meant, or a
// consumer decides on a stream it is not looking at.
func TestFactsFrom_CarriesTheRingsMeaning(t *testing.T) {
	facts := factsFrom(ring.ReadinessFacts{
		Generation:        7,
		HasPAT:            true,
		HasPMT:            true,
		VideoPID:          256,
		VideoCodec:        ring.CodecH264,
		ParameterSetsSeen: true,
		CleanEntryPoints:  3,
		CleanAccessUnits:  42,
		Scrambling:        ring.StreamScrambling{VideoScrambled: 5, VideoClear: 900},
	})

	if facts.Generation != 7 {
		t.Errorf("Generation = %d, want 7", facts.Generation)
	}
	if facts.VideoCodec != "h264" {
		t.Errorf("VideoCodec = %q, want %q", facts.VideoCodec, "h264")
	}
	if facts.ScrambledVideoPackets != 5 || facts.ClearVideoPackets != 900 {
		t.Errorf("scrambling counters = %d/%d, want 5/900", facts.ScrambledVideoPackets, facts.ClearVideoPackets)
	}
	if !facts.Descrambled() {
		t.Error("Descrambled() = false with clean access units present")
	}
	if !facts.Joinable() {
		t.Error("Joinable() = false with PAT, PMT, parameter sets and a clean entry point")
	}
}

// The two questions the old TS preflight opened a second connection to answer
// must not be answered "yes" by a stream that has not shown it yet.
func TestFacts_NotJoinableOrDescrambledWhenNothingClean(t *testing.T) {
	scrambled := factsFrom(ring.ReadinessFacts{
		HasPAT: true, HasPMT: true, ParameterSetsSeen: true,
		CleanEntryPoints: 0, CleanAccessUnits: 0,
		Scrambling: ring.StreamScrambling{VideoScrambled: 500},
	})
	if scrambled.Descrambled() {
		t.Error("Descrambled() = true with no clean access unit")
	}
	if scrambled.Joinable() {
		t.Error("Joinable() = true with no clean entry point")
	}

	noTopology := factsFrom(ring.ReadinessFacts{CleanEntryPoints: 2, CleanAccessUnits: 9})
	if noTopology.Joinable() {
		t.Error("Joinable() = true without PAT/PMT and parameter sets")
	}
}
