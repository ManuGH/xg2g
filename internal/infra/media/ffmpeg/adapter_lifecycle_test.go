package ffmpeg

import (
	"context"
	"io"
	"os/exec"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/procgroup"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMonitorProcess_RemovesHandleOnNaturalExit(t *testing.T) {
	adapter := NewLocalAdapter(
		"ffmpeg",
		"",
		t.TempDir(),
		nil,
		zerolog.New(io.Discard),
		"",
		"",
		0,
		0,
		false,
		2*time.Second,
		6,
		5*time.Second,
		5*time.Second,
		"",
	)

	cmd := exec.Command("sh", "-c", "echo out_time_ms=1 1>&2")
	stderr, err := cmd.StderrPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())

	handle := ports.RunHandle("session-1-123")
	adapter.mu.Lock()
	adapter.activeProcs[handle] = cmd
	adapter.mu.Unlock()

	done := make(chan struct{})
	go func() {
		adapter.monitorProcess(context.Background(), handle, cmd, stderr, "session-1")
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("monitorProcess did not finish in time")
	}

	status := adapter.Health(context.Background(), handle)
	assert.False(t, status.Healthy)
	assert.Equal(t, "process not found", status.Message)

	adapter.mu.Lock()
	_, exists := adapter.activeProcs[handle]
	adapter.mu.Unlock()
	assert.False(t, exists)
}

func TestHealth_ExitedProcessInMapIsUnhealthyAndCleanedUp(t *testing.T) {
	adapter := NewLocalAdapter(
		"ffmpeg",
		"",
		t.TempDir(),
		nil,
		zerolog.New(io.Discard),
		"",
		"",
		0,
		0,
		false,
		2*time.Second,
		6,
		5*time.Second,
		5*time.Second,
		"",
	)

	cmd := exec.Command("sh", "-c", "exit 0")
	require.NoError(t, cmd.Start())
	require.NoError(t, cmd.Wait())

	handle := ports.RunHandle("session-2-456")
	adapter.mu.Lock()
	adapter.activeProcs[handle] = cmd
	adapter.mu.Unlock()

	status := adapter.Health(context.Background(), handle)
	assert.False(t, status.Healthy)
	assert.Equal(t, "process exited", status.Message)

	adapter.mu.Lock()
	_, exists := adapter.activeProcs[handle]
	adapter.mu.Unlock()
	assert.False(t, exists)
}

func TestMonitorProcess_WatchdogTimeoutKillsProcess(t *testing.T) {
	adapter := NewLocalAdapter(
		"ffmpeg",
		"",
		t.TempDir(),
		nil,
		zerolog.New(io.Discard),
		"",
		"",
		0,
		1*time.Second,
		false,
		2*time.Second,
		6,
		100*time.Millisecond,
		100*time.Millisecond,
		"",
	)

	cmd := exec.Command("sh", "-c", "sleep 10")
	procgroup.Set(cmd)
	stderr, err := cmd.StderrPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())

	handle := ports.RunHandle("session-timeout-1")
	adapter.mu.Lock()
	adapter.activeProcs[handle] = cmd
	adapter.mu.Unlock()

	start := time.Now()
	done := make(chan struct{})
	go func() {
		adapter.monitorProcess(context.Background(), handle, cmd, stderr, "session-timeout")
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("monitorProcess did not stop stalled process in time")
	}

	assert.Less(t, time.Since(start), 5*time.Second)
	status := adapter.Health(context.Background(), handle)
	assert.False(t, status.Healthy)
}
