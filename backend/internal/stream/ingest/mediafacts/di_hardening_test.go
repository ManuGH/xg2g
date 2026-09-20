// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package mediafacts

import (
	"bytes"
	"context"
	"testing"
)

func makeTSPacket(pid uint16, pusi bool, afc byte, cc byte, adaptation []byte, payload []byte) []byte {
	pkt := make([]byte, TSPacketSize)
	pkt[0] = SyncByte
	if pusi {
		pkt[1] |= 0x40
	}
	pkt[1] |= byte((pid >> 8) & 0x1F)
	pkt[2] = byte(pid & 0xFF)
	pkt[3] = ((afc & 0x03) << 4) | (cc & 0x0F)

	idx := 4
	if afc == 0x02 || afc == 0x03 {
		pkt[4] = byte(len(adaptation))
		idx = 5
		copy(pkt[idx:], adaptation)
		idx += len(adaptation)
	}
	copy(pkt[idx:], payload)
	return pkt
}

func makeAdaptationOnlyDIPacket(pid uint16, cc byte) []byte {
	// afc = 0b10 (adaptation only), afl = 183, DI flag set at byte 5 (0x80)
	adapt := make([]byte, 183)
	adapt[0] = 0x80 // discontinuity_indicator = 1
	return makeTSPacket(pid, false, 0x02, cc, adapt, nil)
}

func makePayloadDIPacket(pid uint16, pusi bool, cc byte, payload []byte) []byte {
	// afc = 0b11 (adaptation + payload), afl = 1, DI flag set at byte 5 (0x80)
	adapt := []byte{0x80}
	return makeTSPacket(pid, pusi, 0x03, cc, adapt, payload)
}

func TestDITracker_SequentialPlusDI_IsSequential(t *testing.T) {
	tracker := &esPacketTracker{}
	p0 := makeTSPacket(0x100, false, 0x01, 1, nil, bytes.Repeat([]byte{0xAA}, 184))
	if seq := tracker.classify(p0); seq != esFirst {
		t.Fatalf("first packet: got %v, want esFirst", seq)
	}

	p1 := makePayloadDIPacket(0x100, false, 2, bytes.Repeat([]byte{0xBB}, 182))
	if seq := tracker.classify(p1); seq != esSequential {
		t.Fatalf("sequential + DI: got %v, want esSequential", seq)
	}
}

func TestDITracker_UnexpectedJumpPlusDI_IsDiscontinuity(t *testing.T) {
	tracker := &esPacketTracker{}
	p0 := makeTSPacket(0x100, false, 0x01, 1, nil, bytes.Repeat([]byte{0xAA}, 184))
	tracker.classify(p0)

	// Jump from CC 1 to CC 5 with DI
	p1 := makePayloadDIPacket(0x100, false, 5, bytes.Repeat([]byte{0xBB}, 182))
	if seq := tracker.classify(p1); seq != esDiscontinuity {
		t.Fatalf("unexpected jump + DI: got %v, want esDiscontinuity", seq)
	}

	// Next sequential CC 6 is esSequential
	p2 := makeTSPacket(0x100, false, 0x01, 6, nil, bytes.Repeat([]byte{0xCC}, 184))
	if seq := tracker.classify(p2); seq != esSequential {
		t.Fatalf("subsequent packet: got %v, want esSequential", seq)
	}
}

func TestDITracker_SameCCDifferentPlusDI_IsDiscontinuity(t *testing.T) {
	tracker := &esPacketTracker{}
	p0 := makeTSPacket(0x100, false, 0x01, 7, nil, bytes.Repeat([]byte{0xAA}, 184))
	tracker.classify(p0)

	// Same CC 7 with different bytes and DI
	p1 := makePayloadDIPacket(0x100, false, 7, bytes.Repeat([]byte{0xBB}, 182))
	if seq := tracker.classify(p1); seq != esDiscontinuity {
		t.Fatalf("same CC different + DI: got %v, want esDiscontinuity", seq)
	}
}

func TestDITracker_AdaptationOnlyDISurvivesExactDuplicate(t *testing.T) {
	tracker := &esPacketTracker{}
	p0 := makeTSPacket(0x100, false, 0x01, 4, nil, bytes.Repeat([]byte{0xAA}, 184))
	tracker.classify(p0)

	// Adaptation-only DI arms pendingDiscontinuity
	tracker.pendingDiscontinuity = true

	// Exact duplicate of CC 4 arrives
	dup := append([]byte(nil), p0...)
	if seq := tracker.classify(dup); seq != esExactDuplicate {
		t.Fatalf("exact duplicate: got %v, want esExactDuplicate", seq)
	}
	if !tracker.pendingDiscontinuity {
		t.Fatalf("pendingDiscontinuity must survive exact duplicate")
	}

	// Unexpected jump CC 8 without DI on the packet itself
	p1 := makeTSPacket(0x100, false, 0x01, 8, nil, bytes.Repeat([]byte{0xBB}, 184))
	if seq := tracker.classify(p1); seq != esDiscontinuity {
		t.Fatalf("CC jump after pending DI: got %v, want esDiscontinuity", seq)
	}
	if tracker.pendingDiscontinuity {
		t.Fatalf("pendingDiscontinuity must be consumed after jump")
	}
}

