package manager

import (
	"context"
	"sync"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
)

// StubBus implements ports.Bus for testing without legacy dependencies.
type StubBus struct {
	mu     sync.Mutex
	subs   map[string][]chan interface{}
	ready  map[string]chan struct{}
	closed map[string]bool
}

func NewStubBus() *StubBus {
	return &StubBus{
		subs:   make(map[string][]chan interface{}),
		ready:  make(map[string]chan struct{}),
		closed: make(map[string]bool),
	}
}

func (b *StubBus) Publish(ctx context.Context, topic string, event interface{}) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ch := range b.subs[topic] {
		select {
		case ch <- event:
		default:
			// Non-blocking publish for tests
		}
	}
	return nil
}

func (b *StubBus) Subscribe(ctx context.Context, topic string) (ports.Subscription, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(chan interface{}, 10)
	b.subs[topic] = append(b.subs[topic], ch)
	if _, ok := b.ready[topic]; !ok {
		b.ready[topic] = make(chan struct{})
	}
	if !b.closed[topic] {
		close(b.ready[topic])
		b.closed[topic] = true
	}
	return &StubSubscription{ch: ch}, nil
}

func (b *StubBus) WaitForSubscriber(topic string) {
	b.mu.Lock()
	ch, ok := b.ready[topic]
	if !ok {
		ch = make(chan struct{})
		b.ready[topic] = ch
	}
	b.mu.Unlock()
	<-ch
}

type StubSubscription struct {
	ch chan interface{}
}

func (s *StubSubscription) C() <-chan interface{} {
	return s.ch
}

func (s *StubSubscription) Close() error {
	return nil
}
