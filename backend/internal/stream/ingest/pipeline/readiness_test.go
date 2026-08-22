// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package pipeline

import (
	"io"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/ManuGH/xg2g/internal/stream/ingest/ring"
)

func quietLogger() zerolog.Logger { return zerolog.New(io.Discard) }

// presentableFacts is a stream that satisfies every criterion.
func presentableFacts() ring.ReadinessFacts {
	return ring.ReadinessFacts{
		Generation:        1,
		HasPAT:            true,
		HasPMT:            true,
		PMTVersion:        3,
		ProgramNumber:     0x132F,
		VideoPID:          0x780,
		VideoCodec:        ring.CodecH264,
		AudioPIDs:         []uint16{0x781},
		ParameterSetsSeen: true,
		RandomAccess:      ring.RandomAccessObservation{IntraPoints: 1, RecoveryPointSEIs: 1},
		Scrambling: ring.StreamScrambling{
			VideoClear: 900, VideoClearRun: 900,
			AudioClear: 40, AudioClearRun: 40,
			AudioPIDs: []uint16{0x781},
		},
		CleanEntryPoints: 1,
		AttachAvailable:  true,
	}
}

func TestReadiness_PresentableStreamSatisfiesEveryCriterion(t *testing.T) {
	start := time.Unix(0, 0)
	o := NewReadinessObserver(start, quietLogger())
	o.Observe(presentableFacts(), start.Add(1200*time.Millisecond))

	snap := o.Snapshot()
	if !snap.Ready {
		t.Fatalf("expected ready, pending: %v", snap.Pending)
	}
	if snap.ReadyAfter != 1200*time.Millisecond {
		t.Fatalf("ready time must be the slowest criterion, got %v", snap.ReadyAfter)
	}
	if len(snap.Met) != len(AllReadinessCriteria) {
		t.Fatalf("expected all %d criteria met, got %d", len(AllReadinessCriteria), len(snap.Met))
	}
}

// Each criterion has to be able to fail on its own, and say why: an observation that
// reports "not ready" without naming the reason cannot answer whether the definition
// itself is right.
func TestReadiness_EachCriterionFailsForItsOwnReason(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*ring.ReadinessFacts)
		expect  ReadinessCriterion
		because string
	}{
		{"no PAT", func(f *ring.ReadinessFacts) { f.HasPAT = false }, CriterionTransport, "no PAT"},
		{"PMT incomplete", func(f *ring.ReadinessFacts) { f.HasPMT = false }, CriterionTransport, "PAT seen, PMT not yet complete"},
		{"no decodable video", func(f *ring.ReadinessFacts) { f.VideoCodec = ring.CodecUnknown }, CriterionProgram, "PMT names no video stream this build can decode"},
		{"no audio stream", func(f *ring.ReadinessFacts) { f.AudioPIDs = nil; f.Scrambling.AudioPIDs = nil }, CriterionProgram, "PMT names no audio stream"},
		{"nothing descrambled", func(f *ring.ReadinessFacts) {
			f.CleanEntryPoints = 0
			f.Scrambling.VideoClear = 0
			f.Scrambling.VideoScrambled = 4000
		}, CriterionDescrambled, "video is scrambled; receiver is not descrambling this service"},
		{"audio still scrambled", func(f *ring.ReadinessFacts) { f.Scrambling.AudioClearRun = 0 }, CriterionDescrambled, "video is clear but audio is not"},
		{"no entry point", func(f *ring.ReadinessFacts) {
			f.RandomAccess = ring.RandomAccessObservation{PredictedRejected: 7}
		}, CriterionRandomAccess, "only predicted pictures so far (7 rejected)"},
		{"entry point aged out", func(f *ring.ReadinessFacts) { f.AttachAvailable = false }, CriterionRandomAccess, "entry point was found but has already left the buffer"},
		{"no parameter sets", func(f *ring.ReadinessFacts) { f.ParameterSetsSeen = false }, CriterionCodecConfig, "decoder configuration not seen"},
		{"no clear audio", func(f *ring.ReadinessFacts) { f.Scrambling.AudioClear = 0 }, CriterionAudio, "no clear audio packet yet"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			facts := presentableFacts()
			tc.mutate(&facts)

			start := time.Unix(0, 0)
			o := NewReadinessObserver(start, quietLogger())
			o.Observe(facts, start.Add(time.Second))

			snap := o.Snapshot()
			if snap.Ready {
				t.Fatal("expected not ready")
			}
			reason, pending := snap.Pending[tc.expect]
			if !pending {
				t.Fatalf("expected %s to be pending, pending set was %v", tc.expect, snap.Pending)
			}
			if reason != tc.because {
				t.Fatalf("reason for %s:\n got %q\nwant %q", tc.expect, reason, tc.because)
			}
		})
	}
}

// A criterion that has been satisfied stays satisfied within a generation, so a
// transient dip cannot make an already-presentable stream look unready.
func TestReadiness_SatisfiedCriteriaAreNotRevoked(t *testing.T) {
	start := time.Unix(0, 0)
	o := NewReadinessObserver(start, quietLogger())
	o.Observe(presentableFacts(), start.Add(500*time.Millisecond))

	dipped := presentableFacts()
	dipped.AttachAvailable = false
	dipped.Scrambling.AudioClearRun = 0
	o.Observe(dipped, start.Add(900*time.Millisecond))

	if snap := o.Snapshot(); !snap.Ready {
		t.Fatalf("readiness must not be revoked within a generation, pending: %v", snap.Pending)
	}
}

