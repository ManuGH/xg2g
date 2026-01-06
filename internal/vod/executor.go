package vod

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/metrics"
	"github.com/rs/zerolog"
)

// ExecuteInput (Phase A definition) - Keeping simple for now
type ExecuteInput struct {
	Exec Exec
	Log  Logger
	Name string // ffmpeg binary path
	Args []string
}

// Execute runs the command using the abstract executor
func Execute(ctx context.Context, in ExecuteInput) error {
	return in.Exec.Run(ctx, in.Name, in.Args)
}

// DefaultExecutor implements Exec with robust supervision (stall detection, progress)
type DefaultExecutor struct {
	Logger zerolog.Logger
}

func (e *DefaultExecutor) Run(ctx context.Context, name string, args []string) error {
	// Configure Watchdog
	watchCfg := ProgressWatchConfig{
		StartupGrace: 30 * time.Second,
		StallTimeout: 5 * time.Minute,
		Tick:         5 * time.Second,
		Strategy:     "default", // TODO: Pass this in?
		RecordingID:  "unknown", // TODO: Pass this in?
	}

	// Enriched context or interface extension can be done in Phase D

	_, _, err := runFFmpegWithProgress(ctx, name, args, watchCfg, e.Logger)
	return err
}

// FFmpegProgress and helper types

type FFmpegProgress struct {
	Frame     int
	Fps       float64
	OutTimeUs int64
	TotalSize int64
	Speed     string
}

func (p FFmpegProgress) hasAdvanced(prev FFmpegProgress) bool {
	return p.OutTimeUs > prev.OutTimeUs || p.TotalSize > prev.TotalSize || p.Frame > prev.Frame
}

type ProgressWatchConfig struct {
	StartupGrace time.Duration
	StallTimeout time.Duration
	Tick         time.Duration
	Strategy     string
	RecordingID  string
}

// runFFmpegWithProgress executes ffmpeg with progress supervision and stall detection
func runFFmpegWithProgress(
	ctx context.Context,
	bin string,
	args []string,
	cfg ProgressWatchConfig,
	logger zerolog.Logger,
) (stderr string, exitCode int, err error) {
	fullArgs := append([]string{"-nostdin", "-progress", "pipe:1"}, args...)
	cmd := exec.CommandContext(ctx, bin, fullArgs...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", 1, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		return "", 1, fmt.Errorf("failed to start ffmpeg: %w", err)
	}

	// Parse progress in background
	progressCh := make(chan FFmpegProgress, 100)
	go func() {
		defer close(progressCh)
		parseFFmpegProgress(stdout, progressCh)
	}()

	// Wait for completion in background
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	// Run watchdog
	watchErr := watchFFmpegProgress(ctx, done, progressCh, cmd.Process, cfg, logger)

	// Capture final state
	stderr = stderrBuf.String()
	exitCode = 1
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}

	return stderr, exitCode, watchErr
}

// watchFFmpegProgress monitors ffmpeg progress and kills on stall
func watchFFmpegProgress(
	ctx context.Context,
	done <-chan error,
	progressCh <-chan FFmpegProgress,
	proc *os.Process,
	cfg ProgressWatchConfig,
	logger zerolog.Logger,
) error {
	start := time.Now()
	lastProgressAt := start
	var last FFmpegProgress

	ticker := time.NewTicker(cfg.Tick)
	defer ticker.Stop()

	for {
		select {
		case err := <-done:
			// ffmpeg completed (success or failure)
			return err

		case <-ctx.Done():
			// Context cancelled - kill process
			if proc != nil {
				_ = proc.Kill()
			}
			return ctx.Err()

		case p, ok := <-progressCh:
			if !ok {
				// Progress channel closed (parser ended)
				continue
			}
			if p.hasAdvanced(last) {
				last = p
				lastProgressAt = time.Now()
			}

		case <-ticker.C:
			// Skip stall check during grace period
			if time.Since(start) < cfg.StartupGrace {
				continue
			}

			// Check for stall
			if time.Since(lastProgressAt) > cfg.StallTimeout {
				// STALL DETECTED
				metrics.IncVODRemuxStall(cfg.Strategy)
				logger.Error().
					Str("strategy", cfg.Strategy).
					Str("recording", cfg.RecordingID).
					Dur("since_progress", time.Since(lastProgressAt)).
					Int64("last_out_time_us", last.OutTimeUs).
					Int64("last_total_size", last.TotalSize).
					Str("last_speed", last.Speed).
					Msg("vod remux stalled - killing ffmpeg")

				if proc != nil {
					_ = proc.Kill()
				}
				return fmt.Errorf("ffmpeg stalled")
			}
		}
	}
}

// parseFFmpegProgress reads key=value lines from r and sends updates to ch.
func parseFFmpegProgress(r io.Reader, ch chan<- FFmpegProgress) {
	scanner := bufio.NewScanner(r)
	var current FFmpegProgress

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key, val := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])

		switch key {
		case "out_time_us":
			if v, err := strconv.ParseInt(val, 10, 64); err == nil {
				current.OutTimeUs = v
			}
		case "total_size":
			if v, err := strconv.ParseInt(val, 10, 64); err == nil {
				current.TotalSize = v
				// Determine if this is a good flush point?
				// FFmpeg 'progress' key is usually the flush.
			}
		case "speed":
			current.Speed = val
		case "progress":
			// Flush
			ch <- current
		}
	}
}
