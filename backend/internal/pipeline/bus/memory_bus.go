// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package bus

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/metrics"
)

// MemoryBus is an in-memory pub/sub used for unit tests and local prototyping.
// It is not durable and provides at-least-once in-process delivery while
// publish contexts remain active.
type MemoryBus struct {
	mu   sync.RWMutex
	subs map[string][]*memSub
}

const dropLogEvery = 100

var dropCount atomic.Uint64

func NewMemoryBus() *MemoryBus {
	return &MemoryBus{subs: make(map[string][]*memSub)}
}

func publishDropReason(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	default:
		return "context_done"
	}
}

func (b *MemoryBus) Publish(ctx context.Context, topic string, msg Message) error {
	if ctx == nil {
		return fmt.Errorf("publish context is nil")
	}
	b.mu.RLock()
	subs := append([]*memSub(nil), b.subs[topic]...)
	b.mu.RUnlock()
	for _, s := range subs {
		if err := s.deliver(ctx, topic, msg); err != nil {
			return err
		}
	}
	return nil
}

func (b *MemoryBus) Subscribe(_ context.Context, topic string) (Subscriber, error) {
	s := &memSub{b: b, topic: topic, ch: make(chan Message, 64), done: make(chan struct{})}

	b.mu.Lock()
	b.subs[topic] = append(b.subs[topic], s)
	b.mu.Unlock()

	return s, nil
}

type memSub struct {
	b     *MemoryBus
	topic string
	ch    chan Message

	// mu guards the send/close handshake: deliver holds it for read while it is
	// selecting on ch, Close holds it for write while it closes ch. Because the
	// two are mutually exclusive, a publish can never send on a closed channel.
	mu     sync.RWMutex
	closed bool

	// done is closed by Close before acquiring mu (write), giving in-flight
	// delivers a way to bail out when the subscriber stops draining. Without it
	// a publish blocked on a full channel behind a long-lived context would
	// prevent Close from ever acquiring the write lock, causing a deadlock.
	done chan struct{}
}

func (s *memSub) C() <-chan Message {
	return s.ch
}

// deliver sends msg to this subscriber. Holding the read lock for the duration
// of the select guarantees ch is not closed underneath us (Close takes the
// write lock before closing). A closed subscriber is skipped, not panicked on.
func (s *memSub) deliver(ctx context.Context, topic string, msg Message) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil
	}
	select {
	case s.ch <- msg:
		return nil
	case <-ctx.Done():
		reason := publishDropReason(ctx.Err())
		metrics.IncBusDropReason(topic, reason)
		count := dropCount.Add(1)
		if count%dropLogEvery == 0 {
			log.L().Warn().
				Str("topic", topic).
				Str("reason", reason).
				Uint64("dropped", count).
				Msg("memory bus failed to publish due to context cancellation")
		}
		return fmt.Errorf("publish topic %q: %w", topic, ctx.Err())
	case <-s.done:
		return nil
	}
}

func (s *memSub) Close() error {
	// Detach from the bus first so no future Publish snapshots this subscriber.
	s.b.mu.Lock()
	lst := s.b.subs[s.topic]
	out := lst[:0]
	for _, c := range lst {
		if c != s {
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		delete(s.b.subs, s.topic)
	} else {
		s.b.subs[s.topic] = out
	}
	s.b.mu.Unlock()

	// Signal in-flight delivers to bail out before acquiring the write lock.
	// This avoids a deadlock where a publish blocked on a full channel behind a
	// long-lived context holds the read lock and prevents Close from proceeding.
	close(s.done)

	// Close the channel under the write lock so it cannot race an in-flight
	// deliver (which holds the read lock while selecting on ch). Closing the
	// channel is the shutdown signal consumers detect via the receive ok flag.
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		close(s.ch) // Signal subscriber to stop
	}
	return nil
}

// Ensure compliance
var _ Bus = (*MemoryBus)(nil)
