// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ffmpeg

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/pipeline/exec/enigma2"
	"github.com/ManuGH/xg2g/internal/pipeline/model"
	"github.com/ManuGH/xg2g/internal/procgroup"
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

	enigmaPTSJump = promauto.NewCounter(prometheus.CounterOpts{
		Name: "v3_enigma_pts_jump_total",
		Help: "Total number of source PTS jumps detected",
	})

	enigmaDecodeError = promauto.NewCounter(prometheus.CounterOpts{
		Name: "v3_enigma_decode_error_total",
		Help: "Total number of decode errors/corrupt packets from source",
	})

	enigmaHardReset = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "v3_enigma_ingest_reset_total",
		Help: "Total number of hard resets triggered by source changes",
	}, []string{"reason"})
)

// Runner manages a single ffmpeg process.
type Runner struct {
	BinPath string
	HLSRoot string
	Client  *enigma2.Client
	Args    []string // Default args or template

	// FFmpeg input probing config (from Enigma2Config)
	AnalyzeDuration string
	ProbeSize       string

	cmd     *exec.Cmd
	curlCmd *exec.Cmd // Upstream fetcher for reliable HTTP handling
	start   time.Time

	ring *LineRing

	mu       sync.Mutex
	status   *model.ExitStatus // Cached status once exited
	resultCh chan error        // Channel for Wait() to receive final result

	killTimeout time.Duration
}

// NewRunner creates a new FFmpeg runner.
func NewRunner(binPath, hlsRoot string, client *enigma2.Client, killTimeout time.Duration) *Runner {
	if binPath == "" {
		binPath = "ffmpeg"
	}
	return &Runner{
		BinPath:     binPath,
		HLSRoot:     hlsRoot,
		Client:      client,
		ring:        NewLineRing(256), // Keep last 256 lines of stderr
		killTimeout: killTimeout,
		resultCh:    make(chan error, 1),
	}
}

// Start launches the process pipeline with supervision.
func (r *Runner) Start(ctx context.Context, sessionID, source string, profileSpec model.ProfileSpec, startMs int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.status != nil { // Already finished?
		return fmt.Errorf("process already finished")
	}
	// Note: We can't easily check r.cmd != nil here because runOnce sets it later.
	// But relying on r.resultCh being non-empty (via Wait) or a separate flag is safer.
	// For MVP, we assume Start is called once.

	// Launch Supervisor
	go r.supervisor(ctx, sessionID, source, profileSpec, startMs)

	// Return success immediately to allow Orchestrator to proceed to polling
	startTotal.WithLabelValues("ok").Inc()
	return nil
}

// supervisor manages the restart loop
func (r *Runner) supervisor(ctx context.Context, sessionID, source string, profileSpec model.ProfileSpec, startMs int64) {
	err := r.runWithRestarts(ctx, sessionID, source, profileSpec, startMs)
	r.resultCh <- err
	close(r.resultCh)
}

