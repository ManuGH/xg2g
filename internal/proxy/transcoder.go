package proxy

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ManuGH/xg2g/internal/core/urlutil"
	"github.com/ManuGH/xg2g/internal/metrics"
	"github.com/ManuGH/xg2g/internal/telemetry"
	"github.com/ManuGH/xg2g/internal/transcoder"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// TranscoderConfig holds configuration for audio transcoding.
type TranscoderConfig struct {
	Enabled           bool   // Whether transcoding is enabled
	Codec             string // Target audio codec (aac, mp3)
	Bitrate           string // Audio bitrate (e.g., "192k")
	Channels          int    // Number of audio channels (2 for stereo)
	FFmpegPath        string // Path to ffmpeg binary
	GPUEnabled        bool   // Whether GPU transcoding is enabled
	TranscoderURL     string // URL of the GPU transcoder service
	UseRustRemuxer    bool   // Whether to use native Rust remuxer instead of FFmpeg
	H264RepairEnabled bool   // Whether H.264 stream repair is enabled (fixes PPS/SPS headers for Plex)
	VideoTranscode    bool   // Whether full video transcoding is enabled
	VideoCodec        string // Target video codec (auto, av1, hevc, h264)
}

// Transcoder handles audio transcoding for streams.
type Transcoder struct {
	Config TranscoderConfig // Public for access from proxy handler
	logger zerolog.Logger
}

// NewTranscoder creates a new audio transcoder.
func NewTranscoder(config TranscoderConfig, logger zerolog.Logger) *Transcoder {
	return &Transcoder{
		Config: config,
		logger: logger,
	}
}

// TranscodeStream transcodes the stream from the target URL.
// It proxies the request to the target, pipes it through ffmpeg for transcoding, and streams the result back to the client.
// Supports Smart Codec Fallback (AV1 -> HEVC -> H264) if enabled.
func (t *Transcoder) TranscodeStream(ctx context.Context, w http.ResponseWriter, r *http.Request, targetURL string) error {
	// Inject stream_id into context logger for traceability
	// This ensures all logs from this stream session share the same ID
	streamID := fmt.Sprintf("transcode-%s-%d", time.Now().Format("150405"), time.Now().UnixNano()%1000)
	logger := zerolog.Ctx(ctx).With().Str("stream_id", streamID).Logger()
	ctx = logger.WithContext(ctx)

	// Log stream start (Lifecycle Event)
	logger.Info().
		Str("event", "stream_start").
		Str("mode", "transcode").
		Str("input_url", sanitizeURL(targetURL)).
		Str("client_ip", r.RemoteAddr).
		Str("user_agent", r.UserAgent()).
		Msg("starting transcode session")

	startTime := time.Now()
	var exitReason = "internal_error" // Default
	var bytesOut int64
	var lastStats *FFmpegStats

	defer func() {
		// Log stream end (Lifecycle Event)
		event := logger.Info().
			Str("event", "stream_end").
			Str("exit_reason", exitReason).
			Dur("duration_ms", time.Since(startTime)).
			Int64("bytes_out", bytesOut)

		if lastStats != nil {
			event.Float64("ffmpeg_last_speed", lastStats.Speed).
				Float64("ffmpeg_last_bitrate_kbps", lastStats.BitrateKBPS)
		}
		event.Msg("transcode session ended")
	}()

	// Determine list of codecs to try
	var codecs []string
	if t.Config.VideoTranscode && t.Config.VideoCodec == "auto" {
		// Smart Fallback Cascade
		codecs = []string{"av1_vaapi", "hevc_vaapi", "h264_vaapi"}
	} else if t.Config.VideoTranscode {
		// Specific codec requested (map simple names to VAAPI names)
		switch t.Config.VideoCodec {
		case "av1":
			codecs = []string{"av1_vaapi"}
		case "hevc":
			codecs = []string{"hevc_vaapi"}
		case "h264":
			codecs = []string{"h264_vaapi"}
		default:
			codecs = []string{t.Config.VideoCodec} // User custom
		}
	} else {
		// Audio only (Video Copy)
		codecs = []string{"copy"}
	}

	var lastErr error
	for _, codec := range codecs {
		logger.Info().Str("codec", codec).Msg("attempting transcoding with codec")

		// Create a child context for this attempt implies we should respect parent cancellation
		attemptCtx, cancel := context.WithCancel(ctx)
		defer cancel()

		err := t.streamTranscodeInternal(attemptCtx, w, r, targetURL, codec, &bytesOut, &exitReason, &lastStats)
		if err == nil {
			// Success! Stream finished normally (client disconnected or stream ended)
			if exitReason == "internal_error" {
				exitReason = "success" // Should have been set by internal, but just in case
			}
			return nil
		}

		// Check if error is recoverable
		if ctx.Err() != nil {
			exitReason = "client_disconnect"
			return ctx.Err() // Client disconnected, stop
		}

		lastErr = err
		logger.Warn().Err(err).Str("codec", codec).Msg("transcoding failed, trying next fallback if available")
		// Reset exit reason for next try
		exitReason = "ffmpeg_exit"
	}

	exitReason = "ffmpeg_exit" // Last error was likely ffmpeg failure
	return fmt.Errorf("all transcoding attempts failed: %w", lastErr)
}

