// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ring

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/stream/ingest/mediafacts"
)

var (
	ErrRingClosed        = errors.New("master ring buffer closed")
	ErrNoKeyframeFound   = errors.New("no keyframe index found in buffer")
	ErrSubscriberOverrun = errors.New("subscriber was overtaken by master ring write head")
	// ErrScrambledStream reports that the upstream video elementary stream carries a non-zero
	// transport_scrambling_control field. Encrypted payload can never yield a valid Annex-B
	// keyframe, so waiting for one is futile: the receiver is not descrambling this service.
	ErrScrambledStream = errors.New("upstream video elementary stream is scrambled; receiver descrambling unavailable")
)

// Transport constants and the alignment error belong with the parser; they are
// re-exported here because the whole repository already imports them by this name.
const (
	TSPacketSize = mediafacts.TSPacketSize
	SyncByte     = mediafacts.SyncByte
)

// ErrInvalidPacketSize reports data that is not 188-byte packet aligned.
var ErrInvalidPacketSize = mediafacts.ErrInvalidPacketSize

// ErrCoreIncomplete reports a media core that interpreted less of a chunk than it
// was given. The chunk is refused rather than committed, because bytes the ring
// cannot explain are worse than bytes it does not have.
var ErrCoreIncomplete = errors.New("media facts core did not interpret the whole chunk")

// ErrCoreUnusable reports a core that has already failed once. A core that
// errored or came back short may have consumed bytes the ring then refused, so
// its state and the ring's have diverged and no later answer from it can be
// trusted. Recovery is a new core, not another attempt at this one.
var ErrCoreUnusable = errors.New("media facts core is unusable after an earlier failure")

// ErrRingAdvanced reports that the ring moved while a chunk was being
// interpreted, so what the core read no longer describes where the bytes would
// land. The chunk is refused rather than committed at the wrong offset.
var ErrRingAdvanced = errors.New("ring advanced while the chunk was being interpreted")

// The interpretation of transport stream bytes lives in mediafacts. These aliases
// keep the names consumers already import while the boundary is drawn: the ring
// owns bytes, offsets and the generation; mediafacts owns what the bytes mean.
type (
	VideoCodec              = mediafacts.VideoCodec
	AudioTrackInfo          = mediafacts.AudioTrackInfo
	AudioChannelDeclaration = mediafacts.AudioChannelDeclaration
	RandomAccessObservation = mediafacts.RandomAccessObservation
	StreamScrambling        = mediafacts.StreamScrambling
)

const (
	CodecUnknown = mediafacts.CodecUnknown
	CodecH264    = mediafacts.CodecH264
	CodecH265    = mediafacts.CodecH265
	CodecMPEG2   = mediafacts.CodecMPEG2
)

// CalculateMPEG2CRC32 calculates the standard ISO/IEC 13818-1 32-bit CRC.
func CalculateMPEG2CRC32(data []byte) uint32 { return mediafacts.CalculateMPEG2CRC32(data) }

