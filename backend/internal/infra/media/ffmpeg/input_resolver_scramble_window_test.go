package ffmpeg

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
)

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
	// Negative control: revert preflightLockProneScanBytes to 188*1024 and this fails.
	if preflightLockProneScanBytes < 188*(lockPackets+512) {
		t.Fatalf("preflightLockProneScanBytes=%d too small to clear a ~%d-packet descrambler lock", preflightLockProneScanBytes, lockPackets)
	}

	// Classified over the actual configured window -> cleared stream -> NOT scrambled.
	n := min(len(buf), preflightLockProneScanBytes)
	got := classifyScramble(buf[:n], true)
	if got.Verdict != ports.ScrambleVerdictClear {
		t.Fatalf("relay window must classify the post-lock stream as clear, got %s frac=%.3f pkts=%d", got.Verdict, got.Fraction, got.Classified)
	}

	// Sanity: a genuinely scrambled channel (scrambled throughout) still flags.
	allScr := tsBuf(4096, true)
	if got := classifyScramble(allScr, true); got.Verdict != ports.ScrambleVerdictScrambled {
		t.Fatalf("a fully-scrambled relay stream must still flag, got %s frac=%.3f", got.Verdict, got.Fraction)
	}
}
