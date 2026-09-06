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

// PSI table identifiers, ISO/IEC 13818-1 Table 2-31. Named once so the scan that
// stops at a foreign table and the check that refuses to interpret one cannot
// drift apart.
const (
	tableIDPAT = 0x00
	tableIDPMT = 0x02
)

// What a PAT or PMT section may declare about its own length.
//
// section_length is a twelve bit field, but ISO/IEC 13818-1 does not let either
// of these tables use all of it, and does not let them use the bottom of it
// either.
//
// The ceiling is the same for both: the first two bits of the field shall be
// '00' and the value shall not exceed 1021, so 1024 bytes in all. The field
// width is not the bound - treating it as one would have this parser collect
// four kilobytes for a section that cannot legally be longer than one.
//
// The floor is what each table's own syntax costs, counted from the field that
// declares it. A PAT cannot be shorter than
//
//	transport_stream_id      2
//	version, current_next    1
//	section_number           1
//	last_section_number      1
//	CRC_32                   4
//	                        --
//	                         9
//
// and a PMT adds its PCR PID and program_info_length to the same fixed part:
//
//	program_number           2
//	version, current_next    1
//	section_number           1
//	last_section_number      1
//	PCR_PID                  2
//	program_info_length      2
//	CRC_32                   4
//	                        --
//	                        13
//
// A section declaring less than that is not a short table. It is a declaration
// the table it claims to be cannot make - which is why this is a floor per
// table rather than a check against one value that happened to be a problem.
const (
	minPATSectionLength = 9
	minPMTSectionLength = 13
	maxPSISectionLength = 1021
	maxPSISectionBytes  = maxPSISectionLength + 3
)

// What the tables in force can cost, derived from the two bounds above rather
// than chosen.
//
// section_number is one byte and a section is accepted only when it numbers
// itself within its own table, so a table is at most 256 sections; each of them
// is at most maxPSISectionBytes. Nothing here is a policy: change either bound
// and these follow.
//
// They are the bound on what this package retains for a caller, which is why
// they are stated as bytes of table rather than as a packet count. How many
// transport packets a sender used to deliver a section is the sender's choice
// and is not retained.
const (
	maxSectionsPerTable = 256
	maxActiveTableBytes = maxSectionsPerTable * maxPSISectionBytes
	maxActivePSIBytes   = 2 * maxActiveTableBytes
)

// minPSISectionLength is what the expected table's own syntax costs.
func minPSISectionLength(isPAT bool) int {
	if isPAT {
		return minPATSectionLength
	}
	return minPMTSectionLength
}

// psiSectionDeclaration reads the three bytes that open a section and reports
// the total length they declare.
//
// It answers false for a declaration the expected table cannot make. Four things
// make one impossible, and they are checked together because they are all read
// from the same two bytes and all knowable the moment those bytes arrive:
//
//	section_syntax_indicator must be 1  - both tables use the long form
//	the bit after it must be 0          - fixed by the syntax, not reserved
//	section_length must reach the table's own minimum
//	section_length must not exceed 1021
//
// One helper for one question, used everywhere the question is asked: by the
// scan that meets a header whole inside a payload, by the assembler deciding how
// many bytes to collect, and by the completed-section check. A second definition
// of "how long may this be" is how two paths come to disagree about it.
//
// The caller fails closed on false. The length is the only thing that could say
// where the next section begins, and it has just been established that this one
// is impossible - so it is not something to navigate by either.
func psiSectionDeclaration(isPAT bool, prefix []byte) (int, bool) {
	if len(prefix) < 3 {
		return 0, false
	}
	if prefix[1]&0x80 == 0 || prefix[1]&0x40 != 0 {
		return 0, false
	}
	length := int((uint16(prefix[1]&0x0F) << 8) | uint16(prefix[2]))
	if length < minPSISectionLength(isPAT) || length > maxPSISectionLength {
		return 0, false
	}
	return length + 3, true
}

// expectedTableIDFor names the table a PID is being read for.
func expectedTableIDFor(isPAT bool) byte {
	if isPAT {
		return tableIDPAT
	}
	return tableIDPMT
}

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

