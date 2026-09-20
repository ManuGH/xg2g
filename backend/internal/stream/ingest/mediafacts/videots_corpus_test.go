// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package mediafacts

import (
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"strings"
	"testing"
)

// The raw-transport-to-video-facts corpus.
//
// The video path had no corpus of its own before this file: two tests of the
// slice-header and SEI helpers, two fuzzers over the same helpers, and an
// archive replay that is skipped without the archive. Nothing pinned what the
// reference says about a random access point, an access unit, a parameter set
// or a scrambled picture on transport a reviewer can read.
//
// A case is raw transport, and its expectation is what the core reports for it:
// the events in the order they are emitted, attributed to the ingest call that
// emitted them, and the facts after any call the author cares about. The
// expectations are authored from ISO/IEC 13818-1, H.264, H.265 and 13818-2 and
// from the fields' own documentation, not recorded from the implementation. A
// corpus written down from a parser proves that the parser is self-consistent
// and nothing else.
//
// Where the reference does something else, the case says so with `diverges`
// and carries the reference's own answer beside the authored one. The class
// names what kind of difference it is:
//
//	defect      the reference contradicts its own contract or fabricates a fact
//	quirk       the reference is inaccurate in a way nothing decides on
//	limitation  the reference cannot see something by construction
//	divergence  a reviewed migration difference, kept on purpose
//
// None of them is adopted. A diverging case is a finding with a number, and
// the number is in its description.

var updateVideoTSCorpus = flag.Bool("update-video-ts-corpus", false, "rewrite the checked-in raw-TS video corpus")

const videoTSCorpusPath = "../../../../../testdata/video-ts-corpus/corpus.txt"

// --- the shape of a case ---------------------------------------------------

type videoTSStepKind int

const (
	videoTSStepChunk videoTSStepKind = iota
	videoTSStepTarget
)

// videoTSEvent is one event the core emitted, as the corpus states it.
type videoTSEvent struct {
	kind     string // "rap" or "identityEv"
	offset   int64
	joinable bool
}

func rapAt(offset int64, joinable bool) videoTSEvent {
	return videoTSEvent{kind: "rap", offset: offset, joinable: joinable}
}

func rapInvalidatedAt(offset int64) videoTSEvent {
	return videoTSEvent{kind: "rap_invalidated", offset: offset}
}

var identityEv = videoTSEvent{kind: "identity"}

// videoTSFacts is the video-scoped part of Facts, as the corpus states it.
//
// Every field here is one a consumer reads or one the migration has to carry.
// The audio and PSI fields are not here: they have corpora of their own.
type videoTSFacts struct {
	paramSets          bool
	irapPoints         uint64
	intraPoints        uint64
	recoverySEIs       uint64
	predictedRejected  uint64
	unreadable         uint64
	videoScrambled     uint64
	videoClear         uint64
	videoClearRun      uint64
	cleanEntry         uint64
	cleanAUs           uint64
	scrambledConfirmed bool
	videoPID           uint16
	codec              VideoCodec
}

func videoTSFactsOf(f Facts) videoTSFacts {
	return videoTSFacts{
		paramSets:          f.ParameterSetsSeen,
		irapPoints:         f.RandomAccess.IRAPPoints,
		intraPoints:        f.RandomAccess.IntraPoints,
		recoverySEIs:       f.RandomAccess.RecoveryPointSEIs,
		predictedRejected:  f.RandomAccess.PredictedRejected,
		unreadable:         f.RandomAccess.UnreadableSlices,
		videoScrambled:     f.Scrambling.VideoScrambled,
		videoClear:         f.Scrambling.VideoClear,
		videoClearRun:      f.Scrambling.VideoClearRun,
		cleanEntry:         f.CleanEntryPoints,
		cleanAUs:           f.CleanAccessUnits,
		scrambledConfirmed: f.ScrambledVideoConfirmed,
		videoPID:           f.VideoPID,
		codec:              f.VideoCodec,
	}
}

// Authoring notation. Each method returns a modified copy, so an expectation
// reads as one expression: h264().ps(true).irap(1).clear(1).cleanrap(1).
func h264() videoTSFacts  { return videoTSFacts{videoPID: videoTSPID, codec: CodecH264} }
func hevc() videoTSFacts  { return videoTSFacts{videoPID: videoTSPID, codec: CodecH265} }
func mpeg2() videoTSFacts { return videoTSFacts{videoPID: videoTSPID, codec: CodecMPEG2} }
func noVideo() videoTSFacts {
	return videoTSFacts{codec: CodecUnknown}
}

func (f videoTSFacts) ps(b bool) videoTSFacts        { f.paramSets = b; return f }
func (f videoTSFacts) irap(n uint64) videoTSFacts    { f.irapPoints = n; return f }
func (f videoTSFacts) intra(n uint64) videoTSFacts   { f.intraPoints = n; return f }
func (f videoTSFacts) rpsei(n uint64) videoTSFacts   { f.recoverySEIs = n; return f }
func (f videoTSFacts) predrej(n uint64) videoTSFacts { f.predictedRejected = n; return f }
func (f videoTSFacts) unread(n uint64) videoTSFacts  { f.unreadable = n; return f }
func (f videoTSFacts) vscr(n uint64) videoTSFacts    { f.videoScrambled = n; return f }
func (f videoTSFacts) vclr(n uint64) videoTSFacts    { f.videoClear = n; return f }
func (f videoTSFacts) vrun(n uint64) videoTSFacts    { f.videoClearRun = n; return f }

// clear is the common case of clear packets with no scrambled one between
// them: the total and the run are the same number.
func (f videoTSFacts) clear(n uint64) videoTSFacts    { f.videoClear = n; f.videoClearRun = n; return f }
func (f videoTSFacts) cleanrap(n uint64) videoTSFacts { f.cleanEntry = n; return f }
func (f videoTSFacts) cleanau(n uint64) videoTSFacts  { f.cleanAUs = n; return f }
func (f videoTSFacts) scrconf(b bool) videoTSFacts    { f.scrambledConfirmed = b; return f }
func (f videoTSFacts) vpid(p uint16) videoTSFacts     { f.videoPID = p; return f }

type videoTSStep struct {
	kind   videoTSStepKind
	chunk  []byte
	target uint16

	// The authored answer for this step: the events the call emits, and the
	// facts after it when the author states them.
	events []videoTSEvent
	facts  *videoTSFacts

	// The reference's answer where it differs. Unset means "the same as
	// authored", which is what most steps of a diverging case are; the
	// renderer writes them all out so the file is explicit.
	refEvents    []videoTSEvent
	refEventsSet bool
	refFacts     *videoTSFacts
}

type videoTSCase struct {
	name    string
	desc    string
	initial uint16
	steps   []videoTSStep

	// class and divergence name a case where the reference does something
	// else. Both are in the corpus file beside the case.
	class      string
	divergence string
}

// --- fixtures --------------------------------------------------------------

const (
	videoTSPID     = 0x0100
	videoTSPID2    = 0x0102
	videoTSAudio   = 0x0101
	videoTSPMTPID2 = 0x1001
	videoTSProgram = 1
	videoTSOther   = 2
)

// videoTSPAT is a PAT naming several programmes, in the order given.
func videoTSPAT(programs ...[2]uint16) []byte {
	var payload []byte
	for _, p := range programs {
		payload = append(payload,
			byte(p[0]>>8), byte(p[0]&0xFF),
			0xE0|byte(p[1]>>8), byte(p[1]&0xFF),
		)
	}
	return audioTSSection(0x00, 1, 0, payload)
}

func h264Stream(pid uint16) audioTSEs  { return audioTSEs{streamType: 0x1B, pid: pid} }
func hevcStream(pid uint16) audioTSEs  { return audioTSEs{streamType: 0x24, pid: pid} }
func mpeg2Stream(pid uint16) audioTSEs { return audioTSEs{streamType: 0x02, pid: pid} }

func cat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// nal is an H.264 NAL unit behind a three-byte Annex-B start code.
func nal(header byte, body ...byte) []byte {
	return append([]byte{0x00, 0x00, 0x01, header}, body...)
}

// nal4 is the same behind a four-byte start code.
func nal4(header byte, body ...byte) []byte {
	return append([]byte{0x00, 0x00, 0x00, 0x01, header}, body...)
}

// hevcNAL is an HEVC NAL unit: two header bytes, nal_unit_type in the first,
// nuh_layer_id zero and nuh_temporal_id_plus1 one in the second.
func hevcNAL(nalType byte, body ...byte) []byte {
	return append([]byte{0x00, 0x00, 0x01, nalType << 1, 0x01}, body...)
}

// hevcNALSecondByte is an HEVC NAL unit with a chosen second header byte, for
// the case that proves the byte is passed over rather than read as payload.
func hevcNALSecondByte(nalType, second byte, body ...byte) []byte {
	return append([]byte{0x00, 0x00, 0x01, nalType << 1, second}, body...)
}

// m2 is an MPEG-2 start code and what follows it.
func m2(code byte, body ...byte) []byte {
	return append([]byte{0x00, 0x00, 0x01, code}, body...)
}

func repeat(b byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return out
}

// Slice header bytes. first_mb_in_slice and slice_type are the first two
// Exp-Golomb fields; the values are written out so the intent is checkable
// against the bit pattern rather than against a helper.
const (
	// 1 | 0001000: first_mb 0, slice_type 7 (I, whole picture).
	sliceI = 0x88
	// 1 | 1: first_mb 0, slice_type 0 (P).
	sliceP = 0xC0
)

// H.264 NAL units. Body bytes avoid 00 00 runs so nothing in them can be
// mistaken for a start code.
var (
	h264SPS    = nal(0x67, 0x42, 0xC0, 0x1E, 0xDA, 0x02, 0x80, 0xF6, 0x80)
	h264PPS    = nal(0x68, 0xCE, 0x38, 0x80)
	h264IDR    = nal(0x65, sliceI, 0x84, 0x21, 0xA0, 0x33, 0xFF)
	h264SliceI = nal(0x41, sliceI, 0x84, 0x21, 0xA0, 0x33, 0xFF)
	h264SliceP = nal(0x41, sliceP, 0x21, 0xA0, 0x33, 0xFF)
	// Twelve zero bytes: more than 32 leading zero bits, which the Exp-Golomb
	// reader refuses. Unreadable by construction, not by accident.
	h264SliceUnreadable = nal(0x41, repeat(0x00, 12)...)
	h264PartA           = nal(0x42, sliceI, 0x84, 0x21, 0xA0, 0x33, 0xFF)
	h264PartB           = nal(0x43, 0xAA, 0xBB, 0xCC)
	h264PartC           = nal(0x44, 0xAA, 0xBB, 0xCC)
	h264AUD             = nal(0x09, 0xF0)
	h264PrefixNAL       = nal(0x6E, 0xAA, 0xBB) // type 14
	h264SliceExtension  = nal(0x74, 0xAA, 0xBB) // type 20

	// SEI payloads: (type, size, body) triples, then rbsp_trailing_bits.
	h264SEIRecovery     = nal(0x06, 0x06, 0x01, 0x84, 0x80)
	h264SEIPicTiming    = nal(0x06, 0x01, 0x02, 0x21, 0xA0, 0x80)
	h264SEIBoth         = nal(0x06, 0x01, 0x02, 0x21, 0xA0, 0x06, 0x01, 0x84, 0x80)
	h264SEIUnterminated = nal(0x06, 0x01, 0x02, 0x21, 0xA0)
	// A 60-byte user_data_unregistered payload ahead of the recovery point:
	// more than the 48-byte capture the reference reads.
	h264SEILongThenRecovery = nal(0x06, cat([]byte{0x05, 0x3C}, repeat(0xAA, 60), []byte{0x06, 0x01, 0x84, 0x80})...)
	// A non-IDR slice whose first bytes are what a recovery_point SEI payload
	// looks like. It is a slice; nothing about it is an SEI.
	h264SliceLooksLikeSEI = nal(0x41, 0x06, 0x01, 0x84, 0x80)
)

