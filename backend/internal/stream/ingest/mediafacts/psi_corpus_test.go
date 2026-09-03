// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package mediafacts

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ManuGH/xg2g/internal/stream/ingest/esaudio"
)

// The shared PSI corpus.
//
// Every case is a sequence of calls into a Core and the PSI-scoped part of the
// ParseResult expected after each one. Both are written out here by hand: the
// point of a corpus a second implementation has to satisfy is that it states the
// semantics, not that it records whatever the first implementation happens to
// do. The Go core is then checked against these expectations, and the same file
// is checked into the repository for the Rust core to be checked against once it
// has a PSI parser at all.
//
// Chunk boundaries are part of the case, not an implementation detail. A section
// assembler carries a partial section across a call, so "the same packets, cut
// differently" is a different input - which is why a case is a list of chunks
// rather than one blob.
//
// What this corpus deliberately does NOT cover: anything downstream of PSI.
// There is no video elementary stream payload in any case here, so no case can
// produce a random access point, and the video facts that appear are the ones
// the PMT alone declares - the PID and the codec. Random access, parameter sets,
// timing and scrambling belong to later steps and to their own corpora; folding
// them in here would make a PSI disagreement and a video disagreement produce
// the same red test.
//
// The tables are built with an independent CRC implementation rather than with
// CalculateMPEG2CRC32, for the same reason the audio corpus writes out the AC-3
// byte 6 layout by hand: a fixture built with the function under test cannot
// catch a mistake in that function.

var updatePSICorpus = flag.Bool("update-psi-corpus", false, "rewrite the checked-in PSI corpus")

// psiCorpusPath is the shared location both languages read.
const psiCorpusPath = "../../../../../testdata/psi-corpus/corpus.txt"

const psiCorpusFormatVersion = 1

// --- expectation shapes ---------------------------------------------------

type psiTrackExpect struct {
	pid              uint16
	streamType       byte
	codec            string
	lang             string
	channels         int
	multichannel     bool
	componentType    uint8
	hasComponentType bool
}

type psiExpect struct {
	through       int64
	hasPAT        bool
	hasPMT        bool
	pmtVersion    uint8
	programNumber uint16
	pmtPID        uint16
	videoPID      uint16
	videoCodec    VideoCodec
	audioPIDs     []uint16
	tracks        []psiTrackExpect

	// events names the ordered event kinds this call produced. "identity" is
	// EventProgramIdentityChanged; a "rap" here would mean a PSI case grew video
	// payload, which is why the field records every event rather than filtering.
	events []string

	// patPackets and pmtPackets are the indices, into the case's own packet
	// sequence, of the packets expected in ActivePSI. The comparison is on the
	// bytes at those indices, so this is an exact check written compactly: a
	// case's packet N is bytes [188*N, 188*N+188) of its chunks concatenated.
	patPackets []int
	pmtPackets []int
}

type psiStepKind uint8

const (
	psiStepChunk psiStepKind = iota
	psiStepTarget
)

type psiStep struct {
	kind   psiStepKind
	chunk  []byte
	target uint16
	want   psiExpect
}

type psiCorpusCase struct {
	name    string
	desc    string
	initial uint16 // the target program number the core is constructed with
	steps   []psiStep

	// note marks a case whose expectation records what the Go reference does
	// today without a decision having been taken that it is what it should do.
	// It travels with the corpus rather than only with the pull request, so a
	// reader of the file knows which lines are settled semantics and which are
	// an open question. A case without a note is settled.
	note string
}

// --- table builders -------------------------------------------------------

// psiCRC32 is the ISO/IEC 13818-1 CRC, computed bit by bit and without a table.
// Deliberately not CalculateMPEG2CRC32: a section whose CRC was produced by the
// function that later validates it agrees with that function even when both are
// wrong.
func psiCRC32(data []byte) uint32 {
	crc := uint32(0xFFFFFFFF)
	for _, b := range data {
		for i := 0; i < 8; i++ {
			bit := (b >> (7 - uint(i))) & 1
			top := (crc >> 31) & 1
			crc <<= 1
			if top != uint32(bit) {
				crc ^= 0x04C11DB7
			}
		}
	}
	return crc
}

func appendCRC(section []byte) []byte {
	crc := psiCRC32(section)
	return append(section,
		byte(crc>>24), byte(crc>>16), byte(crc>>8), byte(crc))
}

// psiSectionHeader writes table_id, the syntax/length pair, the table id
// extension, and the version/section triple. sectionLen counts every byte after
// the length field itself, the CRC included.
func psiSectionHeader(tableID byte, idExtension uint16, sectionLen int, version, sectionNum, lastSectionNum, currentNext uint8) []byte {
	return []byte{
		tableID,
		0xB0 | byte((sectionLen>>8)&0x0F), // section_syntax_indicator=1, '0', reserved='11'
		byte(sectionLen & 0xFF),
		byte(idExtension >> 8), byte(idExtension & 0xFF),
		0xC0 | (version&0x1F)<<1 | (currentNext & 0x01), // reserved='11'
		sectionNum,
		lastSectionNum,
	}
}

type patProgram struct {
	number uint16
	pid    uint16
}

func patSection(tsID uint16, version, sectionNum, lastSectionNum, currentNext uint8, programs ...patProgram) []byte {
	// after the length field: 5 header bytes + 4 per program + 4 CRC
	sectionLen := 5 + 4*len(programs) + 4
	s := psiSectionHeader(0x00, tsID, sectionLen, version, sectionNum, lastSectionNum, currentNext)
	for _, p := range programs {
		s = append(s,
			byte(p.number>>8), byte(p.number&0xFF),
			0xE0|byte((p.pid>>8)&0x1F), byte(p.pid&0xFF))
	}
	return appendCRC(s)
}

type pmtStream struct {
	streamType  byte
	pid         uint16
	descriptors []byte
}

func pmtSection(programNumber, pcrPID uint16, version, sectionNum, lastSectionNum, currentNext uint8, programInfo []byte, streams ...pmtStream) []byte {
	esBytes := 0
	for _, st := range streams {
		esBytes += 5 + len(st.descriptors)
	}
	// after the length field: 9 fixed bytes + program_info + ES loop + 4 CRC
	sectionLen := 9 + len(programInfo) + esBytes + 4
	s := psiSectionHeader(0x02, programNumber, sectionLen, version, sectionNum, lastSectionNum, currentNext)
	s = append(s,
		0xE0|byte((pcrPID>>8)&0x1F), byte(pcrPID&0xFF),
		0xF0|byte((len(programInfo)>>8)&0x0F), byte(len(programInfo)&0xFF))
	s = append(s, programInfo...)
	for _, st := range streams {
		s = append(s,
			st.streamType,
			0xE0|byte((st.pid>>8)&0x1F), byte(st.pid&0xFF),
			0xF0|byte((len(st.descriptors)>>8)&0x0F), byte(len(st.descriptors)&0xFF))
		s = append(s, st.descriptors...)
	}
	return appendCRC(s)
}

// --- descriptor builders --------------------------------------------------

func descLanguage(lang string, audioType byte) []byte {
	return append([]byte{0x0A, 0x04}, append([]byte(lang), audioType)...)
}

// descAC3 and descEAC3 carry the component_type_flag and the component type
// itself, which is the only place a channel declaration comes from.
func descAC3(componentType byte) []byte {
	return []byte{descriptorAC3, 0x02, 0x80, componentType}
}

func descEAC3(componentType byte) []byte {
	return []byte{descriptorEnhAC3, 0x02, 0x80, componentType}
}

// descAC3NoComponentType is the descriptor present but declaring nothing.
func descAC3NoComponentType() []byte {
	return []byte{descriptorAC3, 0x01, 0x00}
}

func descAAC(aacType byte) []byte {
	return []byte{descriptorAAC, 0x03, 0x51, 0x80, aacType}
}

func descRegistration(id string) []byte {
	return append([]byte{descriptorAudioReg, byte(len(id))}, []byte(id)...)
}

// descUnknown is a tag this parser assigns no meaning to. It exists to prove the
// descriptor walk steps over what it does not know rather than stopping there.
func descUnknown(tag byte, n int) []byte {
	d := []byte{tag, byte(n)}
	for i := 0; i < n; i++ {
		d = append(d, 0x5A)
	}
	return d
}

func descs(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// --- packet builders ------------------------------------------------------

// tsPacket assembles one 188-byte packet. payload is placed directly after the
// 4-byte header and padded to the end.
func tsPacket(pid uint16, pusi bool, cc uint8, pad byte, payload []byte) []byte {
	p := make([]byte, TSPacketSize)
	for i := range p {
		p[i] = pad
	}
	p[0] = SyncByte
	p[1] = byte((pid >> 8) & 0x1F)
	if pusi {
		p[1] |= 0x40
	}
	p[2] = byte(pid & 0xFF)
	p[3] = 0x10 | (cc & 0x0F) // adaptation_field_control = payload only
	if len(payload) > TSPacketSize-4 {
		panic("psi corpus: payload does not fit one packet")
	}
	copy(p[4:], payload)
	return p
}

// psiPackets splits one section into as many packets as it needs, with the
// pointer field on the first and 0xFF stuffing after the last byte.
func psiPackets(pid uint16, startCC uint8, pointerField int, section []byte) [][]byte {
	first := append([]byte{byte(pointerField)}, make([]byte, pointerField)...)
	for i := 1; i <= pointerField; i++ {
		first[i] = 0xFF
	}
	body := append(first, section...)

	var out [][]byte
	cc := startCC
	pusi := true
	for len(body) > 0 {
		n := TSPacketSize - 4
		if n > len(body) {
			n = len(body)
		}
		out = append(out, tsPacket(pid, pusi, cc, 0xFF, body[:n]))
		body = body[n:]
		cc = (cc + 1) & 0x0F
		pusi = false
	}
	return out
}

// psiPacketRaw places already-assembled payload bytes into one packet without
// adding a pointer field, for the cases that need to control the payload
// exactly - a section starting mid-packet behind another one, for instance.
func psiPacketRaw(pid uint16, pusi bool, cc uint8, payload []byte) []byte {
	return tsPacket(pid, pusi, cc, 0xFF, payload)
}

// nullPacket is the PID 0x1FFF filler a real transport is mostly made of. It
// carries no PSI and must change nothing.
func nullPacket(cc uint8) []byte {
	return tsPacket(0x1FFF, false, cc, 0xFF, nil)
}

func chunkOf(packets ...[]byte) []byte {
	var out []byte
	for _, p := range packets {
		out = append(out, p...)
	}
	return out
}

func flatten(groups ...[][]byte) [][]byte {
	var out [][]byte
	for _, g := range groups {
		out = append(out, g...)
	}
	return out
}

// --- stream shorthands ----------------------------------------------------

const (
	psiPMTPID1   = 0x0100
	psiPMTPID2   = 0x0200
	psiPMTPID3   = 0x0300
	psiVideoPID  = 0x0101
	psiVideoPID2 = 0x0111
	psiAudioPID1 = 0x0102
	psiAudioPID2 = 0x0103
	psiTSID      = 0x0001
)

func esH264(pid uint16) pmtStream  { return pmtStream{streamType: 0x1B, pid: pid} }
func esHEVC(pid uint16) pmtStream  { return pmtStream{streamType: 0x24, pid: pid} }
func esMPEG2(pid uint16) pmtStream { return pmtStream{streamType: 0x02, pid: pid} }

func esAC3(pid uint16, d []byte) pmtStream {
	return pmtStream{streamType: 0x06, pid: pid, descriptors: d}
}

func esMP2(pid uint16, d []byte) pmtStream {
	return pmtStream{streamType: 0x03, pid: pid, descriptors: d}
}

func esAAC(pid uint16, d []byte) pmtStream {
	return pmtStream{streamType: 0x0F, pid: pid, descriptors: d}
}

// esPrivate is stream type 0x06 without anything that says it carries audio -
// the type subtitles and teletext share with AC-3.
func esPrivate(pid uint16, d []byte) pmtStream {
	return pmtStream{streamType: 0x06, pid: pid, descriptors: d}
}

// --- case builder ---------------------------------------------------------

type psiCaseBuilder struct {
	c      psiCorpusCase
	offset int64
}

func psiCase(name, desc string, initial uint16) *psiCaseBuilder {
	return &psiCaseBuilder{c: psiCorpusCase{name: name, desc: desc, initial: initial}}
}

// chunk appends one Ingest call. ProcessedThroughOffset is filled in from the
// running offset rather than authored per step: it is mechanical, and the claim
// it encodes - a core consumes exactly what it was given - is authored once, in
// TestPSI_ProcessedThroughOffsetIsTheWholeChunk.
func (b *psiCaseBuilder) chunk(packets [][]byte, want psiExpect) *psiCaseBuilder {
	data := chunkOf(packets...)
	b.offset += int64(len(data))
	want.through = b.offset
	b.c.steps = append(b.c.steps, psiStep{kind: psiStepChunk, chunk: data, want: want})
	return b
}

// target appends a SetTargetProgram call. It reports through=0: it interprets no
// bytes and has no position in the byte stream.
func (b *psiCaseBuilder) target(n uint16, want psiExpect) *psiCaseBuilder {
	want.through = 0
	b.c.steps = append(b.c.steps, psiStep{kind: psiStepTarget, target: n, want: want})
	return b
}

// note marks the case as recording unadjudicated behaviour. See psiCorpusCase.note.
func (b *psiCaseBuilder) noting(text string) *psiCaseBuilder {
	b.c.note = text
	return b
}

func (b *psiCaseBuilder) done() psiCorpusCase { return b.c }

// --- expectation shorthands -----------------------------------------------

func trackAC3(pid uint16, lang string, ct byte, channels int, multi bool) psiTrackExpect {
	return psiTrackExpect{
		pid: pid, streamType: 0x06, codec: "ac3", lang: lang,
		channels: channels, multichannel: multi,
		componentType: ct, hasComponentType: true,
	}
}

var noEvents []string

func identity(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "identity"
	}
	return out
}

