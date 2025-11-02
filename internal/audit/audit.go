// SPDX-License-Identifier: MIT

// Package audit provides structured audit logging for security-sensitive operations.
// It follows the WHO/WHAT/WHEN pattern for compliance and forensics.
package audit

import (
	"context"
	"time"

	"github.com/ManuGH/xg2g/internal/log"
	"github.com/rs/zerolog"
)

// EventType represents the type of audit event.
type EventType string

const (
	// Configuration events
	EventConfigReload      EventType = "config.reload"
	EventConfigReloadError EventType = "config.reload.error"

	// Refresh events
	EventRefreshStart   EventType = "refresh.start"
	EventRefreshSuccess EventType = "refresh.success"
	EventRefreshError   EventType = "refresh.error"

	// Authentication events
	EventAuthSuccess EventType = "auth.success"
	EventAuthFailure EventType = "auth.failure"
	EventAuthMissing EventType = "auth.missing"

	// API access events
	EventAPIAccess    EventType = "api.access"
	EventAPIForbidden EventType = "api.forbidden"
	EventAPIRateLimit EventType = "api.ratelimit"
)

// Event represents a structured audit event.
type Event struct {
	Timestamp  time.Time         `json:"timestamp"`
	Type       EventType         `json:"type"`
	Actor      string            `json:"actor"`             // WHO: username, IP, or "system"
	Action     string            `json:"action"`            // WHAT: human-readable action description
	Resource   string            `json:"resource"`          // Resource affected (e.g., endpoint, config file)
	Result     string            `json:"result"`            // success, failure, denied
	RemoteAddr string            `json:"remote_addr"`       // Client IP address
	UserAgent  string            `json:"user_agent"`        // Client user agent
	RequestID  string            `json:"request_id"`        // Correlation ID
	Details    map[string]string `json:"details,omitempty"` // Additional context
}

// Logger provides audit logging functionality.
type Logger struct {
	logger zerolog.Logger
}

// NewLogger creates a new audit logger with a dedicated "audit" component.
func NewLogger() *Logger {
	// Create a dedicated audit logger with clear identification
	auditLogger := log.WithComponent("audit").With().
		Str("log_type", "audit").
		Logger()

	return &Logger{
		logger: auditLogger,
	}
}

// Log writes an audit event to the audit log.
func (l *Logger) Log(event Event) {
	// Set timestamp if not provided
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	// Build log event
	logEvent := l.logger.Info().
		Time("timestamp", event.Timestamp).
		Str("event_type", string(event.Type)).
		Str("actor", event.Actor).
		Str("action", event.Action).
		Str("resource", event.Resource).
		Str("result", event.Result)

	// Add optional fields
	if event.RemoteAddr != "" {
		logEvent.Str("remote_addr", event.RemoteAddr)
	}
	if event.UserAgent != "" {
		logEvent.Str("user_agent", event.UserAgent)
	}
	if event.RequestID != "" {
		logEvent.Str("request_id", event.RequestID)
	}

	// Add details as flattened fields
	for key, value := range event.Details {
		logEvent.Str(key, value)
	}

	logEvent.Msg("audit event")
}

// LogFromContext logs an audit event with context information.
// It automatically extracts request ID, remote addr, and user agent from the context.
func (l *Logger) LogFromContext(ctx context.Context, event Event) {
	// Extract request metadata from context if available
	// (These would be set by middleware in a real app)
	if event.RequestID == "" {
		if reqID := ctx.Value("request_id"); reqID != nil {
			if id, ok := reqID.(string); ok {
				event.RequestID = id
			}
		}
	}

	if event.RemoteAddr == "" {
		if addr := ctx.Value("remote_addr"); addr != nil {
			if ip, ok := addr.(string); ok {
				event.RemoteAddr = ip
			}
		}
	}

	if event.UserAgent == "" {
		if ua := ctx.Value("user_agent"); ua != nil {
			if agent, ok := ua.(string); ok {
				event.UserAgent = agent
			}
		}
	}

	l.Log(event)
}

