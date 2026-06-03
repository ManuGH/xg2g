// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

package gpu

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// GPU Resource Utilization
	GPUUtilization = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "xg2g",
			Subsystem: "gpu",
			Name:      "utilization_percent",
			Help:      "GPU utilization percentage (0-100)",
		},
		[]string{"device", "mode"}, // device: "renderD128", mode: "video|audio"
	)

	// VRAM Usage in Bytes
	GPUVRAMUsage = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "xg2g",
			Subsystem: "gpu",
			Name:      "vram_usage_bytes",
			Help:      "GPU VRAM usage in bytes",
		},
		[]string{"device"},
	)

	// Transcode Latency Histogram
	TranscodeLatency = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "xg2g",
			Subsystem: "gpu",
			Name:      "transcode_latency_seconds",
			Help:      "Time to transcode a frame (histogram)",
			Buckets:   prometheus.ExponentialBuckets(0.001, 2, 10), // 1ms to 512ms
		},
		[]string{"codec", "resolution", "device"},
	)

	// Active Streams by Mode
	ActiveStreamsByMode = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "xg2g",
			Name:      "active_streams_by_mode",
			Help:      "Number of active streams per deployment mode",
		},
		[]string{"mode"}, // "standard", "audio_proxy", "gpu"
	)

	// Transcode Error Rate
	TranscodeErrors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "xg2g",
			Subsystem: "gpu",
			Name:      "transcode_errors_total",
			Help:      "Total transcode errors",
		},
		[]string{"codec", "reason"}, // reason: "timeout|invalid_format|device_busy"
	)

	// Stream Detection Errors (from Tier 1 requirements)
	StreamDetectionErrors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "xg2g",
			Subsystem: "openwebif",
			Name:      "stream_detection_errors_total",
			Help:      "Stream detection failures by port and error type",
		},
		[]string{"port", "error_type"}, // port: "8001|17999", error_type: "timeout|no_response|invalid"
	)
)

// RecordTranscodeLatency records the latency of a transcode operation
func RecordTranscodeLatency(codec, resolution, device string, latency float64) {
	TranscodeLatency.WithLabelValues(codec, resolution, device).Observe(latency)
}

// UpdateGPUUtilization updates the current GPU utilization
func UpdateGPUUtilization(device, mode string, percent float64) {
	GPUUtilization.WithLabelValues(device, mode).Set(percent)
}

// UpdateVRAMUsage updates the current VRAM usage
func UpdateVRAMUsage(device string, bytes int64) {
	GPUVRAMUsage.WithLabelValues(device).Set(float64(bytes))
}

// IncActiveStreams increments active stream count for a mode
func IncActiveStreams(mode string) {
	ActiveStreamsByMode.WithLabelValues(mode).Inc()
}

// DecActiveStreams decrements active stream count for a mode
func DecActiveStreams(mode string) {
	ActiveStreamsByMode.WithLabelValues(mode).Dec()
}

// RecordTranscodeError records a transcode error
func RecordTranscodeError(codec, reason string) {
	TranscodeErrors.WithLabelValues(codec, reason).Inc()
}

// RecordStreamDetectionError records a stream detection error
func RecordStreamDetectionError(port, errorType string) {
	StreamDetectionErrors.WithLabelValues(port, errorType).Inc()
}

// --- Hardware encoder capability + runtime-demotion telemetry ---
//
// These make the otherwise process-local hardware-encode verdicts observable so
// fleet AV1/HEVC safety is queryable, and so a baseline exists before stricter
// admission checks change which encoders count as verified.

var (
	// EncoderVerified reports whether a hardware encoder passed startup preflight
	// (1) or not (0), per encoder (e.g. "av1_vaapi", "hevc_vaapi", "h264_vaapi").
	EncoderVerified = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "xg2g",
			Subsystem: "gpu",
			Name:      "encoder_verified",
			Help:      "Whether a hardware encoder passed startup preflight (1) or not (0)",
		},
		[]string{"encoder"},
	)

	// EncoderAutoEligible reports whether a verified hardware encoder is eligible
	// for automatic codec negotiation (1) or not (0), per encoder.
	EncoderAutoEligible = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "xg2g",
			Subsystem: "gpu",
			Name:      "encoder_auto_eligible",
			Help:      "Whether a verified hardware encoder is auto-eligible for negotiation (1) or not (0)",
		},
		[]string{"encoder"},
	)

	// EncoderUnverifiable reports whether an encoder's output could NOT be
	// validated (1) — no software decoder available — as distinct from withheld
	// (verified=0, unverifiable=0 = proven-bad output). Three states:
	// verified=1 → verified; verified=0,unverifiable=0 → withheld; unverifiable=1.
	EncoderUnverifiable = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "xg2g",
			Subsystem: "gpu",
			Name:      "encoder_unverifiable",
			Help:      "Whether a hardware encoder's output could not be verified for lack of a software decoder (1) or not (0)",
		},
		[]string{"encoder"},
	)

	// RuntimeDemotionTotal counts hardware-encoder demotions triggered by repeated
	// runtime encode failures, after which the backend falls back to CPU/software.
	RuntimeDemotionTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "xg2g",
			Subsystem: "gpu",
			Name:      "runtime_demotion_total",
			Help:      "Hardware encoder demotions caused by repeated runtime encode failures",
		},
		[]string{"backend"}, // "vaapi" | "nvenc"
	)

	// RuntimeEncodeFailureTotal counts individual runtime hardware-encode failures
	// observed in ffmpeg output (the pre-demotion signal).
	RuntimeEncodeFailureTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "xg2g",
			Subsystem: "gpu",
			Name:      "runtime_encode_failure_total",
			Help:      "Individual runtime hardware-encode failures observed in ffmpeg output",
		},
		[]string{"backend"},
	)
)

// SetEncoderVerified records whether a hardware encoder passed startup preflight.
func SetEncoderVerified(encoder string, verified bool) {
	EncoderVerified.WithLabelValues(encoder).Set(boolToGauge(verified))
}

// SetEncoderUnverifiable records whether a hardware encoder's output could not
// be validated (no software decoder), as distinct from proven-bad (withheld).
func SetEncoderUnverifiable(encoder string, unverifiable bool) {
	EncoderUnverifiable.WithLabelValues(encoder).Set(boolToGauge(unverifiable))
}

// SetEncoderAutoEligible records whether a hardware encoder is auto-eligible.
func SetEncoderAutoEligible(encoder string, eligible bool) {
	EncoderAutoEligible.WithLabelValues(encoder).Set(boolToGauge(eligible))
}

// RecordRuntimeDemotion records a hardware-encoder backend demotion (vaapi|nvenc).
func RecordRuntimeDemotion(backend string) {
	RuntimeDemotionTotal.WithLabelValues(backend).Inc()
}

// RecordRuntimeEncodeFailure records a single runtime hardware-encode failure.
func RecordRuntimeEncodeFailure(backend string) {
	RuntimeEncodeFailureTotal.WithLabelValues(backend).Inc()
}

func boolToGauge(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