// --- group A: section assembly --------------------------------------------

// bigPAT is a PAT whose section does not fit one packet: 45 programs put it at
// 192 bytes, and the target is the last entry so the selection has to survive
// the join rather than being decided by the first packet alone.
func bigPATSection(version uint8, targetPID uint16) []byte {
	programs := make([]patProgram, 0, 45)
	for i := 0; i < 44; i++ {
		programs = append(programs, patProgram{number: uint16(100 + i), pid: uint16(0x0400 + i)})
	}
	programs = append(programs, patProgram{number: 1, pid: targetPID})
	return patSection(psiTSID, version, 0, 0, 1, programs...)
}

// bigPAT3Section needs three packets: 97 programs put the section at 400 bytes,
// so the middle packet is a continuation with neither the start nor the end of
// the section in it.
func bigPAT3Section(targetPID uint16) []byte {
	programs := make([]patProgram, 0, 97)
	programs = append(programs, patProgram{number: 1, pid: targetPID})
	for i := 0; i < 96; i++ {
		programs = append(programs, patProgram{number: uint16(100 + i), pid: uint16(0x0400 + i)})
	}
	s := patSection(psiTSID, 0, 0, 0, 1, programs...)
	if len(s) != 400 {
		panic("psi corpus: the three-packet PAT is no longer 400 bytes")
	}
	return s
}

// pat364Section is sized so that its first packet leaves exactly 181 bytes,
// which puts the section after it 2 bytes from the end of the next payload -
// the split that used to carry a section past the table_id check.
func pat364Section(targetPID uint16) []byte {
	programs := []patProgram{{1, targetPID}}
	for i := 0; i < 87; i++ {
		programs = append(programs, patProgram{number: uint16(100 + i), pid: uint16(0x0400 + i)})
	}
	s := patSection(psiTSID, 0, 0, 0, 1, programs...)
	if len(s) != 364 {
		panic("psi corpus: the split-header PAT is no longer 364 bytes")
	}
	return s
}

// foreignTableSection is well formed in every respect except that it announces
// another table. Its CRC is recomputed so that only the table_id distinguishes
// it from a PAT that would move the programme to a different PMT PID.
func foreignTableSection(tableID byte, pmtPID uint16) []byte {
	s := patSection(psiTSID, 1, 0, 0, 1, patProgram{1, pmtPID})
	s[0] = tableID
	crc := psiCRC32(s[:len(s)-4])
	s[len(s)-4], s[len(s)-3] = byte(crc>>24), byte(crc>>16)
	s[len(s)-2], s[len(s)-1] = byte(crc>>8), byte(crc)
	return s
}

// pmtOverreachingESInfo is a PMT whose last elementary stream entry declares
// four descriptor bytes that are not inside the elementary stream loop: the
// four bytes after it are the section's own CRC.
//
// The CRC is steered until it begins with a descriptor tag the parser accepts,
// so that a bound of len(sData) and a bound of esEnd give visibly different
// answers. Without that collision both bounds agree that there is no track, and
// the case would not tell them apart.
func pmtOverreachingESInfo() []byte {
	for filler := 0; filler < 1<<20; filler++ {
		pi := []byte{0x09, 0x03, byte(filler >> 16), byte(filler >> 8), byte(filler)}
		sec := pmtSection(1, psiVideoPID, 0, 0, 0, 1, pi,
			esH264(psiVideoPID),
			esAC3(psiAudioPID1, descAC3(0x02)),
		)
		// Drop the four descriptor bytes, leave ES_info_length claiming them, and
		// shrink section_length so the section is otherwise well formed.
		out := append(cloneSlice(sec[:len(sec)-8]), 0, 0, 0, 0)
		secLen := len(out) - 3
		out[1] = 0xB0 | byte((secLen>>8)&0x0F)
		out[2] = byte(secLen)
		crc := psiCRC32(out[:len(out)-4])
		out[len(out)-4], out[len(out)-3] = byte(crc>>24), byte(crc>>16)
		out[len(out)-2], out[len(out)-1] = byte(crc>>8), byte(crc)

		switch byte(crc >> 24) {
		case descriptorAC3, descriptorEnhAC3, descriptorDTS, descriptorAAC, descriptorDTSHD:
		default:
			continue
		}
		if byte(crc>>16) > 2 {
			continue
		}
		return out
	}
	panic("psi corpus: no section CRC that reads as a descriptor tag")
}

// shortValidCRCSection is 11 bytes: one short of the 12 the parser needs to read
// the fields it reads, but with a correct CRC and a last_section_number of 0, so
// that the length check is the only thing standing between it and a PAT scan
// that would slice sData[8:len-4] backwards.
func shortValidCRCSection() []byte {
	for a := 0; a < 1<<16; a++ {
		s := appendCRC([]byte{0x00, 0xB0, 0x08, byte(a >> 8), byte(a), 0xC1, 0x00})
		// byte 7, the last_section_number the parser would read, is the first CRC
		// byte here. It has to be 0 or the table never completes and the case
		// would prove nothing.
		if s[7] == 0x00 {
			return s
		}
	}
	panic("psi corpus: no 11-byte section with a zero last_section_number")
}

// descAC3FlagClear declares two body bytes with component_type_flag clear. The
// second byte looks like a usable component type and must not be read as one.
func descAC3FlagClear() []byte {
	return []byte{descriptorAC3, 0x02, 0x00, 0x82}
}

// bigPMTSection carries a large program_info block, which is what pushes a real
// PMT past one packet.
func bigPMTSection(version uint8) []byte {
	return pmtSection(1, psiVideoPID, version, 0, 0, 1, descUnknown(0x09, 180),
		esH264(psiVideoPID),
		esAC3(psiAudioPID1, descs(descLanguage("deu", 0x00), descAC3(0x02))))
}

