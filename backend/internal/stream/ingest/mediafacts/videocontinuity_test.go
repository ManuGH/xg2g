// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package mediafacts

import (
	"context"
	"testing"
)

// 1. CC wrap 15 -> 0 is strictly sequential and does not break AU continuity.
func TestVideoContinuity_Wrap15To0_Continuous(t *testing.T) {
	b := vNew("test", "test", videoTSProgram)
	core := NewGoCore(videoTSProgram)
	ctx := context.Background()
	v := uint16(videoTSPID)

	psi := b.psi(0, h264Stream(v))

	// Fast-forward CC counter to 14
	b.cc[v] = 14
	pkt14 := b.start(v, h264SPS, h264PPS)                      // CC = 14
	pkt15 := b.cont(v, []byte{0x00, 0x00, 0x01, 0x41, sliceI}) // CC = 15
	pkt0 := b.cont(v, []byte{0x84, 0x21, 0xA0, 0x33, 0xFF})    // CC = 0 (wrap)

	chunk := cat(psi, pkt14, pkt15, pkt0)
	res, err := core.Ingest(ctx, 0, chunk)
	if err != nil {
		t.Fatalf("ingest wrap 15->0: %v", err)
	}

	if core.auContinuityBroken {
		t.Fatalf("auContinuityBroken must be false across normal 15->0 wrap")
	}
	if core.videoAwaitingStart {
		t.Fatalf("videoAwaitingStart must be false across normal 15->0 wrap")
	}
	if res.Facts.Scrambling.VideoClear != 3 {
		t.Fatalf("expected 3 clear video packets, got %d", res.Facts.Scrambling.VideoClear)
	}
}

// 2. Exact duplicate packet dropped without corrupting sequential expectation.
func TestVideoContinuity_ExactDuplicate_DroppedWithoutCorruptingSequence(t *testing.T) {
	b := vNew("test", "test", videoTSProgram)
	core := NewGoCore(videoTSProgram)
	ctx := context.Background()
	v := uint16(videoTSPID)

	psi := b.psi(0, h264Stream(v))

	b.cc[v] = 0
	startPkt := b.start(v, h264SPS, h264PPS)   // CC = 0
	dupPkt := append([]byte(nil), startPkt...) // Exact duplicate CC = 0
	contPkt := b.cont(v, h264SliceP)           // CC = 1 (expected next)

	chunk := cat(psi, startPkt, dupPkt, contPkt)
	res, err := core.Ingest(ctx, 0, chunk)
	if err != nil {
		t.Fatalf("ingest with duplicate: %v", err)
	}

	// Exact duplicate was dropped; only startPkt and contPkt are counted as clear packets
	if res.Facts.Scrambling.VideoClear != 2 {
		t.Fatalf("expected 2 clear video packets (duplicate dropped), got %d", res.Facts.Scrambling.VideoClear)
	}
	if core.auContinuityBroken {
		t.Fatalf("exact duplicate must not trigger auContinuityBroken")
	}
}

// 3. Discontinuity Indicator (DI) bypasses gap: announced CC jump with DI flag
// set does not trigger gap recovery (DI hardening remains a separate defect).
func TestVideoContinuity_DiscontinuityIndicator_BypassesGap(t *testing.T) {
	b := vNew("test", "test", videoTSProgram)
	core := NewGoCore(videoTSProgram)
	ctx := context.Background()
	v := uint16(videoTSPID)

	psi := b.psi(0, h264Stream(v))

	b.cc[v] = 0
	startPkt := b.start(v, h264SPS, h264PPS) // CC = 0

	// CC jump from 0 to 5, but with DI set
	b.cc[v] = 5
	diPkt := audioTSShortPacketWithDI(v, false, b.next(v), h264IDR) // CC = 5 with DI

	chunk := cat(psi, startPkt, diPkt)
	_, err := core.Ingest(ctx, 0, chunk)
	if err != nil {
		t.Fatalf("ingest DI jump: %v", err)
	}

	// DI jump must NOT trigger unannounced gap recovery
	if core.auContinuityBroken {
		t.Fatalf("announced DI jump must not set auContinuityBroken")
	}
	if core.videoAwaitingStart {
		t.Fatalf("announced DI jump must not force videoAwaitingStart")
	}
}