// HEVC NAL units. Types per H.265 Table 7-1.
var (
	hevcVPS                 = hevcNAL(32, 0x0C, 0x01, 0xFF, 0xFF)
	hevcSPS                 = hevcNAL(33, 0x01, 0x01, 0x60, 0xAA)
	hevcPPS                 = hevcNAL(34, 0xC1, 0x72, 0xB4)
	hevcIDR                 = hevcNAL(19, 0xAF, 0x1E, 0x33) // IDR_W_RADL
	hevcBLA                 = hevcNAL(16, 0xAF, 0x1E, 0x33) // BLA_W_LP, first of the IRAP range
	hevcCRA                 = hevcNAL(21, 0xAF, 0x1E, 0x33) // CRA_NUT, last of the IRAP range
	hevcTrail               = hevcNAL(1, 0xD0, 0x1E, 0x33)  // TRAIL_R
	hevcRASL                = hevcNAL(9, 0xD0, 0x1E, 0x33)  // RASL_R, last ordinary VCL type
	hevcReserved10          = hevcNAL(10, 0xD0, 0x1E, 0x33) // RSV_VCL_N10
	hevcReserved22          = hevcNAL(22, 0xD0, 0x1E, 0x33) // RSV_IRAP_VCL22
	hevcSEIRecovery         = hevcNAL(39, 0x06, 0x01, 0x84, 0x80)
	hevcSuffixSEIRecovery   = hevcNAL(40, 0x06, 0x01, 0x84, 0x80)
	hevcSEILongThenRecovery = hevcNAL(39,
		cat([]byte{0x05, 0x3C}, repeat(0xAA, 60), []byte{0x06, 0x01, 0x84, 0x80})...)
	// A pic_timing-only SEI whose second header byte is 0x06. Read as payload it
	// would say "recovery_point, one byte" and admit the picture.
	hevcSEIPicTimingHeader06 = hevcNALSecondByte(39, 0x06, 0x01, 0x02, 0x21, 0xA0, 0x80)
)

// MPEG-2 video: ISO/IEC 13818-2 start codes. picture_coding_type sits in
// bits 5..3 of the second byte after picture_start_code.
var (
	m2Seq     = m2(0xB3, 0x14, 0x00, 0xF0, 0x13, 0xFF, 0xFF, 0xE0, 0x18)
	m2GOP     = m2(0xB8, 0x00, 0x08, 0x00, 0x40)
	m2PicI    = m2(0x00, 0x00, 0x08, 0xFF, 0xF8) // type 1
	m2PicP    = m2(0x00, 0x00, 0x10, 0xFF, 0xF8) // type 2
	m2PicB    = m2(0x00, 0x00, 0x18, 0xFF, 0xF8) // type 3
	m2PicBad0 = m2(0x00, 0x00, 0x00, 0xFF, 0xF8) // type 0, forbidden
	m2PicBad4 = m2(0x00, 0x00, 0x20, 0xFF, 0xF8) // type 4, reserved
	m2Slice   = m2(0x01, 0x1A, 0x5B, 0xC3, 0xFF)
)

// A PES header that is not a video PES header: the start code is wrong. The
// bytes after it are what a PES header would carry, so the payload is a
// convincing near-miss rather than noise.
var bogusPESPrefix = []byte{0x00, 0x00, 0x02, 0xE0, 0x00, 0x00, 0x80, 0x00, 0x00}

// --- packet building -------------------------------------------------------

type videoTSBuild struct {
	c  videoTSCase
	cc map[uint16]byte
}

func vNew(name, desc string, target uint16) *videoTSBuild {
	return &videoTSBuild{
		c:  videoTSCase{name: name, desc: desc, initial: target},
		cc: map[uint16]byte{},
	}
}

func (b *videoTSBuild) next(pid uint16) byte {
	c := b.cc[pid]
	b.cc[pid] = (c + 1) & 0x0F
	return c
}

// psi is a PAT and a PMT for the programme, one packet each.
func (b *videoTSBuild) psi(version byte, streams ...audioTSEs) []byte {
	return cat(
		audioTSPSIPacket(0, b.next(0), audioTSPAT(videoTSProgram)),
		b.pmtOnly(version, streams...),
	)
}

func (b *videoTSBuild) pmtOnly(version byte, streams ...audioTSEs) []byte {
	return audioTSPSIPacket(audioTSPMTPID, b.next(audioTSPMTPID), audioTSPMT(videoTSProgram, version, streams...))
}

// start is a packet beginning a video PES packet with no optional header,
// carrying these elementary stream bytes and adaptation-field stuffing.
func (b *videoTSBuild) start(pid uint16, es ...[]byte) []byte {
	return audioTSShortPacket(pid, true, b.next(pid), cat(audioTSPESHeader(0xE0, 0), cat(es...)))
}

// startHdr is a PES start whose optional header is these bytes.
func (b *videoTSBuild) startHdr(pid uint16, header []byte, es ...[]byte) []byte {
	// header_data_length declares the optional header; the bytes given are
	// the whole of it here, so the two agree.
	h := []byte{0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x80, byte(len(header))}
	return audioTSShortPacket(pid, true, b.next(pid), cat(h, header, cat(es...)))
}

// cont is a packet continuing a PES packet.
func (b *videoTSBuild) cont(pid uint16, es ...[]byte) []byte {
	return audioTSShortPacket(pid, false, b.next(pid), cat(es...))
}

// raw is a packet with this payload exactly, for payload units that are not
// what a video PES start looks like.
func (b *videoTSBuild) raw(pid uint16, pusi bool, payload []byte) []byte {
	return audioTSShortPacket(pid, pusi, b.next(pid), payload)
}

func scrambled(p []byte) []byte {
	out := append([]byte(nil), p...)
	out[3] |= 0x80 // transport_scrambling_control = 10
	return out
}

func withTEI(p []byte) []byte {
	out := append([]byte(nil), p...)
	out[1] |= 0x80
	return out
}

func withSyncByte(p []byte, sync byte) []byte {
	out := append([]byte(nil), p...)
	out[0] = sync
	return out
}

// withAFC rewrites adaptation_field_control on a packet built with payload
// only. The bytes after the header stay where they are; what changes is what
// the header says about them.
func withAFC(p []byte, afc byte) []byte {
	out := append([]byte(nil), p...)
	out[3] = (out[3] & 0xCF) | (afc << 4)
	return out
}

// withAdaptationLength rewrites the adaptation field length of a packet that
// has one.
func withAdaptationLength(p []byte, length byte) []byte {
	out := append([]byte(nil), p...)
	out[4] = length
	return out
}

func videoTSNullPacket() []byte {
	p := make([]byte, TSPacketSize)
	for i := range p {
		p[i] = 0xFF
	}
	p[0] = SyncByte
	p[1] = 0x1F
	p[2] = 0xFF
	p[3] = 0x10
	return p
}

// --- authoring -------------------------------------------------------------

func (b *videoTSBuild) chunk(packets ...[]byte) *videoTSBuild {
	b.c.steps = append(b.c.steps, videoTSStep{kind: videoTSStepChunk, chunk: cat(packets...)})
	return b
}

func (b *videoTSBuild) target(n uint16) *videoTSBuild {
	b.c.steps = append(b.c.steps, videoTSStep{kind: videoTSStepTarget, target: n})
	return b
}

func (b *videoTSBuild) last() *videoTSStep {
	if len(b.c.steps) == 0 {
		panic("case " + b.c.name + ": an expectation before any step")
	}
	return &b.c.steps[len(b.c.steps)-1]
}

// ev states the events the last step emits, in order. Called once per step;
// a step with none simply has no call.
func (b *videoTSBuild) ev(events ...videoTSEvent) *videoTSBuild {
	s := b.last()
	if s.events != nil {
		panic("case " + b.c.name + ": events stated twice for one step")
	}
	s.events = append([]videoTSEvent{}, events...)
	return b
}

// facts states the facts after the last step.
func (b *videoTSBuild) facts(f videoTSFacts) *videoTSBuild {
	s := b.last()
	if s.facts != nil {
		panic("case " + b.c.name + ": facts stated twice for one step")
	}
	s.facts = &f
	return b
}

func (b *videoTSBuild) refEv(events ...videoTSEvent) *videoTSBuild {
	s := b.last()
	s.refEvents = append([]videoTSEvent{}, events...)
	s.refEventsSet = true
	return b
}

func (b *videoTSBuild) refFacts(f videoTSFacts) *videoTSBuild {
	b.last().refFacts = &f
	return b
}

func (b *videoTSBuild) diverges(class, why string) *videoTSBuild {
	switch class {
	case "defect", "quirk", "limitation", "divergence":
	default:
		panic("case " + b.c.name + ": unknown divergence class " + class)
	}
	b.c.class, b.c.divergence = class, why
	return b
}

// done checks the case is complete: the final facts are stated, and reference
// answers appear only on a case that says it diverges.
func (b *videoTSBuild) done() videoTSCase {
	if len(b.c.steps) == 0 {
		panic("case " + b.c.name + ": no steps")
	}
	if b.c.steps[len(b.c.steps)-1].facts == nil {
		panic("case " + b.c.name + ": the facts after the last step are not stated")
	}
	for i, s := range b.c.steps {
		if (s.refEventsSet || s.refFacts != nil) && b.c.divergence == "" {
			panic(fmt.Sprintf("case %s step %d: a reference answer without diverges", b.c.name, i))
		}
		if s.refFacts != nil && s.facts == nil {
			panic(fmt.Sprintf("case %s step %d: reference facts where none are authored", b.c.name, i))
		}
	}
	return b.c
}

// reference resolves what the reference is expected to answer for a step: the
// recorded difference where there is one, the authored answer otherwise.
func (s videoTSStep) reference() ([]videoTSEvent, *videoTSFacts) {
	events, facts := s.events, s.facts
	if s.refEventsSet {
		events = s.refEvents
	}
	if s.refFacts != nil {
		facts = s.refFacts
	}
	return events, facts
}

// --- the cases -------------------------------------------------------------

// Offsets. Every case opens with a PAT and a PMT, so the first video packet
// of a case that puts them in one chunk sits at packet index 2.
const (
	pkt0 = int64(0 * TSPacketSize)
	pkt2 = int64(2 * TSPacketSize)
	pkt3 = int64(3 * TSPacketSize)
	pkt4 = int64(4 * TSPacketSize)
	pkt5 = int64(5 * TSPacketSize)
	pkt6 = int64(6 * TSPacketSize)
)