// runWithRestarts Execute pipeline with supervised restarts
func (r *Runner) runWithRestarts(ctx context.Context, sessionID, source string, profileSpec model.ProfileSpec, startMs int64) error {
	logger := log.WithContext(ctx, log.WithComponent("ffmpeg"))
	const maxAttempts = 3
	const startupWindow = 20 * time.Second

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		startTime := time.Now()
		exitCode, err := r.runOnce(ctx, sessionID, source, profileSpec, startMs)

		// 1. Success or Context Cancelled -> Done
		if exitCode == 0 || ctx.Err() != nil {
			return err
		}

		// 2. Check if restartable
		timeSinceStart := time.Since(startTime)

		// 3. Pattern Matching (Strict Allowlist)
		stderrLines := r.ring.LastN(20)
		shouldRestart := false
		allowlist := []string{
			"non-existing PPS",
			"non-existing SPS",
			"no frame!",
			"decode_slice_header error",
			"Invalid NAL unit",
			"SPS unavailable",
			"PPS unavailable",
		}

		for _, line := range stderrLines {
			// Check for Metrics
			if strings.Contains(line, "PTS jump") {
				enigmaPTSJump.Inc()
			}
			if strings.Contains(line, "corrupt") || strings.Contains(line, "invalid data") {
				enigmaDecodeError.Inc()
			}

			// Check for Hard Reset triggers (ADR-004)
			hardResetReason := ""
			if strings.Contains(line, "resolution changed") {
				hardResetReason = "resolution_change"
			} else if strings.Contains(line, "codec for input stream") && strings.Contains(line, "changed") {
				hardResetReason = "codec_change"
			}

			if hardResetReason != "" {
				logger.Warn().Str("reason", hardResetReason).Str("line", line).Msg("Hard Reset: Source metadata changed, restarting pipeline")
				enigmaHardReset.WithLabelValues(hardResetReason).Inc()
				shouldRestart = true
				break
			}

			for _, pattern := range allowlist {
				if strings.Contains(line, pattern) {
					shouldRestart = true
					break
				}
			}
			if shouldRestart {
				break
			}
		}

		if timeSinceStart > startupWindow && !shouldRestart {
			logger.Warn().
				Int("exit_code", exitCode).
				Int("attempt", attempt).
				Dur("uptime", timeSinceStart).
				Msg("process failed after startup window, not restarting")
			return err
		}

		if attempt >= maxAttempts {
			logger.Warn().Int("exit_code", exitCode).Int("attempt", attempt).Msg("max restart attempts reached")
			return err
		}

		if !shouldRestart {
			logger.Warn().Int("exit_code", exitCode).Strs("stderr", stderrLines).Msg("process failure did not match restart allowlist")
			return err
		}

		// 4. Backoff and Restart
		restartLog := logger.Warn().
			Int("attempt", attempt).
			Int("exit_code", exitCode)
		if timeSinceStart > startupWindow {
			restartLog.Dur("uptime", timeSinceStart).Msg("restarting pipeline after late SPS/PPS corruption")
		} else {
			restartLog.Msg("restarting pipeline due to startup corruption")
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(1 * time.Second):
			// Continue loop
		}
	}
	return nil // Should be unreachable given 'return err' logic above
}

