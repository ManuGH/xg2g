// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ring

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

// H.264 NAL header bytes used by these fixtures. The low five bits are the type.
const (
	nalHdrNonIDR = 0x41 // nal_ref_idc=2, type=1
	nalHdrIDR    = 0x65 // nal_ref_idc=3, type=5
	nalHdrSEI    = 0x06
	nalHdrSPS    = 0x67
	nalHdrPPS    = 0x68
)

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

func startCode() []byte { return []byte{0x00, 0x00, 0x00, 0x01} }

// nal builds one Annex-B NAL unit: start code, header byte, then body.
func nal(header byte, body ...byte) []byte {
	out := append(startCode(), header)
	return append(out, body...)
}

// h264Ring returns a ring with PAT and PMT already pushed, announcing H.264.
func h264Ring(t *testing.T, videoPID uint16) (*MasterRing, uint16) {
	t.Helper()
	const pmtPID = 100
	r := NewMasterRing(400 * TSPacketSize)
	t.Cleanup(r.Close)

	for _, pkt := range createMultiPacketPAT(pmtPID) {
		if _, err := r.Push(context.Background(), pkt); err != nil {
			t.Fatalf("push PAT: %v", err)
		}
	}
	for _, pkt := range createMultiPacketPMT(pmtPID, videoPID, false) {
		if _, err := r.Push(context.Background(), pkt); err != nil {
			t.Fatalf("push PMT: %v", err)
		}
	}
	if pid, codec := r.VideoDetails(); pid != videoPID || codec != CodecH264 {
		t.Fatalf("expected H.264 on PID %d, got %d / %v", videoPID, pid, codec)
	}
	return r, videoPID
}

// pushAU pushes one access unit as a single PUSI packet and returns its offset.
func pushAU(t *testing.T, r *MasterRing, videoPID uint16, cc uint8, es []byte) int64 {
	t.Helper()
	offset := r.Head()
	if _, err := r.Push(context.Background(), createVideoPESPacket(videoPID, true, cc, es)); err != nil {
		t.Fatalf("push access unit: %v", err)
	}
	return offset
}

// The counter-case this classification exists for: an encoder that repeats its
// parameter sets ahead of a predicted picture. The previous rule offered exactly
// this as an attach point, and a decoder started there is missing the references
// the P slice names.
func TestRandomAccess_ParameterSetsBeforePredictedSlice_IsNotAnEntryPoint(t *testing.T) {
	r, vpid := h264Ring(t, 256)

	es := append(nal(nalHdrSPS, 0x42, 0x00, 0x1E), nal(nalHdrPPS, 0xCE, 0x3C)...)
	es = append(es, nal(nalHdrNonIDR, sliceHeaderP)...)
	pushAU(t, r, vpid, 0, es)

	// A following access unit closes the first one, which is when it is classified.
	pushAU(t, r, vpid, 1, nal(nalHdrNonIDR, sliceHeaderP))

	if offset, ok := r.LatestKeyframeOffset(); ok {
		t.Fatalf("SPS+PPS ahead of a P slice must not be an entry point, got offset %d", offset)
	}

	obs := r.RandomAccess()
	if obs.PredictedRejected == 0 {
		t.Fatal("expected the rejection to be counted")
	}
	if obs.IRAPPoints != 0 || obs.IntraPoints != 0 {
		t.Fatalf("no entry point may be recorded: %+v", obs)
	}
}

// The case every measured channel actually presents: no IDR anywhere, random access
// signalled by an all-intra picture carrying parameter sets and a recovery_point SEI.
func TestRandomAccess_IntraAccessUnitWithParameterSets_IsAnEntryPoint(t *testing.T) {
	r, vpid := h264Ring(t, 256)

	es := append(nal(nalHdrSPS, 0x42, 0x00, 0x1E), nal(nalHdrPPS, 0xCE, 0x3C)...)
	es = append(es, nal(nalHdrSEI, seiRecoveryPoint...)...)
	es = append(es, nal(nalHdrNonIDR, sliceHeaderI)...)
	want := pushAU(t, r, vpid, 0, es)

	pushAU(t, r, vpid, 1, nal(nalHdrNonIDR, sliceHeaderP))

	offset, ok := r.LatestKeyframeOffset()
	if !ok {
		t.Fatal("an all-intra access unit with parameter sets must be an entry point")
	}
	if offset != want {
		t.Fatalf("entry point must name the access unit start: want %d, got %d", want, offset)
	}

	obs := r.RandomAccess()
	if obs.IntraPoints != 1 {
		t.Fatalf("expected one intra entry point, got %+v", obs)
	}
	if obs.RecoveryPointSEIs != 1 {
		t.Fatalf("recovery_point SEI must be recorded as corroboration, got %+v", obs)
	}
	if obs.IRAPPoints != 0 {
		t.Fatalf("no IDR was sent, so none may be counted: %+v", obs)
	}
}

