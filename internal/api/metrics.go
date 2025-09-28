package api

import (
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

//nolint:unused
var (
	// Metrics for HTTP requests
	httpRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_http_requests_total",
		Help: "Number of HTTP requests, by path and status",
	}, []string{"path", "status"})

	// Metrics für Refresh-Jobs
	refreshDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "xg2g_refresh_duration_seconds",
		Help:    "Duration of refresh operations in seconds",
		Buckets: prometheus.ExponentialBuckets(0.1, 2.0, 10), // 0.1s bis ~51.2s
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

// recordMetrics records metrics for an HTTP request
//
//nolint:unused
func recordHTTPMetric(path string, status int) {
	httpRequestsTotal.WithLabelValues(path, strconv.Itoa(status)).Inc()
}

// recordRefreshMetrics records metrics for a refresh job
//
//nolint:unused
func recordRefreshMetrics(duration time.Duration, channelCount int) {
	refreshDuration.Observe(duration.Seconds())
	channelsFound.Set(float64(channelCount))
	lastRefreshTime.Set(float64(time.Now().Unix()))
}
