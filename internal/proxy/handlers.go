// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package proxy

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/ManuGH/xg2g/internal/metrics"
)

// tryHandleHEAD handles HEAD requests.
// Returns true if request was handled.
func (s *Server) tryHandleHEAD(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodHead {
		// Proxy Logic:
		// If path implies HLS (.m3u8, .m4s) -> Return 200 OK (Content-Type: application/vnd.apple.mpegurl or similar)
		// If path implies MPEG-TS -> Return 200 OK (Content-Type: video/mp2t)
		// Otherwise -> 404

		path := r.URL.Path
		if strings.HasSuffix(path, ".m3u8") ||
			strings.HasSuffix(path, ".m4s") ||
			strings.HasSuffix(path, ".cmfv") {
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl") // or binary
			w.Header().Set("Cache-Control", "no-cache")
			w.WriteHeader(http.StatusOK)
			return true
		}

		// Default to MPEG-TS for legacy stream URLs
		w.Header().Set("Content-Type", "video/mp2t")
		w.Header().Set("Accept-Ranges", "none")
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Connection", "close")
		w.WriteHeader(http.StatusOK)

		s.logger.Debug().
			Str("path", r.URL.Path).
			Msg("answered HEAD request")
		return true
	}
	return false
}

// tryHandleHLS handles HLS related requests (UA-based routing and serving profile files).
// Returns true if request was handled.
func (s *Server) tryHandleHLS(w http.ResponseWriter, r *http.Request) bool {
	// Skip if HLS is disabled or not a GET request
	if s.hlsManager == nil || r.Method != http.MethodGet {
		return false
	}

	path := r.URL.Path

	// 1. Explicit HLS requests
	// - /hls/...
	// - *.m3u8, *.m4s, *.cmfv, *.cmfa (CMAF/LL-HLS)
	// - segment_*.ts (Legacy HLS)
	if strings.HasPrefix(path, "/hls/") ||
		strings.HasSuffix(path, ".m3u8") ||
		strings.HasSuffix(path, ".m4s") ||
		strings.HasSuffix(path, ".cmfv") ||
		strings.HasSuffix(path, ".cmfa") ||
		strings.HasSuffix(path, ".mp4") ||
		(strings.Contains(path, "segment_") && strings.HasSuffix(path, ".ts")) {

		// Handle segment requests without /hls/ prefix (Safari sometimes does this)
		if !strings.HasPrefix(path, "/hls/") && strings.Contains(path, "segment_") && strings.HasSuffix(path, ".ts") {
			segmentName := filepath.Base(path)
			if err := s.hlsManager.ServeSegmentFromAnyStream(w, segmentName); err != nil {
				s.logger.Error().Err(err).Str("segment", segmentName).Msg("failed to serve HLS segment")
				http.Error(w, "Segment not found", http.StatusNotFound)
			}
			return true
		}

		// Handle playlist/stream requests
		s.handleHLSRequest(w, r)
		return true
	}

	// 2. Client auto-detection (client requests a regular path, e.g. /1:0:1...)
	// Exclude HLS component files to prevent recursion loop
	if !strings.HasSuffix(path, ".ts") {
		userAgent := r.Header.Get("User-Agent")

		// Check for Apple native clients (AVFoundation/WebKit HLS stack)
		isIOSClient := (strings.Contains(userAgent, "iPhone") ||
			strings.Contains(userAgent, "iPad") ||
			strings.Contains(userAgent, "iOS") ||
			strings.Contains(userAgent, "AppleCoreMedia") ||
			strings.Contains(userAgent, "CFNetwork"))

		if isIOSClient {
			hlsPath := "/hls" + path
			s.logger.Info().
				Str("user_agent", userAgent).
				Str("original_path", path).
				Str("hls_path", hlsPath).
				Str("client_ip", r.RemoteAddr).
				Msg("auto-redirecting iOS client to HLS")

			r.URL.Path = hlsPath
			s.handleHLSRequest(w, r)
			return true
		}
	}

	return false
}

