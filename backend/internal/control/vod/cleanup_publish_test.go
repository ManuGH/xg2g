package vod

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Gate M3: Cleanup Under Faults

// TestVOD_Cleanup_OnStall validates cleanup on heartbeat timeout.
func TestVOD_Cleanup_OnStall(t *testing.T) {
	ctx := context.Background()
	clock := NewMockClock(time.Now())
	mockFS := &MockFS{}

	behavior := &MockHandleBehavior{
		ProgressChan: make(chan ProgressEvent),
		WaitBlocks:   true,
	}
	runner := NewMockRunner(nil, behavior)

	mon := NewBuildMonitor(BuildMonitorConfig{
		JobID:  "test-stall",
		Spec:   Spec{WorkDir: "/tmp/stall-test", OutputTemp: "output.m3u8"},
		Runner: runner,
		Clock:  clock,
		FS:     mockFS,
	})
	mockFS.SetExists("/tmp/stall-test/output.m3u8", true)

	// Run monitor
	done := make(chan struct{})
	go func() {
		mon.Run(ctx)
		close(done)
	}()

	// Simulate stall by advancing clock without progress
	time.Sleep(10 * time.Millisecond)
	clock.Advance(HeartbeatTimeout + 1*time.Second)

	<-done

	// Assert cleanup was attempted
	calls := mockFS.GetRemoveAllCalls()
	assert.Contains(t, calls, "/tmp/stall-test", "WorkDir should be cleaned up on stall")

	// Assert Stop was called (already proven in Gate M2, but verify here too)
	handle := mon.handle.(*MockHandle)
	assert.True(t, handle.WasStopped())
}

// TestVOD_Cleanup_OnCrash validates cleanup when process crashes.
func TestVOD_Cleanup_OnCrash(t *testing.T) {
	ctx := context.Background()
	clock := NewMockClock(time.Now())
	mockFS := &MockFS{}

	crashErr := errors.New("ffmpeg crashed")
	behavior := &MockHandleBehavior{
		ProgressChan: make(chan ProgressEvent),
		WaitErr:      crashErr,
		WaitBlocks:   false, // Return immediately with error
	}
	runner := NewMockRunner(nil, behavior)

	mon := NewBuildMonitor(BuildMonitorConfig{
		JobID:  "test-crash",
		Spec:   Spec{WorkDir: "/tmp/crash-test", OutputTemp: "output.m3u8"},
		Runner: runner,
		Clock:  clock,
		FS:     mockFS,
	})
	mockFS.SetExists("/tmp/crash-test/output.m3u8", true)

	mon.Run(ctx)

	// Assert cleanup was attempted
	calls := mockFS.GetRemoveAllCalls()
	assert.Contains(t, calls, "/tmp/crash-test", "WorkDir should be cleaned up on crash")

	// Assert state is Failed
	status := mon.GetStatus()
	assert.Equal(t, StateFailed, status.State)
	assert.Equal(t, ReasonCrash, status.Reason)
}

// TestVOD_Cleanup_OnStartFail validates cleanup when start fails.
func TestVOD_Cleanup_OnStartFail(t *testing.T) {
	ctx := context.Background()
	clock := NewMockClock(time.Now())
	mockFS := &MockFS{}

	startErr := errors.New("start failed")
	runner := NewMockRunner(startErr, nil)

	mon := NewBuildMonitor(BuildMonitorConfig{
		JobID:  "test-startfail",
		Spec:   Spec{WorkDir: "/tmp/startfail-test", OutputTemp: "output.m3u8"},
		Runner: runner,
		Clock:  clock,
		FS:     mockFS,
	})

	mon.Run(ctx)

	// Assert cleanup was attempted (workspace might have been created by control before start)
	calls := mockFS.GetRemoveAllCalls()
	assert.Contains(t, calls, "/tmp/startfail-test", "WorkDir should be cleaned up on start failure")

	// Assert state is Failed with START_FAIL reason
	status := mon.GetStatus()
	assert.Equal(t, StateFailed, status.State)
	assert.Equal(t, ReasonStartFail, status.Reason)
}

// Gate M4: Atomic Publish

// TestVOD_AtomicPublish_Success validates atomic rename on success.
func TestVOD_AtomicPublish_Success(t *testing.T) {
	ctx := context.Background()
	clock := NewMockClock(time.Now())
	mockFS := &MockFS{}

	progressCh := make(chan ProgressEvent, 1)
	behavior := &MockHandleBehavior{
		ProgressChan: progressCh,
		WaitErr:      nil, // Success
		WaitBlocks:   false,
	}
	runner := NewMockRunner(nil, behavior)

	spec := Spec{
		WorkDir:    "/tmp/success-test",
		OutputTemp: "output.m3u8",
	}

	mon := NewBuildMonitor(BuildMonitorConfig{
		JobID:     "test-success",
		Spec:      spec,
		FinalPath: "/recordings/final.m3u8",
		Runner:    runner,
		Clock:     clock,
		FS:        mockFS,
	})
	mockFS.SetExists("/tmp/success-test/output.m3u8", true)

	// Emit progress to prevent stall
	progressCh <- ProgressEvent{}

	mon.Run(ctx)

	// Assert Rename was called exactly once with correct paths
	renameCalls := mockFS.GetRenameCalls()
	require.Len(t, renameCalls, 1, "Rename should be called exactly once on success")
	assert.Equal(t, "/tmp/success-test/output.m3u8", renameCalls[0].Old)
	assert.Equal(t, "/recordings/final.m3u8", renameCalls[0].New)

	// Assert final state is Succeeded
	status := mon.GetStatus()
	assert.Equal(t, StateSucceeded, status.State)
}

// TestVOD_AtomicPublish_Failure_NoFinal validates no final output on failure.
func TestVOD_AtomicPublish_Failure_NoFinal(t *testing.T) {
	ctx := context.Background()
	clock := NewMockClock(time.Now())
	mockFS := &MockFS{}

	crashErr := errors.New("crash during build")
	behavior := &MockHandleBehavior{
		ProgressChan: make(chan ProgressEvent),
		WaitErr:      crashErr,
		WaitBlocks:   false,
	}
	runner := NewMockRunner(nil, behavior)

	mon := NewBuildMonitor(BuildMonitorConfig{
		JobID:     "test-failure",
		Spec:      Spec{WorkDir: "/tmp/failure-test", OutputTemp: "output.m3u8"},
		FinalPath: "/recordings/final-fail.m3u8",
		Runner:    runner,
		Clock:     clock,
		FS:        mockFS,
	})
	mockFS.SetExists("/tmp/failure-test/output.m3u8", true)

	mon.Run(ctx)

	// Assert Rename was NOT called
	renameCalls := mockFS.GetRenameCalls()
	assert.Empty(t, renameCalls, "Rename should NOT be called on failure")

	// Assert cleanup was attempted
	removeAllCalls := mockFS.GetRemoveAllCalls()
	assert.Contains(t, removeAllCalls, "/tmp/failure-test")
}
