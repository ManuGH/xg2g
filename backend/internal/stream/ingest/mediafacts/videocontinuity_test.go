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

// 9. Mid-AU same-CC-different continuation packet corrupts in-flight AU,
// aborts NAL capture, sets auContinuityBroken and videoAwaitingStart, and prevents RAP.
func TestVideoContinuity_SameCCDifferent_MidAUSliceCorrupted_QuarantinedAndNoRAP(t *testing.T) {
	b := vNew("test", "test", videoTSProgram)
	core := NewGoCore(videoTSProgram)
	ctx := context.Background()
	v := uint16(videoTSPID)

	psi := b.psi(0, h264Stream(v))

	// Packet 1: SPS, PPS, first half of slice header (CC = 0)
	b.cc[v] = 0
	p0 := b.start(v, h264SPS, h264PPS, []byte{0x00, 0x00, 0x01, 0x41, sliceI})

	// Packet 2: Continuation packet with second half of slice (CC = 1)
	p1 := b.cont(v, []byte{0x84, 0x21, 0xA0, 0x33})

	// Packet 3: Same CC = 1 as p1, but DIFFERENT payload bytes
	p1Diff := audioTSShortPacket(v, false, 1, []byte{0xFF, 0xEE, 0xDD, 0xCC})

	chunk1 := cat(psi, p0, p1, p1Diff)
	res1, err := core.Ingest(ctx, 0, chunk1)
	if err != nil {
		t.Fatalf("ingest chunk1: %v", err)
	}

	for _, ev := range res1.Events {
		if ev.Kind == EventRandomAccessPoint {
			t.Fatalf("unexpected RAP emitted during same-CC-different corrupted chunk")
		}
	}
	if !core.auContinuityBroken {
		t.Fatalf("expected auContinuityBroken=true after same-CC-different packet")
	}
	if !core.videoAwaitingStart {
		t.Fatalf("expected videoAwaitingStart=true after same-CC-different packet")
	}
	// Physical clear packets received: p0, p1, and p1Diff (all 3 clear)
	if res1.Facts.Scrambling.VideoClear != 3 {
		t.Fatalf("expected 3 clear video packets, got %d", res1.Facts.Scrambling.VideoClear)
	}

	// Packet 4: Next clean PES start (P-slice, CC = 2)
	b.cc[v] = 2 // b.next(v) will return 2
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
	if core.auContinuityBroken {
		t.Fatalf("expected auContinuityBroken=false on new clean PES")
	}
	if core.videoAwaitingStart {
		t.Fatalf("expected videoAwaitingStart=false on new clean PES")
	}
}

// 10. Mid-AU same-CC-different packet on in-flight IDR AU invalidates provisional RAP
// and decrements CleanEntryPoints.
func TestVideoContinuity_SameCCDifferent_IDRFollowedBySameCCDifferentTriggersInvalidation(t *testing.T) {
	b := vNew("test", "test", videoTSProgram)
	core := NewGoCore(videoTSProgram)
	ctx := context.Background()
	v := uint16(videoTSPID)

	psi := b.psi(0, h264Stream(v))
	b.cc[v] = 0
	idrPkt := b.start(v, h264SPS, h264PPS, h264IDR) // CC = 0
	idrOffset := int64(len(psi))

	p1 := b.cont(v, []byte{0x84, 0x21})                                       // CC = 1
	p1Diff := audioTSShortPacket(v, false, 1, []byte{0x99, 0x88, 0x77, 0x66}) // CC = 1 (different bytes)

	chunk := cat(psi, idrPkt, p1, p1Diff)
	res, err := core.Ingest(ctx, 0, chunk)
	if err != nil {
		t.Fatalf("ingest chunk: %v", err)
	}

	var gotRAP, gotInvalidated bool
	for _, ev := range res.Events {
		if ev.Kind == EventRandomAccessPoint {
			gotRAP = true
		}
		if ev.Kind == EventRandomAccessPointInvalidated {
			if ev.Offset != idrOffset {
				t.Fatalf("expected invalidation at %d, got %+v", idrOffset, ev)
			}
			gotInvalidated = true
		}
	}
	if !gotRAP {
		t.Fatalf("expected initial EventRandomAccessPoint for clear IDR")
	}
	if !gotInvalidated {
		t.Fatalf("expected EventRandomAccessPointInvalidated after same-CC-different packet")
	}
	if !core.auContinuityBroken {
		t.Fatalf("expected auContinuityBroken=true")
	}
	if !core.videoAwaitingStart {
		t.Fatalf("expected videoAwaitingStart=true")
	}
	if core.cleanRAPCount != 0 {
		t.Fatalf("expected cleanRAPCount=0, got %d", core.cleanRAPCount)
	}
	if res.Facts.CleanEntryPoints != 0 {
		t.Fatalf("expected CleanEntryPoints=0 after invalidation, got %d", res.Facts.CleanEntryPoints)
	}
	if res.Facts.Scrambling.VideoClear != 3 {
		t.Fatalf("expected 3 clear video packets, got %d", res.Facts.Scrambling.VideoClear)
	}
}

