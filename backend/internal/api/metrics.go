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
)

func recordRefreshMetrics(duration time.Duration, channelCount int) {
	refreshDuration.Observe(duration.Seconds())
	channelsFound.Set(float64(channelCount))
	lastRefreshTime.Set(float64(time.Now().Unix()))
}
