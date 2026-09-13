// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

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

// Seen on staging 2026-09-12 (Sky Sport F1, session e11ae2ab): the broadcaster
// dropped the second audio PID from the PMT mid-stream. The encoder kept
// producing 50 frames/s and the muxer kept opening video and audio-1 segments,
// but FFmpeg's reported out_time_ms is the trailing (minimum) dts across all
// output streams, so it froze on the starved rendition. The watchdog counted
// only out_time_ms and killed a session that was still delivering.
//
// This test is the negative control for that: out_time_ms advances once, then
// only segment opens keep arriving. Before the fix the watchdog killed the
// process as stalled; with it, every segment the muxer opens is progress.
func TestMonitorProcess_SegmentOpensAreProgressWhenOutTimeFreezes(t *testing.T) {
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

	// One out_time_ms tick moves the watchdog to Running, then out_time_ms
	// never advances again while the muxer keeps opening segments for ~3s,
	// i.e. well past the 1.1s stall timeout.
	cmd := exec.Command("sh", "-c",
		`printf 'out_time_ms=1\n' 1>&2; i=0; while [ $i -lt 10 ]; do printf "Opening '/tmp/seg_0_%06d.m4s.tmp' for writing\n" $i 1>&2; sleep 0.3; i=$((i+1)); done; exit 0`)
	procgroup.Set(cmd)
	stderr, err := cmd.StderrPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())

	handle := ports.RunHandle("session-segment-progress")
	adapter.mu.Lock()
	adapter.activeProcs[handle] = cmd
	adapter.mu.Unlock()

	done := make(chan struct{})
	go func() {
		adapter.monitorProcess(context.Background(), handle, cmd, stderr, "session-segment-progress", false)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(8 * time.Second):
		t.Fatal("monitorProcess did not finish in time")
	}

	status := adapter.Health(context.Background(), handle)
	assert.NotEqual(t, "transcode stalled - no progress detected", status.Message,
		"a muxer that keeps opening segments is not stalled, whatever out_time_ms says")
}
