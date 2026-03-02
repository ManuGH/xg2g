// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

// Package middleware provides HTTP middleware for the API server.
package middleware

import (
	"net/http"

	"github.com/ManuGH/xg2g/internal/telemetry"
	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
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

			// Use route pattern if available (keeps span cardinality bounded).
			route := r.URL.Path
			if routeCtx := chi.RouteContext(r.Context()); routeCtx != nil {
				if pattern := routeCtx.RoutePattern(); pattern != "" {
					route = pattern
				}
			}

			// Never include query values in traces (tokens may be passed via query).
			urlLabel := r.URL.Path
			if r.URL.RawQuery != "" {
				urlLabel += "?"
			}

			// Start a new span for this request
			ctx, span := tracer.Start(ctx, r.Method+" "+route,
				trace.WithSpanKind(trace.SpanKindServer),
			)
			defer span.End()

			// Capture status code while preserving streaming interfaces.
			ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)

			// Add HTTP attributes to span
			attrs := telemetry.HTTPAttributes(
				r.Method,
				route,
				urlLabel,
				0, // Will be set after response
			)
			if reqID := ww.Header().Get("X-Request-ID"); reqID != "" {
				attrs = append(attrs, attribute.String("http.requestId", reqID))
			}
			span.SetAttributes(attrs...)

			// Process request
			next.ServeHTTP(ww, r.WithContext(ctx))

			// Update span with response status
			statusCode := ww.Status()
			finalAttrs := telemetry.HTTPAttributes(
				r.Method,
				route,
				urlLabel,
				statusCode,
			)
			if reqID := ww.Header().Get("X-Request-ID"); reqID != "" {
				finalAttrs = append(finalAttrs, attribute.String("http.requestId", reqID))
			}
			span.SetAttributes(finalAttrs...)

			// Mark span as error if status code >= 500
			if statusCode >= 500 {
				span.SetStatus(codes.Error, http.StatusText(statusCode))
			} else {
				// Treat 4xx as client-side issues to avoid noisy error signal
				span.SetStatus(codes.Ok, "")
			}
		})
	}
}
