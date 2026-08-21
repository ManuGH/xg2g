// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package normalizer

import (
	"sync"
	"time"
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

// PCREstimator tracks stream timing, calculates smoothed packet rates,
// and enforces program-specific PCR PID filtering.
type PCREstimator struct {
	mu               sync.Mutex
	targetPCRPID     uint16
	lockedPCRPID     uint16
	lastPCRSeconds   float64
	lastPCRPacketIdx int64
	totalPacketsSeen int64
	packetsPerSecond float64
	bitrateEstimate  float64 // bits per second
	hasValidPCR      bool
	lastPCRLocalTime time.Time
}

// NewPCREstimator creates a new timeline pacer with an initial rate estimate.
func NewPCREstimator(initialBitrateKbps float64, targetPCRPID uint16) *PCREstimator {
	initialBps := initialBitrateKbps * 1000.0
	initialPPS := initialBps / (float64(TSPacketSize) * 8.0)
	if initialPPS <= 0 {
		initialPPS = 3000.0
		initialBps = initialPPS * float64(TSPacketSize) * 8.0
	}

	return &PCREstimator{
		targetPCRPID:     targetPCRPID,
		packetsPerSecond: initialPPS,
		bitrateEstimate:  initialBps,
	}
}

// SetPCRPID configures the program-specific PCR PID to track.
// Setting 0 enables auto-lock onto the first observed PCR PID.
func (p *PCREstimator) SetPCRPID(pid uint16) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.targetPCRPID != pid {
		p.targetPCRPID = pid
		p.lockedPCRPID = pid
		p.hasValidPCR = false // reset time reference, but PRESERVE rate estimate
	}
}

// FeedPacket inspects a 188-byte packet and updates the PCR rate model.
func (p *PCREstimator) FeedPacket(pkt []byte) {
	h, ok := ParseTSPacket(pkt)
	if !ok {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	p.totalPacketsSeen++

	if !h.HasPCR {
		return
	}

	// Enforce program-specific PCR PID filtering
	if p.targetPCRPID > 0 {
		if h.PID != p.targetPCRPID {
			return
		}
	} else {
		// Auto-lock onto first active PCR PID
		if p.lockedPCRPID == 0 {
			p.lockedPCRPID = h.PID
		} else if h.PID != p.lockedPCRPID {
			return
		}
	}

	now := time.Now()

	// Initial lock or Discontinuity Indicator flagged by upstream
	if !p.hasValidPCR || h.DiscontinuityIndicator {
		p.lastPCRSeconds = h.PCRSeconds
		p.lastPCRPacketIdx = p.totalPacketsSeen
		p.lastPCRLocalTime = now
		p.hasValidPCR = true
		// NOTE: Preserves existing p.packetsPerSecond without abrupt reset!
		return
	}

	deltaPCR := h.PCRSeconds - p.lastPCRSeconds
	deltaPackets := p.totalPacketsSeen - p.lastPCRPacketIdx

	// Check 33-bit PCR rollover (~26.5 hours): deltaPCR will be negative
	if deltaPCR < 0 {
		const pcrMaxSeconds = float64(1<<33*300) / 27_000_000.0 // ~95443.717s
		if (deltaPCR+pcrMaxSeconds) > 0.010 && (deltaPCR+pcrMaxSeconds) < 2.0 {
			deltaPCR += pcrMaxSeconds
		}
	}

	// Valid PCR progression (e.g. 10ms <= deltaPCR <= 2000ms and >= 1 packet)
	if deltaPCR >= 0.010 && deltaPCR <= 2.0 && deltaPackets > 0 {
		instantRate := float64(deltaPackets) / deltaPCR
		if instantRate >= 200.0 && instantRate <= 100000.0 { // ~300 kbps to ~150 Mbps
			p.packetsPerSecond = 0.8*p.packetsPerSecond + 0.2*instantRate
			p.bitrateEstimate = p.packetsPerSecond * float64(TSPacketSize) * 8.0
		}
		p.lastPCRSeconds = h.PCRSeconds
		p.lastPCRPacketIdx = p.totalPacketsSeen
		p.lastPCRLocalTime = now
	} else if deltaPCR < 0.010 || deltaPCR > 2.0 {
		// Unsignalized PCR gap, wrap, or timestamp jump: re-anchor time reference
		// without altering the existing smoothed rate estimate.
		p.lastPCRSeconds = h.PCRSeconds
		p.lastPCRPacketIdx = p.totalPacketsSeen
		p.lastPCRLocalTime = now
	}
}

// PacketsPerSecond returns the authoritative smoothed packet rate.
func (p *PCREstimator) PacketsPerSecond() float64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.packetsPerSecond
}

// BitrateKbps returns the smoothed bitrate in kilobits per second.
func (p *PCREstimator) BitrateKbps() float64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.bitrateEstimate / 1000.0
}

// HasValidPCR returns true if at least one valid PCR anchor was recorded.
func (p *PCREstimator) HasValidPCR() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.hasValidPCR
}
