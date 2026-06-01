// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package ffmpeg

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// makeTSStream builds n packet-aligned 188-byte MPEG-TS packets. The first
// `scrambledCount` packets carry the transport_scrambling_control bits (encrypted
// payload); the rest are clear. Every packet starts with the 0x47 sync byte.
func makeTSStream(n, scrambledCount int) []byte {
	const pktLen = 188
	buf := make([]byte, n*pktLen)
	for i := 0; i < n; i++ {
		off := i * pktLen
		buf[off] = 0x47
		buf[off+3] = 0x10 // adaptation_field_control=payload, transport_scrambling_control=00 (clear)
		if i < scrambledCount {
			buf[off+3] = 0x90 // transport_scrambling_control=10 (scrambled, even key)
		}
	}
	return buf
}

// likelyScrambled mirrors the conservative call-site predicate so the test
// pins the same thresholds the preflight uses.
func likelyScrambled(buf []byte) bool {
	fraction, packets := tsScrambledFraction(buf)
	return packets >= tsScrambleMinPackets && fraction >= tsScrambleThreshold
}

func TestTSScrambledFraction_Counts(t *testing.T) {
	cases := []struct {
		name         string
		packets      int
		scrambled    int
		wantFraction float64
		wantPackets  int
	}{
		{"all clear", 48, 0, 0.0, 48},
		{"all scrambled", 48, 48, 1.0, 48},
		{"a few PSI clear, rest scrambled", 48, 44, 44.0 / 48.0, 48},
		{"empty", 0, 0, 0.0, 0},
		{"sub-packet buffer", 0, 0, 0.0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			frac, pkts := tsScrambledFraction(makeTSStream(tc.packets, tc.scrambled))
			assert.Equal(t, tc.wantPackets, pkts)
			assert.InDelta(t, tc.wantFraction, frac, 0.001)
		})
	}
}

func TestTSScrambledFraction_TooSmallBuffer(t *testing.T) {
	// A buffer shorter than one packet yields no aligned packets.
	frac, pkts := tsScrambledFraction(make([]byte, 100))
	assert.Equal(t, 0, pkts)
	assert.Equal(t, 0.0, frac)
}

func TestTSScrambledFraction_StopsAtAlignmentLoss(t *testing.T) {
	// 48 scrambled packets, but packet index 30 loses its 0x47 sync byte.
	buf := makeTSStream(48, 48)
	buf[30*188] = 0x00 // corrupt the sync byte mid-buffer
	_, pkts := tsScrambledFraction(buf)
	assert.Equal(t, 30, pkts, "scan must stop at the first unaligned packet, not trust the rest")
}

func TestLikelyScrambled_Conservative(t *testing.T) {
	cases := []struct {
		name      string
		packets   int
		scrambled int
		want      bool
	}{
		{"clear channel is never blocked", 48, 0, false},
		{"fully scrambled channel is caught", 48, 48, true},
		{"mostly scrambled (PSI + AV) is caught", 48, 44, true},
		{"tiny sample is never classified, even if all scrambled", 10, 10, false},
		{"just below the majority threshold is not blocked", 48, 23, false},
		{"at the majority threshold is caught", 48, 24, true},
		{"exactly the minimum sample, fully scrambled", 24, 24, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, likelyScrambled(makeTSStream(tc.packets, tc.scrambled)))
		})
	}
}
