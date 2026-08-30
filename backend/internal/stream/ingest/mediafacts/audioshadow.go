// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package mediafacts

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/ManuGH/xg2g/internal/stream/ingest/esaudio"
)

// AudioShadowBatch is one ordered run of feeds for one elementary stream under
// one epoch.
//
// Feeds keeps the boundaries the core's own observer was fed at. They are not a
// detail of how the bytes arrived: an observer carries a partial header across a
// call and skips frame payload that ran past the end of one, so the same bytes
// concatenated would be a different input and agreement on it would prove less.
//
// The bytes a shadow receives are the shadow's own. They are copied out of the
// core's chunk before the batch leaves the ingest call, because the chunk does
// not outlive that call and the shadow may run long after it.
type AudioShadowBatch struct {
	PID   uint16
	Epoch uint64
	Feeds [][]byte
}

// AudioShadowObservation is what a shadow made of one stream under one epoch.
type AudioShadowObservation struct {
	PID         uint16
	Epoch       uint64
	Observation esaudio.Observation
}

// AudioShadow is a second observer of the same elementary stream bytes.
//
// It is given every batch from one chunk, in the order they happened, and
// answers what it made of them. It is asked, never obeyed: nothing it returns
// reaches a fact, an event or the control flow of the core.
//
// It is also never waited for by the ingest path. Calls arrive on a worker of
// the core's own, one chunk at a time and never concurrently, and however long
// one takes is the worker's problem alone - the ingest that produced the batches
// has long returned.
//
// One thing is required rather than tolerated:
//
//	ObserveAudio MUST return when ctx is cancelled.
//
// This is the only lever anything has over a call in progress. Go cannot end a
// goroutine sitting inside an interface method from the outside, so an
// implementation that ignores its context cannot be shut down at all - not
// bounded, not eventually, not by anyone. Everything below that says "the worker
// ends" says it on the strength of this line.
//
// An implementation that reaches another process owns enforcing it locally, and
// may not assume the peer cooperates: a sidecar that never replies, or that
// ignores a cancellation of its own, must still not keep ObserveAudio from
// returning. That is a socket deadline and a connection this side can tear down,
// not a request the far end is trusted to honour. The RemoteAudioShadow of 4b.2b
// is where that has to be proven; nothing here can prove it for it.
type AudioShadow interface {
	ObserveAudio(ctx context.Context, batches []AudioShadowBatch) ([]AudioShadowObservation, error)
}

// AudioShadowMismatch is one stream the two observers disagree about.
type AudioShadowMismatch struct {
	PID       uint16
	Epoch     uint64
	Fields    []string
	Reference esaudio.Observation
	Shadow    esaudio.Observation
}

// AudioShadowReport is what the shadow has been doing.
//
// Counters rather than a log: at a chunk every few seconds per session, a line
// per comparison is not something anyone reads, and a count that stops rising is
// something a dashboard can see.
type AudioShadowReport struct {
	// Batches is how many stream-and-epoch runs have been accepted for
	// comparison. Accepted, not handed over: a chunk that arrived at a full queue
	// is not in here, and is the last thing that happens before Disabled.
	//
	// Counted before the work carrying it is put where the worker can reach it,
	// which is what keeps Compared from ever running ahead of it.
	Batches uint64
	// Compared is how many observations were held against the core's own answer.
	// Every batch has one, including a batch from an epoch that ended inside the
	// chunk: the reference is frozen with the batch, so an epoch the core has
	// left is still compared rather than skipped.
	Compared uint64
	// Mismatches counts comparisons that disagreed about anything.
	Mismatches uint64
	// Errors counts why the comparison stopped, and stops at one. A shadow that
	// failed and a queue that filled up behind it are one retirement, not two.
	Errors uint64
	// Disabled reports that the shadow has been retired and will not be called
	// again.
	Disabled bool
	// Recent holds the last few mismatches, oldest first.
	Recent []AudioShadowMismatch
}

