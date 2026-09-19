// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package mediafacts

import (
	"context"
	"testing"
)

// 1. IDR followed by scrambled packet in the same chunk emits a provisional RAP
// and immediately invalidates it, keeping CleanEntryPoints at zero.
func TestVideoScrambled_IDRFollowedByScrambledInSameChunk(t *testing.T) {
	b := vNew("test", "test", videoTSProgram)
	core := NewGoCore(videoTSProgram)
	ctx := context.Background()
	v := uint16(videoTSPID)

	psi := b.psi(0, h264Stream(v))
	idrPkt := b.start(v, h264SPS, h264PPS, h264IDR)
	scrPkt := scrambled(b.cont(v, []byte{0xAA, 0xBB}))

	chunk := cat(psi, idrPkt, scrPkt)
	res, err := core.Ingest(ctx, 0, chunk)
	if err != nil {
		t.Fatalf("ingest chunk: %v", err)
	}

	idrOffset := int64(len(psi))
	var gotRAP, gotInvalidated bool
	for _, ev := range res.Events {
		switch ev.Kind {
		case EventRandomAccessPoint:
			if ev.Offset != idrOffset || !ev.Joinable {
				t.Fatalf("expected Joinable RAP at offset %d, got %+v", idrOffset, ev)
			}
			gotRAP = true
		case EventRandomAccessPointInvalidated:
			if ev.Offset != idrOffset {
				t.Fatalf("expected RAP invalidated at offset %d, got %+v", idrOffset, ev)
			}
			gotInvalidated = true
		}
	}

	if !gotRAP {
		t.Fatalf("expected EventRandomAccessPoint in events, got: %+v", res.Events)
	}
	if !gotInvalidated {
		t.Fatalf("expected EventRandomAccessPointInvalidated in events, got: %+v", res.Events)
	}

	if res.Facts.CleanEntryPoints != 0 {
		t.Fatalf("expected CleanEntryPoints=0 after scrambled packet, got %d", res.Facts.CleanEntryPoints)
	}
	if res.Facts.RandomAccess.IRAPPoints != 1 {
		t.Fatalf("expected IRAPPoints=1, got %d", res.Facts.RandomAccess.IRAPPoints)
	}
	if res.Facts.Scrambling.VideoScrambled != 1 {
		t.Fatalf("expected VideoScrambled=1, got %d", res.Facts.Scrambling.VideoScrambled)
	}
	if res.Facts.Scrambling.VideoClear != 1 {
		t.Fatalf("expected VideoClear=1, got %d", res.Facts.Scrambling.VideoClear)
	}

	// Next clean PES start finishes AU
	nextPES := b.start(v, h264SliceP)
	res2, err := core.Ingest(ctx, int64(len(chunk)), nextPES)
	if err != nil {
		t.Fatalf("ingest nextPES: %v", err)
	}
	for _, ev := range res2.Events {
		if ev.Kind == EventRandomAccessPoint || ev.Kind == EventRandomAccessPointInvalidated {
			t.Fatalf("unexpected RAP event on next PES: %+v", ev)
		}
	}
	if res2.Facts.CleanEntryPoints != 0 {
		t.Fatalf("CleanEntryPoints must remain 0, got %d", res2.Facts.CleanEntryPoints)
	}
}

// 2. Cross-Ingest: IDR published in Ingest #1, scrambled packet in Ingest #2.
// The provisional RAP is invalidated across the call boundary and CleanEntryPoints reverts.
func TestVideoScrambled_IDRFollowedByScrambledAcrossSeparateIngestCalls(t *testing.T) {
	b := vNew("test", "test", videoTSProgram)
	core := NewGoCore(videoTSProgram)
	ctx := context.Background()
	v := uint16(videoTSPID)

	psi := b.psi(0, h264Stream(v))
	idrPkt := b.start(v, h264SPS, h264PPS, h264IDR)
	chunk1 := cat(psi, idrPkt)

	res1, err := core.Ingest(ctx, 0, chunk1)
	if err != nil {
		t.Fatalf("ingest chunk1: %v", err)
	}

	idrOffset := int64(len(psi))
	var gotRAP bool
	for _, ev := range res1.Events {
		if ev.Kind == EventRandomAccessPoint {
			if ev.Offset != idrOffset || !ev.Joinable {
				t.Fatalf("expected Joinable RAP at offset %d, got %+v", idrOffset, ev)
			}
			gotRAP = true
		}
	}
	if !gotRAP {
		t.Fatalf("expected EventRandomAccessPoint in Ingest #1")
	}
	if res1.Facts.CleanEntryPoints != 1 {
		t.Fatalf("expected CleanEntryPoints=1 after clear IDR header, got %d", res1.Facts.CleanEntryPoints)
	}

	// Ingest #2: Continuation packet in the same AU is scrambled
	scrPkt := scrambled(b.cont(v, []byte{0xAA, 0xBB}))
	res2, err := core.Ingest(ctx, int64(len(chunk1)), scrPkt)
	if err != nil {
		t.Fatalf("ingest chunk2: %v", err)
	}

	var gotInvalidated bool
	for _, ev := range res2.Events {
		if ev.Kind == EventRandomAccessPointInvalidated {
			if ev.Offset != idrOffset {
				t.Fatalf("expected invalidation of offset %d, got %+v", idrOffset, ev)
			}
			gotInvalidated = true
		}
	}
	if !gotInvalidated {
		t.Fatalf("expected EventRandomAccessPointInvalidated in Ingest #2, got %+v", res2.Events)
	}
	if res2.Facts.CleanEntryPoints != 0 {
		t.Fatalf("expected CleanEntryPoints=0 after scrambled packet in Ingest #2, got %d", res2.Facts.CleanEntryPoints)
	}

	// Ingest #3: Next clean PES start begins cleanly
	nextPES := b.start(v, h264SliceP)
	res3, err := core.Ingest(ctx, int64(len(chunk1)+len(scrPkt)), nextPES)
	if err != nil {
		t.Fatalf("ingest chunk3: %v", err)
	}
	if res3.Facts.CleanEntryPoints != 0 {
		t.Fatalf("expected CleanEntryPoints=0 in Ingest #3, got %d", res3.Facts.CleanEntryPoints)
	}
}