// 11. Same-CC-different packet detected across separate Ingest calls.
func TestVideoContinuity_SameCCDifferent_AcrossSeparateIngestCalls(t *testing.T) {
	b := vNew("test", "test", videoTSProgram)
	core := NewGoCore(videoTSProgram)
	ctx := context.Background()
	v := uint16(videoTSPID)

	psi := b.psi(0, h264Stream(v))
	b.cc[v] = 0
	idrPkt := b.start(v, h264SPS, h264PPS, h264IDR) // CC = 0
	idrOffset := int64(len(psi))

	// Ingest Call 1: Establish IDR RAP
	res1, err := core.Ingest(ctx, 0, cat(psi, idrPkt))
	if err != nil {
		t.Fatalf("ingest 1: %v", err)
	}
	if res1.Facts.CleanEntryPoints != 1 {
		t.Fatalf("expected CleanEntryPoints=1 after call 1")
	}

	// Ingest Call 2: Normal continuation CC = 1
	p1 := b.cont(v, []byte{0x84, 0x21})
	res2, err := core.Ingest(ctx, int64(len(psi)+len(idrPkt)), p1)
	if err != nil {
		t.Fatalf("ingest 2: %v", err)
	}
	if res2.Facts.CleanEntryPoints != 1 {
		t.Fatalf("expected CleanEntryPoints=1 after call 2")
	}

	// Ingest Call 3: Same CC = 1, different bytes
	p1Diff := audioTSShortPacket(v, false, 1, []byte{0xDE, 0xAD, 0xBE, 0xEF})
	res3, err := core.Ingest(ctx, int64(len(psi)+len(idrPkt)+len(p1)), p1Diff)
	if err != nil {
		t.Fatalf("ingest 3: %v", err)
	}

	var gotInvalidated bool
	for _, ev := range res3.Events {
		if ev.Kind == EventRandomAccessPointInvalidated {
			if ev.Offset != idrOffset {
				t.Fatalf("expected invalidation at %d, got %+v", idrOffset, ev)
			}
			gotInvalidated = true
		}
	}
	if !gotInvalidated {
		t.Fatalf("expected EventRandomAccessPointInvalidated in call 3")
	}
	if res3.Facts.CleanEntryPoints != 0 {
		t.Fatalf("expected CleanEntryPoints=0 after call 3, got %d", res3.Facts.CleanEntryPoints)
	}
	if !core.videoAwaitingStart {
		t.Fatalf("expected videoAwaitingStart=true after call 3")
	}
}

