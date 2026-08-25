// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package pipeline

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ManuGH/xg2g/internal/stream/ingest/normalizer"
	"github.com/ManuGH/xg2g/internal/stream/ingest/ring"
	"github.com/ManuGH/xg2g/internal/stream/ingest/variant"
)

var (
	ErrPipelineClosed    = errors.New("session pipeline closed")
	ErrNoAttachAvailable = errors.New("no primed attach point available")
)

// SessionPipeline represents the unified live ingest engine for an active channel stream.
// It pumps raw upstream reads through the 20ms StreamNormalizer into the multi-reader MasterRing.
type SessionPipeline struct {
	norm          *normalizer.StreamNormalizer
	ring          *ring.MasterRing
	variantMgr    *variant.AudioVariantManager
	cancelFunc    context.CancelFunc
	runErr        error
	runErrMu      sync.Mutex
	doneCh        chan struct{}
	onDone        func(err error)
	onDoneMu      sync.Mutex
	completed     bool
	completionErr error
	closed        atomic.Bool
	// observeOnce keeps the readiness observation to one per ingest. Subscribers
	// coalesce onto a shared pipeline, and a second observer would time a stream
	// that had already been running from the moment its second viewer arrived.
	observeOnce sync.Once
}

// ObserveOnce runs fn at most once for this pipeline.
func (p *SessionPipeline) ObserveOnce(fn func()) {
	p.observeOnce.Do(fn)
}

// NewSessionPipeline creates a new live ingest pipeline.
func NewSessionPipeline(normCfg normalizer.Config, ringCapacity int, targetProgram uint16) (*SessionPipeline, error) {
	master := ring.NewMasterRingWithProgram(ringCapacity, targetProgram)

	norm, err := normalizer.NewStreamNormalizer(normCfg, func(ctx context.Context, chunk []byte) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		_, pushErr := master.Push(chunk)
		return pushErr
	})
	if err != nil {
		master.Close()
		return nil, err
	}

	p := &SessionPipeline{
		norm:       norm,
		ring:       master,
		variantMgr: variant.NewAudioVariantManager(master),
		doneCh:     make(chan struct{}),
	}
	return p, nil
}

// Start launches the normalizer pump over the provided upstream ReadCloser.
// The caller passes a session-scoped context.
func (p *SessionPipeline) Start(ctx context.Context, upstream io.ReadCloser) {
	ctx, cancel := context.WithCancel(ctx)
	p.cancelFunc = cancel

	go func() {
		defer close(p.doneCh)
		defer func() { _ = upstream.Close() }()
		defer p.ring.Close()
		defer p.norm.Close()

		err := p.norm.Run(ctx, upstream)
		p.runErrMu.Lock()
		p.runErr = err
		p.runErrMu.Unlock()

		p.onDoneMu.Lock()
		p.completed = true
		p.completionErr = err
		cb := p.onDone
		p.onDoneMu.Unlock()
		if cb != nil {
			cb(err)
		}
	}()
}

// OnDone registers a lifecycle completion callback executed when the upstream finishes.
// It is late-subscriber safe: if the pipeline has already finished, the callback is invoked immediately.
func (p *SessionPipeline) OnDone(callback func(err error)) {
	if callback == nil {
		return
	}

	p.onDoneMu.Lock()
	if p.completed {
		err := p.completionErr
		p.onDoneMu.Unlock()
		callback(err)
		return
	}
	p.onDone = callback
	p.onDoneMu.Unlock()
}

// PrimedAttach captures an atomic snapshot of the active PAT/PMT preamble and latest keyframe,
// attaching a subscriber reader positioned at that exact keyframe boundary.
// It returns ErrNoAttachAvailable if no valid keyframe is present in the buffer.
func (p *SessionPipeline) PrimedAttach() (ring.PrimedAttachPoint, *ring.SubscriberReader, error) {
	if p.closed.Load() {
		return ring.PrimedAttachPoint{}, nil, ErrPipelineClosed
	}

	attach, reader, err := p.ring.NewPrimedSubscriber()
	if err != nil {
		if errors.Is(err, ring.ErrNoKeyframeAvailable) {
			return ring.PrimedAttachPoint{}, nil, ErrNoAttachAvailable
		}
		// ring.ErrScrambledStream is deliberately NOT folded into ErrNoAttachAvailable:
		// it is terminal, and PrimedAttachWithTimeout must surface it without retrying.
		return ring.PrimedAttachPoint{}, nil, err
	}
	return attach, reader, nil
}

