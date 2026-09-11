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

	"github.com/ManuGH/xg2g/internal/stream/ingest/esaudio"
)

// The raw-transport-to-audio corpus.
//
// Everything above this file already had a corpus of its own. The audio
// observer has one of elementary stream feeds, the PSI parser one of tables.
// What neither of them could say is which bytes are elementary stream in the
// first place: that answer lives between the transport header, the table that
// says which PID carries audio, and the PES header that says where the payload
// of a packet stops being a header. This is the corpus for that answer.
//
// A case is raw transport, and its expectation is the feeds an observer must be
// given - in order, per stream incarnation, with the boundaries kept. The
// expectations are authored from ISO/IEC 13818-1 and ATSC A/52 rather than
// recorded from either implementation, because a corpus written down from a
// parser proves that the parser is self-consistent and nothing else.

var updateAudioTSCorpus = flag.Bool("update-audio-ts-corpus", false, "rewrite the checked-in raw-TS audio corpus")

const audioTSCorpusPath = "../../../../../testdata/audio-ts-corpus/corpus.txt"

// --- the shape of a case ---------------------------------------------------

type audioTSStepKind int

const (
	audioTSStepChunk audioTSStepKind = iota
	audioTSStepTarget
)

type audioTSStep struct {
	kind   audioTSStepKind
	chunk  []byte
	target uint16
}

// audioTSFeed is one run of elementary stream bytes an observer must be given.
//
// incarnation is not an epoch number. It counts the stream lifetimes a case
// produces, in the order their first feed appears, so that two implementations
// can be held to splitting the stream in the same places without being held to
// the same internal counter.
type audioTSFeed struct {
	incarnation int
	pid         uint16
	es          []byte
	obs         esaudio.Observation
}

// audioTSStream is one stream still being followed when the case ends.
type audioTSStream struct {
	pid   uint16
	codec string
	feeds int
	obs   esaudio.Observation
}

type audioTSCase struct {
	name    string
	desc    string
	initial uint16
	steps   []audioTSStep
	want    []audioTSFeed
	streams []audioTSStream

	// divergence names a case where the reference is known to do something else,
	// and reference holds what it does. Exactly one case has this, and the
	// reason is in the corpus file beside it rather than only in the pull
	// request that added it.
	divergence      string
	reference       []audioTSFeed
	referenceStream []audioTSStream
}

// --- fixtures --------------------------------------------------------------

const (
	audioTSPMTPID  = 0x1000
	audioTSAudioA  = 0x0100
	audioTSAudioB  = 0x0101
	audioTSProgram = 1
	audioTSOther   = 2
)

// audioTSCRC32 is the ISO/IEC 13818-1 CRC, computed bit by bit. Deliberately
// not the package's own: a section whose CRC came from the function that later
// validates it agrees with that function even when both are wrong.
func audioTSCRC32(data []byte) uint32 {
	crc := uint32(0xFFFFFFFF)
	for _, b := range data {
		for bit := 7; bit >= 0; bit-- {
			top := (crc >> 31) & 1
			in := uint32((b >> uint(bit)) & 1)
			crc <<= 1
			if top^in == 1 {
				crc ^= 0x04C11DB7
			}
		}
	}
	return crc
}

func audioTSSeal(body []byte) []byte {
	crc := audioTSCRC32(body)
	return append(body, byte(crc>>24), byte(crc>>16), byte(crc>>8), byte(crc))
}

func audioTSSection(tableID byte, idExtension uint16, version byte, payload []byte) []byte {
	sectionLen := 9 + len(payload) // through CRC
	body := []byte{
		tableID,
		0xB0 | byte((sectionLen>>8)&0x0F),
		byte(sectionLen & 0xFF),
		byte(idExtension >> 8),
		byte(idExtension & 0xFF),
		0xC1 | (version << 1),
		0x00,
		0x00,
	}
	body = append(body, payload...)
	return audioTSSeal(body)
}

func audioTSPAT(program uint16) []byte {
	return audioTSSection(0x00, 1, 0, []byte{
		byte(program >> 8), byte(program & 0xFF),
		0xE0 | byte(audioTSPMTPID>>8), byte(audioTSPMTPID & 0xFF),
	})
}

// audioTSEs is one elementary stream entry of a PMT.
type audioTSEs struct {
	streamType  byte
	pid         uint16
	descriptors []byte
}

func audioTSPMT(program uint16, version byte, streams ...audioTSEs) []byte {
	payload := []byte{
		0xE0 | byte(audioTSPMTPID>>8), byte(audioTSPMTPID & 0xFF),
		0xF0, 0x00,
	}
	for _, s := range streams {
		payload = append(payload,
			s.streamType,
			0xE0|byte(s.pid>>8), byte(s.pid&0xFF),
			0xF0, byte(len(s.descriptors)),
		)
		payload = append(payload, s.descriptors...)
	}
	return audioTSSection(0x02, program, version, payload)
}

var (
	audioTSAC3Descriptor  = []byte{0x6A, 0x00}
	audioTSEAC3Descriptor = []byte{0x7A, 0x00}
)

// audioTSPacket builds one transport packet with a full 184-byte payload.
func audioTSPacket(pid uint16, pusi bool, cc byte, payload []byte) []byte {
	if len(payload) != 184 {
		panic(fmt.Sprintf("payload is %d bytes, a full packet carries 184", len(payload)))
	}
	p := make([]byte, TSPacketSize)
	p[0] = SyncByte
	p[1] = byte((pid >> 8) & 0x1F)
	if pusi {
		p[1] |= 0x40
	}
	p[2] = byte(pid & 0xFF)
	p[3] = 0x10 | (cc & 0x0F)
	copy(p[4:], payload)
	return p
}

// audioTSShortPacket builds a packet whose adaptation field leaves exactly
// len(payload) bytes for it.
func audioTSShortPacket(pid uint16, pusi bool, cc byte, payload []byte) []byte {
	if len(payload) > 182 {
		panic("a packet with an adaptation field carries at most 182 payload bytes")
	}
	p := make([]byte, TSPacketSize)
	for i := range p {
		p[i] = 0xFF
	}
	p[0] = SyncByte
	p[1] = byte((pid >> 8) & 0x1F)
	if pusi {
		p[1] |= 0x40
	}
	p[2] = byte(pid & 0xFF)
	p[3] = 0x30 | (cc & 0x0F)
	afLen := TSPacketSize - 5 - len(payload)
	p[4] = byte(afLen)
	p[5] = 0x00
	copy(p[TSPacketSize-len(payload):], payload)
	return p
}

// audioTSAdaptationOnly builds a packet with no payload at all.
func audioTSAdaptationOnly(pid uint16) []byte {
	p := make([]byte, TSPacketSize)
	for i := range p {
		p[i] = 0xFF
	}
	p[0] = SyncByte
	p[1] = byte((pid >> 8) & 0x1F)
	p[2] = byte(pid & 0xFF)
	p[3] = 0x20
	p[4] = 183
	p[5] = 0x00
	return p
}

// audioTSPSIPacket carries one whole section, pointer field first.
func audioTSPSIPacket(pid uint16, cc byte, section []byte) []byte {
	payload := make([]byte, 0, 184)
	payload = append(payload, 0x00)
	payload = append(payload, section...)
	return audioTSPacket(pid, true, cc, audioTSPad(payload))
}

// audioTSPad fills a payload out to the 184 bytes a packet carries, with the
// 0xFF stuffing a real multiplexer uses. Nothing trims it: it is transport
// payload, and by the time the elementary stream has been located it is part of
// what was fed.
func audioTSPad(body []byte) []byte {
	if len(body) > 184 {
		panic(fmt.Sprintf("payload is %d bytes", len(body)))
	}
	out := make([]byte, 184)
	copy(out, body)
	for i := len(body); i < 184; i++ {
		out[i] = 0xFF
	}
	return out
}

// audioTSPESHeader is the fixed nine bytes plus headerDataLength of optional
// header, as ISO/IEC 13818-1 lays them out for a stream id that carries one.
func audioTSPESHeader(streamID byte, headerDataLength byte) []byte {
	h := []byte{0x00, 0x00, 0x01, streamID, 0x00, 0x00, 0x80, 0x00, headerDataLength}
	return append(h, make([]byte, int(headerDataLength))...)
}

