// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package mediafacts

import (
	"context"
	"testing"
)

// 1. PSI Continuity with TEI:
// A TEI continuation packet at CC=n records CC in the tracker, so a subsequent
// clear continuation at CC=n+1 does NOT see an artificial second CC gap.
// The in-flight section corrupted by TEI remains discarded, and the existing
// active table remains valid throughout.
func TestTEI_PSI_ContinuityTracking_NoSyntheticGap(t *testing.T) {
	core := NewGoCore(audioTSProgram)
	ctx := context.Background()

	// Establish initial active PSI: PAT (v0) and PMT (v0) with AudioA on PID 0x0100
	pat0 := audioTSPSIPacket(0, 0, audioTSPAT(audioTSProgram))
	pmt0 := audioTSPSIPacket(audioTSPMTPID, 0, audioTSPMT(audioTSProgram, 0, ac3Stream(audioTSAudioA)))
	start := pesStart(0xBD, 0, audioTSAC3Frame(audioTSByte6Stereo))
	audioPkt1 := audioTSPacket(audioTSAudioA, true, 0, start)

	_, err := core.Ingest(ctx, 0, cat(pat0, pmt0, audioPkt1))
	if err != nil {
		t.Fatalf("initial ingest: %v", err)
	}

	// Now a new PMT version 1 (switching to AudioB) is split across packets:
	// Packet 0 (PUSI=1, CC=1): first 10 bytes of PMT v1
	pmtV1 := audioTSPMT(audioTSProgram, 1, ac3Stream(audioTSAudioB))
	head := append([]byte{0x00}, pmtV1[:10]...)
	p0 := audioTSShortPacket(audioTSPMTPID, true, 1, head)

	// Packet 1 (PUSI=0, CC=2, TEI=1): damaged continuation of PMT v1
	p1TEI := withTEI(audioTSPacket(audioTSPMTPID, false, 2, audioTSPad(pmtV1[10:15])))

	// Packet 2 (PUSI=0, CC=3, Clear): trailing bytes of PMT v1
	p2Clear := audioTSPacket(audioTSPMTPID, false, 3, audioTSPad(pmtV1[15:]))

	// Ingest p0, p1TEI, p2Clear
	_, err = core.Ingest(ctx, 3*TSPacketSize, cat(p0, p1TEI, p2Clear))
	if err != nil {
		t.Fatalf("ingest split PMT: %v", err)
	}

	// Verify CC tracking: p1TEI recorded CC=2, so p2Clear with CC=3 was sequential (not gap).
	// But because p1TEI was TEI, assembler.buf was emptied, so PMT v1 was never assembled.
	// PMT v0 must remain the active table.
	if len(core.audioPIDs) != 1 || core.audioPIDs[0] != audioTSAudioA {
		t.Fatalf("expected audio PID to remain AudioA (0x%04X), got %v", audioTSAudioA, core.audioPIDs)
	}

	// Subsequent AudioA packet is fed normally, confirming active PSI retention
	audioPkt2 := audioTSPacket(audioTSAudioA, true, 1, start)
	res, err := core.Ingest(ctx, 6*TSPacketSize, audioPkt2)
	if err != nil {
		t.Fatalf("ingest audioPkt2: %v", err)
	}
	stream := core.audioStreams[audioTSAudioA]
	if stream == nil || stream.awaitingStart {
		t.Fatalf("AudioA stream must remain active and not awaiting start")
	}
	if len(res.Facts.AudioTracks) != 1 || res.Facts.AudioTracks[0].Observed.Frames != 2 {
		t.Fatalf("expected 2 audio frames on AudioA, got %+v", res.Facts.AudioTracks)
	}
}

