// SPDX-License-Identifier: MIT

// Package log provides structured logging utilities.
package log

import (
	"context"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/trace"
)

// Config captures options for configuring the global logger.
type Config struct {
	Level   string    // optional log level ("debug", "info", etc.)
	Output  io.Writer // optional writer (defaults to os.Stdout)
	Service string    // optional service name attached to every log entry
}

var (
	mu   sync.RWMutex
	base zerolog.Logger
	once sync.Once
)

// Configure initialises the global zerolog logger.
func Configure(cfg Config) {
	mu.Lock()
	defer mu.Unlock()
	level := zerolog.InfoLevel
	if cfg.Level != "" {
		if parsed, err := zerolog.ParseLevel(cfg.Level); err == nil {
			level = parsed
		}
	} else if env := os.Getenv("LOG_LEVEL"); env != "" {
		if parsed, err := zerolog.ParseLevel(env); err == nil {
			level = parsed
		}
	}
	zerolog.SetGlobalLevel(level)
	zerolog.TimeFieldFormat = time.RFC3339

	writer := cfg.Output
	if writer == nil {
		writer = os.Stdout
	}

	service := cfg.Service
	if service == "" {
		service = os.Getenv("LOG_SERVICE")
		if service == "" {
			service = "xg2g"
		}
	}

	base = zerolog.New(writer).With().
		Timestamp().
		Str("service", service).
		Str("version", os.Getenv("VERSION")).
		Logger()
}

func logger() zerolog.Logger {
	once.Do(func() {
		Configure(Config{})
	})
	mu.RLock()
	defer mu.RUnlock()
	return base
}

// Base returns the configured base logger instance.
func Base() zerolog.Logger {
	return logger()
}

// L provides access to the global logger instance.
func L() *zerolog.Logger {
	once.Do(func() {
		Configure(Config{})
	})
	mu.RLock()
	defer mu.RUnlock()
	return &base
}

// Middleware returns a new http.Handler middleware that logs requests using zerolog.
func Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			// Create a logger with a unique request ID
			reqID := uuid.New().String()
			logCtx := logger().With().
				Str("req_id", reqID).
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Str("remote_addr", r.RemoteAddr).
				Str("user_agent", r.UserAgent())

			// Add trace context if available (OpenTelemetry integration)
			span := trace.SpanFromContext(r.Context())
			if span.SpanContext().IsValid() {
				logCtx = logCtx.
					Str("trace_id", span.SpanContext().TraceID().String()).
					Str("span_id", span.SpanContext().SpanID().String())
			}

			l := logCtx.Logger()

			// Add the logger to the request context
			ctx := l.WithContext(r.Context())
			r = r.WithContext(ctx)

			// Use a status recorder to capture the response status
			sr := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

			// Process the request
			next.ServeHTTP(sr, r)

			// Log the request details
			l.Info().
				Str("event", "request.handled").
				Int("status", sr.status).
				Dur("duration", time.Since(start)).
				Msg("http request")
		})
	}
}

// statusRecorder wraps http.ResponseWriter to capture the status code.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

// WriteHeader captures the status code before writing it to the response.
func (sr *statusRecorder) WriteHeader(code int) {
	sr.status = code
	sr.ResponseWriter.WriteHeader(code)
}

// WithComponent returns a child logger annotated with the given component name.
func WithComponent(component string) zerolog.Logger {
	l := logger().With().Str("component", component).Logger()
	return l
}

// Derive attaches arbitrary fields to a child logger using the provided builder function.
func Derive(build func(*zerolog.Context)) zerolog.Logger {
	ctx := logger().With()
	if build != nil {
		build(&ctx)
	}
	return ctx.Logger()
}

// WithTraceContext returns a logger enriched with trace_id and span_id from the context.
// This enables correlation between logs and distributed traces.
func WithTraceContext(ctx context.Context) zerolog.Logger {
	l := logger()
	span := trace.SpanFromContext(ctx)
	if span.SpanContext().IsValid() {
		l = l.With().
			Str("trace_id", span.SpanContext().TraceID().String()).
			Str("span_id", span.SpanContext().SpanID().String()).
			Logger()
	}
	return l
}

//nolint:gochecknoinits // Required to ensure logger is initialized before any usage
func init() {
	Configure(Config{})
}