// audioTSAC3Frame is one 128-byte AC-3 syncframe: 48 kHz, the smallest frame
// size, bsid 8. byte6 carries acmod and lfeon.
func audioTSAC3Frame(byte6 byte) []byte {
	f := make([]byte, 128)
	f[0], f[1] = 0x0B, 0x77
	f[4] = 0x00
	f[5] = 8 << 3
	f[6] = byte6
	return f
}

// audioTSEAC3Frame is one E-AC-3 syncframe of 128 bytes: bsid 16, frmsiz
// written as (128/2)-1 words.
func audioTSEAC3Frame(acmod byte, lfe bool) []byte {
	f := make([]byte, 128)
	f[0], f[1] = 0x0B, 0x77
	f[2] = 0x00 // strmtyp 0, substreamid 0, top bits of frmsiz
	f[3] = 63   // frmsiz = 63 -> (63+1)*2 = 128 bytes
	f[4] = 0x00 // fscod 0
	f[4] |= acmod << 1
	if lfe {
		f[4] |= 0x01
	}
	f[5] = 16 << 3 // bsid 16
	return f
}

const (
	audioTSByte6Stereo   = 0x40 // acmod 2, lfeon 0
	audioTSByte6Surround = 0xEB // acmod 7, lfeon 1
)

// obs writes an observation out by hand. The fields are what A/52 says the
// frames carry, not what a parser reported.
func obs(channels int, lfe bool, acmod uint8, frames uint64) esaudio.Observation {
	return esaudio.Observation{
		Channels: channels, LFE: lfe, Acmod: acmod, HasAcmod: channels > 0, Frames: frames,
	}
}

// obsFrames is an observation of a stream that has parsed frames without yet
// proving a layout: three agreeing frames are needed before one is established.
func obsFrames(frames uint64) esaudio.Observation {
	return esaudio.Observation{Frames: frames}
}

// --- authoring -------------------------------------------------------------

type audioTSBuild struct {
	c  audioTSCase
	cc map[uint16]byte
}

func audioTSNew(name, desc string, target uint16) *audioTSBuild {
	return &audioTSBuild{
		c:  audioTSCase{name: name, desc: desc, initial: target},
		cc: map[uint16]byte{},
	}
}

// next hands out the continuity counter a PID's next payload-carrying packet
// must have. Written here rather than per case because a fixture with a wrong
// counter would be testing a transport nobody sends.
func (b *audioTSBuild) next(pid uint16) byte {
	c := b.cc[pid]
	b.cc[pid] = (c + 1) & 0x0F
	return c
}

func (b *audioTSBuild) chunk(packets ...[]byte) *audioTSBuild {
	var data []byte
	for _, p := range packets {
		data = append(data, p...)
	}
	b.c.steps = append(b.c.steps, audioTSStep{kind: audioTSStepChunk, chunk: data})
	return b
}

func (b *audioTSBuild) target(n uint16) *audioTSBuild {
	b.c.steps = append(b.c.steps, audioTSStep{kind: audioTSStepTarget, target: n})
	return b
}

func (b *audioTSBuild) feed(incarnation int, pid uint16, es []byte, o esaudio.Observation) *audioTSBuild {
	b.c.want = append(b.c.want, audioTSFeed{incarnation: incarnation, pid: pid, es: es, obs: o})
	return b
}

func (b *audioTSBuild) refFeed(incarnation int, pid uint16, es []byte, o esaudio.Observation) *audioTSBuild {
	b.c.reference = append(b.c.reference, audioTSFeed{incarnation: incarnation, pid: pid, es: es, obs: o})
	return b
}

func (b *audioTSBuild) diverges(why string) *audioTSBuild {
	b.c.divergence = why
	return b
}

func (b *audioTSBuild) stream(pid uint16, codec string, feeds int, o esaudio.Observation) *audioTSBuild {
	b.c.streams = append(b.c.streams, audioTSStream{pid: pid, codec: codec, feeds: feeds, obs: o})
	return b
}

func (b *audioTSBuild) done() audioTSCase { return b.c }

// psi is a PAT and a PMT, each in its own packet.
func (b *audioTSBuild) psi(version byte, streams ...audioTSEs) [][]byte {
	return [][]byte{
		audioTSPSIPacket(0, b.next(0), audioTSPAT(audioTSProgram)),
		audioTSPSIPacket(audioTSPMTPID, b.next(audioTSPMTPID), audioTSPMT(audioTSProgram, version, streams...)),
	}
}

// pmtOnly is a new PMT for the same programme.
func (b *audioTSBuild) pmtOnly(version byte, streams ...audioTSEs) []byte {
	return audioTSPSIPacket(audioTSPMTPID, b.next(audioTSPMTPID), audioTSPMT(audioTSProgram, version, streams...))
}

// ac3Stream is the elementary stream entry for AC-3 as DVB declares it: the
// private stream type, with the AC-3 descriptor saying what it is.
func ac3Stream(pid uint16) audioTSEs {
	return audioTSEs{streamType: 0x06, pid: pid, descriptors: audioTSAC3Descriptor}
}

func eac3Stream(pid uint16) audioTSEs {
	return audioTSEs{streamType: 0x06, pid: pid, descriptors: audioTSEAC3Descriptor}
}

// pesStart is the payload of a packet beginning a PES packet: the header, the
// elementary stream bytes, and the stuffing a multiplexer fills the rest with.
func pesStart(streamID, headerDataLength byte, es []byte) []byte {
	return audioTSPad(append(audioTSPESHeader(streamID, headerDataLength), es...))
}

// esOf is where the elementary stream begins in a payload that started a PES
// packet, per ISO/IEC 13818-1: after the six fixed bytes, the two flag bytes and
// the length byte, then the optional header that length declares.
func esOf(payload []byte, headerDataLength int) []byte {
	return payload[9+headerDataLength:]
}

func (b *audioTSBuild) refStream(pid uint16, codec string, feeds int, o esaudio.Observation) *audioTSBuild {
	b.c.referenceStream = append(b.c.referenceStream, audioTSStream{pid: pid, codec: codec, feeds: feeds, obs: o})
	return b
}

// --- the cases -------------------------------------------------------------

