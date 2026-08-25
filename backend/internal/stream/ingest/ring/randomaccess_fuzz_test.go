// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ring

import "testing"

// These parsers read bit-packed fields out of a broadcast the receiver hands over
// unverified. A truncated PES, a scrambled payload misread as clear, or a NAL cut
// short by a lost packet all arrive here as arbitrary bytes, so the property that
// matters is that no input crashes the ingest and none of them opens the attach
// gate by accident.

func FuzzH264SliceIsIntra(f *testing.F) {
	f.Add([]byte{sliceHeaderI})
	f.Add([]byte{sliceHeaderP})
	f.Add([]byte{})
	f.Add([]byte{0x00})
	f.Add([]byte{0x00, 0x00, 0x03, 0x88})       // emulation prevention
	f.Add([]byte{0x00, 0x00, 0x00, 0x00, 0x00}) // long zero run, no terminator
	f.Add([]byte{0x01, 0x88, 0xFF, 0xFF, 0xFF, 0xFF})

	f.Fuzz(func(t *testing.T, data []byte) {
		isIntra, ok := h264SliceIsIntra(data)
		// An unreadable header must never be reported as intra: that is what keeps a
		// truncated capture from admitting a predicted picture as an entry point.
		if !ok && isIntra {
			t.Fatalf("unreadable slice header reported as intra: %x", data)
		}
	})
}

func FuzzSEIHasRecoveryPoint(f *testing.F) {
	f.Add(seiRecoveryPoint)
	f.Add([]byte{})
	f.Add([]byte{0x80})
	f.Add([]byte{0xFF, 0xFF, 0xFF, 0xFF})             // payload type run with no terminator
	f.Add([]byte{0x01, 0xFF, 0xFF, 0xFF})             // payload size run with no terminator
	f.Add([]byte{0x01, 0x7F, 0x00})                   // payload size larger than the bytes present
	f.Add([]byte{0x00, 0x00, 0x06, 0x01, 0x84, 0x80}) // recovery point behind another message

	f.Fuzz(func(t *testing.T, data []byte) {
		// The contract is termination without panic; the walk advances at least two
		// bytes per message, so a malformed payload cannot loop forever.
		_ = seiHasRecoveryPoint(data)
	})
}

func FuzzHasAudioDescriptor(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{descriptorAC3, 0x00})
	f.Add([]byte{descriptorAudioReg, 0x04, 'A', 'C', '-', '3'})
	f.Add([]byte{descriptorAudioReg, 0x02, 'A', 'C'}) // registration shorter than a format id
	f.Add([]byte{0x0A, 0xFF, 0x01})                   // length running past the buffer

	f.Fuzz(func(t *testing.T, data []byte) {
		_ = hasAudioDescriptor(data)
	})
}

// The ring is what actually meets the receiver's bytes, so it is fuzzed as a whole:
// packets with a broken sync byte, an adaptation field longer than the packet, or a
// PES header claiming more data than it carries must not take the process down.
func FuzzMasterRingPush(f *testing.F) {
	f.Add(make([]byte, TSPacketSize))
	f.Add([]byte{0x47, 0x01, 0x00, 0x10})
	f.Add(append([]byte{0x47, 0x41, 0x00, 0x30, 0xFF}, make([]byte, TSPacketSize-5)...))

	f.Fuzz(func(t *testing.T, data []byte) {
		r := NewMasterRing(64 * TSPacketSize)
		defer r.Close()
		_, _ = r.Push(data)
		_, _ = r.LatestKeyframeOffset()
		_ = r.RandomAccess()
		_, _ = r.VideoDetails()
	})
}
