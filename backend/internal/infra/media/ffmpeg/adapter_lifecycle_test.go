package ffmpeg

import (
	"bytes"
	"context"
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
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

func TestMonitorProcess_LogsStartupMarkersOnce(t *testing.T) {
	var buf bytes.Buffer
	adapter := NewLocalAdapter(
		"ffmpeg",
		"",
		t.TempDir(),
		nil,
		zerolog.New(&buf),
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

	cmd := exec.Command("sh", "-c", "printf 'frame=    1 fps=0.0 q=0.0\\rframe=    2 fps=0.0 q=0.0\\rOpening '\\''/tmp/seg_000001.m4s'\\'' for writing\\nOpening '\\''/tmp/seg_000002.m4s'\\'' for writing\\n' 1>&2")
	stderr, err := cmd.StderrPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())

	handle := ports.RunHandle("session-3-789")
	adapter.mu.Lock()
	adapter.activeProcs[handle] = cmd
	adapter.mu.Unlock()

	adapter.monitorProcess(context.Background(), handle, cmd, stderr, "session-3")

	logs := buf.String()
	assert.Equal(t, 1, strings.Count(logs, `"startup_phase":"first_frame"`))
	assert.Equal(t, 1, strings.Count(logs, `"startup_phase":"first_segment_write"`))
	assert.Contains(t, logs, `"frame":1`)
	assert.Contains(t, logs, `"segment_path":"/tmp/seg_000001.m4s"`)
}
