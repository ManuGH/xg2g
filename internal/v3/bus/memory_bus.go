// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package bus

import (
	"context"
	"sync"
)

// MemoryBus is an in-memory pub/sub used for unit tests and local prototyping.
// It is not durable and provides best-effort delivery.
type MemoryBus struct {
	mu   sync.RWMutex
	subs map[string][]chan Message
}

func NewMemoryBus() *MemoryBus {
	return &MemoryBus{subs: make(map[string][]chan Message)}
}

func (b *MemoryBus) Publish(_ context.Context, topic string, msg Message) error {
	b.mu.RLock()
	chs := append([]chan Message(nil), b.subs[topic]...)
	b.mu.RUnlock()
	for _, ch := range chs {
		select {
		case ch <- msg:
		default:
			// drop on backpressure to avoid producer blockage
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
