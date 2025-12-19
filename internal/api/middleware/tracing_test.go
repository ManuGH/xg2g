// SPDX-License-Identifier: MIT
package middleware

import (
	"bufio"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ManuGH/xg2g/internal/telemetry"
	"go.opentelemetry.io/otel/trace"
)

func TestTracing_Success(t *testing.T) {
	// Setup noop tracer provider for testing
	_, err := telemetry.NewProvider(context.Background(), telemetry.Config{
		Enabled:     false,
		ServiceName: "test",
	})
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	// Create test handler
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify span exists in context (noop spans are not valid, but are present)
		span := trace.SpanFromContext(r.Context())
		if span == nil {
			t.Error("Expected span in context")
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	// Wrap with tracing middleware
	tracedHandler := Tracing("test-tracer")(handler)

	// Create test request
	req := httptest.NewRequest(http.MethodGet, "/api/v2/status", nil)
	rec := httptest.NewRecorder()

	// Execute request
	tracedHandler.ServeHTTP(rec, req)

	// Verify response
	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}

	if rec.Body.String() != "OK" {
		t.Errorf("Expected body 'OK', got %s", rec.Body.String())
	}
}

func TestTracing_Error(t *testing.T) {
	// Setup noop tracer provider
	_, err := telemetry.NewProvider(context.Background(), telemetry.Config{
		Enabled:     false,
		ServiceName: "test",
	})
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	// Create test handler that returns error
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("Internal Server Error"))
	})

	// Wrap with tracing middleware
	tracedHandler := Tracing("test-tracer")(handler)

	// Create test request
	req := httptest.NewRequest(http.MethodGet, "/api/v2/error", nil)
	rec := httptest.NewRecorder()

	// Execute request
	tracedHandler.ServeHTTP(rec, req)

	// Verify response
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d", rec.Code)
	}
}

func TestTracing_ClientError(t *testing.T) {
	// Setup noop tracer provider
	_, err := telemetry.NewProvider(context.Background(), telemetry.Config{
		Enabled:     false,
		ServiceName: "test",
	})
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	// Create test handler that returns 404
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("Not Found"))
	})

	// Wrap with tracing middleware
	tracedHandler := Tracing("test-tracer")(handler)

	// Create test request
	req := httptest.NewRequest(http.MethodGet, "/api/v2/notfound", nil)
	rec := httptest.NewRecorder()

	// Execute request
	tracedHandler.ServeHTTP(rec, req)

	// Verify response
	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", rec.Code)
	}
}

func TestTracing_WithUserAgent(t *testing.T) {
	// Setup noop tracer provider
	_, err := telemetry.NewProvider(context.Background(), telemetry.Config{
		Enabled:     false,
		ServiceName: "test",
	})
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	// Create test handler
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Wrap with tracing middleware
	tracedHandler := Tracing("test-tracer")(handler)

	// Create test request with User-Agent
	req := httptest.NewRequest(http.MethodGet, "/api/v2/status", nil)
	req.Header.Set("User-Agent", "TestClient/1.0")
	rec := httptest.NewRecorder()

	// Execute request
	tracedHandler.ServeHTTP(rec, req)

	// Verify response
	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}

func TestTracing_MultipleRequests(t *testing.T) {
	// Setup noop tracer provider
	_, err := telemetry.NewProvider(context.Background(), telemetry.Config{
		Enabled:     false,
		ServiceName: "test",
	})
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	// Create test handler
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Wrap with tracing middleware
	tracedHandler := Tracing("test-tracer")(handler)

	// Execute multiple requests
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v2/status", nil)
		rec := httptest.NewRecorder()
		tracedHandler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Request %d: Expected status 200, got %d", i, rec.Code)
		}
	}
}

type testResponseWriter struct {
	*httptest.ResponseRecorder
}

func (t testResponseWriter) Flush() {}

func (t testResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, errors.New("not implemented")
}

func TestTracing_PreservesResponseWriterInterfaces(t *testing.T) {
	_, err := telemetry.NewProvider(context.Background(), telemetry.Config{
		Enabled:     false,
		ServiceName: "test",
	})
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, ok := w.(http.Flusher); !ok {
			t.Error("expected ResponseWriter to implement http.Flusher")
		}
		if _, ok := w.(http.Hijacker); !ok {
			t.Error("expected ResponseWriter to implement http.Hijacker")
		}
		w.WriteHeader(http.StatusOK)
	})

	tracedHandler := Tracing("test-tracer")(handler)
	req := httptest.NewRequest(http.MethodGet, "/api/v2/status", nil)
	rec := testResponseWriter{ResponseRecorder: httptest.NewRecorder()}
	tracedHandler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}
