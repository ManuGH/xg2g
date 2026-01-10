package vod

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBuildMonitor_BuildingRequiresRunnerArtifacts(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mockFS := &MockFS{}
	behavior := &MockHandleBehavior{
		ProgressChan: make(chan ProgressEvent),
		WaitBlocks:   true,
	}
	runner := NewMockRunner(nil, behavior)

	spec := Spec{
		WorkDir:    "/tmp/contract-test",
		OutputTemp: "output.m3u8",
	}
	mon := NewBuildMonitor(BuildMonitorConfig{
		JobID:  "contract-job",
		Spec:   spec,
		Runner: runner,
		Clock:  RealClock{},
		FS:     mockFS,
	})

	done := make(chan struct{})
	go func() {
		mon.Run(ctx)
		close(done)
	}()

	time.Sleep(20 * time.Millisecond)
	require.Equal(t, StateIdle, mon.GetStatus().State)

	mockFS.SetExists("/tmp/contract-test/output.m3u8", true)

	require.Eventually(t, func() bool {
		return mon.GetStatus().State == StateBuilding
	}, BuildStartTimeout, 10*time.Millisecond)

	cancel()
	<-done
}

func TestBuildMonitor_ContractViolationFails(t *testing.T) {
	ctx := context.Background()

	mockFS := &MockFS{}
	behavior := &MockHandleBehavior{
		ProgressChan: make(chan ProgressEvent),
		WaitBlocks:   true,
	}
	runner := NewMockRunner(nil, behavior)

	spec := Spec{
		WorkDir:    "/tmp/contract-violation",
		OutputTemp: "output.m3u8",
	}
	mon := NewBuildMonitor(BuildMonitorConfig{
		JobID:  "contract-fail",
		Spec:   spec,
		Runner: runner,
		Clock:  RealClock{},
		FS:     mockFS,
	})

	done := make(chan struct{})
	go func() {
		mon.Run(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(BuildStartTimeout + 150*time.Millisecond):
		t.Fatal("expected monitor to fail on runner contract violation")
	}

	status := mon.GetStatus()
	require.Equal(t, StateFailed, status.State)
	require.Equal(t, ReasonContract, status.Reason)
}
