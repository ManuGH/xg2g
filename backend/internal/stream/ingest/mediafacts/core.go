// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package mediafacts

import (
	"bytes"
	"context"
	"errors"
	"time"

	"github.com/ManuGH/xg2g/internal/stream/ingest/esaudio"
)

// DefaultIngestDeadline bounds how long one chunk may be interpreted.
//
// It is a stall detector, not a budget meant to be spent. The in-process core
// finishes a 64 KiB chunk in microseconds and a core on a local socket should
// answer in milliseconds, so anything approaching this value means the core has
// stopped answering rather than that it is working hard.
//
// The number is derived from two measurements rather than chosen. Measured on
// the repository's own DVB captures, GoCore interprets a chunk in:
//
//	 64 KiB   p50 ~230us   p99 ~350us
//	256 KiB   p50 ~1.0ms   p99 ~1.05ms
//	  1 MiB   p50 ~3.7ms   p99 ~4.1ms
//	  4 MiB           ~14.9ms
//
// 4 MiB is the ceiling: the normalizer's sink is handed its staging buffer, which
// defaults to that size. So the worst realistic chunk costs about 15 ms in
// process, and a core across a local socket adds a transfer and a round trip to
// that, not an order of magnitude.
//
// The upper bound comes from the viewer. A zap fails when transport readiness is
// not reached within the pipeline's ReadyTimeout, 8s by default. A hung core
// costs that budget exactly one deadline and no more - the caller retires it, and
// every later chunk fails immediately - so the deadline can afford headroom
// without putting the zap at risk.
//
// 500ms is roughly 33x the slowest measured chunk and an eighth of the smallest
// viewer-facing budget. A test in the pipeline package holds the two against each
// other, because a deadline that quietly grew past the zap budget would turn a
// hung core into a hung zap instead of a failed one.
const DefaultIngestDeadline = 500 * time.Millisecond

// Ways a core fails. They are separate because a caller may want to tell them
// apart in a log or a metric - but not in its handling, which is identical for
// all of them and stated on Core.Ingest.
var (
	// ErrCoreTimeout means the core did not answer within the deadline. It says
	// nothing about whether the core is alive; a core that answers later has still
	// missed the chunk, and its state has moved on without the caller.
	ErrCoreTimeout = errors.New("media facts core did not answer within the deadline")

	// ErrCoreGone means the core ended the conversation - a closed pipe, an EOF,
	// a process that is no longer there.
	ErrCoreGone = errors.New("media facts core is gone")

	// ErrCoreCrashed means the core terminated abnormally.
	ErrCoreCrashed = errors.New("media facts core crashed")

	// ErrCoreInvalidResponse means the core answered with something that is not a
	// result: a truncated frame, a field that cannot be, a decode failure.
	ErrCoreInvalidResponse = errors.New("media facts core returned an invalid response")

	// ErrCoreProtocolVersion means the core speaks a version of this contract that
	// this build does not. It is fatal on purpose: a partially understood protocol
	// is how a caller ends up acting on fields that mean something else.
	ErrCoreProtocolVersion = errors.New("media facts core protocol version mismatch")
)

// IsCoreFailure reports whether an error means the core can no longer be trusted
// with the next chunk.
//
// Every failure of a core is one of these, and every one of them has the same
// consequence: the chunk is refused and the core is retired. The classification
// exists so that consequence is applied by asking one question rather than by
// listing errors at each call site - a list that would eventually miss one.
func IsCoreFailure(err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded),
		errors.Is(err, context.Canceled),
		errors.Is(err, ErrCoreTimeout),
		errors.Is(err, ErrCoreGone),
		errors.Is(err, ErrCoreCrashed),
		errors.Is(err, ErrCoreInvalidResponse),
		errors.Is(err, ErrCoreProtocolVersion),
		errors.Is(err, ErrInvalidPacketSize):
		return true
	}
	// An unrecognised error from a core is still an error from a core. Treating
	// only known failures as failures is how an unknown one becomes a silent
	// success.
	return true
}

// Core reads transport stream bytes and reports what they say.
//
// The contract is deliberately coarse and deliberately synchronous. Coarse,
// because a call per 188-byte packet would put a boundary crossing on the hot
// path ten thousand times a second per service and hand the core none of the
// state it needs to answer anything. Synchronous, because the caller has to know
// what a chunk meant before it lets anyone read those bytes - a caller that
// commits bytes first and learns afterwards that they began a new program has
// already handed a subscriber the old stream's truth.
//
//	chunk in -> facts and ordered events out -> caller commits the bytes
//
// The contract is shaped so an implementation could live behind a socket, but
// that is a statement about this interface, not about the caller. MasterRing
// currently calls Ingest while holding the lock its readers use, so a remote core
// that hung would block subscribers, readiness and Close along with it. The
// semantic boundary is here; the fault-isolation boundary is not, and the
// changeset that introduces a remote core has to move the call out of the reader
// lock and give it a deadline before that sentence becomes true.
type Core interface {
	// Ingest consumes one chunk of transport stream. startOffset is the chunk's
	// position in the caller's own monotonic byte coordinate system, and every
	// offset in the result is expressed in that same system.
	//
	// The context bounds the call. An implementation that reaches another process
	// must stop waiting when it expires and must not answer afterwards - a late
	// answer describes a chunk the caller has already refused.
	//
	// Any error, of any kind, means the same thing to the caller and is not
	// negotiable: no facts, no events, no PSI, no bytes committed, and this core
	// is never asked again. That includes a result that is well-formed but
	// incomplete, because a core that consumed less than it was given has moved
	// past bytes the caller is about to throw away.
	Ingest(ctx context.Context, startOffset int64, data []byte) (ParseResult, error)

	// SetTargetProgram selects which program of a multi-program transport is
	// followed. Changing it discards everything read about the previous one.
	//
	// Bounded and fallible for the same reason Ingest is: it is the only other
	// call that reaches the core, and an implementation behind a socket can hang
	// here just as easily. Its failures carry the same consequence.
	SetTargetProgram(ctx context.Context, programNumber uint16) (ParseResult, error)
}