// streamTranscodeInternal is the worker logic for a single codec attempt.
func (t *Transcoder) streamTranscodeInternal(ctx context.Context, w http.ResponseWriter, r *http.Request, targetURL string, videoCodec string, bytesOut *int64, exitReason *string, lastStats **FFmpegStats) error {
	logger := zerolog.Ctx(ctx)
	// Start tracing span
	tracer := telemetry.Tracer("xg2g.proxy")
	ctx, span := tracer.Start(ctx, "transcode.ffmpeg",
		trace.WithSpanKind(trace.SpanKindInternal),
	)
	defer span.End()

	// Add transcoding attributes
	span.SetAttributes(
		attribute.String(telemetry.TranscodeCodecKey, t.Config.Codec),
		attribute.String("video.codec", videoCodec),
		attribute.Bool(telemetry.TranscodeGPUEnabledKey, videoCodec != "copy"),
	)

	// Create request to target (Fresh Connection)
	proxyReq, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to create proxy request")
		return fmt.Errorf("create proxy request: %w", err)
	}

	// Copy Safe Headers from original request
	copyHeaders(r.Header, proxyReq.Header)

	// Execute request to target
	span.AddEvent("fetching source stream")
	client := &http.Client{}
	resp, err := client.Do(proxyReq)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "proxy request failed")
		return fmt.Errorf("proxy request failed: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.logger.Debug().Err(err).Msg("failed to close response body")
		}
	}()

	// Build ffmpeg command
	audioCopy := strings.EqualFold(t.Config.Codec, "copy")
	buildAudioArgs := func(includeAsync bool) []string {
		if audioCopy {
			return []string{"-map", "0:a?", "-c:a", "copy"}
		}
		args := []string{
			"-map", "0:a?", "-c:a", t.Config.Codec,
			"-b:a", t.Config.Bitrate,
			"-ac", fmt.Sprintf("%d", t.Config.Channels),
		}
		if includeAsync {
			args = append(args, "-async", "1")
		}
		return args
	}
	muxArgs := []string{
		"-start_at_zero", "-avoid_negative_ts", "make_zero",
		"-muxdelay", "0", "-muxpreload", "0",
		"-mpegts_copyts", "1",
		"-mpegts_flags", "resend_headers+initial_discontinuity",
		"-pcr_period", "20",
		"-pat_period", "0.1",
		"-sdt_period", "0.5",
		"-f", "mpegts",
		"pipe:1",
	}
	var args []string
	if videoCodec == "copy" {
		// Audio Transcode Only
		args = []string{"-hide_banner"}
		args = append(args, logLevelArgs("error", "")...)
		args = append(args,
			"-fflags", "+genpts+igndts",
			"-i", "pipe:0",
			"-map", "0:v", "-c:v", "copy",
		)
		args = append(args, buildAudioArgs(true)...)
		args = append(args, muxArgs...)
	} else {
		// Hardware Video Transcode
		args = []string{"-hide_banner"}
		args = append(args, logLevelArgs("error", "")...)
		args = append(args,
			"-init_hw_device", "vaapi=d1:/dev/dri/renderD128", // HW Device
			"-filter_hw_device", "d1",
			"-fflags", "+genpts+igndts",
			"-i", "pipe:0",
			"-vf", "format=nv12,hwupload", // Upload to GPU
			"-map", "0:v", "-c:v", videoCodec, // Selected Codec
			"-qp", "24", // Quality,
		)
		args = append(args, buildAudioArgs(false)...)
		args = append(args, "-map", "0:s?", "-c:s", "copy") // Copy subtitles if present
		args = append(args, muxArgs...)
	}

	logger.Debug().Str("ffmpeg_path", t.Config.FFmpegPath).Strs("args", args).Msg("starting ffmpeg")

	// Create ffmpeg command
	cmd := exec.CommandContext(ctx, t.Config.FFmpegPath, args...) // #nosec G204

	// Connect pipes
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("create stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("create stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("create stderr pipe: %w", err)
	}

	// Start ffmpeg
	if err := cmd.Start(); err != nil {
		span.RecordError(err)
		return fmt.Errorf("start ffmpeg: %w", err)
	}

	// Capture Stderr
	// Capture Stderr with Stats Parsing
	var stderrWg sync.WaitGroup
	stderrWg.Add(1)
	go func() {
		defer stderrWg.Done()

		scanner := bufio.NewScanner(stderr)
		statsTicker := time.NewTicker(5 * time.Second)
		defer statsTicker.Stop()

		var statsSeq int

		for scanner.Scan() {
			line := scanner.Text()

			// Parse stats
			if stats := ParseFFmpegStats(line); stats != nil {
				*lastStats = stats // Update pointer for parent

				select {
				case <-statsTicker.C:
					statsSeq++
					logger.Info().
						Str("event", "ffmpeg_stats").
						Int("seq", statsSeq).
						Float64("speed", stats.Speed).
						Float64("bitrate_kbps", stats.BitrateKBPS).
						Float64("fps", stats.FPS).
						Int("frame", stats.Frame).
						Msg("ffmpeg progress")
				default:
					// Debounce
				}
			} else {
				// Log other notable lines or debug
				if strings.Contains(strings.ToLower(line), "error") || strings.Contains(line, "Invalid") {
					logger.Warn().Str("stderr", line).Msg("ffmpeg warning")
				} else {
					logger.Debug().Str("stderr", line).Msg("ffmpeg output")
				}
			}
		}
	}()

	// Async Pipe Input
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer stdin.Close()
		io.Copy(stdin, resp.Body)
	}()

	// --- CRITICAL FALLBACK LOGIC ---
	// We must NOT write headers to 'w' until we are sure ffmpeg is working.
	// We try to read the first chunk from stdout.
	// If that read fails (e.g. ffmpeg exited because AV1 not supported), we return error.
	// AND we have NOT written to 'w' yet, so the caller can retry with next codec!

	firstChunk := make([]byte, 4096) // 4KB peek
	n, startErr := stdout.Read(firstChunk)

	if startErr != nil {
		// Failed to read ANY data. This means ffmpeg likely died immediately or stream is empty.
		// Kill process just in case
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		return fmt.Errorf("failed to read first chunk from ffmpeg (codec likely failed): %w", startErr)
	}

	// If we got here, FFmpeg is alive and producing data!
	// NOW we commit to this stream.
	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Connection", "close")
	// w.WriteHeader(http.StatusOK) // Implicit on Write

	// Write the first chunk we peered
	if _, err := w.Write(firstChunk[:n]); err != nil {
		// Client disconnected probably
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		return nil
	}

	// Copy the rest of the stream
	wg.Add(1)
	go func() {
		defer wg.Done()
		n, err := io.Copy(w, stdout)
		if n > 0 {
			atomic.AddInt64(bytesOut, n)
		}
		// If copy error and context is not cancelled, it's a broken pipe (client disconnect) or ffmpeg death
		if err != nil {
			if ctx.Err() != nil {
				*exitReason = "client_disconnect"
			} else if strings.Contains(err.Error(), "broken pipe") || strings.Contains(err.Error(), "connection reset") {
				*exitReason = "client_disconnect"
			} else {
				// Could be ffmpeg died
				*exitReason = "ffmpeg_exit"
			}
		} else {
			// Copy finished normally (EOF from stdout)
			// This means FFmpeg exited
			if ctx.Err() != nil {
				*exitReason = "client_disconnect"
			} else {
				*exitReason = "ffmpeg_exit"
			}
		}
	}()

	wg.Wait()

	// Wait for stderr to finish (ensure we got all logs)
	stderrWg.Wait()

	cmd.Wait() // Wait for exit

	if *exitReason == "internal_error" {
		*exitReason = "success" // If we reached here without setting specific error
	}

	return nil
}