func psiAssemblyCases() []psiCorpusCase {
	basePAT := patSection(psiTSID, 0, 0, 0, 1, patProgram{1, psiPMTPID1})
	basePMT := pmtSection(1, psiVideoPID, 0, 0, 0, 1, nil,
		esH264(psiVideoPID),
		esAC3(psiAudioPID1, descs(descLanguage("deu", 0x00), descAC3(0x02))))
	baseFacts := psiExpect{
		hasPAT: true, hasPMT: true, pmtVersion: 0, programNumber: 1,
		pmtPID: psiPMTPID1, videoPID: psiVideoPID, videoCodec: CodecH264,
		audioPIDs: []uint16{psiAudioPID1},
		tracks:    []psiTrackExpect{trackAC3(psiAudioPID1, "deu", 0x02, 2, false)},
	}

	return []psiCorpusCase{
		func() psiCorpusCase {
			w := baseFacts
			w.events, w.patPackets, w.pmtPackets = identity(2), []int{0}, []int{1}
			return psiCase("pat_then_pmt_in_one_chunk",
				"the PAT names the PMT PID and the PMT names the streams; each is one identity change",
				1).
				chunk(flatten(psiPackets(0, 0, 0, basePAT), psiPackets(psiPMTPID1, 0, 0, basePMT)), w).
				done()
		}(),

		func() psiCorpusCase {
			afterPAT := psiExpect{
				hasPAT: true, pmtPID: psiPMTPID1, videoCodec: CodecUnknown,
				events: identity(1), patPackets: []int{0},
			}
			w := baseFacts
			w.events, w.patPackets, w.pmtPackets = identity(1), []int{0}, []int{1}
			return psiCase("pat_and_pmt_arrive_in_separate_chunks",
				"a chunk boundary between the two tables changes nothing about either",
				1).
				chunk(psiPackets(0, 0, 0, basePAT), afterPAT).
				chunk(psiPackets(psiPMTPID1, 0, 0, basePMT), w).
				done()
		}(),

		func() psiCorpusCase {
			w := baseFacts
			w.events, w.patPackets, w.pmtPackets = identity(2), []int{0, 1}, []int{2}
			return psiCase("a_pat_section_split_across_two_packets",
				"a 192-byte PAT is joined from both packets, and both are kept as the raw table",
				1).
				chunk(flatten(
					psiPackets(0, 0, 0, bigPATSection(0, psiPMTPID1)),
					psiPackets(psiPMTPID1, 0, 0, basePMT)), w).
				done()
		}(),

		func() psiCorpusCase {
			w := baseFacts
			w.events, w.patPackets, w.pmtPackets = identity(2), []int{0}, []int{1, 2}
			return psiCase("a_pmt_section_split_across_two_packets",
				"a PMT with a large program_info block spans two packets and both are the raw table",
				1).
				chunk(flatten(
					psiPackets(0, 0, 0, basePAT),
					psiPackets(psiPMTPID1, 0, 0, bigPMTSection(0))), w).
				done()
		}(),

		func() psiCorpusCase {
			pmtPkts := psiPackets(psiPMTPID1, 0, 0, bigPMTSection(0))
			afterFirst := psiExpect{
				hasPAT: true, pmtPID: psiPMTPID1, videoCodec: CodecUnknown,
				events: identity(1), patPackets: []int{0},
			}
			w := baseFacts
			w.events, w.patPackets, w.pmtPackets = identity(1), []int{0}, []int{1, 2}
			return psiCase("a_section_spans_the_chunk_boundary",
				"half a PMT in one chunk and half in the next is the same table as one chunk carrying both",
				1).
				chunk(flatten(psiPackets(0, 0, 0, basePAT), pmtPkts[:1]), afterFirst).
				chunk(pmtPkts[1:], w).
				done()
		}(),

		func() psiCorpusCase {
			// Packet 1 carries the tail of section A behind a pointer field, then
			// a whole new section. Section B moves the program to another PMT PID
			// so that its acceptance is visible rather than inferred.
			sectionA := bigPATSection(0, psiPMTPID1)
			sectionB := patSection(psiTSID, 1, 0, 0, 1, patProgram{1, psiPMTPID2})
			first := psiPackets(0, 0, 0, sectionA)[0]
			tail := sectionA[183:]
			payload := append([]byte{byte(len(tail))}, tail...)
			payload = append(payload, sectionB...)
			second := psiPacketRaw(0, true, 1, payload)

			return psiCase("a_non_zero_pointer_field_completes_the_previous_section",
				"the bytes before the pointer finish the section in flight; the section after it is a new table",
				1).
				chunk([][]byte{first, second}, psiExpect{
					hasPAT: true, pmtPID: psiPMTPID2, videoCodec: CodecUnknown,
					events: identity(2), patPackets: []int{1},
				}).
				done()
		}(),

		func() psiCorpusCase {
			// Two sections of one PAT inside a single packet. The table activates
			// on the second, and the packet is listed once, not twice.
			secA := patSection(psiTSID, 0, 0, 1, 1, patProgram{9, 0x0900})
			secB := patSection(psiTSID, 0, 1, 1, 1, patProgram{1, psiPMTPID1})
			payload := append([]byte{0x00}, append(secA, secB...)...)

			return psiCase("two_complete_sections_in_one_packet",
				"both sections of a two-section PAT in one packet complete the table, and the packet is not listed twice",
				1).
				chunk([][]byte{psiPacketRaw(0, true, 0, payload)}, psiExpect{
					hasPAT: true, pmtPID: psiPMTPID1, videoCodec: CodecUnknown,
					events: identity(1), patPackets: []int{0},
				}).
				done()
		}(),

		func() psiCorpusCase {
			pkts := psiPackets(0, 0, 0, bigPAT3Section(psiPMTPID1))
			if len(pkts) != 3 {
				panic("psi corpus: expected a three-packet PAT")
			}
			return psiCase("a_duplicate_of_a_continuation_packet_is_ignored",
				"a repeated middle packet must not be read as a glitch: doing so would throw away the section it is part of",
				1).
				chunk([][]byte{pkts[0], pkts[1], pkts[1], pkts[2]}, psiExpect{
					hasPAT: true, pmtPID: psiPMTPID1, videoCodec: CodecUnknown,
					events: identity(1), patPackets: []int{0, 1, 3},
				}).
				done()
		}(),

		psiCase("a_psi_payload_of_pure_stuffing_changes_nothing",
			"a pointer field followed only by 0xFF is a packet with no section in it",
			1).
			chunk([][]byte{psiPacketRaw(0, true, 0, []byte{0x00})},
				psiExpect{videoCodec: CodecUnknown, events: noEvents}).
			done(),

		psiCase("null_packets_change_nothing",
			"PID 0x1FFF carries no table and can never be mistaken for one",
			1).
			chunk([][]byte{nullPacket(0), nullPacket(1), nullPacket(2)},
				psiExpect{videoCodec: CodecUnknown, events: noEvents}).
			done(),

		func() psiCorpusCase {
			w := baseFacts
			w.events, w.patPackets, w.pmtPackets = identity(2), []int{1}, []int{2}
			return psiCase("a_pmt_packet_before_any_pat_is_ignored",
				"until a PAT names the PMT PID, a packet on it is just another elementary stream",
				1).
				chunk(psiPackets(psiPMTPID1, 0, 0, basePMT),
					psiExpect{videoCodec: CodecUnknown, events: noEvents}).
				chunk(flatten(psiPackets(0, 0, 0, basePAT), psiPackets(psiPMTPID1, 1, 0, basePMT)), w).
				done()
		}(),
	}
}

// --- group B: what a section has to be before it counts -------------------

func corruptLastByte(section []byte) []byte {
	out := cloneSlice(section)
	out[len(out)-1] ^= 0xFF
	return out
}

func psiRejectionCases() []psiCorpusCase {
	basePAT := patSection(psiTSID, 0, 0, 0, 1, patProgram{1, psiPMTPID1})
	accepted := psiExpect{
		hasPAT: true, pmtPID: psiPMTPID1, videoCodec: CodecUnknown,
		events: identity(1),
	}
	nothing := psiExpect{videoCodec: CodecUnknown, events: noEvents}

	return []psiCorpusCase{
		func() psiCorpusCase {
			w := accepted
			w.patPackets = []int{1}
			return psiCase("a_section_with_a_bad_crc_is_ignored",
				"a corrupted section is not a table, and the good one after it still is",
				1).
				chunk(psiPackets(0, 0, 0, corruptLastByte(basePAT)), nothing).
				chunk(psiPackets(0, 1, 0, basePAT), w).
				done()
		}(),

		func() psiCorpusCase {
			w := accepted
			w.patPackets = []int{1}
			return psiCase("a_section_marked_not_current_is_ignored",
				"current_next_indicator=0 describes a table that is not in force yet",
				1).
				chunk(psiPackets(0, 0, 0, patSection(psiTSID, 0, 0, 0, 0, patProgram{1, psiPMTPID1})), nothing).
				chunk(psiPackets(0, 1, 0, basePAT), w).
				done()
		}(),

		psiCase("a_truncated_section_never_completes",
			"half a section is not a table however long the stream stays quiet afterwards",
			1).
			chunk([][]byte{psiPackets(0, 0, 0, bigPATSection(0, psiPMTPID1))[0], nullPacket(0)}, nothing).
			done(),

		psiCase("a_section_length_that_never_arrives_leaves_the_table_inactive",
			"a header claiming 1021 more bytes waits for them and reports nothing meanwhile",
			1).
			chunk([][]byte{psiPacketRaw(0, true, 0, []byte{0x00, 0x00, 0xB3, 0xFD}), nullPacket(0)}, nothing).
			done(),

		psiCase("a_section_too_short_to_hold_its_fields_is_dropped",
			"eleven bytes with a correct CRC is still not a section: the fields the parser reads are not all there",
			1).
			chunk(psiPackets(0, 0, 0, shortValidCRCSection()), nothing).
			done(),

		func() psiCorpusCase {
			// A section numbered outside the range the table declares. The count of
			// stored sections reaches last_section_number+1, so a completeness test
			// that only counted would call the table complete with section 1 absent.
			sec0 := patSection(psiTSID, 0, 0, 1, 1, patProgram{1, psiPMTPID1})
			sec5 := patSection(psiTSID, 0, 5, 1, 1, patProgram{9, 0x0900})
			return psiCase("a_section_numbered_outside_the_table_does_not_complete_it",
				"two sections arrived and the table declares two, but section 1 is still missing and section 5 does not stand in for it",
				1).
				chunk(flatten(psiPackets(0, 0, 0, sec0), psiPackets(0, 1, 0, sec5)), nothing).
				done()
		}(),

		func() psiCorpusCase {
			// A well-formed section in every respect except its table_id, arriving
			// whole inside one payload so the scanner sees its header.
			fake := patSection(psiTSID, 0, 0, 0, 1, patProgram{1, psiPMTPID1})
			fake[0] = 0x42 // service_description_section
			crc := psiCRC32(fake[:len(fake)-4])
			fake[len(fake)-4], fake[len(fake)-3] = byte(crc>>24), byte(crc>>16)
			fake[len(fake)-2], fake[len(fake)-1] = byte(crc>>8), byte(crc)
			return psiCase("a_section_of_another_table_on_the_pat_pid_is_not_a_pat",
				"a valid CRC says the bytes are intact, not that they are the table this PID is being read for",
				1).
				chunk(psiPackets(0, 0, 0, fake), nothing).
				done()
		}(),

		func() psiCorpusCase {
			sectionA := pat364Section(psiPMTPID1)
			fake := foreignTableSection(0x42, psiPMTPID2)
			payload1 := append([]byte{181}, sectionA[183:]...)
			payload1 = append(payload1, fake[:2]...)
			if len(payload1) != TSPacketSize-4 {
				panic("psi corpus: the split-header payload is not a full payload")
			}
			return psiCase("a_section_of_another_table_split_across_packets_is_not_a_pat",
				"the table_id of a section whose header arrived in two packets is checked on the completed section, not skipped because the scan never saw it",
				1).
				chunk([][]byte{
					psiPackets(0, 0, 0, sectionA)[0],
					psiPacketRaw(0, true, 1, payload1),
					psiPacketRaw(0, false, 2, fake[2:]),
				}, psiExpect{
					hasPAT: true, pmtPID: psiPMTPID1, videoCodec: CodecUnknown,
					events: identity(1), patPackets: []int{0, 1},
				}).
				done()
		}(),

		func() psiCorpusCase {
			return psiCase("an_es_entry_declaring_more_descriptors_than_the_loop_holds_is_not_read",
				"an entry that runs past the elementary stream loop ends the walk; the section's own CRC bytes are not its descriptors",
				1).
				chunk(flatten(
					psiPackets(0, 0, 0, patSection(psiTSID, 0, 0, 0, 1, patProgram{1, psiPMTPID1})),
					psiPackets(psiPMTPID1, 0, 0, pmtOverreachingESInfo())), psiExpect{
					hasPAT: true, hasPMT: true, programNumber: 1, pmtPID: psiPMTPID1,
					videoPID: psiVideoPID, videoCodec: CodecH264,
					events: identity(2), patPackets: []int{0}, pmtPackets: []int{1},
				}).
				done()
		}(),

		func() psiCorpusCase {
			// A foreign section standing in front of a real one, both whole inside
			// the same payload.
			payload := append([]byte{0x00}, foreignTableSection(0x42, psiPMTPID2)...)
			payload = append(payload, patSection(psiTSID, 0, 0, 0, 1, patProgram{1, psiPMTPID1})...)
			return psiCase("a_foreign_table_ends_the_scan_of_the_payload_it_is_in",
				"the scan stops at the first section that is not the table this PID is read for, and does not step over it to the one behind",
				1).
				chunk([][]byte{psiPacketRaw(0, true, 0, payload)}, nothing).
				noting("This pins that the scan STOPS rather than skipping the foreign section and " +
					"continuing. Sections are self-delimiting, so stepping over one and reading the " +
					"PAT behind it would also be defensible and is arguably closer to 13818-1. The " +
					"reference has always stopped; that has not been adjudicated. Kept as a case " +
					"because the scan-level table_id check is otherwise unobservable now that the " +
					"completed-section check refuses the foreign section anyway.").
				done()
		}(),

		psiCase("a_zero_length_section_is_dropped",
			"a section too short to hold the fields the parser reads is discarded, not read past",
			1).
			chunk([][]byte{psiPacketRaw(0, true, 0, []byte{0x00, 0x00, 0xB0, 0x00})}, nothing).
			done(),

		func() psiCorpusCase {
			sectionA := bigPATSection(0, psiPMTPID1)
			w := accepted
			w.patPackets = []int{2}
			return psiCase("a_continuity_gap_aborts_an_in_flight_section",
				"a jump in continuity_counter means the missing packet may have carried section bytes",
				1).
				chunk([][]byte{
					psiPackets(0, 0, 0, sectionA)[0],
					psiPacketRaw(0, false, 5, sectionA[183:]),
				}, nothing).
				chunk(psiPackets(0, 6, 0, basePAT), w).
				done()
		}(),

		func() psiCorpusCase {
			pkts := psiPackets(0, 0, 0, bigPATSection(0, psiPMTPID1))
			w := accepted
			w.patPackets = []int{0, 2}
			return psiCase("an_exact_duplicate_packet_is_ignored",
				"a repeated packet with the repeated counter is the transport saying the same thing twice, not a discontinuity",
				1).
				chunk([][]byte{pkts[0], pkts[0], pkts[1]}, w).
				done()
		}(),

		func() psiCorpusCase {
			pkts := psiPackets(0, 0, 0, bigPATSection(0, psiPMTPID1))
			w := accepted
			w.patPackets = []int{3}
			return psiCase("the_same_counter_with_different_bytes_resets_assembly",
				"one counter value cannot describe two different packets, so what was in flight is unusable",
				1).
				chunk([][]byte{pkts[0], psiPacketRaw(0, false, 0, []byte{0xAA, 0xBB}), pkts[1]}, nothing).
				chunk(psiPackets(0, 2, 0, basePAT), w).
				done()
		}(),
	}
}

