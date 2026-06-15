// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

package middleware

import (
	"fmt"
	"net"
	"net/http"
	"strings"
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

	whitelistIPs, whitelistNets := parseWhitelist(cfg.Whitelist)

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
		limitedNext := limiter(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check whitelist
			if len(whitelistIPs) > 0 || len(whitelistNets) > 0 {
				if clientIP := requestIP(r); clientIP != nil && isWhitelisted(clientIP, whitelistIPs, whitelistNets) {
					next.ServeHTTP(w, r)
					return
				}
			}
			// Delegate to limiter
			limitedNext.ServeHTTP(w, r)
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
//
// There is intentionally NO burst parameter: this limiter (httprate) is a sliding-window
// counter, as is the per-class exposureRateLimiter, and a window counter has no burst
// capacity to configure. The former api.rateLimit.burst knob is deprecated and inert — see
// DeprecatedBurstWarning. (The working burst lives elsewhere, on the Enigma2 client's
// x/time/rate token bucket, and is unrelated to this API limiter.)
func APIRateLimit(enabled bool, rps int, whitelist []string) func(http.Handler) http.Handler {
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

// DeprecatedAPIRateLimitBurstDefault is the historical default of the now-inert
// api.rateLimit.burst config knob. A configured value differing from it means the operator
// explicitly tuned a setting that has no effect.
const DeprecatedAPIRateLimitBurstDefault = 20

// DeprecatedBurstWarning reports whether a configured API rate-limit burst value warrants a
// one-time startup warning, plus the message to log. The API rate limiter is window-based and
// has no burst capacity, so the value is inert; an operator who set a NON-DEFAULT value
// expects an effect and deserves to be told there is none, rather than silently debugging why
// their tuning does nothing. A value equal to the default (i.e. effectively unconfigured)
// warrants no warning. Pure function so the decision is unit-tested without log capture.
func DeprecatedBurstWarning(burst int) (string, bool) {
	if burst == DeprecatedAPIRateLimitBurstDefault {
		return "", false
	}
	return fmt.Sprintf("api.rateLimit.burst is set to %d but has no effect: the API rate limiter is window-based and does not support burst capacity (deprecated, inert)", burst), true
}

func parseWhitelist(entries []string) ([]net.IP, []*net.IPNet) {
	if len(entries) == 0 {
		return nil, nil
	}

	ips := make([]net.IP, 0, len(entries))
	nets := make([]*net.IPNet, 0, len(entries))
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if ip := net.ParseIP(entry); ip != nil {
			ips = append(ips, ip)
			continue
		}
		if _, ipNet, err := net.ParseCIDR(entry); err == nil {
			nets = append(nets, ipNet)
		}
	}

	return ips, nets
}

func requestIP(r *http.Request) net.IP {
	if ipStr, err := httprate.KeyByIP(r); err == nil && ipStr != "" {
		if ip := net.ParseIP(ipStr); ip != nil {
			return ip
		}
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return net.ParseIP(host)
}

func isWhitelisted(ip net.IP, ips []net.IP, nets []*net.IPNet) bool {
	for _, allowed := range ips {
		if allowed.Equal(ip) {
			return true
		}
	}
	for _, allowed := range nets {
		if allowed.Contains(ip) {
			return true
		}
	}
	return false
}
