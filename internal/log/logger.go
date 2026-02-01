// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Package log provides structured logging utilities.
package log

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// ErrInvalidLogLevel is returned when a level string cannot be parsed.
var ErrInvalidLogLevel = errors.New("invalid log level")

// Config captures options for configuring the global logger.
type Config struct {
	Level   string    // optional log level ("debug", "info", etc.)
	Output  io.Writer // optional writer (defaults to os.Stdout)
	Service string    // optional service name attached to every log entry
	Version string    // optional version attached to every log entry
}

var (
	mu          sync.RWMutex
	base        zerolog.Logger
	auditBase   zerolog.Logger
	initialized bool
)

// Configure initialises the global zerolog logger with the provided configuration.
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

	// We use a MultiWriter to feed both the output and our structured buffer.
	bufferWriter := &structuredBufferWriter{}
	multi := io.MultiWriter(writer, bufferWriter)

	base = zerolog.New(multi).With().
		Timestamp().
		Str("service", service).
		Str("version", version).
		Logger()

	auditBase = zerolog.New(multi).With().
		Timestamp().
		Str("service", service).
		Str("version", version).
		Str("component", "audit").
		Logger()

	initialized = true
}

func ensureInitialized() {
	mu.RLock()
	if initialized {
		mu.RUnlock()
		return
	}
	mu.RUnlock()

	Configure(Config{})
}

// SetLevel updates the global log level using a thread-safe transition.
func SetLevel(ctx context.Context, principal string, scopes []string, newLevel string) error {
	ensureInitialized()
	parsed, err := zerolog.ParseLevel(newLevel)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidLogLevel, newLevel)
	}

	mu.Lock()
	oldLevel := zerolog.GlobalLevel()
	if oldLevel == parsed {
		mu.Unlock()
		return nil
	}
	zerolog.SetGlobalLevel(parsed)
	mu.Unlock()

	// Audit Trail: Functional API ensures no-silence policy.
	AuditInfo(ctx, "log.level_changed", "runtime log level updated", map[string]any{
		"who":    principal,
		"scopes": scopes,
		"from":   oldLevel.String(),
		"to":     parsed.String(),
	})

	return nil
}

// AuditInfo records a governance-critical event.
// It bypasses the global log level filter to ensure a complete audit trail.
func AuditInfo(ctx context.Context, event string, msg string, fields map[string]any) {
	ensureInitialized()
	mu.RLock()
	logger := auditBase
	mu.RUnlock()

	// Bypass GlobalLevel gating by using .Log()
	ev := logger.Log().
		Str("audit_severity", "info"). // Honest governance field
		Str("event", event).
		Str("request_id", RequestIDFromContext(ctx))

	for k, v := range fields {
		ev = ev.Interface(k, v)
	}

	ev.Msg(msg)
}

func logger() zerolog.Logger {
	ensureInitialized()
	mu.RLock()
	defer mu.RUnlock()
	return base
}

// Base returns the configured base logger instance by value.
func Base() zerolog.Logger {
	return logger()
}

// L provides access to the global logger instance as a pointer to a copy.
func L() *zerolog.Logger {
	l := logger()
	return &l
}

// Middleware returns a http.Handler middleware that logs requests using zerolog.
func Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			ctx := r.Context()

			// Request-ID Continuity: Don't overwrite if subrouter/upstream already set it.
			reqID := RequestIDFromContext(ctx)
			if reqID == "" {
				reqID = uuid.New().String()
				ctx = ContextWithRequestID(ctx, reqID)
			}

			// Secondary metadata for correlation.
			if clientID := r.Header.Get("X-Request-ID"); clientID != "" {
				ctx = ContextWithClientRequestID(ctx, clientID)
			}

			if w.Header().Get("X-Request-ID") == "" {
				w.Header().Set("X-Request-ID", reqID)
			}

			logCtx := logger().With().
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Str("remote_addr", r.RemoteAddr).
				Str("user_agent", r.UserAgent())

			span := trace.SpanFromContext(r.Context())
			if span.SpanContext().IsValid() {
				logCtx = logCtx.
					Str("trace_id", span.SpanContext().TraceID().String()).
					Str("span_id", span.SpanContext().SpanID().String())
			}

			l := WithContext(ctx, logCtx.Logger())
			r = r.WithContext(l.WithContext(ctx))

			ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)

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