// Deliberately absent from this interface: Snapshot and Reset.
//
// Neither is called on a Core anywhere in production - the caller keeps the facts
// from the last Ingest, and nothing resets a core it is about to retire. Leaving
// them in would mean a later wire protocol has to carry two messages that exist
// only because an interface once listed them. GoCore keeps both as its own
// methods, where they are used internally and by tests.

// EventKind names something the core saw that the caller has to act on. Facts
// describe a state; events describe a moment in the byte stream, and their order
// within a chunk is the order they happened in.
type EventKind uint8

const (
	// EventUnknown is the zero value and means nothing. It exists so that a
	// missing, uninitialised or badly decoded event cannot arrive as a lifecycle
	// change: across a wire boundary the zero value is what a truncated frame or a
	// failed decode produces, and "advance the generation" is the last thing that
	// should mean.
	EventUnknown EventKind = iota

	// EventProgramIdentityChanged reports that the program this stream carries is
	// no longer the one it was: a new PMT version, a different program number, a
	// changed elementary stream layout.
	//
	// It is not a generation. What a caller does with it - bump an epoch, cut
	// workers, invalidate an index - is the caller's decision, and this event
	// carries no opinion about it.
	EventProgramIdentityChanged

	// EventRandomAccessPoint reports an access unit a decoder can be started on,
	// at Offset in the caller's coordinate system.
	EventRandomAccessPoint
)

// Event is one ordered occurrence within an ingested chunk.
type Event struct {
	Kind   EventKind
	Offset int64

	// Joinable reports that the entry point's own access unit contained no
	// scrambled packet. An entry point that classified perfectly out of encrypted
	// payload is not one a decoder can start on.
	Joinable bool
}

// ParseResult is what one chunk meant.
type ParseResult struct {
	// ProcessedThroughOffset is the offset one past the last byte interpreted. A
	// core that consumed less than it was given must say so here; the caller
	// rejects the chunk rather than committing bytes whose meaning it does not
	// have. Partial processing is not a supported outcome - this field exists to
	// make the unsupported case detectable rather than silent.
	ProcessedThroughOffset int64

	// Events are ordered as they occurred within the chunk. A caller must replay
	// them in order: an entry point found before an identity change and one found
	// after it belong to different programs.
	Events []Event

	// Facts is the state after the chunk.
	Facts Facts

	// PSI carries the raw tables for a caller that has to deliver them, unparsed,
	// ahead of an entry point.
	PSI ActivePSI
}

// ActivePSI is the raw PAT and PMT packets of the current program.
//
// The core parses these; it hands the bytes back anyway because the subscriber
// preamble is a transport concern and reconstructing it would mean parsing PSI a
// second time, in a second place, with a second answer.
type ActivePSI struct {
	PAT [][]byte
	PMT [][]byte
}

// Facts is what the core knows about the stream right now.
//
// Deliberately without a generation and without any statement about whether an
// entry point is still reachable: the first is a lifecycle decision and the
// second depends on a buffer this package cannot see.
type Facts struct {
	HasPAT        bool
	HasPMT        bool
	PMTVersion    uint8
	ProgramNumber uint16

	// PMTPID is the PID the PAT names for the target program, or 0 while no PAT
	// naming it has been accepted.
	PMTPID uint16

	VideoPID    uint16
	VideoCodec  VideoCodec
	AudioPIDs   []uint16
	AudioTracks []AudioTrackInfo

	// ParameterSetsSeen reports whether the decoder configuration for the current
	// codec has been observed: SPS and PPS, plus VPS for HEVC.
	ParameterSetsSeen bool

	RandomAccess RandomAccessObservation
	Scrambling   StreamScrambling

	// CleanEntryPoints counts entry points whose own access unit contained no
	// scrambled packet.
	CleanEntryPoints uint64

	// CleanAccessUnits counts every complete picture that arrived without an
	// encrypted packet in it, joinable or not.
	CleanAccessUnits uint64

	// ScrambledVideoConfirmed reports that the video elementary stream is encrypted
	// and enough packets have arrived with none of them clear. It is a judgement
	// about bytes, so it is made here - the caller only has to act on it.
	ScrambledVideoConfirmed bool
}

