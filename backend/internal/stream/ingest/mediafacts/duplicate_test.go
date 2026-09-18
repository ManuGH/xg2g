// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package mediafacts

import (
	"bytes"
	"testing"
)

func makeTestTSPacket(pid uint16, pusi bool, cc byte, payload []byte) []byte {
	pkt := make([]byte, TSPacketSize)
	pkt[0] = SyncByte
	if pusi {
		pkt[1] = 0x40 | byte((pid>>8)&0x1F)
	} else {
		pkt[1] = byte((pid >> 8) & 0x1F)
	}
	pkt[2] = byte(pid & 0xFF)
	pkt[3] = 0x10 | (cc & 0x0F) // payload only, afc=01
	copy(pkt[4:], payload)
	return pkt
}

func TestESPacketTracker_DirectTransitions(t *testing.T) {
	tracker := &esPacketTracker{}

	pkt1 := makeTestTSPacket(0x100, true, 0, []byte("packet-zero-content"))
	pkt1Copy := bytes.Clone(pkt1)
	pkt1Different := makeTestTSPacket(0x100, true, 0, []byte("different-payload!!"))
	pkt2 := makeTestTSPacket(0x100, false, 1, []byte("packet-one-content!"))
	pktWrap15 := makeTestTSPacket(0x100, false, 15, []byte("packet-fifteen!!!!!"))
	pktWrap0 := makeTestTSPacket(0x100, false, 0, []byte("packet-wrapped-zero"))

	// 1. First packet -> not duplicate, remembered
	if tracker.observeExactDuplicate(pkt1) {
		t.Fatalf("first packet must not be diagnosed as duplicate")
	}

	// 2. Same CC + identical packet -> duplicate
	if !tracker.observeExactDuplicate(pkt1Copy) {
		t.Fatalf("same CC + identical full 188-byte packet must be detected as duplicate")
	}

	// 3. Same CC + different packet -> not duplicate (processed with current behavior)
	if tracker.observeExactDuplicate(pkt1Different) {
		t.Fatalf("same CC with different packet content must NOT be suppressed as exact duplicate")
	}

	// 4. Next sequential packet -> not duplicate
	if tracker.observeExactDuplicate(pkt2) {
		t.Fatalf("next sequential packet (different CC) must not be duplicate")
	}

	// 5. CC wrap 15 -> 0 -> not duplicate
	if tracker.observeExactDuplicate(pktWrap15) {
		t.Fatalf("packet at CC 15 must not be duplicate")
	}
	if tracker.observeExactDuplicate(pktWrap0) {
		t.Fatalf("packet at CC 0 after CC 15 must not be duplicate")
	}

	// 6. Reset -> same packet again -> first/not duplicate
	*tracker = esPacketTracker{}
	if tracker.observeExactDuplicate(pktWrap0) {
		t.Fatalf("after reset, initial packet must not be duplicate")
	}
}
