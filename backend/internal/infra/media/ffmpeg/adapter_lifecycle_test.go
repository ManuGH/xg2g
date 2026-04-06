package ffmpeg

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/pipeline/hardware"
	"github.com/ManuGH/xg2g/internal/procgroup"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func prepareVAAPIRuntimeState(t *testing.T) {
	t.Helper()
	hardware.SetVAAPIPreflightResult(true)
	hardware.SetVAAPIEncoderPreflight(map[string]bool{
		"h264_vaapi": true,
	})
	t.Cleanup(func() {
		hardware.SetVAAPIPreflightResult(false)
		hardware.SetVAAPIEncoderCapabilities(nil)
	})
}

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
		adapter.monitorProcess(context.Background(), handle, cmd, stderr, "session-1", false)
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

func TestMonitorProcess_SurfacesMeaningfulExitDetail(t *testing.T) {
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

	cmd := exec.Command("sh", "-c", "printf '[http @ 0x1] Stream ends prematurely at 0\\nError opening input files: Input/output error\\n' 1>&2; exit 1")
	stderr, err := cmd.StderrPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())

	handle := ports.RunHandle("session-1b-321")
	adapter.mu.Lock()
	adapter.activeProcs[handle] = cmd
	adapter.mu.Unlock()

	done := make(chan struct{})
	go func() {
		adapter.monitorProcess(context.Background(), handle, cmd, stderr, "session-1b", false)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("monitorProcess did not finish in time")
	}

	status := adapter.Health(context.Background(), handle)
	assert.False(t, status.Healthy)
	assert.Equal(t, "upstream stream ended prematurely", status.Message)
}

func TestMonitorProcess_KillsStalledProcessAndPreservesStallDetail(t *testing.T) {
	adapter := NewLocalAdapter(
		"ffmpeg",
		"",
		t.TempDir(),
		nil,
		zerolog.New(io.Discard),
		"",
		"",
		0,
		1500*time.Millisecond,
		false,
		2*time.Second,
		6,
		5*time.Second,
		1100*time.Millisecond,
		"",
	)

	cmd := exec.Command("sh", "-c", "printf 'out_time_ms=1\\n' 1>&2; exec sleep 30")
	procgroup.Set(cmd)
	stderr, err := cmd.StderrPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())

	handle := ports.RunHandle("session-stall-123")
	adapter.mu.Lock()
	adapter.activeProcs[handle] = cmd
	adapter.mu.Unlock()

	done := make(chan struct{})
	go func() {
		adapter.monitorProcess(context.Background(), handle, cmd, stderr, "session-stall", false)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(6 * time.Second):
		t.Fatal("monitorProcess did not terminate stalled process in time")
	}

	status := adapter.Health(context.Background(), handle)
	assert.False(t, status.Healthy)
	assert.Equal(t, "transcode stalled - no progress detected", status.Message)

	if err := cmd.Process.Signal(syscall.Signal(0)); err == nil {
		t.Fatal("stalled process still running after watchdog timeout")
	}
}

func TestMonitorProcess_FirstFramePreventsStartupTimeout(t *testing.T) {
	adapter := NewLocalAdapter(
		"ffmpeg",
		"",
		t.TempDir(),
		nil,
		zerolog.New(io.Discard),
		"",
		"",
		0,
		1500*time.Millisecond,
		false,
		2*time.Second,
		6,
		150*time.Millisecond,
		1200*time.Millisecond,
		"",
	)

	cmd := exec.Command("sh", "-c", "printf 'frame=    1\\n' 1>&2; sleep 0.3")
	stderr, err := cmd.StderrPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())

	handle := ports.RunHandle("session-startup-frame-123")
	adapter.mu.Lock()
	adapter.activeProcs[handle] = cmd
	adapter.mu.Unlock()

	done := make(chan struct{})
	go func() {
		adapter.monitorProcess(context.Background(), handle, cmd, stderr, "session-startup-frame", false)
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
}

func TestHealth_MonitorRemovedHandleIsUnhealthy(t *testing.T) {
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

	handle := ports.RunHandle("session-2-456")

	// Handle not in activeProcs → monitorProcess has finished.
	status := adapter.Health(context.Background(), handle)
	assert.False(t, status.Healthy)
	assert.Equal(t, "process not found", status.Message)
}

