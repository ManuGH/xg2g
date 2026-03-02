package ffmpeg

import (
	"context"
	"io"
	"os/exec"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/vod"
	"github.com/ManuGH/xg2g/internal/media/ffmpeg/watchdog"
	"github.com/ManuGH/xg2g/internal/procgroup"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleMonitor_WatchdogTimeoutKillsProcess(t *testing.T) {
	cmd := exec.Command("sh", "-c", "sleep 10")
	procgroup.Set(cmd)
	stderr, err := cmd.StderrPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())

	h := &handle{
		cmd:      cmd,
		progress: make(chan vod.ProgressEvent, 10),
		done:     make(chan error, 1),
		ring:     NewRingBuffer(32),
		wd:       watchdog.New(100*time.Millisecond, 100*time.Millisecond),
		logger:   zerolog.New(io.Discard),
		ctx:      context.Background(),
	}

	go h.monitor(stderr)

	select {
	case err := <-h.done:
		require.Error(t, err)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
	case <-time.After(5 * time.Second):
		t.Fatal("monitor did not terminate stalled process in time")
	}
}

func TestHandleMonitor_ContextCancelKillsProcess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", "sleep 10")
	procgroup.Set(cmd)
	stderr, err := cmd.StderrPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())

	h := &handle{
		cmd:      cmd,
		progress: make(chan vod.ProgressEvent, 10),
		done:     make(chan error, 1),
		ring:     NewRingBuffer(32),
		wd:       watchdog.New(10*time.Second, 10*time.Second),
		logger:   zerolog.New(io.Discard),
		ctx:      ctx,
	}

	go h.monitor(stderr)
	cancel()

	select {
	case err := <-h.done:
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("monitor did not terminate canceled process in time")
	}
}

func TestHandleMonitor_NormalExit(t *testing.T) {
	cmd := exec.Command("sh", "-c", "echo out_time_ms=1 1>&2")
	procgroup.Set(cmd)
	stderr, err := cmd.StderrPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())

	h := &handle{
		cmd:      cmd,
		progress: make(chan vod.ProgressEvent, 10),
		done:     make(chan error, 1),
		ring:     NewRingBuffer(32),
		wd:       watchdog.New(10*time.Second, 10*time.Second),
		logger:   zerolog.New(io.Discard),
		ctx:      context.Background(),
	}

	go h.monitor(stderr)

	select {
	case err := <-h.done:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("monitor did not return on normal process exit")
	}
}