//nolint:maintidx // a corpus is a list; splitting it would hide the list.
func audioTSCorpusCases() []audioTSCase {
	var cases []audioTSCase

	// One PES packet, started in one transport packet and continued in the
	// next three. Each payload carries one syncframe and then the stuffing a
	// multiplexer fills the packet with; A/52 needs three agreeing frames
	// before a layout is established, so the third feed is where the channels
	// appear.
	{
		b := audioTSNew("ac3_start_then_continuations",
			"one AC-3 PES packet across four transport packets", audioTSProgram)
		psi := b.psi(0, ac3Stream(audioTSAudioA))
		b.chunk(psi...)

		start := pesStart(0xBD, 0, audioTSAC3Frame(audioTSByte6Stereo))
		c1 := audioTSPad(audioTSAC3Frame(audioTSByte6Stereo))
		c2 := audioTSPad(audioTSAC3Frame(audioTSByte6Stereo))
		c3 := audioTSPad(audioTSAC3Frame(audioTSByte6Stereo))
		b.chunk(
			audioTSPacket(audioTSAudioA, true, b.next(audioTSAudioA), start),
			audioTSPacket(audioTSAudioA, false, b.next(audioTSAudioA), c1),
			audioTSPacket(audioTSAudioA, false, b.next(audioTSAudioA), c2),
			audioTSPacket(audioTSAudioA, false, b.next(audioTSAudioA), c3),
		)
		b.feed(0, audioTSAudioA, esOf(start, 0), obsFrames(1))
		b.feed(0, audioTSAudioA, c1, obsFrames(2))
		b.feed(0, audioTSAudioA, c2, obs(2, false, 2, 3))
		b.feed(0, audioTSAudioA, c3, obs(2, false, 2, 4))
		b.stream(audioTSAudioA, esaudio.CodecAC3, 4, obs(2, false, 2, 4))
		cases = append(cases, b.done())
	}

	// The same shape in E-AC-3, whose syncframe says its own size and puts
	// acmod and lfeon in byte four rather than after a run of fields byte six
	// ends.
	{
		b := audioTSNew("eac3_start_then_continuations",
			"one E-AC-3 PES packet across four transport packets", audioTSProgram)
		b.chunk(b.psi(0, eac3Stream(audioTSAudioA))...)

		frame := func() []byte { return audioTSEAC3Frame(2, false) }
		start := pesStart(0xBD, 0, frame())
		c1, c2, c3 := audioTSPad(frame()), audioTSPad(frame()), audioTSPad(frame())
		b.chunk(
			audioTSPacket(audioTSAudioA, true, b.next(audioTSAudioA), start),
			audioTSPacket(audioTSAudioA, false, b.next(audioTSAudioA), c1),
			audioTSPacket(audioTSAudioA, false, b.next(audioTSAudioA), c2),
			audioTSPacket(audioTSAudioA, false, b.next(audioTSAudioA), c3),
		)
		b.feed(0, audioTSAudioA, esOf(start, 0), obsFrames(1))
		b.feed(0, audioTSAudioA, c1, obsFrames(2))
		b.feed(0, audioTSAudioA, c2, obs(2, false, 2, 3))
		b.feed(0, audioTSAudioA, c3, obs(2, false, 2, 4))
		b.stream(audioTSAudioA, esaudio.CodecEAC3, 4, obs(2, false, 2, 4))
		cases = append(cases, b.done())
	}

	// 0xFD is extended_stream_id. It is not an audio stream id in the
	// numbering and it carries audio in these streams, which is why the
	// reference accepts it beside 0xBD and the range.
	{
		b := audioTSNew("ac3_extended_stream_id",
			"AC-3 announced with stream id 0xFD", audioTSProgram)
		b.chunk(b.psi(0, ac3Stream(audioTSAudioA))...)
		start := pesStart(0xFD, 0, audioTSAC3Frame(audioTSByte6Surround))
		b.chunk(audioTSPacket(audioTSAudioA, true, b.next(audioTSAudioA), start))
		b.feed(0, audioTSAudioA, esOf(start, 0), obsFrames(1))
		b.stream(audioTSAudioA, esaudio.CodecAC3, 1, obsFrames(1))
		cases = append(cases, b.done())
	}

	// An optional header of ten bytes. Its length is a field, so the
	// elementary stream begins ten bytes later and nowhere else.
	{
		b := audioTSNew("optional_header_of_ten_bytes",
			"header_data_length moves where the elementary stream begins", audioTSProgram)
		b.chunk(b.psi(0, ac3Stream(audioTSAudioA))...)
		start := pesStart(0xBD, 10, audioTSAC3Frame(audioTSByte6Stereo))
		b.chunk(audioTSPacket(audioTSAudioA, true, b.next(audioTSAudioA), start))
		b.feed(0, audioTSAudioA, esOf(start, 10), obsFrames(1))
		b.stream(audioTSAudioA, esaudio.CodecAC3, 1, obsFrames(1))
		cases = append(cases, b.done())
	}

	// 9 + 175 = 184: the header ends on the last byte of the payload. There is
	// no elementary stream in this packet, and an observer given nothing has
	// been told nothing - which is a different thing from being given bytes.
	{
		b := audioTSNew("header_ends_at_the_payload_end",
			"a PES header filling its packet exactly feeds nothing", audioTSProgram)
		b.chunk(b.psi(0, ac3Stream(audioTSAudioA))...)
		start := pesStart(0xBD, 175, nil)
		c1 := audioTSPad(audioTSAC3Frame(audioTSByte6Stereo))
		b.chunk(
			audioTSPacket(audioTSAudioA, true, b.next(audioTSAudioA), start),
			audioTSPacket(audioTSAudioA, false, b.next(audioTSAudioA), c1),
		)
		b.feed(0, audioTSAudioA, c1, obsFrames(1))
		b.stream(audioTSAudioA, esaudio.CodecAC3, 1, obsFrames(1))
		cases = append(cases, b.done())
	}

	// Two PES packets back to back, each starting its own transport packet.
	// The second start is a start and not a continuation, so its header is
	// stepped over again.
	{
		b := audioTSNew("two_consecutive_pes_starts",
			"a second PES packet begins in the very next transport packet", audioTSProgram)
		b.chunk(b.psi(0, ac3Stream(audioTSAudioA))...)
		first := pesStart(0xBD, 0, audioTSAC3Frame(audioTSByte6Stereo))
		second := pesStart(0xBD, 6, audioTSAC3Frame(audioTSByte6Stereo))
		b.chunk(
			audioTSPacket(audioTSAudioA, true, b.next(audioTSAudioA), first),
			audioTSPacket(audioTSAudioA, true, b.next(audioTSAudioA), second),
		)
		b.feed(0, audioTSAudioA, esOf(first, 0), obsFrames(1))
		b.feed(0, audioTSAudioA, esOf(second, 6), obsFrames(2))
		b.stream(audioTSAudioA, esaudio.CodecAC3, 2, obsFrames(2))
		cases = append(cases, b.done())
	}

	// A syncframe whose header is cut in two by the packet boundary. The
	// observer keeps a short tail for exactly this and reads the frame from the
	// feed that completes it, so the frame is counted once and in the second
	// feed.
	{
		b := audioTSNew("a_syncframe_header_split_across_two_packets",
			"a frame header cut by a transport boundary is read from the second", audioTSProgram)
		b.chunk(b.psi(0, ac3Stream(audioTSAudioA))...)

		f1 := audioTSAC3Frame(audioTSByte6Stereo)
		f2 := audioTSAC3Frame(audioTSByte6Stereo)
		// 9 header bytes and 131 of elementary stream: one whole frame and the
		// first three bytes of the next.
		body := append(append([]byte{}, f1...), f2[:3]...)
		start := append(audioTSPESHeader(0xBD, 0), body...)
		c1 := audioTSPad(f2[3:])
		c2 := audioTSPad(audioTSAC3Frame(audioTSByte6Stereo))
		c3 := audioTSPad(audioTSAC3Frame(audioTSByte6Stereo))
		b.chunk(
			audioTSShortPacket(audioTSAudioA, true, b.next(audioTSAudioA), start),
			audioTSPacket(audioTSAudioA, false, b.next(audioTSAudioA), c1),
			audioTSPacket(audioTSAudioA, false, b.next(audioTSAudioA), c2),
			audioTSPacket(audioTSAudioA, false, b.next(audioTSAudioA), c3),
		)
		b.feed(0, audioTSAudioA, start[9:], obsFrames(1))
		b.feed(0, audioTSAudioA, c1, obsFrames(2))
		b.feed(0, audioTSAudioA, c2, obs(2, false, 2, 3))
		b.feed(0, audioTSAudioA, c3, obs(2, false, 2, 4))
		b.stream(audioTSAudioA, esaudio.CodecAC3, 4, obs(2, false, 2, 4))
		cases = append(cases, b.done())
	}

	// A scrambled packet on an observable PID. Its bytes are encrypted, so
	// they are not elementary stream and are fed to nothing; the clear packets
	// on either side of it are unaffected.
	{
		b := audioTSNew("a_scrambled_packet_is_not_elementary_stream",
			"encrypted payload reaches no observer", audioTSProgram)
		b.chunk(b.psi(0, ac3Stream(audioTSAudioA))...)
		start := pesStart(0xBD, 0, audioTSAC3Frame(audioTSByte6Stereo))
		c1 := audioTSPad(audioTSAC3Frame(audioTSByte6Stereo))
		// Built in the order it is sent, so the continuity counter runs the way
		// a real multiplexer writes it. A scrambled packet carries payload and
		// advances the counter like any other.
		p0 := audioTSPacket(audioTSAudioA, true, b.next(audioTSAudioA), start)
		p1 := audioTSPacket(audioTSAudioA, false, b.next(audioTSAudioA), audioTSPad(audioTSAC3Frame(audioTSByte6Surround)))
		p1[3] |= 0x80
		p2 := audioTSPacket(audioTSAudioA, false, b.next(audioTSAudioA), c1)
		b.chunk(p0, p1, p2)
		b.feed(0, audioTSAudioA, esOf(start, 0), obsFrames(1))
		b.feed(0, audioTSAudioA, c1, obsFrames(2))
		b.stream(audioTSAudioA, esaudio.CodecAC3, 2, obsFrames(2))
		cases = append(cases, b.done())
	}

	// Audio-looking packets on a PID the table never named. Nothing follows
	// that PID, so nothing is fed - the table decides which streams exist, not
	// the bytes.
	{
		b := audioTSNew("packets_on_a_pid_no_table_named",
			"an undeclared PID is followed by nothing", audioTSProgram)
		b.chunk(b.psi(0, ac3Stream(audioTSAudioA))...)
		start := pesStart(0xBD, 0, audioTSAC3Frame(audioTSByte6Stereo))
		b.chunk(audioTSPacket(audioTSAudioB, true, b.next(audioTSAudioB), start))
		b.stream(audioTSAudioA, esaudio.CodecAC3, 0, esaudio.Observation{})
		cases = append(cases, b.done())
	}

	// MPEG-1 layer II is audio and is not a codec whose frames this reads. The
	// track is declared, it is simply not observed: the absence is the
	// observation, not a guess at one.
	{
		b := audioTSNew("a_codec_whose_frames_are_not_read",
			"mp2 is declared audio and gets no observer", audioTSProgram)
		b.chunk(b.psi(0, audioTSEs{streamType: 0x03, pid: audioTSAudioA})...)
		start := pesStart(0xC0, 0, audioTSAC3Frame(audioTSByte6Stereo))
		b.chunk(audioTSPacket(audioTSAudioA, true, b.next(audioTSAudioA), start))
		cases = append(cases, b.done())
	}

	// A payload unit that starts with a video stream id. Where its elementary
	// stream begins was not read, so this packet feeds nothing; the packets
	// after it are still this PID's payload and are still elementary stream.
	{
		b := audioTSNew("a_payload_unit_with_a_stream_id_that_is_not_audio",
			"a video stream id on an audio PID starts nothing", audioTSProgram)
		b.chunk(b.psi(0, ac3Stream(audioTSAudioA))...)
		start := pesStart(0xE0, 0, audioTSAC3Frame(audioTSByte6Stereo))
		c1 := audioTSPad(audioTSAC3Frame(audioTSByte6Stereo))
		b.chunk(
			audioTSPacket(audioTSAudioA, true, b.next(audioTSAudioA), start),
			audioTSPacket(audioTSAudioA, false, b.next(audioTSAudioA), c1),
		)
		b.feed(0, audioTSAudioA, c1, obsFrames(1))
		b.stream(audioTSAudioA, esaudio.CodecAC3, 1, obsFrames(1))
		cases = append(cases, b.done())
	}

	// 0xFF is program_stream_directory, one of the ids that carries no optional
	// header at all. Reading byte eight of one of them as a header length
	// invents a header - a padding stream stuffed with 0xFF would declare 255
	// bytes of it - and it is not an audio id either way.
	{
		b := audioTSNew("a_payload_unit_with_a_stream_id_that_has_no_header",
			"0xFF carries no optional header and is not audio", audioTSProgram)
		b.chunk(b.psi(0, ac3Stream(audioTSAudioA))...)
		start := audioTSPad(append([]byte{0x00, 0x00, 0x01, 0xFF, 0xFF, 0xFF}, audioTSAC3Frame(audioTSByte6Stereo)...))
		c1 := audioTSPad(audioTSAC3Frame(audioTSByte6Stereo))
		b.chunk(
			audioTSPacket(audioTSAudioA, true, b.next(audioTSAudioA), start),
			audioTSPacket(audioTSAudioA, false, b.next(audioTSAudioA), c1),
		)
		b.feed(0, audioTSAudioA, c1, obsFrames(1))
		b.stream(audioTSAudioA, esaudio.CodecAC3, 1, obsFrames(1))
		cases = append(cases, b.done())
	}

	// A payload unit whose start code is not there. Nothing says a PES packet
	// begins here, so nothing is skipped and nothing is fed from it.
	{
		b := audioTSNew("a_payload_unit_that_is_not_a_pes_packet",
			"a prefix that is not 00 00 01 starts nothing", audioTSProgram)
		b.chunk(b.psi(0, ac3Stream(audioTSAudioA))...)
		start := audioTSPad(append([]byte{0x00, 0x00, 0x02, 0xBD, 0x00, 0x00, 0x80, 0x00, 0x00}, audioTSAC3Frame(audioTSByte6Stereo)...))
		c1 := audioTSPad(audioTSAC3Frame(audioTSByte6Stereo))
		b.chunk(
			audioTSPacket(audioTSAudioA, true, b.next(audioTSAudioA), start),
			audioTSPacket(audioTSAudioA, false, b.next(audioTSAudioA), c1),
		)
		b.feed(0, audioTSAudioA, c1, obsFrames(1))
		b.stream(audioTSAudioA, esaudio.CodecAC3, 1, obsFrames(1))
		cases = append(cases, b.done())
	}

	// A payload unit too short to hold the fixed header. An adaptation field
	// can leave a payload of any length, and five bytes cannot say where the
	// elementary stream begins.
	{
		b := audioTSNew("a_payload_unit_shorter_than_the_fixed_header",
			"five bytes cannot say where the elementary stream begins", audioTSProgram)
		b.chunk(b.psi(0, ac3Stream(audioTSAudioA))...)
		c1 := audioTSPad(audioTSAC3Frame(audioTSByte6Stereo))
		b.chunk(
			audioTSShortPacket(audioTSAudioA, true, b.next(audioTSAudioA), []byte{0x00, 0x00, 0x01, 0xBD, 0x00}),
			audioTSPacket(audioTSAudioA, false, b.next(audioTSAudioA), c1),
		)
		b.feed(0, audioTSAudioA, c1, obsFrames(1))
		b.stream(audioTSAudioA, esaudio.CodecAC3, 1, obsFrames(1))
		cases = append(cases, b.done())
	}

	// A packet with an adaptation field and no payload. There is nothing to
	// feed and nothing to read, and the stream is where it was.
	{
		b := audioTSNew("a_packet_with_no_payload",
			"an adaptation-only packet carries nothing to feed", audioTSProgram)
		b.chunk(b.psi(0, ac3Stream(audioTSAudioA))...)
		start := pesStart(0xBD, 0, audioTSAC3Frame(audioTSByte6Stereo))
		c1 := audioTSPad(audioTSAC3Frame(audioTSByte6Stereo))
		b.chunk(
			audioTSPacket(audioTSAudioA, true, b.next(audioTSAudioA), start),
			audioTSAdaptationOnly(audioTSAudioA),
			audioTSPacket(audioTSAudioA, false, b.next(audioTSAudioA), c1),
		)
		b.feed(0, audioTSAudioA, esOf(start, 0), obsFrames(1))
		b.feed(0, audioTSAudioA, c1, obsFrames(2))
		b.stream(audioTSAudioA, esaudio.CodecAC3, 2, obsFrames(2))
		cases = append(cases, b.done())
	}

	// The same PID, declared again by a new version of the table. It may be a
	// different elementary stream, and nothing it said before may be carried
	// across: an observation that survived the table would describe audio that
	// is not there.
	{
		b := audioTSNew("the_same_pid_under_a_new_pmt_version",
			"a new table version ends the stream on the PID it reuses", audioTSProgram)
		b.chunk(b.psi(0, ac3Stream(audioTSAudioA))...)

		s0 := pesStart(0xBD, 0, audioTSAC3Frame(audioTSByte6Stereo))
		a1 := audioTSPad(audioTSAC3Frame(audioTSByte6Stereo))
		a2 := audioTSPad(audioTSAC3Frame(audioTSByte6Stereo))
		a3 := audioTSPad(audioTSAC3Frame(audioTSByte6Stereo))
		b.chunk(
			audioTSPacket(audioTSAudioA, true, b.next(audioTSAudioA), s0),
			audioTSPacket(audioTSAudioA, false, b.next(audioTSAudioA), a1),
			audioTSPacket(audioTSAudioA, false, b.next(audioTSAudioA), a2),
			audioTSPacket(audioTSAudioA, false, b.next(audioTSAudioA), a3),
		)
		b.feed(0, audioTSAudioA, esOf(s0, 0), obsFrames(1))
		b.feed(0, audioTSAudioA, a1, obsFrames(2))
		b.feed(0, audioTSAudioA, a2, obs(2, false, 2, 3))
		b.feed(0, audioTSAudioA, a3, obs(2, false, 2, 4))

		b.chunk(b.pmtOnly(1, ac3Stream(audioTSAudioA)))

		s1 := pesStart(0xBD, 0, audioTSAC3Frame(audioTSByte6Surround))
		c1 := audioTSPad(audioTSAC3Frame(audioTSByte6Surround))
		c2 := audioTSPad(audioTSAC3Frame(audioTSByte6Surround))
		b.chunk(
			audioTSPacket(audioTSAudioA, true, b.next(audioTSAudioA), s1),
			audioTSPacket(audioTSAudioA, false, b.next(audioTSAudioA), c1),
			audioTSPacket(audioTSAudioA, false, b.next(audioTSAudioA), c2),
		)
		// A new incarnation: the frame count starts at one again, and three
		// agreeing frames are needed before the new layout is established.
		b.feed(1, audioTSAudioA, esOf(s1, 0), obsFrames(1))
		b.feed(1, audioTSAudioA, c1, obsFrames(2))
		b.feed(1, audioTSAudioA, c2, obs(6, true, 7, 3))
		b.stream(audioTSAudioA, esaudio.CodecAC3, 3, obs(6, true, 7, 3))
		cases = append(cases, b.done())
	}

	// The table moves the audio to another PID. The old one is not followed any
	// more, and its packets are not fed even though they look exactly as they
	// did a moment ago.
	{
		b := audioTSNew("the_table_moves_the_audio_to_another_pid",
			"a PID that is no longer declared is no longer followed", audioTSProgram)
		b.chunk(b.psi(0, ac3Stream(audioTSAudioA))...)
		s0 := pesStart(0xBD, 0, audioTSAC3Frame(audioTSByte6Stereo))
		b.chunk(audioTSPacket(audioTSAudioA, true, b.next(audioTSAudioA), s0))
		b.feed(0, audioTSAudioA, esOf(s0, 0), obsFrames(1))

		b.chunk(b.pmtOnly(1, ac3Stream(audioTSAudioB)))

		s1 := pesStart(0xBD, 0, audioTSAC3Frame(audioTSByte6Surround))
		old := pesStart(0xBD, 0, audioTSAC3Frame(audioTSByte6Stereo))
		b.chunk(
			audioTSPacket(audioTSAudioB, true, b.next(audioTSAudioB), s1),
			audioTSPacket(audioTSAudioA, true, b.next(audioTSAudioA), old),
		)
		b.feed(1, audioTSAudioB, esOf(s1, 0), obsFrames(1))
		b.stream(audioTSAudioB, esaudio.CodecAC3, 1, obsFrames(1))
		cases = append(cases, b.done())
	}

	// Audio taken out of the table and put back. The incarnation that had no
	// audio produced no feeds, so the two runs of feeds are consecutive
	// incarnations - and the second still begins from nothing.
	{
		b := audioTSNew("audio_removed_and_declared_again",
			"a programme without audio, and then with it again", audioTSProgram)
		b.chunk(b.psi(0, ac3Stream(audioTSAudioA))...)
		s0 := pesStart(0xBD, 0, audioTSAC3Frame(audioTSByte6Stereo))
		b.chunk(audioTSPacket(audioTSAudioA, true, b.next(audioTSAudioA), s0))
		b.feed(0, audioTSAudioA, esOf(s0, 0), obsFrames(1))

		// A table with video and no audio at all.
		b.chunk(b.pmtOnly(1, audioTSEs{streamType: 0x1B, pid: 0x0200}))
		orphan := pesStart(0xBD, 0, audioTSAC3Frame(audioTSByte6Stereo))
		b.chunk(audioTSPacket(audioTSAudioA, true, b.next(audioTSAudioA), orphan))

		b.chunk(b.pmtOnly(2, ac3Stream(audioTSAudioA)))
		s2 := pesStart(0xBD, 0, audioTSAC3Frame(audioTSByte6Surround))
		b.chunk(audioTSPacket(audioTSAudioA, true, b.next(audioTSAudioA), s2))
		b.feed(1, audioTSAudioA, esOf(s2, 0), obsFrames(1))
		b.stream(audioTSAudioA, esaudio.CodecAC3, 1, obsFrames(1))
		cases = append(cases, b.done())
	}

	// The identity changes half way through one call. The feeds on either side
	// of it belong to different elementary streams that share a number, and a
	// stage that noticed the change only at the end of the chunk would have fed
	// the second half to the observer of the first.
	{
		b := audioTSNew("an_identity_change_inside_one_chunk",
			"a PMT completing mid-chunk splits the chunk's own audio", audioTSProgram)
		b.chunk(b.psi(0, ac3Stream(audioTSAudioA))...)

		s0 := pesStart(0xBD, 0, audioTSAC3Frame(audioTSByte6Stereo))
		a1 := audioTSPad(audioTSAC3Frame(audioTSByte6Stereo))
		p0 := audioTSPacket(audioTSAudioA, true, b.next(audioTSAudioA), s0)
		p1 := audioTSPacket(audioTSAudioA, false, b.next(audioTSAudioA), a1)
		table := b.pmtOnly(1, ac3Stream(audioTSAudioA))
		s1 := pesStart(0xBD, 0, audioTSAC3Frame(audioTSByte6Surround))
		c1 := audioTSPad(audioTSAC3Frame(audioTSByte6Surround))
		p2 := audioTSPacket(audioTSAudioA, true, b.next(audioTSAudioA), s1)
		p3 := audioTSPacket(audioTSAudioA, false, b.next(audioTSAudioA), c1)
		b.chunk(p0, p1, table, p2, p3)

		b.feed(0, audioTSAudioA, esOf(s0, 0), obsFrames(1))
		b.feed(0, audioTSAudioA, a1, obsFrames(2))
		b.feed(1, audioTSAudioA, esOf(s1, 0), obsFrames(1))
		b.feed(1, audioTSAudioA, c1, obsFrames(2))
		b.stream(audioTSAudioA, esaudio.CodecAC3, 2, obsFrames(2))
		cases = append(cases, b.done())
	}

	// Two observable streams at once. They interleave in the transport and do
	// not interleave in the feeds: each is its own sequence, and a stage that
	// fed one observer from both would be describing a programme nobody sent.
	{
		b := audioTSNew("two_observable_streams_at_once",
			"two AC-3 tracks, interleaved packet by packet", audioTSProgram)
		b.chunk(b.psi(0, ac3Stream(audioTSAudioA), ac3Stream(audioTSAudioB))...)

		sa := pesStart(0xBD, 0, audioTSAC3Frame(audioTSByte6Stereo))
		sb := pesStart(0xBD, 0, audioTSAC3Frame(audioTSByte6Surround))
		ca := audioTSPad(audioTSAC3Frame(audioTSByte6Stereo))
		cb := audioTSPad(audioTSAC3Frame(audioTSByte6Surround))
		b.chunk(
			audioTSPacket(audioTSAudioA, true, b.next(audioTSAudioA), sa),
			audioTSPacket(audioTSAudioB, true, b.next(audioTSAudioB), sb),
			audioTSPacket(audioTSAudioA, false, b.next(audioTSAudioA), ca),
			audioTSPacket(audioTSAudioB, false, b.next(audioTSAudioB), cb),
		)
		b.feed(0, audioTSAudioA, esOf(sa, 0), obsFrames(1))
		b.feed(0, audioTSAudioA, ca, obsFrames(2))
		b.feed(0, audioTSAudioB, esOf(sb, 0), obsFrames(1))
		b.feed(0, audioTSAudioB, cb, obsFrames(2))
		b.stream(audioTSAudioA, esaudio.CodecAC3, 2, obsFrames(2))
		b.stream(audioTSAudioB, esaudio.CodecAC3, 2, obsFrames(2))
		cases = append(cases, b.done())
	}

	// Selecting a programme the table does not carry. What the PIDs carried
	// belonged to the programme that was selected, and it is not selected any
	// more.
	{
		b := audioTSNew("selecting_another_programme",
			"a target change ends every stream being followed", audioTSProgram)
		b.chunk(b.psi(0, ac3Stream(audioTSAudioA))...)
		s0 := pesStart(0xBD, 0, audioTSAC3Frame(audioTSByte6Stereo))
		b.chunk(audioTSPacket(audioTSAudioA, true, b.next(audioTSAudioA), s0))
		b.feed(0, audioTSAudioA, esOf(s0, 0), obsFrames(1))

		b.target(audioTSOther)
		orphan := pesStart(0xBD, 0, audioTSAC3Frame(audioTSByte6Stereo))
		b.chunk(audioTSPacket(audioTSAudioA, true, b.next(audioTSAudioA), orphan))
		cases = append(cases, b.done())
	}

	// A header that reached past its packet, and then a payload unit that is
	// not the rest of it.
	//
	// The transport started something else, so the header's remainder never
	// arrives and the count of it is not a count of anything any more. Carrying
	// it forward would take those bytes off the front of a later packet that
	// owes them to nobody. The reference keeps no such state and feeds the
	// continuation whole, which is also the right answer - this is a case where
	// having state is the thing that could go wrong.
	{
		b := audioTSNew("a_header_remainder_abandoned_by_a_new_payload_unit",
			"a payload unit start ends a header that was still being read", audioTSProgram)
		b.chunk(b.psi(0, ac3Stream(audioTSAudioA))...)

		full := audioTSPESHeader(0xBD, 200)
		start := full[:184]
		// A payload unit of its own, on an id this does not read.
		other := pesStart(0xE0, 0, audioTSAC3Frame(audioTSByte6Stereo))
		c1 := audioTSPad(audioTSAC3Frame(audioTSByte6Stereo))
		b.chunk(
			audioTSPacket(audioTSAudioA, true, b.next(audioTSAudioA), start),
			audioTSPacket(audioTSAudioA, true, b.next(audioTSAudioA), other),
			audioTSPacket(audioTSAudioA, false, b.next(audioTSAudioA), c1),
		)
		b.feed(0, audioTSAudioA, c1, obsFrames(1))
		b.stream(audioTSAudioA, esaudio.CodecAC3, 1, obsFrames(1))
		cases = append(cases, b.done())
	}

	// A table that declares an AC-3 track on the video PID. The reference asks
	// which PID a packet is on in a fixed order - the table, then the video,
	// then the audio - so the video wins and the track's observer is never fed.
	// A PES packet carrying perfectly good AC-3 on that PID is still not this
	// track's audio: the table has already said the PID is video.
	{
		b := audioTSNew("an_audio_track_declared_on_the_video_pid",
			"a PID the table names for video is not fed as audio", audioTSProgram)
		const shared = 0x0200
		b.chunk(b.psi(0,
			audioTSEs{streamType: 0x1B, pid: shared},
			ac3Stream(shared),
		)...)
		start := pesStart(0xBD, 0, audioTSAC3Frame(audioTSByte6Stereo))
		c1 := audioTSPad(audioTSAC3Frame(audioTSByte6Stereo))
		c2 := audioTSPad(audioTSAC3Frame(audioTSByte6Stereo))
		b.chunk(
			audioTSPacket(shared, true, b.next(shared), start),
			audioTSPacket(shared, false, b.next(shared), c1),
			audioTSPacket(shared, false, b.next(shared), c2),
		)
		b.stream(shared, esaudio.CodecAC3, 0, esaudio.Observation{})
		cases = append(cases, b.done())
	}

	// A table that declares an AC-3 track on its own PID. The table's packets
	// keep arriving on that PID, and the second copy here is split across two
	// packets the way a longer section would be, so one of them is a
	// continuation - which is exactly the payload a follower would take whole
	// if nothing asked first whether the PID carried the table.
	{
		b := audioTSNew("an_audio_track_declared_on_the_pmt_pid",
			"the table's own PID is not fed as audio", audioTSProgram)
		pmt := audioTSPMT(audioTSProgram, 0, ac3Stream(audioTSPMTPID))
		b.chunk(
			audioTSPSIPacket(0, b.next(0), audioTSPAT(audioTSProgram)),
			audioTSPSIPacket(audioTSPMTPID, b.next(audioTSPMTPID), pmt),
		)
		// The same table again, its first ten bytes in one packet and the rest
		// in the next.
		head := append([]byte{0x00}, pmt[:10]...)
		b.chunk(
			audioTSShortPacket(audioTSPMTPID, true, b.next(audioTSPMTPID), head),
			audioTSPacket(audioTSPMTPID, false, b.next(audioTSPMTPID), audioTSPad(pmt[10:])),
		)
		b.stream(audioTSPMTPID, esaudio.CodecAC3, 0, esaudio.Observation{})
		cases = append(cases, b.done())
	}

	// The one case where the reference and the corpus disagree on purpose.
	//
	// A PES header whose optional part reaches past the packet that started it.
	// The reference has no state for a header in progress: it feeds nothing for
	// the packet that began one, and then feeds the next continuation whole -
	// so the rest of the header arrives at the observer as elementary stream.
	//
	// Those bytes are not elementary stream, and what they can do to an
	// observation is not theoretical. Here the header's last 25 bytes happen to
	// begin with something an AC-3 parser can read: a syncframe declaring 5.1
	// and 128 bytes of length. Fed them, the reference counts a frame the
	// stream does not carry and then steps 128 bytes forward - over the real
	// syncframe that followed. The programme is stereo; the reference sees one
	// surround frame, loses a stereo one, and needs a fourth frame to establish
	// what three should have.
	{
		b := audioTSNew("a_pes_header_reaching_past_its_packet",
			"the header remainder is not elementary stream", audioTSProgram)
		b.diverges("the reference feeds the remainder of a PES header as elementary stream")
		b.chunk(b.psi(0, ac3Stream(audioTSAudioA))...)

		// 9 + 200 = 209 bytes of header for a payload that carries 184: 25 of it
		// belong to the packets that follow.
		full := audioTSPESHeader(0xBD, 200)
		start := full[:184]
		tail := make([]byte, 25)
		copy(tail, audioTSAC3Frame(audioTSByte6Surround)[:7])

		p1 := audioTSPad(append(append([]byte{}, tail...), audioTSAC3Frame(audioTSByte6Stereo)...))
		p2 := audioTSPad(audioTSAC3Frame(audioTSByte6Stereo))
		p3 := audioTSPad(audioTSAC3Frame(audioTSByte6Stereo))
		b.chunk(
			audioTSPacket(audioTSAudioA, true, b.next(audioTSAudioA), start),
			audioTSPacket(audioTSAudioA, false, b.next(audioTSAudioA), p1),
			audioTSPacket(audioTSAudioA, false, b.next(audioTSAudioA), p2),
			audioTSPacket(audioTSAudioA, false, b.next(audioTSAudioA), p3),
		)

		// Authored: the elementary stream begins where the header ends.
		b.feed(0, audioTSAudioA, p1[25:], obsFrames(1))
		b.feed(0, audioTSAudioA, p2, obsFrames(2))
		b.feed(0, audioTSAudioA, p3, obs(2, false, 2, 3))
		b.stream(audioTSAudioA, esaudio.CodecAC3, 3, obs(2, false, 2, 3))

		// The reference: the whole continuation, header bytes and all.
		b.refFeed(0, audioTSAudioA, p1, obsFrames(1))
		b.refFeed(0, audioTSAudioA, p2, obsFrames(2))
		b.refFeed(0, audioTSAudioA, p3, obsFrames(3))
		b.refStream(audioTSAudioA, esaudio.CodecAC3, 3, obsFrames(3))
		cases = append(cases, b.done())
	}

	return cases
}