// MasterRing is a thread-safe, multi-reader circular FIFO buffer for MPEG-TS streams.
// It maintains an in-band, stateful index of PAT, PMT, and PES/IDR Keyframe byte offsets.
// MasterRing is a thread-safe, multi-reader circular FIFO buffer for MPEG-TS
// streams. It owns the bytes, their monotonic offsets, the entry-point index and
// the generation; what the bytes mean is read by a mediafacts.Core it holds.
type MasterRing struct {
	mu              sync.Mutex
	notEmpty        *sync.Cond
	buf             []byte
	capacity        int
	head            int64 // total bytes written monotonically
	tail            int64 // oldest valid byte offset in buffer
	isClosed        bool
	keyframeOffsets []int64
	maxKeyframes    int
	generation      uint64

	// ingestMu serialises writers and owns the core for the length of a call. It
	// exists so the core can run without r.mu: a core behind a socket may hang,
	// and a hung core must not take subscribers, readiness and Close with it.
	//
	// Lock order is always ingestMu before mu, never the reverse. Close and every
	// reader take mu alone, which is what keeps them reachable while a core runs.
	ingestMu sync.Mutex

	// coreUnusable marks a core that answered with an error or an incomplete
	// result. Such a core may already have advanced past bytes the ring refused,
	// so its next answer would describe a stream nobody is holding. Guarded by
	// ingestMu, because it is a property of the core rather than of the ring.
	coreUnusable bool

	// ingestDeadline bounds a single call into the core. It is not a budget to be
	// spent - it is how long a core may go quiet before it is treated as gone.
	ingestDeadline time.Duration

	// core reads what the transport stream says about itself. It is given byte
	// chunks and the offset they start at, and answers with facts and ordered
	// events; it never sees this struct. Guarded by ingestMu.
	core mediafacts.Core

	// facts is the last answer the core gave, cached so an accessor never calls
	// across the boundary while holding the ring lock. The facts only move when a
	// chunk is ingested, so a cache cannot be stale between chunks.
	facts mediafacts.Facts

	// activePSI is the raw PAT/PMT the core last parsed, kept because the
	// subscriber delivers those packets ahead of an entry point. Interpretation is
	// the core's; delivery is the ring's, and neither parses them twice.
	activePSI mediafacts.ActivePSI
}

// NewMasterRing creates a new MasterRing with the specified capacity (aligned to 188 bytes).
func NewMasterRing(capacityBytes int) *MasterRing {
	return NewMasterRingWithProgram(capacityBytes, 0)
}

// NewMasterRingWithProgram creates a new MasterRing targeting a specific program number in multi-program PATs.
func NewMasterRingWithProgram(capacityBytes int, targetProgram uint16) *MasterRing {
	capacityBytes = (capacityBytes / TSPacketSize) * TSPacketSize
	if capacityBytes < TSPacketSize*5 {
		capacityBytes = TSPacketSize * 5 // min 5 packets (~940 bytes)
	}

	r := &MasterRing{
		buf:            make([]byte, capacityBytes),
		capacity:       capacityBytes,
		maxKeyframes:   64,
		ingestDeadline: mediafacts.DefaultIngestDeadline,
		core:           mediafacts.NewGoCore(targetProgram),
	}
	r.notEmpty = sync.NewCond(&r.mu)
	return r
}

