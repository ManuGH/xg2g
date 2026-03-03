package ports

import "context"

// Bus defines the interface for the event bus.
// It mirrors the legacy bus.Bus interface to allow drop-in replacement.
type Bus interface {
	Publish(ctx context.Context, topic string, event interface{}) error
	Subscribe(ctx context.Context, topic string) (Subscription, error)
}

type Subscription interface {
	C() <-chan interface{}
	Close() error
}
