package manager

import (
	"context"
	"sync"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
)

// StubBus implements ports.Bus for testing without legacy dependencies.
type StubBus struct {
	mu   sync.Mutex
	subs map[string][]chan interface{}
}

func NewStubBus() *StubBus {
	return &StubBus{
		subs: make(map[string][]chan interface{}),
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
	return &StubSubscription{ch: ch}, nil
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