// 3. Multiple scrambled packets in the same AU trigger invalidation exactly once
// and decrement cleanRAPCount only once (no underflow).
func TestVideoScrambled_MultipleScrambledPacketsInSameAU(t *testing.T) {
	b := vNew("test", "test", videoTSProgram)
	core := NewGoCore(videoTSProgram)
	ctx := context.Background()
	v := uint16(videoTSPID)

	psi := b.psi(0, h264Stream(v))
	idrPkt := b.start(v, h264SPS, h264PPS, h264IDR)
	scrPkt1 := scrambled(b.cont(v, []byte{0x11, 0x22}))
	scrPkt2 := scrambled(b.cont(v, []byte{0x33, 0x44}))

	chunk := cat(psi, idrPkt, scrPkt1, scrPkt2)
	res, err := core.Ingest(ctx, 0, chunk)
	if err != nil {
		t.Fatalf("ingest chunk: %v", err)
	}

	rapCount := 0
	invalCount := 0
	for _, ev := range res.Events {
		if ev.Kind == EventRandomAccessPoint {
			rapCount++
		}
		if ev.Kind == EventRandomAccessPointInvalidated {
			invalCount++
		}
	}

	if rapCount != 1 {
		t.Fatalf("expected exactly 1 RAP event, got %d", rapCount)
	}
	if invalCount != 1 {
		t.Fatalf("expected exactly 1 RAP invalidated event, got %d", invalCount)
	}
	if res.Facts.CleanEntryPoints != 0 {
		t.Fatalf("expected CleanEntryPoints=0, got %d", res.Facts.CleanEntryPoints)
	}
	if res.Facts.Scrambling.VideoScrambled != 2 {
		t.Fatalf("expected VideoScrambled=2, got %d", res.Facts.Scrambling.VideoScrambled)
	}
}

// 4. CC gap inside an in-flight IDR AU invalidates the provisional RAP and
// marks continuity broken.
func TestVideoScrambled_CCGapOnInFlightIDRAUTriggersInvalidation(t *testing.T) {
	b := vNew("test", "test", videoTSProgram)
	core := NewGoCore(videoTSProgram)
	ctx := context.Background()
	v := uint16(videoTSPID)

	psi := b.psi(0, h264Stream(v))
	idrPkt := b.start(v, h264SPS, h264PPS, h264IDR)
	idrOffset := int64(len(psi))

	// Skip CC to create an unannounced gap
	b.next(v)
	gapPkt := b.cont(v, []byte{0x84, 0x21, 0xA0, 0x33})

	chunk := cat(psi, idrPkt, gapPkt)
	res, err := core.Ingest(ctx, 0, chunk)
	if err != nil {
		t.Fatalf("ingest chunk: %v", err)
	}

	var gotInvalidated bool
	for _, ev := range res.Events {
		if ev.Kind == EventRandomAccessPointInvalidated {
			if ev.Offset != idrOffset {
				t.Fatalf("expected invalidation at %d, got %+v", idrOffset, ev)
			}
			gotInvalidated = true
		}
	}
	if !gotInvalidated {
		t.Fatalf("expected EventRandomAccessPointInvalidated after CC gap in IDR AU")
	}
	if !core.auContinuityBroken {
		t.Fatalf("expected auContinuityBroken=true after CC gap")
	}
	if res.Facts.CleanEntryPoints != 0 {
		t.Fatalf("expected CleanEntryPoints=0 after CC gap, got %d", res.Facts.CleanEntryPoints)
	}
}

// 5. Baseline: Clean IDR AU without scrambled packets or CC gaps preserves
// CleanEntryPoints=1 and emits no invalidation.
func TestVideoScrambled_CleanIDRFullyPreserved(t *testing.T) {
	b := vNew("test", "test", videoTSProgram)
	core := NewGoCore(videoTSProgram)
	ctx := context.Background()
	v := uint16(videoTSPID)

	psi := b.psi(0, h264Stream(v))
	idrPkt := b.start(v, h264SPS, h264PPS, h264IDR)
	contPkt := b.cont(v, []byte{0x84, 0x21, 0xA0, 0x33})
	nextPES := b.start(v, h264SliceP)

	chunk := cat(psi, idrPkt, contPkt, nextPES)
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
			gotInvalidated = true
		}
	}
	if !gotRAP {
		t.Fatalf("expected RAP event for clean IDR")
	}
	if gotInvalidated {
		t.Fatalf("unexpected RAP invalidation for clean IDR")
	}
	if res.Facts.CleanEntryPoints != 1 {
		t.Fatalf("expected CleanEntryPoints=1, got %d", res.Facts.CleanEntryPoints)
	}
	if res.Facts.CleanAccessUnits != 1 {
		t.Fatalf("expected CleanAccessUnits=1, got %d", res.Facts.CleanAccessUnits)
	}
}
