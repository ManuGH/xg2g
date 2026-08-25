package ffmpeg

import (
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// countingBody stands in for a receiver connection: it always has data, and it
// records whether it was closed.
type countingBody struct {
	mu     sync.Mutex
	closed bool
	reads  atomic.Int64
}

func (b *countingBody) Read(p []byte) (int, error) {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return 0, io.ErrClosedPipe
	}
	b.mu.Unlock()
	b.reads.Add(1)
	p[0] = 0x47
	return 1, nil
}

func (b *countingBody) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	return nil
}

func (b *countingBody) isClosed() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closed
}

// A connection nobody reads from is what an orphaned session leaves behind. It
// holds a receiver tuner for as long as it stays open, so the watchdog has to
// close it without any help from the session context.
func TestGuardedStreamBodyReapsIdleConnection(t *testing.T) {
	body := &countingBody{}
	guarded := newGuardedStreamBodyWithTimeouts(body, "session-orphan", "proxy", 60*time.Millisecond, 10*time.Millisecond)
	t.Cleanup(func() { _ = guarded.Close() })

	deadline := time.Now().Add(2 * time.Second)
	for !body.isClosed() {
		if time.Now().After(deadline) {
			t.Fatal("idle connection was never closed: an orphaned session would hold the tuner indefinitely")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// The receiver delivers in bursts — pauses up to 888 ms were measured on the
// wire — so a stream that is being consumed must survive gaps without the
// watchdog mistaking them for an orphan.
func TestGuardedStreamBodySurvivesWhileConsumed(t *testing.T) {
	body := &countingBody{}
	guarded := newGuardedStreamBodyWithTimeouts(body, "session-live", "proxy", 100*time.Millisecond, 10*time.Millisecond)
	t.Cleanup(func() { _ = guarded.Close() })

	buf := make([]byte, 1)
	done := time.Now().Add(400 * time.Millisecond)
	for time.Now().Before(done) {
		if _, err := guarded.Read(buf); err != nil {
			t.Fatalf("read failed while the stream was being consumed: %v", err)
		}
		if body.isClosed() {
			t.Fatal("watchdog closed a connection that was actively being read")
		}
		time.Sleep(40 * time.Millisecond) // a delivery gap, well inside the timeout
	}
}

// Both the context goroutine and the watchdog may reach Close, in either order.
// Closing twice must not double-decrement the active-connection gauge.
func TestGuardedStreamBodyCloseIsIdempotent(t *testing.T) {
	body := &countingBody{}
	guarded := newGuardedStreamBodyWithTimeouts(body, "session-double", "spool", time.Hour, time.Hour)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = guarded.Close()
		}()
	}
	wg.Wait()

	if !body.isClosed() {
		t.Fatal("body was not closed")
	}
}

// After the watchdog closes it, reads must fail rather than silently returning
// zero bytes forever — the consumer has to learn the stream is gone.
func TestGuardedStreamBodyReadFailsAfterReap(t *testing.T) {
	body := &countingBody{}
	guarded := newGuardedStreamBodyWithTimeouts(body, "session-reaped", "proxy", 40*time.Millisecond, 10*time.Millisecond)
	t.Cleanup(func() { _ = guarded.Close() })

	deadline := time.Now().Add(2 * time.Second)
	for !body.isClosed() {
		if time.Now().After(deadline) {
			t.Fatal("watchdog did not close the idle connection")
		}
		time.Sleep(5 * time.Millisecond)
	}

	if _, err := guarded.Read(make([]byte, 1)); err == nil {
		t.Fatal("read succeeded after the connection was reaped")
	}
}
