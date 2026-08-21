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
)

var (
	ErrPipelineClosed    = errors.New("session pipeline closed")
	ErrNoAttachAvailable = errors.New("no primed attach point available")
)

// SessionPipeline represents the unified live ingest engine for an active channel stream.
// It pumps raw upstream reads through the 20ms StreamNormalizer into the multi-reader MasterRing.
type SessionPipeline struct {
	norm       *normalizer.StreamNormalizer
	ring       *ring.MasterRing
	cancelFunc context.CancelFunc
	runErr     error
	runErrMu   sync.Mutex
	doneCh     chan struct{}
	onDone     func(err error)
	onDoneMu   sync.Mutex
	closed     atomic.Bool
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
		norm:   norm,
		ring:   master,
		doneCh: make(chan struct{}),
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
		defer upstream.Close()
		defer p.ring.Close()
		defer p.norm.Close()

		err := p.norm.Run(ctx, upstream)
		p.runErrMu.Lock()
		p.runErr = err
		p.runErrMu.Unlock()

		p.onDoneMu.Lock()
		cb := p.onDone
		p.onDoneMu.Unlock()
		if cb != nil {
			cb(err)
		}
	}()
}

// OnDone registers a lifecycle completion callback executed when the upstream finishes.
func (p *SessionPipeline) OnDone(callback func(err error)) {
	p.onDoneMu.Lock()
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

// Close terminates the pipeline and wakes all subscribers.
func (p *SessionPipeline) Close() {
	if p.closed.CompareAndSwap(false, true) {
		if p.cancelFunc != nil {
			p.cancelFunc()
		}
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
