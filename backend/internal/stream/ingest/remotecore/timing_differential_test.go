// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package remotecore

import (
	"context"
	"encoding/binary"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/stream/ingest/mediafacts"
	"github.com/ManuGH/xg2g/internal/stream/ingest/ring"
)

// buildPATPacket constructs a 188-byte TS packet carrying a PAT for program 1 -> PMT PID 0x0100.
func buildPATPacket(cc uint8) []byte {
	section := []byte{
		0x00, 0xB0, 0x0D, 0x00, 0x01, 0xC1, 0x00, 0x00, // header
		0x00, 0x01, 0xE1, 0x00, // program 1 -> PID 0x0100
		0x00, 0x00, 0x00, 0x00, // CRC placeholder
	}
	crc := ring.CalculateMPEG2CRC32(section[:len(section)-4])
	binary.BigEndian.PutUint32(section[len(section)-4:], crc)

	pkt := make([]byte, mediafacts.TSPacketSize)
	pkt[0] = 0x47
	pkt[1] = 0x40 // PUSI = 1, PID = 0
	pkt[2] = 0x00
	pkt[3] = 0x10 | (cc & 0x0F) // payload only
	pkt[4] = 0x00               // pointer field
	copy(pkt[5:], section)
	for i := 5 + len(section); i < len(pkt); i++ {
		pkt[i] = 0xFF
	}
	return pkt
}

// buildPMTPacket constructs a 188-byte TS packet carrying a PMT on PID 0x0100 for program 1.
// PCR PID = 0x0100, Video PID = 0x0101 (stream_type 0x1B H.264).
func buildPMTPacket(cc uint8) []byte {
	section := []byte{
		0x02, 0xB0, 0x12, 0x00, 0x01, 0xC1, 0x00, 0x00, // header
		0xE1, 0x00, // PCR PID 0x0100
		0xF0, 0x00, // program info length 0
		0x1B, 0xE1, 0x01, 0xF0, 0x00, // stream_type 0x1B, elementary PID 0x0101, ES info length 0
		0x00, 0x00, 0x00, 0x00, // CRC placeholder
	}
	crc := ring.CalculateMPEG2CRC32(section[:len(section)-4])
	binary.BigEndian.PutUint32(section[len(section)-4:], crc)

	pkt := make([]byte, mediafacts.TSPacketSize)
	pkt[0] = 0x47
	pkt[1] = 0x41 // PUSI = 1, PID = 0x0100
	pkt[2] = 0x00
	pkt[3] = 0x10 | (cc & 0x0F) // payload only
	pkt[4] = 0x00               // pointer field
	copy(pkt[5:], section)
	for i := 5 + len(section); i < len(pkt); i++ {
		pkt[i] = 0xFF
	}
	return pkt
}

// buildPCRPacket constructs a 188-byte TS packet on PID 0x0100 with an adaptation field.
// If di is true, discontinuity_indicator is set.
// If hasPCR is true, PCR is encoded.
func buildPCRPacket(cc uint8, di bool, hasPCR bool, pcr27m int64) []byte {
	pkt := make([]byte, mediafacts.TSPacketSize)
	pkt[0] = 0x47
	pkt[1] = 0x01 // PID = 0x0100
	pkt[2] = 0x00
	pkt[3] = 0x20 | (cc & 0x0F) // adaptation field only (AFC = 2)

	afLen := byte(183)
	pkt[4] = afLen
	var flags byte
	if di {
		flags |= 0x80
	}
	if hasPCR {
		flags |= 0x10
	}
	pkt[5] = flags

	idx := 6
	if hasPCR {
		base := uint64(pcr27m / 300)
		ext := uint16(pcr27m % 300)
		pkt[idx] = byte(base >> 25)
		pkt[idx+1] = byte(base >> 17)
		pkt[idx+2] = byte(base >> 9)
		pkt[idx+3] = byte(base >> 1)
		pkt[idx+4] = byte((base&1)<<7 | 0x7E | uint64(ext>>8))
		pkt[idx+5] = byte(ext & 0xFF)
		idx += 6
	}

	for i := idx; i < len(pkt); i++ {
		pkt[i] = 0xFF
	}
	return pkt
}