// 12. PUSI=1 with same-CC-different invalidates prior in-flight published IDR RAP.
func TestVideoContinuity_SameCCDifferent_OnPUSI_InvalidatesPriorPublishedIDR(t *testing.T) {
	b := vNew("test", "test", videoTSProgram)
	core := NewGoCore(videoTSProgram)
	ctx := context.Background()
	v := uint16(videoTSPID)

	psi := b.psi(0, h264Stream(v))
	b.cc[v] = 0
	idrPkt := b.start(v, h264SPS, h264PPS, h264IDR) // CC = 0
	idrOffset := int64(len(psi))

	p1 := b.cont(v, []byte{0x84, 0x21}) // CC = 1

	// Ingest AU 1 (provisional IDR emitted)
	res1, err := core.Ingest(ctx, 0, cat(psi, idrPkt, p1))
	if err != nil {
		t.Fatalf("ingest AU 1: %v", err)
	}
	if res1.Facts.CleanEntryPoints != 1 {
		t.Fatalf("expected CleanEntryPoints=1 for initial IDR")
	}

	// Conflicting PUSI arrives with CC = 1 (same CC as p1!), carrying new PES header
	pusiDiff := audioTSShortPacket(v, true, 1, cat(audioTSPESHeader(0xE0, 0), h264SliceP))
	res2, err := core.Ingest(ctx, int64(len(psi)+len(idrPkt)+len(p1)), pusiDiff)
	if err != nil {
		t.Fatalf("ingest conflicting PUSI: %v", err)
	}

	var gotInvalidated bool
	for _, ev := range res2.Events {
		if ev.Kind == EventRandomAccessPointInvalidated {
			if ev.Offset != idrOffset {
				t.Fatalf("expected invalidation at %d, got %+v", idrOffset, ev)
			}
			gotInvalidated = true
		}
	}
	if !gotInvalidated {
		t.Fatalf("expected EventRandomAccessPointInvalidated when PUSI arrives with same-CC-different")
	}
	if res2.Facts.CleanEntryPoints != 0 {
		t.Fatalf("expected CleanEntryPoints=0 after invalidation, got %d", res2.Facts.CleanEntryPoints)
	}
	if !core.videoAwaitingStart {
		t.Fatalf("expected videoAwaitingStart=true (conflicting PUSI quarantined)")
	}
}

// 13. PUSI=1 with same-CC-different does not parse conflicting payload:
// Even if the conflicting PUSI carries valid SPS/PPS/IDR syntax, no RAP or facts are emitted.
func TestVideoContinuity_SameCCDifferent_OnPUSI_DoesNotParseConflictingPayload(t *testing.T) {
	b := vNew("test", "test", videoTSProgram)
	core := NewGoCore(videoTSProgram)
	ctx := context.Background()
	v := uint16(videoTSPID)

	psi := b.psi(0, h264Stream(v))

	// Initial clean PES start (P-slice, CC = 0)
	b.cc[v] = 0
	p0 := b.start(v, h264SliceP)

	chunk1 := cat(psi, p0)
	_, err := core.Ingest(ctx, 0, chunk1)
	if err != nil {
		t.Fatalf("ingest chunk1: %v", err)
	}

	// Conflicting PUSI arrives with CC = 0 (same CC as p0!) carrying SPS/PPS/IDR
	conflictingPUSI := audioTSShortPacket(v, true, 0, cat(audioTSPESHeader(0xE0, 0), h264SPS, h264PPS, h264IDR))
	res2, err := core.Ingest(ctx, int64(len(chunk1)), conflictingPUSI)
	if err != nil {
		t.Fatalf("ingest conflicting PUSI: %v", err)
	}

	// Must NOT emit RAP or establish parameter sets from the quarantined PUSI
	for _, ev := range res2.Events {
		if ev.Kind == EventRandomAccessPoint {
			t.Fatalf("conflicting same-CC PUSI must NOT emit RandomAccessPoint")
		}
	}
	if res2.Facts.RandomAccess.IntraPoints != 0 {
		t.Fatalf("expected IntraPoints=0, got %d", res2.Facts.RandomAccess.IntraPoints)
	}
	if res2.Facts.CleanEntryPoints != 0 {
		t.Fatalf("expected CleanEntryPoints=0, got %d", res2.Facts.CleanEntryPoints)
	}
	if res2.Facts.ParameterSetsSeen {
		t.Fatalf("quarantined PUSI must NOT admit parameter sets")
	}
	if core.pesHasSPS || core.pesHasPPS {
		t.Fatalf("quarantined PUSI must NOT set pesHasSPS or pesHasPPS")
	}
	if !core.videoAwaitingStart {
		t.Fatalf("expected videoAwaitingStart=true (quarantined)")
	}
	// Physical reception: counted as clear packets (p0 and conflictingPUSI)
	if res2.Facts.Scrambling.VideoClear != 2 {
		t.Fatalf("expected 2 clear video packets, got %d", res2.Facts.Scrambling.VideoClear)
	}
}