// GoCore is the in-process implementation: the parser xg2g has always had, now
// behind the boundary instead of inside the ring.
//
// Not safe for concurrent use. The caller serialises Ingest with whatever lock it
// already holds over its own buffer.
type GoCore struct {
	// PSI Table Assembly & Selection
	targetProgramNumber uint16
	patAssembler        psiStreamAssembler
	pmtAssembler        psiStreamAssembler
	patTracker          tableSectionTracker
	pmtTracker          tableSectionTracker
	rawPATPackets       [][]byte
	rawPMTPackets       [][]byte
	hasPATVersion       bool
	patVersion          uint8
	hasPMTVersion       bool
	pmtVersion          uint8
	pmtProgramNumber    uint16
	pmtPID              uint16
	videoPID            uint16
	videoCodec          VideoCodec

	// Stateful Video Parsing & Keyframe Indexing
	currentPESOffset int64
	pesHasKeyframe   bool
	pesHasSPS        bool
	pesHasPPS        bool
	pesHasVPS        bool
	annexBState      uint32 // 4-byte shift register for startcode scanning
	expectingNALByte bool

	// Random access classification for the access unit currently being assembled.
	// Held per access unit because joinability is a property of all of its slices,
	// which is only known once the next access unit begins.
	auHasIRAP       bool
	auHasRecoveryPt bool
	auVCLCount      int
	auIntraVCLCount int
	// auScrambledPackets counts scrambled packets inside the access unit being
	// assembled. An entry point whose own access unit was partly encrypted is not
	// one a decoder can be started on.
	auScrambledPackets int
	cleanRAPCount      uint64
	cleanAccessUnits   uint64
	nalBuf             []byte
	nalKind            nalCaptureKind
	nalLeft            int
	// nalSkip is the number of bytes of the NAL header still to pass over before
	// the capture starts. H.264 has a one byte header, which is already consumed by
	// the classification step; HEVC has two, and capturing from the first of the
	// remaining bytes would read nuh_layer_id as if it were an SEI payload type.
	nalSkip int
	raObs   RandomAccessObservation

	// Scrambling observation, kept per elementary stream rather than as one total.
	// A service whose video descrambles while its audio does not is a different
	// fault from one where neither does, and a single counter cannot tell them apart.
	scrambledVideoPackets uint64
	clearVideoPackets     uint64
	scrambledAudioPackets uint64
	clearAudioPackets     uint64
	// videoClearRun counts consecutive clear video packets, reset by any scrambled
	// one. Descrambling on this receiver was measured coming up intermittently -
	// 733 scrambled packets interleaved with clear ones on one tune - so "a clear
	// packet was seen" is not evidence that the stream is clear.
	videoClearRun uint64
	// audioClearRun is the same measure on the audio streams, kept separately so a
	// service whose video is through but whose audio is not can be told apart.
	audioClearRun uint64
	// audioPIDs holds the elementary audio streams named by the active PMT.
	audioPIDs   []uint16
	audioTracks []AudioTrackInfo

	// audioObservers follow the audio payload of the tracks whose codec this path
	// can read, one per PID. They exist because the audio topology is not a
	// property of the session start: a service moves between stereo and 5.1 on the
	// same PID as programmes change, and a reading taken once at attach describes
	// only whatever happened to be running then.
	//
	// Keyed by PID and rebuilt from scratch whenever the PMT changes, so an
	// observation can never outlive the table that named the stream it came from.
	audioObservers map[uint16]*esaudio.Observer

	// events accumulates what the chunk being ingested meant, in order.
	events []Event
}

// NewGoCore returns a core with no stream state.
func NewGoCore(targetProgramNumber uint16) *GoCore {
	c := &GoCore{targetProgramNumber: targetProgramNumber}
	c.currentPESOffset = -1
	c.annexBState = 0xFFFFFFFF
	c.videoCodec = CodecUnknown
	return c
}

// Ingest interprets one chunk of transport stream.
func (c *GoCore) Ingest(ctx context.Context, startOffset int64, data []byte) (ParseResult, error) {
	if err := ctx.Err(); err != nil {
		return ParseResult{}, err
	}
	if len(data)%TSPacketSize != 0 {
		return ParseResult{}, ErrInvalidPacketSize
	}

	c.events = c.events[:0]
	for i := 0; i < len(data); i += TSPacketSize {
		// Checked during the chunk, not only before it. The largest chunk this
		// path sees is the normalizer's staging buffer, and a caller that gave up
		// half way through one should not wait for the rest.
		//
		// Returning here leaves the parser part way through a chunk the caller
		// will refuse. That is not a leak: the caller retires a core whose Ingest
		// returned an error, so this state is never read again.
		if i%(ctxCheckPackets*TSPacketSize) == 0 {
			if err := ctx.Err(); err != nil {
				return ParseResult{}, err
			}
		}
		c.indexPacketLocked(data[i:i+TSPacketSize], startOffset+int64(i))
	}
	return c.result(startOffset + int64(len(data))), nil
}

// ctxCheckPackets is how often the context is re-read while a chunk is being
// interpreted. At the measured throughput this is a few milliseconds apart -
// often enough that cancellation is not delayed noticeably, rare enough that the
// check does not show up beside the parsing.
const ctxCheckPackets = 4096

// SetTargetProgram selects the program to follow.
func (c *GoCore) SetTargetProgram(ctx context.Context, programNumber uint16) (ParseResult, error) {
	if err := ctx.Err(); err != nil {
		return ParseResult{}, err
	}
	c.events = c.events[:0]
	if c.targetProgramNumber != programNumber {
		c.targetProgramNumber = programNumber
		c.hasPATVersion = false
		c.hasPMTVersion = false
		c.pmtPID = 0
		c.patAssembler.reset()
		c.pmtAssembler.reset()
		c.patTracker.reset()
		c.pmtTracker.reset()
		c.rawPATPackets = nil
		c.resetProgramStateLocked()
	}
	return c.result(0), nil
}

// Reset discards everything read so far.
func (c *GoCore) Reset() ParseResult {
	target := c.targetProgramNumber
	*c = *NewGoCore(target)
	c.events = append(c.events, Event{Kind: EventProgramIdentityChanged})
	return c.result(0)
}

func (c *GoCore) result(through int64) ParseResult {
	events := make([]Event, len(c.events))
	copy(events, c.events)
	return ParseResult{
		ProcessedThroughOffset: through,
		Events:                 events,
		Facts:                  c.Snapshot(),
		PSI:                    c.activePSI(),
	}
}

func (c *GoCore) activePSI() ActivePSI {
	return ActivePSI{PAT: cloneSliceList(c.rawPATPackets), PMT: cloneSliceList(c.rawPMTPackets)}
}

