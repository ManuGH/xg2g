// SPDX-License-Identifier: MIT

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
