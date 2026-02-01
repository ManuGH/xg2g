package ffmpeg

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/vod"
	"github.com/ManuGH/xg2g/internal/media/ffmpeg/watchdog"
	"github.com/ManuGH/xg2g/internal/procgroup"
	"github.com/rs/zerolog"
)

// Ensure Executor implements vod.Runner
var _ vod.Runner = (*Executor)(nil)

// Executor implements the vod.Runner interface.
type Executor struct {
	BinaryPath   string
	Logger       zerolog.Logger
	StartTimeout time.Duration
	StallTimeout time.Duration
}

func NewExecutor(binaryPath string, logger zerolog.Logger, startTimeout, stallTimeout time.Duration) *Executor {
	if binaryPath == "" {
		binaryPath = "ffmpeg"
	}
	if startTimeout <= 0 {
		startTimeout = 10 * time.Second
	}
	if stallTimeout <= 0 {
		stallTimeout = 30 * time.Second
	}
	return &Executor{
		BinaryPath:   binaryPath,
		Logger:       logger,
		StartTimeout: startTimeout,
		StallTimeout: stallTimeout,
	}
}

func (e *Executor) Start(ctx context.Context, spec vod.Spec) (vod.Handle, error) {
	args, err := mapProfileToArgs(spec)
	if err != nil {
		return nil, fmt.Errorf("invalid spec: %w", err)
	}

	// #nosec G204 - BinaryPath is trusted from config; args are generated from strict profile logic
	cmd := exec.CommandContext(ctx, e.BinaryPath, args...)
	procgroup.Set(cmd) // Mandatory for group cleanup

	e.Logger.Debug().
		Str("bin", e.BinaryPath).
		Strs("args", args).
		Msg("starting ffmpeg remux process")
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
		wd:       watchdog.New(e.StartTimeout, e.StallTimeout),
		logger:   e.Logger,
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
	wd       *watchdog.Watchdog
	logger   zerolog.Logger
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

	// Use procgroup for deterministic tree reaping
	return procgroup.KillGroup(h.cmd.Process.Pid, grace, kill)
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

	// Start watchdog in background
	wdCtx, wdCancel := context.WithCancel(context.Background())
	defer wdCancel()

	wdErrCh := make(chan error, 1)
	go func() {
		wdErrCh <- h.wd.Run(wdCtx)
	}()

	// Scanning stderr for progress
	scanner := bufio.NewScanner(stderr)

	for scanner.Scan() {
		line := scanner.Text()
		h.ring.Add(line)
		h.wd.ParseLine(line)

		// Heuristic: If line contains frame= or size=, emit heartbeat for legacy consumers
		if strings.Contains(line, "frame=") || strings.Contains(line, "size=") || strings.Contains(line, "time=") {
			select {
			case h.progress <- vod.ProgressEvent{At: time.Now()}:
			default:
				// Dropped heartbeat is fine
			}
		}
	}

	// Wait for process or watchdog
	waitErr := h.cmd.Wait()
	wdCancel() // Stop watchdog if process exited first

	select {
	case wdErr := <-wdErrCh:
		if wdErr != nil {
			h.logger.Error().Err(wdErr).Int("state", int(h.wd.State())).Msg("watchdog triggered failure")
			h.done <- wdErr
			return
		}
	default:
	}

	h.done <- waitErr
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
