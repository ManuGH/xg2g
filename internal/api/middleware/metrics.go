// SPDX-License-Identifier: MIT

package middleware

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
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

			// Track in-flight requests
			httpRequestsInFlight.Inc()
			defer httpRequestsInFlight.Dec()

			// Record request size
			if r.ContentLength > 0 {
				httpRequestSize.WithLabelValues(r.Method, r.URL.Path).Observe(float64(r.ContentLength))
			}

			// Wrap response writer to capture status and size
			mw := &metricsWriter{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
			}

			// Process request
			next.ServeHTTP(mw, r)

			// Calculate duration
			duration := time.Since(start).Seconds()

			// Extract route pattern for cleaner metrics (avoids cardinality explosion)
			path := r.URL.Path
			if routePattern := chi.RouteContext(r.Context()); routePattern != nil {
				if pattern := routePattern.RoutePattern(); pattern != "" {
					path = pattern
				}
			}

			// Record metrics
			status := strconv.Itoa(mw.statusCode)
			httpRequestDuration.WithLabelValues(r.Method, path, status).Observe(duration)

			if mw.bytesWritten > 0 {
				httpResponseSize.WithLabelValues(r.Method, path, status).Observe(float64(mw.bytesWritten))
			}
		})
	}
}

// metricsWriter wraps http.ResponseWriter to capture metrics.
type metricsWriter struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int
	written      bool
}

// WriteHeader captures the status code.
func (mw *metricsWriter) WriteHeader(statusCode int) {
	if !mw.written {
		mw.statusCode = statusCode
		mw.written = true
	}
	mw.ResponseWriter.WriteHeader(statusCode)
}

// Write captures the number of bytes written.
func (mw *metricsWriter) Write(b []byte) (int, error) {
	if !mw.written {
		mw.WriteHeader(http.StatusOK)
	}
	n, err := mw.ResponseWriter.Write(b)
	mw.bytesWritten += n
	return n, err
}
