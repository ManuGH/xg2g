// SPDX-License-Identifier: MIT
package middleware

import (
	"context"
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
	req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
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
	req := httptest.NewRequest(http.MethodGet, "/api/v1/error", nil)
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
	req := httptest.NewRequest(http.MethodGet, "/api/v1/notfound", nil)
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
	handler := http.HandlerFunc(func(w http.ResponseWriter, _  *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Wrap with tracing middleware
	tracedHandler := Tracing("test-tracer")(handler)

	// Create test request with User-Agent
	req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
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
		req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
		rec := httptest.NewRecorder()
		tracedHandler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Request %d: Expected status 200, got %d", i, rec.Code)
		}
	}
}

func TestResponseWriter_WriteHeader(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := &responseWriter{
		ResponseWriter: rec,
		statusCode:     http.StatusOK,
	}

	// First call should set status
	rw.WriteHeader(http.StatusCreated)
	if rw.statusCode != http.StatusCreated {
		t.Errorf("Expected status 201, got %d", rw.statusCode)
	}

	// Second call should be ignored
	rw.WriteHeader(http.StatusBadRequest)
	if rw.statusCode != http.StatusCreated {
		t.Errorf("Expected status to remain 201, got %d", rw.statusCode)
	}
}

func TestResponseWriter_Write(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := &responseWriter{
		ResponseWriter: rec,
		statusCode:     http.StatusOK,
	}

	// Write should implicitly call WriteHeader if not already called
	n, err := rw.Write([]byte("test"))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	if n != 4 {
		t.Errorf("Expected 4 bytes written, got %d", n)
	}

	if !rw.written {
		t.Error("Expected written flag to be true")
	}

	if rw.statusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rw.statusCode)
	}
}

func TestResponseWriter_WriteWithExplicitHeader(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := &responseWriter{
		ResponseWriter: rec,
		statusCode:     http.StatusOK,
	}

	// Explicitly set header first
	rw.WriteHeader(http.StatusAccepted)

	// Then write
	n, err := rw.Write([]byte("test"))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	if n != 4 {
		t.Errorf("Expected 4 bytes written, got %d", n)
	}

	if rw.statusCode != http.StatusAccepted {
		t.Errorf("Expected status 202, got %d", rw.statusCode)
	}
}
