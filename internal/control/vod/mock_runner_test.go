package vod

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMockRunner_StartFailure validates start failure injection.
func TestMockRunner_StartFailure(t *testing.T) {
	startErr := errors.New("start failed")
	runner := NewMockRunner(startErr, nil)

	h, err := runner.Start(context.Background(), Spec{})
	assert.Error(t, err)
	assert.Equal(t, startErr, err)
	assert.Nil(t, h)
}

// TestMockRunner_Success validates successful start and controllable behaviors.
func TestMockRunner_Success(t *testing.T) {
	progressCh := make(chan ProgressEvent, 10)
	behavior := &MockHandleBehavior{
		WaitErr:      nil,
		ProgressChan: progressCh,
	}
	runner := NewMockRunner(nil, behavior)

	h, err := runner.Start(context.Background(), Spec{})
	require.NoError(t, err)
	require.NotNil(t, h)

	mockHandle := h.(*MockHandle)

	// Test progress channel (send a simple event)
	progressCh <- ProgressEvent{}
	<-h.Progress() // Receive it

	// Test Wait (success)
	err = h.Wait()
	assert.NoError(t, err)

	// Test Stop call recording
	h.Stop(2*time.Second, 5*time.Second)
	calls := mockHandle.GetStopCalls()
	require.Len(t, calls, 1)
	assert.Equal(t, 2*time.Second, calls[0].Grace)
	assert.Equal(t, 5*time.Second, calls[0].Kill)
	assert.True(t, mockHandle.WasStopped())
}

// TestMockRunner_Crash validates crash injection.
func TestMockRunner_Crash(t *testing.T) {
	crashErr := errors.New("ffmpeg crashed")
	behavior := &MockHandleBehavior{
		WaitErr: crashErr,
	}
	runner := NewMockRunner(nil, behavior)

	h, err := runner.Start(context.Background(), Spec{})
	require.NoError(t, err)

	err = h.Wait()
	assert.Equal(t, crashErr, err)
}

// TestMockRunner_StopUnblocksWait validates that Stop() unblocks Wait() when configured.
func TestMockRunner_StopUnblocksWait(t *testing.T) {
	behavior := &MockHandleBehavior{
		WaitBlocks:   true,
		StopUnblocks: true,
		WaitErr:      nil,
	}
	runner := NewMockRunner(nil, behavior)

	h, err := runner.Start(context.Background(), Spec{})
	require.NoError(t, err)

	done := make(chan error, 1)
	go func() {
		done <- h.Wait()
	}()

	// Wait should block initially
	select {
	case <-done:
		t.Fatal("Wait returned before Stop was called")
	case <-time.After(10 * time.Millisecond):
	}

	// Calling Stop should unblock Wait
	h.Stop(2*time.Second, 5*time.Second)

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Wait did not unblock after Stop")
	}
}