// 2. Audio: Split PES Header followed by TEI Continuation forces AwaitingStart.
func TestTEI_Audio_SplitPESHeader_TEIForcesAwaitingStart(t *testing.T) {
	b := audioTSNew("test", "test", audioTSProgram)
	core := NewGoCore(audioTSProgram)
	ctx := context.Background()

	psi := b.psi(0, ac3Stream(audioTSAudioA))
	full := audioTSPESHeader(0xBD, 200)
	start := full[:184]
	tail := make([]byte, 25)
	copy(tail, audioTSAC3Frame(audioTSByte6Surround)[:7])
	p1 := audioTSPad(append(append([]byte{}, tail...), audioTSAC3Frame(audioTSByte6Stereo)...))
	p0 := audioTSPacket(audioTSAudioA, true, 0, start)
	p1TEI := withTEI(audioTSPacket(audioTSAudioA, false, 1, p1))
	c2 := audioTSPad(audioTSAC3Frame(audioTSByte6Stereo))
	p2 := audioTSPacket(audioTSAudioA, false, 2, c2)

	res, err := core.Ingest(ctx, 0, cat(append(psi, p0, p1TEI, p2)...))
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}

	stream := core.audioStreams[audioTSAudioA]
	if stream == nil {
		t.Fatalf("AudioA stream not found")
	}
	if !stream.awaitingStart {
		t.Fatalf("expected awaitingStart=true due to TEI during incomplete header")
	}
	if stream.headerRemaining != 0 {
		t.Fatalf("expected headerRemaining=0 after transition to awaitingStart, got %d", stream.headerRemaining)
	}
	if len(res.Facts.AudioTracks) != 1 || res.Facts.AudioTracks[0].Observed.Frames != 0 {
		t.Fatalf("expected 0 observed frames due to awaitingStart, got %+v", res.Facts.AudioTracks)
	}
}

// 3. Audio: Split PES Header followed by normal clear continuation preserves
// existing Go reference feed behavior (divergence guard for a_pes_header_reaching_past_its_packet).
func TestTEI_Audio_SplitPESHeader_ClearContinuation_PreservesGoFeedBehavior(t *testing.T) {
	b := audioTSNew("test", "test", audioTSProgram)
	core := NewGoCore(audioTSProgram)
	ctx := context.Background()

	psi := b.psi(0, ac3Stream(audioTSAudioA))
	full := audioTSPESHeader(0xBD, 200)
	start := full[:184]
	tail := make([]byte, 25)
	copy(tail, audioTSAC3Frame(audioTSByte6Surround)[:7])
	p1 := audioTSPad(append(append([]byte{}, tail...), audioTSAC3Frame(audioTSByte6Stereo)...))
	p0 := audioTSPacket(audioTSAudioA, true, 0, start)
	p1Clear := audioTSPacket(audioTSAudioA, false, 1, p1)
	c2 := audioTSPad(audioTSAC3Frame(audioTSByte6Stereo))
	p2 := audioTSPacket(audioTSAudioA, false, 2, c2)

	res, err := core.Ingest(ctx, 0, cat(append(psi, p0, p1Clear, p2)...))
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}

	stream := core.audioStreams[audioTSAudioA]
	if stream == nil {
		t.Fatalf("AudioA stream not found")
	}
	if stream.awaitingStart {
		t.Fatalf("expected awaitingStart=false for clear continuation")
	}
	// Go reference feeds p1Clear and p2 (2 feeds), establishing 2 frames
	if len(res.Facts.AudioTracks) != 1 || res.Facts.AudioTracks[0].Observed.Frames != 2 {
		t.Fatalf("expected 2 audio frames in Go reference, got %+v", res.Facts.AudioTracks)
	}
}

