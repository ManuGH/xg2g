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
	"strings"
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

	cmd     *exec.Cmd
	curlCmd *exec.Cmd // Upstream fetcher for reliable HTTP handling
	start   time.Time

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
func (r *Runner) Start(ctx context.Context, sessionID, serviceRef string, profileSpec model.ProfileSpec) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.cmd != nil {
		return fmt.Errorf("process already started")
	}
	if !model.IsSafeSessionID(sessionID) {
		startTotal.WithLabelValues("err_bad_session").Inc()
		return fmt.Errorf("invalid session id: %s", sessionID)
	}
	logger := log.WithContext(ctx, log.WithComponent("ffmpeg"))

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
	// Phase 9-5: fMP4/LL-HLS handling
	isFMP4 := false
	ext := ".ts"
	profile := profileSpec.Name

	if profileSpec.LLHLS {
		isFMP4 = true
		ext = ".m4s"
		// Ensure transcoding for LL-HLS clients to guarantee codec compatibility.
		if !profileSpec.TranscodeVideo {
			profileSpec.TranscodeVideo = true
		}
	}

	streamURL, err := r.Client.ResolveStreamURL(ctx, serviceRef)
	if err != nil {
		startTotal.WithLabelValues("err_url").Inc()
		return fmt.Errorf("failed to resolve stream url: %w", err)
	}

	in := InputSpec{
		StreamURL: streamURL,
	}

	// DVR window configuration
	// Calculate playlist size based on DVR window duration from ProfileSpec
	// Standard profiles: 3 segments (6 seconds, minimal latency)
	// DVR profiles: Use DVRWindowSec from config (default: 2700s = 45min)
	segmentDuration := 2 // seconds
	playlistSize := 3    // default: minimal latency

	if profileSpec.DVRWindowSec > 0 {
		// Use configured DVR window
		playlistSize = profileSpec.DVRWindowSec / segmentDuration
		logger.Info().
			Int("dvr_window_sec", profileSpec.DVRWindowSec).
			Int("playlist_size", playlistSize).
			Str("profile", profile).
			Msg("using configured DVR window")
	}

	out := OutputSpec{
		HLSPlaylist:        tmpPlaylist,
		SegmentFilename:    SegmentPattern(sessionDir, ext),
		SegmentDuration:    segmentDuration,
		PlaylistWindowSize: playlistSize,
	}

	if isFMP4 {
		out.InitFilename = "init.mp4" // Relative path for FFmpeg
	}

	// 3. Pipeline Setup (Curl -> FFmpeg)
	// We use curl because FFmpeg's HTTP client is too strict for some Enigma2 receivers (malformed headers).
	var curlCmd *exec.Cmd
	if strings.HasPrefix(streamURL, "http") {
		// Verify if we should use pipe (default: yes for robustness)
		usePipe := true
		if usePipe {
			// Construct curl command
			// Use -s (silent) but we might want errors?
			// Use -N (no buffer) to flush immediately? streaming usually implies it?
			// curl default buffer is fine usually, but -N is safer for latency?
			// Using same args as verified manually: -s -H "Icy-MetaData: 1" --user-agent ...
			curlArgs := []string{
				"-v",                     // Verbose for debugging
				"--connect-timeout", "5", // Fail fast on connection issues
				"-H", "Icy-MetaData: 1",
				"--user-agent", "curl/8.14.1",
				streamURL,
			}
			curlCmd = exec.CommandContext(ctx, "curl", curlArgs...)
			curlCmd.Stderr = os.Stderr // Capture curl errors in logs

			// Override input for FFmpeg
			in.StreamURL = "pipe:0"
		}
	}

	args, err := BuildHLSArgs(in, out, profileSpec)
	if err != nil {
		return err
	}

	if profile == "sleep_test" { // For testing
		r.BinPath = "sleep"
		args = []string{"10"}
	}

	r.cmd = exec.CommandContext(ctx, r.BinPath, args...) // #nosec G204 -- args constructed internally; BinPath from trusted config

	// Link Pipe
	if curlCmd != nil {
		r.curlCmd = curlCmd
		pipe, err := curlCmd.StdoutPipe()
		if err != nil {
			startTotal.WithLabelValues("err_pipe").Inc()
			return fmt.Errorf("failed to create curl pipe: %w", err)
		}
		r.cmd.Stdin = pipe
	}

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
			_, _ = r.ring.Write(line)
			_, _ = r.ring.Write([]byte("\n"))
		}
	}()

	// Start Playlist Atomicity Loop (Background)
	if profile != "sleep_test" {
		go r.syncPlaylistLoop(ctx, sessionDir)
	}

	r.start = time.Now()

	// Log FFmpeg command for debugging
	logger.Info().Str("command", r.cmd.String()).Msg("starting ffmpeg process")

	// Start Curl if configured (pipeline)
	if r.curlCmd != nil {
		logger.Info().Str("upstream", "curl").Msg("starting curl pipeline")
		if err := r.curlCmd.Start(); err != nil {
			startTotal.WithLabelValues("err_curl").Inc()
			return fmt.Errorf("curl start failed: %w", err)
		}
	}

	if err := r.cmd.Start(); err != nil {
		// Cleanup curl if ffmpeg fails
		if r.curlCmd != nil {
			_ = r.curlCmd.Process.Kill()
		}
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

// Wait blocks until exit.
func (r *Runner) Wait(ctx context.Context) (model.ExitStatus, error) {
	// Wait for cmd.Wait()
	// Note: cmd.Wait() closes pipes.

	err := r.cmd.Wait()
	logger := log.WithContext(ctx, log.WithComponent("ffmpeg"))

	// Cleanup Curl (Wait/Kill)
	if r.curlCmd != nil {
		// Curl might still be running or blocked on write
		_ = r.curlCmd.Wait() // Don't check error, just ensure it's reaped
	}

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
			logger.Error().
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

	// Stop Curl first to cut input
	if r.curlCmd != nil && r.curlCmd.Process != nil {
		_ = r.curlCmd.Process.Kill()
	}

	// Send SIGTERM
	logger := log.WithContext(ctx, log.WithComponent("ffmpeg"))
	logger.Debug().Msg("sending SIGTERM to ffmpeg")
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