// SetTargetProgram configures the desired program number for PMT resolution,
// immediately invalidating existing PSI and decoder states if the target changed.
func (r *MasterRing) SetTargetProgram(ctx context.Context, progNum uint16) error {
	// Before the core is entered, a caller that gave up means the call never
	// happened. Nothing was interpreted, so nothing diverged, and the core is
	// still good for the next chunk.
	if err := ctx.Err(); err != nil {
		return err
	}

	// The core is single-threaded by contract, so this cannot run beside an
	// Ingest. It waits behind a slow core, which is the right trade: this is
	// control plane, and the calls that must never wait are Close and the readers.
	r.ingestMu.Lock()
	defer r.ingestMu.Unlock()

	if r.coreUnusable {
		return ErrCoreUnusable
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	callCtx, cancel := context.WithTimeout(ctx, r.ingestDeadline)
	defer cancel()

	res, err := r.core.SetTargetProgram(callCtx, progNum)
	if err != nil {
		return r.retireCore(ctx, err)
	}

	// Same reasoning as in Push: a core that answered after the call stopped being
	// wanted has still answered, and its result is not applied.
	if err := ctx.Err(); err != nil {
		return r.retireCore(ctx, err)
	}
	if err := callCtx.Err(); err != nil {
		return r.retireCore(ctx, err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.isClosed {
		r.coreUnusable = true
		return ErrRingClosed
	}
	r.applyLocked(res)
	return nil
}

// retireCore records that the core can no longer be trusted and names why.
//
// Every failure after the core has been entered lands here, because every one of
// them leaves the same wreckage: the core consumed something the ring is about to
// throw away, so the two no longer describe the same stream. The distinction the
// caller may care about - did it time out, or did the caller give up - is kept in
// the returned error, not in whether the core survives.
func (r *MasterRing) retireCore(callerCtx context.Context, err error) error {
	r.coreUnusable = true
	if callerCtx.Err() == nil && errors.Is(err, context.DeadlineExceeded) {
		// Our own deadline, not the caller's. The core went quiet.
		return errors.Join(mediafacts.ErrCoreTimeout, err)
	}
	return err
}

// applyLocked takes what the core said about a chunk and acts on it.
//
// This is where the ownership split is executed. The core reported that the
// program's identity changed and where the entry points are; the epoch those
// belong to, and whether an entry point is still reachable, are decided here -
// in order, because an entry point found before an identity change and one found
// after it belong to different programs.
func (r *MasterRing) applyLocked(res mediafacts.ParseResult) {
	for _, ev := range res.Events {
		switch ev.Kind {
		case mediafacts.EventProgramIdentityChanged:
			r.keyframeOffsets = r.keyframeOffsets[:0]
			r.generation++
		case mediafacts.EventRandomAccessPoint:
			r.keyframeOffsets = append(r.keyframeOffsets, ev.Offset)
			if len(r.keyframeOffsets) > r.maxKeyframes {
				r.keyframeOffsets = r.keyframeOffsets[1:]
			}
		default:
			// EventUnknown, or a kind this build does not know. Both are refused
			// rather than guessed at: across a wire boundary the zero value is what a
			// truncated or mis-decoded event looks like, and the two things this
			// switch can do - end an epoch, offer an attach point - are the two things
			// that must never happen by accident.
		}
	}
	r.facts = res.Facts
	r.activePSI = res.PSI
}

// Push writes a chunk of TS packets into the ring buffer and indexes PAT/PMT/IDR boundaries.
func (r *MasterRing) Push(ctx context.Context, data []byte) (int, error) {
	if len(data)%TSPacketSize != 0 {
		return 0, ErrInvalidPacketSize
	}

	n := len(data)

	// Cancellation before the core is entered is not a core failure. Nothing was
	// interpreted, so the core and the ring still agree and it stays usable. That
	// is the whole distinction: not that the context expired, but whether the core
	// had already consumed something by the time it did.
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	// One writer at a time, and the core belongs to whoever holds this. Taken
	// before r.mu and released after it, never the other way round.
	r.ingestMu.Lock()
	defer r.ingestMu.Unlock()

	if r.coreUnusable {
		return 0, ErrCoreUnusable
	}

	// Re-checked: waiting for the writer lock can take as long as the core call
	// ahead of it, and a caller that gave up during that wait is in the same
	// position as one that gave up before it.
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	// 1. Read the ring's position, then let go of it. Everything a reader needs -
	//    subscribers, readiness, Close - stays reachable while the core works.
	r.mu.Lock()
	if r.isClosed {
		r.mu.Unlock()
		return 0, ErrRingClosed
	}
	startOffset := r.head
	r.mu.Unlock()

	// An empty chunk has nothing to interpret and nothing to commit, so the core
	// is never entered for one. It is answered here rather than at the top of the
	// function: whether the ring is still accepting writes is a question about the
	// ring, and an empty chunk does not exempt a caller from the answer.
	if n == 0 {
		return 0, nil
	}

	// 2. Interpret the chunk with no ring lock held. This is the call that may sit
	//    on a socket, and it is the reason the lock above was released.
	// The core runs under a deadline of its own, derived from the caller's context
	// rather than replacing it: whichever expires first ends the call.
	ingestCtx, cancel := context.WithTimeout(ctx, r.ingestDeadline)
	defer cancel()

	res, err := r.core.Ingest(ingestCtx, startOffset, data)
	if err != nil {
		// Past this point the core has been entered, so every way out that does
		// not commit retires it - the caller's own cancellation included.
		return 0, r.retireCore(ctx, err)
	}

	// A successful return is not the same as a return that is still wanted. A core
	// that ignores its context - which a remote one may do by accident, or by
	// being a process that finished its work before noticing the socket closed -
	// can hand back a complete, well-formed result for a chunk nobody is waiting
	// for any more. Committing it would publish bytes past the point the caller
	// gave up, which is the one thing the deadline exists to prevent.
	//
	// The contract cannot assume cooperation. It has to hold for a core that does
	// the wrong thing, because that is the core it will eventually meet.
	if err := ctx.Err(); err != nil {
		return 0, r.retireCore(ctx, err)
	}
	if err := ingestCtx.Err(); err != nil {
		return 0, r.retireCore(ctx, err)
	}
	// A core that interpreted less than it was given leaves the ring with bytes it
	// has no meaning for. Committing them anyway is exactly the failure this
	// boundary exists to prevent, so the chunk is refused and nothing moves: not
	// the head, not the generation, not the index, not the facts. The core is
	// finished either way - it consumed what the ring is about to throw away.
	if want := startOffset + int64(n); res.ProcessedThroughOffset != want {
		return 0, r.retireCore(ctx, ErrCoreIncomplete)
	}

	// 3. Commit. Facts, events, PSI and bytes become visible together, under one
	//    hold of the lock, or none of them do.
	r.mu.Lock()
	defer r.mu.Unlock()

	// The ring was unlocked while the core ran, so what it read may no longer
	// describe the ring it was read against. Committing after a Close would
	// publish into a stream nobody is holding; committing at a moved head would
	// publish at the wrong offset. Both are refused rather than reconciled.
	//
	// Both also leave the core holding a chunk the ring does not have, which is
	// the same divergence an error or a short result produces - the reason for it
	// differs, the consequence does not. Every path that returns after Ingest
	// without committing retires the core, so no later path can be added that
	// forgets to. ingestMu is still held, which is what guards the flag.
	if r.isClosed {
		return 0, r.retireCore(ctx, ErrRingClosed)
	}
	if r.head != startOffset {
		return 0, r.retireCore(ctx, ErrRingAdvanced)
	}

	r.applyLocked(res)

	// 4. Write data into circular buffer safely, supporting len(data) > capacity without panic
	remaining := data
	for len(remaining) > 0 {
		chunk := remaining
		if len(chunk) > r.capacity {
			chunk = chunk[:r.capacity]
		}
		remaining = remaining[len(chunk):]

		chunkLen := len(chunk)
		writePos := int(r.head % int64(r.capacity))
		firstChunk := r.capacity - writePos
		if chunkLen <= firstChunk {
			copy(r.buf[writePos:], chunk)
		} else {
			copy(r.buf[writePos:], chunk[:firstChunk])
			copy(r.buf[:chunkLen-firstChunk], chunk[firstChunk:])
		}

		r.head += int64(chunkLen)
		if r.head-r.tail > int64(r.capacity) {
			r.tail = r.head - int64(r.capacity)
			r.pruneKeyframesLocked()
		}
	}

	r.notEmpty.Broadcast()
	return n, nil
}

// RandomAccess returns how access units have been classified on this stream.
func (r *MasterRing) RandomAccess() RandomAccessObservation {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.facts.RandomAccess
}

func (r *MasterRing) pruneKeyframesLocked() {
	validIdx := -1
	for i, offset := range r.keyframeOffsets {
		if offset >= r.tail {
			validIdx = i
			break
		}
	}
	if validIdx == -1 {
		r.keyframeOffsets = r.keyframeOffsets[:0]
	} else if validIdx > 0 {
		r.keyframeOffsets = r.keyframeOffsets[validIdx:]
	}
}

// PrimedAttachPoint represents an atomic, generation-locked stream entry point.
type PrimedAttachPoint struct {
	Preamble       []byte
	KeyframeOffset int64
	Generation     uint64
	HasKeyframe    bool
}

// PrimedAttachPoint captures an atomic snapshot of the active PAT/PMT preamble,
// the latest valid keyframe offset, and the active stream generation under a single lock.
func (r *MasterRing) PrimedAttachPoint() PrimedAttachPoint {
	r.mu.Lock()
	defer r.mu.Unlock()

	var preamble []byte
	for _, pkt := range r.activePSI.PAT {
		preamble = append(preamble, pkt...)
	}
	for _, pkt := range r.activePSI.PMT {
		preamble = append(preamble, pkt...)
	}

	var kfOffset int64
	var hasKf bool
	if len(r.keyframeOffsets) > 0 {
		latest := r.keyframeOffsets[len(r.keyframeOffsets)-1]
		if latest >= r.tail {
			kfOffset = latest
			hasKf = true
		}
	}

	return PrimedAttachPoint{
		Preamble:       preamble,
		KeyframeOffset: kfOffset,
		Generation:     r.generation,
		HasKeyframe:    hasKf,
	}
}

// LatestKeyframeOffset returns the absolute byte offset of the most recent valid keyframe.
func (r *MasterRing) LatestKeyframeOffset() (int64, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.latestKeyframeOffsetLocked()
}

// latestKeyframeOffsetLocked reports the newest random access point still held by
// the ring. A keyframe that has fallen behind the tail is gone even though its
// offset is still indexed, so it is not a valid entry point.
//
// Callers that already hold r.mu use this; the exported wrappers must not, because
// r.mu is not reentrant and SubscriberReader.Read holds it across recovery.
func (r *MasterRing) latestKeyframeOffsetLocked() (int64, bool) {
	if len(r.keyframeOffsets) == 0 {
		return 0, false
	}
	latest := r.keyframeOffsets[len(r.keyframeOffsets)-1]
	if latest < r.tail {
		return 0, false
	}
	return latest, true
}

// PATPMTPreamble returns concatenated raw PAT and PMT TS packets across all active sections.
func (r *MasterRing) PATPMTPreamble() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.patpmtPreambleLocked()
}

// patpmtPreambleLocked builds the active topology preamble for callers already
// holding r.mu. See latestKeyframeOffsetLocked for why the split exists.
func (r *MasterRing) patpmtPreambleLocked() []byte {
	var preamble []byte
	for _, pkt := range r.activePSI.PAT {
		preamble = append(preamble, pkt...)
	}
	for _, pkt := range r.activePSI.PMT {
		preamble = append(preamble, pkt...)
	}
	return preamble
}

// Generation returns the ring's current topology epoch. It advances whenever the
// video state is invalidated, which a PMT version bump and a program number change
// both do, so a consumer that captured a generation at attach can tell whether the
// stream it is reading is still the one it was configured for.
func (r *MasterRing) Generation() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.generation
}

// VideoDetails returns authoritative video PID and Codec discovered from PMT.
func (r *MasterRing) VideoDetails() (uint16, VideoCodec) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.facts.VideoPID, r.facts.VideoCodec
}

