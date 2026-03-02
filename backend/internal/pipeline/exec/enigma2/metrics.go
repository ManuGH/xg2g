// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package enigma2

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	requestTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "xg2g_v3_enigma2_request_total",
			Help: "Total number of Enigma2 HTTP request attempts",
		},
		[]string{"method", "endpoint", "status_class"},
	)
	requestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "xg2g_v3_enigma2_request_duration_seconds",
			Help:    "Duration of Enigma2 HTTP requests per attempt",
			Buckets: prometheus.ExponentialBuckets(0.05, 2.0, 8),
		},
		[]string{"method", "endpoint", "status_class"},
	)
	requestErrors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "xg2g_v3_enigma2_request_errors_total",
			Help: "Number of Enigma2 request attempts that failed",
		},
		[]string{"method", "endpoint", "status_class"},
	)
	requestRetries = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "xg2g_v3_enigma2_request_retries_total",
			Help: "Number of Enigma2 request retries performed",
		},
		[]string{"method", "endpoint", "status_class"},
	)
)

func statusClass(err error, status int) string {
	if err != nil {
		return "error"
	}
	switch {
	case status >= 500:
		return "5xx"
	case status >= 400:
		return "4xx"
	case status >= 300:
		return "3xx"
	case status >= 200:
		return "2xx"
	case status > 0:
		return "1xx"
	case status == 0:
		return "unknown"
	}
	return "unknown"
}

func recordAttemptMetrics(method, endpoint string, status int, duration time.Duration, err error, retry bool) {
	class := statusClass(err, status)
	requestTotal.WithLabelValues(method, endpoint, class).Inc()
	requestDuration.WithLabelValues(method, endpoint, class).Observe(duration.Seconds())
	if class != "2xx" {
		requestErrors.WithLabelValues(method, endpoint, class).Inc()
	}
	if retry {
		requestRetries.WithLabelValues(method, endpoint, class).Inc()
	}
}