// structuredBufferWriter is an io.Writer that robustly parses JSON logs for the buffer.
type structuredBufferWriter struct {
	mu      sync.Mutex
	partial bytes.Buffer
}

const (
	maxPartialBytes = 1 << 20  // 1 MiB: limit accumulation of non-terminated lines
	maxLineBytes    = 64 << 10 // 64 KiB: limit parsing of giant log lines
)

// BufferMetrics captures telemetry about the diagnostic log buffer.
type BufferMetrics struct {
	DroppedTooLargeLines   int64
	DroppedPartialOverflow int64
	DroppedIrrelevant      int64
	UnmarshalFailures      int64
}

var bufferMetrics BufferMetrics

// GetBufferMetrics returns current log buffer telemetry.
func GetBufferMetrics() BufferMetrics {
	logBufferMu.RLock()
	defer logBufferMu.RUnlock()
	return bufferMetrics
}

func (w *structuredBufferWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	if w.partial.Len()+len(p) > maxPartialBytes {
		// Prevent OOM: if accumulation exceeds 1MiB without a newline, reset.
		w.partial.Reset()
		bufferMetrics.DroppedPartialOverflow++
		w.mu.Unlock()
		return len(p), nil
	}
	w.partial.Write(p)
	data := w.partial.Bytes()

	lastNL := bytes.LastIndexByte(data, '\n')
	if lastNL == -1 {
		w.mu.Unlock()
		return len(p), nil
	}

	// Extract full lines
	lines := make([]byte, lastNL+1)
	copy(lines, data[:lastNL+1])

	// Keep remainder
	remainder := data[lastNL+1:]
	w.partial.Reset()
	w.partial.Write(remainder)
	w.mu.Unlock()

	// Process lines outside of the framing lock to reduce contention
	start := 0
	for i := 0; i < len(lines); i++ {
		if lines[i] == '\n' {
			w.processLine(lines[start:i])
			start = i + 1
		}
	}

	return len(p), nil
}

func (w *structuredBufferWriter) processLine(line []byte) {
	if len(line) == 0 {
		return
	}
	if len(line) > maxLineBytes {
		logBufferMu.Lock()
		bufferMetrics.DroppedTooLargeLines++
		logBufferMu.Unlock()
		return
	}

	// HARTER HINWEIS: Filter for relevance before Allocation/Unmarshal
	// CONTRACT: Only component:audit or event:request.handled are captured.
	isAudit := bytes.Contains(line, []byte("\"component\":\"audit\""))
	isRequest := bytes.Contains(line, []byte("\"event\":\"request.handled\""))
	if !isAudit && !isRequest {
		logBufferMu.Lock()
		bufferMetrics.DroppedIrrelevant++
		logBufferMu.Unlock()
		return
	}

	var raw map[string]any
	if err := json.Unmarshal(line, &raw); err != nil {
		logBufferMu.Lock()
		bufferMetrics.UnmarshalFailures++
		logBufferMu.Unlock()
		return
	}

	entry := LogEntry{Fields: make(map[string]any)}

	// Extract known fields
	if ts, ok := raw["time"].(string); ok {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			entry.Timestamp = t
		}
	}
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}

	if lvl, ok := raw["level"].(string); ok {
		entry.Level = lvl
	} else if as, ok := raw["audit_severity"].(string); ok {
		entry.Level = as
	} else {
		entry.Level = "info"
	}

	if msg, ok := raw["message"].(string); ok {
		entry.Message = msg
	}

	// Capture all other fields
	for k, v := range raw {
		switch k {
		case "time", "level", "message", "audit_severity":
			continue
		default:
			entry.Fields[k] = v
		}
	}

	logBufferMu.Lock()
	logBuffer = append(logBuffer, entry)
	if len(logBuffer) > maxLogEntries {
		logBuffer = logBuffer[1:]
	}
	logBufferMu.Unlock()
}

// GetRecentLogs returns the most recent log entries
func GetRecentLogs() []LogEntry {
	logBufferMu.RLock()
	defer logBufferMu.RUnlock()

	result := make([]LogEntry, len(logBuffer))
	copy(result, logBuffer)
	return result
}

// ClearRecentLogs clears the in-memory log buffer.
func ClearRecentLogs() {
	logBufferMu.Lock()
	defer logBufferMu.Unlock()
	logBuffer = nil
}
