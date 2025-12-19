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

	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/trace"
)

// Config captures options for configuring the global logger.
type Config struct {
	Level   string    // optional log level ("debug", "info", etc.)
	Output  io.Writer // optional writer (defaults to os.Stdout)
	Service string    // optional service name attached to every log entry
	Version string    // optional version attached to every log entry
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
	}
	zerolog.SetGlobalLevel(level)
	zerolog.TimeFieldFormat = time.RFC3339

	writer := cfg.Output
	if writer == nil {
		writer = os.Stdout
	}

	service := cfg.Service
	if service == "" {
		service = "xg2g"
	}

	version := cfg.Version

	base = zerolog.New(writer).With().
		Timestamp().
		Str("service", service).
		Str("version", version).
		Logger().Hook(bufferHook{})
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

			ctx := r.Context()
			reqID := RequestIDFromContext(ctx)
			if reqID == "" {
				// Fallback for callers that don't run the RequestID middleware.
				reqID = r.Header.Get("X-Request-ID")
				if reqID == "" {
					reqID = uuid.New().String()
				}
				ctx = ContextWithRequestID(ctx, reqID)
				if w.Header().Get("X-Request-ID") == "" {
					w.Header().Set("X-Request-ID", reqID)
				}
			}

			logCtx := logger().With().
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

			l := WithContext(ctx, logCtx.Logger())

			// Add the logger to the request context
			r = r.WithContext(l.WithContext(ctx))

			// Capture status while preserving streaming interfaces (Flusher/Hijacker/etc).
			ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)

			// Process the request
			next.ServeHTTP(ww, r)

			// Log the request details
			l.Info().
				Str("event", "request.handled").
				Int("status", ww.Status()).
				Dur("duration", time.Since(start)).
				Msg("http request")
		})
	}
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

// LogBuffer implementation
type LogEntry struct {
	Timestamp time.Time              `json:"timestamp"`
	Level     string                 `json:"level"`
	Message   string                 `json:"message"`
	Fields    map[string]interface{} `json:"fields,omitempty"`
}

const maxLogEntries = 100

var (
	logBufferMu sync.RWMutex
	logBuffer   []LogEntry
)

type bufferHook struct{}

func (h bufferHook) Run(e *zerolog.Event, level zerolog.Level, msg string) {
	if level < zerolog.InfoLevel {
		return
	}

	logBufferMu.Lock()
	defer logBufferMu.Unlock()

	// Create entry
	entry := LogEntry{
		Timestamp: time.Now(),
		Level:     level.String(),
		Message:   msg,
		// Note: We can't easily get fields here without reflection or custom hook logic
		// For now, simple message capture is enough for UI
	}

	// Append and trim
	logBuffer = append(logBuffer, entry)
	if len(logBuffer) > maxLogEntries {
		logBuffer = logBuffer[1:]
	}
}

// GetRecentLogs returns the most recent log entries
func GetRecentLogs() []LogEntry {
	logBufferMu.RLock()
	defer logBufferMu.RUnlock()

	// Return copy
	result := make([]LogEntry, len(logBuffer))
	copy(result, logBuffer)
	return result
}

//nolint:gochecknoinits // Required to ensure logger is initialized before any usage
// Init removed to prevent side-effect environment reads.
// Configure must be called explicitly, or logger() will lazy-init with defaults.