// Snapshot returns what the core knows about the stream right now.
func (c *GoCore) Snapshot() Facts {
	pids := make([]uint16, len(c.audioPIDs))
	copy(pids, c.audioPIDs)

	tracks := make([]AudioTrackInfo, len(c.audioTracks))
	copy(tracks, c.audioTracks)
	// The declaration is fixed by the PMT and only changes with it; the
	// observation moves with the audio. Reading it here rather than storing it on
	// the track is what makes a mid-programme change to 5.1 visible to the next
	// snapshot instead of to the next PMT version.
	for i := range tracks {
		if obs := c.audioObservers[tracks[i].PID]; obs != nil {
			tracks[i].Observed = obs.Current()
		}
	}

	parameterSets := false
	switch c.videoCodec {
	case CodecMPEG2:
		parameterSets = c.pesHasSPS // MPEG-2 Sequence Header (0xB3)
	case CodecH265:
		parameterSets = c.pesHasSPS && c.pesHasPPS && c.pesHasVPS
	default:
		parameterSets = c.pesHasSPS && c.pesHasPPS
	}

	return Facts{
		HasPAT:            c.hasPATVersion,
		HasPMT:            c.hasPMTVersion,
		PMTVersion:        c.pmtVersion,
		ProgramNumber:     c.pmtProgramNumber,
		PMTPID:            c.pmtPID,
		VideoPID:          c.videoPID,
		VideoCodec:        c.videoCodec,
		AudioPIDs:         pids,
		AudioTracks:       tracks,
		ParameterSetsSeen: parameterSets,
		RandomAccess:      c.raObs,
		Scrambling: StreamScrambling{
			VideoScrambled: c.scrambledVideoPackets,
			VideoClear:     c.clearVideoPackets,
			AudioScrambled: c.scrambledAudioPackets,
			AudioClear:     c.clearAudioPackets,
			VideoClearRun:  c.videoClearRun,
			AudioClearRun:  c.audioClearRun,
			AudioPIDs:      pids,
		},
		CleanEntryPoints:        c.cleanRAPCount,
		CleanAccessUnits:        c.cleanAccessUnits,
		ScrambledVideoConfirmed: c.scrambledVideoConfirmedLocked(),
	}
}

// ScrambledVideoConfirmed reports that the video elementary stream is encrypted
// and no clear packet has been seen.
func (c *GoCore) ScrambledVideoConfirmed() bool { return c.scrambledVideoConfirmedLocked() }

var _ Core = (*GoCore)(nil)

func (c *GoCore) indexPacketLocked(pkt []byte, offset int64) {
	if len(pkt) < TSPacketSize || pkt[0] != SyncByte {
		return
	}

	pid := (uint16(pkt[1]&0x1F) << 8) | uint16(pkt[2])
	pusi := (pkt[1] & 0x40) != 0
	afc := (pkt[3] >> 4) & 0x03
	hasPayload := (afc == 0x01 || afc == 0x03)
	if !hasPayload {
		return
	}

	payloadOffset := 4
	if afc == 0x03 {
		afLen := int(pkt[4])
		payloadOffset = 5 + afLen
		if payloadOffset >= TSPacketSize {
			return
		}
	}
	payload := pkt[payloadOffset:]

	// 1. PAT (PID 0) Section Assembly
	if pid == 0 {
		c.feedPSIPacketLocked(true, pkt, pusi, payload)
		return
	}

	// 2. PMT Section Assembly
	if c.pmtPID > 0 && pid == c.pmtPID {
		c.feedPSIPacketLocked(false, pkt, pusi, payload)
		return
	}

	// 3. Video Elementary Stream Keyframe / IDR Indexing
	if c.videoPID > 0 && pid == c.videoPID {
		c.parseVideoPacketLocked(pkt, offset, pusi, payload)
		return
	}

	// 4. Audio elementary streams. Whether the payload arrives clear decides
	//    whether a channel is presentable, and for the codecs that carry their
	//    channel layout in the frame header the payload is also the only place a
	//    channel count above two can be read at all.
	for _, apid := range c.audioPIDs {
		if pid == apid {
			if (pkt[3]>>6)&0x03 != 0 {
				c.scrambledAudioPackets++
				c.audioClearRun = 0
				return
			}
			c.clearAudioPackets++
			c.audioClearRun++
			c.observeAudioPayloadLocked(apid, pusi, payload)
			return
		}
	}
}

