// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ring

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/stream/ingest/mediafacts"
	"github.com/ManuGH/xg2g/internal/stream/timeline"
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

// ErrCoreIncompleteResult means the core answered about less than the ring
// commits.
//
// Separate from ErrCoreIncomplete, which is about bytes: that one is a core that
// read part of a chunk, this one is a core that read part of a *stream*. A core
// answering only about PSI gives real PAT and PMT facts and leaves the rest of
// Facts at its zero value - and every one of those zero values is also a
// legitimate answer. No entry point seen, nothing scrambled, no parameter sets:
// a ring committing that would publish an absence as an observation.
//
// This is the gate that keeps a differential from becoming a cutover. During the
// media-core migration a second core is asked the same chunks as the first, and
// it answers about PSI alone; the ring must be structurally unable to use it,
// not merely not configured to.
var ErrCoreIncompleteResult = errors.New("media facts core answered about less than the ring commits")

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

// MasterRing is a thread-safe, multi-reader circular FIFO coordinator for MPEG-TS streams.
// It composes a pure byte store (packetStore), an atomic media facts coordinator,
// an optional canonical timeline index (timeline.MediaIndex), and a lightweight
// derived subscriber join cache (attachIndex).
//
// Invariant: MasterRing.mu is the sole COMMIT and PUBLICATION lock for the stream.
// All visible state (bytes, head, tail, keyframes, generation, facts, PSI) is
// committed and published atomically under this lock.
type MasterRing struct {
	mu       sync.Mutex
	notEmpty *sync.Cond
	isClosed bool

	// store is the pure circular byte buffer for monotonic stream storage.
	store *packetStore

	// attachIndex is the lightweight derived join and recovery cache for subscribers.
	attachIndex *attachIndex

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
	// chunk is ingested, so a cache cannot be stale between chunks. Guarded by mu.
	facts mediafacts.Facts

	// activePSI is the PAT/PMT sections the core last accepted, kept because the
	// subscriber is delivered those tables ahead of an entry point. Guarded by mu.
	activePSI mediafacts.ActivePSI

	// timelineIndex maintains the canonical timing index when configured via WithCanonicalTimeline.
	// MasterRing acts as the exclusive owner, single writer, and commit gatekeeper under mu.
	timelineIndex *timeline.MediaIndex

	// timelineReader exposes canonical timeline queries synchronized under MasterRing.mu.
	timelineReader timeline.TimelineReader
}

// Option configures an optional capability on a MasterRing.
type Option func(*MasterRing)

// WithCanonicalTimeline configures the ring to instantiate and exclusively own its
// canonical timeline.MediaIndex. Callers access the timeline strictly via
// MasterRing.Timeline() to guarantee synchronized visibility under MasterRing.mu.
// No raw pointer is retained by the caller, eliminating any potential bypass path.
func WithCanonicalTimeline() Option {
	return func(r *MasterRing) {
		r.timelineIndex = timeline.NewMediaIndex()
		r.timelineReader = &ringTimelineReader{ring: r}
	}
}

// NewMasterRing creates a new MasterRing with the specified capacity (aligned to 188 bytes).
func NewMasterRing(capacityBytes int) *MasterRing {
	return NewMasterRingWithProgram(capacityBytes, 0)
}

// NewMasterRingWithCore creates a new MasterRing using an explicitly supplied media facts core and options.
func NewMasterRingWithCore(capacityBytes int, core mediafacts.Core, opts ...Option) *MasterRing {
	capacityBytes = (capacityBytes / TSPacketSize) * TSPacketSize
	if capacityBytes < TSPacketSize*5 {
		capacityBytes = TSPacketSize * 5 // min 5 packets (~940 bytes)
	}

	r := &MasterRing{
		store:          newPacketStore(capacityBytes),
		attachIndex:    newAttachIndex(64),
		ingestDeadline: mediafacts.DefaultIngestDeadline,
		core:           core,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(r)
		}
	}
	r.notEmpty = sync.NewCond(&r.mu)
	return r
}