// One predicted slice is enough to disqualify a picture: the decoder would be
// missing exactly the references that slice names.
func TestRandomAccess_MixedSliceTypes_IsNotAnEntryPoint(t *testing.T) {
	r, vpid := h264Ring(t, 256)

	es := append(nal(nalHdrSPS, 0x42, 0x00, 0x1E), nal(nalHdrPPS, 0xCE, 0x3C)...)
	es = append(es, nal(nalHdrNonIDR, sliceHeaderI)...)
	es = append(es, nal(nalHdrNonIDR, sliceHeaderP)...)
	pushAU(t, r, vpid, 0, es)
	pushAU(t, r, vpid, 1, nal(nalHdrNonIDR, sliceHeaderP))

	if _, ok := r.LatestKeyframeOffset(); ok {
		t.Fatal("an access unit with a predicted slice must not be an entry point")
	}
}

// An IDR is joinable on its own and is indexed without waiting for the access unit
// to end, so streams that do emit them attach exactly as promptly as before.
func TestRandomAccess_IDR_IsIndexedWithoutWaitingForTheNextAccessUnit(t *testing.T) {
	r, vpid := h264Ring(t, 256)

	es := append(nal(nalHdrSPS, 0x42, 0x00, 0x1E), nal(nalHdrPPS, 0xCE, 0x3C)...)
	es = append(es, nal(nalHdrIDR, 0x88)...)
	want := pushAU(t, r, vpid, 0, es)

	offset, ok := r.LatestKeyframeOffset()
	if !ok || offset != want {
		t.Fatalf("IDR must be indexed immediately at %d, got %d (ok=%v)", want, offset, ok)
	}
	if obs := r.RandomAccess(); obs.IRAPPoints != 1 {
		t.Fatalf("expected one IRAP entry point, got %+v", obs)
	}
}

// Parameter sets are what a cold decoder configures itself from; an intra picture
// without them cannot start one.
func TestRandomAccess_IntraWithoutParameterSets_IsNotAnEntryPoint(t *testing.T) {
	r, vpid := h264Ring(t, 256)

	pushAU(t, r, vpid, 0, nal(nalHdrNonIDR, sliceHeaderI))
	pushAU(t, r, vpid, 1, nal(nalHdrNonIDR, sliceHeaderP))

	if _, ok := r.LatestKeyframeOffset(); ok {
		t.Fatal("an intra picture without SPS/PPS must not be an entry point")
	}
}

// A NAL unit routinely spans TS packets, and the slice header sits in its first
// bytes. Splitting mid-header must not change the classification.
func TestRandomAccess_SliceHeaderSplitAcrossTSPackets(t *testing.T) {
	r, vpid := h264Ring(t, 256)

	// A PUSI packet carries 170 payload bytes. The NAL header is placed on the very
	// last of them, so the slice header genuinely arrives in the following packet -
	// the builder zero-fills any remaining space, and that filler would otherwise be
	// captured instead of the header.
	head := append(nal(nalHdrSPS, 0x42, 0x00, 0x1E), nal(nalHdrPPS, 0xCE, 0x3C)...)
	const pusiPayloadBytes = TSPacketSize - 18
	fillerBody := pusiPayloadBytes - len(head) - 5 /* filler start code + header */ - 5 /* slice start code + header */
	head = append(head, nal(0x0C, bytes.Repeat([]byte{0xFF}, fillerBody)...)...)
	head = append(head, startCode()...)
	head = append(head, nalHdrNonIDR) // header byte here, slice header in the next packet
	if len(head) != pusiPayloadBytes {
		t.Fatalf("fixture must fill the packet exactly: %d of %d", len(head), pusiPayloadBytes)
	}

	want := r.Head()
	if _, err := r.Push(context.Background(), createVideoPESPacket(vpid, true, 0, head)); err != nil {
		t.Fatalf("push head: %v", err)
	}
	if _, err := r.Push(context.Background(), createVideoPESPacket(vpid, false, 1, []byte{sliceHeaderI, 0x00, 0x00})); err != nil {
		t.Fatalf("push tail: %v", err)
	}
	pushAU(t, r, vpid, 2, nal(nalHdrNonIDR, sliceHeaderP))

	offset, ok := r.LatestKeyframeOffset()
	if !ok || offset != want {
		t.Fatalf("split slice header must still classify: want %d, got %d (ok=%v)", want, offset, ok)
	}
}