// Head returns total bytes written monotonically.
func (r *MasterRing) Head() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.head
}

// Tail returns the oldest valid byte offset.
func (r *MasterRing) Tail() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.tail
}

// BufferedBytes returns total valid unpruned bytes in the ring.
func (r *MasterRing) BufferedBytes() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return int(r.head - r.tail)
}

// Close closes the master ring buffer, waking all blocked subscriber readers.
func (r *MasterRing) Close() {
	// Deliberately does not take ingestMu. Close has to work while a core is
	// running, including one that will never return; a chunk in flight sees the
	// closed flag when it comes back to commit and is refused there.
	r.mu.Lock()
	defer r.mu.Unlock()

	r.isClosed = true
	r.notEmpty.Broadcast()
}

// ScramblingObservation reports how many payload-carrying TS packets on the selected video PID
// were seen scrambled versus clear. Intended for diagnostics and telemetry.
func (r *MasterRing) ScramblingObservation() (scrambled uint64, clear uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.facts.Scrambling.VideoScrambled, r.facts.Scrambling.VideoClear
}

// StreamScrambling reports descrambling per elementary stream.
//
// Separate counters because the two faults they distinguish need different answers:
// video clear with audio scrambled is a service the receiver is only half
// descrambling, while neither clear is a service it is not descrambling at all.