// psiStreamAssembler joins one section out of the packets carrying it.
//
// It holds the section being assembled and nothing about how it arrived. The
// packets are the transport's segmentation of a section; once their bytes are in
// buf they have said everything they had to say, and keeping them would make
// this parser's memory a function of how finely a sender chose to fragment.
//
// lastPacket is the one exception and is bounded at one packet: an exact
// byte-for-byte repeat of the packet just seen is a carousel duplicate rather
// than a continuity error, and telling those apart needs the previous packet.
type psiStreamAssembler struct {
	buf        []byte
	sectionLen int
	lastCC     uint8
	hasCC      bool
	lastPacket []byte
}

func (s *psiStreamAssembler) reset() {
	s.buf = s.buf[:0]
	s.sectionLen = 0
	s.hasCC = false
	s.lastPacket = nil
}

// tableSectionTracker collects the sections of one generation of one table.
//
// Keyed by section_number, so a section that arrives twice replaces itself and
// a generation cannot grow past the 256 numbers a section may have. What it
// holds is the accepted section bytes; the packets that carried them are not
// part of a table's identity and are not kept.
type tableSectionTracker struct {
	inFlightVersion uint8
	hasInFlight     bool
	lastSectionNum  uint8
	sections        map[uint8][]byte
}

func (t *tableSectionTracker) reset() {
	t.hasInFlight = false
	t.lastSectionNum = 0
	t.sections = make(map[uint8][]byte)
}

func (t *tableSectionTracker) addSection(version uint8, sectionNum uint8, lastSectionNum uint8, sectionBytes []byte) bool {
	if !t.hasInFlight || t.inFlightVersion != version || t.lastSectionNum != lastSectionNum {
		t.inFlightVersion = version
		t.hasInFlight = true
		t.lastSectionNum = lastSectionNum
		t.sections = make(map[uint8][]byte)
	}

	t.sections[sectionNum] = cloneSlice(sectionBytes)

	if len(t.sections) == int(lastSectionNum)+1 {
		// Counted in int, not in a uint8.
		//
		// last_section_number may be 255, and 255 is also the largest value a
		// uint8 counter can hold: incrementing past the last section wraps it to
		// zero, the bound is met again, and this walk never ends. A table using
		// all 256 numbers it is allowed is entirely legal, so nothing upstream
		// refuses it and nothing downstream is ever reached - the loop spins
		// holding the caller's lock, on ordinary input.
		for n := 0; n <= int(lastSectionNum); n++ {
			// #nosec G115 -- n is bounded by lastSectionNum, itself a uint8
			if _, ok := t.sections[uint8(n)]; !ok {
				return false
			}
		}
		return true
	}
	return false
}

// generation returns the sections of the table that has just completed, in the
// order the table numbers them rather than the order they arrived in.
//
// The copies are the caller's. The tracker replaces a slot's slice rather than
// writing through it, so aliasing would be safe today - but what the core keeps
// as the table in force outlives the generation it came from, and it must not be
// something a later section could reach.
//
// The count is kept in int, for the reason given on the completeness check
// above: a uint8 counter bounded by last_section_number cannot end when that
// value is 255.
func (t *tableSectionTracker) generation(lastSectionNum uint8) [][]byte {
	out := make([][]byte, 0, int(lastSectionNum)+1)
	for n := 0; n <= int(lastSectionNum); n++ {
		// #nosec G115 -- n is bounded by lastSectionNum, itself a uint8
		out = append(out, cloneSlice(t.sections[uint8(n)]))
	}
	return out
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

// MaxSectionBytes is the largest a PAT or PMT section may be, and
// MaxSectionsPerTable the most sections one table may have. Exported because a
// caller that has to deliver the tables needs the same bound this package holds
// itself to, and two copies of a number are how two bounds come to differ.
const (
	MaxSectionBytes     = maxPSISectionBytes
	MaxSectionsPerTable = maxSectionsPerTable
)