// observeAudioPayloadLocked hands one clear audio packet's elementary stream
// bytes to the observer for that PID.
//
// This is where transport ends. The observer is given elementary stream payload
// and nothing else - no PID, no packet, no PMT - so what it reads is a property
// of the audio rather than of how the audio arrived.
func (c *GoCore) observeAudioPayloadLocked(pid uint16, pusi bool, payload []byte) {
	obs := c.audioObservers[pid]
	if obs == nil {
		return
	}

	es := payload
	if pusi {
		// A PES packet starts here, so the elementary stream does not: skip the
		// header. Audio arrives either as an audio stream id or, for AC-3 in DVB,
		// as private_stream_1.
		if len(payload) < 9 || payload[0] != 0x00 || payload[1] != 0x00 || payload[2] != 0x01 {
			return
		}
		if sid := payload[3]; sid != 0xBD && sid != 0xFD && (sid < 0xC0 || sid > 0xDF) {
			return
		}
		esStart := 9 + int(payload[8])
		if esStart >= len(payload) {
			// The optional header ran past this packet. Rare, and not worth state of
			// its own: the remainder is fed as if it were elementary stream, where it
			// has to survive a syncword check and a run of agreeing frames before it
			// could reach the observation.
			return
		}
		es = payload[esStart:]
	}
	obs.Feed(es)
}
func (c *GoCore) feedBytesToAssemblerLocked(isPAT bool, assembler *psiStreamAssembler, chunk []byte, pkt []byte) int {
	if len(chunk) == 0 {
		return 0
	}

	consumed := 0

	// Phase 1: Complete 3-byte header if incomplete
	if len(assembler.buf) < 3 {
		needHeader := 3 - len(assembler.buf)
		toTake := len(chunk)
		if toTake > needHeader {
			toTake = needHeader
		}
		assembler.buf = append(assembler.buf, chunk[:toTake]...)
		assembler.rawPackets = append(assembler.rawPackets, cloneSlice(pkt))
		chunk = chunk[toTake:]
		consumed += toTake

		if len(assembler.buf) >= 3 {
			secLen := int((uint16(assembler.buf[1]&0x0F) << 8) | uint16(assembler.buf[2]))
			assembler.sectionLen = secLen + 3
		} else {
			return consumed
		}
	}

	// Phase 2: Complete body up to sectionLen
	if assembler.sectionLen > 0 && len(assembler.buf) < assembler.sectionLen {
		needed := assembler.sectionLen - len(assembler.buf)
		toTake := len(chunk)
		if toTake > needed {
			toTake = needed
		}
		assembler.buf = append(assembler.buf, chunk[:toTake]...)
		if consumed == 0 { // Not added in Phase 1 for this packet
			assembler.rawPackets = append(assembler.rawPackets, cloneSlice(pkt))
		}
		consumed += toTake

		if len(assembler.buf) >= assembler.sectionLen {
			c.processCompletePSISectionLocked(isPAT, assembler.buf[:assembler.sectionLen], assembler.rawPackets)
			assembler.buf = assembler.buf[:0]
			assembler.sectionLen = 0
			assembler.rawPackets = assembler.rawPackets[:0]
		}
	}

	return consumed
}
func (c *GoCore) feedPSIPacketLocked(isPAT bool, pkt []byte, pusi bool, payload []byte) {
	var assembler *psiStreamAssembler
	if isPAT {
		assembler = &c.patAssembler
	} else {
		assembler = &c.pmtAssembler
	}

	cc := pkt[3] & 0x0F
	if assembler.hasCC {
		if cc == assembler.lastCC {
			if bytes.Equal(pkt, assembler.lastPacket) {
				// Exact byte-for-byte duplicate TS packet: silently ignore
				return
			}
			// Same CC but different content: glitch / discontinuity!
			assembler.reset()
			if !pusi {
				return
			}
		} else if cc != (assembler.lastCC+1)&0x0F {
			// Continuity gap detected: abort corrupted in-flight assembly
			assembler.reset()
			if !pusi {
				return
			}
		}
	}
	assembler.lastCC = cc
	assembler.hasCC = true
	assembler.lastPacket = cloneSlice(pkt)

	offset := 0

	if pusi {
		if len(payload) < 1 {
			return
		}
		pointerField := int(payload[0])

		if pointerField > 0 {
			// Case A: Bytes before pointer complete previous in-flight section
			if len(assembler.buf) > 0 {
				toTake := pointerField
				if 1+toTake > len(payload) {
					assembler.reset()
					return
				}
				c.feedBytesToAssemblerLocked(isPAT, assembler, payload[1:1+toTake], pkt)
				if len(assembler.buf) > 0 {
					// Still incomplete after pointer field: missing data, discard
					assembler.buf = assembler.buf[:0]
					assembler.sectionLen = 0
					assembler.rawPackets = assembler.rawPackets[:0]
				}
			}
			offset = 1 + pointerField
		} else {
			// Case B: pointerField == 0. Discard any unfinished prior section
			assembler.buf = assembler.buf[:0]
			assembler.sectionLen = 0
			assembler.rawPackets = assembler.rawPackets[:0]
			offset = 1
		}
	} else {
		// pusi == false: continue in-flight section
		if len(assembler.buf) > 0 {
			consumed := c.feedBytesToAssemblerLocked(isPAT, assembler, payload, pkt)
			offset = consumed
		} else {
			return
		}
	}

	// Parse subsequent sections in this payload (supporting multiple sections per packet)
	for offset < len(payload) {
		if payload[offset] == 0xFF {
			// MPEG-TS PSI padding / stuffing bytes reached
			break
		}

		avail := len(payload) - offset
		if avail < 3 {
			// Fragmented section header (1-2 bytes) spanning into next packet!
			assembler.buf = append(assembler.buf, payload[offset:]...)
			assembler.sectionLen = 0
			assembler.rawPackets = append(assembler.rawPackets, cloneSlice(pkt))
			break
		}

		tableID := payload[offset]
		expectedTableID := byte(0x00)
		if !isPAT {
			expectedTableID = 0x02
		}
		if tableID != expectedTableID {
			break
		}

		secLen := int((uint16(payload[offset+1]&0x0F) << 8) | uint16(payload[offset+2]))
		fullSecLen := secLen + 3

		if avail >= fullSecLen {
			// Complete section self-contained in this payload chunk!
			sectionBytes := payload[offset : offset+fullSecLen]
			c.processCompletePSISectionLocked(isPAT, sectionBytes, [][]byte{cloneSlice(pkt)})
			offset += fullSecLen
			continue
		}

		// Section spans across to next TS packet
		assembler.buf = append(assembler.buf, payload[offset:]...)
		assembler.sectionLen = fullSecLen
		assembler.rawPackets = append(assembler.rawPackets, cloneSlice(pkt))
		break
	}
}
func (c *GoCore) processCompletePSISectionLocked(isPAT bool, table []byte, rawPackets [][]byte) {
	if len(table) < 12 {
		return
	}

	// Validate MPEG-2 CRC32
	if CalculateMPEG2CRC32(table) != 0 {
		return
	}

	currentNext := table[5] & 0x01
	if currentNext != 1 {
		return
	}

	version := (table[5] >> 1) & 0x1F
	sectionNum := table[6]
	lastSectionNum := table[7]

	if isPAT {
		tableComplete := c.patTracker.addSection(version, sectionNum, lastSectionNum, table, rawPackets)
		if !tableComplete {
			return
		}

		// Full PAT Table Generation Complete: scan all sections for target program
		matchedPID := uint16(0)
		for sIdx := uint8(0); sIdx <= lastSectionNum; sIdx++ {
			sData := c.patTracker.sections[sIdx]
			if len(sData) < 12 {
				continue
			}
			programs := sData[8 : len(sData)-4]
			for i := 0; i+4 <= len(programs); i += 4 {
				progNum := (uint16(programs[i]) << 8) | uint16(programs[i+1])
				progPID := ((uint16(programs[i+2]) & 0x1F) << 8) | uint16(programs[i+3])
				if progNum == 0 {
					continue
				}

				if c.targetProgramNumber > 0 {
					if progNum == c.targetProgramNumber {
						matchedPID = progPID
						break
					}
				} else {
					matchedPID = progPID
					break
				}
			}
			if matchedPID > 0 && c.targetProgramNumber > 0 {
				break
			}
		}

		if matchedPID > 0 {
			if !c.hasPMTVersion || c.pmtPID != matchedPID || !c.hasPATVersion || c.patVersion != version {
				if c.pmtPID != matchedPID {
					c.pmtPID = matchedPID
					c.pmtAssembler.reset()
					c.pmtTracker.reset()
					c.hasPMTVersion = false
					c.resetProgramStateLocked()
				}
			}
			c.hasPATVersion = true
			c.patVersion = version

			// Build deduplicated rawPATPackets from all sections 0..lastSectionNum
			var allPATPackets [][]byte
			for sIdx := uint8(0); sIdx <= lastSectionNum; sIdx++ {
				for _, pkt := range c.patTracker.rawPackets[sIdx] {
					if !containsPacket(allPATPackets, pkt) {
						allPATPackets = append(allPATPackets, cloneSlice(pkt))
					}
				}
			}
			c.rawPATPackets = allPATPackets
		}
	} else {
		progNum := (uint16(table[3]) << 8) | uint16(table[4])
		tableComplete := c.pmtTracker.addSection(version, sectionNum, lastSectionNum, table, rawPackets)
		if !tableComplete {
			return
		}

		// Full PMT Table Generation Complete
		isChanged := !c.hasPMTVersion || version != c.pmtVersion || progNum != c.pmtProgramNumber
		if isChanged {
			c.hasPMTVersion = true
			c.pmtVersion = version
			c.pmtProgramNumber = progNum
			c.resetProgramStateLocked()

			// Scan elementary streams across all sections 0..lastSectionNum
			for sIdx := uint8(0); sIdx <= lastSectionNum; sIdx++ {
				sData := c.pmtTracker.sections[sIdx]
				if len(sData) < 12 {
					continue
				}
				progInfoLen := int((uint16(sData[10]&0x0F) << 8) | uint16(sData[11]))
				esStart := 12 + progInfoLen
				esEnd := len(sData) - 4

				for i := esStart; i+5 <= esEnd && i < len(sData); {
					st := sData[i]
					elemPID := ((uint16(sData[i+1]) & 0x1F) << 8) | uint16(sData[i+2])
					esInfoLen := int((uint16(sData[i+3]&0x0F) << 8) | uint16(sData[i+4]))

					switch st {
					case 0x1B: // H.264 / AVC
						if c.videoPID == 0 {
							c.videoPID = elemPID
							c.videoCodec = CodecH264
						}
					case 0x24, 0x27: // H.265 / HEVC
						if c.videoPID == 0 {
							c.videoPID = elemPID
							c.videoCodec = CodecH265
						}
					case 0x02, 0x01: // MPEG-2 / MPEG-1 Video
						if c.videoPID == 0 {
							c.videoPID = elemPID
							c.videoCodec = CodecMPEG2
						}
					default:
						// The elementary stream loop is now walked to its end rather
						// than stopped at the video entry: the audio streams that
						// follow it are needed to tell "video descrambled, audio did
						// not" apart from "nothing descrambled".
						descriptors := []byte(nil)
						if dStart := i + 5; dStart+esInfoLen <= len(sData) {
							descriptors = sData[dStart : dStart+esInfoLen]
						}
						if isAudioStreamType(st, descriptors) {
							c.audioPIDs = appendPID(c.audioPIDs, elemPID)
							codec := AudioCodecFromStreamType(st, descriptors)
							lang := LanguageFromDescriptors(descriptors)
							declared := AudioChannelsFromDescriptors(descriptors)
							c.audioTracks = appendAudioTrack(c.audioTracks, AudioTrackInfo{
								PID:        elemPID,
								StreamType: st,
								Codec:      codec,
								Language:   lang,
								Declared:   declared,
							})
							if observableAudioCodec(codec) {
								if c.audioObservers == nil {
									c.audioObservers = make(map[uint16]*esaudio.Observer, 2)
								}
								c.audioObservers[elemPID] = esaudio.NewObserver()
							}
						}
					}

					i += 5 + esInfoLen
				}
			}
		}

		// Build deduplicated rawPMTPackets from all sections 0..lastSectionNum
		var allPMTPackets [][]byte
		for sIdx := uint8(0); sIdx <= lastSectionNum; sIdx++ {
			for _, pkt := range c.pmtTracker.rawPackets[sIdx] {
				if !containsPacket(allPMTPackets, pkt) {
					allPMTPackets = append(allPMTPackets, cloneSlice(pkt))
				}
			}
		}
		c.rawPMTPackets = allPMTPackets
	}
}
func (c *GoCore) resetProgramStateLocked() {
	c.videoPID = 0
	c.videoCodec = CodecUnknown
	c.currentPESOffset = -1
	c.pesHasKeyframe = false
	c.pesHasSPS = false
	c.pesHasPPS = false
	c.pesHasVPS = false
	c.annexBState = 0xFFFFFFFF
	c.expectingNALByte = false
	c.rawPMTPackets = nil
	c.scrambledVideoPackets = 0
	c.clearVideoPackets = 0
	c.scrambledAudioPackets = 0
	c.clearAudioPackets = 0
	c.videoClearRun = 0
	c.audioClearRun = 0
	c.audioPIDs = nil
	c.audioTracks = nil
	// Observations do not survive the table that named their stream. The same PID
	// can carry a different elementary stream after a PMT change, and carrying the
	// old layout across would describe audio that is no longer there.
	c.audioObservers = nil
	c.raObs = RandomAccessObservation{}
	c.cleanRAPCount = 0
	c.cleanAccessUnits = 0
	c.resetAccessUnitStateLocked()

	// The caller owns the generation. What changed here is the identity of the
	// program this stream carries; whether that begins a new lifecycle epoch - and
	// what happens to subscribers when it does - is not this layer's call.
	c.events = append(c.events, Event{Kind: EventProgramIdentityChanged})
}
func (c *GoCore) parseVideoPacketLocked(pkt []byte, offset int64, pusi bool, payload []byte) {
	// transport_scrambling_control != 0 means the payload is encrypted. Feeding it to the
	// Annex-B scanner would index random bytes as NAL units, so it is never parsed. The
	// observation is recorded instead, allowing attach to fail fast with ErrScrambledStream.
	if (pkt[3]>>6)&0x03 != 0 {
		c.scrambledVideoPackets++
		c.auScrambledPackets++
		c.videoClearRun = 0
		return
	}
	c.clearVideoPackets++
	c.videoClearRun++

	esData := payload

	if pusi {
		// Video PES packet start: verify PES startcode prefix (00 00 01 E0..EF)
		if len(payload) >= 9 && payload[0] == 0x00 && payload[1] == 0x00 && payload[2] == 0x01 && (payload[3] >= 0xE0 && payload[3] <= 0xEF) {
			// Whether an access unit is joinable depends on every slice in it, which
			// is only known once it ends - and it ends where the next one begins.
			c.finalizeAccessUnitLocked()

			c.currentPESOffset = offset
			c.pesHasKeyframe = false
			c.pesHasSPS = false
			c.pesHasPPS = false
			c.pesHasVPS = false
			c.annexBState = 0xFFFFFFFF
			c.expectingNALByte = false
			c.resetAccessUnitStateLocked()

			pesHeaderDataLen := int(payload[8])
			esStart := 9 + pesHeaderDataLen
			if esStart < len(payload) {
				esData = payload[esStart:]
			} else {
				esData = nil
			}
		}
	}

	if len(esData) == 0 {
		return
	}

	// Stateful Annex-B NAL unit parser across packet boundaries.
	for _, b := range esData {
		c.annexBState = (c.annexBState << 8) | uint32(b)

		// Bytes immediately following a NAL header, held for slice-header and SEI
		// parsing. Collected here because a NAL unit routinely spans TS packets and
		// the fields that matter sit in its first bytes.
		if c.nalLeft > 0 {
			if c.nalSkip > 0 {
				c.nalSkip--
			} else {
				c.nalBuf = append(c.nalBuf, b)
				c.nalLeft--
				if c.nalLeft == 0 {
					c.consumeNALCaptureLocked()
				}
			}
		}

		if c.expectingNALByte {
			c.expectingNALByte = false
			// A start code inside a captured NAL means the capture budget outran the
			// unit; read what was collected before moving on.
			c.consumeNALCaptureLocked()

			switch c.videoCodec {
			case CodecH264:
				c.classifyH264NALLocked(b & 0x1F)
			case CodecH265:
				c.classifyHEVCNALLocked((b >> 1) & 0x3F)
			case CodecMPEG2:
				c.classifyMPEG2StartCodeLocked(b)
			}
		}

		// Check for 3-byte (00 00 01) or 4-byte (00 00 00 01) Annex-B startcode
		if (c.annexBState & 0x00FFFFFF) == 0x00000001 {
			c.expectingNALByte = true
		}
	}
}