// HEVC keeps IRAP detection and gains the rest of the IRAP range; the old
// "trailing picture plus parameter sets" heuristic is gone.
func TestRandomAccess_HEVC_IRAPRangeAndTrailingRejection(t *testing.T) {
	const pmtPID = 100
	const videoPID = 256

	hevcRing := func(t *testing.T) *MasterRing {
		t.Helper()
		r := NewMasterRing(400 * TSPacketSize)
		t.Cleanup(r.Close)
		for _, pkt := range createMultiPacketPAT(pmtPID) {
			_, _ = r.Push(context.Background(), pkt)
		}
		for _, pkt := range createMultiPacketPMT(pmtPID, videoPID, true) {
			_, _ = r.Push(context.Background(), pkt)
		}
		return r
	}

	// A HEVC NAL header is two bytes, not one: forbidden_zero_bit, six bits of
	// nal_unit_type, then nuh_layer_id and nuh_temporal_id_plus1. Building these
	// fixtures with a single byte, as they were, hid a parser that started reading
	// SEI payloads one byte early - the fixture and the parser were wrong in the
	// same direction, so the test passed on a stream shape that does not exist.
	hevcNAL := func(nalType byte, body ...byte) []byte {
		out := append(startCode(), nalType<<1, 0x01)
		return append(out, body...)
	}

	for _, nalType := range []byte{16, 17, 18, 19, 20, 21} {
		t.Run("IRAP type", func(t *testing.T) {
			r := hevcRing(t)
			es := append(hevcNAL(32, 0x01), hevcNAL(33, 0x01)...)
			es = append(es, hevcNAL(34, 0x01)...)
			es = append(es, hevcNAL(nalType, 0x01)...)
			want := pushAU(t, r, videoPID, 0, es)
			offset, ok := r.LatestKeyframeOffset()
			if !ok || offset != want {
				t.Fatalf("HEVC NAL type %d must be an entry point", nalType)
			}
		})
	}

	t.Run("trailing picture with parameter sets", func(t *testing.T) {
		r := hevcRing(t)
		es := append(hevcNAL(32, 0x01), hevcNAL(33, 0x01)...)
		es = append(es, hevcNAL(34, 0x01)...)
		es = append(es, hevcNAL(1, 0x01)...) // TRAIL_R
		pushAU(t, r, videoPID, 0, es)
		pushAU(t, r, videoPID, 1, hevcNAL(1, 0x01))

		if _, ok := r.LatestKeyframeOffset(); ok {
			t.Fatal("a HEVC trailing picture must not be an entry point on parameter sets alone")
		}
	})

	// A stream that never emits an IRAP is still admitted where it declares a
	// recovery point itself, which is the only signal such a stream gives.
	t.Run("recovery point without IRAP", func(t *testing.T) {
		r := hevcRing(t)
		es := append(hevcNAL(32, 0x01), hevcNAL(33, 0x01)...)
		es = append(es, hevcNAL(34, 0x01)...)
		es = append(es, hevcNAL(39, seiRecoveryPoint...)...)
		es = append(es, hevcNAL(1, 0x01)...)
		want := pushAU(t, r, videoPID, 0, es)
		pushAU(t, r, videoPID, 1, hevcNAL(1, 0x01))

		offset, ok := r.LatestKeyframeOffset()
		if !ok || offset != want {
			t.Fatalf("HEVC recovery point must be an entry point: want %d, got %d (ok=%v)", want, offset, ok)
		}
	})
}