// --- group C: table lifecycle ---------------------------------------------

func psiLifecycleCases() []psiCorpusCase {
	basePAT := patSection(psiTSID, 0, 0, 0, 1, patProgram{1, psiPMTPID1})
	pmtV := func(version uint8, streams ...pmtStream) []byte {
		return pmtSection(1, psiVideoPID, version, 0, 0, 1, nil, streams...)
	}
	ac3One := esAC3(psiAudioPID1, descs(descLanguage("deu", 0x00), descAC3(0x02)))
	ac3Two := esAC3(psiAudioPID2, descs(descLanguage("eng", 0x00), descAC3(0x02)))

	full := func(version uint8, pids []uint16, tracks []psiTrackExpect) psiExpect {
		return psiExpect{
			hasPAT: true, hasPMT: true, pmtVersion: version, programNumber: 1,
			pmtPID: psiPMTPID1, videoPID: psiVideoPID, videoCodec: CodecH264,
			audioPIDs: pids, tracks: tracks,
		}
	}
	oneTrack := []psiTrackExpect{trackAC3(psiAudioPID1, "deu", 0x02, 2, false)}
	twoTracks := []psiTrackExpect{
		trackAC3(psiAudioPID1, "deu", 0x02, 2, false),
		trackAC3(psiAudioPID2, "eng", 0x02, 2, false),
	}

	return []psiCorpusCase{
		func() psiCorpusCase {
			a := full(0, []uint16{psiAudioPID1}, oneTrack)
			a.events, a.patPackets, a.pmtPackets = identity(2), []int{0}, []int{1}
			b := full(1, []uint16{psiAudioPID1, psiAudioPID2}, twoTracks)
			b.events, b.patPackets, b.pmtPackets = identity(1), []int{0}, []int{2}
			return psiCase("a_new_pmt_version_on_the_same_pid_is_a_new_program_identity",
				"the version is the stream's own statement that the table changed",
				1).
				chunk(flatten(psiPackets(0, 0, 0, basePAT), psiPackets(psiPMTPID1, 0, 0, pmtV(0, esH264(psiVideoPID), ac3One))), a).
				chunk(psiPackets(psiPMTPID1, 1, 0, pmtV(1, esH264(psiVideoPID), ac3One, ac3Two)), b).
				done()
		}(),

		func() psiCorpusCase {
			a := full(31, []uint16{psiAudioPID1}, oneTrack)
			a.events, a.patPackets, a.pmtPackets = identity(2), []int{0}, []int{1}
			b := full(0, []uint16{psiAudioPID1}, oneTrack)
			b.events, b.patPackets, b.pmtPackets = identity(1), []int{0}, []int{2}
			return psiCase("a_pmt_version_wrapping_from_31_to_0_is_still_a_change",
				"the version is five bits and wraps; 0 after 31 is the next table, not the first one again",
				1).
				chunk(flatten(psiPackets(0, 0, 0, basePAT), psiPackets(psiPMTPID1, 0, 0, pmtV(31, esH264(psiVideoPID), ac3One))), a).
				chunk(psiPackets(psiPMTPID1, 1, 0, pmtV(0, esH264(psiVideoPID), ac3One)), b).
				done()
		}(),

		func() psiCorpusCase {
			a := full(0, []uint16{psiAudioPID1}, oneTrack)
			a.events, a.patPackets, a.pmtPackets = identity(2), []int{0}, []int{1}
			b := full(0, []uint16{psiAudioPID1}, oneTrack)
			b.events, b.patPackets, b.pmtPackets = noEvents, []int{2}, []int{3}
			return psiCase("the_same_table_delivered_again_is_not_a_change",
				"a carousel repeats every table on a schedule; a repeat must not look like a new program",
				1).
				chunk(flatten(psiPackets(0, 0, 0, basePAT), psiPackets(psiPMTPID1, 0, 0, pmtV(0, esH264(psiVideoPID), ac3One))), a).
				chunk(flatten(psiPackets(0, 1, 0, basePAT), psiPackets(psiPMTPID1, 1, 0, pmtV(0, esH264(psiVideoPID), ac3One))), b).
				done()
		}(),

		func() psiCorpusCase {
			a := full(0, []uint16{psiAudioPID1}, oneTrack)
			a.events, a.patPackets, a.pmtPackets = identity(2), []int{0}, []int{1}
			b := full(0, []uint16{psiAudioPID1}, oneTrack)
			b.programNumber = 7
			b.events, b.patPackets, b.pmtPackets = identity(1), []int{0}, []int{2}
			return psiCase("a_pmt_naming_another_program_number_on_the_same_pid_is_taken_as_a_change",
				"the program number in the section differs from the one the PAT pointed here for",
				1).
				chunk(flatten(psiPackets(0, 0, 0, basePAT), psiPackets(psiPMTPID1, 0, 0, pmtV(0, esH264(psiVideoPID), ac3One))), a).
				chunk(psiPackets(psiPMTPID1, 1, 0,
					pmtSection(7, psiVideoPID, 0, 0, 0, 1, nil, esH264(psiVideoPID), ac3One)), b).
				noting("The reference accepts a PMT whose program_number is not the one the PAT " +
					"routed to this PID, and reports that number as the program identity. Whether a " +
					"PMT for another program should instead be ignored has not been decided.").
				done()
		}(),

		func() psiCorpusCase {
			a := full(0, []uint16{psiAudioPID1}, oneTrack)
			a.events, a.patPackets, a.pmtPackets = identity(2), []int{0}, []int{1}
			b := psiExpect{
				hasPAT: true, hasPMT: true, pmtVersion: 0, programNumber: 1,
				pmtPID: psiPMTPID2, videoPID: psiVideoPID, videoCodec: CodecH264,
				audioPIDs: []uint16{psiAudioPID1}, tracks: oneTrack,
				events: identity(2), patPackets: []int{2}, pmtPackets: []int{3},
			}
			return psiCase("a_new_pat_moves_the_program_to_another_pmt_pid",
				"the PMT PID is a property of the PAT, and following it discards everything the old one said",
				1).
				chunk(flatten(psiPackets(0, 0, 0, basePAT), psiPackets(psiPMTPID1, 0, 0, pmtV(0, esH264(psiVideoPID), ac3One))), a).
				chunk(flatten(
					psiPackets(0, 1, 0, patSection(psiTSID, 1, 0, 0, 1, patProgram{1, psiPMTPID2})),
					psiPackets(psiPMTPID2, 0, 0, pmtV(0, esH264(psiVideoPID), ac3One))), b).
				done()
		}(),

		func() psiCorpusCase {
			pmtV9 := pmtSection(1, psiVideoPID, 9, 0, 0, 1, nil, esH264(psiVideoPID))
			return psiCase("a_pat_that_moves_the_pmt_pid_forgets_the_old_program_identity",
				"the same fact as a target switch, reached the other way: no PMT means no program number and no version, whatever named the new PID",
				1).
				chunk(flatten(psiPackets(0, 0, 0, basePAT), psiPackets(psiPMTPID1, 0, 0, pmtV9)), psiExpect{
					hasPAT: true, hasPMT: true, pmtVersion: 9, programNumber: 1, pmtPID: psiPMTPID1,
					videoPID: psiVideoPID, videoCodec: CodecH264,
					events: identity(2), patPackets: []int{0}, pmtPackets: []int{1},
				}).
				chunk(psiPackets(0, 1, 0, patSection(psiTSID, 1, 0, 0, 1, patProgram{1, psiPMTPID2})), psiExpect{
					hasPAT: true, pmtPID: psiPMTPID2, videoCodec: CodecUnknown,
					events: identity(1), patPackets: []int{2},
				}).
				done()
		}(),

		func() psiCorpusCase {
			sec0 := patSection(psiTSID, 0, 0, 1, 1, patProgram{9, 0x0900})
			sec1 := patSection(psiTSID, 0, 1, 1, 1, patProgram{1, psiPMTPID1})
			return psiCase("a_missing_section_prevents_the_table_from_activating",
				"a PAT is every one of its sections; one of two is not a table",
				1).
				chunk(psiPackets(0, 0, 0, sec0),
					psiExpect{videoCodec: CodecUnknown, events: noEvents}).
				chunk(psiPackets(0, 1, 0, sec1), psiExpect{
					hasPAT: true, pmtPID: psiPMTPID1, videoCodec: CodecUnknown,
					events: identity(1), patPackets: []int{0, 1},
				}).
				done()
		}(),

		func() psiCorpusCase {
			sec0 := patSection(psiTSID, 0, 0, 1, 1, patProgram{9, 0x0900})
			sec1 := patSection(psiTSID, 0, 1, 1, 1, patProgram{1, psiPMTPID1})
			return psiCase("a_carousel_repeat_of_one_section_keeps_the_table_complete",
				"section 0 arriving again replaces its slot; the table stays complete and its raw packets follow the replacement",
				1).
				chunk(flatten(psiPackets(0, 0, 0, sec0), psiPackets(0, 1, 0, sec1)), psiExpect{
					hasPAT: true, pmtPID: psiPMTPID1, videoCodec: CodecUnknown,
					events: identity(1), patPackets: []int{0, 1},
				}).
				chunk(psiPackets(0, 2, 0, sec0), psiExpect{
					hasPAT: true, pmtPID: psiPMTPID1, videoCodec: CodecUnknown,
					events: noEvents, patPackets: []int{2, 1},
				}).
				done()
		}(),

		func() psiCorpusCase {
			sec0v0 := patSection(psiTSID, 0, 0, 1, 1, patProgram{9, 0x0900})
			sec1v0 := patSection(psiTSID, 0, 1, 1, 1, patProgram{1, psiPMTPID1})
			sec0v1 := patSection(psiTSID, 1, 0, 1, 1, patProgram{9, 0x0900})
			sec1v1 := patSection(psiTSID, 1, 1, 1, 1, patProgram{1, psiPMTPID2})
			held := psiExpect{
				hasPAT: true, pmtPID: psiPMTPID1, videoCodec: CodecUnknown,
				events: noEvents, patPackets: []int{0, 1},
			}
			return psiCase("a_new_version_restarts_the_section_set_without_dropping_the_old_table",
				"a half-delivered new version is not yet a table, and the one in force stays in force until it is",
				1).
				chunk(flatten(psiPackets(0, 0, 0, sec0v0), psiPackets(0, 1, 0, sec1v0)), psiExpect{
					hasPAT: true, pmtPID: psiPMTPID1, videoCodec: CodecUnknown,
					events: identity(1), patPackets: []int{0, 1},
				}).
				chunk(psiPackets(0, 2, 0, sec0v1), held).
				chunk(psiPackets(0, 3, 0, sec1v1), psiExpect{
					hasPAT: true, pmtPID: psiPMTPID2, videoCodec: CodecUnknown,
					events: identity(1), patPackets: []int{2, 3},
				}).
				done()
		}(),
	}
}