// --- running a case against the reference ----------------------------------

// audioTSRun is what one case did: the feeds in the order they happened, with
// the epochs normalised away, and the streams still being followed at the end.
type audioTSRun struct {
	feeds   []audioTSFeed
	streams []audioTSStream
}

// runAudioTSCase drives the reference core through a case and reads back what
// its own observers were fed.
//
// The feeds come from the AudioShadow seam and from nowhere else. Deriving them
// from the packets in test code would be writing a second parser and comparing
// it with itself; the seam is the reference saying what it fed, which is the
// only thing worth comparing against.
//
// rechunk may cut a case's chunks differently. Where an Ingest call begins and
// ends is a property of the caller, not of the transport, and the feeds must
// not know about it.
func runAudioTSCase(t *testing.T, c audioTSCase, rechunk func([]byte) [][]byte) audioTSRun {
	t.Helper()
	ctx := t.Context()

	shadow := newReplayShadow()
	core := NewGoCore(c.initial)
	core.SetAudioShadow(shadow)
	defer core.CloseAudioShadow()

	var facts Facts
	offset := int64(0)
	for i, step := range c.steps {
		switch step.kind {
		case audioTSStepChunk:
			parts := [][]byte{step.chunk}
			if rechunk != nil {
				parts = rechunk(step.chunk)
			}
			for _, part := range parts {
				res, err := core.Ingest(ctx, offset, part)
				if err != nil {
					t.Fatalf("case %s step %d: ingest: %v", c.name, i, err)
				}
				offset += int64(len(part))
				facts = res.Facts
				// The comparison is asynchronous and its queue is four chunks
				// deep. A harness that hands over a hundred one-packet calls
				// faster than the worker can take them fills it and retires the
				// shadow - and a retired shadow is no trace at all. So the next
				// call waits for this one, which is pacing the harness rather
				// than changing what the core does with the bytes.
				audioTSDrain(t, core, c.name)
			}
		case audioTSStepTarget:
			res, err := core.SetTargetProgram(ctx, step.target)
			if err != nil {
				t.Fatalf("case %s step %d: set target: %v", c.name, i, err)
			}
			facts = res.Facts
		}
	}

	// Every batch offered has to have been compared. A queue that filled up
	// retires the shadow, and a retired shadow is no result at all - not an
	// empty trace, and never a pass.
	report := awaitShadow(t, core, "every batch to be compared", func(r AudioShadowReport) bool {
		return r.Compared == r.Batches
	})
	if report.Disabled {
		t.Fatalf("case %s: the shadow was retired, so the trace is incomplete: %+v", c.name, report)
	}
	if report.Errors != 0 {
		t.Fatalf("case %s: the comparison stopped: %+v", c.name, report)
	}
	// The shadow replays the captured feeds through an observer of its own, and
	// the runner holds its answer against the core's. Zero mismatches is what
	// makes the trace below the core's own observation rather than a
	// re-derivation that happens to look like one.
	if report.Mismatches != 0 {
		t.Fatalf("case %s: the replay disagreed with the core: %+v", c.name, report)
	}

	epoch := core.shadowEpoch
	return audioTSRun{
		feeds:   audioTSCanonical(audioTSFeedsOf(shadow)),
		streams: audioTSStreamsOf(shadow, facts, epoch),
	}
}

