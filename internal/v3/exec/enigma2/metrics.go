// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package enigma2

import (
	"context"
	"errors"
	"net"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	requestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "xg2g_v3_enigma2_request_duration_seconds",
			Help:    "Duration of Enigma2 HTTP requests per attempt",
			Buckets: prometheus.ExponentialBuckets(0.05, 2.0, 8),
		},
		[]string{"operation", "status", "attempt"},
	)
	requestRetries = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "xg2g_v3_enigma2_request_retries_total",
			Help: "Number of Enigma2 request retries performed",
		},
		[]string{"operation"},
	)
	requestFailures = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "xg2g_v3_enigma2_request_failures_total",
			Help: "Number of failed Enigma2 requests by error class",
		},
		[]string{"operation", "error_class"},
	)
	requestSuccess = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "xg2g_v3_enigma2_request_success_total",
			Help: "Number of successful Enigma2 requests",
		},
		[]string{"operation"},
	)
)

func classifyError(err error, status int) string {
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return "timeout"
		}
		var netErr net.Error
		if errors.As(err, &netErr) {
			if netErr.Timeout() {
				return "timeout"
			}
			return "network"
		}
		return "error"
	}
	if status >= 500 {
		return "http_5xx"
	}
	if status >= 400 {
		return "http_4xx"
	}
	if status == 0 {
		return "unknown"
	}
	return "ok"
}

func recordAttemptMetrics(operation string, attempt, status int, duration time.Duration, success bool, errClass string, retry bool) {
	statusLabel := "0"
	if status > 0 {
		statusLabel = strconv.Itoa(status)
	}
	requestDuration.WithLabelValues(operation, statusLabel, strconv.Itoa(attempt)).Observe(duration.Seconds())
	if success {
		requestSuccess.WithLabelValues(operation).Inc()
		return
	}
	requestFailures.WithLabelValues(operation, errClass).Inc()
	if retry {
		requestRetries.WithLabelValues(operation).Inc()
	}
}
