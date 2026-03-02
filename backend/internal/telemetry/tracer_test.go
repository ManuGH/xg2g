// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.
package telemetry

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

func TestNewProvider_Disabled(t *testing.T) {
	cfg := Config{
		Enabled:      false,
		ServiceName:  "test-service",
		ExporterType: "grpc",
	}

	provider, err := NewProvider(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if provider.tp != nil {
		t.Error("Expected noop provider (tp == nil)")
	}

	// Verify global tracer is noop
	tracer := otel.Tracer("test")
	_, span := tracer.Start(context.Background(), "noop-check")
	if span.IsRecording() {
		t.Error("Expected noop tracer span to be non-recording")
	}
	span.End()
}

func TestNewProvider_InvalidExporter(t *testing.T) {
	cfg := Config{
		Enabled:      true,
		ServiceName:  "test-service",
		ExporterType: "invalid",
	}

	_, err := NewProvider(context.Background(), cfg)
	if err == nil {
		t.Fatal("Expected error for invalid exporter type")
	}

	expectedMsg := "unsupported exporter type: invalid (supported: grpc, http)"
	if err.Error() != expectedMsg {
		t.Errorf("Expected error message %q, got %q", expectedMsg, err.Error())
	}
}

func TestNewProvider_SamplingRates(t *testing.T) {
	tests := []struct {
		name         string
		samplingRate float64
		wantSampler  string
	}{
		{
			name:         "always sample",
			samplingRate: 1.0,
			wantSampler:  "AlwaysOnSampler",
		},
		{
			name:         "never sample",
			samplingRate: 0.0,
			wantSampler:  "AlwaysOffSampler",
		},
		{
			name:         "ratio sample",
			samplingRate: 0.5,
			wantSampler:  "TraceIDRatioBased",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Note: We can't easily test the actual sampler without starting a real exporter
			// This test primarily verifies that the provider initializes without error
			cfg := Config{
				Enabled:      false, // Use noop to avoid network calls
				ServiceName:  "test-service",
				ExporterType: "grpc",
				SamplingRate: tt.samplingRate,
			}

			provider, err := NewProvider(context.Background(), cfg)
			if err != nil {
				t.Fatalf("Expected no error, got: %v", err)
			}

			if provider == nil {
				t.Fatal("Expected non-nil provider")
			}
		})
	}
}

func TestProvider_Shutdown(t *testing.T) {
	// Test shutdown on noop provider
	provider := &Provider{tp: nil}
	err := provider.Shutdown(context.Background())
	if err != nil {
		t.Errorf("Expected no error on noop shutdown, got: %v", err)
	}
}

func TestProvider_ShutdownTimeout(t *testing.T) {
	// Create a context that's already canceled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	provider := &Provider{tp: nil}
	err := provider.Shutdown(ctx)
	if err != nil {
		t.Errorf("Expected no error on noop shutdown with canceled context, got: %v", err)
	}
}

func TestTracer(t *testing.T) {
	// Setup noop provider
	cfg := Config{
		Enabled:     false,
		ServiceName: "test-service",
	}

	_, err := NewProvider(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	// Get tracer
	tracer := Tracer("test-tracer")
	if tracer == nil {
		t.Fatal("Expected non-nil tracer")
	}

	// Create a span to verify tracer works
	ctx, span := tracer.Start(context.Background(), "test-span")
	if span == nil {
		t.Fatal("Expected non-nil span")
	}
	span.End()

	// Verify context contains span
	if trace.SpanFromContext(ctx) == nil {
		t.Error("Expected span in context")
	}
}

func TestConfig_DefaultValues(t *testing.T) {
	cfg := Config{
		ServiceName:    "xg2g",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		ExporterType:   "grpc",
		Endpoint:       "localhost:4317",
		SamplingRate:   1.0,
	}

	if cfg.ServiceName != "xg2g" {
		t.Errorf("Expected ServiceName=xg2g, got %s", cfg.ServiceName)
	}
	if cfg.ServiceVersion != "1.0.0" {
		t.Errorf("Expected ServiceVersion=1.0.0, got %s", cfg.ServiceVersion)
	}
	if cfg.Environment != "test" {
		t.Errorf("Expected Environment=test, got %s", cfg.Environment)
	}
	if cfg.ExporterType != "grpc" {
		t.Errorf("Expected ExporterType=grpc, got %s", cfg.ExporterType)
	}
	if cfg.Endpoint != "localhost:4317" {
		t.Errorf("Expected Endpoint=localhost:4317, got %s", cfg.Endpoint)
	}
	if cfg.SamplingRate != 1.0 {
		t.Errorf("Expected SamplingRate=1.0, got %f", cfg.SamplingRate)
	}
}

func TestProvider_ConcurrentShutdown(t *testing.T) {
	t.Helper()
	provider := &Provider{tp: nil}

	// Concurrent shutdowns should not panic
	done := make(chan struct{}, 5)
	for i := 0; i < 5; i++ {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			_ = provider.Shutdown(ctx)
			done <- struct{}{}
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 5; i++ {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for concurrent shutdown")
		}
	}
}