// Scrambling returns the per-stream descrambling observation.
func (r *MasterRing) Scrambling() StreamScrambling {
	r.mu.Lock()
	defer r.mu.Unlock()
	pids := make([]uint16, len(r.facts.AudioPIDs))
	copy(pids, r.facts.AudioPIDs)
	return StreamScrambling{
		VideoScrambled: r.facts.Scrambling.VideoScrambled,
		VideoClear:     r.facts.Scrambling.VideoClear,
		AudioScrambled: r.facts.Scrambling.AudioScrambled,
		AudioClear:     r.facts.Scrambling.AudioClear,
		VideoClearRun:  r.facts.Scrambling.VideoClearRun,
		AudioClearRun:  r.facts.Scrambling.AudioClearRun,
		AudioPIDs:      pids,
	}
}

// KeyframeOffsets returns the currently indexed random access offsets.
func (r *MasterRing) KeyframeOffsets() []int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]int64, len(r.keyframeOffsets))
	copy(out, r.keyframeOffsets)
	return out
}

// ReadinessFacts is everything the ring knows that bears on whether a channel is
// presentable. It is a snapshot, taken without blocking the ingest.
//
// Deliberately facts rather than a verdict: what counts as presentable is a policy
// question that belongs one layer up, and keeping it there means the policy can be
// measured against reality before it is enforced.
type ReadinessFacts struct {
	// Generation increments whenever the PSI describing this stream changes, which
	// is what a PMT version bump or a codec change looks like from here. A consumer
	// that carries timestamps across a generation change is describing two different
	// streams as if they were one.
	Generation uint64

	HasPAT        bool
	HasPMT        bool
	PMTVersion    uint8
	ProgramNumber uint16

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
	// scrambled packet. An entry point is only usable if this is non-zero.
	CleanEntryPoints uint64

	// CleanAccessUnits counts every complete picture that arrived without an
	// encrypted packet in it, joinable or not. This is what proves the receiver is
	// descrambling, and it becomes true up to a GOP before the next clean entry
	// point does.
	CleanAccessUnits uint64

	// AttachAvailable reports whether an entry point is currently within the buffer.
	AttachAvailable bool
}

