// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package smoother

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"
)

func createDummyTSPacket(pid uint16, cc uint8, hasPCR bool, pcrTicks uint64) []byte {
	pkt := make([]byte, TSPacketSize)
	pkt[0] = SyncByte
	pkt[1] = byte((pid >> 8) & 0x1F)
	pkt[2] = byte(pid & 0xFF)

	if hasPCR {
		pkt[3] = 0x30 | (cc & 0x0F) // AFC=0x03 (AF+payload)
		pkt[4] = 7                  // AF length = 7
		pkt[5] = 0x10               // pcr_flag = 1
		pcrBase := pcrTicks / 300
		pcrExt := pcrTicks % 300

		pkt[6] = byte(pcrBase >> 25)
		pkt[7] = byte(pcrBase >> 17)
		pkt[8] = byte(pcrBase >> 9)
		pkt[9] = byte(pcrBase >> 1)
		pkt[10] = byte((pcrBase&0x01)<<7) | 0x7E | byte((pcrExt>>8)&0x01)
		pkt[11] = byte(pcrExt & 0xFF)
	} else {
		pkt[3] = 0x10 | (cc & 0x0F) // AFC=0x01 (payload only)
	}

	for i := 12; i < TSPacketSize; i++ {
		pkt[i] = byte(i)
	}
	return pkt
}

func TestTSRingBuffer(t *testing.T) {
	rb := NewTSRingBuffer(TSPacketSize * 10)
	defer rb.Close()

	pkt1 := createDummyTSPacket(100, 0, false, 0)
	pkt2 := createDummyTSPacket(100, 1, false, 0)

	if _, err := rb.Push(pkt1); err != nil {
		t.Fatalf("push failed: %v", err)
	}
	if _, err := rb.Push(pkt2); err != nil {
		t.Fatalf("push failed: %v", err)
	}

	if rb.BufferedBytes() != TSPacketSize*2 {
		t.Fatalf("expected %d bytes, got %d", TSPacketSize*2, rb.BufferedBytes())
	}

	out, ok := rb.Pop(TSPacketSize)
	if !ok || len(out) != TSPacketSize {
		t.Fatalf("pop failed, got len=%d", len(out))
	}
	if !bytes.Equal(out, pkt1) {
		t.Fatalf("popped data mismatch")
	}

	out2, ok := rb.Pop(TSPacketSize)
	if !ok || len(out2) != TSPacketSize {
		t.Fatalf("pop 2 failed")
	}
	if !bytes.Equal(out2, pkt2) {
		t.Fatalf("popped data 2 mismatch")
	}
}

func TestTSIntegrityValidator(t *testing.T) {
	v := NewTSIntegrityValidator()

	for cc := uint8(0); cc < 16; cc++ {
		pkt := createDummyTSPacket(200, cc, false, 0)
		if err := v.ValidatePacket(pkt); err != nil {
			t.Fatalf("validate error: %v", err)
		}
	}

	if v.CCErrors != 0 {
		t.Fatalf("expected 0 CC errors, got %d", v.CCErrors)
	}

	// Inject CC drop: skip cc=0 and send cc=2
	pktDrop := createDummyTSPacket(200, 2, false, 0)
	_ = v.ValidatePacket(pktDrop)

	if v.CCErrors != 1 {
		t.Fatalf("expected 1 CC error for skipped counter, got %d", v.CCErrors)
	}
}

func TestPCRPacer(t *testing.T) {
	p := NewPCRPacer()

	// Feed 100 packets between PCR1 (0s) and PCR2 (100ms)
	pkt1 := createDummyTSPacket(100, 0, true, 0)
	p.FeedPacket(pkt1)

	for i := 1; i < 99; i++ {
		pkt := createDummyTSPacket(100, uint8(i%16), false, 0)
		p.FeedPacket(pkt)
	}

	// 100ms later = 2,700,000 ticks
	pkt2 := createDummyTSPacket(100, 3, true, 2_700_000)
	p.FeedPacket(pkt2)

	// Rate should be approximately 100 packets / 0.1s = 1000 packets/sec
	rate := p.PacketsPerSecond()
	if rate < 500 || rate > 3000 {
		t.Fatalf("unexpected estimated rate: %.1f pkts/s", rate)
	}
}

func TestSmoothStreamEndToEnd(t *testing.T) {
	var inputData []byte
	for i := 0; i < 500; i++ {
		pcrTicks := uint64(float64(i) * 0.04 * 27_000_000)
		hasPCR := (i % 25) == 0
		pkt := createDummyTSPacket(100, uint8(i%16), hasPCR, pcrTicks)
		inputData = append(inputData, pkt...)
	}

	inReader := bytes.NewReader(inputData)
	var outBuf bytes.Buffer

	cfg := DefaultConfig()
	cfg.StartupReservoirMs = 50.0 // small for fast test
	cfg.PacerIntervalMs = 5.0

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	report, err := SmoothStream(ctx, inReader, &outBuf, cfg)
	if err != nil && err != io.EOF {
		t.Fatalf("SmoothStream failed: %v", err)
	}

	if report == nil {
		t.Fatalf("expected non-nil report")
	}

	if report.CCErrorsIntroduced != 0 {
		t.Fatalf("expected 0 CC errors introduced, got %d", report.CCErrorsIntroduced)
	}

	if outBuf.Len() != len(inputData) {
		t.Fatalf("data length mismatch: in=%d out=%d", len(inputData), outBuf.Len())
	}
}