// recentMismatches is how many disagreements are kept. Enough to see a pattern,
// few enough that a stream disagreeing about every chunk cannot grow memory.
const recentMismatches = 8

// audioShadowQueueDepth is how many chunks may be waiting for the shadow.
//
// Small on purpose. A shadow that is a few chunks behind is a shadow that will
// not catch up - chunks arrive for as long as the session runs - and a deep
// queue would only delay noticing that, while holding a copy of every chunk's
// audio in the meantime.
//
// What that costs, exactly, because "bounded" on its own is not a number.
//
// A logical payload-and-metadata upper bound, and deliberately not called a heap
// or RSS ceiling: every term below is a structure this code builds, and none of
// them accounts for what the allocator rounds a size class up to, or for the
// capacity an append leaves behind where a builder does not pre-size exactly.
//
//	largest chunk C = the normalizer's staging capacity, 4 MiB by default;
//	                  it is what one egress tick can hand the ring
//	max feeds      <= C/188, one per transport packet
//	max batches    <= C/188 as well, because every batch needs at least one feed:
//	                  a chunk alternating between streams is the worst case, and
//	                  the per-batch term is not a rounding error against the
//	                  payload the way a per-PMT bound would be
//
//	per work item, worst case:
//	  ES bytes copied    184/188 * C
//	  feed headers        24/188 * C   (24 bytes per []byte)
//	  batch + reference   64/188 * C   (40 + 24 bytes per stream-and-epoch)
//	                    ------------
//	                    272/188 * C  = 1.447 * C = 5.79 MiB at C = 4 MiB
//
//	live work items:
//	  retained by the runner  5 = audioShadowQueueDepth + one in the worker's
//	                              hand                       = 28.9 MiB
//	  peak                    6 = the above plus the one the producer is
//	                              assembling before the offer = 34.7 MiB
//
// Beside that, and only for the duration of one Ingest, the capture side holds
// c.shadowBatches: 64 bytes per batch and 24 per feed, up to 88/188 * C of
// metadata again - and no payload, because those feeds point into the caller's
// chunk instead of copying it.
//
// Retained, not allocated: the allocation volume of a run is larger and is a
// different question. And reaching any of this needs a stream that is audio and
// nothing else with the worker fully stalled - at which point the next chunk
// retires the comparison and all of it becomes garbage.
const audioShadowQueueDepth = 4

// pendingAudioShadowBatch is a batch and the core's own answer to it.
//
// The reference is frozen here rather than read back later, because "later" may
// be after the observer that produced it has been thrown away: a PMT change can
// end an epoch half way through a chunk, and the batch from before it is exactly
// the one a PID-reuse bug would show up in.
//
// The reference never leaves this process. It travels with the work item to the
// worker; a shadow is asked what it made of the bytes, not asked to agree with
// an answer it was given.
type pendingAudioShadowBatch struct {
	Batch     AudioShadowBatch
	Reference esaudio.Observation
}

// audioShadowWork is one chunk's worth of comparison, owning its bytes.
type audioShadowWork struct {
	batches    []AudioShadowBatch
	references []esaudio.Observation
}

