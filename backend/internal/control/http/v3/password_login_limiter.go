// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"context"
	"errors"
	"sync"
	"time"
)

var (
	ErrLoginRateLimited = errors.New("password login rate limit exceeded")
)

const (
	maxIPRequestsPerMinute = 10
	maxLimiterEntries      = 5000
	limiterEntryTTL        = 5 * time.Minute
)

type ipBucket struct {
	count       int
	windowStart time.Time
}

type userBackoff struct {
	consecutiveFailures int
	lastFailure         time.Time
}

// PasswordLoginLimiter enforces per-IP sliding window rate limits, progressive username backoff,
// and bounded memory state without ever locking legitimate users out permanently.
type PasswordLoginLimiter struct {
	mu           sync.Mutex
	ipBuckets    map[string]*ipBucket
	userBackoffs map[string]*userBackoff
	lastCleanup  time.Time
	nowFn        func() time.Time
	sleepFn      func(ctx context.Context, d time.Duration) error
}

// NewPasswordLoginLimiter creates a new bounded login limiter.
func NewPasswordLoginLimiter() *PasswordLoginLimiter {
	return &PasswordLoginLimiter{
		ipBuckets:    make(map[string]*ipBucket),
		userBackoffs: make(map[string]*userBackoff),
		lastCleanup:  time.Now(),
		nowFn:        time.Now,
		sleepFn: func(ctx context.Context, d time.Duration) error {
			timer := time.NewTimer(d)
			defer timer.Stop()
			select {
			case <-timer.C:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	}
}

// CheckAllowed evaluates whether the login request from clientIP for username is permitted.
// If the IP rate limit is exceeded, ErrLoginRateLimited is returned.
// If the username has consecutive failures, a progressive backoff delay is applied.
func (l *PasswordLoginLimiter) CheckAllowed(ctx context.Context, clientIP, username string) error {
	l.mu.Lock()
	now := l.nowFn()
	l.maybeEvictExpiredLocked(now)

	// 1. Enforce IP Rate Limit (max 10 requests per minute)
	bucket, ok := l.ipBuckets[clientIP]
	if !ok || now.Sub(bucket.windowStart) > time.Minute {
		// New window
		if len(l.ipBuckets) >= maxLimiterEntries {
			l.evictOldestIPLocked()
		}
		bucket = &ipBucket{
			count:       0,
			windowStart: now,
		}
		l.ipBuckets[clientIP] = bucket
	}

	if bucket.count >= maxIPRequestsPerMinute {
		l.mu.Unlock()
		return ErrLoginRateLimited
	}

	// 2. Compute Progressive Username Backoff (NO hard account lockout)
	var delay time.Duration
	if username != "" {
		if ub, exists := l.userBackoffs[username]; exists && ub.consecutiveFailures >= 3 {
			switch {
			case ub.consecutiveFailures == 3:
				delay = 500 * time.Millisecond
			case ub.consecutiveFailures == 4:
				delay = 1 * time.Second
			case ub.consecutiveFailures == 5:
				delay = 2 * time.Second
			default:
				delay = 3 * time.Second
			}
		}
	}
	l.mu.Unlock()

	// Apply progressive delay outside the lock
	if delay > 0 {
		if err := l.sleepFn(ctx, delay); err != nil {
			return err
		}
	}

	return nil
}

// RecordFailure records a failed login attempt for clientIP and username.
func (l *PasswordLoginLimiter) RecordFailure(clientIP, username string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.nowFn()

	// Increment IP bucket
	if bucket, ok := l.ipBuckets[clientIP]; ok {
		bucket.count++
	} else {
		if len(l.ipBuckets) >= maxLimiterEntries {
			l.evictOldestIPLocked()
		}
		l.ipBuckets[clientIP] = &ipBucket{
			count:       1,
			windowStart: now,
		}
	}

	// Increment user consecutive failures
	if username != "" {
		if ub, ok := l.userBackoffs[username]; ok {
			ub.consecutiveFailures++
			ub.lastFailure = now
		} else {
			if len(l.userBackoffs) >= maxLimiterEntries {
				l.evictOldestUserLocked()
			}
			l.userBackoffs[username] = &userBackoff{
				consecutiveFailures: 1,
				lastFailure:         now,
			}
		}
	}
}

// RecordSuccess clears the failed attempts for the given username upon successful authentication.
func (l *PasswordLoginLimiter) RecordSuccess(clientIP, username string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Reset username failures on success (legitimate credentials succeed without persistent penalty)
	delete(l.userBackoffs, username)

	// Increment IP bucket for total requests in the window
	if bucket, ok := l.ipBuckets[clientIP]; ok {
		bucket.count++
	}
}

func (l *PasswordLoginLimiter) maybeEvictExpiredLocked(now time.Time) {
	if now.Sub(l.lastCleanup) < time.Minute {
		return
	}
	l.lastCleanup = now

	for ip, bucket := range l.ipBuckets {
		if now.Sub(bucket.windowStart) > limiterEntryTTL {
			delete(l.ipBuckets, ip)
		}
	}
	for user, ub := range l.userBackoffs {
		if now.Sub(ub.lastFailure) > limiterEntryTTL {
			delete(l.userBackoffs, user)
		}
	}
}

func (l *PasswordLoginLimiter) evictOldestIPLocked() {
	var oldestIP string
	var oldestTime time.Time
	for ip, b := range l.ipBuckets {
		if oldestIP == "" || b.windowStart.Before(oldestTime) {
			oldestIP = ip
			oldestTime = b.windowStart
		}
	}
	if oldestIP != "" {
		delete(l.ipBuckets, oldestIP)
	}
}

func (l *PasswordLoginLimiter) evictOldestUserLocked() {
	var oldestUser string
	var oldestTime time.Time
	for u, ub := range l.userBackoffs {
		if oldestUser == "" || ub.lastFailure.Before(oldestTime) {
			oldestUser = u
			oldestTime = ub.lastFailure
		}
	}
	if oldestUser != "" {
		delete(l.userBackoffs, oldestUser)
	}
}
