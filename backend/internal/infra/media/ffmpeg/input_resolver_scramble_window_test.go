package ffmpeg

import "testing"

// tsBuf builds n aligned 188-byte MPEG-TS packets; scrambled sets both
// transport_scrambling_control bits (byte[3] & 0xC0).
func tsBuf(n int, scrambled bool) []byte {
	b := make([]byte, n*188)
	for i := 0; i < n; i++ {
		b[i*188] = 0x47
		if scrambled {
			b[i*188+3] = 0xC0
		}
	}
	return b
}

// A relay (port 17999) icam channel is scrambled until the descrambler/ECM locks,
// then clears. The relay scan window must reach PAST that lock so the trailing
// sample sits in the cleared stream, else a healthy channel is false-flagged
// R_UPSTREAM_SCRAMBLED (real bug: VLC played it fine while xg2g refused).
// Measured lock ~2000 packets (~367KB) on a real channel.
func TestRelayScrambleClassification_ToleratesDescramblerLockLatency(t *testing.T) {
	const lockPackets = 2000
	buf := append(tsBuf(lockPackets, true), tsBuf(3000, false)...) // ~940KB: lock then clear

	// The configured relay window must clear a ~2000-packet lock with margin.
	// Negative control: revert preflightRelayScanBytes to 188*1024 and this fails.
	if preflightRelayScanBytes < 188*(lockPackets+512) {
		t.Fatalf("preflightRelayScanBytes=%d too small to clear a ~%d-packet descrambler lock", preflightRelayScanBytes, lockPackets)
	}

	// Classified over the actual configured window -> cleared stream -> NOT scrambled.
	n := min(len(buf), preflightRelayScanBytes)
	frac, pkts := scrambleFractionForSource(buf[:n], true)
	if pkts < tsScrambleMinPackets || frac >= tsScrambleThreshold {
		t.Fatalf("relay window must classify the post-lock stream as clear, got frac=%.3f pkts=%d", frac, pkts)
	}

	// Sanity: a genuinely scrambled channel (scrambled throughout) still flags.
	allScr := tsBuf(4096, true)
	if frac2, _ := scrambleFractionForSource(allScr, true); frac2 < tsScrambleThreshold {
		t.Fatalf("a fully-scrambled relay stream must still flag, got frac=%.3f", frac2)
	}
}