// NewMasterRingWithProgram creates a new MasterRing targeting a specific program number in multi-program PATs.
func NewMasterRingWithProgram(capacityBytes int, targetProgram uint16) *MasterRing {
	return NewMasterRingWithCore(capacityBytes, mediafacts.NewGoCore(targetProgram))
}

// SetTargetProgram configures the desired program number for PMT resolution,
// immediately invalidating existing PSI and decoder states if the target changed.
//
// Invariant: Control plane SetTargetProgram != transport plane Ingest.
// It never produces synthetic byte-positioned timeline history and does NOT
// invoke MediaCommit or MediaIndex.ApplyIngestResult.
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

	// Before anything else is read from it. What the ring publishes is the whole
	// of Facts, so a core that answers about part of the stream is refused here
	// rather than having the covered part taken and the rest read as zeroes.
	if !res.Covers(mediafacts.ParseCoverageComplete) {
		return r.retireCore(ctx, fmt.Errorf("%w: coverage %s", ErrCoreIncompleteResult, res.Coverage))
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

	// Last read of the context, on the far side of the lock wait. Same
	// linearization point as in Push, and the same reason for it.
	if err := ctx.Err(); err != nil {
		return r.retireCore(ctx, err)
	}
	if err := callCtx.Err(); err != nil {
		return r.retireCore(ctx, err)
	}
	if r.isClosed {
		r.coreUnusable = true
		return ErrRingClosed
	}

	// Control plane does NOT trigger MediaCommit or MediaIndex.ApplyIngestResult.
	// We only invalidate attachIndex if the core reported an actual EventProgramIdentityChanged.
	// A no-op (e.g. reselecting the same program) must not alter generation or keyframes.
	hasIdentityChange := false
	for _, ev := range res.Events {
		if ev.Kind == mediafacts.EventProgramIdentityChanged {
			hasIdentityChange = true
			break
		}
	}

	if hasIdentityChange {
		r.attachIndex.invalidateOnProgramChange(r.store.headOffset())
		r.notEmpty.Broadcast()
	}

	r.facts = res.Facts
	r.activePSI = res.PSI
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
	startOffset := r.store.headOffset()
	r.mu.Unlock()

	// Waiting for that lock is a wait like any other, and a caller can give up
	// during it. The core has still not been entered, so this is the same
	// situation as a cancellation before the call: nothing was interpreted,
	// nothing diverged, and the core is not retired.
	//
	// Without this the rule would quietly read "cancelled before some of the
	// pre-core locks leaves the core usable, cancelled while waiting for this one
	// retires it" - an edge that is invisible here and would be discovered across
	// a socket.
	if err := ctx.Err(); err != nil {
		return 0, err
	}

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
	// A core that answers about part of the stream is refused before its answer
	// is used for anything. See ErrCoreIncompleteResult: the fields it did not
	// fill are not empty, they are absent, and the ring cannot tell those apart
	// once it has committed them.
	if !res.Covers(mediafacts.ParseCoverageComplete) {
		return 0, r.retireCore(ctx, fmt.Errorf("%w: coverage %s", ErrCoreIncompleteResult, res.Coverage))
	}
	// A core that interpreted less than it was given leaves the ring with bytes it
	// has no meaning for. Committing them anyway is exactly the failure this
	// boundary exists to prevent, so the chunk is refused and nothing moves: not
	// the head, not the generation, not the index, not the facts. The core is
	// finished either way - it consumed what the ring is about to throw away.
	if want := startOffset + int64(n); res.ProcessedThroughOffset != want {
		return 0, r.retireCore(ctx, ErrCoreIncomplete)
	}

	// 3. Commit. Facts, events, PSI and bytes become visible together under r.mu (Publication Lock),
	//    or none of them do.
	commit := mediaCommit{
		StartOffset: startOffset,
		Data:        data,
		Result:      res,
	}

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
	//
	// The context is read once more here, under the lock, and that read is the
	// commit's linearization point: a cancellation visible by then wins, and one
	// that becomes visible after it does not.
	if err := ctx.Err(); err != nil {
		return 0, r.retireCore(ctx, err)
	}
	if err := ingestCtx.Err(); err != nil {
		return 0, r.retireCore(ctx, err)
	}
	if r.isClosed {
		return 0, r.retireCore(ctx, ErrRingClosed)
	}
	if r.store.headOffset() != commit.StartOffset {
		return 0, r.retireCore(ctx, ErrRingAdvanced)
	}

	// Step A: Timeline index update.
	// Fail-Closed: If timelineIndex rejects the result, fail closed without mutating packetStore or attachIndex.
	if r.timelineIndex != nil {
		if err := r.timelineIndex.ApplyIngestResult(commit.Result); err != nil {
			return 0, r.retireCore(ctx, err)
		}
	}

	// Step B: Derived join cache update.
	genBefore := r.attachIndex.generationValue()
	genChanged := r.attachIndex.applyEvents(commit.Result.Events)

	// Step C: Infallible byte commit into packetStore.
	newHead, newTail := r.store.writeCommitted(commit.Data)

	// Step D: Pruning on new tail.
	r.attachIndex.pruneBefore(newTail)
	if r.timelineIndex != nil {
		r.timelineIndex.PruneBefore(newTail)
	}

	if genChanged || r.attachIndex.generationValue() != genBefore {
		r.attachIndex.setGenerationResumeFloor(newHead)
	}

	// Step E: Publish facts and active PSI.
	r.facts = commit.Result.Facts
	r.activePSI = commit.Result.PSI

	// Step F: Wake blocked readers.
	r.notEmpty.Broadcast()
	return n, nil
}