// audioTSFeedsOf flattens the captured batches into one ordered sequence per
// stream incarnation, and states the observation after every feed.
//
// The epochs are normalised: the first incarnation to produce a feed is 0, the
// next 1. What is being compared is where the stream was split, not what either
// implementation called the pieces.
// audioTSCanonical puts the feeds into the one order that survives rechunking:
// streams in the order their first feed appeared, and each stream's feeds in
// the order they were given.
//
// A global order does not survive it. The reference batches a chunk's feeds per
// stream, so two streams interleaved packet by packet come out as one run each
// in a single call and as an alternation in one call per packet - the same
// feeds to the same observers either way. What must not change is each
// stream's own sequence, and that is what this compares.
func audioTSCanonical(feeds []audioTSFeed) []audioTSFeed {
	type key struct {
		inc int
		pid uint16
	}
	order := make([]key, 0, 4)
	byStream := map[key][]audioTSFeed{}
	for _, f := range feeds {
		k := key{f.incarnation, f.pid}
		if _, seen := byStream[k]; !seen {
			order = append(order, k)
		}
		byStream[k] = append(byStream[k], f)
	}
	out := make([]audioTSFeed, 0, len(feeds))
	for _, k := range order {
		out = append(out, byStream[k]...)
	}
	return out
}

