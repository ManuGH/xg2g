//go:build v3
// +build v3

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

func (b *MemoryBus) Subscribe(ctx context.Context, topic string, handler Handler) error {
	ch := make(chan Message, 64)

	b.mu.Lock()
	b.subs[topic] = append(b.subs[topic], ch)
	b.mu.Unlock()

	defer func() {
		b.mu.Lock()
		lst := b.subs[topic]
		out := lst[:0]
		for _, c := range lst {
			if c != ch {
				out = append(out, c)
			}
		}
		if len(out) == 0 {
			delete(b.subs, topic)
		} else {
			b.subs[topic] = out
		}
		b.mu.Unlock()
		close(ch)
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg := <-ch:
			_ = handler(ctx, msg)
		}
	}
}
