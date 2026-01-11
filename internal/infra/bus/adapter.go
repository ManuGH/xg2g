package bus

import (
	"context"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/pipeline/bus"
)

// Adapter wraps the legacy bus to match the domain port options.
type Adapter struct {
	inner bus.Bus
}

func NewAdapter(b bus.Bus) *Adapter {
	return &Adapter{inner: b}
}

func (a *Adapter) Publish(ctx context.Context, topic string, event interface{}) error {
	return a.inner.Publish(ctx, topic, event)
}

func (a *Adapter) Subscribe(ctx context.Context, topic string) (ports.Subscription, error) {
	sub, err := a.inner.Subscribe(ctx, topic)
	if err != nil {
		return nil, err
	}
	return &subAdapter{inner: sub}, nil
}

type subAdapter struct {
	inner bus.Subscriber
}

func (s *subAdapter) C() <-chan interface{} {
	return s.inner.C()
}

func (s *subAdapter) Close() error {
	return s.inner.Close()
}