// --- group D: which program the core follows ------------------------------

func psiSelectionCases() []psiCorpusCase {
	pmt1 := pmtSection(1, psiVideoPID, 0, 0, 0, 1, nil, esH264(psiVideoPID))
	pmt2 := pmtSection(2, psiVideoPID, 0, 0, 0, 1, nil, esH264(psiVideoPID))
	multiPAT := patSection(psiTSID, 0, 0, 0, 1,
		patProgram{5, 0x0500}, patProgram{1, psiPMTPID1}, patProgram{2, psiPMTPID2})

	return []psiCorpusCase{
		psiCase("the_target_program_is_selected_out_of_several",
			"a transport carries many programs and the PAT entry for the one asked for is the only one that matters",
			1).
			chunk(psiPackets(0, 0, 0, multiPAT), psiExpect{
				hasPAT: true, pmtPID: psiPMTPID1, videoCodec: CodecUnknown,
				events: identity(1), patPackets: []int{0},
			}).
			done(),

		psiCase("a_pat_without_the_target_program_names_no_pmt_pid",
			"a PAT that does not carry the program asked for is not a PAT for this stream",
			1).
			chunk(psiPackets(0, 0, 0, patSection(psiTSID, 0, 0, 0, 1,
				patProgram{5, 0x0500}, patProgram{9, 0x0900})),
				psiExpect{videoCodec: CodecUnknown, events: noEvents}).
			chunk(psiPackets(0, 1, 0, multiPAT), psiExpect{
				hasPAT: true, pmtPID: psiPMTPID1, videoCodec: CodecUnknown,
				events: identity(1), patPackets: []int{1},
			}).
			done(),

		psiCase("program_number_zero_is_the_network_information_table",
			"entry 0 of a PAT points at the NIT, and following it would tune to a table instead of a service",
			1).
			chunk(psiPackets(0, 0, 0, patSection(psiTSID, 0, 0, 0, 1,
				patProgram{0, 0x0010}, patProgram{1, psiPMTPID1})),
				psiExpect{
					hasPAT: true, pmtPID: psiPMTPID1, videoCodec: CodecUnknown,
					events: identity(1), patPackets: []int{0},
				}).
			done(),

		psiCase("a_target_of_zero_follows_the_first_program_the_pat_names",
			"asked for no program in particular, the core takes the first real one and still skips the NIT",
			0).
			chunk(psiPackets(0, 0, 0, patSection(psiTSID, 0, 0, 0, 1,
				patProgram{0, 0x0010}, patProgram{3, psiPMTPID3}, patProgram{1, psiPMTPID1})),
				psiExpect{
					hasPAT: true, pmtPID: psiPMTPID3, videoCodec: CodecUnknown,
					events: identity(1), patPackets: []int{0},
				}).
			done(),

		func() psiCorpusCase {
			sec0 := patSection(psiTSID, 0, 0, 1, 1, patProgram{3, psiPMTPID3})
			sec1 := patSection(psiTSID, 0, 1, 1, 1, patProgram{5, psiPMTPID2})
			return psiCase("a_target_of_zero_takes_the_first_program_of_the_whole_table",
				"first means the table's own order - sections by section_number, entries as listed - and a later section does not get to re-choose",
				0).
				chunk(flatten(psiPackets(0, 0, 0, sec0), psiPackets(0, 1, 0, sec1)), psiExpect{
					hasPAT: true, pmtPID: psiPMTPID3, videoCodec: CodecUnknown,
					events: identity(1), patPackets: []int{0, 1},
				}).
				done()
		}(),

		func() psiCorpusCase {
			sec0 := patSection(psiTSID, 0, 0, 1, 1, patProgram{0, 0x0010})
			sec1 := patSection(psiTSID, 0, 1, 1, 1, patProgram{7, psiPMTPID2}, patProgram{9, psiPMTPID3})
			return psiCase("a_target_of_zero_reaches_into_a_later_section_when_the_first_names_only_the_nit",
				"a section that names no service is not a reason to stop looking, and the NIT is not a service",
				0).
				chunk(flatten(psiPackets(0, 0, 0, sec0), psiPackets(0, 1, 0, sec1)), psiExpect{
					hasPAT: true, pmtPID: psiPMTPID2, videoCodec: CodecUnknown,
					events: identity(1), patPackets: []int{0, 1},
				}).
				done()
		}(),

		func() psiCorpusCase {
			sec0 := patSection(psiTSID, 0, 0, 1, 1, patProgram{3, psiPMTPID3})
			sec1 := patSection(psiTSID, 0, 1, 1, 1, patProgram{5, psiPMTPID2})
			return psiCase("sections_delivered_out_of_order_are_read_in_section_number_order",
				"the table is assembled before it is read, so which packet arrived first decides neither the program nor the order of the raw table",
				0).
				chunk(flatten(psiPackets(0, 0, 0, sec1), psiPackets(0, 1, 0, sec0)), psiExpect{
					hasPAT: true, pmtPID: psiPMTPID3, videoCodec: CodecUnknown,
					events: identity(1), patPackets: []int{1, 0},
				}).
				done()
		}(),

		psiCase("switching_the_target_program_discards_everything_about_the_old_one",
			"a zap to another service on the same transport keeps no fact from the service left behind",
			1).
			chunk(flatten(psiPackets(0, 0, 0, multiPAT), psiPackets(psiPMTPID1, 0, 0, pmt1)), psiExpect{
				hasPAT: true, hasPMT: true, programNumber: 1, pmtPID: psiPMTPID1,
				videoPID: psiVideoPID, videoCodec: CodecH264,
				events: identity(2), patPackets: []int{0}, pmtPackets: []int{1},
			}).
			target(2, psiExpect{videoCodec: CodecUnknown, events: identity(1)}).
			chunk(flatten(psiPackets(0, 1, 0, multiPAT), psiPackets(psiPMTPID2, 0, 0, pmt2)), psiExpect{
				hasPAT: true, hasPMT: true, programNumber: 2, pmtPID: psiPMTPID2,
				videoPID: psiVideoPID, videoCodec: CodecH264,
				events: identity(2), patPackets: []int{2}, pmtPackets: []int{3},
			}).
			done(),

		func() psiCorpusCase {
			pmtV9 := pmtSection(1, psiVideoPID, 9, 0, 0, 1, nil, esH264(psiVideoPID))
			return psiCase("a_target_switch_forgets_the_program_number_and_version",
				"HasPMT, the program number and the table version are one fact, so a discarded program takes all three with it",
				1).
				chunk(flatten(psiPackets(0, 0, 0, multiPAT), psiPackets(psiPMTPID1, 0, 0, pmtV9)), psiExpect{
					hasPAT: true, hasPMT: true, pmtVersion: 9, programNumber: 1, pmtPID: psiPMTPID1,
					videoPID: psiVideoPID, videoCodec: CodecH264,
					events: identity(2), patPackets: []int{0}, pmtPackets: []int{1},
				}).
				target(2, psiExpect{videoCodec: CodecUnknown, events: identity(1)}).
				done()
		}(),

		psiCase("setting_the_target_to_the_program_already_followed_changes_nothing",
			"re-selecting the current program is not a zap and must not cost the stream its state",
			1).
			chunk(flatten(psiPackets(0, 0, 0, multiPAT), psiPackets(psiPMTPID1, 0, 0, pmt1)), psiExpect{
				hasPAT: true, hasPMT: true, programNumber: 1, pmtPID: psiPMTPID1,
				videoPID: psiVideoPID, videoCodec: CodecH264,
				events: identity(2), patPackets: []int{0}, pmtPackets: []int{1},
			}).
			target(1, psiExpect{
				hasPAT: true, hasPMT: true, programNumber: 1, pmtPID: psiPMTPID1,
				videoPID: psiVideoPID, videoCodec: CodecH264,
				events: noEvents, patPackets: []int{0}, pmtPackets: []int{1},
			}).
			done(),
	}
}

// --- group E: what the PMT says the program is made of --------------------

