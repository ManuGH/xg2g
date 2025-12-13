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

// tryHandleHLS handles HLS related requests (iOS/Plex detection and serving files).
// Returns true if request was handled.
func (s *Server) tryHandleHLS(w http.ResponseWriter, r *http.Request) bool {
	// Skip if HLS is disabled or not a GET request
	if s.hlsManager == nil || r.Method != http.MethodGet {
		return false
	}

	path := r.URL.Path
	// s.logger.Info().Str("path", path).Bool("manager_ok", s.hlsManager != nil).Str("method", r.Method).Msg("DEBUG: tryHandleHLS checking request")

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

	// 2. Client Auto-Detection (Client is requesting a regular path, e.g. /1:0:1...)
	// Exclude HLS component files to prevent recursion loop
	if !strings.HasSuffix(path, ".ts") {
		userAgent := r.Header.Get("User-Agent")

		// Check for Plex
		if IsPlexClient(userAgent) {
			hlsPath := "/hls" + path
			s.logger.Info().
				Str("user_agent", userAgent).
				Str("original_path", path).
				Str("hls_path", hlsPath).
				Str("client_ip", r.RemoteAddr).
				Msg("auto-redirecting Plex client to optimized HLS profile")

			r.URL.Path = hlsPath
			s.handleHLSRequest(w, r)
			return true
		}

		// Check for iOS
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

	targetURL := s.resolveTargetURL(r.Context(), r.URL.Path, r.URL.RawQuery)

	// Priority 0: H.264 Stream Repair
	if s.transcoder.Config.H264RepairEnabled {
		s.logger.Info().
			Str("path", r.URL.Path).
			Str("target", targetURL).
			Msg("routing stream through H.264 PPS/SPS repair (Plex compatibility fix)")

		metrics.IncActiveStreams("repair")
		defer metrics.DecActiveStreams("repair")

		if err := s.transcoder.RepairH264Stream(r.Context(), w, r, targetURL); err != nil {
			if !errors.Is(err, context.Canceled) {
				s.logger.Error().Err(err).Str("path", r.URL.Path).Msg("H.264 stream repair failed, falling back to direct proxy")
				metrics.IncTranscodeError()
			}
			return false // Fallback to direct
		}
		return true
	}

	// Priority 1: GPU Transcoding
	if s.transcoder.IsGPUEnabled() {
		s.logger.Debug().
			Str("path", r.URL.Path).
			Str("target", targetURL).
			Msg("routing stream through GPU transcoder")

		if err := s.transcoder.ProxyToGPUTranscoder(r.Context(), w, r, targetURL); err != nil {
			if !errors.Is(err, context.Canceled) {
				s.logger.Error().Err(err).Str("path", r.URL.Path).Msg("GPU transcoding failed, falling back to direct proxy")
			}
			return false // Fallback to direct
		}
		return true
	}

	// Track active stream
	metrics.IncActiveStreams("transcode")
	defer metrics.DecActiveStreams("transcode")

	// Priority 2: Audio-only Transcoding (Rust/FFmpeg)
	var err error
	if s.transcoder.Config.UseRustRemuxer {
		s.logger.Debug().Str("method", "rust").Msg("attempting native rust remuxer")
		err = s.transcoder.TranscodeStreamRust(r.Context(), w, r, targetURL)

		// If Rust fails (and not client disconnect), try FFmpeg
		if err != nil && r.Context().Err() == nil {
			s.logger.Warn().Err(err).Str("path", r.URL.Path).Msg("rust remuxer failed, falling back to FFmpeg subprocess")
			s.logger.Debug().Str("method", "ffmpeg").Msg("using ffmpeg transcoding")

			// Increment restart/fallback metric if desired, or just error?
			// The user asked for "ffmpeg_restarts", maybe this counts as a "restart" of the pipeline strategy
			// But specialized "rust_fallback" might be better.
			// For now, let's just log it.

			err = s.transcoder.TranscodeStream(r.Context(), w, r, targetURL)
		}
	} else {
		// Rust disabled, use FFmpeg
		s.logger.Debug().Str("method", "ffmpeg").Msg("using ffmpeg transcoding")
		err = s.transcoder.TranscodeStream(r.Context(), w, r, targetURL)
	}

	if err == nil || r.Context().Err() != nil {
		if err != nil {
			s.logger.Debug().Str("path", r.URL.Path).Msg("audio transcoding stopped (client disconnected)")
		}
		return true
	}

	// If all transcoding failed -> fallback to direct
	s.logger.Warn().Err(err).Str("path", r.URL.Path).Msg("all transcoding methods failed, falling back to direct proxy")
	metrics.IncTranscodeError()
	return false
}
