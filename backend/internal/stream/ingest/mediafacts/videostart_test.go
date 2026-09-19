// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package mediafacts

import (
	"context"
	"testing"
)

// 1. Valid PES -> Invalid PUSI/PES-Prefix -> No false RAP emitted,
// old PES coordinate abandoned, previous AU finalized.
func TestVideoStart_ValidPESThenInvalidPUSIPrefix_NoFalseRAP(t *testing.T) {
	b := vNew("test", "test", videoTSProgram)
	core := NewGoCore(videoTSProgram)
	ctx := context.Background()

	v := uint16(0x0100)
	psi := b.psi(0, h264Stream(v))
	startP := b.start(v, h264SPS, h264PPS, h264SliceP)

	chunk1 := cat(psi, startP)
	res1, err := core.Ingest(ctx, 0, chunk1)
	if err != nil {
		t.Fatalf("ingest chunk1: %v", err)
	}
	if !res1.Facts.ParameterSetsSeen {
		t.Fatalf("expected ParameterSetsSeen=true after chunk1")
	}
	if res1.Facts.RandomAccess.IRAPPoints != 0 {
		t.Fatalf("expected 0 IRAP after P-slice, got %d", res1.Facts.RandomAccess.IRAPPoints)
	}

	// Packet 2: PUSI=1 but invalid PES prefix 00 00 02, carrying an IDR slice
	rawBadPUSI := b.raw(v, true, cat(bogusPESPrefix, h264IDR))

	res2, err := core.Ingest(ctx, int64(len(chunk1)), rawBadPUSI)
	if err != nil {
		t.Fatalf("ingest chunk2: %v", err)
	}

	// Must emit ZERO RAP events
	for _, ev := range res2.Events {
		if ev.Kind == EventRandomAccessPoint {
			t.Fatalf("unexpected EventRandomAccessPoint emitted on invalid PUSI: offset=%d joinable=%v", ev.Offset, ev.Joinable)
		}
	}

	facts2 := res2.Facts
	if facts2.RandomAccess.IRAPPoints != 0 {
		t.Fatalf("expected 0 IRAP points, got %d", facts2.RandomAccess.IRAPPoints)
	}
	if facts2.CleanEntryPoints != 0 {
		t.Fatalf("expected 0 clean entry points, got %d", facts2.CleanEntryPoints)
	}
	// Preceding P-slice access unit was closed cleanly and accounted as predicted-rejected
	if facts2.RandomAccess.PredictedRejected != 1 {
		t.Fatalf("expected PredictedRejected=1, got %d", facts2.RandomAccess.PredictedRejected)
	}
	if facts2.CleanAccessUnits != 1 {
		t.Fatalf("expected CleanAccessUnits=1, got %d", facts2.CleanAccessUnits)
	}
}

// 2. Valid PES -> Invalid PUSI -> Subsequent valid PES + IDR -> RAP recognized cleanly at new coordinate.
func TestVideoStart_SubsequentValidPESThenIDR_RecognizedCleanly(t *testing.T) {
	b := vNew("test", "test", videoTSProgram)
	core := NewGoCore(videoTSProgram)
	ctx := context.Background()

	v := uint16(0x0100)
	psi := b.psi(0, h264Stream(v))
	startP := b.start(v, h264SPS, h264PPS, h264SliceP)
	chunk1 := cat(psi, startP)

	if _, err := core.Ingest(ctx, 0, chunk1); err != nil {
		t.Fatalf("ingest chunk1: %v", err)
	}

	// Invalid PUSI
	rawBadPUSI := b.raw(v, true, cat(bogusPESPrefix, h264IDR))
	offset2 := int64(len(chunk1))
	if _, err := core.Ingest(ctx, offset2, rawBadPUSI); err != nil {
		t.Fatalf("ingest chunk2: %v", err)
	}

	// Now a valid PES starts with SPS, PPS, IDR
	offset3 := offset2 + int64(len(rawBadPUSI))
	startIDR := b.start(v, h264SPS, h264PPS, h264IDR)
	res3, err := core.Ingest(ctx, offset3, startIDR)
	if err != nil {
		t.Fatalf("ingest chunk3: %v", err)
	}

	// Must detect RAP at offset3 exactly
	foundRAP := false
	for _, ev := range res3.Events {
		if ev.Kind == EventRandomAccessPoint {
			foundRAP = true
			if ev.Offset != offset3 {
				t.Fatalf("RAP offset = %d, want %d", ev.Offset, offset3)
			}
			if !ev.Joinable {
				t.Fatalf("expected Joinable=true on clean IDR")
			}
		}
	}
	if !foundRAP {
		t.Fatal("expected EventRandomAccessPoint on valid subsequent PES + IDR")
	}

	facts3 := res3.Facts
	if facts3.RandomAccess.IRAPPoints != 1 {
		t.Fatalf("expected IRAPPoints=1, got %d", facts3.RandomAccess.IRAPPoints)
	}
	if facts3.CleanEntryPoints != 1 {
		t.Fatalf("expected CleanEntryPoints=1, got %d", facts3.CleanEntryPoints)
	}
}

