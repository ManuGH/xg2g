// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package hls

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspectSegmentRAP_TmpFile(t *testing.T) {
	sap, err := InspectSegmentRAP("seg_000000.ts.tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sap.Safe || sap.Type != AccessPointNone {
		t.Errorf("tmp file got sap = %+v, want Safe: false, Type: None", sap)
	}
}

func TestBitReader_ReadUE(t *testing.T) {
	cases := []struct {
		name     string
		bits     []byte // raw bitstream bytes
		expected uint64
	}{
		{"ue(0) -> 1", []byte{0x80}, 0},              // 1 (in binary 1) -> value 0
		{"ue(1) -> 010", []byte{0x40}, 1},            // 010 -> value 1
		{"ue(2) -> 011", []byte{0x60}, 2},            // 011 -> value 2
		{"ue(132) -> 132", []byte{0x01, 0x0A}, 132},  // 000000010000101 -> 7 zeros + 1 + 0000101 (5) -> 127 + 5 = 132
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			br := newBitReader(tc.bits)
			val, ok := br.readUE()
			if !ok || val != tc.expected {
				t.Errorf("readUE() = %d, %v; want %d", val, ok, tc.expected)
			}
		})
	}
}

func TestRemoveEmulationPreventionBytes(t *testing.T) {
	src := []byte{0x00, 0x00, 0x03, 0x01, 0x00, 0x00, 0x03, 0x02}
	want := []byte{0x00, 0x00, 0x01, 0x00, 0x00, 0x02}
	got := removeEmulationPreventionBytes(src)
	if !bytes.Equal(got, want) {
		t.Errorf("got %x, want %x", got, want)
	}
}

// buildMockTSPacket wraps payload into an 188-byte TS packet with PID 0x0100 and PUSI set if start=true.
func buildMockTSPacket(pid int, pusi bool, payload []byte) []byte {
	pkt := make([]byte, 188)
	pkt[0] = 0x47
	pkt[1] = byte(pid >> 8)
	if pusi {
		pkt[1] |= 0x40
	}
	pkt[2] = byte(pid & 0xFF)
	pkt[3] = 0x10 // payload only, continuity counter 0

	// If payload < 184 bytes, we use adaptation field stuffing to pad it out precisely to 188 bytes.
	if len(payload) < 184 {
		pkt[3] = 0x30 // adaptation field followed by payload
		padLen := 184 - len(payload) - 1
		pkt[4] = byte(padLen)
		if padLen > 0 {
			pkt[5] = 0x00 // flags
			for j := 1; j < padLen; j++ {
				pkt[5+j] = 0xFF
			}
		}
		copy(pkt[5+padLen:], payload)
	} else {
		copy(pkt[4:], payload[:184])
	}
	return pkt
}

func buildMockPATPMT(videoPID int) []byte {
	var buf bytes.Buffer
	// PAT PID 0x0000 pointing to PMT PID 0x0020
	patPayload := []byte{
		0x00,                               // pointer
		0x00, 0xB0, 0x0D, 0x00, 0x01, 0xC1, 0x00, // table header
		0x00, 0x01, 0xE0, 0x20, // program 1 -> PMT PID 0x0020
		0x2C, 0x80, 0xB8, 0x3A, // CRC
	}
	buf.Write(buildMockTSPacket(0x0000, true, patPayload))

	// PMT PID 0x0020 pointing to Video PID (stream type 0x1B = H.264)
	pmtPayload := []byte{
		0x00,                                     // pointer
		0x02, 0xB0, 0x12, 0x00, 0x01, 0xC1, 0x00, // table header
		0xE1, 0x00, // PCR PID
		0xF0, 0x00, // program info len 0
		0x1B, byte(videoPID >> 8), byte(videoPID & 0xFF), 0xF0, 0x00, // H.264 stream
		0x4E, 0x59, 0x3D, 0x1E, // CRC
	}
	buf.Write(buildMockTSPacket(0x0020, true, pmtPayload))
	return buf.Bytes()
}