// audioShadowRunner is the shadow, its queue and the one goroutine that drives
// it.
//
// The whole point of this type is that nothing in it is on the authoritative
// path. Ingest copies the audio it captured, offers it to the queue without
// waiting, and returns; whether the shadow is fast, slow, hung or broken can
// change how much of the comparison happens, and nothing else.
//
// Which is why the counters are atomics rather than fields of a report behind
// the mutex. The mutex is held while the worker records a disagreement, and an
// ingest that waits on it - however briefly - has the worker's speed back inside
// its own duration. What is left under the mutex is the mismatch ring alone,
// which the ingest path never touches.
//
// The runner owns a context for the shadow's whole lifetime, and every end of
// the comparison cancels it. That is what lets the producer side finish a
// retirement without waiting for anything: cancelling is not a handshake, and
// the worker is woken by its own context rather than by anybody watching it.
type audioShadowRunner struct {
	shadow AudioShadow
	queue  chan audioShadowWork

	batches    atomic.Uint64
	compared   atomic.Uint64
	mismatches atomic.Uint64
	errors     atomic.Uint64

	// retired is read on the capture path for every audio packet of every chunk,
	// which is why it is an atomic and not something the lock below has to be
	// taken for.
	retired atomic.Bool

	// lifecycle pairs the two things that must be one decision: putting work in
	// the queue, and ending the comparison. Checking a flag and then sending are
	// two moments, and a producer that passed the check before a retirement would
	// otherwise land a chunk's audio in a channel nobody will read again.
	//
	// It is worth naming for what it is not. It is never held across ObserveAudio,
	// a comparison, or a mismatch being recorded, and it never nests with mu. The
	// longest a producer can wait on it is one channel send and at most
	// audioShadowQueueDepth non-blocking receives - local instructions, never the
	// speed of the shadow.
	lifecycle sync.Mutex

	mu     sync.Mutex
	recent []AudioShadowMismatch

	// cancel ends the shadow's context. Non-blocking, callable from anywhere, and
	// the only thing that can reach a call already in progress.
	cancel context.CancelFunc
	// done is closed when the worker goroutine has ended. Close waits on it, so
	// that "the comparison is over" and "the goroutine is gone" are the same
	// moment for every shadow that keeps the contract above.
	done chan struct{}
}

func newAudioShadowRunner(shadow AudioShadow) *audioShadowRunner {
	ctx, cancel := context.WithCancel(context.Background())
	r := &audioShadowRunner{
		shadow: shadow,
		queue:  make(chan audioShadowWork, audioShadowQueueDepth),
		cancel: cancel,
		done:   make(chan struct{}),
	}
	go r.run(ctx)
	return r
}

// Close ends the comparison and does not come back until the worker is gone.
//
// Cancel, then wait. The wait is what makes this a shutdown rather than an
// announcement: without it Close would return while a goroutine of ours was
// still inside somebody else's code, and "no leaked workers" would be a hope
// instead of a postcondition.
//
// Bounded for every shadow that keeps the AudioShadow contract, and for no
// other. One that never returns from a cancelled ObserveAudio hangs this call -
// deliberately, because the alternative is to walk away from a goroutine that
// cannot be accounted for and call it clean. The hang names the shadow that
// broke the contract; the walk-away would name nothing.
//
// It is not on the ingest path and never becomes one: retiring the comparison
// from a full queue cancels, and does not wait.
func (r *audioShadowRunner) Close() {
	// Through the same door as a retirement, so a shutdown does not also get
	// counted as a failure - and so a shadow erroring out on its way to the exit
	// does not add one either. Whatever ended it first is what the report says.
	r.lifecycle.Lock()
	r.retireLocked(false)
	r.lifecycle.Unlock()
	// Waited for with the lock released. The worker takes it to retire itself,
	// and a Close holding it while waiting for that worker to finish would be
	// waiting for something it had made impossible.
	<-r.done
}

func (r *audioShadowRunner) run(ctx context.Context) {
	defer close(r.done)
	for {
		// Checked before the select rather than left to it. Draining is not this
		// loop's job: the retirement path empties the queue under lifecycle, and no
		// later offer can refill it. So by the time this is visible, nothing queued
		// has a semantic future and nothing is left to collect - the worker's only
		// remaining job is to stop being a goroutine.
		if r.retired.Load() {
			return
		}
		select {
		case <-ctx.Done():
			return
		case work := <-r.queue:
			r.process(ctx, work)
		}
	}
}

