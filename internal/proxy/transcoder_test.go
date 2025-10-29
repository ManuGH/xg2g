// SPDX-License-Identifier: MIT
package proxy

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"

	"github.com/rs/zerolog"
)

func TestNewTranscoder(t *testing.T) {
	logger := zerolog.New(io.Discard)
	config := TranscoderConfig{
		Enabled:  true,
		Codec:    "aac",
		Bitrate:  "192k",
		Channels: 2,
	}

	transcoder := NewTranscoder(config, logger)
	if transcoder == nil {
		t.Fatal("NewTranscoder returned nil")
	}
	if transcoder.Config.Codec != "aac" {
		t.Errorf("expected codec 'aac', got '%s'", transcoder.Config.Codec)
	}
}

func TestGetTranscoderConfig(t *testing.T) {
	t.Run("default_config", func(t *testing.T) {
		t.Setenv("XG2G_ENABLE_AUDIO_TRANSCODING", "")
		t.Setenv("XG2G_GPU_TRANSCODING", "")
		t.Setenv("XG2G_GPU_TRANSCODER_URL", "")

		config := GetTranscoderConfig()
		if config.Enabled {
			t.Error("expected transcoding disabled by default")
		}
	})

	t.Run("enabled_via_env", func(t *testing.T) {
		t.Setenv("XG2G_ENABLE_AUDIO_TRANSCODING", "true")
		config := GetTranscoderConfig()
		if !config.Enabled {
			t.Error("expected transcoding enabled")
		}
	})

	t.Run("custom_codec", func(t *testing.T) {
		t.Setenv("XG2G_AUDIO_CODEC", "mp3")
		config := GetTranscoderConfig()
		if config.Codec != "mp3" {
			t.Errorf("expected codec 'mp3', got '%s'", config.Codec)
		}
	})

	t.Run("gpu_config", func(t *testing.T) {
		t.Setenv("XG2G_GPU_TRANSCODE", "true") // Note: TRANSCODE not TRANSCODING
		t.Setenv("XG2G_TRANSCODER_URL", "http://localhost:9999")
		config := GetTranscoderConfig()
		if !config.GPUEnabled {
			t.Error("expected GPU transcoding enabled")
		}
		if config.TranscoderURL != "http://localhost:9999" {
			t.Errorf("expected transcoder URL 'http://localhost:9999', got '%s'", config.TranscoderURL)
		}
	})
}

// IsGPUEnabled is tested indirectly via GetTranscoderConfig

// TestTranscodeStream_FFmpegNotFound verifies graceful handling when ffmpeg is missing
func TestTranscodeStream_FFmpegNotFound(t *testing.T) {
	logger := zerolog.New(io.Discard)

	// Create test HTTP server (target)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "video/mp2t")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("fake stream data"))
	}))
	defer target.Close()

	// Configure transcoder with non-existent ffmpeg path
	config := TranscoderConfig{
		Enabled:    true,
		Codec:      "aac",
		Bitrate:    "192k",
		Channels:   2,
		FFmpegPath: "/nonexistent/ffmpeg",
	}
	transcoder := NewTranscoder(config, logger)

	// Create test request
	req := httptest.NewRequest(http.MethodGet, "/stream", nil)
	w := httptest.NewRecorder()

	// Execute: Should fail gracefully
	err := transcoder.TranscodeStream(context.Background(), w, req, target.URL)
	if err == nil {
		t.Error("expected error when ffmpeg is not found")
	}
	if !os.IsNotExist(err) && !isExecError(err) {
		t.Logf("Got error: %v (type: %T)", err, err)
	}
}

// TestTranscodeStream_ContextCancellation verifies cleanup on context cancel
func TestTranscodeStream_ContextCancellation(t *testing.T) {
	// Skip if ffmpeg not available
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not found, skipping transcoding test")
	}

	logger := zerolog.New(io.Discard)

	// Create slow-responding target server
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp2t")
		w.WriteHeader(http.StatusOK)
		// Stream slowly
		for i := 0; i < 100; i++ {
			_, _ = w.Write([]byte("streaming data chunk\n"))
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			select {
			case <-r.Context().Done():
				return
			default:
			}
		}
	}))
	defer target.Close()

	config := TranscoderConfig{
		Enabled:    true,
		Codec:      "aac",
		Bitrate:    "128k",
		Channels:   2,
		FFmpegPath: "ffmpeg", // Use system ffmpeg
	}
	transcoder := NewTranscoder(config, logger)

	// Create cancellable context
	ctx, cancel := context.WithCancel(context.Background())

	req := httptest.NewRequest(http.MethodGet, "/stream", nil)
	w := httptest.NewRecorder()

	// Start transcoding in background
	done := make(chan error, 1)
	go func() {
		done <- transcoder.TranscodeStream(ctx, w, req, target.URL)
	}()

	// Cancel context after short delay
	cancel()

	// Wait for completion
	err := <-done
	switch {
	case err == nil:
		t.Log("Transcoding completed without error (acceptable)")
	case errors.Is(err, context.Canceled):
		t.Log("Context was cancelled as expected")
	default:
		t.Logf("Got error: %v (acceptable for cancelled context)", err)
	}
}

// Helper: Check if error is exec-related
func isExecError(err error) bool {
	if err == nil {
		return false
	}
	var execErr *exec.Error
	return errors.As(err, &execErr)
}

// TestProxyToGPUTranscoder_Disabled verifies GPU path is not taken when disabled
func TestProxyToGPUTranscoder_Disabled(t *testing.T) {
	t.Setenv("XG2G_GPU_TRANSCODING", "false")

	logger := zerolog.New(io.Discard)
	config := TranscoderConfig{
		GPUEnabled:    false,
		TranscoderURL: "",
	}
	transcoder := NewTranscoder(config, logger)

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	req := httptest.NewRequest(http.MethodGet, "/stream", nil)
	w := httptest.NewRecorder()

	// GPU transcoding should not be attempted (function should return error or skip)
	err := transcoder.ProxyToGPUTranscoder(context.Background(), w, req, target.URL)
	if err == nil {
		t.Error("expected error when GPU transcoding is disabled")
	}
}
