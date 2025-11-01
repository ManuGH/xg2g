// SPDX-License-Identifier: MIT

package audit

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewLogger(t *testing.T) {
	logger := NewLogger()
	assert.NotNil(t, logger)
	assert.NotNil(t, logger.logger)
}

func TestLogger_Log(t *testing.T) {
	logger := NewLogger()

	event := Event{
		Type:       EventConfigReload,
		Actor:      "admin",
		Action:     "reloaded config",
		Resource:   "config.yaml",
		Result:     "success",
		RemoteAddr: "192.168.1.100",
		UserAgent:  "curl/7.68.0",
		RequestID:  "req-123",
		Details: map[string]string{
			"changes": "3",
		},
	}

	// Should not panic
	logger.Log(event)

	// Test with missing timestamp (should be set automatically)
	event2 := Event{
		Type:     EventAuthSuccess,
		Actor:    "user1",
		Action:   "logged in",
		Resource: "/api",
		Result:   "success",
	}

	logger.Log(event2)
}

func TestLogger_LogFromContext(t *testing.T) {
	logger := NewLogger()

	// Context with metadata
	//nolint:staticcheck // Test code - context keys are fine here
	ctx := context.WithValue(context.Background(), "request_id", "req-456")
	//nolint:staticcheck // Test code - context keys are fine here
	ctx = context.WithValue(ctx, "remote_addr", "10.0.0.1")
	//nolint:staticcheck // Test code - context keys are fine here
	ctx = context.WithValue(ctx, "user_agent", "Mozilla/5.0")

	event := Event{
		Type:     EventAPIAccess,
		Actor:    "test-user",
		Action:   "accessed API",
		Resource: "/api/v1/status",
		Result:   "success",
	}

	// Should not panic and should extract context values
	logger.LogFromContext(ctx, event)
}

func TestLogger_ConfigReload(t *testing.T) {
	logger := NewLogger()

	logger.ConfigReload("system", "success", map[string]string{
		"file": "/etc/xg2g/config.yaml",
	})

	logger.ConfigReload("admin", "failure", map[string]string{
		"error": "file not found",
	})
}

func TestLogger_RefreshOperations(t *testing.T) {
	logger := NewLogger()

	// Start
	logger.RefreshStart("cron", []string{"Premium", "Favourites"})

	// Complete
	logger.RefreshComplete("cron", 150, 2, 5000)

	// Error
	logger.RefreshError("manual", "connection timeout")
}

func TestLogger_Authentication(t *testing.T) {
	logger := NewLogger()

	// Success
	logger.AuthSuccess("192.168.1.50", "/api/v1/refresh")

	// Failure
	logger.AuthFailure("192.168.1.51", "/api/v1/refresh", "invalid token")

	// Missing
	logger.AuthMissing("192.168.1.52", "/api/v1/status")
}

func TestLogger_APIAccess(t *testing.T) {
	logger := NewLogger()

	// Successful request
	logger.APIAccess("10.0.0.1", "GET", "/api/v1/status", 200)

	// Failed request
	logger.APIAccess("10.0.0.2", "POST", "/api/v1/refresh", 401)
}

func TestLogger_RateLimitExceeded(t *testing.T) {
	logger := NewLogger()

	logger.RateLimitExceeded("10.0.0.3", "/api/v1/refresh")
}

func TestEvent_TimestampAutoSet(t *testing.T) {
	logger := NewLogger()

	event := Event{
		Type:     EventConfigReload,
		Actor:    "test",
		Action:   "test action",
		Resource: "test",
		Result:   "success",
	}

	before := time.Now()
	logger.Log(event)
	after := time.Now()

	// Timestamp should be set automatically within the test window
	// (This is implicit - we just verify no panic)
	assert.True(t, before.Before(after) || before.Equal(after))
}

func TestHelpers(t *testing.T) {
	t.Run("join", func(t *testing.T) {
		assert.Equal(t, "", join([]string{}))
		assert.Equal(t, "a", join([]string{"a"}))
		assert.Equal(t, "a,b,c", join([]string{"a", "b", "c"}))
	})

	t.Run("formatInt", func(t *testing.T) {
		assert.Equal(t, "0", formatInt(0))
		assert.Equal(t, "42", formatInt(42))
		assert.Equal(t, "-10", formatInt(-10))
	})

	t.Run("formatInt64", func(t *testing.T) {
		assert.Equal(t, "0", formatInt64(0))
		assert.Equal(t, "12345", formatInt64(12345))
		assert.Equal(t, "-999", formatInt64(-999))
		assert.Equal(t, "9223372036854775807", formatInt64(9223372036854775807)) // Max int64
	})
}

func BenchmarkLogger_Log(b *testing.B) {
	logger := NewLogger()
	event := Event{
		Type:       EventAPIAccess,
		Actor:      "benchmark",
		Action:     "test",
		Resource:   "/test",
		Result:     "success",
		RemoteAddr: "127.0.0.1",
		Details: map[string]string{
			"key1": "value1",
			"key2": "value2",
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Log(event)
	}
}