// 14. PUSI=1 with same-CC-different recovers at next trusted PUSI:
// Sequence: clean A -> conflicting same-CC B (quarantined) -> quarantined continuation -> clean C (recovers & establishes RAP).
func TestVideoContinuity_SameCCDifferent_OnPUSI_RecoversAtNextTrustedPUSI(t *testing.T) {
	b := vNew("test", "test", videoTSProgram)
	core := NewGoCore(videoTSProgram)
	ctx := context.Background()
	v := uint16(videoTSPID)

	psi := b.psi(0, h264Stream(v))

	// PUSI A: Clean PES start (P-slice, CC = 0)
	b.cc[v] = 0
	pesA := b.start(v, h264SliceP)

	chunk1 := cat(psi, pesA)
	_, err := core.Ingest(ctx, 0, chunk1)
	if err != nil {
		t.Fatalf("ingest chunk1: %v", err)
	}

	// Conflicting PUSI B: Same CC = 0, different bytes (carrying IDR).
	// Must be rejected and quarantined.
	pesB := audioTSShortPacket(v, true, 0, cat(audioTSPESHeader(0xE0, 0), h264SPS, h264PPS, h264IDR))
	resB, err := core.Ingest(ctx, int64(len(chunk1)), pesB)
	if err != nil {
		t.Fatalf("ingest pesB: %v", err)
	}
	for _, ev := range resB.Events {
		if ev.Kind == EventRandomAccessPoint {
			t.Fatalf("quarantined pesB must not emit RAP")
		}
	}
	if !core.videoAwaitingStart {
		t.Fatalf("expected videoAwaitingStart=true after pesB")
	}

	// Continuation packet under quarantine (CC = 1):
	// Note: tracker classified pesB as reference at CC = 0, so next sequential is CC = 1.
	contBad := audioTSShortPacket(v, false, 1, []byte{0x11, 0x22, 0x33})
	resCont, err := core.Ingest(ctx, int64(len(chunk1)+len(pesB)), contBad)
	if err != nil {
		t.Fatalf("ingest contBad: %v", err)
	}
	if !core.videoAwaitingStart {
		t.Fatalf("continuation during quarantine must maintain videoAwaitingStart=true")
	}
	for _, ev := range resCont.Events {
		if ev.Kind == EventRandomAccessPoint {
			t.Fatalf("quarantined continuation must not emit RAP")
		}
	}

	// PUSI C: Clean PES start at next sequential CC = 2, carrying SPS, PPS, IDR.
	pesC := audioTSShortPacket(v, true, 2, cat(audioTSPESHeader(0xE0, 0), h264SPS, h264PPS, h264IDR))
	pesCOffset := int64(len(chunk1) + len(pesB) + len(contBad))
	resC, err := core.Ingest(ctx, pesCOffset, pesC)
	if err != nil {
		t.Fatalf("ingest pesC: %v", err)
	}

	if core.videoAwaitingStart {
		t.Fatalf("trusted PUSI C must exit quarantine (videoAwaitingStart=false)")
	}
	var gotRAPAtC bool
	for _, ev := range resC.Events {
		if ev.Kind == EventRandomAccessPoint && ev.Offset == pesCOffset {
			gotRAPAtC = true
		}
	}
	if !gotRAPAtC {
		t.Fatalf("expected EventRandomAccessPoint at trusted PUSI C offset %d", pesCOffset)
	}
	if resC.Facts.CleanEntryPoints != 1 {
		t.Fatalf("expected CleanEntryPoints=1 after recovery at PUSI C, got %d", resC.Facts.CleanEntryPoints)
	}
	if !resC.Facts.ParameterSetsSeen {
		t.Fatalf("expected ParameterSetsSeen to be established by trusted PUSI C")
	}
}

