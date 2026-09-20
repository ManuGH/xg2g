// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package mediafacts

import (
	"context"
	"testing"
)

// TestAudioContinuity_SameCCDifferent_InElementaryStream_DiscardsPayloadAndCountsClear verifies
// that an unannounced same-CC packet with different bytes during elementary stream:
// 1. Increments clearAudioPackets and audioClearRun.
// 2. Is NOT fed to the audio frame observer (corrupt duplicate).
// 3. Allows subsequent sequential continuations to be fed normally.
func TestAudioContinuity_SameCCDifferent_InElementaryStream_DiscardsPayloadAndCountsClear(t *testing.T) {
	core := NewGoCore(audioTSProgram)
	ctx := context.Background()

	pat := audioTSPSIPacket(0, 0, audioTSPAT(audioTSProgram))
	pmt := audioTSPSIPacket(audioTSPMTPID, 0, audioTSPMT(audioTSProgram, 0, ac3Stream(audioTSAudioA)))
	start := pesStart(0xBD, 0, audioTSAC3Frame(audioTSByte6Stereo))
	p0 := audioTSPacket(audioTSAudioA, true, 5, start)

	_, err := core.Ingest(ctx, 0, cat(pat, pmt, p0))
	if err != nil {
		t.Fatalf("ingest initial: %v", err)
	}

	stream := core.audioStreams[audioTSAudioA]
	if stream == nil {
		t.Fatal("expected audio stream 0x0100 to be registered")
	}
	if frames := stream.observer.Current().Frames; frames != 1 {
		t.Fatalf("expected 1 initial frame, got %d", frames)
	}
	clearBefore := core.clearAudioPackets

	// Send conflicting same-CC packet (CC=5, diffPayload, pusi=false, !DI)
	diffPayload := audioTSPad(audioTSAC3Frame(audioTSByte6Surround))
	p1Broken := audioTSPacket(audioTSAudioA, false, 5, diffPayload)

	_, err = core.Ingest(ctx, 3*TSPacketSize, p1Broken)
	if err != nil {
		t.Fatalf("ingest same-cc broken: %v", err)
	}

	// Physical clear packet counted, but payload NOT fed to observer
	if core.clearAudioPackets != clearBefore+1 {
		t.Fatalf("expected clearAudioPackets %d, got %d", clearBefore+1, core.clearAudioPackets)
	}
	if frames := stream.observer.Current().Frames; frames != 1 {
		t.Fatalf("expected frames to remain 1 (broken packet suppressed), got %d", frames)
	}

	// Send subsequent sequential continuation (CC=6, valid AC-3 frame)
	p2Seq := audioTSPacket(audioTSAudioA, false, 6, audioTSPad(audioTSAC3Frame(audioTSByte6Stereo)))
	_, err = core.Ingest(ctx, 4*TSPacketSize, p2Seq)
	if err != nil {
		t.Fatalf("ingest sequential: %v", err)
	}

	if frames := stream.observer.Current().Frames; frames != 2 {
		t.Fatalf("expected frames to advance to 2 on sequential packet, got %d", frames)
	}
}

