// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package mediafacts

import (
	"context"

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
// The slices point into the chunk the core was handed. They are valid for the
// duration of the call that produced them and must not be retained past it.
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
// reaches a fact, an event or the control flow of the core. What it is for is
// the comparison.
type AudioShadow interface {
	// ObserveAudio is called once per chunk that produced any audio, after the
	// chunk has been interpreted. An error retires the shadow for the lifetime of
	// the core - see (*GoCore).runAudioShadow for why there is no second chance.
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
	// Batches is how many stream-and-epoch runs have been handed over.
	Batches uint64
	// Compared is how many observations were held against the core's own answer.
	// Every batch has one, including a batch from an epoch that ended inside the
	// chunk: the reference is frozen with the batch, so an epoch the core has
	// left is still compared rather than skipped.
	Compared uint64
	// Mismatches counts comparisons that disagreed about anything.
	Mismatches uint64
	// Errors counts failures of the shadow itself.
	Errors uint64
	// Disabled reports that the shadow has been retired and will not be called
	// again.
	Disabled bool
	// Recent holds the last few mismatches, oldest first, for whoever is looking
	// at why the counter moved.
	Recent []AudioShadowMismatch
}

// pendingAudioShadowBatch is a batch and the core's own answer to it.
//
// The reference is frozen here rather than read back later, because "later" may
// be after the observer that produced it has been thrown away: a PMT change can
// end an epoch half way through a chunk, and the batch from before it is exactly
// the one a PID-reuse bug would show up in. Comparing only what is still alive at
// the end of the chunk would skip it - and a shadow that gets the ended epoch
// wrong and the current one right would report no disagreement at all.
//
// The reference never leaves this process. A shadow is asked what it made of the
// bytes, not asked to agree with an answer it was given.
type pendingAudioShadowBatch struct {
	Batch     AudioShadowBatch
	Reference esaudio.Observation
}

// recentMismatches is how many disagreements are kept. Enough to see a pattern,
// few enough that a stream disagreeing about every chunk cannot grow memory.
const recentMismatches = 8

// SetAudioShadow attaches a second observer to this core.
//
// Deliberately not part of the Core interface and not on the constructor: a
// shadow is something an operator turns on for a migration, not part of what a
// core is. A core without one behaves exactly as it did before this existed,
// down to not capturing the feeds.
func (c *GoCore) SetAudioShadow(shadow AudioShadow) {
	c.shadow = shadow
	c.shadowBatches = nil
	c.shadowReport = AudioShadowReport{}
}

// AudioShadowReport returns what the shadow has been doing so far.
func (c *GoCore) AudioShadowReport() AudioShadowReport {
	out := c.shadowReport
	out.Recent = append([]AudioShadowMismatch(nil), c.shadowReport.Recent...)
	return out
}

// captureAudioShadowFeedLocked records one feed exactly as the core's own
// observer received it.
//
// Batches are keyed by stream and epoch, and a new epoch closes the ones before
// it: a PMT change can happen half way through a chunk, and the feeds either
// side of it belong to different elementary streams that merely share a number.
// Folding them together would hand the shadow a stream that never existed.
func (c *GoCore) captureAudioShadowFeedLocked(pid uint16, es []byte, observer *esaudio.Observer) {
	if c.shadow == nil || c.shadowReport.Disabled || len(es) == 0 {
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

// runAudioShadow hands the chunk's batches over and compares what comes back.
//
// Everything in here is best effort in one direction only: it reads the core's
// state and writes nothing back to it. A shadow that fails, hangs past its
// caller's deadline, or answers nonsense changes no fact, no event and no error
// this core returns - the only thing it can change is whether it is asked again.
//
// And it is not asked again. A shadow that failed once is a second
// implementation that is now in an unknown state, and the useful thing to
// report from that point on is "the comparison stopped", not a stream of
// disagreements between a working observer and a broken one. Reviving it would
// also be the transparent restart the process contract already refuses.
func (c *GoCore) runAudioShadow(ctx context.Context) {
	// Not cleared here. Ingest clears on every way out, including the ones that
	// never reach this function, and dropping the length early would hide the
	// elements that still have to be zeroed from that cleanup.
	pending := c.shadowBatches

	if c.shadow == nil || c.shadowReport.Disabled || len(pending) == 0 {
		return
	}
	c.shadowReport.Batches += uint64(len(pending))

	batches := make([]AudioShadowBatch, len(pending))
	for i := range pending {
		batches[i] = pending[i].Batch
	}

	observations, err := c.shadow.ObserveAudio(ctx, batches)
	if err != nil {
		c.retireAudioShadow()
		return
	}

	// The answer is checked before any of it is believed. An answer that is
	// missing, extra, reordered or labelled with somebody else's stream is not a
	// smaller comparison - it is a shadow that has lost track of what it was
	// asked, and every remaining number it produces is about an unknown stream.
	if len(observations) != len(batches) {
		c.retireAudioShadow()
		return
	}
	for i := range observations {
		if observations[i].PID != batches[i].PID || observations[i].Epoch != batches[i].Epoch {
			c.retireAudioShadow()
			return
		}
	}

	for i, o := range observations {
		reference := pending[i].Reference
		c.shadowReport.Compared++
		fields := esaudio.Compare(reference, o.Observation)
		if len(fields) == 0 {
			continue
		}
		c.shadowReport.Mismatches++
		c.shadowReport.Recent = append(c.shadowReport.Recent, AudioShadowMismatch{
			PID:       o.PID,
			Epoch:     o.Epoch,
			Fields:    fields,
			Reference: reference,
			Shadow:    o.Observation,
		})
		if len(c.shadowReport.Recent) > recentMismatches {
			c.shadowReport.Recent = c.shadowReport.Recent[len(c.shadowReport.Recent)-recentMismatches:]
		}
	}
}

// retireAudioShadow ends the comparison for the lifetime of this core.
//
// A shadow that failed once is a second implementation in an unknown state, and
// the useful thing to report from here on is "the comparison stopped", not a
// stream of disagreements between a working observer and a broken one. Reviving
// it would also be the transparent restart the process contract already refuses.
func (c *GoCore) retireAudioShadow() {
	c.shadowReport.Errors++
	// One flag, read everywhere: the report is the state, not a copy of it.
	c.shadowReport.Disabled = true
}
