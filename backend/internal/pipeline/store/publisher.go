package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/rs/zerolog"
)

// ErrObjectTooLarge is returned when an object is larger than the entire store limit.
var ErrObjectTooLarge = errors.New("object too large")

// ErrQueueFull is returned when the publisher's queue drops a command.
var ErrQueueFull = errors.New("shadow publisher queue full")

// ErrPublisherClosed is returned when the publisher is already closed.
var ErrPublisherClosed = errors.New("shadow publisher closed")

// ErrInvalidPublisherConfig is returned when publisher configuration is invalid.
var ErrInvalidPublisherConfig = errors.New("invalid shadow publisher configuration")

// PublisherStats provides observability metrics for the publisher.
type PublisherStats struct {
	QueuedBytes      int64
	QueueLength      int
	AcceptedTotal    uint64
	DroppedTotal     uint64
	DeleteDropsTotal uint64
}

type storeCommandKind uint8

const (
	cmdPublish storeCommandKind = iota
	cmdDelete
	cmdDeleteStream
)

type storeCommand struct {
	kind     storeCommandKind
	streamID StreamID
	object   Object
	name     string
}

// ShadowPublisher is a bounded queue that decouples the production path from the ShadowStore.
// It tracks the number of bytes enqueued and drops objects if limits are reached.
// It serializes Publish and Delete commands so they execute in order.
type ShadowPublisher struct {
	queue         chan storeCommand
	store         ShadowStore
	maxQueueBytes int64
	queuedBytes   int64

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	logger zerolog.Logger

	startOnce sync.Once

	enqueueMu sync.Mutex
	closed    bool

	acceptedTotal    uint64
	droppedTotal     uint64
	deleteDropsTotal uint64
}

// NewShadowPublisher creates a new bounded publisher.
func NewShadowPublisher(store ShadowStore, maxObjects int, maxQueueBytes int64, logger zerolog.Logger) (*ShadowPublisher, error) {
	if store == nil || maxObjects <= 0 || maxQueueBytes <= 0 {
		return nil, ErrInvalidPublisherConfig
	}

	ctx, cancel := context.WithCancel(context.Background())
	return &ShadowPublisher{
		queue:         make(chan storeCommand, maxObjects),
		store:         store,
		maxQueueBytes: maxQueueBytes,
		ctx:           ctx,
		cancel:        cancel,
		done:          make(chan struct{}),
		logger:        logger.With().Str("component", "shadow_publisher").Logger(),
	}, nil
}

// Stats returns a snapshot of current publisher statistics.
func (p *ShadowPublisher) Stats() PublisherStats {
	return PublisherStats{
		QueuedBytes:      atomic.LoadInt64(&p.queuedBytes),
		QueueLength:      len(p.queue),
		AcceptedTotal:    atomic.LoadUint64(&p.acceptedTotal),
		DroppedTotal:     atomic.LoadUint64(&p.droppedTotal),
		DeleteDropsTotal: atomic.LoadUint64(&p.deleteDropsTotal),
	}
}

// Start begins the background worker that drains the queue into the store.
func (p *ShadowPublisher) Start() {
	p.startOnce.Do(func() {
		go p.worker()
	})
}

