package ffmpeg

import "testing"

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

// TestScrambleFractionForSource_RelayUsesTrailingWindow is the core of the fix: a
// stream-relay source whose first packets carry the transport_scrambling_control
// bits but which clears soon after is classified on its TRAILING window and read as
// clear — whereas the direct (whole-sample) classification of the SAME bytes flags
// it. The asymmetry is the regression guard: classifying a relay source on the whole
// sample (the prior behaviour) turns the "must read clear" assertion red.
func TestScrambleFractionForSource_RelayUsesTrailingWindow(t *testing.T) {
	// 70 flagged packets then 50 clear: the whole-sample fraction (70/120 = 0.58) is
	// over the threshold, but the trailing 48 packets are entirely clear.
	buf := buildTSStream(70, 50)

	relayFrac, relayPkts := scrambleFractionForSource(buf, true)
	if relayPkts < tsScrambleMinPackets || relayFrac >= tsScrambleThreshold {
		t.Fatalf("relay must read CLEAR on the trailing window; got fraction %.2f over %d packets", relayFrac, relayPkts)
	}

	// Precondition: the whole-sample view of the SAME bytes IS over threshold — i.e.
	// the trailing-window logic is exactly what flips the verdict (the negative control).
	directFrac, _ := scrambleFractionForSource(buf, false)
	if directFrac < tsScrambleThreshold {
		t.Fatalf("precondition: whole-sample fraction should exceed the threshold here; got %.2f", directFrac)
	}
}

// TestScrambleFractionForSource_RelayFlaggedThroughoutStillScrambled guards against a
// blanket bypass: a relay source whose packets carry the bits all the way through
// (the tail is flagged too) must still be classified as scrambled.
func TestScrambleFractionForSource_RelayFlaggedThroughoutStillScrambled(t *testing.T) {
	buf := buildTSStream(1024, 0)
	frac, pkts := scrambleFractionForSource(buf, true)
	if !(pkts >= tsScrambleMinPackets && frac >= tsScrambleThreshold) {
		t.Fatalf("a source flagged throughout must stay classified as scrambled; got fraction %.2f over %d packets", frac, pkts)
	}
}

// TestScrambleFractionForSource_DirectUnchanged confirms the direct (non-relay) fast
// path is unchanged: a clear sample reads clear, a flagged sample reads flagged.
func TestScrambleFractionForSource_DirectUnchanged(t *testing.T) {
	clearFrac, _ := scrambleFractionForSource(buildTSStream(0, 48), false)
	if clearFrac >= tsScrambleThreshold {
		t.Fatalf("clear direct sample must read clear; got %.2f", clearFrac)
	}
	flaggedFrac, flaggedPkts := scrambleFractionForSource(buildTSStream(48, 0), false)
	if !(flaggedPkts >= tsScrambleMinPackets && flaggedFrac >= tsScrambleThreshold) {
		t.Fatalf("flagged direct sample must read flagged; got %.2f over %d packets", flaggedFrac, flaggedPkts)
	}
}
