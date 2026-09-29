// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package smoother

// TSCCSanitizer enforces strict, gapless monotonic MPEG-TS Continuity Counters (0..15)
// per PID, eliminating hardware decoder stutter and drops caused by upstream packet loss.
type TSCCSanitizer struct {
	nextCC      map[uint16]uint8
	hasSeenPID  map[uint16]bool
	RepairedCCs int64
}

// NewTSCCSanitizer creates a new DVB Continuity Counter Sanitizer.
func NewTSCCSanitizer() *TSCCSanitizer {
	return &TSCCSanitizer{
		nextCC:     make(map[uint16]uint8),
		hasSeenPID: make(map[uint16]bool),
	}
}

// SanitizePacket ensures pkt has a strictly sequential Continuity Counter for its PID.
// Returns true if the CC was rewritten/repaired.
func (s *TSCCSanitizer) SanitizePacket(pkt []byte) bool {
	if len(pkt) < TSPacketSize || pkt[0] != SyncByte {
		return false
	}

	pid := (uint16(pkt[1]&0x1F) << 8) | uint16(pkt[2])
	if pid == 0x1FFF {
		// Null packets do not have continuity counters
		return false
	}

	afc := (pkt[3] >> 4) & 0x03
	hasPayload := (afc == 0x01 || afc == 0x03)
	if !hasPayload {
		// Adaptation field only, no payload: CC must not increment
		if !s.hasSeenPID[pid] {
			incomingCC := pkt[3] & 0x0F
			s.nextCC[pid] = (incomingCC + 1) & 0x0F
			s.hasSeenPID[pid] = true
			return false
		}
		expected := (s.nextCC[pid] + 15) & 0x0F // last used CC
		if (pkt[3] & 0x0F) != expected {
			pkt[3] = (pkt[3] & 0xF0) | expected
			s.RepairedCCs++
			return true
		}
		return false
	}

	// Packet has payload: assign sequential CC
	if !s.hasSeenPID[pid] {
		// First packet seen for this PID: synchronize with incoming CC
		incomingCC := pkt[3] & 0x0F
		s.nextCC[pid] = (incomingCC + 1) & 0x0F
		s.hasSeenPID[pid] = true
		return false
	}

	expectedCC := s.nextCC[pid]
	s.nextCC[pid] = (expectedCC + 1) & 0x0F

	incomingCC := pkt[3] & 0x0F
	if incomingCC != expectedCC {
		pkt[3] = (pkt[3] & 0xF0) | expectedCC
		s.RepairedCCs++
		return true
	}

	return false
}
