package ffmpeg

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
)

// buildTSStream builds a packet-aligned MPEG-TS buffer: scrambledPrefix packets
// with the transport_scrambling_control bits set, then clearTail clear packets.
func buildTSStream(scrambledPrefix, clearTail int) []byte {
	const p = 188
	total := scrambledPrefix + clearTail
	buf := make([]byte, total*p)
	for i := 0; i < total; i++ {
		off := i * p
		buf[off] = 0x47 // TS sync byte
		if i < scrambledPrefix {
			buf[off+3] = 0x40 // transport_scrambling_control set
		} else {
			buf[off+3] = 0x10 // payload bits only; scrambling bits clear
		}
	}
	return buf
}

// TestClassifyScramble_LockProneSkipsLockPrefix is the core of the fix: a tuner or
// relay source whose first packets carry the transport_scrambling_control bits but
// which clears once the control word locks is classified past that lock-in and read
// as clear — whereas classifying the SAME bytes whole flags them. The asymmetry is
// the regression guard: classify a lock-prone source over the whole sample and the
// "must read clear" assertion turns red.
func TestClassifyScramble_LockProneSkipsLockPrefix(t *testing.T) {
	// 3500 flagged packets then 3000 clear: whole-sample fraction is 0.538, over
	// the threshold, but the stream past the lock-in is clear.
	buf := buildTSStream(3500, 3000)

	locked := classifyScramble(buf, true)
	if locked.Verdict != ports.ScrambleVerdictClear {
		t.Fatalf("lock-prone source must read CLEAR past the lock prefix; got %s (fraction %.2f over %d packets, window %s)",
			locked.Verdict, locked.Fraction, locked.Classified, locked.Window)
	}
	if locked.Window != "post_lock" {
		t.Fatalf("expected post_lock window, got %q", locked.Window)
	}

	// Precondition: the whole-sample view of the SAME bytes IS over threshold — i.e.
	// skipping the lock prefix is exactly what flips the verdict (the negative control).
	whole := classifyScramble(buf, false)
	if whole.Verdict != ports.ScrambleVerdictScrambled {
		t.Fatalf("precondition: whole-sample verdict should be scrambled here; got %s (%.2f)", whole.Verdict, whole.Fraction)
	}
}

// TestClassifyScramble_FlaggedThroughoutStillScrambled guards against a blanket
// bypass: a source whose packets carry the bits all the way through must stay
// classified as scrambled no matter which window is used.
func TestClassifyScramble_FlaggedThroughoutStillScrambled(t *testing.T) {
	buf := buildTSStream(1024, 0)
	for _, lockProne := range []bool{true, false} {
		if got := classifyScramble(buf, lockProne); got.Verdict != ports.ScrambleVerdictScrambled {
			t.Fatalf("source flagged throughout must stay scrambled (lockProne=%v); got %s (%.2f over %d packets)",
				lockProne, got.Verdict, got.Fraction, got.Classified)
		}
	}
}

// TestClassifyScramble_DirectUnchanged confirms the direct (non-lock-prone) path is
// unchanged: a clear sample reads clear, a flagged sample reads flagged.
func TestClassifyScramble_DirectUnchanged(t *testing.T) {
	if got := classifyScramble(buildTSStream(0, 48), false); got.Verdict != ports.ScrambleVerdictClear {
		t.Fatalf("clear direct sample must read clear; got %s (%.2f)", got.Verdict, got.Fraction)
	}
	if got := classifyScramble(buildTSStream(48, 0), false); got.Verdict != ports.ScrambleVerdictScrambled {
		t.Fatalf("flagged direct sample must read flagged; got %s (%.2f over %d packets)", got.Verdict, got.Fraction, got.Classified)
	}
}

// TestClassifyScramble_TooSmallIsInconclusiveNotClear pins the fail-open that let an
// encrypted service through: a sample with too few packets to judge must report
// inconclusive. Reported as "clear" it is indistinguishable from proven-clear, and
// the caller then starts a transcode on an undecodable stream.
func TestClassifyScramble_TooSmallIsInconclusiveNotClear(t *testing.T) {
	// Fewer than tsScrambleMinPackets, all of them flagged.
	got := classifyScramble(buildTSStream(tsScrambleMinPackets-1, 0), false)
	if got.Verdict != ports.ScrambleVerdictInconclusive {
		t.Fatalf("a sample below the minimum must be inconclusive; got %s", got.Verdict)
	}

	// Same for a lock-prone source whose post-lock remainder is too thin.
	got = classifyScramble(buildTSStream(20, 20), true)
	if got.Verdict != ports.ScrambleVerdictInconclusive {
		t.Fatalf("thin post-lock remainder must be inconclusive; got %s (%d packets)", got.Verdict, got.Classified)
	}

	// And a buffer with no aligned packet at all.
	if got := classifyScramble(make([]byte, 100), false); got.Verdict != ports.ScrambleVerdictInconclusive {
		t.Fatalf("unaligned buffer must be inconclusive; got %s", got.Verdict)
	}
}

