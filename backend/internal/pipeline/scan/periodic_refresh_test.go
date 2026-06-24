package scan

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// newRefreshTestManager builds a Manager whose scan work is replaced by the given
// closure (the scanFn seam), with the lifecycle context attached so backgroundContext
// is available. Setting scanFn also bypasses RunBackground's warm-cache short-circuit,
// so every trigger reaches the closure deterministically.
func newRefreshTestManager(scan func(context.Context) error) *Manager {
	m := NewManager(nil, "", nil)
	m.scanFn = scan
	m.ProbeDelay = 0
	m.AttachLifecycle(context.Background())
	return m
}

// TestStartPeriodicRefresh_FiresRepeatedly is the load-bearing assertion: with a
// positive interval the loop must re-trigger the scan again and again. Neuter
// StartPeriodicRefresh (e.g. flip the interval guard) and this goes red at 0 calls.
func TestStartPeriodicRefresh_FiresRepeatedly(t *testing.T) {
	var calls int32
	m := newRefreshTestManager(func(ctx context.Context) error {
		atomic.AddInt32(&calls, 1)
		return nil
	})
	defer m.Stop()

	m.StartPeriodicRefresh(15 * time.Millisecond)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&calls) >= 3 {
			return // periodic refresh re-triggered the scan as required
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("periodic refresh fired %d times, want >= 3", atomic.LoadInt32(&calls))
}

// TestStartPeriodicRefresh_DisabledWhenIntervalZero guards the off switch: a
// non-positive interval must not start any loop.
func TestStartPeriodicRefresh_DisabledWhenIntervalZero(t *testing.T) {
	var calls int32
	m := newRefreshTestManager(func(ctx context.Context) error {
		atomic.AddInt32(&calls, 1)
		return nil
	})
	defer m.Stop()

	m.StartPeriodicRefresh(0)

	time.Sleep(150 * time.Millisecond)
	if n := atomic.LoadInt32(&calls); n != 0 {
		t.Fatalf("interval<=0 must not start a refresh loop, but scan fired %d times", n)
	}
}

// TestStartPeriodicRefresh_StopsOnLifecycleCancel proves the loop is bound to the
// lifecycle: after Stop() it must stop re-triggering.
func TestStartPeriodicRefresh_StopsOnLifecycleCancel(t *testing.T) {
	var calls int32
	m := newRefreshTestManager(func(ctx context.Context) error {
		atomic.AddInt32(&calls, 1)
		return nil
	})

	m.StartPeriodicRefresh(15 * time.Millisecond)
	time.Sleep(90 * time.Millisecond)

	m.Stop() // cancels runtimeCtx and waits on bgWG
	stopped := atomic.LoadInt32(&calls)

	time.Sleep(150 * time.Millisecond)
	if grew := atomic.LoadInt32(&calls) - stopped; grew != 0 {
		t.Fatalf("scan kept firing %d more times after Stop()", grew)
	}
}
