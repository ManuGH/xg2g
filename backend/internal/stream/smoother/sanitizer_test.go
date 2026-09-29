// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package smoother

import (
	"testing"
)

func TestTSCCSanitizer_Sequential(t *testing.T) {
	s := NewTSCCSanitizer()
	v := NewTSIntegrityValidator()

	for i := 0; i < 64; i++ {
		pkt := createDummyTSPacket(0x100, uint8(i%16), false, 0)
		repaired := s.SanitizePacket(pkt)
		if repaired {
			t.Fatalf("expected no repair on sequential packet #%d", i)
		}
		if err := v.ValidatePacket(pkt); err != nil {
			t.Fatalf("validator error: %v", err)
		}
	}

	if s.RepairedCCs != 0 {
		t.Fatalf("expected 0 repaired CCs, got %d", s.RepairedCCs)
	}
	if v.CCErrors != 0 {
		t.Fatalf("expected 0 validator CC errors, got %d", v.CCErrors)
	}
}

func TestTSCCSanitizer_GapRepair(t *testing.T) {
	s := NewTSCCSanitizer()
	v := NewTSIntegrityValidator()

	// Pattern with intentional upstream packet drops:
	// CC: 0, 1, 2, [gap 3..6 dropped], 7, 8, [gap 9..13 dropped], 14, 15, 0, 1
	ccSequence := []uint8{0, 1, 2, 7, 8, 14, 15, 0, 1}
	// Packets 0, 1, 2 match incoming CC.
	// Packets 3..8 (6 packets) are continuously rewritten to 3, 4, 5, 6, 7, 8
	expectedRepairs := int64(6)

	for i, cc := range ccSequence {
		pkt := createDummyTSPacket(0x100, cc, false, 0)
		s.SanitizePacket(pkt)

		// Post-sanitization, packets must have strictly 0, 1, 2, 3, 4, 5, 6, 7, 8
		expectedCC := uint8(i % 16)
		actualCC := pkt[3] & 0x0F
		if actualCC != expectedCC {
			t.Fatalf("packet #%d: expected CC=%d, got CC=%d", i, expectedCC, actualCC)
		}

		if err := v.ValidatePacket(pkt); err != nil {
			t.Fatalf("validator error on packet #%d: %v", i, err)
		}
	}

	if s.RepairedCCs != expectedRepairs {
		t.Fatalf("expected %d repairs, got %d", expectedRepairs, s.RepairedCCs)
	}
	if v.CCErrors != 0 {
		t.Fatalf("sanitized stream should produce 0 CC errors, got %d", v.CCErrors)
	}
}

func TestTSCCSanitizer_AdaptationOnly(t *testing.T) {
	s := NewTSCCSanitizer()

	// 1. First payload packet: CC=5
	pkt1 := createDummyTSPacket(0x100, 5, false, 0)
	s.SanitizePacket(pkt1)
	if pkt1[3]&0x0F != 5 {
		t.Fatalf("expected CC 5, got %d", pkt1[3]&0x0F)
	}

	// 2. Adaptation field only (AFC=0x02, no payload): should retain CC=5
	pktAF := make([]byte, TSPacketSize)
	pktAF[0] = SyncByte
	pktAF[1] = 0x01
	pktAF[2] = 0x00
	pktAF[3] = 0x20 | 0x08 // AFC=0x02, corrupt incoming CC=8
	pktAF[4] = 183         // AF length filling packet

	repaired := s.SanitizePacket(pktAF)
	if !repaired {
		t.Fatalf("expected adaptation-only CC to be repaired")
	}
	if pktAF[3]&0x0F != 5 {
		t.Fatalf("expected adaptation-only CC to match last payload CC 5, got %d", pktAF[3]&0x0F)
	}

	// 3. Next payload packet: should advance to CC=6
	pkt2 := createDummyTSPacket(0x100, 9, false, 0) // Corrupted incoming CC 9
	s.SanitizePacket(pkt2)
	if pkt2[3]&0x0F != 6 {
		t.Fatalf("expected payload CC to advance to 6, got %d", pkt2[3]&0x0F)
	}
}

func TestTSCCSanitizer_NullPackets(t *testing.T) {
	s := NewTSCCSanitizer()

	// Null packet PID 0x1FFF
	nullPkt := make([]byte, TSPacketSize)
	nullPkt[0] = SyncByte
	nullPkt[1] = 0x1F
	nullPkt[2] = 0xFF
	nullPkt[3] = 0x15 // CC=5

	repaired := s.SanitizePacket(nullPkt)
	if repaired {
		t.Fatalf("null packets should never be modified")
	}
	if nullPkt[3] != 0x15 {
		t.Fatalf("null packet header was modified")
	}
}

func TestTSCCSanitizer_MultiPID(t *testing.T) {
	s := NewTSCCSanitizer()
	v := NewTSIntegrityValidator()

	pids := []uint16{0x100, 0x101}
	for i := 0; i < 50; i++ {
		for _, pid := range pids {
			// Introduce random jumps
			rawCC := uint8((i * 3) % 16)
			pkt := createDummyTSPacket(pid, rawCC, false, 0)
			s.SanitizePacket(pkt)

			if err := v.ValidatePacket(pkt); err != nil {
				t.Fatalf("validator error for PID 0x%X: %v", pid, err)
			}
		}
	}

	if v.CCErrors != 0 {
		t.Fatalf("expected 0 CC errors across multi-PID stream, got %d", v.CCErrors)
	}
}
