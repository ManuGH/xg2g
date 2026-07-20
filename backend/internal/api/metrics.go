// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
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

	// P10: A1.1 Measure - track requests to legacy (non-v3) API routes.
	legacyAPIRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_legacy_api_requests_total",
		Help: "Total number of requests to legacy (non-v3) API endpoints",
	}, []string{"path", "client"})
)

func recordRefreshMetrics(duration time.Duration, channelCount int) {
	refreshDuration.Observe(duration.Seconds())
	channelsFound.Set(float64(channelCount))
	lastRefreshTime.Set(float64(time.Now().Unix()))
}

func recordLegacyAPIMetric(path, client string) {
	legacyAPIRequestsTotal.WithLabelValues(path, client).Inc()
}