// 3. Invalid PUSI without prior PES does not fabricate decoder configuration or RAPs.
func TestVideoStart_InvalidPUSIWithoutPriorPES(t *testing.T) {
	b := vNew("test", "test", videoTSProgram)
	core := NewGoCore(videoTSProgram)
	ctx := context.Background()

	v := uint16(0x0100)
	psi := b.psi(0, h264Stream(v))

	// Refused payload unit carrying SPS, PPS and IDR before any valid PES
	badStart := b.raw(v, true, cat(bogusPESPrefix, h264SPS, h264PPS, h264IDR))

	chunk := cat(psi, badStart)
	res, err := core.Ingest(ctx, 0, chunk)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}

	for _, ev := range res.Events {
		if ev.Kind == EventRandomAccessPoint {
			t.Fatalf("unexpected RAP on invalid initial PUSI: offset=%d", ev.Offset)
		}
	}

	facts := res.Facts
	if facts.ParameterSetsSeen {
		t.Fatal("ParameterSetsSeen must remain false when SPS/PPS are in a refused PUSI")
	}
	if facts.RandomAccess.IRAPPoints != 0 {
		t.Fatalf("expected 0 IRAPPoints, got %d", facts.RandomAccess.IRAPPoints)
	}
}

// 4. Valid normal multi-packet PES remains completely unchanged.
func TestVideoStart_ValidMultiPacketPESContinuesNormally(t *testing.T) {
	b := vNew("test", "test", videoTSProgram)
	core := NewGoCore(videoTSProgram)
	ctx := context.Background()

	v := uint16(0x0100)
	psi := b.psi(0, h264Stream(v))

	// Packet 1: PES start with SPS and PPS
	pesStart := b.start(v, h264SPS, h264PPS)
	// Packet 2: Continuation packet (PUSI=false) carrying IDR slice
	pesCont := b.cont(v, h264IDR)

	pesOffset := int64(len(psi))
	chunk := cat(psi, pesStart, pesCont)
	res, err := core.Ingest(ctx, 0, chunk)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}

	foundRAP := false
	for _, ev := range res.Events {
		if ev.Kind == EventRandomAccessPoint {
			foundRAP = true
			if ev.Offset != pesOffset {
				t.Fatalf("RAP offset = %d, want %d", ev.Offset, pesOffset)
			}
			if !ev.Joinable {
				t.Fatalf("expected Joinable=true")
			}
		}
	}
	if !foundRAP {
		t.Fatal("expected RAP on multi-packet PES where IDR slice is in continuation packet")
	}

	facts := res.Facts
	if facts.RandomAccess.IRAPPoints != 1 {
		t.Fatalf("expected 1 IRAP point, got %d", facts.RandomAccess.IRAPPoints)
	}
}

// 5. Audio and other PID paths remain unaffected by video invalid PUSI.
func TestVideoStart_AudioAndOtherPIDsUnaffected(t *testing.T) {
	b := vNew("test", "test", 1)
	core := NewGoCore(1)
	ctx := context.Background()

	v := uint16(0x0100)
	a := uint16(0x0101)

	// Stream declaring video (H.264) and audio (AC-3, stream_id 0xBD)
	psi := b.psi(0, h264Stream(v), ac3Stream(a))
	if _, err := core.Ingest(ctx, 0, psi); err != nil {
		t.Fatalf("ingest psi: %v", err)
	}

	// Valid audio PES packet declaring stereo AC-3
	audioPayload := cat(audioTSPESHeader(0xBD, 0), audioTSAC3Frame(audioTSByte6Stereo))
	audioPkt := b.raw(a, true, audioPayload)

	// Invalid video PUSI packet
	badVideoPkt := b.raw(v, true, cat(bogusPESPrefix, h264IDR))

	chunk := cat(badVideoPkt, audioPkt)
	res, err := core.Ingest(ctx, int64(len(psi)), chunk)
	if err != nil {
		t.Fatalf("ingest chunk: %v", err)
	}

	facts := res.Facts
	// Video invalid PUSI was quarantined
	if facts.RandomAccess.IRAPPoints != 0 {
		t.Fatalf("video: expected 0 IRAP, got %d", facts.RandomAccess.IRAPPoints)
	}
	// Audio was parsed normally despite interleaved invalid video PUSI
	if len(facts.AudioTracks) == 0 {
		t.Fatal("audio: expected at least 1 audio track")
	}
	if facts.AudioTracks[0].PID != a {
		t.Fatalf("audio: expected PID %d, got %d", a, facts.AudioTracks[0].PID)
	}
}