// TestAudioContinuity_SameCCDifferent_OnPUSI_TriggersAwaitingStart verifies that
// a conflicting same-CC packet asserting PUSI=1 cannot establish a valid PES start,
// forcing AwaitingStart and incrementing audioUnreadableStarts.
func TestAudioContinuity_SameCCDifferent_OnPUSI_TriggersAwaitingStart(t *testing.T) {
	core := NewGoCore(audioTSProgram)
	ctx := context.Background()

	pat := audioTSPSIPacket(0, 0, audioTSPAT(audioTSProgram))
	pmt := audioTSPSIPacket(audioTSPMTPID, 0, audioTSPMT(audioTSProgram, 0, ac3Stream(audioTSAudioA)))
	start := pesStart(0xBD, 0, audioTSAC3Frame(audioTSByte6Stereo))
	p0 := audioTSPacket(audioTSAudioA, true, 4, start)

	_, err := core.Ingest(ctx, 0, cat(pat, pmt, p0))
	if err != nil {
		t.Fatalf("ingest initial: %v", err)
	}

	unreadableBefore := core.audioUnreadableStarts

	// Send conflicting same-CC packet with PUSI=1 (CC=4, new header)
	newStart := pesStart(0xBD, 0, audioTSAC3Frame(audioTSByte6Surround))
	pConflictPUSI := audioTSPacket(audioTSAudioA, true, 4, newStart)

	_, err = core.Ingest(ctx, 3*TSPacketSize, pConflictPUSI)
	if err != nil {
		t.Fatalf("ingest conflict PUSI: %v", err)
	}

	if core.audioUnreadableStarts != unreadableBefore+1 {
		t.Fatalf("expected audioUnreadableStarts %d, got %d", unreadableBefore+1, core.audioUnreadableStarts)
	}
	stream := core.audioStreams[audioTSAudioA]
	if !stream.awaitingStart {
		t.Fatal("expected stream to enter awaitingStart after conflicting PUSI")
	}
}

// TestAudioContinuity_UnannouncedCCJump_InIncompleteHeader_DiscardsPESAndForcesAwaitingStart verifies
// that an unannounced CC gap while optional PES header is incomplete discards partial PES,
// forces awaitingStart, and drops subsequent continuations until the next PUSI.
func TestAudioContinuity_UnannouncedCCJump_InIncompleteHeader_DiscardsPESAndForcesAwaitingStart(t *testing.T) {
	core := NewGoCore(audioTSProgram)
	ctx := context.Background()

	pat := audioTSPSIPacket(0, 0, audioTSPAT(audioTSProgram))
	pmt := audioTSPSIPacket(audioTSPMTPID, 0, audioTSPMT(audioTSProgram, 0, ac3Stream(audioTSAudioA)))

	// PUSI packet with 200-byte optional header; only 184 bytes fit (25 bytes remaining)
	full := audioTSPESHeader(0xBD, 200)
	p0 := audioTSPacket(audioTSAudioA, true, 0, full[:184])

	_, err := core.Ingest(ctx, 0, cat(pat, pmt, p0))
	if err != nil {
		t.Fatalf("ingest initial header: %v", err)
	}

	stream := core.audioStreams[audioTSAudioA]
	if stream.headerRemaining != 25 {
		t.Fatalf("expected headerRemaining=25, got %d", stream.headerRemaining)
	}
	unreadableBefore := core.audioUnreadableStarts

	// Unannounced CC gap: skip CC=1, send CC=2
	c1 := audioTSPad(audioTSAC3Frame(audioTSByte6Stereo))
	p1Gap := audioTSPacket(audioTSAudioA, false, 2, c1)

	_, err = core.Ingest(ctx, 3*TSPacketSize, p1Gap)
	if err != nil {
		t.Fatalf("ingest CC jump continuation: %v", err)
	}

	if core.audioUnreadableStarts != unreadableBefore+1 {
		t.Fatalf("expected audioUnreadableStarts %d, got %d", unreadableBefore+1, core.audioUnreadableStarts)
	}
	if !stream.awaitingStart {
		t.Fatal("expected stream to enter awaitingStart after CC gap in incomplete header")
	}
	if stream.headerRemaining != 0 {
		t.Fatalf("expected headerRemaining to be reset to 0, got %d", stream.headerRemaining)
	}
	if frames := stream.observer.Current().Frames; frames != 0 {
		t.Fatalf("expected 0 frames (header lost, continuation dropped), got %d", frames)
	}
}

