// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// SPDX-License-Identifier: MIT

package middleware

import (
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/go-chi/httprate"
)

// RateLimitConfig holds configuration for rate limiting middleware.
type RateLimitConfig struct {
	// RequestLimit is the maximum number of requests allowed in the window
	RequestLimit int
	// WindowSize is the time window for rate limiting
	WindowSize time.Duration
	// KeyFunc extracts the rate limit key from the request (e.g., IP address)
	// If nil, defaults to IP-based rate limiting
	KeyFunc func(r *http.Request) (string, error)
	// Whitelist is a list of IPs or CIDRs to exempt from rate limiting
	Whitelist []string
}

// RateLimit creates a rate limiting middleware using the httprate library.
// It uses a sliding window counter algorithm for accurate rate limiting.
func RateLimit(cfg RateLimitConfig) func(http.Handler) http.Handler {
	// Default to IP-based rate limiting if no key function provided
	keyFunc := cfg.KeyFunc
	if keyFunc == nil {
		keyFunc = httprate.KeyByIP
	}

	// Create httprate limiter with sliding window
	limiter := httprate.Limit(
		cfg.RequestLimit,
		cfg.WindowSize,
		httprate.WithKeyFuncs(keyFunc),
		httprate.WithLimitHandler(func(w http.ResponseWriter, r *http.Request) {
			// Custom 429 response with Retry-After header
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", fmt.Sprintf("%d", int(cfg.WindowSize.Seconds())))
			w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", cfg.RequestLimit))
			w.WriteHeader(http.StatusTooManyRequests)

			// Write JSON error response
			resp := `{"error":"rate_limit_exceeded","detail":"Too many requests. Please try again later."}`
			_, _ = w.Write([]byte(resp))
		}),
	)

	// Wrap with whitelist check
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check whitelist
			if len(cfg.Whitelist) > 0 {
				ip, _, err := net.SplitHostPort(r.RemoteAddr)
				if err != nil {
					ip = r.RemoteAddr
				}
				for _, allowed := range cfg.Whitelist {
					if allowed == ip { // TODO: Support CIDR
						next.ServeHTTP(w, r)
						return
					}
				}
			}
			// Delegate to limiter
			limiter(next).ServeHTTP(w, r)
		})
	}
}

func RefreshRateLimit() func(http.Handler) http.Handler {
	return RateLimit(RateLimitConfig{
		RequestLimit: 10,
		WindowSize:   time.Minute,
	})
}

// APIRateLimit returns a rate limiter configured via AppConfig.
func APIRateLimit(enabled bool, rps int, burst int, whitelist []string) func(http.Handler) http.Handler {
	if !enabled {
		// Passthrough if disabled
		return func(next http.Handler) http.Handler {
			return next
		}
	}

	if rps <= 0 {
		rps = 100 // Default safety net
	}

	// Sliding window logic: Window is 1 minute.
	// We map RPS to requests per minute.
	limit := rps * 60

	return RateLimit(RateLimitConfig{
		RequestLimit: limit,
		WindowSize:   time.Minute,
		Whitelist:    whitelist,
	})
}
