// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package smoother

import (
	"time"
)

const (
	TSPacketSize = 188
	SyncByte     = 0x47
)

// TSHeader represents the minimal parsed fields of an MPEG-TS 188-byte packet.
type TSHeader struct {
	PID                    uint16
	ContinuityCounter      uint8
	HasAdaptationField     bool
	HasPayload             bool
	DiscontinuityIndicator bool
	HasPCR                 bool
	PCR                    uint64 // in 27 MHz ticks (PCR_base * 300 + PCR_ext)
	PCRSeconds             float64
}

// ParseTSPacket extracts header, adaptation flags, and PCR from a 188-byte packet.
func ParseTSPacket(pkt []byte) (TSHeader, bool) {
	if len(pkt) < TSPacketSize || pkt[0] != SyncByte {
		return TSHeader{}, false
	}

	h := TSHeader{
		PID:               (uint16(pkt[1]&0x1F) << 8) | uint16(pkt[2]),
		ContinuityCounter: pkt[3] & 0x0F,
	}

	afc := (pkt[3] >> 4) & 0x03
	h.HasAdaptationField = (afc == 0x02 || afc == 0x03)
	h.HasPayload = (afc == 0x01 || afc == 0x03)

	if h.HasAdaptationField && len(pkt) > 4 {
		afLen := int(pkt[4])
		if afLen > 0 && len(pkt) >= 6 {
			flags := pkt[5]
			h.DiscontinuityIndicator = (flags & 0x80) != 0
			hasPCR := (flags & 0x10) != 0

			if hasPCR && afLen >= 7 && len(pkt) >= 12 {
				h.HasPCR = true
				pcrBase := (uint64(pkt[6]) << 25) |
					(uint64(pkt[7]) << 17) |
					(uint64(pkt[8]) << 9) |
					(uint64(pkt[9]) << 1) |
					(uint64(pkt[10]) >> 7)

				pcrExt := (uint64(pkt[10]&0x01) << 8) | uint64(pkt[11])
				h.PCR = pcrBase*300 + pcrExt
				h.PCRSeconds = float64(h.PCR) / 27_000_000.0
			}
		}
	}

	return h, true
}

// PCRPacer calculates packet delivery timing based on PCR intervals.
type PCRPacer struct {
	lastPCRSeconds   float64
	lastPCRPacketIdx int64
	totalPacketsSeen int64
	packetsPerSecond float64
	bitrateEstimate  float64 // bits per second
	hasValidPCR      bool
	lastPCRLocalTime time.Time
}

// NewPCRPacer creates a new timeline pacer.
func NewPCRPacer() *PCRPacer {
	return &PCRPacer{
		packetsPerSecond: 3000.0, // initial sensible default (~4.5 Mbps)
		bitrateEstimate:  4_500_000.0,
	}
}

// FeedPacket inspects a packet, updating PCR pacing metrics.
func (p *PCRPacer) FeedPacket(pkt []byte) {
	p.totalPacketsSeen++
	h, ok := ParseTSPacket(pkt)
	if !ok || !h.HasPCR {
		return
	}

	now := time.Now()
	if !p.hasValidPCR || h.DiscontinuityIndicator {
		p.lastPCRSeconds = h.PCRSeconds
		p.lastPCRPacketIdx = p.totalPacketsSeen
		p.lastPCRLocalTime = now
		p.hasValidPCR = true
		return
	}

	deltaPCR := h.PCRSeconds - p.lastPCRSeconds
	deltaPackets := p.totalPacketsSeen - p.lastPCRPacketIdx

	// Valid PCR progression (e.g. 10ms <= deltaPCR <= 2000ms and >= 1 packet)
	if deltaPCR > 0.010 && deltaPCR < 2.0 && deltaPackets > 0 {
		rate := float64(deltaPackets) / deltaPCR
		if rate > 500 && rate < 50000 { // sanity check: ~750 kbps to ~75 Mbps
			p.packetsPerSecond = 0.8*p.packetsPerSecond + 0.2*rate
			p.bitrateEstimate = p.packetsPerSecond * TSPacketSize * 8.0
		}
		p.lastPCRSeconds = h.PCRSeconds
		p.lastPCRPacketIdx = p.totalPacketsSeen
		p.lastPCRLocalTime = now
	} else if deltaPCR < 0 || deltaPCR >= 2.0 {
		// Discontinuity or wrap-around: reset reference
		p.lastPCRSeconds = h.PCRSeconds
		p.lastPCRPacketIdx = p.totalPacketsSeen
		p.lastPCRLocalTime = now
	}
}

// Bitrate returns the current estimated stream bitrate in bits/sec.
func (p *PCRPacer) Bitrate() float64 {
	if p.bitrateEstimate <= 0 {
		return 4_500_000.0
	}
	return p.bitrateEstimate
}

// PacketsPerSecond returns the estimated rate in TS packets/sec.
func (p *PCRPacer) PacketsPerSecond() float64 {
	if p.packetsPerSecond <= 0 {
		return 3000.0
	}
	return p.packetsPerSecond
}