func psiElementaryStreamCases() []psiCorpusCase {
	basePAT := patSection(psiTSID, 0, 0, 0, 1, patProgram{1, psiPMTPID1})
	pmtV := func(version uint8, streams ...pmtStream) []byte {
		return pmtSection(1, psiVideoPID, version, 0, 0, 1, nil, streams...)
	}
	video := func(version uint8, pid uint16, codec VideoCodec, step int) psiExpect {
		return psiExpect{
			hasPAT: true, hasPMT: true, pmtVersion: version, programNumber: 1,
			pmtPID: psiPMTPID1, videoPID: pid, videoCodec: codec,
			events: identity(1), patPackets: []int{0}, pmtPackets: []int{step},
		}
	}

	ac3Stereo := descs(descLanguage("deu", 0x00), descAC3(0x02))
	eac3Multi := descs(descLanguage("eng", 0x00), descEAC3(0x85))
	aacStereo := descs(descLanguage("fra", 0x00), descAAC(0x03))

	return []psiCorpusCase{
		func() psiCorpusCase {
			c := psiCase("every_video_stream_type_the_pmt_can_declare",
				"H.264, both HEVC stream types, MPEG-2 and MPEG-1 each name a codec; the PID follows the table",
				1)
			first := video(0, psiVideoPID, CodecH264, 1)
			first.events = identity(2)
			c.chunk(flatten(psiPackets(0, 0, 0, basePAT),
				psiPackets(psiPMTPID1, 0, 0, pmtV(0, esH264(psiVideoPID)))), first)
			c.chunk(psiPackets(psiPMTPID1, 1, 0, pmtV(1, esHEVC(psiVideoPID))),
				video(1, psiVideoPID, CodecH265, 2))
			c.chunk(psiPackets(psiPMTPID1, 2, 0, pmtV(2, pmtStream{streamType: 0x27, pid: psiVideoPID})),
				video(2, psiVideoPID, CodecH265, 3))
			c.chunk(psiPackets(psiPMTPID1, 3, 0, pmtV(3, esMPEG2(psiVideoPID))),
				video(3, psiVideoPID, CodecMPEG2, 4))
			c.chunk(psiPackets(psiPMTPID1, 4, 0, pmtV(4, pmtStream{streamType: 0x01, pid: psiVideoPID})),
				video(4, psiVideoPID, CodecMPEG2, 5))
			return c.done()
		}(),

		func() psiCorpusCase {
			first := video(0, psiVideoPID, CodecH264, 1)
			first.events = identity(2)
			return psiCase("the_video_pid_moves_with_the_table",
				"a new PMT version can put the video on another PID, and the old one stops being video",
				1).
				chunk(flatten(psiPackets(0, 0, 0, basePAT),
					psiPackets(psiPMTPID1, 0, 0, pmtV(0, esH264(psiVideoPID)))), first).
				chunk(psiPackets(psiPMTPID1, 1, 0, pmtV(1, esH264(psiVideoPID2))),
					video(1, psiVideoPID2, CodecH264, 2)).
				done()
		}(),

		func() psiCorpusCase {
			first := video(0, psiVideoPID, CodecH264, 1)
			first.events = identity(2)
			return psiCase("the_first_video_stream_in_the_table_is_the_one_followed",
				"a program with two video streams is followed on the first the table names",
				1).
				chunk(flatten(psiPackets(0, 0, 0, basePAT),
					psiPackets(psiPMTPID1, 0, 0, pmtV(0, esH264(psiVideoPID), esHEVC(psiVideoPID2)))), first).
				done()
		}(),

		func() psiCorpusCase {
			withAudio := func(version uint8, step int, pids []uint16, tracks []psiTrackExpect, events int) psiExpect {
				return psiExpect{
					hasPAT: true, hasPMT: true, pmtVersion: version, programNumber: 1,
					pmtPID: psiPMTPID1, videoPID: psiVideoPID, videoCodec: CodecH264,
					audioPIDs: pids, tracks: tracks,
					events: identity(events), patPackets: []int{0}, pmtPackets: []int{step},
				}
			}
			one := []psiTrackExpect{trackAC3(psiAudioPID1, "deu", 0x02, 2, false)}
			two := []psiTrackExpect{
				trackAC3(psiAudioPID1, "deu", 0x02, 2, false),
				{pid: psiAudioPID2, streamType: 0x06, codec: "eac3", lang: "eng",
					multichannel: true, componentType: 0x85, hasComponentType: true},
			}
			return psiCase("an_audio_stream_is_added_and_then_removed",
				"the track set is whatever the current table says, and a stream that left leaves nothing behind",
				1).
				chunk(flatten(psiPackets(0, 0, 0, basePAT),
					psiPackets(psiPMTPID1, 0, 0, pmtV(0, esH264(psiVideoPID), esAC3(psiAudioPID1, ac3Stereo)))),
					withAudio(0, 1, []uint16{psiAudioPID1}, one, 2)).
				chunk(psiPackets(psiPMTPID1, 1, 0, pmtV(1, esH264(psiVideoPID),
					esAC3(psiAudioPID1, ac3Stereo), esAC3(psiAudioPID2, eac3Multi))),
					withAudio(1, 2, []uint16{psiAudioPID1, psiAudioPID2}, two, 1)).
				chunk(psiPackets(psiPMTPID1, 2, 0, pmtV(2, esH264(psiVideoPID),
					esAC3(psiAudioPID2, eac3Multi))),
					withAudio(2, 3, []uint16{psiAudioPID2}, []psiTrackExpect{two[1]}, 1)).
				done()
		}(),

		func() psiCorpusCase {
			base := func(version uint8, step int, tracks []psiTrackExpect, events int) psiExpect {
				pids := make([]uint16, 0, len(tracks))
				for _, t := range tracks {
					pids = append(pids, t.pid)
				}
				return psiExpect{
					hasPAT: true, hasPMT: true, pmtVersion: version, programNumber: 1,
					pmtPID: psiPMTPID1, videoPID: psiVideoPID, videoCodec: CodecH264,
					audioPIDs: pids, tracks: tracks,
					events: identity(events), patPackets: []int{0}, pmtPackets: []int{step},
				}
			}
			return psiCase("one_pid_keeps_its_number_and_changes_codec",
				"the same PID carrying MPEG audio after AC-3 is a different elementary stream, not a renamed one",
				1).
				chunk(flatten(psiPackets(0, 0, 0, basePAT),
					psiPackets(psiPMTPID1, 0, 0, pmtV(0, esH264(psiVideoPID), esAC3(psiAudioPID1, ac3Stereo)))),
					base(0, 1, []psiTrackExpect{trackAC3(psiAudioPID1, "deu", 0x02, 2, false)}, 2)).
				chunk(psiPackets(psiPMTPID1, 1, 0, pmtV(1, esH264(psiVideoPID),
					esMP2(psiAudioPID1, descs(descLanguage("deu", 0x00))))),
					base(1, 2, []psiTrackExpect{{pid: psiAudioPID1, streamType: 0x03, codec: "mp2", lang: "deu"}}, 1)).
				done()
		}(),

		func() psiCorpusCase {
			tracks := []psiTrackExpect{
				{pid: psiAudioPID2, streamType: 0x06, codec: "eac3", lang: "eng",
					multichannel: true, componentType: 0x85, hasComponentType: true},
				trackAC3(psiAudioPID1, "deu", 0x02, 2, false),
				{pid: 0x0104, streamType: 0x0F, codec: "aac", lang: "fra",
					channels: 2, componentType: 0x03, hasComponentType: true},
				{pid: 0x0105, streamType: 0x03, codec: "mp2", lang: "und"},
			}
			w := psiExpect{
				hasPAT: true, hasPMT: true, programNumber: 1, pmtPID: psiPMTPID1,
				videoPID: psiVideoPID, videoCodec: CodecH264,
				audioPIDs: []uint16{psiAudioPID2, psiAudioPID1, 0x0104, 0x0105},
				tracks:    tracks,
				events:    identity(2), patPackets: []int{0}, pmtPackets: []int{1},
			}
			return psiCase("several_audio_streams_keep_the_order_the_table_gives_them",
				"E-AC-3, AC-3, AAC and MPEG audio in one program, each with its own declaration, in table order",
				1).
				chunk(flatten(psiPackets(0, 0, 0, basePAT), psiPackets(psiPMTPID1, 0, 0,
					pmtV(0, esH264(psiVideoPID),
						esAC3(psiAudioPID2, eac3Multi),
						esAC3(psiAudioPID1, ac3Stereo),
						esAAC(0x0104, aacStereo),
						esMP2(0x0105, nil)))), w).
				done()
		}(),

		func() psiCorpusCase {
			w := psiExpect{
				hasPAT: true, hasPMT: true, programNumber: 1, pmtPID: psiPMTPID1,
				videoPID: psiVideoPID, videoCodec: CodecH264,
				audioPIDs: []uint16{psiAudioPID1},
				tracks: []psiTrackExpect{
					trackAC3(psiAudioPID1, "deu", 0x02, 2, false),
				},
				events: identity(2), patPackets: []int{0}, pmtPackets: []int{1},
			}
			return psiCase("descriptors_the_parser_does_not_know_are_stepped_over",
				"an unknown tag between the ones that matter must not end the descriptor walk",
				1).
				chunk(flatten(psiPackets(0, 0, 0, basePAT), psiPackets(psiPMTPID1, 0, 0,
					pmtV(0, esH264(psiVideoPID),
						esAC3(psiAudioPID1, descs(
							descUnknown(0x52, 1),
							descLanguage("deu", 0x00),
							descUnknown(0xFF, 3),
							descAC3(0x02),
							descUnknown(0x0E, 2)))))), w).
				done()
		}(),

		func() psiCorpusCase {
			w := psiExpect{
				hasPAT: true, hasPMT: true, programNumber: 1, pmtPID: psiPMTPID1,
				videoPID: psiVideoPID, videoCodec: CodecH264,
				audioPIDs: []uint16{psiAudioPID1},
				tracks: []psiTrackExpect{
					{pid: 0x1FFF, streamType: 0x06, codec: "ac3", lang: "deu",
						channels: 2, componentType: 0x02, hasComponentType: true},
					{pid: 0x0000, streamType: 0x06, codec: "ac3", lang: "eng",
						channels: 2, componentType: 0x02, hasComponentType: true},
					trackAC3(psiAudioPID1, "fra", 0x02, 2, false),
				},
				events: identity(2), patPackets: []int{0}, pmtPackets: []int{1},
			}
			return psiCase("audio_declared_on_a_reserved_pid_is_not_a_stream_that_can_be_watched",
				"the null PID and PID 0 can never carry an elementary stream, so neither becomes an audio PID",
				1).
				chunk(flatten(psiPackets(0, 0, 0, basePAT), psiPackets(psiPMTPID1, 0, 0,
					pmtV(0, esH264(psiVideoPID),
						esAC3(0x1FFF, descs(descLanguage("deu", 0x00), descAC3(0x02))),
						esAC3(0x0000, descs(descLanguage("eng", 0x00), descAC3(0x02))),
						esAC3(psiAudioPID1, descs(descLanguage("fra", 0x00), descAC3(0x02)))))), w).
				noting("audioPIDs excludes PID 0 and the null PID; audioTracks does not. So the track " +
					"list offers two streams that the PID list says are not there, and that nothing " +
					"will ever route payload to or watch for descrambling. Which of the two lists is " +
					"right has not been decided - appendPID filters, appendAudioTrack does not.").
				done()
		}(),

		func() psiCorpusCase {
			w := psiExpect{
				hasPAT: true, hasPMT: true, programNumber: 1, pmtPID: psiPMTPID1,
				videoPID: psiVideoPID, videoCodec: CodecH264,
				audioPIDs: []uint16{psiAudioPID1, psiAudioPID2},
				tracks: []psiTrackExpect{
					{pid: psiAudioPID1, streamType: 0x06, codec: "ac3", lang: "und"},
					{pid: psiAudioPID2, streamType: 0x06, codec: "eac3", lang: "und"},
				},
				events: identity(2), patPackets: []int{0}, pmtPackets: []int{1},
			}
			return psiCase("a_registration_descriptor_is_enough_to_name_the_codec",
				"private-data streams registered as AC-3 or E-AC-3 are audio even with no codec descriptor",
				1).
				chunk(flatten(psiPackets(0, 0, 0, basePAT), psiPackets(psiPMTPID1, 0, 0,
					pmtV(0, esH264(psiVideoPID),
						esAC3(psiAudioPID1, descRegistration("AC-3")),
						esAC3(psiAudioPID2, descRegistration("EAC3"))))), w).
				done()
		}(),

		func() psiCorpusCase {
			w := psiExpect{
				hasPAT: true, hasPMT: true, programNumber: 1, pmtPID: psiPMTPID1,
				videoPID: psiVideoPID, videoCodec: CodecH264,
				events: identity(2), patPackets: []int{0}, pmtPackets: []int{1},
			}
			return psiCase("private_data_without_an_audio_descriptor_is_not_audio",
				"subtitles and teletext share stream type 0x06 with AC-3 and must not be watched as programme audio",
				1).
				chunk(flatten(psiPackets(0, 0, 0, basePAT), psiPackets(psiPMTPID1, 0, 0,
					pmtV(0, esH264(psiVideoPID),
						esPrivate(psiAudioPID1, descs(descLanguage("deu", 0x00), descUnknown(0x56, 2)))))), w).
				done()
		}(),

		func() psiCorpusCase {
			w := psiExpect{
				hasPAT: true, hasPMT: true, programNumber: 1, pmtPID: psiPMTPID1,
				videoPID: psiVideoPID, videoCodec: CodecH264,
				audioPIDs: []uint16{psiAudioPID1, psiAudioPID2, 0x0106},
				tracks: []psiTrackExpect{
					{pid: psiAudioPID1, streamType: 0x06, codec: "ac3", lang: "deu",
						componentType: 0x0F, hasComponentType: true},
					{pid: psiAudioPID2, streamType: 0x06, codec: "ac3", lang: "deu"},
					{pid: 0x0106, streamType: 0x06, codec: "ac3", lang: "deu"},
				},
				events: identity(2), patPackets: []int{0}, pmtPackets: []int{1},
			}
			return psiCase("an_ac3_descriptor_can_carry_no_usable_channel_count",
				"a reserved component type, an absent one, and one the presence flag denies are all silence on the subject",
				1).
				chunk(flatten(psiPackets(0, 0, 0, basePAT), psiPackets(psiPMTPID1, 0, 0,
					pmtV(0, esH264(psiVideoPID),
						esAC3(psiAudioPID1, descs(descLanguage("deu", 0x00), descAC3(0x0F))),
						esAC3(psiAudioPID2, descs(descLanguage("deu", 0x00), descAC3NoComponentType())),
						esAC3(0x0106, descs(descLanguage("deu", 0x00), descAC3FlagClear()))))), w).
				done()
		}(),
	}
}

