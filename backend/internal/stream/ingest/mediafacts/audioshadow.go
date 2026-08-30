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
// It is also never waited for. Calls arrive on a worker of the core's own, one
// chunk at a time and never concurrently, and however long one takes is the
// worker's problem alone - the ingest that produced the batches has long
// returned.
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
type audioShadowRunner struct {
	shadow AudioShadow
	queue  chan audioShadowWork

	batches    atomic.Uint64
	compared   atomic.Uint64
	mismatches atomic.Uint64
	errors     atomic.Uint64

	// retired is read on the capture path for every audio packet of every chunk,
	// which is the other reason none of this is behind the mutex.
	retired    atomic.Bool
	retireOnce sync.Once

	mu     sync.Mutex
	recent []AudioShadowMismatch

	cancel   context.CancelFunc
	stopOnce sync.Once
	stop     chan struct{}
	// done is closed when the worker goroutine has actually ended, which is not
	// the moment Close returns: a shadow that ignores the context it was given
	// holds the goroutine until it comes back. Nothing on the ingest path waits
	// for this - it is how a test can tell "the caller was let go" apart from
	// "the goroutine ended".
	done chan struct{}
}

func newAudioShadowRunner(shadow AudioShadow) *audioShadowRunner {
	ctx, cancel := context.WithCancel(context.Background())
	r := &audioShadowRunner{
		shadow: shadow,
		queue:  make(chan audioShadowWork, audioShadowQueueDepth),
		cancel: cancel,
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
	}
	go r.run(ctx)
	return r
}

// Close stops the runner without waiting for it.
//
// Bounded by construction: it signals and returns. A shadow that respects the
// context it was given comes back at once and the goroutine ends; one that
// ignores it ends the goroutine whenever it finally returns - and until then it
// is a goroutine sitting in somebody else's code, not a caller being held up.
func (r *audioShadowRunner) Close() {
	r.stopOnce.Do(func() {
		r.cancel()
		close(r.stop)
	})
}

func (r *audioShadowRunner) run(ctx context.Context) {
	defer close(r.done)
	for {
		select {
		case <-r.stop:
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
func (r *audioShadowRunner) offer(work audioShadowWork) {
	if r.retired.Load() {
		return
	}
	select {
	case r.queue <- work:
		r.batches.Add(uint64(len(work.batches)))
	default:
		r.retire()
	}
}

// retire ends the comparison for good, once, whichever side noticed first.
//
// Once, because Errors is not "how many things went wrong" but "the comparison
// stopped, and this is the reason": a shadow that failed while the queue behind
// it filled up is one retirement. The error is counted before the flag is
// raised, so anything that sees the comparison stopped also sees why.
func (r *audioShadowRunner) retire() {
	r.retireOnce.Do(func() {
		r.errors.Add(1)
		r.retired.Store(true)
	})
}

func (r *audioShadowRunner) snapshot() AudioShadowReport {
	out := AudioShadowReport{
		Batches:    r.batches.Load(),
		Compared:   r.compared.Load(),
		Mismatches: r.mismatches.Load(),
		Errors:     r.errors.Load(),
		Disabled:   r.retired.Load(),
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out.Recent = append([]AudioShadowMismatch(nil), r.recent...)
	return out
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
// Bounded: it does not wait for the shadow to come back. See
// (*audioShadowRunner).Close.
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
