// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ffmpeg

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/v3/model"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	startTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "v3_ffmpeg_start_total",
		Help: "Total number of ffmpeg process starts",
	}, []string{"result"})

	exitTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "v3_ffmpeg_exit_total",
		Help: "Total number of ffmpeg process exits",
	}, []string{"reason"})
)

// Runner manages a single ffmpeg process.
type Runner struct {
	BinPath string
	HLSRoot string
	E2Host  string
	Args    []string // Default args or template

	cmd   *exec.Cmd
	start time.Time

	ring *LineRing

	mu     sync.Mutex
	status *model.ExitStatus // Cached status once exited
}

// NewRunner creates a runner.
func NewRunner(binPath, hlsRoot, e2Host string) *Runner {
	if binPath == "" {
		binPath = "ffmpeg"
	}
	return &Runner{
		BinPath: binPath,
		HLSRoot: hlsRoot,
		E2Host:  e2Host,
		ring:    NewLineRing(100),
	}
}

// Start launches the process.
func (r *Runner) Start(ctx context.Context, sessionID, serviceRef string, profile model.ProfileID) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.cmd != nil {
		return fmt.Errorf("process already started")
	}

	// 1. Prepare Layout
	sessionDir := SessionOutputDir(r.HLSRoot, sessionID)
	// mkdir -p
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		startTotal.WithLabelValues("err_mkdir").Inc()
		return fmt.Errorf("failed to create session dir: %w", err)
	}

	tmpPlaylist, _ := PlaylistPaths(sessionDir)

	// 2. Build Args
	in := InputSpec{
		StreamURL: fmt.Sprintf("%s/%s", r.E2Host, serviceRef), // E2Host is base e.g. http://localhost:8001
	}

	// Profile Configuration (Stubbed selection logic)
	out := OutputSpec{
		HLSPlaylist:        tmpPlaylist,
		SegmentFilename:    SegmentPattern(sessionDir),
		SegmentDuration:    6,
		PlaylistWindowSize: 5,
	}

	// In real impl, checking profile ID would affect codec flags
	profSpec := model.ProfileSpec{Name: string(profile)}

	args, err := BuildHLSArgs(in, out, profSpec)
	if err != nil {
		return err
	}

	if profile == "sleep_test" { // For testing
		r.BinPath = "sleep"
		args = []string{"10"}
	}

	r.cmd = exec.CommandContext(ctx, r.BinPath, args...)

	// Capture Stderr
	stderr, err := r.cmd.StderrPipe()
	if err != nil {
		startTotal.WithLabelValues("err").Inc()
		return err
	}

	// Start Log Consumer
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			r.ring.Write(scanner.Bytes())
			r.ring.Write([]byte("\n"))
		}
	}()

	// Start Playlist Atomicity Loop (Background)
	if profile != "sleep_test" {
		go r.syncPlaylistLoop(ctx, sessionDir)
	}

	r.start = time.Now()
	if err := r.cmd.Start(); err != nil {
		startTotal.WithLabelValues("err").Inc()
		return fmt.Errorf("ffmpeg start failed: %w", err)
	}

	startTotal.WithLabelValues("ok").Inc()
	log.L().Debug().Str("bin", r.BinPath).Str("sess", sessionID).Msg("ffmpeg started")
	return nil
}

// syncPlaylistLoop watches for valid .tmp playlist and promotes it to final .m3u8 atomically.
func (r *Runner) syncPlaylistLoop(ctx context.Context, dir string) {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	tmpPath, finalPath := PlaylistPaths(dir)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Check if tmp exists
			info, err := os.Stat(tmpPath)
			if err != nil {
				continue // not yet
			}
			if info.Size() == 0 {
				continue // empty
			}

			// Atomic Rename (Move)
			// This works because FFmpeg recreates the file on next update (standard HLS Muxer behavior).
			// If we restricted FFmpeg to keep fd open, this would break. But HLS Muxer closes file.
			if err := os.Rename(tmpPath, finalPath); err != nil {
				log.L().Warn().Err(err).Msg("failed to sync playlist")
			}
		}
	}
}

// Wait blocks until exit.
func (r *Runner) Wait(ctx context.Context) (model.ExitStatus, error) {
	// Wait for cmd.Wait()
	// Note: cmd.Wait() closes pipes.

	err := r.cmd.Wait()
	r.mu.Lock()
	defer r.mu.Unlock()

	end := time.Now()
	code := 0
	reason := "clean"

	if err != nil {
		code = 1 // Default error
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		}

		// Check cancellation
		select {
		case <-ctx.Done():
			reason = "ctx_cancel"
		default:
			reason = "error"
		}
	} else {
		select {
		case <-ctx.Done():
			reason = "ctx_cancel"
		default:
		}
	}

	// Populate status
	status := model.ExitStatus{
		Code:      code,
		Reason:    reason,
		StartedAt: r.start,
		EndedAt:   end,
	}
	r.status = &status

	exitTotal.WithLabelValues(reason).Inc()
	return status, err
}

// Stop terminates the process.
func (r *Runner) Stop(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.cmd == nil || r.cmd.Process == nil {
		return nil // Already stopped or not started
	}

	// Double check if already exited
	if r.cmd.ProcessState != nil && r.cmd.ProcessState.Exited() {
		return nil
	}

	// Send SIGTERM
	log.L().Debug().Msg("sending SIGTERM to ffmpeg")
	if err := r.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		// If error (e.g. process gone), just return
		return err
	}

	// In a real runner, we might wait specific time then SIGKILL.
	// But exec.CommandContext handles SIGKILL on context cancel.
	// If caller cancels ctx passed to Start(), it kills.
	// Here we just signal. Wait() is called by Orchestrator.

	return nil
}

func (r *Runner) LastLogLines(n int) []string {
	return r.ring.LastN(n)
}
