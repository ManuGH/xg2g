// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

// Package telemetry provides OpenTelemetry tracing utilities for the xg2g application.
package telemetry

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// Config holds telemetry configuration.
type Config struct {
	// Enabled determines if telemetry is active
	Enabled bool

	// ServiceName is the name of the service (e.g., "xg2g")
	ServiceName string

	// ServiceVersion is the version of the service
	ServiceVersion string

	// Environment is the deployment environment (e.g., "production", "development")
	Environment string

	// ExporterType defines the exporter to use: "grpc", "http", or "noop"
	ExporterType string

	// Endpoint is the OTLP collector endpoint (e.g., "localhost:4317" for gRPC, "localhost:4318" for HTTP)
	Endpoint string

	// SamplingRate is the trace sampling rate (0.0 to 1.0, where 1.0 = 100%)
	SamplingRate float64
}

// Provider manages the OpenTelemetry tracer provider.
type Provider struct {
	tp *sdktrace.TracerProvider
}

// NewProvider creates and initializes a new OpenTelemetry tracer provider.
func NewProvider(ctx context.Context, cfg Config) (*Provider, error) {
	if !cfg.Enabled {
		// Return a noop provider when telemetry is disabled
		otel.SetTracerProvider(noop.NewTracerProvider())
		return &Provider{tp: nil}, nil
	}

	// Create resource with service information
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(cfg.ServiceName),
			semconv.ServiceVersionKey.String(cfg.ServiceVersion),
			semconv.DeploymentEnvironmentKey.String(cfg.Environment),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Create exporter based on configuration
	var exporter sdktrace.SpanExporter
	switch cfg.ExporterType {
	case "grpc":
		exporter, err = otlptracegrpc.New(ctx,
			otlptracegrpc.WithEndpoint(cfg.Endpoint),
			otlptracegrpc.WithInsecure(), // Use TLS in production
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create gRPC exporter: %w", err)
		}

	case "http":
		exporter, err = otlptracehttp.New(ctx,
			otlptracehttp.WithEndpoint(cfg.Endpoint),
			otlptracehttp.WithInsecure(), // Use TLS in production
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create HTTP exporter: %w", err)
		}

	default:
		return nil, fmt.Errorf("unsupported exporter type: %s (supported: grpc, http)", cfg.ExporterType)
	}

	// Create sampler based on sampling rate
	var sampler sdktrace.Sampler
	switch {
	case cfg.SamplingRate >= 1.0:
		sampler = sdktrace.AlwaysSample()
	case cfg.SamplingRate <= 0.0:
		sampler = sdktrace.NeverSample()
	default:
		sampler = sdktrace.TraceIDRatioBased(cfg.SamplingRate)
	}

	// Create tracer provider
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)

	// Set global tracer provider
	otel.SetTracerProvider(tp)

	// Set global propagator for trace context propagation
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return &Provider{tp: tp}, nil
}

// Shutdown gracefully shuts down the tracer provider.
func (p *Provider) Shutdown(ctx context.Context) error {
	if p.tp == nil {
		return nil // noop provider
	}

	// Create a context with timeout for shutdown
	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return p.tp.Shutdown(shutdownCtx)
}

// Tracer returns a tracer for the given name.
func Tracer(name string) trace.Tracer {
	return otel.Tracer(name)
}