func buildPESPacket(annexBPayload []byte) []byte {
	var buf bytes.Buffer
	// PES start code + stream_id 0xE0 (video) + length 0 (unbounded)
	buf.Write([]byte{0x00, 0x00, 0x01, 0xE0, 0x00, 0x00})
	// flags: PTS present (0x80), header length 5
	buf.Write([]byte{0x80, 0x80, 0x05})
	// 5 bytes dummy PTS
	buf.Write([]byte{0x21, 0x00, 0x01, 0x00, 0x01})
	buf.Write(annexBPayload)
	return buf.Bytes()
}

func TestInspectSegmentRAP_IDRSafe(t *testing.T) {
	// SPS (7), PPS (8), IDR (5)
	annexB := []byte{
		0x00, 0x00, 0x00, 0x01, 0x67, 0x42, 0xC0, 0x1E, // SPS
		0x00, 0x00, 0x00, 0x01, 0x68, 0xCE, 0x3C, 0x80, // PPS
		0x00, 0x00, 0x00, 0x01, 0x65, 0x88, 0x84, 0x00, // IDR slice
	}
	videoPID := 0x0100
	var tsData bytes.Buffer
	tsData.Write(buildMockPATPMT(videoPID))
	tsData.Write(buildMockTSPacket(videoPID, true, buildPESPacket(annexB)))

	tmpFile := filepath.Join(t.TempDir(), "seg_idr.ts")
	if err := os.WriteFile(tmpFile, tsData.Bytes(), 0600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	sap, err := InspectSegmentRAP(tmpFile)
	if err != nil {
		t.Fatalf("InspectSegmentRAP error: %v", err)
	}
	if !sap.Safe {
		t.Errorf("expected Safe=true for IDR+SPS+PPS, got %+v", sap)
	}
	if sap.Type != AccessPointIDR || !sap.HasSPS || !sap.HasPPS {
		t.Errorf("unexpected SAP fields: %+v", sap)
	}

	// Verify caching
	sapCached, _ := InspectSegmentRAP(tmpFile)
	if sapCached != sap {
		t.Errorf("expected cached pointer %p, got %p", sap, sapCached)
	}
}

func TestInspectSegmentRAP_RecoveryPointZeroSafe(t *testing.T) {
	// SPS (7), PPS (8), SEI recovery_point payload_type=6, size=2, ue(0)+exact_match(1)+broken_link(0)+slice_group(0) = 0x80 (10000000b) -> byte 1: 0x80, byte 2: 0x00
	annexB := []byte{
		0x00, 0x00, 0x00, 0x01, 0x67, 0x42, 0xC0, 0x1E, // SPS
		0x00, 0x00, 0x00, 0x01, 0x68, 0xCE, 0x3C, 0x80, // PPS
		0x00, 0x00, 0x00, 0x01, 0x06, 0x06, 0x02, 0xC0, 0x00, 0x80, // SEI recovery_point cnt=0 exact=true
		0x00, 0x00, 0x00, 0x01, 0x41, 0x9A, 0x24, 0x00, // NAL 1 non-IDR slice
	}
	videoPID := 0x0100
	var tsData bytes.Buffer
	tsData.Write(buildMockPATPMT(videoPID))
	tsData.Write(buildMockTSPacket(videoPID, true, buildPESPacket(annexB)))

	tmpFile := filepath.Join(t.TempDir(), "seg_recovery_zero.ts")
	if err := os.WriteFile(tmpFile, tsData.Bytes(), 0600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	sap, err := InspectSegmentRAP(tmpFile)
	if err != nil {
		t.Fatalf("InspectSegmentRAP error: %v", err)
	}
	if !sap.Safe {
		t.Errorf("expected Safe=true for recovery_frame_cnt=0 exact_match=true, got %+v", sap)
	}
	if sap.Type != AccessPointRecoveryPoint || sap.RecoveryFrameCount != 0 || !sap.ExactMatch {
		t.Errorf("unexpected SAP fields: %+v", sap)
	}
}

func TestInspectSegmentRAP_RecoveryPoint132Unsafe(t *testing.T) {
	// SPS (7), PPS (8), SEI recovery_point payload_type=6, cnt=132 -> ue(132) -> 000000010000101 (15 bits) -> byte 1: 0x08, byte 2: 0x4A, byte 3: 0x00
	annexB := []byte{
		0x00, 0x00, 0x00, 0x01, 0x67, 0x42, 0xC0, 0x1E, // SPS
		0x00, 0x00, 0x00, 0x01, 0x68, 0xCE, 0x3C, 0x80, // PPS
		0x00, 0x00, 0x00, 0x01, 0x06, 0x06, 0x03, 0x01, 0x0B, 0x00, // SEI recovery_point cnt=132 exact=true
		0x00, 0x00, 0x00, 0x01, 0x41, 0x9A, 0x24, 0x00, // NAL 1 non-IDR slice
	}
	videoPID := 0x0100
	var tsData bytes.Buffer
	tsData.Write(buildMockPATPMT(videoPID))
	tsData.Write(buildMockTSPacket(videoPID, true, buildPESPacket(annexB)))

	tmpFile := filepath.Join(t.TempDir(), "seg_recovery_132.ts")
	if err := os.WriteFile(tmpFile, tsData.Bytes(), 0600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	sap, err := InspectSegmentRAP(tmpFile)
	if err != nil {
		t.Fatalf("InspectSegmentRAP error: %v", err)
	}
	if sap.Safe {
		t.Errorf("expected Safe=false for recovery_frame_cnt=132, got %+v", sap)
	}
	if sap.Type != AccessPointRecoveryPoint || sap.RecoveryFrameCount != 132 {
		t.Errorf("unexpected SAP fields: %+v", sap)
	}
}

func TestFilterPlaylistRAP(t *testing.T) {
	dir := t.TempDir()

	// 1. Create seg_unsafe.ts (recovery_frame_cnt=132)
	annexBUnsafe := []byte{
		0x00, 0x00, 0x00, 0x01, 0x67, 0x64, 0x00, 0x1F, // SPS
		0x00, 0x00, 0x00, 0x01, 0x68, 0xCE, 0x3C, 0x80, // PPS
		0x00, 0x00, 0x00, 0x01, 0x06, 0x06, 0x03, 0x01, 0x0B, 0x00, // SEI recovery_point cnt=132 exact=true
		0x00, 0x00, 0x00, 0x01, 0x41, 0x9A, 0x24, 0x00,
	}
	var tsUnsafe bytes.Buffer
	tsUnsafe.Write(buildMockPATPMT(0x0100))
	tsUnsafe.Write(buildMockTSPacket(0x0100, true, buildPESPacket(annexBUnsafe)))
	if err := os.WriteFile(filepath.Join(dir, "seg_000000.ts"), tsUnsafe.Bytes(), 0600); err != nil {
		t.Fatalf("write unsafe seg: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "seg_000001.ts"), tsUnsafe.Bytes(), 0600); err != nil {
		t.Fatalf("write unsafe seg 1: %v", err)
	}

	// 2. Create seg_safe.ts (IDR)
	annexBSafe := []byte{
		0x00, 0x00, 0x00, 0x01, 0x67, 0x64, 0x00, 0x1F, // SPS
		0x00, 0x00, 0x00, 0x01, 0x68, 0xCE, 0x3C, 0x80, // PPS
		0x00, 0x00, 0x00, 0x01, 0x65, 0x88, 0x84, 0x00, // IDR
	}
	var tsSafe bytes.Buffer
	tsSafe.Write(buildMockPATPMT(0x0100))
	tsSafe.Write(buildMockTSPacket(0x0100, true, buildPESPacket(annexBSafe)))
	if err := os.WriteFile(filepath.Join(dir, "seg_000002.ts"), tsSafe.Bytes(), 0600); err != nil {
		t.Fatalf("write safe seg: %v", err)
	}

	t.Run("DropsUnsafeLeadingSegments", func(t *testing.T) {
		rawPlaylist := []byte(`#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:2
#EXT-X-MEDIA-SEQUENCE:0
#EXTINF:2.000,
seg_000000.ts
#EXTINF:2.000,
seg_000001.ts
#EXTINF:2.000,
seg_000002.ts
`)
		filtered, dropped, err := FilterPlaylistRAP(rawPlaylist, dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if dropped != 2 {
			t.Errorf("expected dropped=2, got %d", dropped)
		}
		filteredStr := string(filtered)
		if !strings.Contains(filteredStr, "#EXT-X-MEDIA-SEQUENCE:2") {
			t.Errorf("expected MEDIA-SEQUENCE:2, got:\n%s", filteredStr)
		}
		if strings.Contains(filteredStr, "seg_000000.ts") || strings.Contains(filteredStr, "seg_000001.ts") {
			t.Errorf("expected seg_000000 and seg_000001 to be dropped, got:\n%s", filteredStr)
		}
		if !strings.Contains(filteredStr, "seg_000002.ts") {
			t.Errorf("expected seg_000002.ts to be retained, got:\n%s", filteredStr)
		}
	})

	t.Run("FirstSegmentSafe_NoDrop", func(t *testing.T) {
		rawPlaylist := []byte(`#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:2
#EXT-X-MEDIA-SEQUENCE:2
#EXTINF:2.000,
seg_000002.ts
`)
		filtered, dropped, err := FilterPlaylistRAP(rawPlaylist, dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if dropped != 0 {
			t.Errorf("expected dropped=0, got %d", dropped)
		}
		if !bytes.Equal(filtered, rawPlaylist) {
			t.Errorf("expected unmodified playlist, got:\n%s", string(filtered))
		}
	})

	t.Run("AllSegmentsUnsafe_ReturnsError", func(t *testing.T) {
		rawPlaylist := []byte(`#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:2
#EXT-X-MEDIA-SEQUENCE:0
#EXTINF:2.000,
seg_000000.ts
#EXTINF:2.000,
seg_000001.ts
`)
		_, dropped, err := FilterPlaylistRAP(rawPlaylist, dir)
		if !errors.Is(err, ErrNoSafeSegmentAvailable) {
			t.Fatalf("expected ErrNoSafeSegmentAvailable, got: %v", err)
		}
		if dropped != 2 {
			t.Errorf("expected dropped=2 when all unsafe, got %d", dropped)
		}
	})

	t.Run("MasterOrVOD_NoFilter", func(t *testing.T) {
		master := []byte(`#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=1000000
index.m3u8
`)
		filtered, dropped, err := FilterPlaylistRAP(master, dir)
		if err != nil || dropped != 0 || !bytes.Equal(filtered, master) {
			t.Errorf("master playlist should not be filtered")
		}

		vod := []byte(`#EXTM3U
#EXT-X-PLAYLIST-TYPE:VOD
#EXTINF:2.000,
seg_000000.ts
#EXT-X-ENDLIST
`)
		filtered, dropped, err = FilterPlaylistRAP(vod, dir)
		if err != nil || dropped != 0 || !bytes.Equal(filtered, vod) {
			t.Errorf("vod playlist should not be filtered")
		}
	})

	t.Run("PreservesStatefulTagsAndDiscontinuities", func(t *testing.T) {
		rawPlaylist := []byte(`#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:2
#EXT-X-MEDIA-SEQUENCE:10
#EXT-X-DISCONTINUITY-SEQUENCE:2
#EXT-X-KEY:METHOD=AES-128,URI="key.bin"
#EXTINF:2.000,
seg_000000.ts
#EXT-X-DISCONTINUITY
#EXT-X-KEY:METHOD=AES-128,URI="key2.bin"
#EXTINF:2.000,
seg_000001.ts
#EXTINF:2.000,
seg_000002.ts
`)
		filtered, dropped, err := FilterPlaylistRAP(rawPlaylist, dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if dropped != 2 {
			t.Errorf("expected dropped=2, got %d", dropped)
		}
		filteredStr := string(filtered)
		if !strings.Contains(filteredStr, "#EXT-X-MEDIA-SEQUENCE:12") {
			t.Errorf("expected MEDIA-SEQUENCE:12, got:\n%s", filteredStr)
		}
		if !strings.Contains(filteredStr, "#EXT-X-DISCONTINUITY-SEQUENCE:3") {
			t.Errorf("expected DISCONTINUITY-SEQUENCE:3 (2+1 dropped), got:\n%s", filteredStr)
		}
		if !strings.Contains(filteredStr, `#EXT-X-KEY:METHOD=AES-128,URI="key.bin"`) || !strings.Contains(filteredStr, `#EXT-X-KEY:METHOD=AES-128,URI="key2.bin"`) {
			t.Errorf("expected stateful KEY tags to be preserved before retained segment, got:\n%s", filteredStr)
		}
	})

	t.Run("PathTraversalRejected", func(t *testing.T) {
		rawPlaylist := []byte(`#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:2
#EXT-X-MEDIA-SEQUENCE:0
#EXTINF:2.000,
../../etc/passwd.ts
`)
		_, _, err := FilterPlaylistRAP(rawPlaylist, dir)
		if err == nil || !strings.Contains(err.Error(), "invalid segment URI path traversal") {
			t.Errorf("expected path traversal rejection, got: %v", err)
		}
	})
}

func TestVerifyBatchSegmentRAPs_100Segments(t *testing.T) {
	dir := t.TempDir()

	// Build 10 safe segments and 90 unsafe/safe mix or all safe after index 5
	// Suppose seg 0..4 are unsafe (cnt=132) and seg 5..99 are safe (IDR).
	annexBUnsafe := []byte{
		0x00, 0x00, 0x00, 0x01, 0x67, 0x64, 0x00, 0x1F, // SPS
		0x00, 0x00, 0x00, 0x01, 0x68, 0xCE, 0x3C, 0x80, // PPS
		0x00, 0x00, 0x00, 0x01, 0x06, 0x06, 0x03, 0x01, 0x0B, 0x00, // SEI recovery_point cnt=132 exact=true
		0x00, 0x00, 0x00, 0x01, 0x41, 0x9A, 0x24, 0x00,
	}
	annexBSafe := []byte{
		0x00, 0x00, 0x00, 0x01, 0x67, 0x64, 0x00, 0x1F, // SPS
		0x00, 0x00, 0x00, 0x01, 0x68, 0xCE, 0x3C, 0x80, // PPS
		0x00, 0x00, 0x00, 0x01, 0x65, 0x88, 0x84, 0x00, // IDR
	}

	var tsUnsafe, tsSafe bytes.Buffer
	tsUnsafe.Write(buildMockPATPMT(0x0100))
	tsUnsafe.Write(buildMockTSPacket(0x0100, true, buildPESPacket(annexBUnsafe)))
	tsSafe.Write(buildMockPATPMT(0x0100))
	tsSafe.Write(buildMockTSPacket(0x0100, true, buildPESPacket(annexBSafe)))

	var segFiles []string
	for i := 0; i < 100; i++ {
		name := fmt.Sprintf("seg_%06d.ts", i)
		segFiles = append(segFiles, name)
		path := filepath.Join(dir, name)
		if i < 5 {
			_ = os.WriteFile(path, tsUnsafe.Bytes(), 0600)
		} else {
			_ = os.WriteFile(path, tsSafe.Bytes(), 0600)
		}
	}

	report, err := VerifyBatchSegmentRAPs(dir, segFiles)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.TotalSegments != 100 {
		t.Errorf("expected TotalSegments=100, got %d", report.TotalSegments)
	}
	if report.FirstSafeIndex != 5 {
		t.Errorf("expected FirstSafeIndex=5, got %d", report.FirstSafeIndex)
	}
	if report.SafeSegments != 95 {
		t.Errorf("expected SafeSegments=95, got %d", report.SafeSegments)
	}
	if !report.AllSafeAfterFirst || report.UnsafeAfterFirst != 0 {
		t.Errorf("expected AllSafeAfterFirst=true and UnsafeAfterFirst=0, got %+v", report)
	}
}

