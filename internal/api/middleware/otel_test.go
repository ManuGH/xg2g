// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestShouldTrace(t *testing.T) {
	t.Parallel()

	skipPaths := []string{"/healthz", "/readyz", "/livez", "/metrics"}
	for _, p := range skipPaths {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		if shouldTrace(req) {
			t.Errorf("expected shouldTrace to skip %s", p)
		}
	}

	tracePaths := []string{"/api/v3/system/health", "/api/v3/intents", "/files/playlist.m3u"}
	for _, p := range tracePaths {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		if !shouldTrace(req) {
			t.Errorf("expected shouldTrace to trace %s", p)
		}
	}
}

func TestSpanNameFormatter(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/api/v3/system/health", nil)
	if got := spanNameFormatter("HTTP GET", req); got != "HTTP GET /api/v3/system/health" {
		t.Fatalf("unexpected span name: %s", got)
	}

	reqWithQuery := httptest.NewRequest(http.MethodGet, "/api/v3/system/health?token=secret", nil)
	if got := spanNameFormatter("HTTP GET", reqWithQuery); got != "HTTP GET /api/v3/system/health?" {
		t.Fatalf("unexpected span name with query: %s", got)
	}
}

func TestExtractAndAddSpanAttributes(t *testing.T) {
	// Enable robust testing by using a real SDK TraceProvider (not the global noop default)
	tp := sdktrace.NewTracerProvider()

	// Create a tracer from the provider
	tr := tp.Tracer("test-tracer")

	ctx, span := tr.Start(context.Background(), "test-span")
	defer span.End()

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil).WithContext(ctx)

	traceID, spanID := ExtractTraceContext(req)
	if traceID == "" || spanID == "" || traceID == "00000000000000000000000000000000" {
		t.Fatalf("expected valid trace/span ids, got %q %q", traceID, spanID)
	}

	// Ensure adding attributes does not panic and attaches to the current span
	AddSpanAttributes(req, attribute.String("test.key", "value"))
	if got := SpanFromContext(req).SpanContext().TraceID().String(); got != traceID {
		t.Fatalf("span context mismatch, expected trace id %s got %s", traceID, got)
	}
}