// offer hands one chunk's work to the queue without ever waiting for room.
//
// A full queue is not a batch to drop and carry on from. The shadow's observers
// are stateful, so a missing batch makes every later one a comparison against a
// stream the shadow never saw - agreement and disagreement would both stop
// meaning anything. So the queue filling up ends the comparison instead.
//
// The send is under the lifecycle lock, and so is every retirement it can race
// with. An unlocked check followed by an unlocked send is two moments: a
// producer can pass "not retired yet", a worker can retire and empty the queue
// in between, and the send then lands in a channel that will never be read -
// one chunk's owned audio, held for as long as the core holds the runner.
//
// Room is looked for rather than found out about, which is what a send inside a
// select cannot do: there is no point between choosing the send and performing
// it where the count could be published, and a count published after the work is
// a count the worker's own can already have run past.
func (r *audioShadowRunner) offer(work audioShadowWork) {
	if r.retired.Load() {
		return
	}
	r.lifecycle.Lock()
	defer r.lifecycle.Unlock()
	// Again, now that the answer cannot change underneath the send.
	if r.retired.Load() {
		return
	}
	// The room seen here is still there below. Offers are the only senders and
	// this lock serialises them; the one thing that empties the queue behind a
	// producer's back is a retirement, and that is under this lock too. What is
	// left is a worker that only ever receives - so between the check and the
	// send the queue can grow emptier, never fuller, and the send cannot block.
	if len(r.queue) == cap(r.queue) {
		// Still holding the lock. The retirement a full queue causes has to be
		// the same moment as the check, or the queue it empties could be refilled
		// by the offer immediately behind it.
		r.retireLocked(true)
		return
	}
	r.publishLocked(work)
}

// publishLocked counts the work and then hands it over, in that order.
//
// The order is the whole of it, which is why it is a function and not two
// statements at the end of offer. Batches is what the report says was accepted,
// and the worker starts raising Compared the moment it can reach the work - so a
// count published after the send is a count that a comparison of the very work
// it has not counted yet can already be ahead of, and the report would say more
// comparisons happened than there was work to compare.
//
// The caller holds lifecycle and has established that the queue has room. The
// send is unconditional and must not be reached without that.
func (r *audioShadowRunner) publishLocked(work audioShadowWork) {
	r.batches.Add(uint64(len(work.batches)))
	r.queue <- work
}

// retire ends the comparison for good, once, whichever side noticed first.
func (r *audioShadowRunner) retire() {
	r.lifecycle.Lock()
	defer r.lifecycle.Unlock()
	r.retireLocked(true)
}

// retireLocked is the one state transition there is, and the only place the
// queue is emptied. The caller holds lifecycle.
//
// Once, because Errors is not "how many things went wrong" but "the comparison
// stopped, and this is the reason": a shadow that failed while the queue behind
// it filled up is one retirement. counted is false for a Close, which ends the
// comparison without anything having gone wrong. The error is counted before the
// flag is raised, so anything that sees the comparison stopped also sees why.
//
// It cancels. A retirement decided on the producer side - a full queue - must
// reach a worker that is already inside a call, or the comparison would be over
// while the goroutine ran on. Cancelling is how it reaches it, and cancelling
// never blocks, which is why this is safe to reach from Ingest.
//
// And it empties the queue, which is not the same thing as dropping a batch. A
// dropped batch would be a hole in a stateful comparison that carries on either
// side of it; this is the comparison ending, after which those work items have
// no future at all. Nothing will ever hold them against a reference. Left in the
// channel they would keep several chunks of copied audio reachable for as long
// as the core keeps the runner - so the worse the shadow behaved, the more
// memory its corpse would hold.
func (r *audioShadowRunner) retireLocked(counted bool) {
	if r.retired.Load() {
		return
	}
	if counted {
		r.errors.Add(1)
	}
	r.retired.Store(true)
	r.cancel()
	for {
		select {
		case <-r.queue:
		default:
			return
		}
	}
}

