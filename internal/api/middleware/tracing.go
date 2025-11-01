// SPDX-License-Identifier: MIT

// Package middleware provides HTTP middleware for the API server.
package middleware

import (
	"net/http"

	"github.com/ManuGH/xg2g/internal/telemetry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// Tracing creates a middleware that adds OpenTelemetry tracing to HTTP requests.
func Tracing(tracerName string) func(http.Handler) http.Handler {
	tracer := telemetry.Tracer(tracerName)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract trace context from incoming request headers (W3C Trace Context)
			// This enables distributed tracing across service boundaries
			ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))

			// Start a new span for this request
			ctx, span := tracer.Start(ctx, r.Method+" "+r.URL.Path,
				trace.WithSpanKind(trace.SpanKindServer),
			)
			defer span.End()

			// Create a response writer wrapper to capture status code
			rw := &responseWriter{
				ResponseWriter: w,
				statusCode:     http.StatusOK, // Default to 200
			}

			// Add HTTP attributes to span
			span.SetAttributes(telemetry.HTTPAttributes(
				r.Method,
				r.URL.Path,
				r.URL.String(),
				0, // Will be set after response
			)...)

			// Process request
			next.ServeHTTP(rw, r.WithContext(ctx))

			// Update span with response status
			span.SetAttributes(telemetry.HTTPAttributes(
				r.Method,
				r.URL.Path,
				r.URL.String(),
				rw.statusCode,
			)...)

			// Mark span as error if status code >= 500
			if rw.statusCode >= 500 {
				span.SetStatus(codes.Error, http.StatusText(rw.statusCode))
			} else {
				// Treat 4xx as client-side issues to avoid noisy error signal
				span.SetStatus(codes.Ok, "")
			}
		})
	}
}

// responseWriter wraps http.ResponseWriter to capture the status code.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
	written    bool
}

// WriteHeader captures the status code.
func (rw *responseWriter) WriteHeader(statusCode int) {
	if !rw.written {
		rw.statusCode = statusCode
		rw.written = true
	}
	rw.ResponseWriter.WriteHeader(statusCode)
}

// Write ensures WriteHeader is called with default status if not already written.
func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.written {
		rw.WriteHeader(http.StatusOK)
	}
	return rw.ResponseWriter.Write(b)
}
