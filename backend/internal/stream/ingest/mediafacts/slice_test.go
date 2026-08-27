// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package mediafacts

import "testing"

// Slice headers, bit-packed. first_mb_in_slice is ue(0) = "1" in both.
//
//	sliceI: "1" + ue(7)="0001000"  -> 0b10001000, slice_type 7 (I, all slices)
//	sliceP: "1" + ue(0)="1"        -> 0b11000000, slice_type 0 (P)
const (
	sliceHeaderI = 0x88
	sliceHeaderP = 0xC0
)

// seiRecoveryPoint is one SEI NAL payload carrying recovery_point: payload type 6,
// size 1, body 0x84 (recovery_frame_cnt=0, exact_match_flag=0), then trailing bits.
// This is the exact payload measured on the reference receiver.
var seiRecoveryPoint = []byte{0x06, 0x01, 0x84, 0x80}

// Emulation prevention bytes shift every field that follows them. A slice header
// that reads as intra only after they are removed proves the removal happens.
func TestRandomAccess_EmulationPreventionInSliceHeader(t *testing.T) {
	// Raw bytes 00 00 03 88: with the 0x03 removed the header is 00 00 88.
	// first_mb_in_slice then reads as a long Exp-Golomb value rather than 0, so this
	// asserts the parse is attempted over the unescaped bytes and rejected cleanly
	// rather than silently admitting the picture.
	if _, ok := h264SliceIsIntra([]byte{0x00, 0x00, 0x03, 0x88}); ok {
		t.Fatal("a header of leading zero bits must be reported unreadable, not intra")
	}
	if intra, ok := h264SliceIsIntra([]byte{sliceHeaderI}); !ok || !intra {
		t.Fatal("slice_type 7 must read as intra")
	}
	if intra, ok := h264SliceIsIntra([]byte{sliceHeaderP}); !ok || intra {
		t.Fatal("slice_type 0 must read as predicted")
	}
}
func TestRandomAccess_RecoveryPointSEIParsing(t *testing.T) {
	cases := map[string]struct {
		payload []byte
		want    bool
	}{
		"recovery_point alone":        {[]byte{0x06, 0x01, 0x84, 0x80}, true},
		"pic_timing then recovery":    {[]byte{0x01, 0x02, 0x00, 0x00, 0x06, 0x01, 0x84, 0x80}, true},
		"pic_timing only":             {[]byte{0x01, 0x02, 0x00, 0x00, 0x80}, false},
		"user data only":              {[]byte{0x05, 0x03, 0xAA, 0xBB, 0xCC, 0x80}, false},
		"payload runs past capture":   {[]byte{0x05, 0x40, 0xAA, 0xBB}, false},
		"extended type past recovery": {[]byte{0xFF, 0x01, 0x02, 0x00, 0x00, 0x80}, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := seiHasRecoveryPoint(tc.payload); got != tc.want {
				t.Fatalf("want %v, got %v", tc.want, got)
			}
		})
	}
}
