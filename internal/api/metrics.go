// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	refreshDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "xg2g_refresh_duration_seconds",
		Help:    "Duration of refresh operations in seconds",
		Buckets: prometheus.ExponentialBuckets(0.1, 2.0, 10), // 0.1s .. ~51.2s
	})

	channelsFound = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "xg2g_channels",
		Help: "Number of channels found during the last refresh",
	})

	lastRefreshTime = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "xg2g_last_refresh_timestamp",
		Help: "Timestamp of the last successful refresh (Unix timestamp)",
	})

	fileRequestsDeniedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_file_requests_denied_total",
		Help: "Number of file requests denied for security reasons",
	}, []string{"reason"})

	fileRequestsAllowedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "xg2g_file_requests_allowed_total",
		Help: "Number of file requests allowed",
	})

	fileCacheHitsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "xg2g_file_cache_hits_total",
		Help: "Number of file requests served as 304 Not Modified",
	})

	fileCacheMissesTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "xg2g_file_cache_misses_total",
		Help: "Number of file requests resulting in 200 OK (content served)",
	})

	// V3 Metrics (Phase 2 Observability Gate)
	v3IntentsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_v3_intents_total", // Prefix aligned with xg2g_
		Help: "Total V3 intents processed by outcome and phase.",
	}, []string{"type", "mode", "outcome"})

	v3IdempotentReplayTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_v3_idempotent_replay_total",
		Help: "Total duplicate intents suppressed via idempotency.",
	}, []string{"type"})

	v3BusPublishTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_v3_bus_publish_total",
		Help: "Total bus events published by API.",
	}, []string{"topic", "outcome"})
)

func recordRefreshMetrics(duration time.Duration, channelCount int) {
	refreshDuration.Observe(duration.Seconds())
	channelsFound.Set(float64(channelCount))
	lastRefreshTime.Set(float64(time.Now().Unix()))
}

func recordFileRequestAllowed() {
	fileRequestsAllowedTotal.Inc()
}

func recordFileRequestDenied(reason string) {
	fileRequestsDeniedTotal.WithLabelValues(reason).Inc()
}

func recordFileCacheHit() {
	fileCacheHitsTotal.Inc()
}

func recordFileCacheMiss() {
	fileCacheMissesTotal.Inc()
}

// RecordV3Intent observes the outcome of an intent request.
func RecordV3Intent(intentType, mode, outcome string) {
	v3IntentsTotal.WithLabelValues(intentType, mode, outcome).Inc()
}

// RecordV3Replay counts suppressed idempotent replays.
func RecordV3Replay(intentType string) {
	v3IdempotentReplayTotal.WithLabelValues(intentType).Inc()
}

// RecordV3Publish counts bus publication attempts.
func RecordV3Publish(topic, outcome string) {
	v3BusPublishTotal.WithLabelValues(topic, outcome).Inc()
}