// RandomAccess returns how access units have been classified on this stream.
func (r *MasterRing) RandomAccess() RandomAccessObservation {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.facts.RandomAccess
}

// Timeline returns the canonical read-only timeline reader, or nil if this ring
// does not maintain a timeline index (e.g. variant or non-canonical rings).
//
// Invariant: The returned TimelineReader synchronizes all queries under MasterRing.mu,
// establishing MasterRing.mu as the single shared publication lock across timeline truth
// and packet store bytes.
//
// To prevent Go typed-nil interface bugs where an interface variable containing a
// nil pointer is not equal to nil, this returns an explicit untyped nil when
// r.timelineIndex is nil.
func (r *MasterRing) Timeline() timeline.TimelineReader {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.timelineIndex == nil {
		return nil
	}
	if r.timelineReader == nil {
		r.timelineReader = &ringTimelineReader{ring: r}
	}
	return r.timelineReader
}

// ReadAt reads up to len(p) bytes from the master ring starting at offset under the publication lock r.mu.
// It returns ErrSubscriberOverrun if offset < tail, or reads available bytes up to head.
func (r *MasterRing) ReadAt(p []byte, offset int64) (int, int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.store.readAt(p, offset)
}

// TimelineObservation captures an atomic snapshot of timeline state and packet store bounds under a single lock.
type TimelineObservation struct {
	Tail        int64
	Head        int64
	ActiveEpoch mediafacts.TimelineEpoch
	HasEpoch    bool
	LatestRAP   timeline.RAPEntry
	HasLatest   bool
	RAPCount    int
}

// TimelineObservation returns an atomic snapshot of the current stream bounds and timeline state
// under a single acquisition of the publication lock MasterRing.mu.
func (r *MasterRing) TimelineObservation() (TimelineObservation, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.timelineIndex == nil {
		return TimelineObservation{}, false
	}
	obs := TimelineObservation{
		Tail: r.store.tailOffset(),
		Head: r.store.headOffset(),
	}
	obs.ActiveEpoch, obs.HasEpoch = r.timelineIndex.ActiveEpoch()
	obs.LatestRAP, obs.HasLatest = r.timelineIndex.FindPrecedingRAP(obs.Head)
	if obs.HasLatest && (obs.LatestRAP.Offset < obs.Tail || obs.LatestRAP.Offset >= obs.Head) {
		obs.HasLatest = false
	}
	obs.RAPCount = r.timelineIndex.Stats().TotalRAPs
	return obs, true
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

	preamble := r.patpmtPreambleLocked()
	kfOffset, hasKf := r.attachIndex.latestKeyframeOffset(r.store.tailOffset())

	return PrimedAttachPoint{
		Preamble:       preamble,
		KeyframeOffset: kfOffset,
		Generation:     r.attachIndex.generationValue(),
		HasKeyframe:    hasKf,
	}
}

