package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/metrics"
	"github.com/rs/zerolog"
)

// executeVODRemux performs the complete VOD remux with:
// - Probe-based codec decisions
// - Three-tier fallback ladder (default → fallback → transcode)
// - Operator artifacts (.meta.json on success, .err.log on failure)
// - Dynamic timeout based on file size
func (s *Server) executeVODRemux(recordingID, serviceRef, localPath, cachePath string) error {
	logger := log.L().With().
		Str("component", "vod-remux").
		Str("recording", recordingID).
		Str("src", localPath).
		Str("dest", cachePath).
		Logger()

	logger.Info().Msg("starting vod remux")

	// Binaries
	ffmpegBin := s.cfg.FFmpegBin
	if ffmpegBin == "" {
		ffmpegBin = "ffmpeg"
	}
	// ffprobe is typically in the same directory as ffmpeg
	ffprobeBin := "ffprobe"

	// Dynamic timeout based on file size (baseline 20min + 1min/GB, max 2h)
	timeout := 20 * time.Minute
	if info, err := os.Stat(localPath); err == nil {
		sizeGB := float64(info.Size()) / (1024 * 1024 * 1024)
		extraTime := time.Duration(sizeGB) * time.Minute
		timeout = 20*time.Minute + extraTime
		if timeout > 2*time.Hour {
			timeout = 2 * time.Hour
		}
		logger.Debug().
			Float64("size_gb", sizeGB).
			Dur("timeout", timeout).
			Msg("calculated dynamic timeout")
	}

	// Use server root context for clean shutdown
	parent := s.rootCtx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	// Paths
	tmpOut := cachePath + ".tmp"
	metaPath := cachePath + ".meta.json"
	errLogPath := cachePath + ".err.log"

	// Input selection: support multi-part recordings via concat list.
	inputPath := localPath
	probePath := localPath
	concatPath := ""
	useConcat := false
	if parts, partErr := recordingParts(localPath); partErr == nil && len(parts) > 0 {
		probePath = parts[0]
		if len(parts) > 1 {
			concatPath = cachePath + ".concat.txt"
			if err := writeConcatList(concatPath, parts); err != nil {
				logger.Error().Err(err).Msg("failed to write concat list for vod remux")
				return fmt.Errorf("concat list: %w", err)
			}
			inputPath = concatPath
			useConcat = true
			logger.Info().Int("parts", len(parts)).Msg("vod remux using concat input")
		} else {
			inputPath = parts[0]
		}
	} else if partErr != nil && !errors.Is(partErr, errRecordingNotFound) {
		logger.Error().Err(partErr).Msg("failed to resolve recording parts for vod remux")
		return fmt.Errorf("recording parts: %w", partErr)
	}
	if concatPath != "" {
		defer os.Remove(concatPath)
	}

	// Clean up any stale artifacts
	defer func() {
		if _, err := os.Stat(cachePath); err != nil {
			// Remux failed - clean up tmp file
			os.Remove(tmpOut)
		}
	}()

	// Step 1: Probe streams (with fallback to default DVB assumptions)
	logger.Debug().Msg("probing streams")
	streamInfo, err := probeStreams(ctx, ffprobeBin, probePath)
	if err != nil {
		// Probe failed (likely corrupted frames at start)
		// Fall back to typical ORF/ARD/ZDF DVB-T2/Sat defaults:
		// - H.264 8-bit yuv420p (95% of recordings)
		// - AC3 5.1 audio (85% of recordings)
		logger.Warn().Err(err).Msg("stream probe failed - using default DVB assumptions (H.264 + AC3)")
		streamInfo = &StreamInfo{
			Video: VideoStreamInfo{
				CodecName: "h264",
				PixFmt:    "yuv420p",
				BitDepth:  8,
			},
			Audio: AudioStreamInfo{
				CodecName:  "ac3",
				TrackCount: 1,
			},
		}
	}

	logger.Info().
		Str("video_codec", streamInfo.Video.CodecName).
		Str("pix_fmt", streamInfo.Video.PixFmt).
		Int("bit_depth", streamInfo.Video.BitDepth).
		Str("audio_codec", streamInfo.Audio.CodecName).
		Int("audio_tracks", streamInfo.Audio.TrackCount).
		Msg("stream info detected")

	// Step 2: Build remux decision
	decision := buildRemuxArgs(streamInfo, inputPath, tmpOut)
	if useConcat {
		decision.Args = insertArgsBefore(decision.Args, "-i", []string{"-f", "concat", "-safe", "0"})
	}
	logRemuxDecision(decision, recordingID)

	if decision.Strategy == StrategyUnsupported {
		logger.Error().Str("reason", decision.Reason).Msg("codec unsupported")
		writeErrorLog(errLogPath, decision.Reason)
		return fmt.Errorf("codec unsupported: %s", decision.Reason)
	}

	// Step 3: Execute remux with ladder (default → fallback → transcode)
	var finalErr error
	var stderrStr string
	var usedStrategy RemuxStrategy

	// Try primary strategy (default or transcode)
	logger.Info().
		Str("strategy", string(decision.Strategy)).
		Str("reason", decision.Reason).
		Msg("attempting remux")

	watchCfg := ProgressWatchConfig{
		StartupGrace: 30 * time.Second,
		StallTimeout: 90 * time.Second,
		Tick:         5 * time.Second,
		Strategy:     string(decision.Strategy),
		RecordingID:  recordingID,
	}

	stderrStr, exitCode, err := runFFmpegWithProgress(ctx, ffmpegBin, decision.Args, watchCfg, logger)

	if err != nil {
		classifiedErr := classifyRemuxError(stderrStr, exitCode)
		logger.Warn().
			Err(classifiedErr).
			Int("exit_code", exitCode).
			Msg("primary remux failed")

		// Ladder Step 1: Retry with fallback flags if applicable
		if decision.Strategy == StrategyDefault && shouldRetryWithFallback(classifiedErr) {
			logger.Info().Msg("retrying with fallback flags")
			fallbackArgs := buildFallbackRemuxArgs(inputPath, tmpOut)
			if useConcat {
				fallbackArgs = insertArgsBefore(fallbackArgs, "-i", []string{"-f", "concat", "-safe", "0"})
			}

			watchCfg.Strategy = "fallback"
			stderrStr, exitCode, err = runFFmpegWithProgress(ctx, ffmpegBin, fallbackArgs, watchCfg, logger)

			if err != nil {
				classifiedErr = classifyRemuxError(stderrStr, exitCode)
				logger.Warn().
					Err(classifiedErr).
					Int("exit_code", exitCode).
					Msg("fallback remux failed")

				// Ladder Step 2: Last resort - transcode
				if shouldRetryWithTranscode(classifiedErr) {
					logger.Info().Msg("retrying with full transcode")
					transcodeArgs := buildTranscodeArgs(inputPath, tmpOut)
					if useConcat {
						transcodeArgs = insertArgsBefore(transcodeArgs, "-i", []string{"-f", "concat", "-safe", "0"})
					}

					watchCfg.Strategy = "transcode"
					stderrStr, exitCode, err = runFFmpegWithProgress(ctx, ffmpegBin, transcodeArgs, watchCfg, logger)

					if err != nil {
						finalErr = classifyRemuxError(stderrStr, exitCode)
						usedStrategy = StrategyTranscode
					} else {
						usedStrategy = StrategyTranscode
					}
				} else {
					finalErr = classifiedErr
					usedStrategy = StrategyFallback
				}
			} else {
				usedStrategy = StrategyFallback
			}
		} else {
			finalErr = classifiedErr
			usedStrategy = decision.Strategy
		}
	} else {
		usedStrategy = decision.Strategy
	}

	// Step 4: Handle final result
	if finalErr != nil {
		logger.Error().
			Err(finalErr).
			Str("strategy", string(usedStrategy)).
			Msg("all remux strategies failed")

		// Write error log for operator debugging
		writeErrorLog(errLogPath, fmt.Sprintf(
			"Strategy: %s\nError: %v\n\nffmpeg stderr:\n%s",
			usedStrategy,
			finalErr,
			truncateForLog(stderrStr, 2000),
		))

		return fmt.Errorf("remux failed after all strategies: %w", finalErr)
	}

	// Step 5: Success - Write metadata and commit
	meta := map[string]interface{}{
		"strategy":      usedStrategy,
		"reason":        decision.Reason,
		"video_codec":   streamInfo.Video.CodecName,
		"video_pix_fmt": streamInfo.Video.PixFmt,
		"video_bitdepth": streamInfo.Video.BitDepth,
		"audio_codec":   streamInfo.Audio.CodecName,
		"audio_tracks":  streamInfo.Audio.TrackCount,
		"remux_time":    time.Now().Format(time.RFC3339),
		"service_ref":   serviceRef,
	}

	if metaJSON, err := json.MarshalIndent(meta, "", "  "); err == nil {
		if err := os.WriteFile(metaPath, metaJSON, 0644); err != nil {
			logger.Warn().Err(err).Msg("failed to write .meta.json")
		}
	}

	// Atomic commit
	if err := os.Rename(tmpOut, cachePath); err != nil {
		logger.Error().Err(err).Msg("failed to commit vod cache")
		os.Remove(tmpOut)
		return fmt.Errorf("failed to commit cache: %w", err)
	}

	logger.Info().
		Str("strategy", string(usedStrategy)).
		Str("cache", cachePath).
		Msg("vod remux completed successfully")

	return nil
}

// writeErrorLog writes an error log file for operator debugging
func writeErrorLog(path, content string) {
	_ = os.WriteFile(path, []byte(content), 0644)
}

// ProgressWatchConfig configures the progress watchdog
type ProgressWatchConfig struct {
	StartupGrace time.Duration
	StallTimeout time.Duration
	Tick         time.Duration
	Strategy     string // "default"|"fallback"|"transcode"
	RecordingID  string
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
				return fmt.Errorf("%w: no progress for %v", ErrFFmpegStalled, cfg.StallTimeout)
			}
		}
	}
}

// runFFmpegWithProgress executes ffmpeg with progress supervision and stall detection
func runFFmpegWithProgress(
	ctx context.Context,
	bin string,
	args []string,
	cfg ProgressWatchConfig,
	logger zerolog.Logger,
) (stderr string, exitCode int, err error) {
	// Add -nostdin to prevent ffmpeg from blocking on stdin
	// Add -progress pipe:1 for stall detection
	fullArgs := append([]string{"-nostdin", "-progress", "pipe:1"}, args...)

	cmd := exec.CommandContext(ctx, bin, fullArgs...)

	// Setup pipes
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", 1, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	// Start ffmpeg
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