// classifyMPEG2StartCodeLocked records what an MPEG-2 Video Elementary Stream start code
// contributes to the current access unit and stream configuration.
// ISO/IEC 13818-2:
// - 0xB3: sequence_header_code (SPS equivalent / Codec Config)
// - 0xB8: group_start_code (GOP Header / Entry Point)
// - 0x00: picture_start_code (Picture Header -> contains picture_coding_type I/P/B)
func (c *GoCore) classifyMPEG2StartCodeLocked(startCode uint8) {
	switch startCode {
	case 0xB3: // Sequence Header
		c.pesHasSPS = true
	case 0xB8: // Group of Pictures Header
		c.pesHasVPS = true
	case 0x00: // Picture Header
		c.auVCLCount++
		c.beginNALCaptureLocked(captureMPEG2PictureHeader, mpeg2PictureHeaderCaptureBytes, 0)
	}
}

// classifyH264NALLocked records what one H.264 NAL unit contributes to the access
// unit being assembled, and starts a capture where the following bytes are needed.
func (c *GoCore) classifyH264NALLocked(nalType uint8) {
	switch nalType {
	case h264NALSliceIDR:
		c.auHasIRAP = true
		c.auVCLCount++
		c.auIntraVCLCount++
		// An IDR needs nothing else to be joinable, so it is indexed without waiting
		// for the access unit to end. This keeps streams that do emit IDRs attaching
		// exactly as fast as before.
		c.indexRandomAccessPointLocked(true)
	case h264NALSliceNonIDR, h264NALSlicePartA:
		c.auVCLCount++
		c.beginNALCaptureLocked(captureH264SliceHeader, sliceHeaderCaptureBytes, 0)
	case h264NALSPS:
		c.pesHasSPS = true
	case h264NALPPS:
		c.pesHasPPS = true
	case h264NALSEI:
		// The one byte H.264 NAL header has already been consumed, so the SEI
		// payload starts with the very next byte.
		c.beginNALCaptureLocked(captureSEI, seiCaptureBytes, 0)
	}
}