func psiCorpusCases() []psiCorpusCase {
	var all []psiCorpusCase
	all = append(all, psiAssemblyCases()...)
	all = append(all, psiRejectionCases()...)
	all = append(all, psiLifecycleCases()...)
	all = append(all, psiSelectionCases()...)
	all = append(all, psiElementaryStreamCases()...)
	return all
}

// --- running a case -------------------------------------------------------

// casePackets is the case's own packet sequence: every chunk concatenated, cut
// into 188-byte packets. The raw-PSI expectations index into it.
func casePackets(c psiCorpusCase) [][]byte {
	var all []byte
	for _, s := range c.steps {
		if s.kind == psiStepChunk {
			all = append(all, s.chunk...)
		}
	}
	var out [][]byte
	for i := 0; i+TSPacketSize <= len(all); i += TSPacketSize {
		out = append(out, all[i:i+TSPacketSize])
	}
	return out
}

func packetsAt(packets [][]byte, indices []int) [][]byte {
	out := make([][]byte, 0, len(indices))
	for _, i := range indices {
		out = append(out, packets[i])
	}
	return out
}

func eventNames(events []Event) []string {
	out := make([]string, 0, len(events))
	for _, e := range events {
		switch e.Kind {
		case EventProgramIdentityChanged:
			out = append(out, "identity")
		case EventRandomAccessPoint:
			out = append(out, fmt.Sprintf("rap@%d", e.Offset))
		default:
			out = append(out, fmt.Sprintf("unknown(%d)", e.Kind))
		}
	}
	return out
}

func factsLineOf(through int64, f Facts) string {
	pids := make([]string, 0, len(f.AudioPIDs))
	for _, p := range f.AudioPIDs {
		pids = append(pids, fmt.Sprintf("%d", p))
	}
	return fmt.Sprintf(
		"through=%d hasPAT=%d hasPMT=%d pmtVersion=%d programNumber=%d pmtPID=%d videoPID=%d videoCodec=%s audioPIDs=%s",
		through, psiBit(f.HasPAT), psiBit(f.HasPMT), f.PMTVersion, f.ProgramNumber,
		f.PMTPID, f.VideoPID, f.VideoCodec, strings.Join(pids, ","))
}

func wantFactsLine(w psiExpect) string {
	pids := make([]string, 0, len(w.audioPIDs))
	for _, p := range w.audioPIDs {
		pids = append(pids, fmt.Sprintf("%d", p))
	}
	return fmt.Sprintf(
		"through=%d hasPAT=%d hasPMT=%d pmtVersion=%d programNumber=%d pmtPID=%d videoPID=%d videoCodec=%s audioPIDs=%s",
		w.through, psiBit(w.hasPAT), psiBit(w.hasPMT), w.pmtVersion, w.programNumber,
		w.pmtPID, w.videoPID, w.videoCodec, strings.Join(pids, ","))
}

func trackLineOf(t AudioTrackInfo) string {
	return fmt.Sprintf(
		"pid=%d streamType=%d codec=%s lang=%s channels=%d multichannel=%d componentType=%d hasComponentType=%d",
		t.PID, t.StreamType, t.Codec, t.Language,
		t.Declared.Channels, psiBit(t.Declared.Multichannel),
		t.Declared.ComponentType, psiBit(t.Declared.HasComponentType))
}

func wantTrackLine(t psiTrackExpect) string {
	return fmt.Sprintf(
		"pid=%d streamType=%d codec=%s lang=%s channels=%d multichannel=%d componentType=%d hasComponentType=%d",
		t.pid, t.streamType, t.codec, t.lang,
		t.channels, psiBit(t.multichannel), t.componentType, psiBit(t.hasComponentType))
}

func psiBit(b bool) int {
	if b {
		return 1
	}
	return 0
}

func psiPacketsLine(pat, pmt [][]byte, packets [][]byte) string {
	return fmt.Sprintf("pat=%s pmt=%s", psiIndexList(pat, packets), psiIndexList(pmt, packets))
}

// psiIndexList names each raw packet by where it came from in the case's own
// packet sequence. A packet the case never contained is reported as bytes, which
// is what an implementation inventing a preamble would produce.
func psiIndexList(got [][]byte, packets [][]byte) string {
	parts := make([]string, 0, len(got))
	for _, g := range got {
		idx := -1
		for i, p := range packets {
			if string(p) == string(g) {
				idx = i
				break
			}
		}
		if idx < 0 {
			parts = append(parts, "!"+hex.EncodeToString(g))
			continue
		}
		parts = append(parts, fmt.Sprintf("%d", idx))
	}
	return strings.Join(parts, ",")
}

func wantPSILine(w psiExpect) string {
	return fmt.Sprintf("pat=%s pmt=%s", psiIntList(w.patPackets), psiIntList(w.pmtPackets))
}

func psiIntList(in []int) string {
	parts := make([]string, 0, len(in))
	for _, v := range in {
		parts = append(parts, fmt.Sprintf("%d", v))
	}
	return strings.Join(parts, ",")
}

// runPSICase drives one case and reports every field that disagreed rather than
// stopping at the first, so a semantic change shows its whole shape at once.
func runPSICase(t *testing.T, c psiCorpusCase) {
	t.Helper()
	packets := casePackets(c)
	core := NewGoCore(c.initial)
	ctx := context.Background()
	offset := int64(0)

	for i, step := range c.steps {
		var got ParseResult
		var err error
		switch step.kind {
		case psiStepChunk:
			got, err = core.Ingest(ctx, offset, step.chunk)
			offset += int64(len(step.chunk))
		case psiStepTarget:
			got, err = core.SetTargetProgram(ctx, step.target)
		}
		if err != nil {
			t.Fatalf("step %d of %d: %v", i+1, len(c.steps), err)
		}

		var bad []string
		if line := factsLineOf(got.ProcessedThroughOffset, got.Facts); line != wantFactsLine(step.want) {
			bad = append(bad, "  facts got  "+line+"\n  facts want "+wantFactsLine(step.want))
		}
		if len(got.Facts.AudioTracks) != len(step.want.tracks) {
			bad = append(bad, fmt.Sprintf("  tracks got %d, want %d", len(got.Facts.AudioTracks), len(step.want.tracks)))
		} else {
			for j, tr := range got.Facts.AudioTracks {
				if line := trackLineOf(tr); line != wantTrackLine(step.want.tracks[j]) {
					bad = append(bad, fmt.Sprintf("  track %d got  %s\n  track %d want %s",
						j, line, j, wantTrackLine(step.want.tracks[j])))
				}
				// A PSI case feeds no elementary stream payload, so a track that
				// carries an observation means audio bytes reached a path this
				// corpus does not describe.
				if tr.Observed != (esaudio.Observation{}) {
					bad = append(bad, fmt.Sprintf("  track %d carries an observation: %+v", j, tr.Observed))
				}
			}
		}
		if names := eventNames(got.Events); strings.Join(names, " ") != strings.Join(step.want.events, " ") {
			bad = append(bad, "  events got  ["+strings.Join(names, " ")+"]\n  events want ["+strings.Join(step.want.events, " ")+"]")
		}
		if line := psiPacketsLine(got.PSI.PAT, got.PSI.PMT, packets); line != wantPSILine(step.want) {
			bad = append(bad, "  psi got  "+line+"\n  psi want "+wantPSILine(step.want))
		}
		if len(bad) > 0 {
			t.Errorf("step %d of %d (%s):\n%s", i+1, len(c.steps), psiStepLabel(step), strings.Join(bad, "\n"))
		}
	}
}

func psiStepLabel(s psiStep) string {
	if s.kind == psiStepTarget {
		return fmt.Sprintf("target %d", s.target)
	}
	return fmt.Sprintf("chunk of %d packets", len(s.chunk)/TSPacketSize)
}

func TestPSICorpus_TheGoCoreMeetsTheAuthoredExpectations(t *testing.T) {
	for _, c := range psiCorpusCases() {
		t.Run(c.name, func(t *testing.T) { runPSICase(t, c) })
	}
}

func TestPSICorpus_TheCheckedInFileMatchesTheCases(t *testing.T) {
	want := renderPSICorpus(psiCorpusCases())

	if *updatePSICorpus {
		if err := os.MkdirAll(filepath.Dir(psiCorpusPath), 0o755); err != nil {
			t.Fatalf("create corpus directory: %v", err)
		}
		if err := os.WriteFile(psiCorpusPath, []byte(want), 0o644); err != nil { // #nosec G306 -- a checked-in fixture
			t.Fatalf("write corpus: %v", err)
		}
		t.Logf("wrote %s", psiCorpusPath)
		return
	}

	got, err := os.ReadFile(psiCorpusPath)
	if err != nil {
		t.Fatalf("read corpus: %v\nrun: go test ./internal/stream/ingest/mediafacts/ -run TestPSICorpus -update-psi-corpus", err)
	}
	if string(got) != want {
		t.Errorf("%s is out of date; run:\n"+
			"  go test ./internal/stream/ingest/mediafacts/ -run TestPSICorpus -update-psi-corpus\n"+
			"and review the diff - the Rust core will be checked against this file", psiCorpusPath)
	}
}

// renderPSICorpus writes the corpus in a line format both languages can read
// without a parser library, and a reviewer can read without either.
func renderPSICorpus(cases []psiCorpusCase) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# xg2g PSI (PAT/PMT/descriptor) corpus, format version %d\n", psiCorpusFormatVersion)
	b.WriteString("#\n")
	b.WriteString("# Generated. To change it, edit psiCorpusCases() in\n")
	b.WriteString("# backend/internal/stream/ingest/mediafacts/psi_corpus_test.go and run\n")
	b.WriteString("#   go test ./internal/stream/ingest/mediafacts/ -run TestPSICorpus -update-psi-corpus\n")
	b.WriteString("#\n")
	b.WriteString("# A case is a core constructed with a target program number, then a sequence of\n")
	b.WriteString("# calls. Each call is followed by the PSI-scoped part of the ParseResult it must\n")
	b.WriteString("# produce:\n")
	b.WriteString("#\n")
	b.WriteString("#   chunk <hex>   one Ingest of these bytes, at the offset the previous chunks reached\n")
	b.WriteString("#   target <n>    one SetTargetProgram\n")
	b.WriteString("#   facts ...     the scalar facts after that call, and ProcessedThroughOffset\n")
	b.WriteString("#   track ...     one line per audio track, in the order the PMT declares them\n")
	b.WriteString("#   events ...    the event kinds that call produced, in order\n")
	b.WriteString("#   psi ...       the raw ActivePSI packets, named by their index in the case's\n")
	b.WriteString("#                 own packet sequence: packet N is bytes [188N, 188N+188) of the\n")
	b.WriteString("#                 case's chunks concatenated\n")
	b.WriteString("#\n")
	b.WriteString("# Chunk boundaries are part of the case: a section assembler carries a partial\n")
	b.WriteString("# section across a call, so the same packets cut differently are a different\n")
	b.WriteString("# input.\n")
	b.WriteString("#\n")
	b.WriteString("# No case carries video elementary stream payload, so no case produces a random\n")
	b.WriteString("# access point. Video facts here are the ones the PMT alone declares.\n")
	b.WriteString("#\n")
	b.WriteString("# A `note` on a case marks an expectation that records what the Go reference does\n")
	b.WriteString("# today without a decision having been taken that it is what it should do.\n")
	fmt.Fprintf(&b, "version %d\n", psiCorpusFormatVersion)

	for _, c := range cases {
		b.WriteString("\ncase " + c.name + "\n")
		b.WriteString("  desc " + c.desc + "\n")
		if c.note != "" {
			b.WriteString("  note " + c.note + "\n")
		}
		fmt.Fprintf(&b, "  init target=%d\n", c.initial)
		for _, s := range c.steps {
			switch s.kind {
			case psiStepChunk:
				b.WriteString("  chunk " + hex.EncodeToString(s.chunk) + "\n")
			case psiStepTarget:
				fmt.Fprintf(&b, "  target %d\n", s.target)
			}
			b.WriteString("  facts " + wantFactsLine(s.want) + "\n")
			for _, tr := range s.want.tracks {
				b.WriteString("  track " + wantTrackLine(tr) + "\n")
			}
			b.WriteString("  events " + strings.Join(s.want.events, " ") + "\n")
			b.WriteString("  psi " + wantPSILine(s.want) + "\n")
		}
		b.WriteString("end\n")
	}
	return b.String()
}