// ConfigReload logs a configuration reload event.
func (l *Logger) ConfigReload(actor, result string, details map[string]string) {
	l.Log(Event{
		Type:     EventConfigReload,
		Actor:    actor,
		Action:   "reloaded configuration",
		Resource: "config",
		Result:   result,
		Details:  details,
	})
}

// RefreshStart logs the start of a refresh operation.
func (l *Logger) RefreshStart(actor string, bouquets []string) {
	l.Log(Event{
		Type:     EventRefreshStart,
		Actor:    actor,
		Action:   "started refresh operation",
		Resource: "refresh",
		Result:   "started",
		Details: map[string]string{
			"bouquets": join(bouquets),
		},
	})
}

// RefreshComplete logs a completed refresh operation.
func (l *Logger) RefreshComplete(actor string, channels, bouquets int, durationMS int64) {
	l.Log(Event{
		Type:     EventRefreshSuccess,
		Actor:    actor,
		Action:   "completed refresh operation",
		Resource: "refresh",
		Result:   "success",
		Details: map[string]string{
			"channels":    formatInt(channels),
			"bouquets":    formatInt(bouquets),
			"duration_ms": formatInt64(durationMS),
		},
	})
}

// RefreshError logs a failed refresh operation.
func (l *Logger) RefreshError(actor, reason string) {
	l.Log(Event{
		Type:     EventRefreshError,
		Actor:    actor,
		Action:   "refresh operation failed",
		Resource: "refresh",
		Result:   "failure",
		Details: map[string]string{
			"error": reason,
		},
	})
}

// AuthSuccess logs a successful authentication.
func (l *Logger) AuthSuccess(remoteAddr, endpoint string) {
	l.Log(Event{
		Type:       EventAuthSuccess,
		Actor:      remoteAddr,
		Action:     "authenticated successfully",
		Resource:   endpoint,
		Result:     "success",
		RemoteAddr: remoteAddr,
	})
}

// AuthFailure logs a failed authentication attempt.
func (l *Logger) AuthFailure(remoteAddr, endpoint, reason string) {
	l.Log(Event{
		Type:       EventAuthFailure,
		Actor:      remoteAddr,
		Action:     "authentication failed",
		Resource:   endpoint,
		Result:     "failure",
		RemoteAddr: remoteAddr,
		Details: map[string]string{
			"reason": reason,
		},
	})
}

// AuthMissing logs a request without authentication.
func (l *Logger) AuthMissing(remoteAddr, endpoint string) {
	l.Log(Event{
		Type:       EventAuthMissing,
		Actor:      remoteAddr,
		Action:     "accessed endpoint without authentication",
		Resource:   endpoint,
		Result:     "denied",
		RemoteAddr: remoteAddr,
	})
}

// APIAccess logs API endpoint access.
func (l *Logger) APIAccess(remoteAddr, method, endpoint string, statusCode int) {
	result := "success"
	if statusCode >= 400 {
		result = "failure"
	}

	l.Log(Event{
		Type:       EventAPIAccess,
		Actor:      remoteAddr,
		Action:     method + " " + endpoint,
		Resource:   endpoint,
		Result:     result,
		RemoteAddr: remoteAddr,
		Details: map[string]string{
			"method":      method,
			"status_code": formatInt(statusCode),
		},
	})
}

// RateLimitExceeded logs rate limit violations.
func (l *Logger) RateLimitExceeded(remoteAddr, endpoint string) {
	l.Log(Event{
		Type:       EventAPIRateLimit,
		Actor:      remoteAddr,
		Action:     "rate limit exceeded",
		Resource:   endpoint,
		Result:     "denied",
		RemoteAddr: remoteAddr,
	})
}

// Helper functions

func join(strs []string) string {
	result := ""
	for i, s := range strs {
		if i > 0 {
			result += ","
		}
		result += s
	}
	return result
}

func formatInt(i int) string {
	return formatInt64(int64(i))
}

func formatInt64(i int64) string {
	// Simple int64 to string conversion
	if i == 0 {
		return "0"
	}

	neg := i < 0
	if neg {
		i = -i
	}

	var buf [20]byte
	pos := len(buf)

	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}

	if neg {
		pos--
		buf[pos] = '-'
	}

	return string(buf[pos:])
}
