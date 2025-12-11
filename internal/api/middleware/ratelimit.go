// SPDX-License-Identifier: MIT

package middleware

import (
	"fmt"
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
}

// RateLimit creates a rate limiting middleware using the httprate library.
// It uses a sliding window counter algorithm for accurate rate limiting.
//
// Example usage:
//
//	// Limit to 10 requests per minute per IP
//	r.Use(middleware.RateLimit(middleware.RateLimitConfig{
//	    RequestLimit: 10,
//	    WindowSize:   time.Minute,
//	}))
func RateLimit(cfg RateLimitConfig) func(http.Handler) http.Handler {
	// Default to IP-based rate limiting if no key function provided
	keyFunc := cfg.KeyFunc
	if keyFunc == nil {
		keyFunc = httprate.KeyByIP
	}

	// Create httprate limiter with sliding window
	return httprate.Limit(
		cfg.RequestLimit,
		cfg.WindowSize,
		httprate.WithKeyFuncs(keyFunc),
		httprate.WithLimitHandler(func(w http.ResponseWriter, r *http.Request) {
			// Custom 429 response with Retry-After header
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", fmt.Sprintf("%d", int(cfg.WindowSize.Seconds())))
			w.WriteHeader(http.StatusTooManyRequests)

			// Write JSON error response
			resp := `{"error":"rate_limit_exceeded","detail":"Too many requests. Please try again later."}`
			_, _ = w.Write([]byte(resp))
		}),
	)
}

// RefreshRateLimit returns a rate limiter configured for expensive refresh operations.
// Default: 10 requests per minute per IP to prevent abuse of expensive operations.
func RefreshRateLimit() func(http.Handler) http.Handler {
	return RateLimit(RateLimitConfig{
		RequestLimit: 10,
		WindowSize:   time.Minute,
	})
}

// APIRateLimit returns a rate limiter configured for general API endpoints.
// Default: 60 requests per minute per IP for standard API operations.
func APIRateLimit() func(http.Handler) http.Handler {
	return RateLimit(RateLimitConfig{
		RequestLimit: 600,
		WindowSize:   time.Minute,
	})
}