// 4. Mid-AU CC gap between split slice header halves discards NAL capture,
// quarantines continuation packets, and emits no Intra RAP.
func TestVideoContinuity_MidAUSliceGap_CorruptedSliceNotJoined(t *testing.T) {
	b := vNew("test", "test", videoTSProgram)
	core := NewGoCore(videoTSProgram)
	ctx := context.Background()
	v := uint16(videoTSPID)

	psi := b.psi(0, h264Stream(v))

	// Packet 1: SPS, PPS, and first half of Intra slice header (0x41 + sliceI)
	first := b.start(v, h264SPS, h264PPS, []byte{0x00, 0x00, 0x01, 0x41, sliceI})

	// Skip a CC value (unannounced gap)
	b.next(v)

	// Packet 2: Continuation packet after gap with second half of slice bytes
	cont := b.cont(v, []byte{0x84, 0x21, 0xA0, 0x33, 0xFF, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66})

	chunk1 := cat(psi, first, cont)
	res1, err := core.Ingest(ctx, 0, chunk1)
	if err != nil {
		t.Fatalf("ingest chunk1: %v", err)
	}

	for _, ev := range res1.Events {
		if ev.Kind == EventRandomAccessPoint {
			t.Fatalf("unexpected RAP emitted during gap chunk")
		}
	}
	if !core.auContinuityBroken {
		t.Fatalf("expected auContinuityBroken=true after CC gap")
	}
	if !core.videoAwaitingStart {
		t.Fatalf("expected videoAwaitingStart=true after CC gap")
	}

	// Packet 3: Next clean PES start (P-slice)
	nextPES := b.start(v, h264SliceP)
	res2, err := core.Ingest(ctx, int64(len(chunk1)), nextPES)
	if err != nil {
		t.Fatalf("ingest nextPES: %v", err)
	}

	// Corrupted previous AU must NOT have emitted a RAP upon finalization
	for _, ev := range res2.Events {
		if ev.Kind == EventRandomAccessPoint {
			t.Fatalf("corrupted slice AU must NOT emit a RandomAccessPoint")
		}
	}
	if res2.Facts.RandomAccess.IntraPoints != 0 {
		t.Fatalf("expected IntraPoints=0, got %d", res2.Facts.RandomAccess.IntraPoints)
	}
	if res2.Facts.CleanEntryPoints != 0 {
		t.Fatalf("expected CleanEntryPoints=0, got %d", res2.Facts.CleanEntryPoints)
	}
}

// 5. Gap on PUSI=1 closes prior deferred AU as broken and begins new AU cleanly.
// Note: Per review, restricted to deferred RAPs (e.g. Non-IDR Intra).
func TestVideoContinuity_GapOnPUSI_ClosesPriorDeferredAUAsBrokenAndStartsNewPES(t *testing.T) {
	b := vNew("test", "test", videoTSProgram)
	core := NewGoCore(videoTSProgram)
	ctx := context.Background()
	v := uint16(videoTSPID)

	psi := b.psi(0, h264Stream(v))

	// AU 1: SPS, PPS and complete Intra slice (deferred evaluation, not IDR)
	pes1 := b.start(v, h264SPS, h264PPS, h264SliceI)
	chunk1 := cat(psi, pes1)
	res1, err := core.Ingest(ctx, 0, chunk1)
	if err != nil {
		t.Fatalf("ingest chunk1: %v", err)
	}
	// Non-IDR Intra is deferred until AU finalization; no RAP in chunk1
	for _, ev := range res1.Events {
		if ev.Kind == EventRandomAccessPoint {
			t.Fatalf("deferred Intra slice must not emit RAP before AU closes")
		}
	}

	// Unannounced CC gap right before the next PUSI
	b.next(v) // skip CC

	// AU 2: Next valid PES begins at PUSI with CC gap from AU 1
	pes2 := b.start(v, h264SliceP)
	res2, err := core.Ingest(ctx, int64(len(chunk1)), pes2)
	if err != nil {
		t.Fatalf("ingest chunk2: %v", err)
	}

	// Prior AU 1 was closed with auContinuityBroken=true, so it must NOT emit RAP
	for _, ev := range res2.Events {
		if ev.Kind == EventRandomAccessPoint {
			t.Fatalf("prior AU broken by trailing CC gap must not emit RAP on finalization")
		}
	}

	// AU 2 must begin cleanly: auContinuityBroken reset, videoAwaitingStart false
	if core.auContinuityBroken {
		t.Fatalf("new valid PES must reset auContinuityBroken to false")
	}
	if core.videoAwaitingStart {
		t.Fatalf("new valid PES must reset videoAwaitingStart to false")
	}
}

// 6. CleanAccessUnits is incremented on clean unencrypted payload with CC gap
// (descrambling observation).
func TestVideoContinuity_CleanAccessUnitsIncrementedOnClearAUWithGap(t *testing.T) {
	b := vNew("test", "test", videoTSProgram)
	core := NewGoCore(videoTSProgram)
	ctx := context.Background()
	v := uint16(videoTSPID)

	psi := b.psi(0, h264Stream(v))

	// AU 1: Valid PES start + CC gap continuation
	first := b.start(v, h264SPS, h264PPS, []byte{0x00, 0x00, 0x01, 0x41, sliceI})
	b.next(v) // skip CC
	cont := b.cont(v, []byte{0x84, 0x21})

	chunk1 := cat(psi, first, cont)
	_, err := core.Ingest(ctx, 0, chunk1)
	if err != nil {
		t.Fatalf("ingest chunk1: %v", err)
	}

	// AU 2: Closes AU 1
	nextPES := b.start(v, h264SliceP)
	res2, err := core.Ingest(ctx, int64(len(chunk1)), nextPES)
	if err != nil {
		t.Fatalf("ingest chunk2: %v", err)
	}

	// Unscrambled payload was seen, so descrambling observation CleanAccessUnits increments
	if res2.Facts.CleanAccessUnits != 1 {
		t.Fatalf("expected CleanAccessUnits=1 (descrambling observation), got %d", res2.Facts.CleanAccessUnits)
	}
	// But entry points must be 0
	if res2.Facts.CleanEntryPoints != 0 {
		t.Fatalf("expected CleanEntryPoints=0, got %d", res2.Facts.CleanEntryPoints)
	}
}

