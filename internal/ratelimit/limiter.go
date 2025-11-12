// SPDX-License-Identifier: MIT

package ratelimit

import (
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"golang.org/x/time/rate"
)

var (
	rateLimitExceeded = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "xg2g",
			Name:      "ratelimit_exceeded_total",
			Help:      "Total rate limit rejections",
		},
		[]string{"limit_type", "mode"},
	)
)

// Config holds rate limiting configuration
type Config struct {
	// Global limits
	GlobalRate  rate.Limit // requests per second
	GlobalBurst int        // max burst size

	// Per-IP limits
	PerIPRate  rate.Limit
	PerIPBurst int

	// Per-Mode limits (Mode 1: standard, Mode 2: audio_proxy, Mode 3: gpu)
	ModeRates map[string]rate.Limit
	ModeBurst map[string]int

	// Cleanup interval for per-IP limiters
	CleanupInterval time.Duration
}

// DefaultConfig returns sensible defaults
func DefaultConfig() Config {
	return Config{
		GlobalRate:  100, // 100 req/s globally
		GlobalBurst: 200, // burst up to 200

		PerIPRate:  10, // 10 req/s per IP
		PerIPBurst: 20, // burst up to 20

		ModeRates: map[string]rate.Limit{
			"standard":    50, // Mode 1: 50 req/s (lowest overhead)
			"audio_proxy": 30, // Mode 2: 30 req/s (AAC remux overhead)
			"gpu":         20, // Mode 3: 20 req/s (GPU bottleneck)
		},
		ModeBurst: map[string]int{
			"standard":    100,
			"audio_proxy": 60,
			"gpu":         40,
		},

		CleanupInterval: 5 * time.Minute,
	}
}

// Limiter manages rate limiting for streams
type Limiter struct {
	config Config

	global  *rate.Limiter
	perIP   map[string]*rate.Limiter
	perMode map[string]*rate.Limiter
	mu      sync.RWMutex

	lastCleanup time.Time
}

// New creates a new rate limiter with the given config
func New(config Config) *Limiter {
	l := &Limiter{
		config:      config,
		global:      rate.NewLimiter(config.GlobalRate, config.GlobalBurst),
		perIP:       make(map[string]*rate.Limiter),
		perMode:     make(map[string]*rate.Limiter),
		lastCleanup: time.Now(),
	}

	// Initialize per-mode limiters
	for mode, modeRate := range config.ModeRates {
		burst := config.ModeBurst[mode]
		l.perMode[mode] = rate.NewLimiter(modeRate, burst)
	}

	return l
}

// Allow checks if a request is allowed under rate limits
// Returns true if allowed, false if rate limited
func (l *Limiter) Allow(clientIP, mode string) bool {
	// 1. Check global limit
	if !l.global.Allow() {
		rateLimitExceeded.WithLabelValues("global", mode).Inc()
		return false
	}

	// 2. Check per-mode limit
	l.mu.RLock()
	modeLimiter, exists := l.perMode[mode]
	l.mu.RUnlock()

	if exists && !modeLimiter.Allow() {
		rateLimitExceeded.WithLabelValues("per_mode", mode).Inc()
		return false
	}

	// 3. Check per-IP limit
	ipLimiter := l.getIPLimiter(clientIP)
	if !ipLimiter.Allow() {
		rateLimitExceeded.WithLabelValues("per_ip", mode).Inc()
		return false
	}

	// Periodic cleanup of stale IP limiters
	l.maybeCleanup()

	return true
}

// getIPLimiter returns the rate limiter for a specific IP
func (l *Limiter) getIPLimiter(ip string) *rate.Limiter {
	l.mu.Lock()
	defer l.mu.Unlock()

	limiter, exists := l.perIP[ip]
	if !exists {
		limiter = rate.NewLimiter(l.config.PerIPRate, l.config.PerIPBurst)
		l.perIP[ip] = limiter
	}

	return limiter
}

// maybeCleanup removes stale IP limiters if cleanup interval has passed
func (l *Limiter) maybeCleanup() {
	if time.Since(l.lastCleanup) < l.config.CleanupInterval {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// Clear all IP limiters (simple approach)
	// Alternative: Track last access time and only remove stale entries
	l.perIP = make(map[string]*rate.Limiter)
	l.lastCleanup = time.Now()
}

// GetClientIP extracts the real client IP from the request
func GetClientIP(r *http.Request) string {
	// Check X-Forwarded-For header (reverse proxy)
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		// X-Forwarded-For can contain multiple IPs: "client, proxy1, proxy2"
		// Take the first one (original client)
		if idx := findComma(xff); idx > 0 {
			xff = xff[:idx]
		}
		xff = trimSpace(xff)
		if xff != "" {
			return xff
		}
	}

	// Check X-Real-IP header (some proxies)
	xri := r.Header.Get("X-Real-IP")
	if xri != "" {
		return xri
	}

	// Fallback to RemoteAddr
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// findComma returns the index of the first comma in the string
func findComma(s string) int {
	for i, c := range s {
		if c == ',' {
			return i
		}
	}
	return -1
}

// trimSpace removes leading and trailing whitespace
func trimSpace(s string) string {
	start := 0
	end := len(s)
	for start < end && s[start] == ' ' {
		start++
	}
	for end > start && s[end-1] == ' ' {
		end--
	}
	return s[start:end]
}