// sanitizeURL removes credentials and query params for safe logging
func sanitizeURL(u string) string {
	return urlutil.SanitizeURL(u)
}

// RepairH264Stream repairs H.264 streams by adding proper PPS/SPS headers using FFmpeg's h264_mp4toannexb bitstream filter.
// This fixes streams from Enigma2 receivers that lack proper Picture Parameter Sets and Sequence Parameter Sets,
// which causes Plex and other clients to fail with "Playback Error".
//
// Architecture:
//
//	Input: MPEG-TS with broken H.264 (missing PPS/SPS headers) from Enigma2
//	Pipeline: Demux → h264_mp4toannexb bitstream filter → Remux
//	Output: MPEG-TS with proper H.264 Annex-B format (includes PPS/SPS)
//
// Performance:
//   - No transcoding overhead (copy mode for both video and audio)
//   - Minimal latency (~10-20ms for bitstream filter)
//   - Zero CPU usage for encoding/decoding
func (t *Transcoder) RepairH264Stream(ctx context.Context, w http.ResponseWriter, r *http.Request, targetURL string) error {
	// Inject stream_id into context logger for traceability
	streamID := fmt.Sprintf("repair-%s-%d", time.Now().Format("150405"), time.Now().UnixNano()%1000)
	logger := zerolog.Ctx(ctx).With().Str("stream_id", streamID).Logger()
	ctx = logger.WithContext(ctx)

	// Log stream start (Lifecycle Event)
	logger.Info().
		Str("event", "stream_start").
		Str("mode", "repair").
		Str("input_url", sanitizeURL(targetURL)).
		Str("client_ip", r.RemoteAddr).
		Str("user_agent", r.UserAgent()).
		Msg("starting repair session")

	startTime := time.Now()
	var exitReason = "internal_error" // Default
	var bytesOut int64
	var lastStats *FFmpegStats

	defer func() {
		// Log stream end (Lifecycle Event)
		event := logger.Info().
			Str("event", "stream_end").
			Str("exit_reason", exitReason).
			Dur("duration_ms", time.Since(startTime)).
			Int64("bytes_out", bytesOut)

		if lastStats != nil {
			event.Float64("ffmpeg_last_speed", lastStats.Speed).
				Float64("ffmpeg_last_bitrate_kbps", lastStats.BitrateKBPS)
		}
		event.Msg("repair session ended")
	}()

	// Start tracing span
	tracer := telemetry.Tracer("xg2g.proxy")
	ctx, span := tracer.Start(ctx, "stream.h264_repair",
		trace.WithSpanKind(trace.SpanKindInternal),
	)
	defer span.End()

	// Add repair attributes
	span.SetAttributes(
		attribute.String("stream.filter", "h264_mp4toannexb"),
		attribute.String("stream.mode", "copy"),
		attribute.Bool("stream.repair", true),
	)

	// Create request to target
	proxyReq, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to create proxy request")
		return fmt.Errorf("create proxy request: %w", err)
	}

	// Copy headers from original request
	for key, values := range r.Header {
		for _, value := range values {
			proxyReq.Header.Add(key, value)
		}
	}

	// Execute request to target
	span.AddEvent("fetching source stream")
	client := &http.Client{}
	resp, err := client.Do(proxyReq)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "proxy request failed")
		return fmt.Errorf("proxy request failed: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.logger.Debug().Err(err).Msg("failed to close response body")
		}
	}()

	// Build ffmpeg command for H.264 stream repair
	// Input: MPEG-TS stream from stdin with broken H.264
	// Output: MPEG-TS stream with repaired H.264 to stdout
	//
	// Key flags:
	// - bsf:v h264_mp4toannexb: Convert H.264 to Annex-B format, inserting PPS/SPS NAL units
	// - c copy: No transcoding, just copy streams (zero CPU overhead)
	// - fflags +genpts+igndts: Regenerate timestamps (Enigma2 streams have broken DTS)
	// Build ffmpeg command for H.264 stream repair
	// Input: MPEG-TS stream from stdin with broken H.264
	// Output: MPEG-TS stream with repaired H.264 to stdout
	//
	// Key flags:
	// - bsf:v h264_mp4toannexb: Convert H.264 to Annex-B format, inserting PPS/SPS NAL units
	// - c copy: No transcoding (for video), just copy streams
	// - fflags +genpts+igndts: Regenerate timestamps (Enigma2 streams have broken DTS)
	args := []string{
		"-hide_banner",
	}
	args = append(args, logLevelArgs("error", "")...)
	args = append(args,
		"-fflags", "+genpts+igndts", // Generate PTS, ignore broken DTS
		"-i", "pipe:0", // Read from stdin
		"-map", "0:v", "-c:v", "copy", // Copy video stream without transcoding
		"-bsf:v", "h264_mp4toannexb", // CRITICAL: Add PPS/SPS headers for H.264 Annex-B
	)

	// Audio handling: Transcode to AAC if enabled (for iOS support), otherwise copy
	if t.Config.Enabled {
		args = append(args,
			"-map", "0:a", "-c:a", t.Config.Codec, // Transcode audio (usually aac)
			"-b:a", t.Config.Bitrate, // Audio bitrate
			"-ac", fmt.Sprintf("%d", t.Config.Channels), // Audio channels
			"-async", "1", // Audio-video sync
		)
	} else {
		args = append(args,
			"-map", "0:a", "-c:a", "copy", // Copy audio stream without transcoding
		)
	}

	// Add output options
	args = append(args,
		"-start_at_zero",                  // Start timestamps at zero
		"-avoid_negative_ts", "make_zero", // Fix negative timestamps
		"-muxdelay", "0", // No mux delay
		"-muxpreload", "0", // No mux preload
		"-mpegts_copyts", "1", // Preserve timestamps in MPEG-TS
		"-mpegts_flags", "resend_headers+initial_discontinuity", // Regenerate PAT/PMT
		"-pcr_period", "20", // Insert PCR every 20ms
		"-pat_period", "0.1", // Regenerate PAT every 100ms
		"-sdt_period", "0.5", // Regenerate SDT every 500ms
		"-f", "mpegts", // Output format
		"pipe:1", // Write to stdout
	)

	t.logger.Info().
		Str("ffmpeg_path", t.Config.FFmpegPath).
		Strs("args", args).
		Str("target_url", targetURL).
		Msg("starting H.264 stream repair")

	// Ensure the ffmpeg path is clean and absolute before execution
	ffmpegPath := filepath.Clean(t.Config.FFmpegPath)
	if !filepath.IsAbs(ffmpegPath) {
		return fmt.Errorf("ffmpeg path must be absolute: %s", ffmpegPath)
	}

	// Create ffmpeg command
	// #nosec G204 -- ffmpegPath is sanitized above and args contain only predefined options
	cmd := exec.CommandContext(ctx, ffmpegPath, args...) // #nosec G204

	// Connect pipes
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("create stderr pipe: %w", err)
	}

	// Start ffmpeg
	span.AddEvent("starting ffmpeg H.264 repair")
	metrics.IncFFmpegRestart()
	if err := cmd.Start(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to start ffmpeg")
		return fmt.Errorf("start ffmpeg: %w", err)
	}

	// Log ffmpeg stderr in background with stats
	var stderrWg sync.WaitGroup
	stderrWg.Add(1)
	go func() {
		defer stderrWg.Done()
		scanner := bufio.NewScanner(stderr)
		statsTicker := time.NewTicker(5 * time.Second)
		defer statsTicker.Stop()
		var statsSeq int

		for scanner.Scan() {
			line := scanner.Text()
			// Parse stats
			if stats := ParseFFmpegStats(line); stats != nil {
				lastStats = stats
				select {
				case <-statsTicker.C:
					statsSeq++
					logger.Info().
						Str("event", "ffmpeg_stats").
						Int("seq", statsSeq).
						Float64("speed", stats.Speed).
						Float64("bitrate_kbps", stats.BitrateKBPS).
						Msg("ffmpeg progress")
				default:
				}
			} else {
				if strings.Contains(strings.ToLower(line), "error") {
					logger.Warn().Str("stderr", line).Msg("ffmpeg warning")
				} else {
					logger.Debug().Str("stderr", line).Msg("ffmpeg repair output")
				}
			}
		}
	}()

	// Use WaitGroup to ensure all goroutines complete
	var wg sync.WaitGroup
	errChan := make(chan error, 2)

	// Copy stream from target to ffmpeg stdin
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if err := stdin.Close(); err != nil {
				logger.Debug().Err(err).Msg("failed to close stdin")
			}
		}()
		if _, err := io.Copy(stdin, resp.Body); err != nil {
			if !strings.Contains(err.Error(), "broken pipe") {
				errChan <- fmt.Errorf("copy to ffmpeg stdin: %w", err)
			}
		}
	}()

	// Set response headers for repaired stream
	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Connection", "close")

	// Copy ffmpeg output to response writer
	wg.Add(1)
	go func() {
		defer wg.Done()
		n, err := io.Copy(w, stdout)
		if n > 0 {
			atomic.AddInt64(&bytesOut, n)
		}
		if err != nil {
			if ctx.Err() != nil {
				exitReason = "client_disconnect"
			} else if strings.Contains(err.Error(), "broken pipe") {
				exitReason = "client_disconnect"
			} else {
				exitReason = "ffmpeg_exit"
			}
			if !strings.Contains(err.Error(), "broken pipe") && !strings.Contains(err.Error(), "connection reset") {
				errChan <- fmt.Errorf("copy from ffmpeg stdout: %w", err)
			}
		} else {
			if ctx.Err() != nil {
				exitReason = "client_disconnect"
			} else {
				exitReason = "ffmpeg_exit"
			}
		}
	}()

	// Wait for all copy operations to complete
	wg.Wait()

	// Wait for stderr log to finish
	stderrWg.Wait()

	// Wait for ffmpeg to exit
	if err := cmd.Wait(); err != nil {
		// Only log error if it's not a context cancellation
		if ctx.Err() == nil {
			logger.Debug().Err(err).Msg("ffmpeg H.264 repair exited with error")
		}
	}

	if exitReason == "internal_error" {
		exitReason = "success"
	}

	// Check for errors from goroutines
	select {
	case err := <-errChan:
		return err
	default:
		span.SetStatus(codes.Ok, "H.264 stream repair completed")
		return nil
	}
}

