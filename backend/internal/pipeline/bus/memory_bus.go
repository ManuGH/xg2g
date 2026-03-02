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
	subs map[string][]chan Message
}

const dropLogEvery = 100

var dropCount atomic.Uint64

func NewMemoryBus() *MemoryBus {
	return &MemoryBus{subs: make(map[string][]chan Message)}
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
	chs := append([]chan Message(nil), b.subs[topic]...)
	b.mu.RUnlock()
	for _, ch := range chs {
		select {
		case ch <- msg:
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
		}
	}
	return nil
}

func (b *MemoryBus) Subscribe(ctx context.Context, topic string) (Subscriber, error) {
	ch := make(chan Message, 64)

	b.mu.Lock()
	b.subs[topic] = append(b.subs[topic], ch)
	b.mu.Unlock()

	return &memSub{b: b, topic: topic, ch: ch}, nil
}

type memSub struct {
	b     *MemoryBus
	topic string
	ch    chan Message
}

func (s *memSub) C() <-chan Message {
	return s.ch
}

func (s *memSub) Close() error {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()

	lst := s.b.subs[s.topic]
	out := lst[:0]
	for _, c := range lst {
		if c != s.ch {
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		delete(s.b.subs, s.topic)
	} else {
		s.b.subs[s.topic] = out
	}
	close(s.ch) // Signal subscriber to stop
	return nil
}

// Ensure compliance
var _ Bus = (*MemoryBus)(nil)