func TestDITracker_AdaptationOnlyDISurvivesExactDuplicateAndSequentialIsSequential(t *testing.T) {
	tracker := &esPacketTracker{}
	p0 := makeTSPacket(0x100, false, 0x01, 4, nil, bytes.Repeat([]byte{0xAA}, 184))
	tracker.classify(p0)

	// Adaptation-only DI arms pendingDiscontinuity
	tracker.pendingDiscontinuity = true

	// Exact duplicate of CC 4 arrives
	dup := append([]byte(nil), p0...)
	if seq := tracker.classify(dup); seq != esExactDuplicate {
		t.Fatalf("exact duplicate: got %v, want esExactDuplicate", seq)
	}
	if !tracker.pendingDiscontinuity {
		t.Fatalf("pendingDiscontinuity must survive exact duplicate")
	}

	// Sequential CC 5 arrives without DI
	p1 := makeTSPacket(0x100, false, 0x01, 5, nil, bytes.Repeat([]byte{0xBB}, 184))
	if seq := tracker.classify(p1); seq != esSequential {
		t.Fatalf("sequential packet after pending DI: got %v, want esSequential", seq)
	}
	if tracker.pendingDiscontinuity {
		t.Fatalf("pendingDiscontinuity must be cleared after sequential packet")
	}
}

func TestDITracker_ResetClearsPendingDiscontinuity(t *testing.T) {
	tracker := &esPacketTracker{}
	tracker.pendingDiscontinuity = true
	tracker.reset()
	if tracker.pendingDiscontinuity {
		t.Fatalf("reset must clear pendingDiscontinuity")
	}
}

func TestCore_AdaptationOnlyDIRoutesToPID(t *testing.T) {
	b := vNew("test", "test", videoTSProgram)
	core := NewGoCore(videoTSProgram)
	ctx := context.Background()
	v := uint16(videoTSPID)

	psi := b.psi(0, h264Stream(v))
	// Ingest PSI to set up program
	if _, err := core.Ingest(ctx, 0, psi); err != nil {
		t.Fatalf("ingest PSI: %v", err)
	}

	// Adaptation-only packet with DI on video PID
	adaptPkt := makeAdaptationOnlyDIPacket(v, 2)
	if _, err := core.Ingest(ctx, 0, adaptPkt); err != nil {
		t.Fatalf("ingest adaptation-only: %v", err)
	}

	// Clear packets counter must NOT advance on adaptation-only
	if core.clearVideoPackets != 0 {
		t.Fatalf("adaptation-only packet must not increment clearVideoPackets (got %d)", core.clearVideoPackets)
	}

	// Tracker must have pendingDiscontinuity armed
	tracker := core.esTrackers[v]
	if tracker == nil || !tracker.pendingDiscontinuity {
		t.Fatalf("tracker for video PID must have pendingDiscontinuity armed")
	}
}

func TestCore_PSI_DiscontinuityDiscardsInFlightSection(t *testing.T) {
	core := NewGoCore(videoTSProgram)
	ctx := context.Background()

	// Incomplete PAT: declares section_length 250, so it cannot fit in a single 184-byte TS packet
	incompletePAT := make([]byte, 50)
	incompletePAT[0] = 0x00 // table_id = 0 (PAT)
	incompletePAT[1] = 0xB0 // section_syntax_indicator=1
	incompletePAT[2] = 0xFA // section_length = 250 (exceeds single packet capacity of 183 bytes)
	incompletePAT[3] = 0x00 // transport_stream_id = 1
	incompletePAT[4] = 0x01
	incompletePAT[5] = 0xC1 // version=0, current_next_indicator=1
	incompletePAT[6] = 0x00 // section_number = 0
	incompletePAT[7] = 0x00 // last_section_number = 0
	p0 := audioTSPSIPacket(0, 0, incompletePAT)

	if _, err := core.Ingest(ctx, 0, p0); err != nil {
		t.Fatalf("ingest p0: %v", err)
	}
	if len(core.patAssembler.buf) == 0 {
		t.Fatalf("PAT assembler should have in-flight buffer")
	}

	// Adaptation-only DI on PID 0
	adaptPkt := makeAdaptationOnlyDIPacket(0, 0)
	if _, err := core.Ingest(ctx, 0, adaptPkt); err != nil {
		t.Fatalf("ingest adaptPkt: %v", err)
	}
	if !core.patAssembler.pendingDiscontinuity {
		t.Fatalf("PAT assembler must have pendingDiscontinuity armed")
	}

	// Exact duplicate on PID 0
	dup := append([]byte(nil), p0...)
	if _, err := core.Ingest(ctx, 0, dup); err != nil {
		t.Fatalf("ingest dup: %v", err)
	}
	if !core.patAssembler.pendingDiscontinuity {
		t.Fatalf("PAT assembler pendingDiscontinuity must survive duplicate")
	}

	// CC jump on PID 0 without PUSI (continuation packet after jump)
	jumpCont := makeTSPacket(0, false, 0x01, 5, nil, bytes.Repeat([]byte{0xFF}, 184))
	if _, err := core.Ingest(ctx, 0, jumpCont); err != nil {
		t.Fatalf("ingest jumpCont: %v", err)
	}

	// In-flight buffer must have been cleared by Discontinuity
	if len(core.patAssembler.buf) != 0 {
		t.Fatalf("PAT assembler buf must be cleared on discontinuity (got len %d)", len(core.patAssembler.buf))
	}
}