// snapshot reads the counters in the reverse of the order the worker writes
// them, which is the difference between a report that lags and one that lies.
//
// The worker writes batches, then compared, then mismatches, then the ring; and
// errors, then the retired flag. Reading in that same order lets a snapshot
// catch the second write of a pair without the first: Disabled true beside
// Errors zero, which the retirement comment says cannot happen, or a mismatch
// counted with an empty ring beside it. Reading backwards inverts that. Seeing
// the later write now implies seeing the earlier one, so a field can only be
// behind, never ahead of, the fields written before it.
//
// The statements are separate on purpose. As a struct literal the order is real
// but invisible, and the next edit would reorder the fields for tidiness and
// quietly take the guarantee away.
//
// What this does not do is make any two fields consistent for a reader that
// wants them to be: a caller polling one field and asserting on another still
// has to ask for both in the same snapshot. See awaitShadow in the tests.
func (r *audioShadowRunner) snapshot() AudioShadowReport {
	r.mu.Lock()
	recent := append([]AudioShadowMismatch(nil), r.recent...)
	r.mu.Unlock()

	disabled := r.retired.Load()
	mismatches := r.mismatches.Load()
	compared := r.compared.Load()
	batches := r.batches.Load()
	errors := r.errors.Load()

	return AudioShadowReport{
		Batches:    batches,
		Compared:   compared,
		Mismatches: mismatches,
		Errors:     errors,
		Disabled:   disabled,
		Recent:     recent,
	}
}

// process runs one chunk's comparison.
func (r *audioShadowRunner) process(ctx context.Context, work audioShadowWork) {
	if r.retired.Load() {
		return
	}
	observations, err := r.shadow.ObserveAudio(ctx, work.batches)
	if err != nil {
		r.retire()
		return
	}

	// The answer is checked before any of it is believed. An answer that is
	// missing, extra, reordered or labelled with somebody else's stream is not a
	// smaller comparison - it is a shadow that has lost track of what it was
	// asked, and every remaining number it produces is about an unknown stream.
	if len(observations) != len(work.batches) {
		r.retire()
		return
	}
	for i := range observations {
		if observations[i].PID != work.batches[i].PID || observations[i].Epoch != work.batches[i].Epoch {
			r.retire()
			return
		}
	}

	for i, o := range observations {
		reference := work.references[i]
		r.compared.Add(1)
		fields := esaudio.Compare(reference, o.Observation)
		if len(fields) == 0 {
			continue
		}
		r.mismatches.Add(1)
		r.recordMismatch(AudioShadowMismatch{
			PID:       o.PID,
			Epoch:     o.Epoch,
			Fields:    fields,
			Reference: reference,
			Shadow:    o.Observation,
		})
	}
}

func (r *audioShadowRunner) recordMismatch(m AudioShadowMismatch) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recent = append(r.recent, m)
	if len(r.recent) > recentMismatches {
		r.recent = r.recent[len(r.recent)-recentMismatches:]
	}
}

// SetAudioShadow attaches a second observer to this core, replacing any before
// it.
//
// Deliberately not part of the Core interface and not on the constructor: a
// shadow is something an operator turns on for a migration, not part of what a
// core is. A core without one behaves exactly as it did before this existed,
// down to not capturing the feeds.
//
// It starts a goroutine, so whoever attaches a shadow owns ending it:
// CloseAudioShadow, or another SetAudioShadow, or SetAudioShadow(nil).
func (c *GoCore) SetAudioShadow(shadow AudioShadow) {
	c.CloseAudioShadow()
	c.clearAudioShadowBatches()
	if shadow == nil {
		return
	}
	c.shadowRunner = newAudioShadowRunner(shadow)
}

// CloseAudioShadow ends the comparison and the goroutine behind it.
//
// It waits for the worker to have terminated, and is bounded by the AudioShadow
// cancellation contract and by nothing else: it never waits for a shadow to
// finish work it has not been cancelled out of. See (*audioShadowRunner).Close.
func (c *GoCore) CloseAudioShadow() {
	if c.shadowRunner != nil {
		c.shadowRunner.Close()
		c.shadowRunner = nil
	}
}