// ProxyToGPUTranscoder forwards the stream request to the GPU transcoder service.
// The GPU transcoder handles full video+audio transcoding with VAAPI hardware acceleration.
func (t *Transcoder) ProxyToGPUTranscoder(ctx context.Context, w http.ResponseWriter, r *http.Request, sourceURL string) error {
	// Start tracing span
	tracer := telemetry.Tracer("xg2g.proxy")
	ctx, span := tracer.Start(ctx, "transcode.gpu",
		trace.WithSpanKind(trace.SpanKindInternal),
	)
	defer span.End()

	// Add transcoding attributes
	span.SetAttributes(
		attribute.String(telemetry.TranscodeCodecKey, "hevc"), // GPU typically uses HEVC
		attribute.String(telemetry.TranscodeDeviceKey, "vaapi"),
		attribute.Bool(telemetry.TranscodeGPUEnabledKey, true),
		attribute.String("transcoder.url", t.Config.TranscoderURL),
	)

	// Build GPU transcoder URL with source_url parameter
	transcoderURL := fmt.Sprintf("%s/transcode?source_url=%s",
		t.Config.TranscoderURL,
		url.QueryEscape(sourceURL))

	t.logger.Debug().
		Str("source_url", sourceURL).
		Str("transcoder_url", transcoderURL).
		Msg("proxying to GPU transcoder")

	// Create request to GPU transcoder
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, transcoderURL, nil)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to create GPU transcoder request")
		return fmt.Errorf("create GPU transcoder request: %w", err)
	}

	// Copy User-Agent from original request if present
	if ua := r.Header.Get("User-Agent"); ua != "" {
		req.Header.Set("User-Agent", ua)
	}

	// Execute request with no timeout (streaming)
	span.AddEvent("connecting to GPU transcoder")
	client := &http.Client{
		Timeout: 0,
	}
	resp, err := client.Do(req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "GPU transcoder request failed")
		return fmt.Errorf("GPU transcoder request failed: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.logger.Debug().Err(err).Msg("failed to close GPU transcoder response")
		}
	}()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		span.SetAttributes(attribute.Int("http.status_code", resp.StatusCode))
		span.SetStatus(codes.Error, fmt.Sprintf("GPU transcoder returned status %d", resp.StatusCode))
		return fmt.Errorf("GPU transcoder returned status %d", resp.StatusCode)
	}

	// Copy response headers to client
	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Connection", "close")

	// Copy additional headers from GPU transcoder response
	for key, values := range resp.Header {
		// Skip headers we already set
		if key == "Content-Type" || key == "Cache-Control" || key == "Connection" {
			continue
		}
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// Stream response body from GPU transcoder to client
	span.AddEvent("streaming transcoded output")
	_, err = io.Copy(w, resp.Body)
	if err != nil && !isContextCancelled(ctx) && !isBrokenPipe(err) {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to stream GPU transcoder output")
		return fmt.Errorf("failed to stream GPU transcoder output: %w", err)
	}

	span.SetStatus(codes.Ok, "GPU transcode completed successfully")
	return nil
}

