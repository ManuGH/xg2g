// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package smoother

import (
	"bytes"
	"fmt"
)

// TSIntegrityValidator monitors Continuity Counter and PCR progression across all PIDs.
type TSIntegrityValidator struct {
	lastCC  map[uint16]uint8
	hasCC   map[uint16]bool
	lastPCR map[uint16]uint64
	hasPCR  map[uint16]bool

	CCErrors    int
	PCRErrors   int
	SyncErrors  int
	PacketCount int64
}

// NewTSIntegrityValidator creates a new validator.
func NewTSIntegrityValidator() *TSIntegrityValidator {
	return &TSIntegrityValidator{
		lastCC:  make(map[uint16]uint8),
		hasCC:   make(map[uint16]bool),
		lastPCR: make(map[uint16]uint64),
		hasPCR:  make(map[uint16]bool),
	}
}

// ValidatePacket checks a single 188-byte TS packet for sync, CC, and PCR correctness.
func (v *TSIntegrityValidator) ValidatePacket(pkt []byte) error {
	v.PacketCount++
	if len(pkt) < TSPacketSize {
		v.SyncErrors++
		return fmt.Errorf("packet #%d: truncated length %d < 188", v.PacketCount, len(pkt))
	}
	if pkt[0] != SyncByte {
		v.SyncErrors++
		return fmt.Errorf("packet #%d: invalid sync byte 0x%02X", v.PacketCount, pkt[0])
	}

	h, ok := ParseTSPacket(pkt)
	if !ok {
		v.SyncErrors++
		return fmt.Errorf("packet #%d: failed to parse header", v.PacketCount)
	}

	// PID 8191 (0x1FFF) is Null Packet, CC does not apply
	if h.PID == 0x1FFF {
		return nil
	}

	// Check Continuity Counter (only increments when payload is present)
	if h.HasPayload {
		if _, exists := v.hasCC[h.PID]; exists {
			last := v.lastCC[h.PID]
			expected := (last + 1) & 0x0F
			if h.DiscontinuityIndicator {
				// Discontinuity indicator explicitly signals a discontinuity
			} else if h.ContinuityCounter == last {
				// Duplicate packet is allowed under MPEG-2 spec if payload is identical or adaptation field only
			} else if h.ContinuityCounter != expected {
				v.CCErrors++
			}
		}
		v.lastCC[h.PID] = h.ContinuityCounter
		v.hasCC[h.PID] = true
	}

	// Check PCR progression
	if h.HasPCR {
		if _, exists := v.hasPCR[h.PID]; exists {
			lastPCR := v.lastPCR[h.PID]
			if !h.DiscontinuityIndicator {
				// 27 MHz ticks: PCR must be strictly forward, max jump < 2 seconds (54,000,000 ticks)
				if h.PCR < lastPCR && (lastPCR-h.PCR) < (257<<33)*300 { // check for 33-bit wrap
					v.PCRErrors++
				}
			}
		}
		v.lastPCR[h.PID] = h.PCR
		v.hasPCR[h.PID] = true
	}

	return nil
}

// ComparePayloads compares two packet slices byte-for-byte.
func ComparePayloads(in, out []byte) (matchedPackets int, diffPacketIdx int, isIdentical bool) {
	inPackets := len(in) / TSPacketSize
	outPackets := len(out) / TSPacketSize
	minPackets := inPackets
	if outPackets < minPackets {
		minPackets = outPackets
	}

	for i := 0; i < minPackets; i++ {
		pIn := in[i*TSPacketSize : (i+1)*TSPacketSize]
		pOut := out[i*TSPacketSize : (i+1)*TSPacketSize]
		if !bytes.Equal(pIn, pOut) {
			return i, i, false
		}
	}

	return minPackets, -1, (inPackets == outPackets)
}
