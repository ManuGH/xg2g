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
	"sync/atomic"

	"github.com/ManuGH/xg2g/internal/metrics"
)

const (
	routeDecisionHLS   = "hls"
	routeDecisionTS    = "ts"
	routeDecisionProxy = "proxy"

	routeReasonGateRef    = "gate_ref"
	routeReasonGateSlug   = "gate_slug"
	routeReasonGateReject = "gate_reject"
	routeReasonQuery      = "query"
	routeReasonAccept     = "accept"
	routeReasonFetch      = "fetch"
	routeReasonDefault    = "default"
)

var defaultRoutingLogCounter atomic.Uint64

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

// tryHandleHLS handles HLS related requests and auto-routing for browser-like clients.
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
		lookup := func(id string) bool {
			_, ok := s.lookupStreamURL(id)
			return ok
		}

		decision := decideStreamRouting(path, r, lookup)
		metrics.IncStreamRouting(decision.decision, decision.reason)

		if decision.reason == routeReasonGateReject {
			// Only warn if an explicit HLS override was requested but the gate rejected it.
			q := r.URL.Query()
			if strings.EqualFold(q.Get("mode"), "hls") || q.Get("hls") == "1" {
				s.logger.Warn().
					Str("decision", decision.decision).
					Str("reason", decision.reason).
					Str("path_class", decision.pathClass).
					Msg("auto HLS rejected by gate")
			}
			return false
		}

		if decision.reason == routeReasonGateRef || decision.reason == routeReasonGateSlug {
			if defaultRoutingLogCounter.Add(1)%100 == 0 {
				s.logger.Debug().
					Str("decision", decision.decision).
					Str("reason", decision.reason).
					Str("path_class", decision.pathClass).
					Msg("auto-routing decision sampled")
			}
		}

		if decision.route {
			hlsPath := "/hls" + path
			s.logger.Info().
				Str("user_agent", r.Header.Get("User-Agent")).
				Str("original_path", path).
				Str("hls_path", hlsPath).
				Str("client_ip", r.RemoteAddr).
				Msg("auto-routing client to HLS")

			r.URL.Path = hlsPath
			s.handleHLSRequest(w, r)
			return true
		}
	}

	return false
}

type routingDecision struct {
	decision  string
	reason    string
	pathClass string
	route     bool
}

func decideStreamRouting(path string, r *http.Request, lookup func(string) bool) routingDecision {
	gateReason, streamLike := gateStreamPath(path, lookup)
	if !streamLike {
		return routingDecision{
			decision:  routeDecisionProxy,
			reason:    routeReasonGateReject,
			pathClass: "non_stream",
			route:     false,
		}
	}

	q := r.URL.Query()
	if strings.EqualFold(q.Get("mode"), "ts") || q.Get("ts") == "1" {
		return routingDecision{
			decision:  routeDecisionTS,
			reason:    routeReasonQuery,
			pathClass: pathClassFromGate(gateReason),
			route:     false,
		}
	}
	if strings.EqualFold(q.Get("mode"), "hls") || q.Get("hls") == "1" {
		return routingDecision{
			decision:  routeDecisionHLS,
			reason:    routeReasonQuery,
			pathClass: pathClassFromGate(gateReason),
			route:     true,
		}
	}

	accept := strings.ToLower(r.Header.Get("Accept"))
	if acceptWantsHLS(accept) {
		return routingDecision{
			decision:  routeDecisionHLS,
			reason:    routeReasonAccept,
			pathClass: pathClassFromGate(gateReason),
			route:     true,
		}
	}
	if acceptWantsTS(accept) {
		return routingDecision{
			decision:  routeDecisionTS,
			reason:    routeReasonAccept,
			pathClass: pathClassFromGate(gateReason),
			route:     false,
		}
	}

	dest := strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Dest")))
	if dest == "video" || dest == "audio" || dest == "media" {
		return routingDecision{
			decision:  routeDecisionHLS,
			reason:    routeReasonFetch,
			pathClass: pathClassFromGate(gateReason),
			route:     true,
		}
	}

	// Client Hints / Fetch Metadata are a strong browser signal (TV webviews often include them too).
	if strings.TrimSpace(r.Header.Get("Sec-CH-UA")) != "" {
		return routingDecision{
			decision:  routeDecisionHLS,
			reason:    routeReasonFetch,
			pathClass: pathClassFromGate(gateReason),
			route:     true,
		}
	}

	// Default to HLS for broad compatibility when the client is ambiguous.
	defaultReason := gateReason
	if defaultReason != routeReasonGateRef && defaultReason != routeReasonGateSlug {
		defaultReason = routeReasonDefault
	}
	return routingDecision{
		decision:  routeDecisionHLS,
		reason:    defaultReason,
		pathClass: pathClassFromGate(gateReason),
		route:     true,
	}
}

func gateStreamPath(path string, lookup func(string) bool) (string, bool) {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" || strings.Contains(trimmed, "/") {
		return routeReasonGateReject, false
	}
	if strings.Contains(trimmed, ":") {
		return routeReasonGateRef, true
	}
	if lookup != nil && lookup(trimmed) {
		return routeReasonGateSlug, true
	}
	return routeReasonGateReject, false
}

func acceptWantsHLS(accept string) bool {
	return strings.Contains(accept, "application/vnd.apple.mpegurl") ||
		strings.Contains(accept, "application/x-mpegurl") ||
		strings.Contains(accept, "application/mpegurl")
}

func acceptWantsTS(accept string) bool {
	return strings.Contains(accept, "video/mp2t") || strings.Contains(accept, "video/mpeg")
}

func pathClassFromGate(gateReason string) string {
	switch gateReason {
	case routeReasonGateRef:
		return "stream_ref"
	case routeReasonGateSlug:
		return "stream_slug"
	default:
		return "non_stream"
	}
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
			Str("target", sanitizeURL(targetURL)).
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
			Str("target", sanitizeURL(targetURL)).
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
