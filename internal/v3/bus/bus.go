// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package bus

import "context"

// Message is an opaque event payload. For MVP, we use a typed struct.
// For NATS JetStream, this will map to subject + bytes.
type Message interface{}

// Handler applies an event/message within a context.
type Handler func(ctx context.Context, msg Message) error

type Subscriber interface {
	// C returns a read-only message channel.
	C() <-chan Message
	// Close unsubscribes.
	Close() error
}

// Bus is the event transport abstraction.
// MVP: in-memory bus, later: NATS JetStream.
type Bus interface {
	Publish(ctx context.Context, topic string, msg Message) error
	Subscribe(ctx context.Context, topic string) (Subscriber, error)
}
