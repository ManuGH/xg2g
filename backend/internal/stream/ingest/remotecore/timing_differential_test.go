// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package remotecore

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/stream/ingest/mediafacts"
	"github.com/ManuGH/xg2g/internal/stream/ingest/ring"
	"github.com/ManuGH/xg2g/internal/stream/timeline"
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

// buildPATPacketForProgram constructs a 188-byte TS packet carrying a PAT for program -> PMT PID.
func buildPATPacketForProgram(cc uint8, program uint16, pmtPID uint16) []byte {
	section := []byte{
		0x00, 0xB0, 0x0D, 0x00, 0x01, 0xC1, 0x00, 0x00, // header
		byte(program >> 8), byte(program & 0xFF),
		0xE0 | byte(pmtPID>>8), byte(pmtPID & 0xFF),
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

// buildPMTPacketForProgram constructs a 188-byte TS packet carrying a PMT on pmtPID for program.
func buildPMTPacketForProgram(cc uint8, program uint16, pmtPID uint16, pcrPID uint16, videoPID uint16) []byte {
	section := []byte{
		0x02, 0xB0, 0x12, byte(program >> 8), byte(program & 0xFF), 0xC1, 0x00, 0x00, // header
		0xE0 | byte(pcrPID>>8), byte(pcrPID & 0xFF),
		0xF0, 0x00, // program info length 0
		0x1B, 0xE0 | byte(videoPID>>8), byte(videoPID & 0xFF), 0xF0, 0x00, // stream_type 0x1B H.264
		0x00, 0x00, 0x00, 0x00, // CRC placeholder
	}
	crc := ring.CalculateMPEG2CRC32(section[:len(section)-4])
	binary.BigEndian.PutUint32(section[len(section)-4:], crc)

	pkt := make([]byte, mediafacts.TSPacketSize)
	pkt[0] = 0x47
	pkt[1] = 0x40 | byte((pmtPID>>8)&0x1F) // PUSI = 1
	pkt[2] = byte(pmtPID & 0xFF)
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
	if len(rustRes.Timing.Records) != 3 {
		t.Fatalf("step 2: rust timing records count = %d, want 3: %+v",
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

	// Record 2: the IDR in that PES is a RAP, published bound to the PES's timing.
	r2 := rustRes.Timing.Records[2]
	if r2.Type != mediafacts.TimingRecordTypeRandomAccessPoint {
		t.Errorf("step 2 record 2 type %s, want rap", r2.Type)
	}
	if r2.RAP != r1.PES {
		t.Errorf("step 2 record 2 RAP binding %+v, want the PES timing point %+v", r2.RAP, r1.PES)
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

	// Step 5: Control-plane call SetTargetProgram(1) with the SAME target (redundant call).
	// Invariant: Must be strictly idempotent. Preserves ActiveEpoch (1), emits NO events,
	// and fabricates NO artificial timing records.
	goRes, goErr = local.SetTargetProgram(ctx, 1)
	if goErr != nil {
		t.Fatalf("step 5 local failed: %v", goErr)
	}
	rustRes, rustErr = remote.SetTargetProgram(ctx, 1)
	if rustErr != nil {
		t.Fatalf("step 5 remote failed: %v", rustErr)
	}
	if len(rustRes.Events) != 0 {
		t.Errorf("step 5: expected 0 events on redundant SetTargetProgram, got %d", len(rustRes.Events))
	}
	if !rustRes.Timing.HasActiveEpoch || rustRes.Timing.ActiveEpoch != 1 {
		t.Errorf("step 5: active epoch must be preserved across redundant call, got has=%v epoch=%d",
			rustRes.Timing.HasActiveEpoch, rustRes.Timing.ActiveEpoch)
	}
	if len(rustRes.Timing.Records) != 0 {
		t.Errorf("step 5: control plane must not fabricate timing records, got %d", len(rustRes.Timing.Records))
	}
	_ = goRes

	// Step 6: Control-plane call SetTargetProgram(2) with a NEW target.
	// Invariant: Resets follower, deactivates timeline (HasActiveEpoch = false),
	// emits ProgramIdentityChanged, and fabricates NO artificial timing records.
	goRes, goErr = local.SetTargetProgram(ctx, 2)
	if goErr != nil {
		t.Fatalf("step 6 local failed: %v", goErr)
	}
	rustRes, rustErr = remote.SetTargetProgram(ctx, 2)
	if rustErr != nil {
		t.Fatalf("step 6 remote failed: %v", rustErr)
	}
	if len(rustRes.Events) != 1 || rustRes.Events[0].Kind != mediafacts.EventProgramIdentityChanged {
		t.Errorf("step 6: expected ProgramIdentityChanged event, got %+v", rustRes.Events)
	}
	if rustRes.Timing.HasActiveEpoch {
		t.Errorf("step 6: active epoch must be deactivated (false) on program change, got true (epoch=%d)",
			rustRes.Timing.ActiveEpoch)
	}
	if len(rustRes.Timing.Records) != 0 {
		t.Errorf("step 6: control plane must not fabricate timing records on target change, got %d", len(rustRes.Timing.Records))
	}

	// Step 7: Control-plane call SetTargetProgram(2) AGAIN with target 2 (redundant call while deactivated).
	// Invariant: Strictly idempotent. Emits NO events, timeline remains deactivated, NO records.
	goRes, goErr = local.SetTargetProgram(ctx, 2)
	if goErr != nil {
		t.Fatalf("step 7 local failed: %v", goErr)
	}
	rustRes, rustErr = remote.SetTargetProgram(ctx, 2)
	if rustErr != nil {
		t.Fatalf("step 7 remote failed: %v", rustErr)
	}
	if len(rustRes.Events) != 0 {
		t.Errorf("step 7: expected 0 events on repeated SetTargetProgram(2), got %d", len(rustRes.Events))
	}
	if rustRes.Timing.HasActiveEpoch {
		t.Errorf("step 7: active epoch must remain false on repeated call, got true")
	}
	if len(rustRes.Timing.Records) != 0 {
		t.Errorf("step 7: control plane must not fabricate timing records, got %d", len(rustRes.Timing.Records))
	}

	// Step 8: Transport plane: Ingest PAT and PMT for Program 2.
	// Invariant: PAT accepts program 2 -> emits transport discontinuity at real byte offset.
	// PMT accepts program 2 -> mints Epoch 2 and emits transport discontinuity at real byte offset.
	chunk5StartOffset := offset
	var chunk5 []byte
	chunk5 = append(chunk5, buildPATPacketForProgram(4, 2, 0x0200)...)
	chunk5 = append(chunk5, buildPMTPacketForProgram(4, 2, 0x0200, 0x0200, 0x0201)...)

	goRes, goErr = local.Ingest(ctx, offset, chunk5)
	if goErr != nil {
		t.Fatalf("step 8 local failed: %v", goErr)
	}
	rustRes, rustErr = remote.Ingest(ctx, offset, chunk5)
	if rustErr != nil {
		t.Fatalf("step 8 remote failed: %v", rustErr)
	}
	offset += int64(len(chunk5))

	if !rustRes.Timing.HasActiveEpoch || rustRes.Timing.ActiveEpoch != 2 {
		t.Fatalf("step 8: rust active epoch has=%v epoch=%d, want true/2",
			rustRes.Timing.HasActiveEpoch, rustRes.Timing.ActiveEpoch)
	}
	if len(rustRes.Timing.Records) != 3 {
		t.Fatalf("step 8 record count = %d, want 3: %+v", len(rustRes.Timing.Records), rustRes.Timing.Records)
	}
	rCloseDisc := rustRes.Timing.Records[0]
	if rCloseDisc.Type != mediafacts.TimingRecordTypeDiscontinuity {
		t.Fatalf("step 8 record 0 type %s, want discontinuity", rCloseDisc.Type)
	}
	if !rCloseDisc.Discontinuity.HasEpochBefore || rCloseDisc.Discontinuity.EpochBefore != 1 || rCloseDisc.Discontinuity.HasEpochAfter {
		t.Errorf("step 8 record 0 closing edge mismatch: %+v", rCloseDisc.Discontinuity)
	}
	if rCloseDisc.Discontinuity.ObservedAt != chunk5StartOffset {
		t.Errorf("step 8 record 0 byte offset = %d, want %d",
			rCloseDisc.Discontinuity.ObservedAt, chunk5StartOffset)
	}
	rPatDisc := rustRes.Timing.Records[1]
	if rPatDisc.Type != mediafacts.TimingRecordTypeDiscontinuity {
		t.Fatalf("step 8 record 1 type %s, want discontinuity", rPatDisc.Type)
	}
	if rPatDisc.Discontinuity.ObservedAt != chunk5StartOffset {
		t.Errorf("step 8 record 1 byte offset = %d, want %d",
			rPatDisc.Discontinuity.ObservedAt, chunk5StartOffset)
	}
	rPmtDisc := rustRes.Timing.Records[2]
	if rPmtDisc.Type != mediafacts.TimingRecordTypeDiscontinuity {
		t.Fatalf("step 8 record 2 type %s, want discontinuity", rPmtDisc.Type)
	}
	if rPmtDisc.Discontinuity.ObservedAt != chunk5StartOffset+mediafacts.TSPacketSize {
		t.Errorf("step 8 record 2 byte offset = %d, want %d",
			rPmtDisc.Discontinuity.ObservedAt, chunk5StartOffset+mediafacts.TSPacketSize)
	}
	if rPmtDisc.Discontinuity.Scope != mediafacts.DiscontinuityScopeProgram ||
		rPmtDisc.Discontinuity.Reason != mediafacts.DiscontinuityReasonProgramIdentityChanged ||
		rPmtDisc.Discontinuity.HasEpochBefore ||
		!rPmtDisc.Discontinuity.HasEpochAfter || rPmtDisc.Discontinuity.EpochAfter != 2 {
		t.Errorf("step 8 PMT discontinuity record mismatch: %+v", rPmtDisc.Discontinuity)
	}

	t.Logf("timing differential over IPC passed: active epoch lifecycle, unwrapped timing, SetTargetProgram idempotency, and discontinuity publication verified")
}

// TestNegativeControl_ZapWithoutClosingEdgeFailsClosed proves that if the Rust core does NOT
// emit a closing edge on zap before a new program's PMT arrives, MediaIndex fails closed with
// ErrInconsistentEpochTransition. When the closing edge is present (the 9b-1 fix), MediaIndex
// accepts the transition cleanly with zero errors.
func TestNegativeControl_ZapWithoutClosingEdgeFailsClosed(t *testing.T) {
	coreBin := requireVideoRealCore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	remote, err := Start(ctx, coreBin, 1)
	if err != nil {
		t.Fatalf("start remote core: %v", err)
	}
	defer func() { _ = remote.Close() }()

	idx := timeline.NewMediaIndex()
	var offset int64

	// 1. Establish Program 1 -> active Epoch 0 in index.
	var initChunk []byte
	initChunk = append(initChunk, buildPATPacket(0)...)
	initChunk = append(initChunk, buildPMTPacket(0)...)
	res1, err := remote.Ingest(ctx, offset, initChunk)
	if err != nil {
		t.Fatalf("program 1 ingest: %v", err)
	}
	offset = res1.ProcessedThroughOffset
	if err := idx.ApplyIngestResult(res1); err != nil {
		t.Fatalf("apply program 1: %v", err)
	}

	// 2. Control plane zap to Program 2.
	_, err = remote.SetTargetProgram(ctx, 2)
	if err != nil {
		t.Fatalf("set target program 2: %v", err)
	}

	// 3. Ingest Program 2 PAT + PMT.
	var prog2Chunk []byte
	prog2Chunk = append(prog2Chunk, buildPATPacketForProgram(1, 2, 0x0200)...)
	prog2Chunk = append(prog2Chunk, buildPMTPacketForProgram(1, 2, 0x0200, 0x0200, 0x0201)...)
	res2, err := remote.Ingest(ctx, offset, prog2Chunk)
	if err != nil {
		t.Fatalf("program 2 ingest: %v", err)
	}

	// NEGATIVE CONTROL: Simulate the unpatched core (main) by removing the closing edge.
	// On main, res2.Timing.Records only had the PAT and PMT discontinuities,
	// without the preceding closing edge (HasEpochBefore: true, HasEpochAfter: false).
	simulatedMainRecords := make([]mediafacts.TimingRecord, 0, len(res2.Timing.Records))
	for _, rec := range res2.Timing.Records {
		if rec.Type == mediafacts.TimingRecordTypeDiscontinuity &&
			rec.Discontinuity.HasEpochBefore && !rec.Discontinuity.HasEpochAfter {
			// Drop the closing edge to simulate unpatched core behavior.
			continue
		}
		simulatedMainRecords = append(simulatedMainRecords, rec)
	}
	resSimulatedMain := res2
	resSimulatedMain.Timing.Records = simulatedMainRecords

	// Assert that MediaIndex fails closed with ErrInconsistentEpochTransition on unpatched output:
	errUnpatched := idx.ApplyIngestResult(resSimulatedMain)
	if !errors.Is(errUnpatched, timeline.ErrInconsistentEpochTransition) {
		t.Fatalf("negative control: got %v, want ErrInconsistentEpochTransition", errUnpatched)
	}

	// POSITIVE CONTROL: Assert that with the 9b-1 fix (closing edge present),
	// MediaIndex accepts the transition cleanly with zero errors.
	if err := idx.ApplyIngestResult(res2); err != nil {
		t.Fatalf("positive control: ApplyIngestResult failed with fix: %v", err)
	}
}