// 4. Video Counters: TEI with scrambling_control != 0 does not touch Clear, Scrambled, or ClearRun.
func TestTEI_Video_Counters_TEIWithScramblingControl_Invariants(t *testing.T) {
	b := vNew("test", "test", videoTSProgram)
	core := NewGoCore(videoTSProgram)
	ctx := context.Background()
	v := uint16(videoTSPID)

	psi := b.psi(0, h264Stream(v))

	// Packet 1: clear PUSI packet
	p0 := b.start(v, h264SPS, h264PPS, h264IDR)
	res1, err := core.Ingest(ctx, 0, cat(psi, p0))
	if err != nil {
		t.Fatalf("ingest p0: %v", err)
	}
	if res1.Facts.Scrambling.VideoClear != 1 || res1.Facts.Scrambling.VideoScrambled != 0 || res1.Facts.Scrambling.VideoClearRun != 1 {
		t.Fatalf("expected clear=1, scrambled=0, run=1; got clear=%d, scrambled=%d, run=%d",
			res1.Facts.Scrambling.VideoClear, res1.Facts.Scrambling.VideoScrambled, res1.Facts.Scrambling.VideoClearRun)
	}

	// Packet 2: continuation packet with BOTH TEI=1 AND scrambling_control = 0b10 (0x80 on byte 3)
	p1Corrupt := b.cont(v, []byte{0x00, 0x00, 0x01, 0x41, sliceI})
	p1Corrupt[1] |= 0x80 // TEI = 1
	p1Corrupt[3] |= 0x80 // scrambling_control = 0b10

	res2, err := core.Ingest(ctx, int64(len(psi)+len(p0)), p1Corrupt)
	if err != nil {
		t.Fatalf("ingest p1Corrupt: %v", err)
	}

	// Neither Clear nor Scrambled counters must increase; VideoClearRun must NOT be reset or incremented
	if res2.Facts.Scrambling.VideoClear != 1 {
		t.Fatalf("VideoClear must remain 1, got %d", res2.Facts.Scrambling.VideoClear)
	}
	if res2.Facts.Scrambling.VideoScrambled != 0 {
		t.Fatalf("VideoScrambled must remain 0, got %d", res2.Facts.Scrambling.VideoScrambled)
	}
	if res2.Facts.Scrambling.VideoClearRun != 1 {
		t.Fatalf("VideoClearRun must remain 1, got %d", res2.Facts.Scrambling.VideoClearRun)
	}
}

// 5. Audio Counters: TEI with scrambling_control != 0 does not touch Clear, Scrambled, or ClearRun.
func TestTEI_Audio_Counters_TEIWithScramblingControl_Invariants(t *testing.T) {
	b := audioTSNew("test", "test", audioTSProgram)
	core := NewGoCore(audioTSProgram)
	ctx := context.Background()

	psi := b.psi(0, ac3Stream(audioTSAudioA))
	start := pesStart(0xBD, 0, audioTSAC3Frame(audioTSByte6Stereo))
	p0 := audioTSPacket(audioTSAudioA, true, 0, start)

	res1, err := core.Ingest(ctx, 0, cat(append(psi, p0)...))
	if err != nil {
		t.Fatalf("ingest p0: %v", err)
	}
	if res1.Facts.Scrambling.AudioClear != 1 || res1.Facts.Scrambling.AudioScrambled != 0 || res1.Facts.Scrambling.AudioClearRun != 1 {
		t.Fatalf("expected clear=1, scrambled=0, run=1; got clear=%d, scrambled=%d, run=%d",
			res1.Facts.Scrambling.AudioClear, res1.Facts.Scrambling.AudioScrambled, res1.Facts.Scrambling.AudioClearRun)
	}

	// Packet 2: continuation packet with BOTH TEI=1 AND scrambling_control = 0b10
	c1 := audioTSPad(audioTSAC3Frame(audioTSByte6Stereo))
	p1Corrupt := audioTSPacket(audioTSAudioA, false, 1, c1)
	p1Corrupt[1] |= 0x80 // TEI = 1
	p1Corrupt[3] |= 0x80 // scrambling_control = 0b10

	res2, err := core.Ingest(ctx, int64(len(psi)*TSPacketSize+len(p0)), p1Corrupt)
	if err != nil {
		t.Fatalf("ingest p1Corrupt: %v", err)
	}

	// Invariants: neither counter changed, run unmodified
	if res2.Facts.Scrambling.AudioClear != 1 {
		t.Fatalf("AudioClear must remain 1, got %d", res2.Facts.Scrambling.AudioClear)
	}
	if res2.Facts.Scrambling.AudioScrambled != 0 {
		t.Fatalf("AudioScrambled must remain 0, got %d", res2.Facts.Scrambling.AudioScrambled)
	}
	if res2.Facts.Scrambling.AudioClearRun != 1 {
		t.Fatalf("AudioClearRun must remain 1, got %d", res2.Facts.Scrambling.AudioClearRun)
	}
}