// LatestKeyframeOffset returns the absolute byte offset of the most recent valid keyframe.
func (r *MasterRing) LatestKeyframeOffset() (int64, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.latestKeyframeOffsetLocked()
}

// SeekResult captures the resolved entry point and metadata from a time seek.
type SeekResult struct {
	Offset     int64
	RAP        timeline.RAPEntry
	Generation uint64
	Preamble   []byte
}

// SeekToTime resolves a decodable stream entry point for epoch and PTS under the publication lock.
// It unconditionally enforces Joinable == true, HasPMT == true, non-empty preamble, and Offset >= resumeFloor.
func (r *MasterRing) SeekToTime(epoch mediafacts.TimelineEpoch, pts int64, mode timeline.SeekMode) (SeekResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.seekToTimeLocked(epoch, pts, mode)
}

func (r *MasterRing) seekToTimeLocked(epoch mediafacts.TimelineEpoch, pts int64, mode timeline.SeekMode) (SeekResult, error) {
	if r.isClosed {
		return SeekResult{}, ErrRingClosed
	}
	if r.timelineIndex == nil {
		return SeekResult{}, ErrNoTimeline
	}

	if !r.facts.HasPMT {
		return SeekResult{}, ErrTopologyUnresolved
	}
	preamble := r.patpmtPreambleLocked()
	if len(preamble) == 0 {
		return SeekResult{}, ErrTopologyUnresolved
	}

	tail := r.store.tailOffset()
	head := r.store.headOffset()

	rap, ok := r.timelineIndex.FindRAPByTime(epoch, pts, timeline.SeekOptions{
		Mode:         mode,
		JoinableOnly: true,
	})
	if !ok {
		// Distinguish out-of-range PTS from general no RAP
		raps := r.timelineIndex.RAPsBetween(tail, head-1)
		var earliestPTS, latestPTS int64
		var hasAny bool
		for _, rapEntry := range raps {
			if rapEntry.Epoch == epoch && rapEntry.HasPTS {
				if !hasAny {
					hasAny = true
					earliestPTS = rapEntry.PTS90k
					latestPTS = rapEntry.PTS90k
				} else {
					if rapEntry.PTS90k < earliestPTS {
						earliestPTS = rapEntry.PTS90k
					}
					if rapEntry.PTS90k > latestPTS {
						latestPTS = rapEntry.PTS90k
					}
				}
			}
		}
		if hasAny && (pts < earliestPTS || pts > latestPTS) {
			return SeekResult{}, ErrPTSOutOfRange
		}
		return SeekResult{}, ErrNoMatchingRAP
	}

	if rap.Offset < tail || rap.Offset >= head {
		return SeekResult{}, ErrNoMatchingRAP
	}

	floor := r.attachIndex.resumeFloor()
	if rap.Offset < floor {
		return SeekResult{}, ErrHistoricalProgramSeekUnsupported
	}

	return SeekResult{
		Offset:     rap.Offset,
		RAP:        rap,
		Generation: r.attachIndex.generationValue(),
		Preamble:   preamble,
	}, nil
}