// audioTSDrain waits for every chunk offered so far to have been compared.
func audioTSDrain(t *testing.T, core *GoCore, name string) {
	t.Helper()
	report := awaitShadow(t, core, "the shadow to catch up", func(r AudioShadowReport) bool {
		return r.Compared == r.Batches || r.Disabled
	})
	if report.Disabled {
		t.Fatalf("case %s: the shadow was retired mid-run: %+v", name, report)
	}
}

func audioTSFeedsOf(shadow *replayShadow) []audioTSFeed {
	shadow.mu.Lock()
	defer shadow.mu.Unlock()

	index := map[uint64]int{}
	observers := map[[2]uint64]*esaudio.Observer{}
	var out []audioTSFeed
	for _, batch := range shadow.seen {
		inc, ok := index[batch.Epoch]
		if !ok {
			inc = len(index)
			index[batch.Epoch] = inc
		}
		key := [2]uint64{uint64(batch.PID), batch.Epoch}
		o := observers[key]
		if o == nil {
			o = esaudio.NewObserver()
			observers[key] = o
		}
		for _, f := range batch.Feeds {
			o.Feed(f)
			out = append(out, audioTSFeed{
				incarnation: inc,
				pid:         batch.PID,
				es:          append([]byte(nil), f...),
				obs:         o.Current(),
			})
		}
	}
	return out
}

