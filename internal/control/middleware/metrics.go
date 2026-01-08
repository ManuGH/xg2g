// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

package middleware

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// HTTP request metrics
	httpRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "xg2g_http_request_duration_seconds",
		Help:    "HTTP request latencies in seconds",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "path", "status"})

	httpRequestsInFlight = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "xg2g_http_requests_in_flight",
		Help: "Current number of HTTP requests being served",
	})

	httpRequestSize = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "xg2g_http_request_size_bytes",
		Help:    "HTTP request sizes in bytes",
		Buckets: prometheus.ExponentialBuckets(100, 10, 8),
	}, []string{"method", "path"})

	httpResponseSize = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "xg2g_http_response_size_bytes",
		Help:    "HTTP response sizes in bytes",
		Buckets: prometheus.ExponentialBuckets(100, 10, 8),
	}, []string{"method", "path", "status"})
)

// Metrics creates a middleware that records Prometheus metrics for HTTP requests.
// It tracks request duration, in-flight requests, request/response sizes, and status codes.
func Metrics() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			contentLength := r.ContentLength

			// Track in-flight requests
			httpRequestsInFlight.Inc()
			defer httpRequestsInFlight.Dec()

			// Wrap response writer to capture status and size while preserving streaming interfaces
			ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)

			// Process request
			next.ServeHTTP(ww, r)

			// Calculate duration
			duration := time.Since(start).Seconds()

			// Extract route pattern for cleaner metrics (avoids cardinality explosion)
			path := r.URL.Path
			if routePattern := chi.RouteContext(r.Context()); routePattern != nil {
				if pattern := routePattern.RoutePattern(); pattern != "" {
					path = pattern
				}
			}

			// Record request size (label by route pattern to avoid cardinality explosion)
			if contentLength > 0 {
				httpRequestSize.WithLabelValues(r.Method, path).Observe(float64(contentLength))
			}

			// Record metrics
			status := strconv.Itoa(ww.Status())
			httpRequestDuration.WithLabelValues(r.Method, path, status).Observe(duration)

			if written := ww.BytesWritten(); written > 0 {
				httpResponseSize.WithLabelValues(r.Method, path, status).Observe(float64(written))
			}
		})
	}
}