// IsGPUEnabled returns whether GPU transcoding is enabled.
func (t *Transcoder) IsGPUEnabled() bool {
	return t.Config.GPUEnabled
}

// isContextCancelled checks if the context was cancelled (client disconnect).
func isContextCancelled(ctx context.Context) bool {
	return ctx.Err() == context.Canceled
}

// isBrokenPipe checks if the error is a broken pipe (client disconnect).
func isBrokenPipe(err error) bool {
	return strings.Contains(err.Error(), "broken pipe") ||
		strings.Contains(err.Error(), "connection reset")
}

// copyHeaders copies whitelisted headers from source to dest.
// Safe for upstream proxying.
func copyHeaders(src, dst http.Header) {
	// Whitelist of safe headers to forward to Enigma2
	// Enigma2 is picky about Auth and Host, but hates connection/upgrade headers from proxies.
	whitelist := []string{
		"Authorization",
		"User-Agent",
		"Accept",
		"Accept-Language",
		"X-Forwarded-For",
		"X-Real-Ip",
	}

	for _, k := range whitelist {
		if vals := src.Values(k); len(vals) > 0 {
			for _, v := range vals {
				dst.Add(k, v)
			}
		}
	}
}

// TranscodeStreamRust transcodes audio using the native Rust remuxer.
// This provides zero-latency audio remuxing without spawning external processes.
//
// Architecture:
//
//	Input: MPEG-TS with MP2/AC3 audio from Enigma2
//	Pipeline: Demux → Decode → Encode (AAC-LC) → Mux
//	Output: MPEG-TS with AAC audio for iOS Safari
//
// Performance:
//   - Latency: ~39µs per 192KB chunk (vs 200-500ms with FFmpeg)
//   - Throughput: 4.94 GB/s
//   - CPU: <0.1%
//   - Memory: <1MB per stream
func (t *Transcoder) TranscodeStreamRust(ctx context.Context, w http.ResponseWriter, r *http.Request, targetURL string) error {
	// Start tracing span
	tracer := telemetry.Tracer("xg2g.proxy")
	ctx, span := tracer.Start(ctx, "transcode.rust",
		trace.WithSpanKind(trace.SpanKindInternal),
	)
	defer span.End()

	// Add transcoding attributes
	span.SetAttributes(
		attribute.String(telemetry.TranscodeCodecKey, "aac"),
		attribute.String(telemetry.TranscodeDeviceKey, "rust-native"),
		attribute.Bool("rust.remuxer", true),
	)

	// Parse sample rate from config (default 48000 Hz for broadcast)
	sampleRate := 48000

	// Parse bitrate from config string (e.g., "192k" -> 192000)
	bitrate := 192000
	if t.Config.Bitrate != "" {
		bitrateStr := strings.TrimSuffix(strings.ToLower(t.Config.Bitrate), "k")
		if parsedBitrate, err := strconv.Atoi(bitrateStr); err == nil {
			bitrate = parsedBitrate * 1000
		}
	}

	// Initialize Rust audio remuxer
	span.AddEvent("initializing rust remuxer")
	remuxer, err := transcoder.NewRustAudioRemuxer(sampleRate, t.Config.Channels, bitrate)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to initialize rust remuxer")
		t.logger.Error().Err(err).Msg("failed to initialize rust remuxer")
		return fmt.Errorf("initialize rust remuxer: %w", err)
	}
	defer func() {
		if err := remuxer.Close(); err != nil {
			t.logger.Warn().Err(err).Msg("failed to close remuxer")
		}
	}()

	t.logger.Info().
		Int("sample_rate", sampleRate).
		Int("channels", t.Config.Channels).
		Int("bitrate", bitrate).
		Msg("rust remuxer initialized")

	// Create request to target
	proxyReq, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to create proxy request")
		return fmt.Errorf("create proxy request: %w", err)
	}

	// Copy headers from original request
	for key, values := range r.Header {
		for _, value := range values {
			proxyReq.Header.Add(key, value)
		}
	}

	// Execute request to target
	span.AddEvent("fetching source stream")
	client := &http.Client{}
	resp, err := client.Do(proxyReq)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "proxy request failed")
		return fmt.Errorf("proxy request failed: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.logger.Debug().Err(err).Msg("failed to close response body")
		}
	}()

	// Set response headers for MPEG-TS streaming
	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Connection", "close")

	// Copy status code from target
	w.WriteHeader(resp.StatusCode)

	// Stream processing loop
	span.AddEvent("starting rust remuxing stream")

	// Buffer for reading MPEG-TS packets (multiple of 188 bytes)
	const tsPacketSize = 188
	const bufferPackets = 16 // Process 16 packets at a time (3008 bytes)
	inputBuf := make([]byte, tsPacketSize*bufferPackets)

	var (
		totalInput  int64
		totalOutput int64
		errors      int
	)

	for {
		// Check context cancellation
		select {
		case <-ctx.Done():
			t.logger.Debug().Msg("context cancelled, stopping rust remuxing")
			span.AddEvent("context cancelled")
			return nil
		default:
		}

		// Read chunk from source stream
		n, readErr := io.ReadFull(resp.Body, inputBuf)
		if readErr != nil && readErr != io.EOF && readErr != io.ErrUnexpectedEOF {
			// Broken pipe is expected when client disconnects
			if !isExpectedStreamError(readErr) {
				t.logger.Warn().Err(readErr).Msg("error reading from source stream")
				span.RecordError(readErr)
				errors++
			}
			break
		}

		if n == 0 {
			break
		}

		totalInput += int64(n)

		// Process through Rust remuxer
		output, err := remuxer.Process(inputBuf[:n])
		if err != nil {
			t.logger.Error().Err(err).Msg("rust remuxing failed")
			span.RecordError(err)
			errors++

			// On error, pass through original data to maintain stream continuity
			if _, writeErr := w.Write(inputBuf[:n]); writeErr != nil {
				if !isExpectedStreamError(writeErr) {
					t.logger.Warn().Err(writeErr).Msg("error writing passthrough data")
				}
				break
			}
			continue
		}

		// Write remuxed data to client
		written, writeErr := w.Write(output)
		if writeErr != nil {
			if !isExpectedStreamError(writeErr) {
				t.logger.Warn().Err(writeErr).Msg("error writing to client")
				span.RecordError(writeErr)
			}
			break
		}

		totalOutput += int64(written)

		// Flush to ensure immediate delivery
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}

		// End of stream
		if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
			break
		}
	}

	// Record metrics
	span.SetAttributes(
		attribute.Int64("bytes.input", totalInput),
		attribute.Int64("bytes.output", totalOutput),
		attribute.Int("errors", errors),
	)

	t.logger.Info().
		Int64("bytes_input", totalInput).
		Int64("bytes_output", totalOutput).
		Int("errors", errors).
		Float64("compression_ratio", float64(totalOutput)/float64(totalInput)).
		Msg("rust remuxing stream completed")

	span.SetStatus(codes.Ok, "stream completed")
	return nil
}

// isExpectedStreamError returns true for errors that are expected during streaming
// (e.g., broken pipe when client disconnects).
func isExpectedStreamError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()
	return strings.Contains(errStr, "broken pipe") ||
		strings.Contains(errStr, "connection reset") ||
		strings.Contains(errStr, "write: connection timed out") ||
		strings.Contains(errStr, "i/o timeout")
}
