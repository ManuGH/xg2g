// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

// Package log provides structured logging utilities.
package log

import (
	"context"

	"github.com/rs/zerolog"
)

type ctxKey string

const (
	requestIDKey     ctxKey = "request_id"
	correlationIDKey ctxKey = "correlation_id"
	jobIDKey         ctxKey = "job_id"
)

// ContextWithRequestID stores the provided request ID in the context.
func ContextWithRequestID(ctx context.Context, id string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, requestIDKey, id)
}

// ContextWithCorrelationID stores the provided correlation ID in the context.
func ContextWithCorrelationID(ctx context.Context, id string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, correlationIDKey, id)
}

// ContextWithJobID stores the provided job ID in the context.
func ContextWithJobID(ctx context.Context, id string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, jobIDKey, id)
}

// RequestIDFromContext extracts the request ID from context if present.
func RequestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(requestIDKey).(string); ok {
		return v
	}
	return ""
}

// CorrelationIDFromContext extracts the correlation ID from context if present.
func CorrelationIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(correlationIDKey).(string); ok {
		return v
	}
	return ""
}

// JobIDFromContext extracts the job ID from context if present.
func JobIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(jobIDKey).(string); ok {
		return v
	}
	return ""
}

// WithContext enriches the supplied logger with correlation fields from context.
func WithContext(ctx context.Context, logger zerolog.Logger) zerolog.Logger {
	if ctx == nil {
		return logger
	}
	builder := logger.With()
	added := false
	if rid := RequestIDFromContext(ctx); rid != "" {
		builder = builder.Str("request_id", rid)
		added = true
	}
	if cid := CorrelationIDFromContext(ctx); cid != "" {
		builder = builder.Str("correlation_id", cid)
		added = true
	}
	if jid := JobIDFromContext(ctx); jid != "" {
		builder = builder.Str("job_id", jid)
		added = true
	}
	if !added {
		return logger
	}
	return builder.Logger()
}

// WithComponentFromContext returns a logger that is annotated with the component
// name and enriched with correlation fields from ctx.
func WithComponentFromContext(ctx context.Context, component string) zerolog.Logger {
	l := FromContext(ctx)
	return l.With().Str("component", component).Logger()
}

// FromContext returns a logger from the context, or a new one if not present.
func FromContext(ctx context.Context) *zerolog.Logger {
	if ctx == nil {
		l := Base()
		return &l
	}
	l := zerolog.Ctx(ctx)
	if l.GetLevel() == zerolog.Disabled {
		// If no logger is in the context, return the base logger.
		b := Base()
		return &b
	}
	return l
}