// buildVideoPESPacket constructs a 188-byte TS packet on PID 0x0101 carrying an IDR frame with PTS/DTS.
func buildVideoPESPacket(cc uint8, pts90k, dts90k int64) []byte {
	pkt := make([]byte, mediafacts.TSPacketSize)
	pkt[0] = 0x47
	pkt[1] = 0x41 // PUSI = 1, PID = 0x0101
	pkt[2] = 0x01
	pkt[3] = 0x10 | (cc & 0x0F) // payload only

	// PES Header with PTS and DTS
	pes := []byte{
		0x00, 0x00, 0x01, 0xE0, // prefix + video stream id
		0x00, 0x00, // length unbounded
		0x80, 0xC0, // flags: PTS + DTS
		0x0A, // header data length 10
	}

	// PTS (5 bytes, prefix 0011)
	pts := uint64(pts90k)
	p0 := byte(0x31 | ((pts >> 29) & 0x0E))
	p1 := byte((pts >> 22) & 0xFF)
	p2 := byte(0x01 | ((pts >> 14) & 0xFE))
	p3 := byte((pts >> 7) & 0xFF)
	p4 := byte(0x01 | ((pts << 1) & 0xFE))
	pes = append(pes, p0, p1, p2, p3, p4)

	// DTS (5 bytes, prefix 0001)
	dts := uint64(dts90k)
	d0 := byte(0x11 | ((dts >> 29) & 0x0E))
	d1 := byte((dts >> 22) & 0xFF)
	d2 := byte(0x01 | ((dts >> 14) & 0xFE))
	d3 := byte((dts >> 7) & 0xFF)
	d4 := byte(0x01 | ((dts << 1) & 0xFE))
	pes = append(pes, d0, d1, d2, d3, d4)

	// H.264 NAL units: SPS, PPS, IDR slice
	sps := []byte{0x00, 0x00, 0x00, 0x01, 0x67, 0x42, 0x00, 0x1E, 0x9A, 0x74, 0x05, 0x81, 0xEC, 0x80}
	pps := []byte{0x00, 0x00, 0x00, 0x01, 0x68, 0xCE, 0x3C, 0x80}
	idr := []byte{0x00, 0x00, 0x00, 0x01, 0x65, 0x88, 0x84, 0x00, 0x10, 0xFF}
	pes = append(pes, sps...)
	pes = append(pes, pps...)
	pes = append(pes, idr...)

	copy(pkt[4:], pes)
	for i := 4 + len(pes); i < len(pkt); i++ {
		pkt[i] = 0xFF
	}
	return pkt
}