//nolint:maintidx // a corpus is a list; splitting it would hide the list.
func videoTSCorpusCases() []videoTSCase {
	var cases []videoTSCase
	v := uint16(videoTSPID)

	// ===== H.264: entry points, access units, captures =====================

	{
		b := vNew("h264_idr_alone",
			"SPS, PPS and an IDR slice in one PES packet: an IDR is an entry point the moment its header is read", videoTSProgram)
		b.chunk(b.psi(0, h264Stream(v)), b.start(v, h264SPS, h264PPS, h264IDR)).
			ev(identityEv, identityEv, rapAt(pkt2, true)).
			facts(h264().ps(true).irap(1).clear(1).cleanrap(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("h264_next_pes_start_closes_the_access_unit",
			"the access unit is complete when the next PES packet begins; ParameterSetsSeen describes the new PES (V6, pinned as parity)", videoTSProgram)
		b.chunk(b.psi(0, h264Stream(v)), b.start(v, h264SPS, h264PPS, h264IDR)).
			ev(identityEv, identityEv, rapAt(pkt2, true)).
			facts(h264().ps(true).irap(1).clear(1).cleanrap(1))
		b.chunk(b.start(v, h264SliceP)).
			facts(h264().ps(false).irap(1).clear(2).cleanrap(1).cleanau(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("h264_four_byte_start_codes",
			"00 00 00 01 is read as a start code exactly like 00 00 01", videoTSProgram)
		b.chunk(b.psi(0, h264Stream(v)), b.start(v,
			nal4(0x67, 0x42, 0xC0, 0x1E), nal4(0x68, 0xCE, 0x38, 0x80), nal4(0x65, sliceI, 0x84, 0x21))).
			ev(identityEv, identityEv, rapAt(pkt2, true)).
			facts(h264().ps(true).irap(1).clear(1).cleanrap(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("h264_start_code_split_across_packets",
			"the IDR's start code has two bytes in one packet and one in the next", videoTSProgram)
		b.chunk(b.psi(0, h264Stream(v)),
			b.start(v, h264SPS, h264PPS, []byte{0x00, 0x00}),
			b.cont(v, []byte{0x01, 0x65, sliceI, 0x84, 0x21, 0xA0, 0x33, 0xFF})).
			ev(identityEv, identityEv, rapAt(pkt2, true)).
			facts(h264().ps(true).irap(1).clear(2).cleanrap(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("h264_start_code_split_across_ingest_calls",
			"the same split, with the ingest call boundary between the two packets; the scanner state carries", videoTSProgram)
		b.chunk(b.psi(0, h264Stream(v)), b.start(v, h264SPS, h264PPS, []byte{0x00, 0x00})).
			ev(identityEv, identityEv).
			facts(h264().ps(true).clear(1))
		b.chunk(b.cont(v, []byte{0x01, 0x65, sliceI, 0x84, 0x21, 0xA0, 0x33, 0xFF})).
			ev(rapAt(pkt2, true)).
			facts(h264().ps(true).irap(1).clear(2).cleanrap(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("h264_all_intra_access_unit_without_idr",
			"two I slices and no IDR: an entry point, reported when the access unit ends", videoTSProgram)
		b.chunk(b.psi(0, h264Stream(v)), b.start(v, h264SPS, h264PPS, h264SliceI, h264SliceI)).
			ev(identityEv, identityEv).
			facts(h264().ps(true).clear(1))
		b.chunk(b.start(v, h264SliceP)).
			ev(rapAt(pkt2, true)).
			facts(h264().ps(false).intra(1).clear(2).cleanrap(1).cleanau(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("h264_mixed_intra_and_predicted_is_rejected",
			"one I slice and one P slice: the picture references frames that were never delivered", videoTSProgram)
		b.chunk(b.psi(0, h264Stream(v)), b.start(v, h264SPS, h264PPS, h264SliceI, h264SliceP)).
			ev(identityEv, identityEv)
		b.chunk(b.start(v, h264SliceP)).
			facts(h264().ps(false).predrej(1).clear(2).cleanau(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("h264_unreadable_slice_header",
			"a slice header that cannot be read is counted as unreadable and admits nothing; the reference also counts it as predicted, which it was not shown to be (quirk, diagnostic only)", videoTSProgram)
		b.diverges("quirk", "an unreadable slice is counted in PredictedRejected as well as in UnreadableSlices; nothing decides on the former")
		b.chunk(b.psi(0, h264Stream(v)), b.start(v, h264SPS, h264PPS, h264SliceUnreadable)).
			ev(identityEv, identityEv)
		b.chunk(b.start(v, h264SliceP)).
			facts(h264().ps(false).unread(1).clear(2).cleanau(1)).
			refFacts(h264().ps(false).unread(1).predrej(1).clear(2).cleanau(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("h264_recovery_point_sei_before_idr",
			"a recovery_point SEI shorter than the capture budget, then the IDR: the SEI is read and corroborates (V5: capture reaches the next start code)", videoTSProgram)
		b.chunk(b.psi(0, h264Stream(v)), b.start(v, h264SPS, h264PPS, h264SEIRecovery, h264IDR)).
			ev(identityEv, identityEv, rapAt(pkt2, true)).
			facts(h264().ps(true).irap(1).rpsei(1).clear(1).cleanrap(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("h264_sei_without_recovery_point",
			"a pic_timing SEI only, then the IDR: no recovery point is fabricated from the bytes after it (V5)", videoTSProgram)
		b.chunk(b.psi(0, h264Stream(v)), b.start(v, h264SPS, h264PPS, h264SEIPicTiming, h264IDR)).
			ev(identityEv, identityEv, rapAt(pkt2, true)).
			facts(h264().ps(true).irap(1).clear(1).cleanrap(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("h264_sei_with_several_payloads_recovery_point_second",
			"pic_timing then recovery_point in one SEI NAL: the walk passes the first and finds the second", videoTSProgram)
		b.chunk(b.psi(0, h264Stream(v)), b.start(v, h264SPS, h264PPS, h264SEIBoth, h264IDR)).
			ev(identityEv, identityEv, rapAt(pkt2, true)).
			facts(h264().ps(true).irap(1).rpsei(1).clear(1).cleanrap(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("h264_recovery_point_corroborates_an_intra_entry",
			"recovery_point SEI and an all-intra picture: the SEI is recorded on the entry point the slices earned", videoTSProgram)
		b.chunk(b.psi(0, h264Stream(v)), b.start(v, h264SPS, h264PPS, h264SEIRecovery, h264SliceI)).
			ev(identityEv, identityEv)
		b.chunk(b.start(v, h264SliceP)).
			ev(rapAt(pkt2, true)).
			facts(h264().ps(false).intra(1).rpsei(1).clear(2).cleanrap(1).cleanau(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("h264_recovery_point_does_not_admit_a_predicted_picture",
			"recovery_point SEI on a P picture: corroboration is not admission", videoTSProgram)
		b.chunk(b.psi(0, h264Stream(v)), b.start(v, h264SPS, h264PPS, h264SEIRecovery, h264SliceP)).
			ev(identityEv, identityEv)
		b.chunk(b.start(v, h264SliceP)).
			facts(h264().ps(false).predrej(1).clear(2).cleanau(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("h264_sei_unterminated_then_idr",
			"an SEI without rbsp_trailing_bits runs straight into the IDR's start code: no recovery point is read out of the start code (V5)", videoTSProgram)
		b.chunk(b.psi(0, h264Stream(v)), b.start(v, h264SPS, h264PPS, h264SEIUnterminated, h264IDR)).
			ev(identityEv, identityEv, rapAt(pkt2, true)).
			facts(h264().ps(true).irap(1).clear(1).cleanrap(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("h264_next_nal_that_looks_like_an_sei_payload",
			"a pic_timing SEI followed by a slice whose bytes spell a recovery_point payload: the slice is a slice (V5)", videoTSProgram)
		b.chunk(b.psi(0, h264Stream(v)), b.start(v, h264SPS, h264PPS, h264SEIPicTiming, h264SliceLooksLikeSEI, h264IDR)).
			ev(identityEv, identityEv, rapAt(pkt2, true)).
			facts(h264().ps(true).irap(1).clear(1).cleanrap(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("h264_second_sei_is_read_on_its_own",
			"pic_timing SEI, then a separate recovery_point SEI, then the IDR: the second SEI is its own NAL and is read (V5)", videoTSProgram)
		b.chunk(b.psi(0, h264Stream(v)), b.start(v, h264SPS, h264PPS, h264SEIPicTiming, h264SEIRecovery, h264IDR)).
			ev(identityEv, identityEv, rapAt(pkt2, true)).
			facts(h264().ps(true).irap(1).rpsei(1).clear(1).cleanrap(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("h264_sei_longer_than_the_capture_budget_hides_the_recovery_point",
			"60 bytes of user data ahead of the recovery_point: the reference captures 48 and cannot reach it (V12, limitation; H.264 entry is by the slices, so diagnostic only)", videoTSProgram)
		b.diverges("limitation", "the SEI capture is 48 bytes; a recovery_point behind a longer payload is not seen")
		b.chunk(b.psi(0, h264Stream(v)), b.start(v, h264SPS, h264PPS, h264SEILongThenRecovery, h264SliceI)).
			ev(identityEv, identityEv)
		b.chunk(b.start(v, h264SliceP)).
			ev(rapAt(pkt2, true)).
			facts(h264().ps(false).intra(1).rpsei(1).clear(2).cleanrap(1).cleanau(1)).
			refFacts(h264().ps(false).intra(1).clear(2).cleanrap(1).cleanau(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("h264_no_parameter_sets_intra_is_not_an_entry_point",
			"an all-intra picture without SPS and PPS cannot configure a cold decoder; not rejected as predicted either", videoTSProgram)
		b.chunk(b.psi(0, h264Stream(v)), b.start(v, h264SliceI)).
			ev(identityEv, identityEv)
		b.chunk(b.start(v, h264SliceP)).
			facts(h264().ps(false).clear(2).cleanau(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("h264_idr_without_parameter_sets_is_indexed",
			"an IDR alone is an entry point; ParameterSetsSeen stays false and is a separate readiness criterion (V10, pinned as the reference's stated design)", videoTSProgram)
		b.chunk(b.psi(0, h264Stream(v)), b.start(v, h264IDR)).
			ev(identityEv, identityEv, rapAt(pkt2, true)).
			facts(h264().ps(false).irap(1).clear(1).cleanrap(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("h264_sps_without_pps_is_no_configuration",
			"an SPS alone does not configure a decoder", videoTSProgram)
		b.chunk(b.psi(0, h264Stream(v)), b.start(v, h264SPS, h264SliceI)).
			ev(identityEv, identityEv).
			facts(h264().ps(false).clear(1))
		b.chunk(b.start(v, h264SliceP)).
			facts(h264().ps(false).clear(2).cleanau(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("h264_one_nal_across_three_packets",
			"an IDR whose body continues over two more packets: the entry point is known at the header", videoTSProgram)
		b.chunk(b.psi(0, h264Stream(v)),
			b.start(v, h264SPS, h264PPS, []byte{0x00, 0x00, 0x01, 0x65, sliceI, 0x84}),
			b.cont(v, []byte{0x21, 0xA0, 0x33, 0xFF, 0xAA}),
			b.cont(v, []byte{0xBB, 0xCC, 0xDD})).
			ev(identityEv, identityEv, rapAt(pkt2, true)).
			facts(h264().ps(true).irap(1).clear(3).cleanrap(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("h264_slice_header_capture_spans_packets",
			"one byte of the slice header in the first packet, the rest in the next: the capture is assembled across them", videoTSProgram)
		b.chunk(b.psi(0, h264Stream(v)),
			b.start(v, h264SPS, h264PPS, []byte{0x00, 0x00, 0x01, 0x41, sliceI}),
			b.cont(v, []byte{0x84, 0x21, 0xA0, 0x33, 0xFF, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66})).
			ev(identityEv, identityEv)
		b.chunk(b.start(v, h264SliceP)).
			ev(rapAt(pkt2, true)).
			facts(h264().ps(false).intra(1).clear(3).cleanrap(1).cleanau(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("h264_second_idr_in_the_same_pes_is_one_entry_point",
			"two IDR slices of one picture: one entry point, at the PES", videoTSProgram)
		b.chunk(b.psi(0, h264Stream(v)), b.start(v, h264SPS, h264PPS, h264IDR, h264IDR)).
			ev(identityEv, identityEv, rapAt(pkt2, true)).
			facts(h264().ps(true).irap(1).clear(1).cleanrap(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("h264_access_unit_delimiter_does_not_split_the_pes",
			"two pictures separated by AUDs inside one PES packet are one access unit to this core: the boundary is the PES boundary by contract", videoTSProgram)
		b.chunk(b.psi(0, h264Stream(v)), b.start(v, h264AUD, h264SPS, h264PPS, h264SliceI, h264AUD, h264SliceP)).
			ev(identityEv, identityEv)
		b.chunk(b.start(v, h264SliceP)).
			facts(h264().ps(false).predrej(1).clear(2).cleanau(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("h264_partition_a_carries_the_slice_header",
			"slice data partition A (type 2) is a coded slice with a header to read", videoTSProgram)
		b.chunk(b.psi(0, h264Stream(v)), b.start(v, h264SPS, h264PPS, h264PartA)).
			ev(identityEv, identityEv)
		b.chunk(b.start(v, h264SliceP)).
			ev(rapAt(pkt2, true)).
			facts(h264().ps(false).intra(1).clear(2).cleanrap(1).cleanau(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("h264_partitions_b_and_c_are_not_slices",
			"partitions B and C (types 3, 4) carry no slice header and are not counted as coded slices", videoTSProgram)
		b.chunk(b.psi(0, h264Stream(v)), b.start(v, h264SPS, h264PPS, h264SliceI, h264PartB, h264PartC)).
			ev(identityEv, identityEv)
		b.chunk(b.start(v, h264SliceP)).
			ev(rapAt(pkt2, true)).
			facts(h264().ps(false).intra(1).clear(2).cleanrap(1).cleanau(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("h264_extension_nal_types_are_not_pictures",
			"prefix NAL (14) and coded slice extension (20) are not read; a PES with only those has no access unit", videoTSProgram)
		b.chunk(b.psi(0, h264Stream(v)), b.start(v, h264SPS, h264PPS, h264PrefixNAL, h264SliceExtension)).
			ev(identityEv, identityEv)
		b.chunk(b.start(v, h264SliceP)).
			facts(h264().ps(false).clear(2))
		cases = append(cases, b.done())
	}
	{
		b := vNew("h264_capture_beginning_mid_pes_has_no_coordinate",
			"a capture that starts in the middle of a PES packet sees the IDR but has no PES start to attach it to; the parameter sets are still seen", videoTSProgram)
		b.chunk(b.psi(0, h264Stream(v)), b.cont(v, h264SPS, h264PPS, h264IDR)).
			ev(identityEv, identityEv).
			facts(h264().ps(true).clear(1))
		b.chunk(b.start(v, h264SliceP)).
			facts(h264().ps(false).clear(2))
		cases = append(cases, b.done())
	}

	// ===== HEVC ============================================================

	{
		b := vNew("hevc_idr_is_an_immediate_entry_point",
			"VPS, SPS, PPS and IDR_W_RADL (19)", videoTSProgram)
		b.chunk(b.psi(0, hevcStream(v)), b.start(v, hevcVPS, hevcSPS, hevcPPS, hevcIDR)).
			ev(identityEv, identityEv, rapAt(pkt2, true)).
			facts(hevc().ps(true).irap(1).clear(1).cleanrap(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("hevc_irap_range_lower_bound_bla",
			"BLA_W_LP (16), the first type of the IRAP range", videoTSProgram)
		b.chunk(b.psi(0, hevcStream(v)), b.start(v, hevcVPS, hevcSPS, hevcPPS, hevcBLA)).
			ev(identityEv, identityEv, rapAt(pkt2, true)).
			facts(hevc().ps(true).irap(1).clear(1).cleanrap(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("hevc_irap_range_upper_bound_cra",
			"CRA_NUT (21), the last type of the IRAP range", videoTSProgram)
		b.chunk(b.psi(0, hevcStream(v)), b.start(v, hevcVPS, hevcSPS, hevcPPS, hevcCRA)).
			ev(identityEv, identityEv, rapAt(pkt2, true)).
			facts(hevc().ps(true).irap(1).clear(1).cleanrap(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("hevc_trail_with_recovery_point_is_an_intra_entry",
			"a TRAIL picture admitted on the stream's own recovery_point declaration; the second header byte is passed over before the SEI payload", videoTSProgram)
		b.chunk(b.psi(0, hevcStream(v)), b.start(v, hevcVPS, hevcSPS, hevcPPS, hevcSEIRecovery, hevcTrail)).
			ev(identityEv, identityEv)
		b.chunk(b.start(v, hevcTrail)).
			ev(rapAt(pkt2, true)).
			facts(hevc().ps(false).intra(1).rpsei(1).clear(2).cleanrap(1).cleanau(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("hevc_trail_without_recovery_point_is_rejected",
			"HEVC slice headers are not read for intra coding, so a TRAIL picture with no recovery_point is not joinable", videoTSProgram)
		b.chunk(b.psi(0, hevcStream(v)), b.start(v, hevcVPS, hevcSPS, hevcPPS, hevcTrail)).
			ev(identityEv, identityEv)
		b.chunk(b.start(v, hevcTrail)).
			facts(hevc().ps(false).predrej(1).clear(2).cleanau(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("hevc_without_vps_is_not_configured",
			"SPS and PPS but no VPS: not a configuration, so the recovery point admits nothing and nothing is counted as rejected", videoTSProgram)
		b.chunk(b.psi(0, hevcStream(v)), b.start(v, hevcSPS, hevcPPS, hevcSEIRecovery, hevcTrail)).
			ev(identityEv, identityEv).
			facts(hevc().ps(false).clear(1))
		b.chunk(b.start(v, hevcTrail)).
			facts(hevc().ps(false).clear(2).cleanau(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("hevc_without_sps_is_not_configured",
			"VPS and PPS but no SPS", videoTSProgram)
		b.chunk(b.psi(0, hevcStream(v)), b.start(v, hevcVPS, hevcPPS, hevcSEIRecovery, hevcTrail)).
			ev(identityEv, identityEv).
			facts(hevc().ps(false).clear(1))
		b.chunk(b.start(v, hevcTrail)).
			facts(hevc().ps(false).clear(2).cleanau(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("hevc_without_pps_is_not_configured",
			"VPS and SPS but no PPS", videoTSProgram)
		b.chunk(b.psi(0, hevcStream(v)), b.start(v, hevcVPS, hevcSPS, hevcSEIRecovery, hevcTrail)).
			ev(identityEv, identityEv).
			facts(hevc().ps(false).clear(1))
		b.chunk(b.start(v, hevcTrail)).
			facts(hevc().ps(false).clear(2).cleanau(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("hevc_suffix_sei_recovery_point_is_ignored",
			"recovery_point is a prefix SEI message; one carried in a suffix SEI (40) is not a declaration", videoTSProgram)
		b.chunk(b.psi(0, hevcStream(v)), b.start(v, hevcVPS, hevcSPS, hevcPPS, hevcTrail, hevcSuffixSEIRecovery)).
			ev(identityEv, identityEv)
		b.chunk(b.start(v, hevcTrail)).
			facts(hevc().ps(false).predrej(1).clear(2).cleanau(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("hevc_vcl_type_nine_counts_and_type_ten_does_not",
			"RASL_R (9) is a coded slice; RSV_VCL_N10 is reserved and a PES with only that has no access unit", videoTSProgram)
		b.chunk(b.psi(0, hevcStream(v)),
			b.start(v, hevcVPS, hevcSPS, hevcPPS, hevcSEIRecovery, hevcRASL),
			b.start(v, hevcVPS, hevcSPS, hevcPPS, hevcSEIRecovery, hevcReserved10),
			b.start(v, hevcTrail)).
			ev(identityEv, identityEv, rapAt(pkt2, true)).
			facts(hevc().ps(false).intra(1).rpsei(1).clear(3).cleanrap(1).cleanau(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("hevc_reserved_irap_types_are_not_entry_points",
			"RSV_IRAP_VCL22 is reserved: not an IRAP, not a slice", videoTSProgram)
		b.chunk(b.psi(0, hevcStream(v)), b.start(v, hevcVPS, hevcSPS, hevcPPS, hevcReserved22)).
			ev(identityEv, identityEv)
		b.chunk(b.start(v, hevcTrail)).
			facts(hevc().ps(false).clear(2))
		cases = append(cases, b.done())
	}
	{
		b := vNew("hevc_long_sei_hides_the_recovery_point_and_blocks_entry",
			"60 bytes of user data ahead of the recovery_point: on HEVC the SEI is the only admission, so the limitation decides (V12, limitation, decision-visible)", videoTSProgram)
		b.diverges("limitation", "the SEI capture is 48 bytes; on HEVC the recovery_point behind a longer payload is the only entry signal and is not seen")
		b.chunk(b.psi(0, hevcStream(v)), b.start(v, hevcVPS, hevcSPS, hevcPPS, hevcSEILongThenRecovery, hevcTrail)).
			ev(identityEv, identityEv)
		b.chunk(b.start(v, hevcTrail)).
			ev(rapAt(pkt2, true)).
			facts(hevc().ps(false).intra(1).rpsei(1).clear(2).cleanrap(1).cleanau(1)).
			refEv().
			refFacts(hevc().ps(false).predrej(1).clear(2).cleanau(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("hevc_prefix_sei_second_header_byte_is_not_payload",
			"a pic_timing SEI whose second header byte is 0x06: read as payload it would declare a recovery point", videoTSProgram)
		b.chunk(b.psi(0, hevcStream(v)), b.start(v, hevcVPS, hevcSPS, hevcPPS, hevcSEIPicTimingHeader06, hevcTrail)).
			ev(identityEv, identityEv)
		b.chunk(b.start(v, hevcTrail)).
			facts(hevc().ps(false).predrej(1).clear(2).cleanau(1))
		cases = append(cases, b.done())
	}

	// ===== MPEG-2 ==========================================================

	{
		b := vNew("mpeg2_i_picture_is_an_immediate_entry_point",
			"sequence header, GOP header, I picture and a slice: four start codes in one payload", videoTSProgram)
		b.chunk(b.psi(0, mpeg2Stream(v)), b.start(v, m2Seq, m2GOP, m2PicI, m2Slice)).
			ev(identityEv, identityEv, rapAt(pkt2, true)).
			facts(mpeg2().ps(true).irap(1).clear(1).cleanrap(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("mpeg2_i_picture_without_sequence_header_is_indexed",
			"an I picture alone is an entry point; the sequence header is a separate readiness criterion (V10)", videoTSProgram)
		b.chunk(b.psi(0, mpeg2Stream(v)), b.start(v, m2PicI, m2Slice)).
			ev(identityEv, identityEv, rapAt(pkt2, true)).
			facts(mpeg2().ps(false).irap(1).clear(1).cleanrap(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("mpeg2_predicted_pictures_are_not_entry_points",
			"P and B pictures with their sequence header: rejected; the reference never counts an MPEG-2 rejection because its parameter-set gate asks for a PPS MPEG-2 does not have (V9, quirk, diagnostic only)", videoTSProgram)
		b.diverges("quirk", "PredictedRejected is unreachable for MPEG-2: the finalize gate requires pesHasPPS, which no MPEG-2 start code sets")
		b.chunk(b.psi(0, mpeg2Stream(v)),
			b.start(v, m2Seq, m2PicP, m2Slice),
			b.start(v, m2Seq, m2PicB, m2Slice),
			b.start(v, m2Seq, m2PicP, m2Slice)).
			ev(identityEv, identityEv).
			facts(mpeg2().ps(true).predrej(2).clear(3).cleanau(2)).
			refFacts(mpeg2().ps(true).clear(3).cleanau(2))
		cases = append(cases, b.done())
	}
	{
		b := vNew("mpeg2_invalid_picture_coding_type_is_unreadable",
			"picture_coding_type 0 and 4 are not pictures anyone can classify", videoTSProgram)
		b.chunk(b.psi(0, mpeg2Stream(v)),
			b.start(v, m2Seq, m2PicBad0, m2Slice),
			b.start(v, m2Seq, m2PicBad4, m2Slice),
			b.start(v, m2Seq, m2PicP, m2Slice)).
			ev(identityEv, identityEv).
			facts(mpeg2().ps(true).unread(2).clear(3).cleanau(2))
		cases = append(cases, b.done())
	}
	{
		b := vNew("mpeg2_gop_header_is_not_a_decoder_configuration",
			"a GOP header without a sequence header configures nothing; the reference records it in a flag nothing reads for MPEG-2 (V9, GOP flag)", videoTSProgram)
		b.chunk(b.psi(0, mpeg2Stream(v)), b.start(v, m2GOP, m2PicP, m2Slice)).
			ev(identityEv, identityEv).
			facts(mpeg2().ps(false).clear(1))
		b.chunk(b.start(v, m2Seq, m2PicP, m2Slice)).
			facts(mpeg2().ps(true).clear(2).cleanau(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("mpeg2_start_code_split_across_packets",
			"picture_start_code with two bytes in one packet and the rest in the next", videoTSProgram)
		b.chunk(b.psi(0, mpeg2Stream(v)),
			b.start(v, m2Seq, []byte{0x00, 0x00}),
			b.cont(v, []byte{0x01, 0x00, 0x00, 0x08, 0xFF, 0xF8}, m2Slice)).
			ev(identityEv, identityEv, rapAt(pkt2, true)).
			facts(mpeg2().ps(true).irap(1).clear(2).cleanrap(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("mpeg2_picture_header_split_across_packets",
			"the picture_coding_type byte arrives in the next packet: the capture is assembled across them", videoTSProgram)
		b.chunk(b.psi(0, mpeg2Stream(v)),
			b.start(v, m2Seq, []byte{0x00, 0x00, 0x01, 0x00, 0x00}),
			b.cont(v, []byte{0x08, 0xFF, 0xF8}, m2Slice)).
			ev(identityEv, identityEv, rapAt(pkt2, true)).
			facts(mpeg2().ps(true).irap(1).clear(2).cleanrap(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("mpeg2_start_code_split_across_ingest_calls",
			"the split, with the ingest call boundary between the packets", videoTSProgram)
		b.chunk(b.psi(0, mpeg2Stream(v)), b.start(v, m2Seq, []byte{0x00, 0x00})).
			ev(identityEv, identityEv).
			facts(mpeg2().ps(true).clear(1))
		b.chunk(b.cont(v, []byte{0x01, 0x00, 0x00, 0x08, 0xFF, 0xF8}, m2Slice)).
			ev(rapAt(pkt2, true)).
			facts(mpeg2().ps(true).irap(1).clear(2).cleanrap(1))
		cases = append(cases, b.done())
	}

	// ===== Scrambling =======================================================

	{
		b := vNew("scrambled_video_is_not_scanned",
			"an encrypted packet carrying what would be SPS, PPS and IDR: counted, fed to nothing", videoTSProgram)
		b.chunk(b.psi(0, h264Stream(v)), scrambled(b.start(v, h264SPS, h264PPS, h264IDR))).
			ev(identityEv, identityEv).
			facts(h264().vscr(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("clear_scrambled_clear_breaks_the_run",
			"a scrambled packet inside a P picture: the clear run restarts and the access unit is not clean", videoTSProgram)
		b.chunk(b.psi(0, h264Stream(v)),
			b.start(v, h264SPS, h264PPS, h264SliceP),
			scrambled(b.cont(v, []byte{0xAA, 0xBB})),
			b.cont(v, []byte{0xCC, 0xDD})).
			ev(identityEv, identityEv).
			facts(h264().ps(true).vscr(1).vclr(2).vrun(1))
		b.chunk(b.start(v, h264SliceP)).
			facts(h264().ps(false).predrej(1).vscr(1).vclr(3).vrun(2))
		cases = append(cases, b.done())
	}
	{
		b := vNew("scrambled_packet_after_the_idr_header_in_its_access_unit",
			"the IDR header is clear, a later packet of the same picture is not: the provisional entry point is invalidated and CleanEntryPoints stays zero", videoTSProgram)
		b.chunk(b.psi(0, h264Stream(v)),
			b.start(v, h264SPS, h264PPS, h264IDR),
			scrambled(b.cont(v, []byte{0xAA, 0xBB}))).
			ev(identityEv, identityEv, rapAt(pkt2, true), rapInvalidatedAt(pkt2)).
			facts(h264().ps(true).irap(1).vscr(1).vclr(1).vrun(0))
		b.chunk(b.start(v, h264SliceP)).
			facts(h264().ps(false).irap(1).vscr(1).vclr(2).vrun(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("scrambled_packet_before_the_idr_in_its_access_unit",
			"a scrambled packet ahead of the IDR header in the same picture: the entry point is reported and is not joinable", videoTSProgram)
		b.chunk(b.psi(0, h264Stream(v)),
			b.start(v, h264SPS, h264PPS),
			scrambled(b.cont(v, []byte{0xAA, 0xBB})),
			b.cont(v, h264IDR)).
			ev(identityEv, identityEv, rapAt(pkt2, false)).
			facts(h264().ps(true).irap(1).vscr(1).vclr(2).vrun(1))
		b.chunk(b.start(v, h264SliceP)).
			facts(h264().ps(false).irap(1).vscr(1).vclr(3).vrun(2))
		cases = append(cases, b.done())
	}
	{
		b := vNew("scrambled_pes_start_is_invisible_and_merges_access_units",
			"the packet that starts the next PES is scrambled: the previous clear access unit is cleanly finalized, and the scrambled payload does not contaminate it", videoTSProgram)
		b.chunk(b.psi(0, h264Stream(v)),
			b.start(v, h264SPS, h264PPS, h264SliceP),
			scrambled(b.start(v, h264SPS, h264PPS, h264IDR)),
			b.start(v, h264SliceP)).
			ev(identityEv, identityEv).
			facts(h264().ps(false).predrej(1).vscr(1).vclr(2).vrun(1).cleanau(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("scrambled_video_is_confirmed_at_100_packets_and_no_clear_one",
			"99 scrambled packets are not a verdict, 100 are, and one clear packet withdraws it", videoTSProgram)
		b.chunk(b.psi(0, h264Stream(v))).
			ev(identityEv, identityEv)
		var ninetyNine [][]byte
		for i := 0; i < 99; i++ {
			ninetyNine = append(ninetyNine, scrambled(b.cont(v, []byte{0xAA})))
		}
		b.chunk(ninetyNine...).
			facts(h264().vscr(99))
		b.chunk(scrambled(b.cont(v, []byte{0xAA}))).
			facts(h264().vscr(100).scrconf(true))
		b.chunk(b.cont(v, []byte{0xAA})).
			facts(h264().vscr(100).vclr(1).vrun(1))
		cases = append(cases, b.done())
	}

	// ===== Transport: what is not read ======================================

	{
		b := vNew("wrong_sync_byte_is_ignored",
			"a packet that does not begin with 0x47 is not a packet, whatever it carries", videoTSProgram)
		b.chunk(b.psi(0, h264Stream(v)), withSyncByte(b.start(v, h264SPS, h264PPS, h264IDR), 0x46)).
			ev(identityEv, identityEv).
			facts(h264())
		cases = append(cases, b.done())
	}
	{
		b := vNew("reserved_adaptation_control_is_ignored",
			"adaptation_field_control 00 is reserved: the packet carries nothing", videoTSProgram)
		b.chunk(b.psi(0, h264Stream(v)),
			withAFC(audioTSPacket(v, true, b.next(v), audioTSPad(cat(audioTSPESHeader(0xE0, 0), h264SPS, h264PPS, h264IDR))), 0)).
			ev(identityEv, identityEv).
			facts(h264())
		cases = append(cases, b.done())
	}
	{
		b := vNew("adaptation_field_overrunning_the_packet_is_ignored",
			"an adaptation field longer than the packet describes nothing", videoTSProgram)
		b.chunk(b.psi(0, h264Stream(v)), withAdaptationLength(b.start(v, h264SPS, h264PPS, h264IDR), 200)).
			ev(identityEv, identityEv).
			facts(h264())
		cases = append(cases, b.done())
	}
	{
		b := vNew("adaptation_field_filling_the_packet_carries_no_payload",
			"adaptation_field_control 11 with a 183-byte field leaves no payload, whatever the control bits promised", videoTSProgram)
		b.chunk(b.psi(0, h264Stream(v)), withAdaptationLength(b.start(v, h264SPS, h264PPS, h264IDR), 183)).
			ev(identityEv, identityEv).
			facts(h264())
		cases = append(cases, b.done())
	}
	{
		b := vNew("adaptation_only_packet_is_ignored_even_when_scrambled",
			"a packet with no payload is not counted as scrambled or clear", videoTSProgram)
		b.chunk(b.psi(0, h264Stream(v)), scrambled(audioTSAdaptationOnly(v)), audioTSAdaptationOnly(v)).
			ev(identityEv, identityEv).
			facts(h264())
		cases = append(cases, b.done())
	}
	// ===== Transport Hardening: Duplicates, Continuity, TEI & DI (Step 7.0h) ==

	{
		b := vNew("duplicate_pes_start_packet_begins_a_second_access_unit",
			"the same PES-start packet twice, same counter and bytes: video keeps no continuity, so the copy ends one access unit and begins another", videoTSProgram)
		first := b.start(v, h264SPS, h264PPS, h264SliceI)
		b.chunk(b.psi(0, h264Stream(v)), first, first).
			ev(identityEv, identityEv).
			facts(h264().ps(true).clear(1))
		b.chunk(b.start(v, h264SliceP)).
			ev(rapAt(pkt2, true)).
			facts(h264().ps(false).intra(1).clear(2).cleanrap(1).cleanau(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("duplicate_video_continuation_must_not_alter_access_unit_facts",
			"a continuation packet duplicated with identical bytes and counter: must be dropped and not scanned twice", videoTSProgram)
		first := b.start(v, h264SPS, h264PPS, []byte{0x00, 0x00, 0x01, 0x65, sliceI})
		c1 := b.cont(v, []byte{0x84, 0x21, 0xA0, 0x33, 0xFF, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66})
		b.chunk(b.psi(0, h264Stream(v)), first, c1, c1).
			ev(identityEv, identityEv, rapAt(pkt2, true)).
			facts(h264().ps(true).irap(1).clear(2).cleanrap(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("continuity_gap_on_video_is_not_tracked",
			"a skipped continuity counter between the slice header's two halves: the corrupted slice must not be joined", videoTSProgram)
		b.next(v) // the counter value that is never sent
		first := b.start(v, h264SPS, h264PPS, []byte{0x00, 0x00, 0x01, 0x41, sliceI})
		b.next(v) // and the gap before the continuation
		b.chunk(b.psi(0, h264Stream(v)), first,
			b.cont(v, []byte{0x84, 0x21, 0xA0, 0x33, 0xFF, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66})).
			ev(identityEv, identityEv).
			facts(h264().ps(true).clear(2))
		b.chunk(b.start(v, h264SliceP)).
			ev().
			facts(h264().ps(false).clear(3).cleanau(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("same_cc_different_packet_on_video_continuation_breaks_access_unit",
			"same CC with different bytes on video continuation is broken transport: corrupted slice must not be joined", videoTSProgram)
		first := b.start(v, h264SPS, h264PPS, []byte{0x00, 0x00, 0x01, 0x41, sliceI})
		c1 := b.cont(v, []byte{0x84, 0x21, 0xA0, 0x33})
		c1Diff := audioTSShortPacket(v, false, (b.cc[v]-1)&0x0F, []byte{0xFF, 0xEE, 0xDD, 0xCC})
		b.chunk(b.psi(0, h264Stream(v)), first, c1, c1Diff).
			ev(identityEv, identityEv).
			facts(h264().ps(true).clear(3))
		b.chunk(b.start(v, h264SliceP)).
			ev().
			facts(h264().ps(false).clear(4).cleanau(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("tei_on_video_pusi_is_refused",
			"a video PUSI packet with TEI set carrying SPS, PPS and IDR: damaged transport must be dropped, no entry point admitted", videoTSProgram)
		badStart := withTEI(b.start(v, h264SPS, h264PPS, h264IDR))
		b.chunk(b.psi(0, h264Stream(v)), badStart).
			ev(identityEv, identityEv).
			facts(h264().clear(0))
		cases = append(cases, b.done())
	}
	{
		b := vNew("tei_on_video_continuation_is_refused",
			"a video continuation packet with TEI set: damaged slice data must be dropped and not joined", videoTSProgram)
		p0 := b.start(v, h264SPS, h264PPS, []byte{0x00, 0x00})
		p1 := withTEI(b.cont(v, []byte{0x01, 0x65, sliceI, 0x84, 0x21, 0xA0, 0x33, 0xFF}))
		b.chunk(b.psi(0, h264Stream(v)), p0, p1).
			ev(identityEv, identityEv).
			facts(h264().ps(true).clear(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("video_discontinuity_indicator_with_expected_next_cc",
			"discontinuity indicator with sequential CC on video PID is continuous", videoTSProgram)
		first := b.start(v, h264SPS, h264PPS, h264IDR)
		cont := audioTSShortPacketWithDI(v, false, b.next(v), []byte{0x11, 0x22, 0x33})
		b.chunk(b.psi(0, h264Stream(v)), first, cont).
			ev(identityEv, identityEv, rapAt(pkt2, true)).
			facts(h264().ps(true).irap(1).clear(2).cleanrap(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("video_discontinuity_indicator_while_in_header_discards_incomplete_pes",
			"discontinuity indicator while video PES header is incomplete discards PES and awaits next start", videoTSProgram)
		start := audioTSShortPacket(v, true, b.next(v), []byte{0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x80, 0x0A, 0x01, 0x02, 0x03, 0x04, 0x05})
		b.next(v) // jump CC
		c1 := audioTSShortPacketWithDI(v, false, b.next(v), cat(h264SPS, h264PPS, h264IDR))
		b.chunk(b.psi(0, h264Stream(v)), start, c1).
			ev(identityEv, identityEv).
			facts(h264().clear(2))
		cases = append(cases, b.done())
	}
	{
		b := vNew("video_unannounced_cc_jump_while_in_header_discards_incomplete_pes",
			"unannounced CC gap while video PES header is incomplete discards partial PES", videoTSProgram)
		start := audioTSShortPacket(v, true, b.next(v), []byte{0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x80, 0x0A, 0x01, 0x02, 0x03, 0x04, 0x05})
		b.next(v) // skip a counter value (unannounced gap)
		c1 := b.cont(v, h264SPS, h264PPS, h264IDR)
		b.chunk(b.psi(0, h264Stream(v)), start, c1).
			ev(identityEv, identityEv).
			facts(h264().clear(2))
		cases = append(cases, b.done())
	}

	// ===== PES boundary: V1, V2 and what is not a start =====================
	//
	// V1. A payload unit start on the video PID that is not a video PES start
	// establishes no elementary stream boundary. Nothing from it is video, and
	// nothing until the next payload unit start is either: those packets are
	// the body of the unit just refused. The authored answer follows from
	// 13818-1 2.4.3.6 the same way #968 did for audio.
	//
	// Authored here, for review in 7.0b: a payload unit start also ends the PES
	// packet before it (video PES packets are unbounded), so the access unit
	// that was open is complete and is finalized at the refused start, and the
	// coordinate of that PES is given up with it.
	//
	// The reference scans the refused payload and everything after it as
	// elementary stream, with the previous PES's coordinate still in hand.

	{
		b := vNew("invalid_pusi_wrong_prefix_after_a_valid_pes",
			"a payload unit starting 00 00 02 with an IDR inside, after a P picture's PES: no entry point (V1)", videoTSProgram)
		b.chunk(b.psi(0, h264Stream(v)), b.start(v, h264SPS, h264PPS, h264SliceP)).
			ev(identityEv, identityEv).
			facts(h264().ps(true).clear(1))
		b.chunk(b.raw(v, true, cat(bogusPESPrefix, h264IDR))).
			ev().
			facts(h264().ps(false).predrej(1).clear(2).cleanau(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("invalid_pusi_without_a_prior_pes_fabricates_configuration",
			"a refused payload unit carrying SPS and PPS, before any PES start: not a configuration (V1)", videoTSProgram)
		b.chunk(b.psi(0, h264Stream(v)), b.raw(v, true, cat(bogusPESPrefix, h264SPS, h264PPS, h264IDR))).
			ev(identityEv, identityEv).
			facts(h264().clear(1))
		b.chunk(b.start(v, h264SliceP)).
			facts(h264().clear(2))
		cases = append(cases, b.done())
	}
	{
		b := vNew("invalid_pusi_payload_shorter_than_a_pes_header",
			"a six-byte payload unit that is an IDR start code and header, after a P picture's PES (V1)", videoTSProgram)
		b.chunk(b.psi(0, h264Stream(v)), b.start(v, h264SPS, h264PPS, h264SliceP)).
			ev(identityEv, identityEv).
			facts(h264().ps(true).clear(1))
		b.chunk(b.raw(v, true, []byte{0x00, 0x00, 0x01, 0x65, sliceI, 0x84})).
			ev().
			facts(h264().ps(false).predrej(1).clear(2).cleanau(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("invalid_pusi_audio_stream_id_on_the_video_pid",
			"a well-formed PES header whose stream id is private_stream_1, with an IDR inside (V1)", videoTSProgram)
		b.chunk(b.psi(0, h264Stream(v)), b.start(v, h264SPS, h264PPS, h264SliceP)).
			ev(identityEv, identityEv).
			facts(h264().ps(true).clear(1))
		b.chunk(b.raw(v, true, cat(audioTSPESHeader(0xBD, 0), h264IDR))).
			ev().
			facts(h264().ps(false).predrej(1).clear(2).cleanau(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("invalid_pusi_padding_stream_id",
			"a padding_stream PES packet (0xBE, no optional header) with an IDR inside (V1)", videoTSProgram)
		b.chunk(b.psi(0, h264Stream(v)), b.start(v, h264SPS, h264PPS, h264SliceP)).
			ev(identityEv, identityEv).
			facts(h264().ps(true).clear(1))
		b.chunk(b.raw(v, true, cat([]byte{0x00, 0x00, 0x01, 0xBE, 0x00, 0x14}, h264IDR, repeat(0xFF, 4)))).
			ev().
			facts(h264().ps(false).predrej(1).clear(2).cleanau(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("invalid_pusi_hevc_irap_inside",
			"the same refusal on HEVC, with VPS, SPS, PPS and an IDR in the refused unit (V1)", videoTSProgram)
		b.chunk(b.psi(0, hevcStream(v)), b.start(v, hevcVPS, hevcSPS, hevcPPS, hevcTrail)).
			ev(identityEv, identityEv).
			facts(hevc().ps(true).clear(1))
		b.chunk(b.raw(v, true, cat(bogusPESPrefix, hevcVPS, hevcSPS, hevcPPS, hevcIDR))).
			ev().
			facts(hevc().ps(false).predrej(1).clear(2).cleanau(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("invalid_pusi_mpeg2_i_picture_inside",
			"MPEG-2: a PES with only sequence and GOP headers, then a refused unit carrying an I picture (V1)", videoTSProgram)
		b.chunk(b.psi(0, mpeg2Stream(v)), b.start(v, m2Seq, m2GOP)).
			ev(identityEv, identityEv).
			facts(mpeg2().ps(true).clear(1))
		b.chunk(b.raw(v, true, cat(bogusPESPrefix, m2PicI, m2Slice))).
			ev().
			facts(mpeg2().clear(2))
		cases = append(cases, b.done())
	}
	{
		b := vNew("invalid_pusi_continuations_are_quarantined_until_a_valid_pes",
			"a refused unit, its continuation carrying an IDR, then a valid PES with an IDR: one entry point, at the valid PES (V1)", videoTSProgram)
		b.chunk(b.psi(0, h264Stream(v)), b.start(v, h264SPS, h264PPS, h264SliceP)).
			ev(identityEv, identityEv).
			facts(h264().ps(true).clear(1))
		b.chunk(b.raw(v, true, cat(bogusPESPrefix, h264SPS)), b.cont(v, h264PPS, h264IDR)).
			ev().
			facts(h264().ps(false).predrej(1).clear(3).cleanau(1))
		b.chunk(b.start(v, h264SPS, h264PPS, h264IDR)).
			ev(rapAt(pkt5, true)).
			facts(h264().ps(true).irap(1).predrej(1).clear(4).cleanrap(1).cleanau(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("invalid_pusi_with_no_nal_like_bytes",
			"a refused unit carrying nothing that looks like a NAL: the difference is only when the open access unit is completed (V1, the finalize question for 7.0b)", videoTSProgram)
		b.chunk(b.psi(0, h264Stream(v)), b.start(v, h264SPS, h264PPS, h264SliceP)).
			ev(identityEv, identityEv).
			facts(h264().ps(true).clear(1))
		b.chunk(b.raw(v, true, cat(bogusPESPrefix, repeat(0xFF, 6)))).
			facts(h264().ps(false).predrej(1).clear(2).cleanau(1))
		b.chunk(b.start(v, h264SPS, h264PPS, h264IDR)).
			ev(rapAt(pkt4, true)).
			facts(h264().ps(true).irap(1).predrej(1).clear(3).cleanrap(1).cleanau(1))
		cases = append(cases, b.done())
	}

	// V2. A valid video PES start whose optional header reaches past its
	// packet: the boundary is established, the header is incomplete. The
	// authored answer strips the remainder from the next packet; the reference
	// keeps no header state and scans the remainder as elementary stream. The
	// same difference as the audio corpus records, and kept the same way.
	{
		b := vNew("video_pes_header_reaching_past_its_packet",
			"a ten-byte optional header with five bytes in the start packet; the five that follow spell an IDR start code and header, and are header (V2, divergence, same as audio)", videoTSProgram)
		b.diverges("divergence", "the reference has no cross-packet PES header state, so the remainder of the header is scanned as elementary stream")
		b.chunk(b.psi(0, h264Stream(v)),
			b.raw(v, true, []byte{0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x80, 0x0A, 0x01, 0x02, 0x03, 0x04, 0x05}),
			b.cont(v, []byte{0x00, 0x00, 0x01, 0x65, sliceI}, h264SPS, h264PPS, h264SliceP)).
			ev(identityEv, identityEv).
			facts(h264().ps(true).clear(2)).
			refEv(identityEv, identityEv, rapAt(pkt2, true)).
			refFacts(h264().ps(true).irap(1).clear(2).cleanrap(1))
		b.chunk(b.start(v, h264SliceP)).
			facts(h264().ps(false).predrej(1).clear(3).cleanau(1)).
			refFacts(h264().ps(false).irap(1).clear(3).cleanrap(1).cleanau(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("pes_header_ending_exactly_at_the_packet_end",
			"the optional header finishes with the packet; the elementary stream begins in the next one", videoTSProgram)
		b.chunk(b.psi(0, h264Stream(v)),
			b.raw(v, true, []byte{0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x80, 0x05, 0x01, 0x02, 0x03, 0x04, 0x05}),
			b.cont(v, h264SPS, h264PPS, h264IDR)).
			ev(identityEv, identityEv, rapAt(pkt2, true)).
			facts(h264().ps(true).irap(1).clear(2).cleanrap(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("pes_optional_header_inside_the_packet_is_stepped_over",
			"five optional header bytes that spell an IDR start code and header, inside the start packet: header, not elementary stream", videoTSProgram)
		b.chunk(b.psi(0, h264Stream(v)),
			b.startHdr(v, []byte{0x00, 0x00, 0x01, 0x65, sliceI}, h264SPS, h264PPS, h264SliceP)).
			ev(identityEv, identityEv)
		b.chunk(b.start(v, h264SliceP)).
			facts(h264().ps(false).predrej(1).clear(2).cleanau(1))
		cases = append(cases, b.done())
	}

	// ===== The coordinate ===================================================

	{
		b := vNew("entry_point_offset_counts_every_packet_on_the_transport",
			"two null packets between the tables and the IDR: the coordinate is the packet's position in the caller's stream", videoTSProgram)
		b.chunk(b.psi(0, h264Stream(v)), videoTSNullPacket(), videoTSNullPacket(), b.start(v, h264SPS, h264PPS, h264IDR)).
			ev(identityEv, identityEv, rapAt(pkt4, true)).
			facts(h264().ps(true).irap(1).clear(1).cleanrap(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("several_pes_and_one_carries_the_entry_point",
			"P, P without parameter sets, IDR, P: the entry point is at the IDR's PES; the second P is not counted as rejected because nothing configured it", videoTSProgram)
		b.chunk(b.psi(0, h264Stream(v)),
			b.start(v, h264SPS, h264PPS, h264SliceP),
			b.start(v, h264SliceP),
			b.start(v, h264SPS, h264PPS, h264IDR),
			b.start(v, h264SliceP)).
			ev(identityEv, identityEv, rapAt(pkt4, true)).
			facts(h264().ps(false).irap(1).predrej(1).clear(4).cleanrap(1).cleanau(3))
		cases = append(cases, b.done())
	}
	{
		b := vNew("intra_entry_point_is_reported_when_its_access_unit_ends",
			"an all-intra PES then an IDR PES in the next call: the intra entry point is reported first, at its own coordinate, then the IDR at its", videoTSProgram)
		b.chunk(b.psi(0, h264Stream(v)), b.start(v, h264SPS, h264PPS, h264SliceI)).
			ev(identityEv, identityEv).
			facts(h264().ps(true).clear(1))
		b.chunk(b.start(v, h264SPS, h264PPS, h264IDR)).
			ev(rapAt(pkt2, true), rapAt(pkt3, true)).
			facts(h264().ps(true).irap(1).intra(1).clear(2).cleanrap(2).cleanau(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("same_pid_after_a_pmt_change_has_no_coordinate",
			"a new PMT version keeps the video PID; an IDR arriving mid-PES afterwards has no PES start of its own to be attached to", videoTSProgram)
		b.chunk(b.psi(0, h264Stream(v)), b.start(v, h264SPS, h264PPS, h264SliceP)).
			ev(identityEv, identityEv).
			facts(h264().ps(true).clear(1))
		b.chunk(b.pmtOnly(1, h264Stream(v)), b.cont(v, h264IDR)).
			ev(identityEv).
			facts(h264().clear(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("target_change_drops_the_programme_and_its_coordinate",
			"selecting another programme ends this one: no video PID, nothing counted, and the way back needs a PAT", videoTSProgram)
		b.chunk(b.psi(0, h264Stream(v)), b.start(v, h264SPS, h264PPS, h264SliceP)).
			ev(identityEv, identityEv).
			facts(h264().ps(true).clear(1))
		b.target(videoTSOther).
			ev(identityEv).
			facts(noVideo())
		b.chunk(b.cont(v, h264IDR)).
			facts(noVideo())
		b.target(videoTSProgram).
			ev(identityEv).
			facts(noVideo())
		cases = append(cases, b.done())
	}
	{
		b := vNew("reselecting_the_same_programme_changes_nothing",
			"SetTargetProgram with the programme already followed is a no-op: no event, nothing reset", videoTSProgram)
		b.chunk(b.psi(0, h264Stream(v)), b.start(v, h264SPS, h264PPS, h264IDR)).
			ev(identityEv, identityEv, rapAt(pkt2, true)).
			facts(h264().ps(true).irap(1).clear(1).cleanrap(1))
		b.target(videoTSProgram).
			facts(h264().ps(true).irap(1).clear(1).cleanrap(1))
		b.chunk(b.start(v, h264SliceP)).
			facts(h264().ps(false).irap(1).clear(2).cleanrap(1).cleanau(1))
		cases = append(cases, b.done())
	}

	// ===== Lifecycle: nothing survives the table that named the stream ======

	{
		b := vNew("pmt_change_resets_scanner_capture_and_configuration",
			"a slice-header capture and a partial start code are open when the PMT changes; the bytes after it complete neither, and the old configuration is gone", videoTSProgram)
		b.chunk(b.psi(0, h264Stream(v)), b.start(v, h264SPS, h264PPS, []byte{0x00, 0x00, 0x01, 0x41, 0x00, 0x00})).
			ev(identityEv, identityEv).
			facts(h264().ps(true).clear(1))
		b.chunk(b.pmtOnly(1, h264Stream(v)), b.cont(v, repeat(0x00, 10), h264SPS, h264PPS)).
			ev(identityEv).
			facts(h264().ps(true).clear(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("pmt_change_discards_the_open_access_unit",
			"the IDR's access unit is open when the PMT changes: it is never completed, and every counter starts again", videoTSProgram)
		b.chunk(b.psi(0, h264Stream(v)), b.start(v, h264SPS, h264PPS, h264IDR)).
			ev(identityEv, identityEv, rapAt(pkt2, true)).
			facts(h264().ps(true).irap(1).clear(1).cleanrap(1))
		b.chunk(b.pmtOnly(1, h264Stream(v))).
			ev(identityEv).
			facts(h264())
		b.chunk(b.start(v, h264SPS, h264PPS, h264SliceP)).
			facts(h264().ps(true).clear(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("video_pid_change_moves_the_scanner",
			"the new PMT names another PID: the old one is no longer video, the new one is read from its first packet", videoTSProgram)
		b.chunk(b.psi(0, h264Stream(v)), b.start(v, h264SPS, h264PPS, h264SliceP)).
			ev(identityEv, identityEv).
			facts(h264().ps(true).clear(1))
		b.chunk(b.pmtOnly(1, h264Stream(videoTSPID2)),
			b.cont(v, h264IDR),
			b.start(videoTSPID2, h264SPS, h264PPS, h264IDR)).
			ev(identityEv, rapAt(pkt5, true)).
			facts(h264().vpid(videoTSPID2).ps(true).irap(1).clear(1).cleanrap(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("video_codec_change_on_the_same_pid",
			"the same PID declared MPEG-2 in the next PMT version: the H.264 state is gone and MPEG-2 start codes are read", videoTSProgram)
		b.chunk(b.psi(0, h264Stream(v)), b.start(v, h264SPS, h264PPS, h264IDR)).
			ev(identityEv, identityEv, rapAt(pkt2, true)).
			facts(h264().ps(true).irap(1).clear(1).cleanrap(1))
		b.chunk(b.pmtOnly(1, mpeg2Stream(v)), b.start(v, m2Seq, m2PicI, m2Slice)).
			ev(identityEv, rapAt(pkt4, true)).
			facts(mpeg2().ps(true).irap(1).clear(1).cleanrap(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("video_removed_and_added_again",
			"a PMT version without video, then one with it: nothing is read in between, and the stream begins again when it is named again", videoTSProgram)
		b.chunk(b.psi(0, h264Stream(v), ac3Stream(videoTSAudio)), b.start(v, h264SPS, h264PPS, h264IDR)).
			ev(identityEv, identityEv, rapAt(pkt2, true)).
			facts(h264().ps(true).irap(1).clear(1).cleanrap(1))
		b.chunk(b.pmtOnly(1, ac3Stream(videoTSAudio)), b.start(v, h264SPS, h264PPS, h264IDR)).
			ev(identityEv).
			facts(noVideo())
		b.chunk(b.pmtOnly(2, h264Stream(v), ac3Stream(videoTSAudio)), b.start(v, h264SPS, h264PPS, h264IDR)).
			ev(identityEv, rapAt(pkt6, true)).
			facts(h264().ps(true).irap(1).clear(1).cleanrap(1))
		cases = append(cases, b.done())
	}
	{
		b := vNew("target_zero_follows_the_first_programme_the_pat_lists",
			"no programme named: the first entry of the PAT is followed, and its video is read", 0)
		b.chunk(
			audioTSPSIPacket(0, b.next(0), videoTSPAT([2]uint16{videoTSProgram, audioTSPMTPID}, [2]uint16{videoTSOther, videoTSPMTPID2})),
			b.pmtOnly(0, h264Stream(v)),
			b.start(v, h264SPS, h264PPS, h264IDR)).
			ev(identityEv, identityEv, rapAt(pkt2, true)).
			facts(h264().ps(true).irap(1).clear(1).cleanrap(1))
		cases = append(cases, b.done())
	}

	// ===== Routing ==========================================================

	{
		b := vNew("audio_pid_bytes_never_reach_the_video_scanner",
			"an audio PES on its own PID carrying what would be an IDR: nothing about the video changes", videoTSProgram)
		b.chunk(b.psi(0, h264Stream(v), ac3Stream(videoTSAudio)),
			b.raw(videoTSAudio, true, cat(audioTSPESHeader(0xBD, 0), h264SPS, h264PPS, h264IDR))).
			ev(identityEv, identityEv).
			facts(h264())
		cases = append(cases, b.done())
	}

	return cases
}

// --- running ---------------------------------------------------------------

// videoTSStepResult is what one step produced: its events and the facts after
// it. Facts are read after every step; which of them the corpus states is the
// author's choice, and rechunking compares all of them.
type videoTSStepResult struct {
	events []videoTSEvent
	facts  videoTSFacts
}

type videoTSRun struct {
	steps []videoTSStepResult
}

// runVideoTSCase drives the reference core through a case.
//
// rechunk may cut a chunk step into several calls. The events of those calls
// are gathered under the one step, because where a call begins and ends is
// the caller's business: the byte that triggers an event is in exactly one
// call whichever way the chunk is cut, so the set per step is the same.
func runVideoTSCase(t *testing.T, c videoTSCase, rechunk func([]byte) [][]byte) videoTSRun {
	t.Helper()
	ctx := t.Context()

	core := NewGoCore(c.initial)
	var run videoTSRun
	offset := int64(0)
	for i, step := range c.steps {
		var result videoTSStepResult
		switch step.kind {
		case videoTSStepChunk:
			parts := [][]byte{step.chunk}
			if rechunk != nil {
				parts = rechunk(step.chunk)
			}
			var facts Facts
			for _, part := range parts {
				res, err := core.Ingest(ctx, offset, part)
				if err != nil {
					t.Fatalf("case %s step %d: ingest: %v", c.name, i, err)
				}
				offset += int64(len(part))
				result.events = append(result.events, videoTSEventsOf(res.Events)...)
				facts = res.Facts
			}
			result.facts = videoTSFactsOf(facts)
		case videoTSStepTarget:
			res, err := core.SetTargetProgram(ctx, step.target)
			if err != nil {
				t.Fatalf("case %s step %d: set target: %v", c.name, i, err)
			}
			result.events = videoTSEventsOf(res.Events)
			result.facts = videoTSFactsOf(res.Facts)
		}
		run.steps = append(run.steps, result)
	}
	return run
}

func videoTSEventsOf(events []Event) []videoTSEvent {
	out := make([]videoTSEvent, 0, len(events))
	for _, e := range events {
		switch e.Kind {
		case EventRandomAccessPoint:
			out = append(out, rapAt(e.Offset, e.Joinable))
		case EventRandomAccessPointInvalidated:
			out = append(out, rapInvalidatedAt(e.Offset))
		case EventProgramIdentityChanged:
			out = append(out, identityEv)
		default:
			out = append(out, videoTSEvent{kind: fmt.Sprintf("kind(%d)", e.Kind), offset: e.Offset})
		}
	}
	return out
}

// --- rendering -------------------------------------------------------------

func videoTSEventLine(kind string, e videoTSEvent) string {
	if e.kind == "rap" {
		return fmt.Sprintf("  %s rap offset=%d joinable=%d", kind, e.offset, b2i(e.joinable))
	}
	if e.kind == "rap_invalidated" {
		return fmt.Sprintf("  %s rap_invalidated offset=%d", kind, e.offset)
	}
	return fmt.Sprintf("  %s %s", kind, e.kind)
}

func videoTSFactsLine(kind string, f videoTSFacts) string {
	return fmt.Sprintf("  %s ps=%d irap=%d intra=%d rpsei=%d predrej=%d unread=%d vscr=%d vclr=%d vrun=%d cleanrap=%d cleanau=%d scrconf=%d vpid=%04x codec=%s",
		kind, b2i(f.paramSets), f.irapPoints, f.intraPoints, f.recoverySEIs, f.predictedRejected, f.unreadable,
		f.videoScrambled, f.videoClear, f.videoClearRun, f.cleanEntry, f.cleanAUs, b2i(f.scrambledConfirmed),
		f.videoPID, f.codec)
}

func renderVideoTSCorpus(cases []videoTSCase) string {
	var b strings.Builder
	b.WriteString("# xg2g raw transport to video facts corpus, format version 1\n")
	b.WriteString("#\n")
	b.WriteString("# Generated. To change it, edit videoTSCorpusCases() in\n")
	b.WriteString("# backend/internal/stream/ingest/mediafacts/videots_corpus_test.go and run\n")
	b.WriteString("#   go test ./internal/stream/ingest/mediafacts/ -run TestVideoTSCorpus -update-video-ts-corpus\n")
	b.WriteString("#\n")
	b.WriteString("# A case is raw MPEG-TS and what a media core must report for it:\n")
	b.WriteString("#\n")
	b.WriteString("#   program <n>     the programme the core is constructed to follow\n")
	b.WriteString("#   chunk <hex>     one ingest of these bytes, at the offset the previous ones reached\n")
	b.WriteString("#   target <n>      one change of the programme being followed\n")
	b.WriteString("#   event rap offset=<n> joinable=<0|1>\n")
	b.WriteString("#                   a random access point the preceding call emitted, in order;\n")
	b.WriteString("#                   offset is the caller's byte coordinate of the packet that\n")
	b.WriteString("#                   began the access unit's PES packet\n")
	b.WriteString("#   event rap_invalidated offset=<n>\n")
	b.WriteString("#                   a previously emitted random access point was corrupted later\n")
	b.WriteString("#                   in its access unit and is no longer an attach point\n")
	b.WriteString("#   event identityEv  a programme identityEv change the preceding call emitted\n")
	b.WriteString("#   facts ...       the video facts after the preceding call, where stated:\n")
	b.WriteString("#                   ps ParameterSetsSeen; irap intra rpsei predrej unread the\n")
	b.WriteString("#                   RandomAccess counters; vscr vclr vrun the video scrambling\n")
	b.WriteString("#                   observation; cleanrap CleanEntryPoints; cleanau\n")
	b.WriteString("#                   CleanAccessUnits; scrconf ScrambledVideoConfirmed; vpid and\n")
	b.WriteString("#                   codec the stream the table names. The last step always\n")
	b.WriteString("#                   states them.\n")
	b.WriteString("#\n")
	b.WriteString("# Events belong to the call that emitted them. Where a call begins and ends\n")
	b.WriteString("# is otherwise not part of the case: the same bytes cut into other calls\n")
	b.WriteString("# emit the same events under the same steps and leave the same facts, which\n")
	b.WriteString("# is a test of its own rather than a line in this file.\n")
	b.WriteString("#\n")
	b.WriteString("# A case carrying `diverges <class>: <why>` is one where the reference does\n")
	b.WriteString("# something else. Its `ref-event` and `ref-facts` lines are the reference's\n")
	b.WriteString("# whole answer; the `event` and `facts` lines stay the authored one. The\n")
	b.WriteString("# class says what kind of difference it is: defect, quirk, limitation or\n")
	b.WriteString("# divergence. None of them is adopted.\n")
	b.WriteString("version 1\n")

	for _, c := range cases {
		b.WriteString("\ncase " + c.name + "\n")
		b.WriteString("  desc " + c.desc + "\n")
		b.WriteString(fmt.Sprintf("  program %d\n", c.initial))
		if c.divergence != "" {
			b.WriteString("  diverges " + c.class + ": " + c.divergence + "\n")
		}
		for _, s := range c.steps {
			switch s.kind {
			case videoTSStepChunk:
				b.WriteString("  chunk " + hex.EncodeToString(s.chunk) + "\n")
			case videoTSStepTarget:
				b.WriteString(fmt.Sprintf("  target %d\n", s.target))
			}
			for _, e := range s.events {
				b.WriteString(videoTSEventLine("event", e) + "\n")
			}
			if s.facts != nil {
				b.WriteString(videoTSFactsLine("facts", *s.facts) + "\n")
			}
			if c.divergence != "" {
				refEvents, refFacts := s.reference()
				for _, e := range refEvents {
					b.WriteString(videoTSEventLine("ref-event", e) + "\n")
				}
				if refFacts != nil {
					b.WriteString(videoTSFactsLine("ref-facts", *refFacts) + "\n")
				}
			}
		}
		b.WriteString("end\n")
	}
	return b.String()
}

// --- comparing -------------------------------------------------------------

// videoTSExpectation is one side of a comparison: per step, the events and
// the facts where stated. The authored answer and the reference's answer are
// both one of these, and so is a run once everything it produced is kept.
type videoTSExpectation struct {
	events [][]videoTSEvent
	facts  []*videoTSFacts
}

func videoTSAuthored(c videoTSCase) videoTSExpectation {
	var e videoTSExpectation
	for _, s := range c.steps {
		e.events = append(e.events, s.events)
		e.facts = append(e.facts, s.facts)
	}
	return e
}

func videoTSReference(c videoTSCase) videoTSExpectation {
	var e videoTSExpectation
	for _, s := range c.steps {
		events, facts := s.reference()
		e.events = append(e.events, events)
		e.facts = append(e.facts, facts)
	}
	return e
}

func videoTSOfRun(r videoTSRun) videoTSExpectation {
	var e videoTSExpectation
	for _, s := range r.steps {
		f := s.facts
		e.events = append(e.events, s.events)
		e.facts = append(e.facts, &f)
	}
	return e
}

// videoTSDiff lists every way a run differs from an expectation. Facts are
// compared only at the steps the expectation states them for; events at every
// step, including the absence of any.
func videoTSDiff(got videoTSRun, want videoTSExpectation) []string {
	var bad []string
	if len(got.steps) != len(want.events) {
		return []string{fmt.Sprintf("  steps got %d, want %d", len(got.steps), len(want.events))}
	}
	for i, step := range got.steps {
		g, w := step.events, want.events[i]
		if len(g) != len(w) {
			bad = append(bad, fmt.Sprintf("  step %d: %d events, want %d", i, len(g), len(w)))
		}
		for j := 0; j < len(g) && j < len(w); j++ {
			gl, wl := videoTSEventLine("event", g[j]), videoTSEventLine("event", w[j])
			if gl != wl {
				bad = append(bad, fmt.Sprintf("  step %d event %d\n    got  %s\n    want %s", i, j, gl, wl))
			}
		}
		for j := len(w); j < len(g); j++ {
			bad = append(bad, fmt.Sprintf("  step %d: unexpected %s", i, videoTSEventLine("event", g[j])))
		}
		for j := len(g); j < len(w); j++ {
			bad = append(bad, fmt.Sprintf("  step %d: missing    %s", i, videoTSEventLine("event", w[j])))
		}
		if want.facts[i] != nil {
			gl, wl := videoTSFactsLine("facts", step.facts), videoTSFactsLine("facts", *want.facts[i])
			if gl != wl {
				bad = append(bad, fmt.Sprintf("  step %d facts\n    got  %s\n    want %s", i, gl, wl))
			}
		}
	}
	return bad
}

// --- the tests -------------------------------------------------------------

// The reference answers the corpus.
//
// For every case that does not diverge this is "the authored expectation is
// what the Go core does". For a diverging case it is "the reference does the
// other thing, exactly as recorded" - which is a test too: an unnoticed change
// to that behaviour would fail here rather than quietly becoming the new
// reference, and a fix that lands would fail here until the case is
// reclassified as agreeing.
func TestVideoTSCorpus_TheGoCoreMeetsTheAuthoredExpectations(t *testing.T) {
	for _, c := range videoTSCorpusCases() {
		t.Run(c.name, func(t *testing.T) {
			run := runVideoTSCase(t, c, nil)
			want := videoTSAuthored(c)
			if c.divergence != "" {
				want = videoTSReference(c)
			}
			if bad := videoTSDiff(run, want); len(bad) > 0 {
				t.Errorf("case %s:\n%s", c.name, strings.Join(bad, "\n"))
			}
		})
	}
}

// A diverging case has to actually diverge. A case that says the reference
// does something else, whose recorded reference answer is the authored one,
// is a classification with nothing behind it - and a fix that lands would
// leave it standing as a finding that no longer exists.
func TestVideoTSCorpus_EveryClassifiedDivergenceIsReal(t *testing.T) {
	for _, c := range videoTSCorpusCases() {
		if c.divergence == "" {
			continue
		}
		t.Run(c.name, func(t *testing.T) {
			run := runVideoTSCase(t, c, nil)
			if bad := videoTSDiff(run, videoTSAuthored(c)); len(bad) == 0 {
				t.Errorf("case %s is classified %s but the reference meets the authored expectation", c.name, c.class)
			}
		})
	}
}

// Where an ingest call was cut is the caller's business and no part of the
// answer. The same transport handed over in other pieces has to emit the same
// events under the same steps, with the same offsets, and leave the same
// facts after every step.
func TestVideoTSCorpus_WhereAChunkWasCutChangesNothing(t *testing.T) {
	chunkings := []struct {
		name string
		cut  func([]byte) [][]byte
	}{
		{"one packet per call", func(b []byte) [][]byte { return audioTSCut(b, TSPacketSize) }},
		{"irregular small calls", videoTSCutIrregular},
		{"64 KiB of packets per call", func(b []byte) [][]byte { return audioTSCut(b, 348*TSPacketSize) }},
		{"everything in one call", func(b []byte) [][]byte { return [][]byte{b} }},
	}
	for _, c := range videoTSCorpusCases() {
		base := runVideoTSCase(t, c, nil)
		for _, ch := range chunkings {
			t.Run(c.name+"/"+ch.name, func(t *testing.T) {
				got := runVideoTSCase(t, c, ch.cut)
				if bad := videoTSDiff(got, videoTSOfRun(base)); len(bad) > 0 {
					t.Errorf("case %s under %s:\n%s", c.name, ch.name, strings.Join(bad, "\n"))
				}
			})
		}
	}
}

// videoTSCutIrregular cuts a chunk into calls of 1, 3, 2, 1 and 4 packets in
// turn, so that no two consecutive boundaries are the same distance apart.
func videoTSCutIrregular(data []byte) [][]byte {
	sizes := []int{1, 3, 2, 1, 4}
	var out [][]byte
	for i := 0; len(data) > 0; i++ {
		n := sizes[i%len(sizes)] * TSPacketSize
		if n > len(data) {
			n = len(data)
		}
		out = append(out, data[:n])
		data = data[n:]
	}
	return out
}

// The checked-in file is the cases, rendered. Both implementations read it,
// so it is the artefact the agreement is a property of.
func TestVideoTSCorpus_TheCheckedInFileMatchesTheCases(t *testing.T) {
	want := renderVideoTSCorpus(videoTSCorpusCases())
	if *updateVideoTSCorpus {
		if err := os.MkdirAll("../../../../../testdata/video-ts-corpus", 0o750); err != nil {
			t.Fatalf("create corpus directory: %v", err)
		}
		if err := os.WriteFile(videoTSCorpusPath, []byte(want), 0o644); err != nil { // #nosec G306 -- a checked-in fixture
			t.Fatalf("write corpus: %v", err)
		}
		t.Log("corpus rewritten")
		return
	}
	got, err := os.ReadFile(videoTSCorpusPath)
	if err != nil {
		t.Fatalf("read corpus: %v\nrun: go test ./internal/stream/ingest/mediafacts/ -run TestVideoTSCorpus -update-video-ts-corpus", err)
	}
	if string(got) != want {
		t.Fatalf("the checked-in corpus is not what the cases render; run:\n" +
			"  go test ./internal/stream/ingest/mediafacts/ -run TestVideoTSCorpus -update-video-ts-corpus")
	}
}

// The diverging cases are the reviewed ones, by name and class. One appearing
// or disappearing is a finding, not a corpus update: a new one is a reference
// behaviour nobody has classified, and a missing one is either a fix that
// landed (reclassify the case as agreeing) or a case that stopped testing
// what it claims.
func TestVideoTSCorpus_OnlyTheClassifiedDivergencesExist(t *testing.T) {
	want := map[string]string{
		"h264_unreadable_slice_header":                                     "quirk",
		"h264_sei_longer_than_the_capture_budget_hides_the_recovery_point": "limitation",
		"hevc_long_sei_hides_the_recovery_point_and_blocks_entry":          "limitation",
		"mpeg2_predicted_pictures_are_not_entry_points":                    "quirk",
		"video_pes_header_reaching_past_its_packet":                        "divergence",
	}
	got := map[string]string{}
	for _, c := range videoTSCorpusCases() {
		if c.divergence != "" {
			got[c.name] = c.class
		}
	}
	for name, class := range want {
		if got[name] != class {
			t.Errorf("case %s: want class %q, got %q", name, class, got[name])
		}
	}
	for name, class := range got {
		if _, ok := want[name]; !ok {
			t.Errorf("case %s diverges (%s) but is not in the reviewed list", name, class)
		}
	}
}

// Every case name is used once, and every case has the steps it claims.
func TestVideoTSCorpus_CaseNamesAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range videoTSCorpusCases() {
		if seen[c.name] {
			t.Errorf("case %s appears twice", c.name)
		}
		seen[c.name] = true
	}
}

// The comparison notices what it is meant to notice. A corpus whose diff
// function passed a dropped event, a moved offset, a flipped flag or a changed
// counter would pass everything, so each is tried against a real run.
func TestVideoTSCorpus_TheComparisonIsSensitive(t *testing.T) {
	var c videoTSCase
	for _, candidate := range videoTSCorpusCases() {
		if candidate.name == "intra_entry_point_is_reported_when_its_access_unit_ends" {
			c = candidate
		}
	}
	if c.name == "" {
		t.Fatal("the sensitivity case is missing from the corpus")
	}
	base := runVideoTSCase(t, c, nil)
	if bad := videoTSDiff(base, videoTSAuthored(c)); len(bad) > 0 {
		t.Fatalf("the sensitivity case must pass before it is perturbed:\n%s", strings.Join(bad, "\n"))
	}

	perturb := map[string]func(e *videoTSExpectation){
		"an event dropped": func(e *videoTSExpectation) {
			e.events[1] = e.events[1][:1]
		},
		"an event added": func(e *videoTSExpectation) {
			e.events[0] = append(e.events[0], rapAt(pkt0, true))
		},
		"an offset moved by one packet": func(e *videoTSExpectation) {
			e.events[1] = []videoTSEvent{rapAt(pkt2+TSPacketSize, true), e.events[1][1]}
		},
		"two events swapped": func(e *videoTSExpectation) {
			e.events[1] = []videoTSEvent{e.events[1][1], e.events[1][0]}
		},
		"joinable flipped": func(e *videoTSExpectation) {
			e.events[1] = []videoTSEvent{rapAt(pkt2, false), e.events[1][1]}
		},
		"a counter changed": func(e *videoTSExpectation) {
			f := *e.facts[1]
			f.cleanEntry++
			e.facts[1] = &f
		},
		"a flag changed": func(e *videoTSExpectation) {
			f := *e.facts[1]
			f.paramSets = !f.paramSets
			e.facts[1] = &f
		},
		"the codec changed": func(e *videoTSExpectation) {
			f := *e.facts[1]
			f.codec = CodecH265
			e.facts[1] = &f
		},
	}
	for name, p := range perturb {
		t.Run(name, func(t *testing.T) {
			want := videoTSAuthored(c)
			want.events = append([][]videoTSEvent{}, want.events...)
			for i := range want.events {
				want.events[i] = append([]videoTSEvent{}, want.events[i]...)
			}
			want.facts = append([]*videoTSFacts{}, want.facts...)
			p(&want)
			if bad := videoTSDiff(base, want); len(bad) == 0 {
				t.Errorf("the comparison did not notice: %s", name)
			}
		})
	}
}
