// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package bus

import (
	"context"
	"sync"
	"testing"
)

// TestMemoryBusConcurrentPublishCloseNoPanic exercises the send/close handshake.
// Before the per-subscriber lock, a Close() that closed the channel while a
// Publish was selecting on it caused "send on closed channel" and crashed the
// whole process. Run under -race; the test must simply complete.
func TestMemoryBusConcurrentPublishCloseNoPanic(t *testing.T) {
	const iterations = 100
	for i := 0; i < iterations; i++ {
		b := NewMemoryBus()
		ctx := context.Background()

		sub, err := b.Subscribe(ctx, "live")
		if err != nil {
			t.Fatalf("subscribe: %v", err)
		}

		// Consumer drains continuously until Close closes the channel; this keeps
		// publishers unblocked so the close handshake can converge.
		drained := make(chan struct{})
		go func() {
			for range sub.C() {
			}
			close(drained)
		}()

		var wg sync.WaitGroup
		for p := 0; p < 8; p++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 64; j++ {
					_ = b.Publish(ctx, "live", j)
				}
			}()
		}

		// Close races the in-flight publishes.
		go func() { _ = sub.Close() }()

		wg.Wait()
		<-drained
	}
}

// TestMemoryBusPublishAfterCloseIsNoop verifies a publish to a closed
// subscriber is silently skipped rather than panicking.
func TestMemoryBusPublishAfterCloseIsNoop(t *testing.T) {
	b := NewMemoryBus()
	ctx := context.Background()

	sub, err := b.Subscribe(ctx, "live")
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if err := sub.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Must not panic and must not error (subscriber gone => nothing to deliver).
	if err := b.Publish(ctx, "live", "after-close"); err != nil {
		t.Fatalf("publish after close: %v", err)
	}
}