// TestTimingDifferential_CanonicalTimingPublishedOverIPC verifies that the real Rust media-core
// publishes the canonical stream timeline over Protocol v6 IPC and Go remotecore decodes it with
// exact semantic fidelity.
func TestTimingDifferential_CanonicalTimingPublishedOverIPC(t *testing.T) {
	bin := requireVideoRealCore(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	remote, err := Start(ctx, bin, 1)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		if err := remote.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	local := mediafacts.NewGoCore(1)
	offset := int64(0)

	// Step 1: Ingest PAT + PMT -> establishes target program 1 and ActiveEpoch 1.
	var chunk1 []byte
	chunk1 = append(chunk1, buildPATPacket(0)...)
	chunk1 = append(chunk1, buildPMTPacket(0)...)

	goRes, goErr := local.Ingest(ctx, offset, chunk1)
	if goErr != nil {
		t.Fatalf("step 1 local failed: %v", goErr)
	}
	rustRes, rustErr := remote.Ingest(ctx, offset, chunk1)
	if rustErr != nil {
		t.Fatalf("step 1 remote failed: %v", rustErr)
	}
	offset += int64(len(chunk1))

	if goRes.Timing.Authority != mediafacts.TimingAuthorityNone {
		t.Errorf("step 1: go timing authority %s, want none", goRes.Timing.Authority)
	}
	if rustRes.Timing.Authority != mediafacts.TimingAuthorityCanonical {
		t.Fatalf("step 1: rust timing authority %s, want canonical", rustRes.Timing.Authority)
	}
	if !rustRes.Timing.HasActiveEpoch || rustRes.Timing.ActiveEpoch != 0 {
		t.Errorf("step 1: rust active epoch has=%v, epoch=%d, want true/0",
			rustRes.Timing.HasActiveEpoch, rustRes.Timing.ActiveEpoch)
	}

	// Step 2: Ingest PCR packet (PCR = 27,000,000 = 1s) and video PES packet (PTS = 90,000 = 1s).
	var chunk2 []byte
	chunk2 = append(chunk2, buildPCRPacket(1, false, true, 27000000)...)
	chunk2 = append(chunk2, buildVideoPESPacket(1, 90000, 90000)...)

	goRes, goErr = local.Ingest(ctx, offset, chunk2)
	if goErr != nil {
		t.Fatalf("step 2 local failed: %v", goErr)
	}
	rustRes, rustErr = remote.Ingest(ctx, offset, chunk2)
	if rustErr != nil {
		t.Fatalf("step 2 remote failed: %v", rustErr)
	}
	offset += int64(len(chunk2))

	if goRes.Timing.Authority != mediafacts.TimingAuthorityNone {
		t.Errorf("step 2: go timing authority %s, want none", goRes.Timing.Authority)
	}
	if rustRes.Timing.Authority != mediafacts.TimingAuthorityCanonical {
		t.Fatalf("step 2: rust timing authority %s, want canonical", rustRes.Timing.Authority)
	}
	if len(rustRes.Timing.Records) != 2 {
		t.Fatalf("step 2: rust timing records count = %d, want 2: %+v",
			len(rustRes.Timing.Records), rustRes.Timing.Records)
	}

	// Record 0: PCR sample
	r0 := rustRes.Timing.Records[0]
	if r0.Type != mediafacts.TimingRecordTypePCR {
		t.Errorf("step 2 record 0 type %s, want pcr", r0.Type)
	}
	if r0.PCR.Epoch != 0 || r0.PCR.PCRPID != 0x0100 || r0.PCR.ExtendedPCR27m != 27000000 {
		t.Errorf("step 2 record 0 PCR mismatch: %+v", r0.PCR)
	}

	// Record 1: PES timing
	r1 := rustRes.Timing.Records[1]
	if r1.Type != mediafacts.TimingRecordTypePES {
		t.Errorf("step 2 record 1 type %s, want pes", r1.Type)
	}
	if r1.PES.Epoch != 0 || r1.PES.PID != 0x0101 || !r1.PES.HasPTS || r1.PES.PTS90k != 90000 {
		t.Errorf("step 2 record 1 PES mismatch: %+v", r1.PES)
	}

	// Step 3: Ingest PCR Discontinuity Indicator without PCR (DI = 1, PCR = None).
	var chunk3 []byte
	chunk3 = append(chunk3, buildPCRPacket(2, true, false, 0)...)

	goRes, goErr = local.Ingest(ctx, offset, chunk3)
	if goErr != nil {
		t.Fatalf("step 3 local failed: %v", goErr)
	}
	rustRes, rustErr = remote.Ingest(ctx, offset, chunk3)
	if rustErr != nil {
		t.Fatalf("step 3 remote failed: %v", rustErr)
	}
	offset += int64(len(chunk3))

	if !rustRes.Timing.HasActiveEpoch || rustRes.Timing.ActiveEpoch != 1 {
		t.Errorf("step 3: rust active epoch has=%v, epoch=%d, want true/1",
			rustRes.Timing.HasActiveEpoch, rustRes.Timing.ActiveEpoch)
	}
	if len(rustRes.Timing.Records) != 1 {
		t.Fatalf("step 3: rust timing records count = %d, want 1", len(rustRes.Timing.Records))
	}
	disc := rustRes.Timing.Records[0]
	if disc.Type != mediafacts.TimingRecordTypeDiscontinuity {
		t.Fatalf("step 3 record type %s, want discontinuity", disc.Type)
	}
	if disc.Discontinuity.Scope != mediafacts.DiscontinuityScopeProgram ||
		disc.Discontinuity.Reason != mediafacts.DiscontinuityReasonPCRDiscontinuityIndicator ||
		!disc.Discontinuity.HasEpochBefore || disc.Discontinuity.EpochBefore != 0 ||
		!disc.Discontinuity.HasEpochAfter || disc.Discontinuity.EpochAfter != 1 {
		t.Errorf("step 3 discontinuity record mismatch: %+v", disc.Discontinuity)
	}

	// Step 4: Ingest next PCR sample after DI -> establishes new baseline in Epoch 1.
	var chunk4 []byte
	chunk4 = append(chunk4, buildPCRPacket(3, false, true, 54000000)...)

	goRes, goErr = local.Ingest(ctx, offset, chunk4)
	if goErr != nil {
		t.Fatalf("step 4 local failed: %v", goErr)
	}
	rustRes, rustErr = remote.Ingest(ctx, offset, chunk4)
	if rustErr != nil {
		t.Fatalf("step 4 remote failed: %v", rustErr)
	}
	offset += int64(len(chunk4))

	// Must NOT trigger a second discontinuity record!
	if rustRes.Timing.ActiveEpoch != 1 {
		t.Errorf("step 4 active epoch %d, want 1", rustRes.Timing.ActiveEpoch)
	}
	if len(rustRes.Timing.Records) != 1 {
		t.Fatalf("step 4 record count %d, want 1", len(rustRes.Timing.Records))
	}
	if rustRes.Timing.Records[0].Type != mediafacts.TimingRecordTypePCR {
		t.Errorf("step 4 record type %s, want pcr", rustRes.Timing.Records[0].Type)
	}
	if rustRes.Timing.Records[0].PCR.Epoch != 1 {
		t.Errorf("step 4 PCR epoch %d, want 1", rustRes.Timing.Records[0].PCR.Epoch)
	}

	t.Logf("timing differential over IPC passed: active epoch lifecycle, unwrapped timing, and discontinuity publication verified")
}
