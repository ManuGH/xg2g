// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// SPDX-License-Identifier: MIT
package log

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestContextWithRequestID(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		requestID string
		want      string
	}{
		{
			name:      "nil context",
			ctx:       nil,
			requestID: "test-id-123",
			want:      "test-id-123",
		},
		{
			name:      "background context",
			ctx:       context.Background(),
			requestID: "req-456",
			want:      "req-456",
		},
		{
			name:      "empty request ID",
			ctx:       context.Background(),
			requestID: "",
			want:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := ContextWithRequestID(tt.ctx, tt.requestID)
			got := RequestIDFromContext(ctx)
			if got != tt.want {
				t.Errorf("RequestIDFromContext() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestContextWithJobID(t *testing.T) {
	tests := []struct {
		name  string
		ctx   context.Context
		jobID string
		want  string
	}{
		{
			name:  "nil context",
			ctx:   nil,
			jobID: "job-123",
			want:  "job-123",
		},
		{
			name:  "background context",
			ctx:   context.Background(),
			jobID: "job-456",
			want:  "job-456",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := ContextWithJobID(tt.ctx, tt.jobID)
			got := JobIDFromContext(ctx)
			if got != tt.want {
				t.Errorf("JobIDFromContext() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRequestIDFromContextEmpty(t *testing.T) {
	tests := []struct {
		name string
		ctx  context.Context
		want string
	}{
		{
			name: "nil context",
			ctx:  nil,
			want: "",
		},
		{
			name: "context without request ID",
			ctx:  context.Background(),
			want: "",
		},
		{
			name: "context with wrong type",
			ctx:  context.WithValue(context.Background(), requestIDKey, 123),
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RequestIDFromContext(tt.ctx)
			if got != tt.want {
				t.Errorf("RequestIDFromContext() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWithContext(t *testing.T) {
	// Test WithContext enriches logger with context fields
	baseLogger := WithComponent("test")

	// Context with request ID only
	ctx1 := ContextWithRequestID(context.Background(), "req-123")
	logger1 := WithContext(ctx1, baseLogger)

	// Should have request_id field (we can't easily test this without output capture)
	// This test mainly ensures no panics and proper function calls
	if logger1.GetLevel() != baseLogger.GetLevel() {
		t.Error("Logger level should be preserved")
	}

	// Context with both IDs
	ctx2 := ContextWithJobID(ctx1, "job-456")
	logger2 := WithContext(ctx2, baseLogger)

	if logger2.GetLevel() != baseLogger.GetLevel() {
		t.Error("Logger level should be preserved")
	}

	// Empty context should return original logger
	logger3 := WithContext(context.Background(), baseLogger)
	if logger3.GetLevel() != baseLogger.GetLevel() {
		t.Error("Logger level should be preserved")
	}
}

func TestWithComponentFromContext(t *testing.T) {
	logger := WithComponentFromContext(context.Background(), "test-component")
	// Verify it returns a logger (basic smoke test)
	if logger.GetLevel() > zerolog.PanicLevel {
		t.Error("Expected valid logger from WithComponentFromContext")
	}
}

func TestBase(t *testing.T) {
	baseLogger := Base()
	// Verify we get a valid logger instance (basic smoke test)
	if baseLogger.GetLevel() > zerolog.PanicLevel {
		t.Error("Expected valid base logger with reasonable log level")
	}
}

func TestDerive(t *testing.T) {
	// Test with nil builder function
	logger1 := Derive(nil)
	if logger1.GetLevel() > zerolog.PanicLevel {
		t.Error("Expected valid logger from Derive with nil builder")
	}

	// Test with custom builder function
	logger2 := Derive(func(ctx *zerolog.Context) {
		ctx.Str("custom_field", "test_value")
	})
	if logger2.GetLevel() > zerolog.PanicLevel {
		t.Error("Expected valid logger from Derive with custom builder")
	}
}

func TestWithTraceContext(t *testing.T) {
	// Test with no trace (should return logger without trace fields)
	ctx1 := context.Background()
	logger1 := WithTraceContext(ctx1)
	if logger1.GetLevel() > zerolog.PanicLevel {
		t.Error("Expected valid logger without trace")
	}

	// Test with noop tracer (invalid span context)
	noopTracer := noop.NewTracerProvider().Tracer("test")
	ctx2, span := noopTracer.Start(context.Background(), "test-span")
	defer span.End()

	logger2 := WithTraceContext(ctx2)
	if logger2.GetLevel() > zerolog.PanicLevel {
		t.Error("Expected valid logger with noop span")
	}

	// Test that trace_id and span_id are added when valid span exists
	// This is a smoke test - we can't easily verify the exact fields without output capture
	t.Run("WithValidSpan", func(t *testing.T) {
		// Create a real trace context (not noop)
		// In production, this would come from OpenTelemetry SDK
		// For testing, we verify the function doesn't panic
		traceID, _ := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
		spanID, _ := trace.SpanIDFromHex("00f067aa0ba902b7")
		traceFlags := trace.FlagsSampled
		spanCtx := trace.NewSpanContext(trace.SpanContextConfig{
			TraceID:    traceID,
			SpanID:     spanID,
			TraceFlags: traceFlags,
		})

		ctx := trace.ContextWithSpanContext(context.Background(), spanCtx)

		// Capture logger output
		var buf bytes.Buffer
		testLogger := zerolog.New(&buf)
		base = testLogger // Override global for this test

		logger := WithTraceContext(ctx)
		logger.Info().Msg("test with trace")

		// Parse JSON output
		var logEntry map[string]interface{}
		if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
			t.Fatalf("Failed to parse log output: %v", err)
		}

		// Verify trace_id and span_id are present
		if traceIDStr, ok := logEntry["trace_id"].(string); !ok || traceIDStr == "" {
			t.Error("Expected trace_id in log output")
		}
		if spanIDStr, ok := logEntry["span_id"].(string); !ok || spanIDStr == "" {
			t.Error("Expected span_id in log output")
		}

		// Restore global logger
		Configure(Config{})
	})
}