// runOnce executes a single process lifecycle
func (r *Runner) runOnce(ctx context.Context, sessionID, source string, profileSpec model.ProfileSpec, startMs int64) (int, error) {
	r.mu.Lock()
	// Check cancellation before starting
	if ctx.Err() != nil {
		r.mu.Unlock()
		return 0, ctx.Err()
	}

	// Format start offset
	var startOffset string
	if startMs > 0 {
		startOffset = fmt.Sprintf("%.3f", float64(startMs)/1000.0)
	}
	logger := log.WithContext(ctx, log.WithComponent("ffmpeg"))

	// 1. Prepare Layout
	sessionDir := SessionOutputDir(r.HLSRoot, sessionID)
	// mkdir -p
	// #nosec G301 -- 0755
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		r.mu.Unlock()
		return 1, fmt.Errorf("failed to create session dir: %w", err)
	}

	tmpPlaylist, _ := PlaylistPaths(sessionDir)

	// 2. Build Args
	isFMP4 := false
	ext := ".ts"
	profile := profileSpec.Name

	if profileSpec.LLHLS {
		isFMP4 = true
		ext = ".m4s"
		if !profileSpec.TranscodeVideo {
			profileSpec.TranscodeVideo = true
		}
	}

	var streamURL string
	switch {
	case strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://"):
		streamURL = source
	case len(source) > 0 && source[0] == '/':
		streamURL = source // Local path
	default:
		// Resolve via Enigma2
		var err error
		streamURL, err = r.Client.ResolveStreamURL(ctx, source)
		if err != nil {
			r.mu.Unlock()
			return 1, fmt.Errorf("failed to resolve stream url: %w", err)
		}
	}

	in := InputSpec{
		StreamURL:       streamURL,
		StartOffset:     startOffset,
		AnalyzeDuration: r.AnalyzeDuration,
		ProbeSize:       r.ProbeSize,
	}

	// DVR / VOD Config
	// Patch 1: Segment Duration Policy for DVR (6s reduces file count by 3x vs 2s)
	// Rationale: 10800s / 6s = 1800 segments (vs 5400 @ 2s) - manageable for disk I/O
	segmentDuration := 6
	if profileSpec.LLHLS {
		// LL-HLS uses smaller segments for low latency, but still larger than 2s
		segmentDuration = 4
	}

	// Patch 1: Calculate playlist size from DVR window (deterministic windowing)
	// Without this, FFmpeg defaults to hls_list_size=5 (only ~10s of content)
	playlistSize := 3 // Minimum for live streams without DVR
	if profileSpec.DVRWindowSec > 0 {
		// Calculate based on actual segment duration to match DVR window
		playlistSize = profileSpec.DVRWindowSec / segmentDuration
		// Safety clamp: min 3 segments, max 2000 (12000s / 6s for extreme edge cases)
		if playlistSize < 3 {
			playlistSize = 3
		}
		if playlistSize > 2000 {
			playlistSize = 2000
		}
	}
	if profileSpec.VOD {
		// VOD: Keep all segments (hls_list_size=0 means unlimited)
		playlistSize = 0
	}

	// Debug logging for HLS playlist configuration
	logger.Debug().
		Int("dvr_window_sec", profileSpec.DVRWindowSec).
		Int("segment_duration", segmentDuration).
		Int("calculated_playlist_size", playlistSize).
		Bool("vod_mode", profileSpec.VOD).
		Bool("llhls", profileSpec.LLHLS).
		Msg("hls playlist configuration")

	out := OutputSpec{
		HLSPlaylist:        tmpPlaylist,
		SegmentFilename:    SegmentPattern(sessionDir, ext),
		SegmentDuration:    segmentDuration,
		PlaylistWindowSize: playlistSize,
	}

	if isFMP4 {
		out.InitFilename = "init.mp4"
	}

	// 3. Pipeline Setup (Curl -> FFmpeg)
	var curlCmd *exec.Cmd
	var curlStderr *bufio.Scanner
	if strings.HasPrefix(streamURL, "http") {
		usePipe := true
		if usePipe {
			curlArgs := []string{
				"-sS",
				"--connect-timeout", "5",
				// Transport Hardening: Retry Logic
				"--retry", "3",
				"--retry-delay", "1",
				"--retry-connrefused",
				"-H", "Icy-MetaData: 1",
				"--user-agent", "curl/8.14.1",
				streamURL,
			}
			curlCmd = exec.CommandContext(ctx, "curl", curlArgs...)
			procgroup.Set(curlCmd)
			if stderrPipe, err := curlCmd.StderrPipe(); err == nil {
				curlStderr = bufio.NewScanner(stderrPipe)
			} else {
				r.mu.Unlock()
				return 1, fmt.Errorf("failed to capture curl stderr: %w", err)
			}
			in.StreamURL = "pipe:0"
		}
	}

	args, err := BuildHLSArgs(in, out, profileSpec)
	if err != nil {
		r.mu.Unlock()
		return 1, err
	}

	switch profile {
	case "sleep_test":
		r.BinPath = "sleep"
		args = []string{"10"}
	case "ignore_term_test":
		r.BinPath = "sh"
		args = []string{"-c", "trap '' TERM; while true; do sleep 10; done"}
	case "restart_test":
		r.BinPath = "sh"
		// Fail fast with trigger string to stderr
		args = []string{"-c", "echo 'non-existing PPS' >&2; exit 1"}
	}

	r.cmd = exec.CommandContext(ctx, r.BinPath, args...) // #nosec G204
	procgroup.Set(r.cmd)

	if curlCmd != nil {
		r.curlCmd = curlCmd
		pipe, err := curlCmd.StdoutPipe()
		if err != nil {
			r.mu.Unlock()
			return 1, fmt.Errorf("failed to create curl pipe: %w", err)
		}
		r.cmd.Stdin = pipe
	}

	// WaitGroup to ensure all IO is drained before returning
	var ioWg sync.WaitGroup

	// Capture Stderr
	stderr, err := r.cmd.StderrPipe()
	if err != nil {
		r.mu.Unlock()
		return 1, err
	}

	// Consume stderr (shared ring buffer)
	ioWg.Add(1)
	go func() {
		defer ioWg.Done()
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Bytes()
			_, _ = r.ring.Write(line)
			_, _ = r.ring.Write([]byte("\n"))
		}
	}()
	if curlStderr != nil {
		ioWg.Add(1)
		go func() {
			defer ioWg.Done()
			for curlStderr.Scan() {
				line := curlStderr.Text()
				if line == "" {
					continue
				}
				_, _ = r.ring.Write([]byte("curl: " + line))
				_, _ = r.ring.Write([]byte("\n"))
			}
		}()
	}

	// Start Sync Loop (if not testing)
	if profile != "sleep_test" {
		go r.syncPlaylistLoop(ctx, sessionDir)
	}

	r.start = time.Now()
	// Only log start once per run (or debug log level for restarts)
	logger.Info().Str("command", r.cmd.String()).Msg("starting ffmpeg process (runOnce)")

	if r.curlCmd != nil {
		if err := r.curlCmd.Start(); err != nil {
			r.mu.Unlock()
			return 1, fmt.Errorf("curl start failed: %w", err)
		}
	}

	if err := r.cmd.Start(); err != nil {
		// Cleanup curl if ffmpeg fails
		if r.curlCmd != nil {
			_ = r.curlCmd.Process.Kill()
		}
		r.mu.Unlock()
		return 1, fmt.Errorf("ffmpeg start failed: %w", err)
	}

	r.mu.Unlock()

	// Wait Block
	waitErr := r.cmd.Wait()

	// Cleanup Curl
	if curlCmd != nil {
		_ = curlCmd.Wait()
	}

	// Ensure all log output is captured
	ioWg.Wait()

	// Determine Exit Code
	code := 0
	if waitErr != nil {
		code = 1
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		}
	}

	return code, waitErr
}

