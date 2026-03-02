// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

package middleware

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"
)

// OTelHTTP wraps the handler with OpenTelemetry HTTP instrumentation.
// This automatically creates spans for all HTTP requests and propagates trace context.
func OTelHTTP(serviceName string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return otelhttp.NewHandler(
			next,
			serviceName,
			otelhttp.WithTracerProvider(otel.GetTracerProvider()),
			otelhttp.WithSpanOptions(
				trace.WithAttributes(
					semconv.ServiceName(serviceName),
				),
			),
			otelhttp.WithMessageEvents(otelhttp.ReadEvents, otelhttp.WriteEvents),
			otelhttp.WithFilter(shouldTrace),
			otelhttp.WithSpanNameFormatter(spanNameFormatter),
		)
	}
}

// shouldTrace determines if a request should be traced.
// Skip health checks and metrics endpoints to reduce noise.
func shouldTrace(r *http.Request) bool {
	path := r.URL.Path

	// Skip health/metrics endpoints
	switch path {
	case "/healthz", "/readyz", "/livez", "/metrics":
		return false
	}

	return true
}

// spanNameFormatter creates meaningful span names from HTTP requests.
// Format: "HTTP {METHOD} {ROUTE}" (e.g., "HTTP GET /api/status")
func spanNameFormatter(operation string, r *http.Request) string {
	// Extract route pattern if available (chi router sets this)
	route := r.URL.Path

	// Remove query parameters for cleaner span names
	if r.URL.RawQuery != "" {
		return operation + " " + route + "?" // Indicate query params without exposing values
	}

	return operation + " " + route
}

// ExtractTraceContext extracts trace_id and span_id from the request context.
// Returns empty strings if no active span exists.
func ExtractTraceContext(r *http.Request) (traceID, spanID string) {
	spanCtx := trace.SpanContextFromContext(r.Context())
	if !spanCtx.IsValid() {
		return "", ""
	}

	return spanCtx.TraceID().String(), spanCtx.SpanID().String()
}

// AddSpanAttributes adds custom attributes to the current span.
// Safe to call even if tracing is disabled (noop).
func AddSpanAttributes(r *http.Request, attrs ...attribute.KeyValue) {
	span := trace.SpanFromContext(r.Context())
	span.SetAttributes(attrs...)
}

// SpanFromContext returns the current span from the request context.
// Returns a noop span if tracing is disabled.
func SpanFromContext(r *http.Request) trace.Span {
	return trace.SpanFromContext(r.Context())
}