// TestAudioContinuity_UnannouncedCCJump_InRunningES_PreservesContract verifies that an
// unannounced CC gap during already-running elementary stream continues feeding the payload
// to the observer (preserving the existing chosen contract).
func TestAudioContinuity_UnannouncedCCJump_InRunningES_PreservesContract(t *testing.T) {
	core := NewGoCore(audioTSProgram)
	ctx := context.Background()

	pat := audioTSPSIPacket(0, 0, audioTSPAT(audioTSProgram))
	pmt := audioTSPSIPacket(audioTSPMTPID, 0, audioTSPMT(audioTSProgram, 0, ac3Stream(audioTSAudioA)))
	start := pesStart(0xBD, 0, audioTSAC3Frame(audioTSByte6Stereo))
	p0 := audioTSPacket(audioTSAudioA, true, 1, start)

	_, err := core.Ingest(ctx, 0, cat(pat, pmt, p0))
	if err != nil {
		t.Fatalf("ingest initial: %v", err)
	}

	stream := core.audioStreams[audioTSAudioA]
	if frames := stream.observer.Current().Frames; frames != 1 {
		t.Fatalf("expected 1 initial frame, got %d", frames)
	}

	// Skip CC=2, send CC=5 carrying AC-3 frame in running elementary stream
	c1 := audioTSPad(audioTSAC3Frame(audioTSByte6Stereo))
	p1Gap := audioTSPacket(audioTSAudioA, false, 5, c1)

	_, err = core.Ingest(ctx, 3*TSPacketSize, p1Gap)
	if err != nil {
		t.Fatalf("ingest CC jump in ES: %v", err)
	}

	// Existing contract: payload in running ES is fed to observer despite CC gap
	if frames := stream.observer.Current().Frames; frames != 2 {
		t.Fatalf("expected frames to advance to 2 (running ES gap fed to observer), got %d", frames)
	}
}

// TestAudioContinuity_SameCCDifferent_WithDI_PreservesExistingBehaviorForPR_B explicitly verifies
// that a packet with same-CC + different bytes but with Discontinuity Indicator (DI) set:
// 1. Does NOT trigger sameCCConflict (preserves existing behavior).
// 2. Is explicitly left for DI-hardening in PR B as mandated by project governance.
func TestAudioContinuity_SameCCDifferent_WithDI_PreservesExistingBehaviorForPR_B(t *testing.T) {
	core := NewGoCore(audioTSProgram)
	ctx := context.Background()

	pat := audioTSPSIPacket(0, 0, audioTSPAT(audioTSProgram))
	pmt := audioTSPSIPacket(audioTSPMTPID, 0, audioTSPMT(audioTSProgram, 0, ac3Stream(audioTSAudioA)))
	start := pesStart(0xBD, 0, audioTSAC3Frame(audioTSByte6Stereo))
	p0 := audioTSPacket(audioTSAudioA, true, 3, start)

	_, err := core.Ingest(ctx, 0, cat(pat, pmt, p0))
	if err != nil {
		t.Fatalf("ingest initial: %v", err)
	}

	stream := core.audioStreams[audioTSAudioA]
	if frames := stream.observer.Current().Frames; frames != 1 {
		t.Fatalf("expected 1 initial frame, got %d", frames)
	}

	// Send packet with same CC (3), different bytes, but WITH DI set in adaptation field
	diffPayload := audioTSAC3Frame(audioTSByte6Surround)
	p1WithDI := audioTSShortPacketWithDI(audioTSAudioA, false, 3, diffPayload)

	if !hasDiscontinuityIndicator(p1WithDI) {
		t.Fatal("expected test packet to have discontinuity indicator set")
	}

	_, err = core.Ingest(ctx, 3*TSPacketSize, p1WithDI)
	if err != nil {
		t.Fatalf("ingest same-CC with DI: %v", err)
	}

	// PR A invariant: when DI is set, sameCCConflict is false. PR A does NOT preempt PR B.
	// In running ES, the payload is fed just as it was before PR A.
	if frames := stream.observer.Current().Frames; frames != 2 {
		t.Fatalf("expected frames to be 2 (DI behavior preserved for PR B), got %d", frames)
	}
}