// --- what the corpus is claimed to cover ----------------------------------

// TestPSICorpus_CoversWhatItClaimsTo is not a substitute for reading the file.
// It is a reminder that these classes are the reason it exists, so a
// regeneration that quietly drops one is noticed.
func TestPSICorpus_CoversWhatItClaimsTo(t *testing.T) {
	names := make(map[string]bool)
	for _, c := range psiCorpusCases() {
		if names[c.name] {
			t.Errorf("duplicate case name %q", c.name)
		}
		names[c.name] = true
	}

	for _, required := range []string{
		// assembly
		"pat_then_pmt_in_one_chunk",
		"a_pat_section_split_across_two_packets",
		"a_pmt_section_split_across_two_packets",
		"a_section_spans_the_chunk_boundary",
		"a_non_zero_pointer_field_completes_the_previous_section",
		"two_complete_sections_in_one_packet",
		// what a section has to be
		"a_section_with_a_bad_crc_is_ignored",
		"a_section_marked_not_current_is_ignored",
		"a_truncated_section_never_completes",
		"a_section_length_that_never_arrives_leaves_the_table_inactive",
		"a_zero_length_section_is_dropped",
		"a_continuity_gap_aborts_an_in_flight_section",
		"an_exact_duplicate_packet_is_ignored",
		"a_duplicate_of_a_continuation_packet_is_ignored",
		"a_section_too_short_to_hold_its_fields_is_dropped",
		"a_section_numbered_outside_the_table_does_not_complete_it",
		"a_section_of_another_table_on_the_pat_pid_is_not_a_pat",
		"a_section_of_another_table_split_across_packets_is_not_a_pat",
		"an_es_entry_declaring_more_descriptors_than_the_loop_holds_is_not_read",
		"a_foreign_table_ends_the_scan_of_the_payload_it_is_in",
		"the_same_counter_with_different_bytes_resets_assembly",
		// table lifecycle
		"a_new_pmt_version_on_the_same_pid_is_a_new_program_identity",
		"a_pmt_version_wrapping_from_31_to_0_is_still_a_change",
		"the_same_table_delivered_again_is_not_a_change",
		"a_pmt_naming_another_program_number_on_the_same_pid_is_taken_as_a_change",
		"a_new_pat_moves_the_program_to_another_pmt_pid",
		"a_missing_section_prevents_the_table_from_activating",
		"a_carousel_repeat_of_one_section_keeps_the_table_complete",
		"a_new_version_restarts_the_section_set_without_dropping_the_old_table",
		// program selection
		"the_target_program_is_selected_out_of_several",
		"a_pat_without_the_target_program_names_no_pmt_pid",
		"program_number_zero_is_the_network_information_table",
		"switching_the_target_program_discards_everything_about_the_old_one",
		"a_target_switch_forgets_the_program_number_and_version",
		"a_pat_that_moves_the_pmt_pid_forgets_the_old_program_identity",
		"a_target_of_zero_takes_the_first_program_of_the_whole_table",
		"a_target_of_zero_reaches_into_a_later_section_when_the_first_names_only_the_nit",
		"sections_delivered_out_of_order_are_read_in_section_number_order",
		"setting_the_target_to_the_program_already_followed_changes_nothing",
		// elementary streams and descriptors
		"every_video_stream_type_the_pmt_can_declare",
		"the_video_pid_moves_with_the_table",
		"the_first_video_stream_in_the_table_is_the_one_followed",
		"an_audio_stream_is_added_and_then_removed",
		"one_pid_keeps_its_number_and_changes_codec",
		"several_audio_streams_keep_the_order_the_table_gives_them",
		"descriptors_the_parser_does_not_know_are_stepped_over",
		"a_registration_descriptor_is_enough_to_name_the_codec",
		"private_data_without_an_audio_descriptor_is_not_audio",
		"audio_declared_on_a_reserved_pid_is_not_a_stream_that_can_be_watched",
		"an_ac3_descriptor_can_carry_no_usable_channel_count",
	} {
		if !names[required] {
			t.Errorf("corpus no longer covers %s", required)
		}
	}
}

// TestPSICorpus_EveryCaseIsPacketAligned holds the corpus to the shape its own
// raw-PSI expectations assume: packet N is bytes [188N, 188N+188) of the case's
// chunks concatenated. A chunk that was not packet aligned would silently
// renumber every index after it.
func TestPSICorpus_EveryCaseIsPacketAligned(t *testing.T) {
	for _, c := range psiCorpusCases() {
		total := 0
		for i, s := range c.steps {
			if s.kind != psiStepChunk {
				continue
			}
			if len(s.chunk)%TSPacketSize != 0 {
				t.Errorf("%s step %d: chunk of %d bytes is not packet aligned", c.name, i+1, len(s.chunk))
			}
			total += len(s.chunk)
		}
		packets := total / TSPacketSize
		for i, s := range c.steps {
			for _, idx := range append(append([]int(nil), s.want.patPackets...), s.want.pmtPackets...) {
				if idx < 0 || idx >= packets {
					t.Errorf("%s step %d: packet index %d is outside the case's %d packets",
						c.name, i+1, idx, packets)
				}
			}
		}
	}
}

// --- the parts of the contract that are not a chunk of bytes ---------------

// TestPSI_TheCoreConsumesExactlyWhatItIsGiven is the authored claim behind every
// through= in the corpus, stated once here rather than repeated in each case.
func TestPSI_TheCoreConsumesExactlyWhatItIsGiven(t *testing.T) {
	pat := patSection(psiTSID, 0, 0, 0, 1, patProgram{1, psiPMTPID1})
	data := chunkOf(psiPackets(0, 0, 0, pat)...)
	ctx := context.Background()

	c := NewGoCore(1)
	const startOffset = int64(9_400_000)
	res, err := c.Ingest(ctx, startOffset, data)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if got, want := res.ProcessedThroughOffset, startOffset+int64(len(data)); got != want {
		t.Errorf("ProcessedThroughOffset = %d, want %d", got, want)
	}

	// An empty chunk is packet aligned and interprets nothing, so it consumes
	// nothing and reports the offset it was given.
	res, err = c.Ingest(ctx, startOffset, nil)
	if err != nil {
		t.Fatalf("empty ingest: %v", err)
	}
	if got := res.ProcessedThroughOffset; got != startOffset {
		t.Errorf("empty chunk reported through %d, want %d", got, startOffset)
	}

	// SetTargetProgram interprets no bytes and so has no position in the stream.
	res, err = c.SetTargetProgram(ctx, 2)
	if err != nil {
		t.Fatalf("set target: %v", err)
	}
	if got := res.ProcessedThroughOffset; got != 0 {
		t.Errorf("SetTargetProgram reported through %d, want 0", got)
	}
}

// TestPSI_ResetAndATargetSwitchAgreeAboutWhatIsForgotten holds the two ways a
// core is told the programme it was following is gone to the same answer.
//
// Not corpus material: Reset is deliberately absent from the Core interface, so
// no second implementation is asked to have it. The invariant it protects is
// still worth a test, because the two paths drifted once already - Reset rebuilt
// the whole core and so cleared the programme number and the table version,
// while SetTargetProgram cleared neither, and Facts described a discarded
// programme behind a false HasPMT.
func TestPSI_ResetAndATargetSwitchAgreeAboutWhatIsForgotten(t *testing.T) {
	pat := patSection(psiTSID, 0, 0, 0, 1, patProgram{1, psiPMTPID1}, patProgram{2, psiPMTPID2})
	pmt := pmtSection(1, psiVideoPID, 9, 0, 0, 1, nil, esH264(psiVideoPID))

	followingProgramOne := func() *GoCore {
		c := NewGoCore(1)
		if _, err := c.Ingest(context.Background(), 0,
			chunkOf(flatten(psiPackets(0, 0, 0, pat), psiPackets(psiPMTPID1, 0, 0, pmt))...)); err != nil {
			t.Fatalf("ingest: %v", err)
		}
		if f := c.Snapshot(); !f.HasPMT || f.ProgramNumber != 1 || f.PMTVersion != 9 {
			t.Fatalf("setup did not establish the programme: %+v", f)
		}
		return c
	}

	switched, err := followingProgramOne().SetTargetProgram(context.Background(), 2)
	if err != nil {
		t.Fatalf("set target: %v", err)
	}
	reset := followingProgramOne().Reset()

	for _, tc := range []struct {
		what  string
		facts Facts
	}{
		{"SetTargetProgram", switched.Facts},
		{"Reset", reset.Facts},
	} {
		if tc.facts.HasPMT || tc.facts.ProgramNumber != 0 || tc.facts.PMTVersion != 0 {
			t.Errorf("after %s: HasPMT=%v ProgramNumber=%d PMTVersion=%d; all three describe the "+
				"PMT in force and there is none, so all three must be zero",
				tc.what, tc.facts.HasPMT, tc.facts.ProgramNumber, tc.facts.PMTVersion)
		}
	}
}

// TestPSI_TheCoreRefusesWhatItCannotInterpret pins the two ways a call fails
// before any byte is read. Both matter to the corpus: an implementation that
// accepted either would be describing a different contract.
func TestPSI_TheCoreRefusesWhatItCannotInterpret(t *testing.T) {
	c := NewGoCore(1)

	if _, err := c.Ingest(context.Background(), 0, make([]byte, TSPacketSize+1)); err != ErrInvalidPacketSize {
		t.Errorf("unaligned chunk gave %v, want %v", err, ErrInvalidPacketSize)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Ingest(cancelled, 0, make([]byte, TSPacketSize)); err == nil {
		t.Error("ingest with a cancelled context returned no error")
	}
	if _, err := c.SetTargetProgram(cancelled, 3); err == nil {
		t.Error("set target with a cancelled context returned no error")
	}

	// The refusals left the core untouched: the target is still 1, so a PAT that
	// names program 1 is still followed.
	pat := patSection(psiTSID, 0, 0, 0, 1, patProgram{1, psiPMTPID1})
	res, err := c.Ingest(context.Background(), 0, chunkOf(psiPackets(0, 0, 0, pat)...))
	if err != nil {
		t.Fatalf("ingest after refusals: %v", err)
	}
	if res.Facts.PMTPID != uint16(psiPMTPID1) {
		t.Errorf("after two refused calls the core reported PMT PID 0x%04X, want 0x%04X",
			res.Facts.PMTPID, uint16(psiPMTPID1))
	}
}