// NewPrimedSubscriberAtTime atomically creates and positions a SubscriberReader at the given epoch and PTS.
// It primes the reader with the active PAT/PMT preamble and clears overrun/resync state.
// It unconditionally enforces Joinable == true, HasPMT == true, non-empty preamble, and Offset >= resumeFloor.
func (r *MasterRing) NewPrimedSubscriberAtTime(epoch mediafacts.TimelineEpoch, pts int64, mode timeline.SeekMode) (PrimedAttachPoint, *SubscriberReader, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	seekRes, err := r.seekToTimeLocked(epoch, pts, mode)
	if err != nil {
		return PrimedAttachPoint{}, nil, err
	}

	attach := PrimedAttachPoint{
		Preamble:       seekRes.Preamble,
		KeyframeOffset: seekRes.Offset,
		Generation:     seekRes.Generation,
		HasKeyframe:    true,
	}

	reader := r.newSubscriberReaderLocked(seekRes.Offset)
	reader.pendingPrefix = seekRes.Preamble
	reader.pendingPrefixGeneration = seekRes.Generation
	reader.awaitingRandomAccess = false

	return attach, reader, nil
}

// latestKeyframeOffsetLocked reports the newest random access point still held by
// the ring. A keyframe that has fallen behind the tail is gone even though its
// offset is still indexed, so it is not a valid entry point.
//
// Callers that already hold r.mu use this; the exported wrappers must not, because
// r.mu is not reentrant and SubscriberReader.Read holds it across recovery.
func (r *MasterRing) latestKeyframeOffsetLocked() (int64, bool) {
	return r.attachIndex.latestKeyframeOffset(r.store.tailOffset())
}

// PATPMTPreamble returns the active PAT and PMT, packetized for delivery.
func (r *MasterRing) PATPMTPreamble() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.patpmtPreambleLocked()
}

// patpmtPreambleLocked builds the active topology preamble for callers already
// holding r.mu. See latestKeyframeOffsetLocked for why the split exists.
func (r *MasterRing) patpmtPreambleLocked() []byte {
	preamble := packetizePSISections(patPID, r.activePSI.PATSections)
	// The PMT's PID is the one the PAT named for this program. Without it there
	// is no PID to put the sections on, and PID 0 - the zero value - is the PAT's
	// own; emitting them there would deliver a PMT as if it were a PAT.
	if r.facts.PMTPID != patPID {
		preamble = append(preamble, packetizePSISections(r.facts.PMTPID, r.activePSI.PMTSections)...)
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
	return r.attachIndex.generationValue()
}

// GenerationResumeFloor returns the conservative recovery floor byte offset
// established by the latest committed identity change.
func (r *MasterRing) GenerationResumeFloor() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.attachIndex.resumeFloor()
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
	return r.store.headOffset()
}

// Tail returns the oldest valid byte offset.
func (r *MasterRing) Tail() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.store.tailOffset()
}

// BufferedBytes returns total valid unpruned bytes in the ring.
func (r *MasterRing) BufferedBytes() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.store.bufferedBytes()
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
	return r.attachIndex.keyframeOffsetsCopy()
}

// ReadinessFacts is everything the ring knows that bears on whether a channel is
// presentable. It is a snapshot, taken without blocking the ingest.
type ReadinessFacts struct {
	Generation uint64

	HasPAT        bool
	HasPMT        bool
	PMTVersion    uint8
	ProgramNumber uint16

	VideoPID    uint16
	VideoCodec  VideoCodec
	AudioPIDs   []uint16
	AudioTracks []AudioTrackInfo

	ParameterSetsSeen bool

	RandomAccess RandomAccessObservation
	Scrambling   StreamScrambling

	CleanEntryPoints uint64
	CleanAccessUnits uint64
	AttachAvailable  bool
}

// ReadinessFacts captures what the ring currently knows about this stream.
func (r *MasterRing) ReadinessFacts() ReadinessFacts {
	r.mu.Lock()
	defer r.mu.Unlock()

	f := r.facts
	f.AudioPIDs = append([]uint16(nil), f.AudioPIDs...)
	f.AudioTracks = append([]AudioTrackInfo(nil), f.AudioTracks...)
	f.Scrambling.AudioPIDs = append([]uint16(nil), f.Scrambling.AudioPIDs...)

	_, attach := r.attachIndex.latestKeyframeOffset(r.store.tailOffset())

	return ReadinessFacts{
		Generation:        r.attachIndex.generationValue(),
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
