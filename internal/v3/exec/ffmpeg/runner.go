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
	"github.com/ManuGH/xg2g/internal/v3/exec/enigma2"
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
	Client  *enigma2.Client
	Args    []string // Default args or template

	cmd   *exec.Cmd
	start time.Time

	ring *LineRing

	mu     sync.Mutex
	status *model.ExitStatus // Cached status once exited
}

// NewRunner creates a new FFmpeg runner.
func NewRunner(binPath, hlsRoot string, client *enigma2.Client) *Runner {
	if binPath == "" {
		binPath = "ffmpeg"
	}
	return &Runner{
		BinPath: binPath,
		HLSRoot: hlsRoot,
		Client:  client,
		ring:    NewLineRing(256), // Keep last 256 lines of stderr
	}
}

// Start launches the process.
func (r *Runner) Start(ctx context.Context, sessionID, serviceRef string, profile model.ProfileID) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.cmd != nil {
		return fmt.Errorf("process already started")
	}
	if !model.IsSafeSessionID(sessionID) {
		startTotal.WithLabelValues("err_bad_session").Inc()
		return fmt.Errorf("invalid session id: %s", sessionID)
	}

	// 1. Prepare Layout
	sessionDir := SessionOutputDir(r.HLSRoot, sessionID)
	// mkdir -p
	// #nosec G301 -- 0755 required for serving files via web/nginx
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		startTotal.WithLabelValues("err_mkdir").Inc()
		return fmt.Errorf("failed to create session dir: %w", err)
	}

	tmpPlaylist, _ := PlaylistPaths(sessionDir)

	// 2. Build Args
	// Profile Configuration
	// Phase 9-5: Safari DVR / fMP4 Logic
	isFMP4 := false
	ext := ".ts"

	if profile == "safari_dvr" {
		isFMP4 = true
		ext = ".m4s"
	}

	// 2. Build Args
	streamURL, err := r.Client.ResolveStreamURL(ctx, serviceRef)
	if err != nil {
		startTotal.WithLabelValues("err_url").Inc()
		return fmt.Errorf("failed to resolve stream url: %w", err)
	}

	in := InputSpec{
		StreamURL: streamURL,
	}

	profSpec := model.ProfileSpec{Name: string(profile)} // Stub, in future passed in.
	if isFMP4 {
		profSpec.LLHLS = true // Re-using LLHLS flag or add explicit Type field in Args?
		// For now, let's just create profSpec correctly.
		// Wait, BuildHLSArgs uses profSpec?
		// We need to pass the "UseFMP4" intent to BuildHLSArgs.
		// We can add it to OutputSpec or use ProfileSpec.
		// Let's modify ProfileSpec or pass it explicitly.
		// Actually, let's update OutputSpec to have Extension/FMP4 flag.
		// But wait, args.go:BuildHLSArgs takes profSpec.
		// I will update args.go next.
	} else {
		// profSpec.LLHLS = false
	}

	out := OutputSpec{
		HLSPlaylist:        tmpPlaylist,
		SegmentFilename:    SegmentPattern(sessionDir, ext),
		SegmentDuration:    2,
		PlaylistWindowSize: 3,
	}

	if isFMP4 {
		out.InitFilename = InitPath(sessionDir) // We need to add this field to OutputSpec in args.go
	}

	args, err := BuildHLSArgs(in, out, profSpec)
	if err != nil {
		return err
	}

	if profile == "sleep_test" { // For testing
		r.BinPath = "sleep"
		args = []string{"10"}
	}

	r.cmd = exec.CommandContext(ctx, r.BinPath, args...) // #nosec G204 -- args constructed internally; BinPath from trusted config

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
			line := scanner.Bytes()
			r.ring.Write(line)
			r.ring.Write([]byte("\n"))
		}
	}()

	// Start Playlist Atomicity Loop (Background)
	if profile != "sleep_test" {
		go r.syncPlaylistLoop(ctx, sessionDir)
	}

	r.start = time.Now()

	// Log FFmpeg command for debugging
	log.L().Info().Str("component", "ffmpeg").Str("command", r.cmd.String()).Msg("starting ffmpeg process")

	if err := r.cmd.Start(); err != nil {
		startTotal.WithLabelValues("err").Inc()
		return fmt.Errorf("ffmpeg start failed: %w", err)
	}

	startTotal.WithLabelValues("ok").Inc()
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

		// Log FFmpeg stderr for debugging exit errors
		stderrLines := r.ring.LastN(20)
		if len(stderrLines) > 0 {
			log.L().Error().
				Str("component", "ffmpeg").
				Int("exit_code", code).
				Strs("stderr", stderrLines).
				Msg("ffmpeg process failed")
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
	proc := r.cmd.Process
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		// If error (e.g. process gone), just return
		return err
	}

	if ctx == nil {
		return nil
	}
	if ctx.Err() != nil {
		_ = proc.Kill()
		return nil
	}
	if _, ok := ctx.Deadline(); ok {
		go func() {
			<-ctx.Done()
			_ = proc.Kill()
		}()
	}

	return nil
}

func (r *Runner) LastLogLines(n int) []string {
	return r.ring.LastN(n)
}