// audioTSStreamsOf is the observable audio still being followed when the case
// ended, with the observation the core itself holds.
func audioTSStreamsOf(shadow *replayShadow, facts Facts, epoch uint64) []audioTSStream {
	shadow.mu.Lock()
	counts := map[uint16]int{}
	for _, batch := range shadow.seen {
		if batch.Epoch == epoch {
			counts[batch.PID] += len(batch.Feeds)
		}
	}
	shadow.mu.Unlock()

	var out []audioTSStream
	for _, tr := range facts.AudioTracks {
		if !observableAudioCodec(tr.Codec) {
			continue
		}
		out = append(out, audioTSStream{
			pid:   tr.PID,
			codec: tr.Codec,
			feeds: counts[tr.PID],
			obs:   tr.Observed,
		})
	}
	return out
}

// --- rendering -------------------------------------------------------------

func audioTSObsLine(o esaudio.Observation) string {
	return fmt.Sprintf("channels=%d lfe=%d acmod=%d hasAcmod=%d dependent=%d frames=%d",
		o.Channels, b2i(o.LFE), o.Acmod, b2i(o.HasAcmod), b2i(o.DependentSubstream), o.Frames)
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func audioTSFeedLine(kind string, f audioTSFeed) string {
	return fmt.Sprintf("  %s inc=%d pid=%04x es=%s %s",
		kind, f.incarnation, f.pid, hex.EncodeToString(f.es), audioTSObsLine(f.obs))
}

func audioTSStreamLine(kind string, s audioTSStream) string {
	return fmt.Sprintf("  %s pid=%04x codec=%s feeds=%d %s",
		kind, s.pid, s.codec, s.feeds, audioTSObsLine(s.obs))
}

func renderAudioTSCorpus(cases []audioTSCase) string {
	var b strings.Builder
	b.WriteString("# xg2g raw transport to audio elementary stream corpus, format version 1\n")
	b.WriteString("#\n")
	b.WriteString("# Generated. To change it, edit audioTSCorpusCases() in\n")
	b.WriteString("# backend/internal/stream/ingest/mediafacts/audiots_corpus_test.go and run\n")
	b.WriteString("#   go test ./internal/stream/ingest/mediafacts/ -run TestAudioTSCorpus -update-audio-ts-corpus\n")
	b.WriteString("#\n")
	b.WriteString("# A case is raw MPEG-TS and the elementary stream feeds an audio observer\n")
	b.WriteString("# must be given for it:\n")
	b.WriteString("#\n")
	b.WriteString("#   program <n>     the programme the core is constructed to follow\n")
	b.WriteString("#   chunk <hex>     one ingest of these bytes, at the offset the previous ones reached\n")
	b.WriteString("#   target <n>      one change of the programme being followed\n")
	b.WriteString("#   feed inc= pid= es= <observation>\n")
	b.WriteString("#                   one feed, in the order it happened, with the observation\n")
	b.WriteString("#                   that followed it. inc counts stream incarnations in the\n")
	b.WriteString("#                   order their first feed appears - the same PID before and\n")
	b.WriteString("#                   after a programme identity change is two streams.\n")
	b.WriteString("#   stream pid= codec= feeds= <observation>\n")
	b.WriteString("#                   one stream still being followed when the case ends\n")
	b.WriteString("#\n")
	b.WriteString("# Feed boundaries are part of the case. An observer carries a partial frame\n")
	b.WriteString("# header across a call and skips payload that ran past the end of one, so the\n")
	b.WriteString("# same bytes joined differently are a different input.\n")
	b.WriteString("#\n")
	b.WriteString("# Where an ingest call begins and ends is not. A case cut into other chunks\n")
	b.WriteString("# has to produce the same feeds, which is a test of its own rather than a\n")
	b.WriteString("# line in this file.\n")
	b.WriteString("#\n")
	b.WriteString("# A case carrying `diverges` is one where the reference does something else,\n")
	b.WriteString("# and its `ref-feed` and `ref-stream` lines are what the reference does. The\n")
	b.WriteString("# `feed` lines stay the authored answer: they are what the syntax says, and\n")
	b.WriteString("# the difference is recorded rather than adopted.\n")
	b.WriteString("version 1\n")

	for _, c := range cases {
		b.WriteString("\ncase " + c.name + "\n")
		b.WriteString("  desc " + c.desc + "\n")
		b.WriteString(fmt.Sprintf("  program %d\n", c.initial))
		if c.divergence != "" {
			b.WriteString("  diverges " + c.divergence + "\n")
		}
		for _, s := range c.steps {
			switch s.kind {
			case audioTSStepChunk:
				b.WriteString("  chunk " + hex.EncodeToString(s.chunk) + "\n")
			case audioTSStepTarget:
				b.WriteString(fmt.Sprintf("  target %d\n", s.target))
			}
		}
		for _, f := range audioTSCanonical(c.want) {
			b.WriteString(audioTSFeedLine("feed", f) + "\n")
		}
		for _, s := range c.streams {
			b.WriteString(audioTSStreamLine("stream", s) + "\n")
		}
		for _, f := range audioTSCanonical(c.reference) {
			b.WriteString(audioTSFeedLine("ref-feed", f) + "\n")
		}
		for _, s := range c.referenceStream {
			b.WriteString(audioTSStreamLine("ref-stream", s) + "\n")
		}
		b.WriteString("end\n")
	}
	return b.String()
}

// --- the tests -------------------------------------------------------------

func audioTSCompare(t *testing.T, name string, got, want []audioTSFeed, gotStreams, wantStreams []audioTSStream) {
	t.Helper()
	got, want = audioTSCanonical(got), audioTSCanonical(want)
	var bad []string
	if len(got) != len(want) {
		bad = append(bad, fmt.Sprintf("  feeds got %d, want %d", len(got), len(want)))
	}
	for i := 0; i < len(got) && i < len(want); i++ {
		g, w := audioTSFeedLine("feed", got[i]), audioTSFeedLine("feed", want[i])
		if g != w {
			bad = append(bad, fmt.Sprintf("  feed %d\n    got  %s\n    want %s", i, g, w))
		}
	}
	for i := len(want); i < len(got); i++ {
		bad = append(bad, "  unexpected "+audioTSFeedLine("feed", got[i]))
	}
	for i := len(got); i < len(want); i++ {
		bad = append(bad, "  missing    "+audioTSFeedLine("feed", want[i]))
	}
	if len(gotStreams) != len(wantStreams) {
		bad = append(bad, fmt.Sprintf("  streams got %d, want %d", len(gotStreams), len(wantStreams)))
	}
	for i := 0; i < len(gotStreams) && i < len(wantStreams); i++ {
		g, w := audioTSStreamLine("stream", gotStreams[i]), audioTSStreamLine("stream", wantStreams[i])
		if g != w {
			bad = append(bad, fmt.Sprintf("  stream %d\n    got  %s\n    want %s", i, g, w))
		}
	}
	if len(bad) > 0 {
		t.Errorf("case %s:\n%s", name, strings.Join(bad, "\n"))
	}
}

// The reference answers the corpus.
//
// For every case but one this is "the authored expectation is what the Go core
// does". For the case marked as diverging it is "the reference does the other
// thing, exactly as recorded" - which is a test too: an unnoticed change to that
// behaviour would fail here rather than quietly becoming the new reference.
func TestAudioTSCorpus_TheGoCoreMeetsTheAuthoredExpectations(t *testing.T) {
	for _, c := range audioTSCorpusCases() {
		t.Run(c.name, func(t *testing.T) {
			run := runAudioTSCase(t, c, nil)
			want, wantStreams := c.want, c.streams
			if c.divergence != "" {
				want, wantStreams = c.reference, c.referenceStream
			}
			audioTSCompare(t, c.name, run.feeds, want, run.streams, wantStreams)
		})
	}
}

// Where an ingest call was cut is the caller's business and no part of the
// answer. The same transport handed over in other pieces has to produce the
// same feeds, in the same order, with the same boundaries - because the
// boundaries that matter are the packets', not the calls'.
func TestAudioTSCorpus_WhereAChunkWasCutChangesNothing(t *testing.T) {
	chunkings := []struct {
		name string
		cut  func([]byte) [][]byte
	}{
		{"one packet per call", func(b []byte) [][]byte { return audioTSCut(b, TSPacketSize) }},
		{"two packets per call", func(b []byte) [][]byte { return audioTSCut(b, 2*TSPacketSize) }},
		{"three packets per call", func(b []byte) [][]byte { return audioTSCut(b, 3*TSPacketSize) }},
		{"64 KiB of packets per call", func(b []byte) [][]byte { return audioTSCut(b, 348*TSPacketSize) }},
	}
	for _, c := range audioTSCorpusCases() {
		base := runAudioTSCase(t, c, nil)
		for _, ch := range chunkings {
			t.Run(c.name+"/"+ch.name, func(t *testing.T) {
				got := runAudioTSCase(t, c, ch.cut)
				audioTSCompare(t, c.name, got.feeds, base.feeds, got.streams, base.streams)
			})
		}
	}
}

func audioTSCut(data []byte, size int) [][]byte {
	var out [][]byte
	for len(data) > size {
		out = append(out, data[:size])
		data = data[size:]
	}
	return append(out, data)
}

// The checked-in file is the cases, rendered. Both implementations read it, so
// it is the artefact the agreement is a property of.
func TestAudioTSCorpus_TheCheckedInFileMatchesTheCases(t *testing.T) {
	want := renderAudioTSCorpus(audioTSCorpusCases())
	if *updateAudioTSCorpus {
		if err := os.MkdirAll("../../../../../testdata/audio-ts-corpus", 0o750); err != nil {
			t.Fatalf("create corpus directory: %v", err)
		}
		if err := os.WriteFile(audioTSCorpusPath, []byte(want), 0o644); err != nil { // #nosec G306 -- a checked-in fixture
			t.Fatalf("write corpus: %v", err)
		}
		t.Log("corpus rewritten")
		return
	}
	got, err := os.ReadFile(audioTSCorpusPath)
	if err != nil {
		t.Fatalf("read corpus: %v\nrun: go test ./internal/stream/ingest/mediafacts/ -run TestAudioTSCorpus -update-audio-ts-corpus", err)
	}
	if string(got) != want {
		t.Fatalf("the checked-in corpus is not what the cases render; run:\n" +
			"  go test ./internal/stream/ingest/mediafacts/ -run TestAudioTSCorpus -update-audio-ts-corpus")
	}
}

// Exactly one case may diverge from the reference, and it is the one that has
// been reviewed. A second one appearing is a finding, not a corpus update.
func TestAudioTSCorpus_OnlyTheReviewedDivergenceExists(t *testing.T) {
	var diverging []string
	for _, c := range audioTSCorpusCases() {
		if c.divergence != "" {
			diverging = append(diverging, c.name)
		}
		if (c.reference != nil || c.referenceStream != nil) && c.divergence == "" {
			t.Errorf("case %s carries a reference trace without saying it diverges", c.name)
		}
	}
	want := []string{"a_pes_header_reaching_past_its_packet"}
	if strings.Join(diverging, ",") != strings.Join(want, ",") {
		t.Fatalf("diverging cases are %v, want %v", diverging, want)
	}
}