// 7. Cross-call regression test: Intra slice start, later CC gap, and next PES
// all across separate Ingest() calls.
func TestVideoContinuity_IntraAndLaterGapAcrossSeparateIngestCalls(t *testing.T) {
	b := vNew("test", "test", videoTSProgram)
	core := NewGoCore(videoTSProgram)
	ctx := context.Background()
	v := uint16(videoTSPID)

	psi := b.psi(0, h264Stream(v))

	// Ingest #1: PSI + PES start carrying SPS, PPS, and first half of Intra slice
	first := b.start(v, h264SPS, h264PPS, []byte{0x00, 0x00, 0x01, 0x41, sliceI})
	chunk1 := cat(psi, first)
	res1, err := core.Ingest(ctx, 0, chunk1)
	if err != nil {
		t.Fatalf("ingest #1: %v", err)
	}
	for _, ev := range res1.Events {
		if ev.Kind == EventRandomAccessPoint {
			t.Fatalf("Ingest #1 must not emit RAP (deferred intra slice)")
		}
	}
	if !res1.Facts.ParameterSetsSeen {
		t.Fatalf("expected ParameterSetsSeen=true after SPS/PPS in Ingest #1")
	}

	// Ingest #2: Continuation packet with unannounced CC gap carrying second half of slice
	b.next(v) // skip CC counter
	cont := b.cont(v, []byte{0x84, 0x21, 0xA0, 0x33, 0xFF, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66})
	res2, err := core.Ingest(ctx, int64(len(chunk1)), cont)
	if err != nil {
		t.Fatalf("ingest #2: %v", err)
	}
	for _, ev := range res2.Events {
		if ev.Kind == EventRandomAccessPoint {
			t.Fatalf("Ingest #2 must not emit RAP on corrupted slice")
		}
	}
	if !core.auContinuityBroken {
		t.Fatalf("expected auContinuityBroken=true after Ingest #2")
	}
	if !core.videoAwaitingStart {
		t.Fatalf("expected videoAwaitingStart=true after Ingest #2")
	}

	// Ingest #3: Next clean PES start (P slice)
	nextPES := b.start(v, h264SliceP)
	res3, err := core.Ingest(ctx, int64(len(chunk1)+len(cont)), nextPES)
	if err != nil {
		t.Fatalf("ingest #3: %v", err)
	}
	for _, ev := range res3.Events {
		if ev.Kind == EventRandomAccessPoint {
			t.Fatalf("Ingest #3 must not emit RAP for corrupted prior AU")
		}
	}
	if res3.Facts.RandomAccess.IntraPoints != 0 {
		t.Fatalf("expected IntraPoints=0, got %d", res3.Facts.RandomAccess.IntraPoints)
	}
	if res3.Facts.CleanEntryPoints != 0 {
		t.Fatalf("expected CleanEntryPoints=0, got %d", res3.Facts.CleanEntryPoints)
	}
	if res3.Facts.CleanAccessUnits != 1 {
		t.Fatalf("expected CleanAccessUnits=1, got %d", res3.Facts.CleanAccessUnits)
	}
}

// 8. Audio PID isolation: video CC gaps do not impact audio CC tracking.
func TestVideoContinuity_AudioPIDIsolation(t *testing.T) {
	b := vNew("test", "test", videoTSProgram)
	core := NewGoCore(videoTSProgram)
	ctx := context.Background()
	v := uint16(videoTSPID)
	a := uint16(videoTSAudio)

	psi := b.psi(0, h264Stream(v), audioTSEs{streamType: 0x03, pid: a})

	// Audio packet at CC 0
	b.cc[a] = 0
	aPkt0 := audioTSPacket(a, true, b.next(a), audioTSPad([]byte{0x00, 0x00, 0x01, 0xC0, 0x00, 0x04, 0x80, 0x00, 0x00, 0xFF}))

	// Video packet with a CC gap
	b.cc[v] = 0
	vPkt0 := b.start(v, h264SPS, h264PPS)
	b.next(v)                      // skip video CC 1
	vPkt2 := b.cont(v, h264SliceP) // video CC 2 (gap)

	// Audio packet at CC 1 (expected next for audio)
	aPkt1 := audioTSPacket(a, false, b.next(a), audioTSPad([]byte{0x11, 0x22, 0x33}))

	chunk := cat(psi, aPkt0, vPkt0, vPkt2, aPkt1)
	res, err := core.Ingest(ctx, 0, chunk)
	if err != nil {
		t.Fatalf("ingest mixed: %v", err)
	}

	// Audio packets are both sequential and clear
	if res.Facts.Scrambling.AudioClear != 2 {
		t.Fatalf("expected 2 clear audio packets, got %d", res.Facts.Scrambling.AudioClear)
	}
}
