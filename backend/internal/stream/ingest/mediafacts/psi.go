// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Package mediafacts reads what a transport stream says about itself.
//
// It owns interpretation and nothing else. It does not store bytes, hand them
// out, decide when a stream becomes a new lifecycle epoch, or choose what to do
// with what it finds - those belong to the caller, and the split is deliberate:
//
//	the caller owns the bytes, their offsets and the generation
//	this package owns what the bytes mean
//
// Every offset it reports is in the caller's own monotonic byte coordinate
// system, so an entry point it finds can be handed straight back to a reader
// without translation. It reports that the program's identity changed; it does
// not decide that a new generation has begun.
package mediafacts

import "errors"

const (
	TSPacketSize = 188
	SyncByte     = 0x47

	// ScrambledVerdictMinPackets is the number of scrambled video packets that must be observed,
	// with zero clear ones, before the stream is declared scrambled. At broadcast video rates this
	// threshold is crossed within a few tens of milliseconds.
	ScrambledVerdictMinPackets = 100
)

// ErrInvalidPacketSize reports data that is not 188-byte packet aligned.
var ErrInvalidPacketSize = errors.New("data slice is not 188-byte packet aligned")

// VideoCodec identifies the elementary video stream codec parsed from PMT.
type VideoCodec string

const (
	CodecUnknown VideoCodec = "unknown"
	CodecH264    VideoCodec = "h264"
	CodecH265    VideoCodec = "h265"
	CodecMPEG2   VideoCodec = "mpeg2"
)

var mpeg2CRCTable [256]uint32

func init() {
	// Counted as uint32 so the shift needs no narrowing conversion; the bound
	// makes the two identical, but only one of them is checkable.
	for i := uint32(0); i < 256; i++ {
		crc := i << 24
		for j := 0; j < 8; j++ {
			if (crc & 0x80000000) != 0 {
				crc = (crc << 1) ^ 0x04C11DB7
			} else {
				crc <<= 1
			}
		}
		mpeg2CRCTable[i] = crc
	}
}

// CalculateMPEG2CRC32 calculates the standard ISO/IEC 13818-1 32-bit CRC.
func CalculateMPEG2CRC32(data []byte) uint32 {
	crc := uint32(0xFFFFFFFF)
	for _, b := range data {
		crc = (crc << 8) ^ mpeg2CRCTable[byte(crc>>24)^b]
	}
	return crc
}

type psiStreamAssembler struct {
	buf        []byte
	sectionLen int
	lastCC     uint8
	hasCC      bool
	lastPacket []byte
	rawPackets [][]byte
}

func (s *psiStreamAssembler) reset() {
	s.buf = s.buf[:0]
	s.sectionLen = 0
	s.hasCC = false
	s.lastPacket = nil
	s.rawPackets = s.rawPackets[:0]
}

type tableSectionTracker struct {
	inFlightVersion uint8
	hasInFlight     bool
	lastSectionNum  uint8
	sections        map[uint8][]byte
	rawPackets      map[uint8][][]byte
}

func (t *tableSectionTracker) reset() {
	t.hasInFlight = false
	t.lastSectionNum = 0
	t.sections = make(map[uint8][]byte)
	t.rawPackets = make(map[uint8][][]byte)
}

func (t *tableSectionTracker) addSection(version uint8, sectionNum uint8, lastSectionNum uint8, sectionBytes []byte, packets [][]byte) bool {
	if !t.hasInFlight || t.inFlightVersion != version || t.lastSectionNum != lastSectionNum {
		t.inFlightVersion = version
		t.hasInFlight = true
		t.lastSectionNum = lastSectionNum
		t.sections = make(map[uint8][]byte)
		t.rawPackets = make(map[uint8][][]byte)
	}

	t.sections[sectionNum] = cloneSlice(sectionBytes)
	t.rawPackets[sectionNum] = cloneSliceList(packets)

	if len(t.sections) == int(lastSectionNum)+1 {
		for i := uint8(0); i <= lastSectionNum; i++ {
			if _, ok := t.sections[i]; !ok {
				return false
			}
		}
		return true
	}
	return false
}

type StreamScrambling struct {
	VideoScrambled uint64
	VideoClear     uint64
	AudioScrambled uint64
	AudioClear     uint64
	// VideoClearRun is the number of clear video packets since the last scrambled
	// one. Descrambling was measured coming up intermittently on this receiver, so
	// the run length - not the total - is what says the stream is clear now.
	VideoClearRun uint64
	// AudioClearRun is the same measure across the audio streams.
	AudioClearRun uint64
	// AudioPIDs are the audio elementary streams named by the active PMT.
	AudioPIDs []uint16
}

func cloneSlice(in []byte) []byte {
	out := make([]byte, len(in))
	copy(out, in)
	return out
}

func cloneSliceList(in [][]byte) [][]byte {
	out := make([][]byte, len(in))
	for i, s := range in {
		out[i] = cloneSlice(s)
	}
	return out
}