// tryHandleTranscode handles transcoding (Stream Repair, GPU, Rust, FFmpeg).
// Returns true if request was handled (either successfully or failed with error response).
// Returns false if we should fall back to direct proxying.
func (s *Server) tryHandleTranscode(w http.ResponseWriter, r *http.Request) bool {
	// Only GET requests are transcoded
	if r.Method != http.MethodGet || s.transcoder == nil {
		return false
	}

	targetURL, ok := s.resolveTargetURL(r.Context(), r.URL.Path, r.URL.RawQuery)
	if !ok {
		return false
	}
	// Priority 1: GPU Transcoding (Explicit override)
	if s.transcoder.IsGPUEnabled() {
		s.logger.Debug().
			Str("path", r.URL.Path).
			Str("target", targetURL).
			Msg("routing stream through GPU transcoder")

		if err := s.transcoder.ProxyToGPUTranscoder(r.Context(), w, r, targetURL); err != nil {
			if !errors.Is(err, context.Canceled) {
				if s.handleTranscodeError(w, r, err) {
					return true
				}
			}
			return !s.transcodeFailOpen
		}
		return true
	}

	// Track active stream
	metrics.IncActiveStreams("transcode")
	defer metrics.DecActiveStreams("transcode")

	// Priority 2: Audio-only Transcoding (Rust Remuxer) - High Performance / Low Latency
	// Checked BEFORE Repair because user wants "Rust Standard".
	// Note: This means if Rust is enabled, Repair (Plex) will NOT be reached unless Rust fails.
	var err error
	if s.transcoder.Config.UseRustRemuxer {
		s.logger.Debug().Str("method", "rust").Msg("attempting native rust remuxer")
		err = s.transcoder.TranscodeStreamRust(r.Context(), w, r, targetURL)

		if err == nil {
			return true // Success
		}

		// If failed (and not client disconnect/cancel), we might fall back
		if r.Context().Err() != nil {
			// Client disconnected
			s.logger.Debug().Msg("rust transcoding stopped (client disconnected)")
			return true
		}

		s.logger.Warn().Err(err).Msg("rust remuxer failed, falling back to next available strategy")
	}

	// Priority 3: H.264 Stream Repair (FFmpeg) - For Plex/Enigma2 compatibility
	// Only reached if Rust is disabled or failed.
	if s.transcoder.Config.H264RepairEnabled {
		s.logger.Info().
			Str("path", r.URL.Path).
			Str("target", targetURL).
			Msg("routing stream through H.264 PPS/SPS repair (FFmpeg)")

		metrics.IncActiveStreams("repair")
		defer metrics.DecActiveStreams("repair")

		if err := s.transcoder.RepairH264Stream(r.Context(), w, r, targetURL); err != nil {
			if !errors.Is(err, context.Canceled) {
				if s.handleTranscodeError(w, r, err) {
					return true
				}
				metrics.IncTranscodeError()
			}
			return !s.transcodeFailOpen
		}
		return true
	}

	// Priority 4: FFmpeg Transcoding (Legacy/Fallback)
	// If Rust failed or was disabled, and Repair didn't catch it
	s.logger.Debug().Str("method", "ffmpeg").Msg("using ffmpeg transcoding")
	err = s.transcoder.TranscodeStream(r.Context(), w, r, targetURL)

	if err == nil || r.Context().Err() != nil {
		if err != nil {
			s.logger.Debug().Str("path", r.URL.Path).Msg("audio transcoding stopped (client disconnected)")
		}
		return true
	}

	// If all transcoding failed -> fallback to direct
	if s.handleTranscodeError(w, r, err) {
		return true
	}
	metrics.IncTranscodeError()
	return !s.transcodeFailOpen
}

// handleTranscodeError enforces fail-open/fail-closed behaviour for the
// transcoder pipeline. Returns true if the error has been fully handled
// (response written), false if caller should fallback to direct proxy.
func (s *Server) handleTranscodeError(w http.ResponseWriter, r *http.Request, err error) bool {
	if s.transcodeFailOpen {
		s.logger.Warn().
			Err(err).
			Str("path", r.URL.Path).
			Msg("transcode pipeline failed, failing open to direct proxy")
		return false
	}

	s.logger.Error().
		Err(err).
		Str("path", r.URL.Path).
		Msg("transcode pipeline failed, returning 502")
	http.Error(w, "transcode failed", http.StatusBadGateway)
	return true
}