// Close stops accepting new publish requests, drains the queue synchronously, and stops the worker.
func (p *ShadowPublisher) Close(ctx context.Context) error {
	p.Start() // prevents deadlock if Start() was never called

	p.enqueueMu.Lock()
	if !p.closed {
		p.closed = true
		p.cancel()
	}
	p.enqueueMu.Unlock()

	select {
	case <-p.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *ShadowPublisher) reserveBytes(size int64) bool {
	if p.maxQueueBytes <= 0 {
		return true
	}
	for {
		curr := atomic.LoadInt64(&p.queuedBytes)
		if curr+size > p.maxQueueBytes {
			return false
		}
		if atomic.CompareAndSwapInt64(&p.queuedBytes, curr, curr+size) {
			return true
		}
	}
}

// Publish enqueues an object for publishing. It does not block.
// If the queue is full or byte limits are exceeded, the object is dropped.
func (p *ShadowPublisher) Publish(ctx context.Context, streamID StreamID, object Object) error {
	size := int64(len(object.Data))

	p.enqueueMu.Lock()
	defer p.enqueueMu.Unlock()

	if p.closed {
		return ErrPublisherClosed
	}

	if p.maxQueueBytes <= 0 || size > p.maxQueueBytes {
		atomic.AddUint64(&p.droppedTotal, 1)
		return ErrQueueFull
	}

	if !p.reserveBytes(size) {
		p.logger.Warn().
			Str("stream_id", string(streamID)).
			Str("name", object.Name).
			Int64("size", size).
			Msg("shadow queue byte limit exceeded; dropping object")
		atomic.AddUint64(&p.droppedTotal, 1)
		return ErrQueueFull
	}

	object.Data = bytes.Clone(object.Data)

	req := storeCommand{
		kind:     cmdPublish,
		streamID: streamID,
		object:   object,
	}

	select {
	case p.queue <- req:
		atomic.AddUint64(&p.acceptedTotal, 1)
		return nil
	default:
		atomic.AddInt64(&p.queuedBytes, -size) // rollback
		p.logger.Warn().
			Str("stream_id", string(streamID)).
			Str("name", object.Name).
			Msg("shadow queue full; dropping object")
		atomic.AddUint64(&p.droppedTotal, 1)
		return ErrQueueFull
	}
}

// Delete enqueues a deletion command. It does not block.
func (p *ShadowPublisher) Delete(ctx context.Context, streamID StreamID, name string) error {
	p.enqueueMu.Lock()
	defer p.enqueueMu.Unlock()

	if p.closed {
		return ErrPublisherClosed
	}

	req := storeCommand{
		kind:     cmdDelete,
		streamID: streamID,
		name:     name,
	}

	select {
	case p.queue <- req:
		return nil
	default:
		p.logger.Warn().
			Str("stream_id", string(streamID)).
			Str("name", name).
			Msg("shadow queue full; dropping delete command")
		atomic.AddUint64(&p.deleteDropsTotal, 1)
		return ErrQueueFull
	}
}

// DeleteStream enqueues a stream deletion command. It does not block.
func (p *ShadowPublisher) DeleteStream(ctx context.Context, streamID StreamID) error {
	p.enqueueMu.Lock()
	defer p.enqueueMu.Unlock()

	if p.closed {
		return ErrPublisherClosed
	}

	req := storeCommand{
		kind:     cmdDeleteStream,
		streamID: streamID,
	}

	select {
	case p.queue <- req:
		return nil
	default:
		p.logger.Warn().
			Str("stream_id", string(streamID)).
			Msg("shadow queue full; dropping delete_stream command")
		atomic.AddUint64(&p.deleteDropsTotal, 1)
		return ErrQueueFull
	}
}

func (p *ShadowPublisher) worker() {
	defer close(p.done)

	// Process items until the context is canceled, then drain the remaining items.
	for {
		select {
		case <-p.ctx.Done():
			p.drain()
			return
		case req := <-p.queue:
			p.process(req)
		}
	}
}

func (p *ShadowPublisher) drain() {
	for {
		select {
		case req := <-p.queue:
			p.process(req)
		default:
			return
		}
	}
}

func (p *ShadowPublisher) process(req storeCommand) {
	bgCtx := context.Background()

	switch req.kind {
	case cmdPublish:
		objSize := int64(len(req.object.Data))
		defer atomic.AddInt64(&p.queuedBytes, -objSize)

		if err := p.store.Publish(bgCtx, req.streamID, req.object); err != nil {
			p.logger.Warn().
				Err(err).
				Str("stream_id", string(req.streamID)).
				Str("name", req.object.Name).
				Msg("shadow publish failed")
		} else {
			// TEMPORARY SPRINT 1 VERIFIER
			diskHash := sha256.Sum256(req.object.Data)
			ramObject, err := p.store.Get(bgCtx, req.streamID, req.object.Name)
			if err == nil {
				ramHash := sha256.Sum256(ramObject.Data)
				if diskHash != ramHash {
					p.logger.Error().
						Str("filename", req.object.Name).
						Msg("shadow object hash mismatch")
				}
			}
		}
	case cmdDelete:
		if err := p.store.Delete(bgCtx, req.streamID, req.name); err != nil {
			p.logger.Warn().Err(err).Msg("shadow delete failed")
		}
	case cmdDeleteStream:
		if err := p.store.DeleteStream(bgCtx, req.streamID); err != nil {
			p.logger.Warn().Err(err).Msg("shadow delete stream failed")
		}
	}
}