// A PMT version bump or a codec change makes every timestamp collected so far
// describe a stream that is no longer playing.
func TestReadiness_ReIdentificationDiscardsEarlierObservation(t *testing.T) {
	start := time.Unix(0, 0)
	o := NewReadinessObserver(start, quietLogger())
	o.Observe(presentableFacts(), start.Add(400*time.Millisecond))
	if !o.Snapshot().Ready {
		t.Fatal("precondition: first identity should be ready")
	}

	// The programme re-identifies: same transport, new PMT version, and the ring has
	// dropped everything it knew about the elementary streams.
	changed := ring.ReadinessFacts{
		Generation:    2,
		HasPAT:        true,
		HasPMT:        true,
		PMTVersion:    4,
		ProgramNumber: 0x132F,
		VideoPID:      0x780,
		VideoCodec:    ring.CodecH264,
	}
	o.Observe(changed, start.Add(500*time.Millisecond))

	snap := o.Snapshot()
	if snap.Ready {
		t.Fatal("a re-identified stream must not inherit the previous readiness")
	}
	if snap.Resets != 1 {
		t.Fatalf("expected the reset to be counted, got %d", snap.Resets)
	}

	// Per-generation timing restarts; the ingest-relative timing does not, so the two
	// stay comparable with a client that measures from one fixed instant.
	full := presentableFacts()
	full.Generation = 2
	full.PMTVersion = 4
	o.Observe(full, start.Add(1500*time.Millisecond))

	snap = o.Snapshot()
	if snap.ReadyAfter != time.Second {
		t.Fatalf("readiness must be timed from the new identity: got %v, want 1s", snap.ReadyAfter)
	}
	if snap.ReadyAfterIngest != 1500*time.Millisecond {
		t.Fatalf("ingest-relative readiness must survive the reset: got %v, want 1.5s", snap.ReadyAfterIngest)
	}
}

// The ring increments its generation twice during an ordinary tune - once for the
// first PAT, once for the first PMT. Neither is a change of programme, and treating
// them as one restarted the clock mid-tune and made every reported timing relative
// to a moment that meant nothing. This is what the first live measurement showed.
func TestReadiness_InitialPSILatchIsNotAReIdentification(t *testing.T) {
	start := time.Unix(0, 0)
	o := NewReadinessObserver(start, quietLogger())

	// Nothing known yet.
	o.Observe(ring.ReadinessFacts{Generation: 0}, start)
	// First PAT: the ring invalidates and the generation moves to 1.
	o.Observe(ring.ReadinessFacts{Generation: 1, HasPAT: true}, start.Add(1400*time.Millisecond))
	// First PMT: it invalidates again and the generation moves to 2.
	facts := presentableFacts()
	facts.Generation = 2
	o.Observe(facts, start.Add(1900*time.Millisecond))

	snap := o.Snapshot()
	if snap.Resets != 0 {
		t.Fatalf("the initial PSI latch must not count as a re-identification, got %d resets", snap.Resets)
	}
	if !snap.Ready {
		t.Fatalf("expected ready, pending: %v", snap.Pending)
	}
	if snap.ReadyAfter != 1900*time.Millisecond {
		t.Fatalf("readiness must be timed from the ingest, got %v", snap.ReadyAfter)
	}
	if snap.ReadyAfterIngest != snap.ReadyAfter {
		t.Fatalf("with no reset the two timings must agree: %v vs %v", snap.ReadyAfterIngest, snap.ReadyAfter)
	}
}

// A snapshot that arrives late, carrying the generation of a preparation that was
// already abandoned, must not touch the observation of the one that replaced it.
func TestReadiness_StaleGenerationSnapshotDoesNotDisturbTheCurrentOne(t *testing.T) {
	start := time.Unix(0, 0)
	o := NewReadinessObserver(start, quietLogger())

	first := presentableFacts() // generation 1
	o.Observe(first, start.Add(300*time.Millisecond))

	second := presentableFacts()
	second.Generation = 2
	second.PMTVersion = 9
	o.Observe(second, start.Add(400*time.Millisecond))
	readyAfter := o.Snapshot().ReadyAfter

	// The abandoned generation reports in late.
	o.Observe(first, start.Add(2*time.Second))

	snap := o.Snapshot()
	if snap.Generation != 2 {
		t.Fatalf("a stale snapshot must not take the observation back to generation %d", snap.Generation)
	}
	if snap.ReadyAfter != readyAfter {
		t.Fatalf("a stale snapshot must not change the timing: got %v, want %v", snap.ReadyAfter, readyAfter)
	}
}

func TestSanitizeZapID(t *testing.T) {
	cases := map[string]string{
		"":                         "unset",
		"zap-14":                   "zap-14",
		"ios:42.1_a":               "ios:42.1_a",
		"drop these: \n\t<script>": "dropthese:script",
		"!!!":                      "invalid",
	}
	for in, want := range cases {
		if got := sanitizeZapID(in); got != want {
			t.Fatalf("sanitizeZapID(%q) = %q, want %q", in, got, want)
		}
	}

	long := make([]byte, 200)
	for i := range long {
		long[i] = 'a'
	}
	if got := sanitizeZapID(string(long)); len(got) != maxZapIDLength {
		t.Fatalf("expected the identifier to be capped at %d, got %d", maxZapIDLength, len(got))
	}
}