// classifyHEVCNALLocked mirrors classifyH264NALLocked for HEVC.
//
// The whole IRAP range qualifies, not only IDR: a CRA or BLA picture is equally a
// point a decoder can be started on. HEVC slice headers are not parsed for intra
// coding - the fields needed sit behind the active parameter sets - so a stream that
// never emits an IRAP is admitted on its recovery_point SEI instead, which is the
// stream's own declaration of a random access point.
func (c *GoCore) classifyHEVCNALLocked(nalType uint8) {
	switch {
	case nalType >= hevcNALIRAPFirst && nalType <= hevcNALIRAPLast:
		c.auHasIRAP = true
		c.auVCLCount++
		c.auIntraVCLCount++
		c.indexRandomAccessPointLocked(true)
	case nalType <= 9:
		c.auVCLCount++
	case nalType == hevcNALVPS:
		c.pesHasVPS = true
	case nalType == hevcNALSPS:
		c.pesHasSPS = true
	case nalType == hevcNALPPS:
		c.pesHasPPS = true
	case nalType == hevcNALPrefixSEI:
		// The HEVC NAL header is two bytes. Only the first has been consumed by the
		// classification above, so the second is passed over: reading it as the SEI
		// payload type shifts every field that follows and hides the recovery point,
		// which on a stream that emits no IRAP is the only signal there is.
		c.beginNALCaptureLocked(captureSEI, seiCaptureBytes, 1)
	}
}
func (c *GoCore) beginNALCaptureLocked(kind nalCaptureKind, budget, skipHeaderBytes int) {
	c.nalKind = kind
	c.nalLeft = budget
	c.nalSkip = skipHeaderBytes
	c.nalBuf = c.nalBuf[:0]
}