// AudioShadowReport returns what the shadow has been doing so far.
//
// Safe to call while the worker is running: the counters are the worker's and
// this is a copy of them.
func (c *GoCore) AudioShadowReport() AudioShadowReport {
	if c.shadowRunner == nil {
		return AudioShadowReport{}
	}
	return c.shadowRunner.snapshot()
}

// captureAudioShadowFeedLocked records one feed exactly as the core's own
// observer received it.
//
// Batches are keyed by stream and epoch, and a new epoch closes the ones before
// it: a PMT change can happen half way through a chunk, and the feeds either
// side of it belong to different elementary streams that merely share a number.
// Folding them together would hand the shadow a stream that never existed.
func (c *GoCore) captureAudioShadowFeedLocked(pid uint16, es []byte, observer *esaudio.Observer) {
	if c.shadowRunner == nil || c.shadowRunner.retired.Load() || len(es) == 0 {
		return
	}
	for i := len(c.shadowBatches) - 1; i >= 0; i-- {
		b := &c.shadowBatches[i]
		if b.Batch.Epoch != c.shadowEpoch {
			// Everything from here back belongs to an epoch that has ended.
			break
		}
		if b.Batch.PID == pid {
			b.Batch.Feeds = append(b.Batch.Feeds, es)
			// Kept level with the feeds: after the last one of a batch this is the
			// core's answer to exactly those bytes, whatever happens to the observer
			// afterwards.
			b.Reference = observer.Current()
			return
		}
	}
	c.shadowBatches = append(c.shadowBatches, pendingAudioShadowBatch{
		Batch: AudioShadowBatch{
			PID:   pid,
			Epoch: c.shadowEpoch,
			Feeds: [][]byte{es},
		},
		Reference: observer.Current(),
	})
}

// clearAudioShadowBatches drops the captured feeds and the references to them.
//
// clear before the reslice, not instead of it. Reslicing to zero length leaves
// the elements in the backing array untouched, and those elements hold slices
// into the chunk that has just been interpreted - so a core that reslices and
// nothing more keeps the last chunk it saw reachable, and one whose shadow has
// since been retired keeps it for good, because nothing will ever overwrite
// those slots again.
func (c *GoCore) clearAudioShadowBatches() {
	clear(c.shadowBatches)
	c.shadowBatches = c.shadowBatches[:0]
}

// handOffAudioShadow copies the chunk's audio and offers it to the worker.
//
// This is the only part of the comparison that is still on the authoritative
// path, and it is the part that has to be: the captured feeds point into the
// chunk, and the chunk is gone the moment Ingest returns. So the bytes are
// copied here, once, into a single buffer per chunk - the feed boundaries are
// preserved as offsets into it, because those boundaries are the input.
//
// Every feed is capped at its own end. A shadow that appends to one of them gets
// a copy rather than the next feed's first bytes, which is the kind of thing
// that would otherwise show up as the other implementation disagreeing.
//
// Then the work is offered without waiting. Nothing after this line is allowed
// to affect how long this call takes.
func (c *GoCore) handOffAudioShadow() {
	if c.shadowRunner == nil || c.shadowRunner.retired.Load() || len(c.shadowBatches) == 0 {
		return
	}

	total := 0
	for i := range c.shadowBatches {
		for _, f := range c.shadowBatches[i].Batch.Feeds {
			total += len(f)
		}
	}
	owned := make([]byte, 0, total)

	work := audioShadowWork{
		batches:    make([]AudioShadowBatch, len(c.shadowBatches)),
		references: make([]esaudio.Observation, len(c.shadowBatches)),
	}
	for i := range c.shadowBatches {
		src := c.shadowBatches[i].Batch
		feeds := make([][]byte, len(src.Feeds))
		for j, f := range src.Feeds {
			start := len(owned)
			owned = append(owned, f...)
			feeds[j] = owned[start:len(owned):len(owned)]
		}
		work.batches[i] = AudioShadowBatch{PID: src.PID, Epoch: src.Epoch, Feeds: feeds}
		work.references[i] = c.shadowBatches[i].Reference
	}
	c.shadowRunner.offer(work)
}