// 15. PUSI=1 with same-CC-different closes prior deferred Non-IDR AU as broken.
func TestVideoContinuity_SameCCDifferent_OnPUSI_ClosesPriorDeferredAUAsBroken(t *testing.T) {
	b := vNew("test", "test", videoTSProgram)
	core := NewGoCore(videoTSProgram)
	ctx := context.Background()
	v := uint16(videoTSPID)

	psi := b.psi(0, h264Stream(v))

	// AU 1: SPS, PPS and complete Non-IDR Intra slice (CC = 0)
	b.cc[v] = 0
	pes1 := b.start(v, h264SPS, h264PPS, h264SliceI)
	chunk1 := cat(psi, pes1)
	res1, err := core.Ingest(ctx, 0, chunk1)
	if err != nil {
		t.Fatalf("ingest chunk1: %v", err)
	}
	for _, ev := range res1.Events {
		if ev.Kind == EventRandomAccessPoint {
			t.Fatalf("deferred Intra slice must not emit RAP before AU closes")
		}
	}

	// AU 2 arrives at PUSI with CC = 0 (same CC as pes1, different bytes)
	pes2 := audioTSShortPacket(v, true, 0, cat(audioTSPESHeader(0xE0, 0), h264SliceP))
	res2, err := core.Ingest(ctx, int64(len(chunk1)), pes2)
	if err != nil {
		t.Fatalf("ingest chunk2: %v", err)
	}

	// Prior AU 1 must NOT emit RAP on finalization
	for _, ev := range res2.Events {
		if ev.Kind == EventRandomAccessPoint {
			t.Fatalf("prior AU broken by trailing same-CC conflict must not emit RAP")
		}
	}
	if res2.Facts.RandomAccess.IntraPoints != 0 {
		t.Fatalf("expected IntraPoints=0, got %d", res2.Facts.RandomAccess.IntraPoints)
	}
	if res2.Facts.CleanEntryPoints != 0 {
		t.Fatalf("expected CleanEntryPoints=0, got %d", res2.Facts.CleanEntryPoints)
	}
	if !core.videoAwaitingStart {
		t.Fatalf("expected videoAwaitingStart=true after conflicting PUSI")
	}
}

// 16. Exact duplicate vs same-CC-different distinction:
// Exact duplicate (identical bytes) is dropped without side-effects or incrementing clear count,
// whereas same-CC-different increments clearVideoPackets and corrupts in-flight AU.
func TestVideoContinuity_SameCC_ExactDuplicateVsDifferentDistinction(t *testing.T) {
	b := vNew("test", "test", videoTSProgram)
	core := NewGoCore(videoTSProgram)
	ctx := context.Background()
	v := uint16(videoTSPID)

	psi := b.psi(0, h264Stream(v))

	// Initial packet CC = 4
	b.cc[v] = 4
	p0 := b.start(v, h264SPS, h264PPS)

	// Ingest p0
	res1, err := core.Ingest(ctx, 0, cat(psi, p0))
	if err != nil {
		t.Fatalf("ingest p0: %v", err)
	}
	if res1.Facts.Scrambling.VideoClear != 1 {
		t.Fatalf("expected VideoClear=1")
	}

	// Ingest exact duplicate of p0 (identical bytes, CC = 4)
	p0Dup := append([]byte(nil), p0...)
	res2, err := core.Ingest(ctx, int64(len(psi)+len(p0)), p0Dup)
	if err != nil {
		t.Fatalf("ingest p0Dup: %v", err)
	}
	// Exact duplicate dropped: VideoClear remains 1, no continuity broken
	if res2.Facts.Scrambling.VideoClear != 1 {
		t.Fatalf("exact duplicate must NOT increment VideoClear, got %d", res2.Facts.Scrambling.VideoClear)
	}
	if core.auContinuityBroken {
		t.Fatalf("exact duplicate must not set auContinuityBroken")
	}

	// Ingest same CC = 4 with different bytes
	p0Diff := audioTSShortPacket(v, false, 4, []byte{0xAA, 0xBB, 0xCC, 0xDD})
	res3, err := core.Ingest(ctx, int64(len(psi)+len(p0)+len(p0Dup)), p0Diff)
	if err != nil {
		t.Fatalf("ingest p0Diff: %v", err)
	}
	// Same-CC-different is physically clear, so VideoClear becomes 2
	if res3.Facts.Scrambling.VideoClear != 2 {
		t.Fatalf("same-CC-different must increment VideoClear to 2, got %d", res3.Facts.Scrambling.VideoClear)
	}
	// But continuity is broken and quarantined
	if !core.auContinuityBroken {
		t.Fatalf("same-CC-different must set auContinuityBroken")
	}
	if !core.videoAwaitingStart {
		t.Fatalf("same-CC-different must set videoAwaitingStart")
	}
}

