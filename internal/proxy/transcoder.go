package proxy

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/rs/zerolog"
)

// TranscoderConfig holds configuration for audio transcoding.
type TranscoderConfig struct {
	Enabled    bool   // Whether transcoding is enabled
	Codec      string // Target audio codec (aac, mp3)
	Bitrate    string // Audio bitrate (e.g., "192k")
	Channels   int    // Number of audio channels (2 for stereo)
	FFmpegPath string // Path to ffmpeg binary
}

// Transcoder handles audio transcoding for streams.
type Transcoder struct {
	config TranscoderConfig
	logger zerolog.Logger
}

// NewTranscoder creates a new audio transcoder.
func NewTranscoder(config TranscoderConfig, logger zerolog.Logger) *Transcoder {
	return &Transcoder{
		config: config,
		logger: logger,
	}
}

// TranscodeStream transcodes the audio of a stream from the target URL.
// It proxies the request to the target, pipes it through ffmpeg for audio transcoding,
// and streams the result back to the client.
func (t *Transcoder) TranscodeStream(ctx context.Context, w http.ResponseWriter, r *http.Request, targetURL string) error {
	// Create request to target
	proxyReq, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return fmt.Errorf("create proxy request: %w", err)
	}

	// Copy headers from original request
	for key, values := range r.Header {
		for _, value := range values {
			proxyReq.Header.Add(key, value)
		}
	}

	// Execute request to target
	client := &http.Client{}
	resp, err := client.Do(proxyReq)
	if err != nil {
		return fmt.Errorf("proxy request failed: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.logger.Debug().Err(err).Msg("failed to close response body")
		}
	}()

	// Build ffmpeg command for audio transcoding
	// Input: MPEG-TS stream from stdin
	// Output: MPEG-TS stream with transcoded audio to stdout
	//
	// CRITICAL: Do NOT use -copyts!
	// Enigma2 streams have broken DTS timestamps. We must regenerate them.
	// Using -start_at_zero + -fflags genpts instead of -copyts fixes audio sync issues.
	args := []string{
		"-hide_banner",
		"-loglevel", "error",
		"-fflags", "+genpts+igndts", // Generate PTS, ignore broken DTS
		"-i", "pipe:0", // Read from stdin
		"-map", "0:v", "-c:v", "copy", // Copy video stream
		"-map", "0:a", "-c:a", t.config.Codec, // Transcode audio
		"-b:a", t.config.Bitrate, // Audio bitrate
		"-ac", fmt.Sprintf("%d", t.config.Channels), // Audio channels
		"-async", "1", // Audio-video sync
		"-start_at_zero",                  // Start timestamps at zero
		"-avoid_negative_ts", "make_zero", // Fix negative timestamps
		"-muxdelay", "0", // No mux delay
		"-muxpreload", "0", // No mux preload
		"-f", "mpegts", // Output format
		"pipe:1", // Write to stdout
	}

	t.logger.Debug().
		Str("ffmpeg_path", t.config.FFmpegPath).
		Strs("args", args).
		Msg("starting ffmpeg transcoding")

	// Create ffmpeg command
	cmd := exec.CommandContext(ctx, t.config.FFmpegPath, args...)

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
		return fmt.Errorf("start ffmpeg: %w", err)
	}

	// Log ffmpeg stderr in background
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			t.logger.Debug().Str("ffmpeg_stderr", scanner.Text()).Msg("ffmpeg output")
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
				t.logger.Debug().Err(err).Msg("failed to close stdin")
			}
		}()
		if _, err := io.Copy(stdin, resp.Body); err != nil {
			if !strings.Contains(err.Error(), "broken pipe") {
				errChan <- fmt.Errorf("copy to ffmpeg stdin: %w", err)
			}
		}
	}()

	// Set response headers for transcoded stream
	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Connection", "close")

	// Copy ffmpeg output to response writer
	wg.Add(1)
	go func() {
		defer wg.Done()
		if _, err := io.Copy(w, stdout); err != nil {
			if !strings.Contains(err.Error(), "broken pipe") && !strings.Contains(err.Error(), "connection reset") {
				errChan <- fmt.Errorf("copy from ffmpeg stdout: %w", err)
			}
		}
	}()

	// Wait for all copy operations to complete
	wg.Wait()

	// Wait for ffmpeg to exit
	if err := cmd.Wait(); err != nil {
		// Only log error if it's not a context cancellation
		if ctx.Err() == nil {
			t.logger.Debug().Err(err).Msg("ffmpeg exited with error")
		}
	}

	// Check for errors from goroutines
	select {
	case err := <-errChan:
		return err
	default:
		return nil
	}
}

// IsEnabled checks if audio transcoding is enabled via environment variable.
func IsTranscodingEnabled() bool {
	return strings.ToLower(os.Getenv("XG2G_ENABLE_AUDIO_TRANSCODING")) == "true"
}

// GetTranscoderConfig builds transcoder configuration from environment variables.
func GetTranscoderConfig() TranscoderConfig {
	codec := os.Getenv("XG2G_AUDIO_CODEC")
	if codec == "" {
		codec = "aac" // Default to AAC
	}

	bitrate := os.Getenv("XG2G_AUDIO_BITRATE")
	if bitrate == "" {
		bitrate = "192k" // Default bitrate
	}

	channels := 2 // Default to stereo
	if ch := os.Getenv("XG2G_AUDIO_CHANNELS"); ch != "" {
		if ch == "1" {
			channels = 1
		}
	}

	ffmpegPath := os.Getenv("XG2G_FFMPEG_PATH")
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg" // Use system ffmpeg by default
	}

	return TranscoderConfig{
		Enabled:    IsTranscodingEnabled(),
		Codec:      codec,
		Bitrate:    bitrate,
		Channels:   channels,
		FFmpegPath: ffmpegPath,
	}
}
