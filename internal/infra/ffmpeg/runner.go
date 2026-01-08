package ffmpeg

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ManuGH/xg2g/internal/control/vod"
	"github.com/rs/zerolog"
)

// Ensure Executor implements vod.Runner
var _ vod.Runner = (*Executor)(nil)

// Executor implements the vod.Runner interface.
type Executor struct {
	BinaryPath string
	Logger     zerolog.Logger
}

func NewExecutor(binaryPath string, logger zerolog.Logger) *Executor {
	if binaryPath == "" {
		binaryPath = "ffmpeg"
	}
	return &Executor{
		BinaryPath: binaryPath,
		Logger:     logger,
	}
}

func (e *Executor) Start(ctx context.Context, spec vod.Spec) (vod.Handle, error) {
	args := mapProfileToArgs(spec)

	cmd := exec.CommandContext(ctx, e.BinaryPath, args...)
	// Set WorkDir context if needed, but we pass absolute paths usually.

	// Capture pipes
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to pipe stderr: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("exec start failed: %w", err)
	}

	h := &handle{
		cmd:      cmd,
		progress: make(chan vod.ProgressEvent, 10), // Buffered to prevent blocking
		done:     make(chan error, 1),
		ring:     NewRingBuffer(100),
	}

	// Start monitor
	go h.monitor(stderr)

	return h, nil
}

type handle struct {
	cmd      *exec.Cmd
	progress chan vod.ProgressEvent
	done     chan error
	ring     *RingBuffer
	mu       sync.Mutex
}

func (h *handle) Wait() error {
	return <-h.done
}

func (h *handle) Stop(grace, kill time.Duration) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.cmd.Process == nil {
		return nil
	}

	// Graceful
	_ = h.cmd.Process.Signal(syscall.SIGTERM)

	// Deadline for Kill
	if kill > 0 {
		time.AfterFunc(kill, func() {
			_ = h.cmd.Process.Kill()
		})
	}

	return nil // Actual exit observed via Wait()
}

func (h *handle) Progress() <-chan vod.ProgressEvent {
	return h.progress
}

func (h *handle) Diagnostics() []string {
	return h.ring.GetAll()
}

func (h *handle) monitor(stderr io.Reader) {
	defer close(h.done)
	defer close(h.progress)

	// Scanning stderr for progress
	scanner := bufio.NewScanner(stderr)
	// Default split function is ScanLines

	for scanner.Scan() {
		line := scanner.Text()
		h.ring.Add(line)

		// Heuristic: If line contains frame= or size=, emit heartbeat
		if strings.Contains(line, "frame=") || strings.Contains(line, "size=") || strings.Contains(line, "time=") {
			select {
			case h.progress <- vod.ProgressEvent{}:
			default:
				// Dropped heartbeat is fine, we just need frequent enough ones
			}
		}
	}

	h.done <- h.cmd.Wait()
}

// RingBuffer simple implementation
type RingBuffer struct {
	lines []string
	pos   int
	full  bool
	mu    sync.Mutex
}

func NewRingBuffer(size int) *RingBuffer {
	return &RingBuffer{lines: make([]string, size)}
}

func (r *RingBuffer) Add(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines[r.pos] = line
	r.pos = (r.pos + 1) % len(r.lines)
	if r.pos == 0 {
		r.full = true
	}
}

func (r *RingBuffer) GetAll() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.full {
		return append([]string(nil), r.lines[:r.pos]...)
	}
	// Reorder
	res := make([]string, len(r.lines))
	copy(res, r.lines[r.pos:])
	copy(res[len(r.lines)-r.pos:], r.lines[:r.pos])
	return res
}