// Descrambling was measured coming up intermittently, so the run of consecutive
// clear packets is tracked rather than only the totals.
func TestScrambling_PerStreamCountersAndClearRun(t *testing.T) {
	r, vpid := h264Ring(t, 256)

	clear := createVideoPESPacket(vpid, true, 0, nal(nalHdrNonIDR, sliceHeaderP))
	scrambled := createVideoPESPacket(vpid, true, 1, nal(nalHdrNonIDR, sliceHeaderP))
	scrambled[3] |= 0xC0 // transport_scrambling_control = odd key

	for i := 0; i < 3; i++ {
		if _, err := r.Push(context.Background(), clear); err != nil {
			t.Fatalf("push clear: %v", err)
		}
	}
	if got := r.Scrambling(); got.VideoClearRun != 3 || got.VideoClear != 3 || got.VideoScrambled != 0 {
		t.Fatalf("after three clear packets: %+v", got)
	}

	if _, err := r.Push(context.Background(), scrambled); err != nil {
		t.Fatalf("push scrambled: %v", err)
	}
	got := r.Scrambling()
	if got.VideoClearRun != 0 {
		t.Fatalf("a scrambled packet must reset the clear run: %+v", got)
	}
	if got.VideoScrambled != 1 || got.VideoClear != 3 {
		t.Fatalf("totals must keep accumulating: %+v", got)
	}
}

// Golden regression against the real broadcast captures in the tree, H.264 and
// HEVC alike. The point is not the exact numbers but that they stop changing: any
// future edit to the classification has to justify itself against streams that were
// actually on the air. Measured here, the H.264 captures emit no IDR at all and are
// admitted entirely on all-intra access units carrying a recovery_point SEI, which
// is what every channel measured against the reference receiver looks like; the
// HEVC capture is admitted on IRAP pictures instead.
func TestRandomAccess_RealCapture_ClassificationIsStable(t *testing.T) {
	root := findProjectRoot(t)

	for _, tc := range []struct {
		file      string
		codec     VideoCodec
		wantIRAP  uint64
		wantIntra uint64
		wantSEI   uint64
	}{
		{"verify_final_v3.ts", CodecH264, 0, 4, 4},
		{"verify_seg.ts", CodecH264, 0, 2, 2},
		{"verify_aac.ts", CodecH264, 0, 3, 3},
		{"downloaded_seg.ts", CodecH264, 0, 3, 3},
		{"test_hevc_stream.ts", CodecH265, 13, 0, 0},
	} {
		t.Run(tc.file, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(root, "backend", "testdata", "segments", tc.file))
			if err != nil {
				t.Skipf("capture unavailable: %v", err)
			}

			r := NewMasterRing(len(data) + TSPacketSize)
			defer r.Close()
			if _, err := r.Push(context.Background(), data); err != nil {
				t.Fatalf("push capture: %v", err)
			}

			if _, codec := r.VideoDetails(); codec != tc.codec {
				t.Fatalf("codec: want %v, got %v", tc.codec, codec)
			}

			obs := r.RandomAccess()
			if obs.IRAPPoints != tc.wantIRAP || obs.IntraPoints != tc.wantIntra {
				t.Errorf("entry points: want %d IRAP / %d intra, got %d / %d",
					tc.wantIRAP, tc.wantIntra, obs.IRAPPoints, obs.IntraPoints)
			}
			if obs.RecoveryPointSEIs != tc.wantSEI {
				t.Errorf("recovery_point SEIs: want %d, got %d", tc.wantSEI, obs.RecoveryPointSEIs)
			}
			if obs.UnreadableSlices > 0 {
				t.Errorf("every slice header in a real capture must parse: %+v", obs)
			}

			// Every offer must name a real access unit boundary, never a byte inside one.
			offsets := r.KeyframeOffsets()
			if len(offsets) == 0 {
				t.Fatal("entry points were counted but none are indexed")
			}
			for _, off := range offsets {
				if off%TSPacketSize != 0 {
					t.Fatalf("entry point %d is not on a packet boundary", off)
				}
			}
		})
	}
}
