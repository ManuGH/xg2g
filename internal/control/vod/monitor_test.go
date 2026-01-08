package vod

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestVOD_Heartbeat validates deterministic heartbeat monitoring (Gate M2).
// Proves:
// 1. Heartbeat based on event receive (not event timestamps)
// 2. Timeout uses injected clock (no wall-clock)
// 3. On stall: state=Failed, reason=STALL, Stop(2s, 5s) called once
func TestVOD_Heartbeat(t *testing.T) {
	ctx := context.Background()
	clock := NewMockClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	// Create controllable progress channel
	progressCh := make(chan ProgressEvent, 10)
	behavior := &MockHandleBehavior{
		ProgressChan: progressCh,
		WaitBlocks:   true,
		WaitErr:      nil,
	}
	runner := NewMockRunner(nil, behavior)

	// Create monitor with injected clock
	mon := NewBuildMonitor(BuildMonitorConfig{
		JobID:  "test-job",
		Spec:   Spec{WorkDir: "/tmp/test"},
		Runner: runner,
		Clock:  clock,
	})

	// Start monitoring in background
	done := make(chan struct{})
	go func() {
		mon.Run(ctx)
		close(done)
	}()

	// Emit one heartbeat at t=0 to arm lastSeen
	progressCh <- ProgressEvent{}

	// Wait a bit for monitor to process (in real test, use synchronization)
	time.Sleep(10 * time.Millisecond)

	// Advance clock past timeout WITHOUT emitting progress (simulate stall)
	clock.Advance(HeartbeatTimeout + 1*time.Second)

	// Wait for monitor to terminate
	select {
	case <-done:
		// Monitor ended as expected
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Monitor did not terminate after timeout")
	}

	// Assert final state
	status := mon.GetStatus()
	assert.Equal(t, StateFailed, status.State, "Expected Failed state on stall")
	assert.Equal(t, ReasonStall, status.Reason, "Expected STALL reason")

	// Assert Stop was called with correct arguments
	handle := mon.handle.(*MockHandle)
	calls := handle.GetStopCalls()
	require.Len(t, calls, 1, "Stop should be called exactly once")
	assert.Equal(t, StopGrace, calls[0].Grace)
	assert.Equal(t, KillDelay, calls[0].Kill)
}
