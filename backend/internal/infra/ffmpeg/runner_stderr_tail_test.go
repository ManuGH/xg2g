package ffmpeg

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/vod"
	"github.com/ManuGH/xg2g/internal/infra/ffmpeg/watchdog"
	"github.com/ManuGH/xg2g/internal/procgroup"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

// stderrTailMarker is the unique LAST line the helper writes before exiting. The whole point
// of the test is that this exact tail survives into Diagnostics.
const stderrTailMarker = "XG2G-STDERR-TAIL-MARKER-d4e7c1a9"

// growPipeViaSyscallConn enlarges the kernel buffer of the pipe behind f without disturbing
// its runtime-poller association — using SyscallConn().Control, NOT f.Fd() (which would move
// the file to blocking mode and change the read path relative to production). The scanner
// (bufio over this same *os.File) therefore exercises the exact prod read path; only the
// buffer size is changed up front.
func growPipeViaSyscallConn(f *os.File, size int) error {
	rc, err := f.SyscallConn()
	if err != nil {
		return err
	}
	var ctrlErr error
	if err := rc.Control(func(fd uintptr) { ctrlErr = growPipeBuffer(fd, size) }); err != nil {
		return err
	}
	return ctrlErr
}

// TestHelperProcess is the hermetic child invoked by TestHandleMonitor_StderrTailNotTruncated
// via the Go stdlib helper-process pattern (no shell, no separate build). When the gate env
// var is unset it is a no-op; when set it emits a large stderr burst ending with
// stderrTailMarker, then exits immediately.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	// Buffered so the writer is fast and races ahead of the slower line-by-line scanner; the
	// enlarged pipe buffer (set by the parent) lets it leave a large unread tail at exit.
	w := bufio.NewWriterSize(os.Stderr, 1<<16)
	for i := 0; i < 20000; i++ {
		fmt.Fprintf(w, "ffmpeg-stderr-noise-line-%06d-padddddddddddddddddddddding\n", i)
	}
	fmt.Fprintln(w, stderrTailMarker)
	_ = w.Flush()
	os.Exit(0)
}

// TestHandleMonitor_StderrTailNotTruncated is L18's RED control. It drives the real monitor
// against a real exec.Cmd with a real StderrPipe — the only configuration in which the bug
// (cmd.Wait closing the pipe read-end before the scanner drains it) actually exists. The read
// path is byte-identical to production: bufio.Scanner over the *os.File from cmd.StderrPipe(),
// fed to h.monitor; only the pipe buffer is enlarged up front so the writer races far ahead
// and leaves a large unread tail at exit.
//
// PLATFORM SCOPE: this is a Linux-specific negative control. The losing condition (reader far
// behind) requires a resizable pipe buffer (F_SETPIPE_SZ); macOS caps pipes at ~64KB and the
// prod scanner, paced by backpressure, stays caught up there, so the truncation is not
// reproducible and the test SKIPS. The platform-INDEPENDENT justification for the fix is the
// os/exec contract ("it is incorrect to call Wait before all reads from the pipe have
// completed") and structural parity with the already-correct sibling adapter_process.go.
//
// Without the fix (the scanDone gate on the Wait goroutine), cmd.Wait reaps the just-exited
// process and closes the pipe while the scanner is still draining the large unread tail, so
// the marker is lost — RED. With the fix, Wait waits for the scanner's natural EOF first, so
// every byte is read and the marker is present in EVERY iteration (a deterministic guarantee).
func TestHandleMonitor_StderrTailNotTruncated(t *testing.T) {
	const (
		iterations = 20
		pipeSize   = 1 << 20 // 1 MiB
	)

	// Probe resize support once on a throwaway pipe; skip cleanly where it is unsupported.
	probeR, probeW, err := os.Pipe()
	require.NoError(t, err)
	supportErr := growPipeViaSyscallConn(probeR, pipeSize)
	_ = probeR.Close()
	_ = probeW.Close()
	if supportErr != nil {
		t.Skipf("cannot enlarge the stderr pipe buffer here (%v); the reader-falls-behind condition that triggers the truncation is not reproducible — see os/exec Wait contract and sibling adapter_process.go for the platform-independent justification", supportErr)
	}

	for i := 0; i < iterations; i++ {
		cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$")
		cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
		procgroup.Set(cmd)

		rc, err := cmd.StderrPipe()
		require.NoError(t, err)
		f, ok := rc.(*os.File)
		require.True(t, ok, "cmd.StderrPipe must return an *os.File so we can enlarge its pipe buffer")
		require.NoError(t, growPipeViaSyscallConn(f, pipeSize))

		require.NoError(t, cmd.Start())

		h := &handle{
			cmd:      cmd,
			progress: make(chan vod.ProgressEvent, 10),
			done:     make(chan error, 1),
			ring:     NewRingBuffer(256),
			wd:       watchdog.New(30*time.Second, 30*time.Second),
			logger:   zerolog.Nop(),
			ctx:      context.Background(),
		}

		// monitor owns cmd.Wait(); the test must not call it. f is the same object StderrPipe
		// returned, so the read path matches prod exactly.
		go h.monitor(f)
		_ = h.Wait()

		diags := strings.Join(h.Diagnostics(), "\n")
		require.Contains(t, diags, stderrTailMarker,
			"iteration %d: stderr tail (the final line before exit) was truncated from Diagnostics — cmd.Wait closed the pipe before the scanner drained it", i)
	}
}