// PrimedAttachWithTimeout waits up to timeout for the first valid keyframe to arrive in the ring buffer.
func (p *SessionPipeline) PrimedAttachWithTimeout(ctx context.Context, timeout time.Duration) (ring.PrimedAttachPoint, *ring.SubscriberReader, error) {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		attach, reader, err := p.PrimedAttach()
		if err == nil {
			return attach, reader, nil
		}
		// Only "not yet" is retried. Terminal conditions (closed ring, scrambled upstream)
		// return straight away rather than consuming the full timeout budget.
		if !errors.Is(err, ErrNoAttachAvailable) {
			return ring.PrimedAttachPoint{}, nil, err
		}

		if time.Now().After(deadline) {
			return ring.PrimedAttachPoint{}, nil, ErrNoAttachAvailable
		}

		select {
		case <-ctx.Done():
			return ring.PrimedAttachPoint{}, nil, ctx.Err()
		case <-p.doneCh:
			return ring.PrimedAttachPoint{}, nil, ErrPipelineClosed
		case <-ticker.C:
		}
	}
}

// LiveAttach attaches a subscriber reader starting at the current live head.
func (p *SessionPipeline) LiveAttach() (*ring.SubscriberReader, error) {
	if p.closed.Load() {
		return nil, ErrPipelineClosed
	}
	return p.ring.NewSubscriberReader(p.ring.Head()), nil
}

// MasterRing returns the underlying circular buffer.
func (p *SessionPipeline) MasterRing() *ring.MasterRing {
	return p.ring
}

// Normalizer returns the underlying stream normalizer.
func (p *SessionPipeline) Normalizer() *normalizer.StreamNormalizer {
	return p.norm
}

// AudioVariants returns the AudioVariantManager for this pipeline.
func (p *SessionPipeline) AudioVariants() *variant.AudioVariantManager {
	return p.variantMgr
}

// AttachAudioVariantWithTimeout attaches a subscriber to an audio variant stream.
// It returns the primed attach point, subscriber reader, and a release function that the caller MUST invoke upon disconnect.
func (p *SessionPipeline) AttachAudioVariantWithTimeout(ctx context.Context, key variant.AudioVariantKey, timeout time.Duration) (ring.PrimedAttachPoint, *ring.SubscriberReader, func(), error) {
	if p.closed.Load() {
		return ring.PrimedAttachPoint{}, nil, nil, ErrPipelineClosed
	}

	worker, err := p.variantMgr.GetOrCreateWorker(ctx, key)
	if err != nil {
		return ring.PrimedAttachPoint{}, nil, nil, err
	}

	attach, reader, err := worker.PrimedAttachWithTimeout(ctx, timeout)
	if err != nil {
		p.variantMgr.ReleaseWorker(key)
		return ring.PrimedAttachPoint{}, nil, nil, err
	}

	releaseFunc := func() {
		p.variantMgr.ReleaseWorker(key)
	}

	return attach, reader, releaseFunc, nil
}

// Close terminates the pipeline and wakes all subscribers.
func (p *SessionPipeline) Close() {
	if p.closed.CompareAndSwap(false, true) {
		if p.cancelFunc != nil {
			p.cancelFunc()
		}
		p.variantMgr.Close()
		p.norm.Close()
		p.ring.Close()
	}
}

// Done returns a channel that closes when the upstream ingest finishes.
func (p *SessionPipeline) Done() <-chan struct{} {
	return p.doneCh
}

// Err returns the error that caused the ingest to finish.
func (p *SessionPipeline) Err() error {
	p.runErrMu.Lock()
	defer p.runErrMu.Unlock()
	return p.runErr
}
