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
