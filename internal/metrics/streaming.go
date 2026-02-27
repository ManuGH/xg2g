// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package metrics

import (
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// ZapDelayDuration tracks the dynamic delay duration applied after a Zap.
	ZapDelayDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "xg2g_hls_zap_delay_seconds",
		Help:    "Dynamic delay wait time after channel zap",
		Buckets: []float64{0.1, 0.5, 1, 2, 5},
	})

	// StreamStartAttempts tracks the outcome of stream start attempts by profile.
	StreamStartAttempts = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_hls_start_attempt_total",
		Help: "Total number of HLS start attempts by profile and outcome",
	}, []string{"profile", "outcome"})
)

var (
	// StreamTuneDuration tracks the time taken for the receiver to tune
	// (Zap request + post-zap delay).
	StreamTuneDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "xg2g_stream_tune_duration_seconds",
		Help:    "Time taken for receiver tuning (Zap + delay)",
		Buckets: []float64{0.1, 0.25, 0.5, 1, 2, 3, 5, 8, 13, 20},
	}, []string{"encrypted"})

	// StreamFirstSegmentLatency tracks the time until the first media segment is written to disk.
	// This helps distinguish between "Playlist ready" and "Player ready".
	StreamFirstSegmentLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "xg2g_stream_first_segment_latency_seconds",
		Help:    "Time from request to first segment availability",
		Buckets: []float64{1, 2, 3, 5, 8, 13, 20, 30},
	}, []string{"encrypted"})

	// StreamFirstSegmentServeLatency tracks the time until the first media segment is served to the client via HTTP.
	// This captures the full end-to-end latency including network round-trips.
	StreamFirstSegmentServeLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "xg2g_stream_first_segment_serve_latency_seconds",
		Help:    "Time from request to first segment served to client",
		Buckets: []float64{1, 2, 3, 5, 8, 13, 20, 30, 45, 60},
	}, []string{"encrypted"})

	// StreamStartTotal tracks the outcome of stream start attempts.
	StreamStartTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_stream_start_total",
		Help: "Total number of stream start attempts by result and reason",
	}, []string{"result", "reason", "encrypted"})

	StreamRoutingTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_proxy_stream_routing_total",
		Help: "Routing decisions for stream-like requests (auto HLS vs TS vs proxy)",
	}, []string{"decision", "reason"})

	LiveIntentsPlaybackKeyTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_live_intents_playback_key_total",
		Help: "Usage and validation outcomes for live playback decision keys in /api/v3/intents",
	}, []string{"key", "result"})

	// --- Phase 4 Metrics ---

	// Enigma2ReadyDuration tracks time spent waiting for readiness check
	Enigma2ReadyDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "xg2g_enigma2_ready_duration_seconds",
		Help:    "Time waiting for Enigma2 to lock and stabilize",
		Buckets: []float64{0.25, 0.5, 1, 1.5, 2, 3, 5, 8},
	})

	// Enigma2ReadyOutcome tracks readiness check results
	Enigma2ReadyOutcome = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_enigma2_ready_outcome_total",
		Help: "Enigma2 readiness check outcomes (ready, timeout, cancelled, error)",
	}, []string{"outcome"})

	// FFmpegExitTotal tracks ffmpeg process exit codes
	FFmpegExitTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_ffmpeg_exit_total",
		Help: "Total number of ffmpeg process exits by code and reason",
	}, []string{"profile", "code", "reason"})

	// Enigma2InvariantViolation tracks critical invariant checks failure
	Enigma2InvariantViolationTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "xg2g_enigma2_invariant_violation_total",
		Help: "Critical invariant violations (expected_ref != actual_ref at stream start)",
	})

	// PreflightLatency tracks the duration of the TS preflight check.
	PreflightLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "xg2g_preflight_latency_seconds",
		Help:    "Duration of TS preflight check",
		Buckets: []float64{0.1, 0.25, 0.5, 0.75, 1, 1.5, 2, 5},
	}, []string{"port_type"})
)

// ObservePreflightLatency records the preflight latency.
func ObservePreflightLatency(port int, duration time.Duration) {
	portType := "direct"
	if port == 17999 {
		portType = "relay"
	}
	PreflightLatency.WithLabelValues(portType).Observe(duration.Seconds())
}

// ObserveStreamTuneDuration records the tune duration.
func ObserveStreamTuneDuration(encrypted bool, duration time.Duration) {
	StreamTuneDuration.WithLabelValues(strconv.FormatBool(encrypted)).Observe(duration.Seconds())
}

// ObserveStreamFirstSegmentLatency records the first segment latency.
func ObserveStreamFirstSegmentLatency(encrypted bool, duration time.Duration) {
	StreamFirstSegmentLatency.WithLabelValues(strconv.FormatBool(encrypted)).Observe(duration.Seconds())
}

// ObserveStreamFirstSegmentServeLatency records the first segment serve latency.
func ObserveStreamFirstSegmentServeLatency(encrypted bool, duration time.Duration) {
	StreamFirstSegmentServeLatency.WithLabelValues(strconv.FormatBool(encrypted)).Observe(duration.Seconds())
}

// IncStreamStart records a stream start attempt outcome.
func IncStreamStart(encrypted bool, success bool, reason string) {
	result := "failure"
	if success {
		result = "success"
	}
	StreamStartTotal.WithLabelValues(result, reason, strconv.FormatBool(encrypted)).Inc()
}

func IncStreamRouting(decision, reason string) {
	StreamRoutingTotal.WithLabelValues(decision, reason).Inc()
}

func IncLiveIntentsPlaybackKey(key, result string) {
	LiveIntentsPlaybackKeyTotal.WithLabelValues(
		normalizeLiveIntentPlaybackKeyLabel(key),
		normalizeLiveIntentPlaybackResultLabel(result),
	).Inc()
}

// ObserveEnigma2Ready records readiness check duration.
func ObserveEnigma2Ready(duration time.Duration) {
	Enigma2ReadyDuration.Observe(duration.Seconds())
}

// IncEnigma2ReadyOutcome records readiness check result.
func IncEnigma2ReadyOutcome(outcome string) {
	Enigma2ReadyOutcome.WithLabelValues(outcome).Inc()
}

// IncEnigma2InvariantViolation increments the invariant violation counter
func IncEnigma2InvariantViolation() {
	Enigma2InvariantViolationTotal.Inc()
}

// IncFFmpegExit increments the ffmpeg exit counter
func IncFFmpegExit(profile, code, reason string) {
	FFmpegExitTotal.WithLabelValues(profile, code, reason).Inc()
}

func normalizeLiveIntentPlaybackKeyLabel(key string) string {
	switch key {
	case "playback_decision_token", "playback_decision_id", "both", "none":
		return key
	default:
		return "unknown"
	}
}

func normalizeLiveIntentPlaybackResultLabel(result string) string {
	switch result {
	case "accepted", "equal", "mismatch", "rejected_missing", "rejected_invalid":
		return result
	default:
		return "unknown"
	}
}