// syncPlaylistLoop watches for valid .tmp playlist and promotes it to final .m3u8 atomically.
func (r *Runner) syncPlaylistLoop(ctx context.Context, dir string) {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	tmpPath, finalPath := PlaylistPaths(dir)
	logger := log.WithContext(ctx, log.WithComponent("ffmpeg"))

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
				logger.Warn().Err(err).Msg("failed to sync playlist")
			}
		}
	}
}

// Wait blocks until exit (consumes from supervisor channel).
func (r *Runner) Wait(ctx context.Context) (model.ExitStatus, error) {
	// Wait for supervisor result
	var err error
	select {
	case <-ctx.Done():
		err = ctx.Err()
	case e := <-r.resultCh:
		err = e
	}

	logger := log.WithContext(ctx, log.WithComponent("ffmpeg"))
	r.mu.Lock()
	defer r.mu.Unlock()

	end := time.Now()
	// Default status if not explicitly set by loop?
	// The Supervisor doesn't set r.status. We populate it here based on final error.

	code := 0
	reason := "clean"

	if err != nil {
		code = 1 // Default error
		// Try to unwrap exit code?
		// runOnce returned error, which might be ExitError
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		}

		// Log if error
		stderrLines := r.ring.LastN(20)
		if len(stderrLines) > 0 {
			logger.Error().
				Int("exit_code", code).
				Strs("stderr", stderrLines).
				Msg("ffmpeg pipeline final failure")
		}

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
		StartedAt: r.start, // Approximate start of first run
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

	// Stop Curl first to cut input
	if r.curlCmd != nil && r.curlCmd.Process != nil {
		_ = procgroup.Kill(r.curlCmd, syscall.SIGTERM)
	}

	// Send SIGTERM
	logger := log.WithContext(ctx, log.WithComponent("ffmpeg"))
	logger.Debug().Msg("sending SIGTERM to ffmpeg")
	if err := procgroup.Kill(r.cmd, syscall.SIGTERM); err != nil {
		// If error (e.g. process gone), just return
		return err
	}

	killTimeout := r.killTimeout
	if killTimeout <= 0 {
		killTimeout = 5 * time.Second
	}
	if ctx == nil {
		ctx = context.Background()
	}

	if ctx.Err() != nil {
		_ = procgroup.Kill(r.cmd, syscall.SIGKILL)
		_ = procgroup.Kill(r.curlCmd, syscall.SIGKILL)
		return nil
	}

	deadlineCtx, cancel := context.WithTimeout(ctx, killTimeout)
	go func() {
		defer cancel()
		<-deadlineCtx.Done()
		_ = procgroup.Kill(r.cmd, syscall.SIGKILL)
		_ = procgroup.Kill(r.curlCmd, syscall.SIGKILL)
	}()

	return nil
}

func (r *Runner) LastLogLines(n int) []string {
	return r.ring.LastN(n)
}
