// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// SPDX-License-Identifier: MIT
package proxy

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
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

func TestBuildTranscoderConfigFromRuntime(t *testing.T) {
	t.Parallel()

	t.Run("ffmpeg_missing_disables_ffmpeg_features", func(t *testing.T) {
		t.Parallel()

		cfg := buildTranscoderConfigFromRuntime(config.TranscoderRuntime{
			Enabled:           true,
			H264RepairEnabled: true,
			AudioEnabled:      true,
			Codec:             "aac",
			Bitrate:           "192k",
			Channels:          2,
			FFmpegPath:        "/nonexistent/ffmpeg",
			UseRustRemuxer:    false,
			VideoTranscode:    true,
		})

		if cfg.Enabled {
			t.Errorf("expected audio transcoding disabled when ffmpeg is missing and rust is disabled")
		}
		if cfg.H264RepairEnabled {
			t.Errorf("expected H.264 repair disabled when ffmpeg is missing")
		}
		if cfg.VideoTranscode {
			t.Errorf("expected video transcode disabled when ffmpeg is missing")
		}
	})

	t.Run("rust_keeps_audio_without_ffmpeg", func(t *testing.T) {
		t.Parallel()

		cfg := buildTranscoderConfigFromRuntime(config.TranscoderRuntime{
			Enabled:        true,
			AudioEnabled:   true,
			FFmpegPath:     "/nonexistent/ffmpeg",
			UseRustRemuxer: true,
		})

		if !cfg.Enabled {
			t.Errorf("expected audio enabled when rust remuxer is enabled even without ffmpeg")
		}
		if cfg.H264RepairEnabled {
			t.Errorf("expected H.264 repair disabled when ffmpeg is missing")
		}
	})
}

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
	if !isExecError(err) {
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