// TestClassifyScramble_AsymmetricConfidence pins the rule the measurements imply: a
// small window may only be trusted when it says "scrambled" or when it is entirely
// clear. A window that is small AND partly flagged is exactly the PSI/EPG burst
// zone that produced the false-clear verdicts, so it must be inconclusive.
func TestClassifyScramble_AsymmetricConfidence(t *testing.T) {
	// Small window, mostly clear but not entirely — cannot be trusted either way.
	burstZone := buildTSStream(10, 50) // 60 packets, fraction 0.167
	if got := classifyScramble(burstZone, false); got.Verdict != ports.ScrambleVerdictInconclusive {
		t.Fatalf("a small partly-flagged window is the burst zone and must be inconclusive; got %s (%.3f over %d)",
			got.Verdict, got.Fraction, got.Classified)
	}

	// Small window, entirely clear — a stream that is not encrypted carries no
	// scrambling bits at all, so this is trustworthy at any size. The clear-lead
	// fast path depends on it.
	if got := classifyScramble(buildTSStream(0, 128), false); got.Verdict != ports.ScrambleVerdictClear {
		t.Fatalf("an entirely clear window must read clear at any size; got %s", got.Verdict)
	}

	// Small window, mostly flagged — nothing can push a clear stream's fraction up,
	// so a high fraction is trustworthy at any size.
	if got := classifyScramble(buildTSStream(50, 10), false); got.Verdict != ports.ScrambleVerdictScrambled {
		t.Fatalf("a small mostly-flagged window must read scrambled; got %s", got.Verdict)
	}
}

// TestClassifyScramble_ClearEPGBurstCannotFlipVerdict reproduces the incident that
// motivated this code. EIT/PSI packets are never scrambled, so a fully encrypted
// service still emits clear bursts. Measured on the real channel: 86% of packets
// scrambled overall, yet 1.31% of all 48-packet windows read as clear — enough for
// roughly one preflight in a handful to pass an undecodable stream.
//
// A peephole window landing in such a burst is the failure mode; classifying the
// whole post-lock remainder is the fix. Shrink the classification window back to a
// short tail and this test fails.
func TestClassifyScramble_ClearEPGBurstCannotFlipVerdict(t *testing.T) {
	const (
		lockPackets = 2600 // past tsScrambleLockPrefixPackets
		bodyPackets = 1500
		burstAtEnd  = 60 // clear EIT burst landing exactly on the tail
	)
	buf := buildTSStream(lockPackets+bodyPackets-burstAtEnd, 0)
	buf = append(buf, buildTSStream(0, burstAtEnd)...)

	got := classifyScramble(buf, true)
	if got.Verdict != ports.ScrambleVerdictScrambled {
		t.Fatalf("a clear tail burst must not flip an encrypted stream to %s (fraction %.4f over %d packets, window %s)",
			got.Verdict, got.Fraction, got.Classified, got.Window)
	}
	if got.Classified <= burstAtEnd {
		t.Fatalf("classification window (%d packets) is small enough for a %d-packet burst to dominate it",
			got.Classified, burstAtEnd)
	}
}

// TestClassifyScramble_ProductionReadKeepsConfidentWindow pins the empirical floor
// recorded at tsScrambleMinConfidentWindow: on the read size the adapter actually
// takes from a lock-prone source, the verdict must rest on at least that many
// packets. Shrinking either the read or the window below it puts the classifier
// back in the range where a clear PSI/EPG burst flips the verdict — measured on
// real captures, 1024-packet windows still did.
func TestClassifyScramble_ProductionReadKeepsConfidentWindow(t *testing.T) {
	readPackets := preflightLockProneScanBytes / 188
	got := classifyScramble(buildTSStream(readPackets, 0), true)
	if got.Classified < tsScrambleMinConfidentWindow {
		t.Fatalf("a %d-packet production read classifies on only %d packets; the measured floor is %d",
			readPackets, got.Classified, tsScrambleMinConfidentWindow)
	}
}