func TestCore_PSI_PartialPMT_DIContinuationJump_BaselineUpdated_ActivePMTKept(t *testing.T) {
	b := vNew("test", "test", videoTSProgram)
	core := NewGoCore(videoTSProgram)
	ctx := context.Background()
	v := uint16(videoTSPID)

	// Step 1: establish initial active PMT
	initPSI := b.psi(0, h264Stream(v))
	if _, err := core.Ingest(ctx, 0, initPSI); err != nil {
		t.Fatalf("ingest initPSI: %v", err)
	}
	if core.pmtPID != audioTSPMTPID {
		t.Fatalf("expected pmtPID %d, got %d", audioTSPMTPID, core.pmtPID)
	}
	if core.videoPID != v {
		t.Fatalf("expected videoPID %d, got %d", v, core.videoPID)
	}

	// Step 2: partial PMT on PMT PID starts assembly (version 1)
	// Declares 250 bytes so it stays incomplete in assembler.buf
	incompletePMT := make([]byte, 50)
	incompletePMT[0] = 0x02 // table_id = 2 (PMT)
	incompletePMT[1] = 0xB0 // section_syntax_indicator=1
	incompletePMT[2] = 0xFA // section_length = 250
	incompletePMT[3] = byte((videoTSProgram >> 8) & 0xFF)
	incompletePMT[4] = byte(videoTSProgram & 0xFF)
	incompletePMT[5] = 0xC3 // version=1, current_next_indicator=1
	incompletePMT[6] = 0x00 // section_number = 0
	incompletePMT[7] = 0x00 // last_section_number = 0

	pmtP0 := audioTSPSIPacket(audioTSPMTPID, 2, incompletePMT)
	if _, err := core.Ingest(ctx, 0, pmtP0); err != nil {
		t.Fatalf("ingest pmtP0: %v", err)
	}
	if len(core.pmtAssembler.buf) == 0 {
		t.Fatalf("pmtAssembler must have in-flight buffer")
	}

	// Step 3: DI continuation CC jump (e.g. CC jump from 2 to 7 with DI flag, pusi=false)
	diCont := makePayloadDIPacket(audioTSPMTPID, false, 7, bytes.Repeat([]byte{0xAA}, 182))
	if _, err := core.Ingest(ctx, 0, diCont); err != nil {
		t.Fatalf("ingest diCont: %v", err)
	}

	// In-flight section was discarded, but old active PMT remains!
	if len(core.pmtAssembler.buf) != 0 {
		t.Fatalf("pmtAssembler buf must be cleared on DI jump (got len %d)", len(core.pmtAssembler.buf))
	}
	if core.videoPID != v {
		t.Fatalf("active PMT must be retained across partial section discard (got videoPID %d)", core.videoPID)
	}

	// Step 4: Next clear packet on PMT PID arrives with CC=8 (CC+1 sequential)
	// It MUST be treated as sequential, NOT as a second artificial continuity break!
	seqCont := makeTSPacket(audioTSPMTPID, false, 0x01, 8, nil, bytes.Repeat([]byte{0xBB}, 184))
	if _, err := core.Ingest(ctx, 0, seqCont); err != nil {
		t.Fatalf("ingest seqCont: %v", err)
	}

	// Baseline was updated to 8
	if core.pmtAssembler.lastCC != 8 {
		t.Fatalf("pmtAssembler lastCC must be 8, got %d", core.pmtAssembler.lastCC)
	}
}