// consumeNALCaptureLocked reads whatever was captured and clears the capture.
func (c *GoCore) consumeNALCaptureLocked() {
	if c.nalKind == captureNone || len(c.nalBuf) == 0 {
		c.nalKind = captureNone
		c.nalLeft = 0
		c.nalSkip = 0
		return
	}

	switch c.nalKind {
	case captureH264SliceHeader:
		isIntra, ok := h264SliceIsIntra(c.nalBuf)
		switch {
		case !ok:
			c.raObs.UnreadableSlices++
		case isIntra:
			c.auIntraVCLCount++
		}
	case captureSEI:
		if seiHasRecoveryPoint(c.nalBuf) {
			c.auHasRecoveryPt = true
		}
	case captureMPEG2PictureHeader:
		isIntra, ok := mpeg2PictureIsIntra(c.nalBuf)
		switch {
		case !ok:
			c.raObs.UnreadableSlices++
		case isIntra:
			c.auHasIRAP = true
			c.auIntraVCLCount++
			c.indexRandomAccessPointLocked(true)
		}
	}

	c.nalKind = captureNone
	c.nalLeft = 0
	c.nalSkip = 0
	c.nalBuf = c.nalBuf[:0]
}

// finalizeAccessUnitLocked decides whether the access unit that just ended was a
// random access point, now that all of its slices have been seen.
func (c *GoCore) finalizeAccessUnitLocked() {
	c.consumeNALCaptureLocked()

	if c.currentPESOffset < 0 || c.auVCLCount == 0 {
		return
	}

	// Descrambling is proven by any complete picture that arrived without an
	// encrypted packet in it, not only by one a decoder could start on. Measured
	// against the receiver, tying the two together made them true at the same
	// millisecond in every zap, so the descrambling observation carried no
	// information of its own. A clear predicted picture arrives up to a GOP before
	// the next clear entry point does.
	if c.auScrambledPackets == 0 {
		c.cleanAccessUnits++
	}

	if c.pesHasKeyframe {
		return
	}

	// An access unit with no parameter sets cannot configure a decoder that is
	// starting cold, whatever its slices contain.
	hasParameterSets := c.pesHasSPS && c.pesHasPPS
	if c.videoCodec == CodecH265 {
		hasParameterSets = hasParameterSets && c.pesHasVPS
	}
	if !hasParameterSets {
		return
	}

	joinable := false
	switch c.videoCodec {
	case CodecH264:
		// Every coded slice intra means the picture stands alone. One predicted
		// slice is enough to disqualify it: the decoder would be missing exactly
		// the references that slice names.
		joinable = c.auIntraVCLCount == c.auVCLCount
	case CodecH265:
		joinable = c.auHasRecoveryPt
	}

	if !joinable {
		if c.auHasRecoveryPt || c.auVCLCount > c.auIntraVCLCount {
			c.raObs.PredictedRejected++
		}
		return
	}

	c.indexRandomAccessPointLocked(false)
}

// indexRandomAccessPointLocked records the current access unit as an attach point.
func (c *GoCore) indexRandomAccessPointLocked(irap bool) {
	if c.pesHasKeyframe || c.currentPESOffset < 0 {
		return
	}
	c.pesHasKeyframe = true

	if irap {
		c.raObs.IRAPPoints++
	} else {
		c.raObs.IntraPoints++
	}
	if c.auHasRecoveryPt {
		c.raObs.RecoveryPointSEIs++
	}
	// An entry point whose own access unit carried scrambled packets is not one a
	// decoder can be started on, however well it classified.
	if c.auScrambledPackets == 0 {
		c.cleanRAPCount++
	}

	// Reported in the caller's own byte coordinate system, so the offset can be
	// handed straight to a reader without translation. Indexing it, ageing it out
	// and deciding it is still reachable belong to whoever owns the buffer.
	c.events = append(c.events, Event{
		Kind:     EventRandomAccessPoint,
		Offset:   c.currentPESOffset,
		Joinable: c.auScrambledPackets == 0,
	})
}
func (c *GoCore) resetAccessUnitStateLocked() {
	c.auHasIRAP = false
	c.auHasRecoveryPt = false
	c.auVCLCount = 0
	c.auIntraVCLCount = 0
	c.auScrambledPackets = 0
	c.nalKind = captureNone
	c.nalLeft = 0
	c.nalSkip = 0
	c.nalBuf = c.nalBuf[:0]
}

// scrambledVideoConfirmedLocked reports whether the video stream is conclusively scrambled.
// It requires a minimum sample so that a handful of packets observed mid key-change cannot
// trip the verdict, and demands that not one clear payload packet has been seen: a receiver
// that descrambles clears transport_scrambling_control on every packet it emits.
func (c *GoCore) scrambledVideoConfirmedLocked() bool {
	return c.clearVideoPackets == 0 && c.scrambledVideoPackets >= ScrambledVerdictMinPackets
}

// observableAudioCodec reports whether the frame headers of a codec are read for
// their channel layout. Everything else is left to the declaration and to
// whatever the media path already does; a codec this parser cannot read produces
// no observation rather than a guessed one.
func observableAudioCodec(codec string) bool {
	return codec == esaudio.CodecAC3 || codec == esaudio.CodecEAC3
}
func containsPacket(list [][]byte, pkt []byte) bool {
	for _, existing := range list {
		if bytes.Equal(existing, pkt) {
			return true
		}
	}
	return false
}
func appendAudioTrack(list []AudioTrackInfo, track AudioTrackInfo) []AudioTrackInfo {
	for i, existing := range list {
		if existing.PID == track.PID {
			list[i] = track
			return list
		}
	}
	return append(list, track)
}