// ReadinessFacts captures what the ring currently knows about this stream.
func (r *MasterRing) ReadinessFacts() ReadinessFacts {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Everything the stream says about itself comes from the core. The two fields
	// added here are the ones it cannot answer: which lifecycle epoch this is, and
	// whether an entry point is still inside a buffer it cannot see.
	// Copied on the way out. The cached facts are the ring's own state, and a
	// consumer that received the backing arrays could write through a snapshot
	// into the ring - or read them while a concurrent Push replaces what they
	// describe. The pre-seam code copied these for the same reason.
	f := r.facts
	f.AudioPIDs = append([]uint16(nil), f.AudioPIDs...)
	f.AudioTracks = append([]AudioTrackInfo(nil), f.AudioTracks...)
	f.Scrambling.AudioPIDs = append([]uint16(nil), f.Scrambling.AudioPIDs...)

	attach := false
	if len(r.keyframeOffsets) > 0 {
		attach = r.keyframeOffsets[len(r.keyframeOffsets)-1] >= r.tail
	}

	return ReadinessFacts{
		Generation:        r.generation,
		HasPAT:            f.HasPAT,
		HasPMT:            f.HasPMT,
		PMTVersion:        f.PMTVersion,
		ProgramNumber:     f.ProgramNumber,
		VideoPID:          f.VideoPID,
		VideoCodec:        f.VideoCodec,
		AudioPIDs:         f.AudioPIDs,
		AudioTracks:       f.AudioTracks,
		ParameterSetsSeen: f.ParameterSetsSeen,
		RandomAccess:      f.RandomAccess,
		Scrambling:        f.Scrambling,
		CleanEntryPoints:  f.CleanEntryPoints,
		CleanAccessUnits:  f.CleanAccessUnits,
		AttachAvailable:   attach,
	}
}

// scrambledVideoConfirmedLocked asks the core's verdict. The reader goes through
// the ring because the ring holds the lock, not because the ring decides.
func (r *MasterRing) scrambledVideoConfirmedLocked() bool {
	return r.facts.ScrambledVideoConfirmed
}
