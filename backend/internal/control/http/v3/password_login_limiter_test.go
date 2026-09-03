// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPasswordLoginLimiter_IPRateLimit(t *testing.T) {
	limiter := NewPasswordLoginLimiter()
	ctx := context.Background()
	clientIP := "192.168.1.50"

	// 10 requests allowed within the 1-minute window
	for i := 0; i < maxIPRequestsPerMinute; i++ {
		err := limiter.CheckAllowed(ctx, clientIP, "alice")
		require.NoError(t, err, "request %d should be allowed", i+1)
		limiter.RecordFailure(clientIP, "alice")
	}

	// 11th request must be rate limited
	err := limiter.CheckAllowed(ctx, clientIP, "alice")
	assert.ErrorIs(t, err, ErrLoginRateLimited, "11th request must exceed rate limit")

	// Different IP should still be allowed
	errOtherIP := limiter.CheckAllowed(ctx, "192.168.1.51", "alice")
	assert.NoError(t, errOtherIP, "different IP should have independent rate limit bucket")
}

func TestPasswordLoginLimiter_UsernameProgressiveBackoff(t *testing.T) {
	limiter := NewPasswordLoginLimiter()
	ctx := context.Background()
	clientIP := "10.0.0.1"
	username := "admin"

	var lastSleptDuration atomic.Int64
	limiter.sleepFn = func(ctx context.Context, d time.Duration) error {
		lastSleptDuration.Store(d.Milliseconds())
		return nil
	}

	// 0 failures: no delay
	err := limiter.CheckAllowed(ctx, clientIP, username)
	require.NoError(t, err)
	assert.Equal(t, int64(0), lastSleptDuration.Load())

	// Record 3 failures
	limiter.RecordFailure(clientIP, username)
	limiter.RecordFailure(clientIP, username)
	limiter.RecordFailure(clientIP, username)

	// 4th check: 3 previous failures trigger 500ms progressive delay
	err = limiter.CheckAllowed(ctx, clientIP, username)
	require.NoError(t, err, "must NOT hard-lockout user")
	assert.Equal(t, int64(500), lastSleptDuration.Load())

	// Record 4th failure
	limiter.RecordFailure(clientIP, username)

	// 5th check: 4 failures trigger 1000ms delay
	err = limiter.CheckAllowed(ctx, clientIP, username)
	require.NoError(t, err)
	assert.Equal(t, int64(1000), lastSleptDuration.Load())

	// Success resets username failure count
	limiter.RecordSuccess(clientIP, username)
	lastSleptDuration.Store(0)

	// Check after success: delay is reset to 0
	err = limiter.CheckAllowed(ctx, clientIP, username)
	require.NoError(t, err)
	assert.Equal(t, int64(0), lastSleptDuration.Load(), "delay must reset on successful authentication")
}

func TestPasswordLoginLimiter_BoundedMemory(t *testing.T) {
	limiter := NewPasswordLoginLimiter()
	ctx := context.Background()

	// Fill with 6000 distinct IPs and usernames (cap is 5000)
	for i := 0; i < 6000; i++ {
		ip := fmt.Sprintf("10.0.%d.%d", i/256, i%256)
		user := fmt.Sprintf("user_%d", i)
		_ = limiter.CheckAllowed(ctx, ip, user)
		limiter.RecordFailure(ip, user)
	}

	limiter.mu.Lock()
	defer limiter.mu.Unlock()

	assert.LessOrEqual(t, len(limiter.ipBuckets), maxLimiterEntries, "IP buckets map must be bounded")
	assert.LessOrEqual(t, len(limiter.userBackoffs), maxLimiterEntries, "User backoff map must be bounded")
}