func TestHealth_MonitorRemovedHandleReturnsDetail(t *testing.T) {
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

	handle := ports.RunHandle("session-3-789")
	adapter.recordProcessDetail(handle, "copy output missing codec parameters")

	// monitorProcess finished (handle removed) but detail was recorded.
	status := adapter.Health(context.Background(), handle)
	assert.False(t, status.Healthy)
	assert.Equal(t, "copy output missing codec parameters", status.Message)
}

func TestHealth_ActiveHandleIsHealthy(t *testing.T) {
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

	handle := ports.RunHandle("session-4-101")
	adapter.mu.Lock()
	adapter.activeProcs[handle] = cmd
	adapter.mu.Unlock()

	// Handle still in activeProcs → monitorProcess not finished → healthy.
	status := adapter.Health(context.Background(), handle)
	assert.True(t, status.Healthy)
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

	stderr, writer, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = stderr.Close()
		_ = writer.Close()
	})

	cmd := exec.Command("sh", "-c", "sleep 0.1")
	require.NoError(t, cmd.Start())
	go func() {
		defer writer.Close()
		_, _ = io.WriteString(writer, "frame=    1 fps=0.0 q=0.0\rframe=    2 fps=0.0 q=0.0\rOpening '/tmp/seg_000001.m4s' for writing\nOpening '/tmp/seg_000002.m4s' for writing\n")
	}()

	handle := ports.RunHandle("session-3-789")
	adapter.mu.Lock()
	adapter.activeProcs[handle] = cmd
	adapter.mu.Unlock()

	adapter.monitorProcess(context.Background(), handle, cmd, stderr, "session-3", false)

	logs := buf.String()
	assert.Equal(t, 1, strings.Count(logs, `"startup_phase":"first_frame"`))
	assert.Equal(t, 1, strings.Count(logs, `"startup_phase":"first_segment_write"`))
	assert.Contains(t, logs, `"frame":1`)
	assert.Contains(t, logs, `"segment_path":"/tmp/seg_000001.m4s"`)
	markerPath := filepath.Join(adapter.HLSRoot, "sessions", "session-3", model.SessionFirstFrameMarkerFilename)
	marker, err := os.ReadFile(markerPath)
	require.NoError(t, err)
	assert.NotEmpty(t, strings.TrimSpace(string(marker)))
}

func TestMonitorProcess_RecordsVAAPIRuntimeFailureForVAAPIError(t *testing.T) {
	prepareVAAPIRuntimeState(t)

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

	cmd := exec.Command("sh", "-c", "printf '[h264_vaapi @ 0x1] Failed to end picture encode issue: 23 (internal encoding error).\\n' 1>&2; exit 1")
	stderr, err := cmd.StderrPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())

	handle := ports.RunHandle("session-vaapi-runtime-123")
	adapter.mu.Lock()
	adapter.activeProcs[handle] = cmd
	adapter.mu.Unlock()

	done := make(chan struct{})
	go func() {
		adapter.monitorProcess(context.Background(), handle, cmd, stderr, "session-vaapi-runtime", true)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("monitorProcess did not finish in time")
	}

	_, demoted := hardware.RecordVAAPIRuntimeFailure()
	require.False(t, demoted, "runtime failure should already have been counted once")
	_, demoted = hardware.RecordVAAPIRuntimeFailure()
	require.True(t, demoted, "third observed runtime failure should demote VAAPI readiness")
	assert.False(t, hardware.IsVAAPIReady())
}

func TestMonitorProcess_DoesNotRecordVAAPIRuntimeFailureForGenericExit(t *testing.T) {
	prepareVAAPIRuntimeState(t)

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

	cmd := exec.Command("sh", "-c", "printf 'Error opening input files: Input/output error\\n' 1>&2; exit 1")
	stderr, err := cmd.StderrPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())

	handle := ports.RunHandle("session-generic-runtime-123")
	adapter.mu.Lock()
	adapter.activeProcs[handle] = cmd
	adapter.mu.Unlock()

	done := make(chan struct{})
	go func() {
		adapter.monitorProcess(context.Background(), handle, cmd, stderr, "session-generic-runtime", true)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("monitorProcess did not finish in time")
	}

	_, demoted := hardware.RecordVAAPIRuntimeFailure()
	require.False(t, demoted)
	_, demoted = hardware.RecordVAAPIRuntimeFailure()
	require.False(t, demoted, "generic upstream exits must not consume the VAAPI demotion budget")
	assert.True(t, hardware.IsVAAPIReady())
}