// 6. Video PUSI: Clean prior AU is cleanly finalized and separated when next PUSI has TEI.
func TestTEI_Video_PUSI_CleanIDRFollowedByTEIPUSI_CleanlyFinalized(t *testing.T) {
	b := vNew("test", "test", videoTSProgram)
	core := NewGoCore(videoTSProgram)
	ctx := context.Background()
	v := uint16(videoTSPID)

	psi := b.psi(0, h264Stream(v))

	// AU 1: clean IDR packet
	p0 := b.start(v, h264SPS, h264PPS, h264IDR)
	p0Offset := int64(len(psi))

	res1, err := core.Ingest(ctx, 0, cat(psi, p0))
	if err != nil {
		t.Fatalf("ingest AU 1: %v", err)
	}
	if res1.Facts.CleanEntryPoints != 1 {
		t.Fatalf("expected CleanEntryPoints=1 for clean IDR, got %d", res1.Facts.CleanEntryPoints)
	}

	// AU 2: arrives with PUSI=1 and TEI=1, sequential CC
	p1TEIPUSI := withTEI(b.start(v, h264SPS, h264PPS, h264IDR))
	res2, err := core.Ingest(ctx, int64(len(psi)+len(p0)), p1TEIPUSI)
	if err != nil {
		t.Fatalf("ingest AU 2 TEI PUSI: %v", err)
	}

	// AU 1 must be cleanly finalized as a clean AU:
	if res2.Facts.CleanAccessUnits != 1 {
		t.Fatalf("expected CleanAccessUnits=1, got %d", res2.Facts.CleanAccessUnits)
	}
	// AU 1's RAP must NOT be invalidated:
	for _, ev := range res2.Events {
		if ev.Kind == EventRandomAccessPointInvalidated && ev.Offset == p0Offset {
			t.Fatalf("AU 1's RAP must not be invalidated by a subsequent TEI PUSI packet")
		}
	}
	if res2.Facts.CleanEntryPoints != 1 {
		t.Fatalf("expected CleanEntryPoints=1 retained from AU 1, got %d", res2.Facts.CleanEntryPoints)
	}
	// AU 2 is quarantined:
	if !core.videoAwaitingStart {
		t.Fatalf("expected videoAwaitingStart=true for AU 2")
	}
}

// 7. Video Continuation: Clean IDR followed by TEI continuation invalidates provisional RAP.
func TestTEI_Video_Continuation_IDRFollowedByTEIContinuation_InvalidatesRAP(t *testing.T) {
	b := vNew("test", "test", videoTSProgram)
	core := NewGoCore(videoTSProgram)
	ctx := context.Background()
	v := uint16(videoTSPID)

	psi := b.psi(0, h264Stream(v))

	// Packet 0: starts AU with provisional IDR RAP
	p0 := b.start(v, h264SPS, h264PPS, h264IDR)
	p0Offset := int64(len(psi))

	res1, err := core.Ingest(ctx, 0, cat(psi, p0))
	if err != nil {
		t.Fatalf("ingest p0: %v", err)
	}
	if res1.Facts.CleanEntryPoints != 1 {
		t.Fatalf("expected CleanEntryPoints=1 after p0")
	}

	// Packet 1: continuation of same AU with TEI=1
	p1TEI := withTEI(b.cont(v, []byte{0x00, 0x00, 0x01, 0x41, sliceI}))
	res2, err := core.Ingest(ctx, int64(len(psi)+len(p0)), p1TEI)
	if err != nil {
		t.Fatalf("ingest p1TEI: %v", err)
	}

	var gotInvalidated bool
	for _, ev := range res2.Events {
		if ev.Kind == EventRandomAccessPointInvalidated && ev.Offset == p0Offset {
			gotInvalidated = true
		}
	}
	if !gotInvalidated {
		t.Fatalf("expected EventRandomAccessPointInvalidated for provisional IDR RAP on TEI continuation")
	}
	if res2.Facts.CleanEntryPoints != 0 {
		t.Fatalf("expected CleanEntryPoints=0, got %d", res2.Facts.CleanEntryPoints)
	}
	if core.cleanRAPCount != 0 {
		t.Fatalf("expected cleanRAPCount=0, got %d", core.cleanRAPCount)
	}
	if !core.videoAwaitingStart {
		t.Fatalf("expected videoAwaitingStart=true after TEI continuation")
	}
}
