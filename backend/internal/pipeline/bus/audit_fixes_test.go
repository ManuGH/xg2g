package bus

import (
	"context"
	"testing"
)

// L13: a second Close must be idempotent. Previously close(s.done) was unguarded, so a
// double Close panicked with "close of closed channel".
func TestMemSubCloseIsIdempotent(t *testing.T) {
	b := NewMemoryBus()
	sub, err := b.Subscribe(context.Background(), "topic")
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if err := sub.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	// Must not panic on close(s.done).
	if err := sub.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}
