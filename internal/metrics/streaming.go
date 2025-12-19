package metrics

import (
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// StreamTuneDuration tracks the time taken for the receiver to tune
	// (Zap request + post-zap delay).
	StreamTuneDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "xg2g_stream_tune_duration_seconds",
		Help:    "Time taken for receiver tuning (Zap + delay)",
		Buckets: []float64{0.1, 0.25, 0.5, 1, 2, 3, 5, 8, 13, 20},
	}, []string{"encrypted"})

	// StreamStartupLatency tracks the total time from start request to valid playlist.
	StreamStartupLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "xg2g_stream_startup_latency_seconds",
		Help:    "Total time from request to playlist availability",
		Buckets: []float64{1, 2, 3, 5, 8, 13, 20, 30},
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
)

// ObserveStreamTuneDuration records the tune duration.
func ObserveStreamTuneDuration(encrypted bool, duration time.Duration) {
	StreamTuneDuration.WithLabelValues(strconv.FormatBool(encrypted)).Observe(duration.Seconds())
}

// ObserveStreamStartupLatency records the startup latency.
func ObserveStreamStartupLatency(encrypted bool, duration time.Duration) {
	StreamStartupLatency.WithLabelValues(strconv.FormatBool(encrypted)).Observe(duration.Seconds())
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
