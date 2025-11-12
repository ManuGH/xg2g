# Tier 1: Rate Limiting Implementation

## Ziel
Production-Grade Rate Limiting für alle 3 Modi zum Schutz vor Überlastung

## Problem
Aktuell kein Rate Limiting:
- Client kann unbegrenzt Requests senden
- GPU Mode 3 kann durch zu viele Streams überlastet werden
- Kein Schutz vor versehentlichem DDoS (z.B. Loop in Client)
- 2.5 Gbps Netzwerk kann bei vielen parallelen Streams gesättigt werden

## Lösung

### 1. Rate Limiter Package

**Datei:** `internal/ratelimit/limiter.go` (neu)

```go
// SPDX-License-Identifier: MIT

package ratelimit

import (
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
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
		GlobalRate:  100,  // 100 req/s globally
		GlobalBurst: 200,  // burst up to 200

		PerIPRate:  10,   // 10 req/s per IP
		PerIPBurst: 20,   // burst up to 20

		ModeRates: map[string]rate.Limit{
			"standard":    50,  // Mode 1: 50 req/s (lowest overhead)
			"audio_proxy": 30,  // Mode 2: 30 req/s (AAC remux overhead)
			"gpu":         20,  // Mode 3: 20 req/s (GPU bottleneck)
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
		return false
	}

	// 2. Check per-mode limit
	l.mu.RLock()
	modeLimiter, exists := l.perMode[mode]
	l.mu.RUnlock()

	if exists && !modeLimiter.Allow() {
		return false
	}

	// 3. Check per-IP limit
	ipLimiter := l.getIPLimiter(clientIP)
	if !ipLimiter.Allow() {
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
		ips := parseXForwardedFor(xff)
		if len(ips) > 0 {
			return ips[0]
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

// parseXForwardedFor parses the X-Forwarded-For header
func parseXForwardedFor(xff string) []string {
	var ips []string
	for _, ip := range splitCommas(xff) {
		ip = trimSpace(ip)
		if ip != "" {
			ips = append(ips, ip)
		}
	}
	return ips
}

func splitCommas(s string) []string {
	// Simple CSV split
	var result []string
	current := ""
	for _, c := range s {
		if c == ',' {
			result = append(result, current)
			current = ""
		} else {
			current += string(c)
		}
	}
	if current != "" {
		result = append(result, current)
	}
	return result
}

func trimSpace(s string) string {
	// Simple trim
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
```

### 2. HTTP Middleware

**Datei:** `internal/api/middleware/ratelimit.go` (neu)

```go
// SPDX-License-Identifier: MIT

package middleware

import (
	"net/http"
	"strconv"
	"time"

	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/ratelimit"
)

// RateLimitMiddleware returns a middleware that enforces rate limits
func RateLimitMiddleware(limiter *ratelimit.Limiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			clientIP := ratelimit.GetClientIP(r)
			mode := detectMode(r)

			if !limiter.Allow(clientIP, mode) {
				logger := log.FromContext(r.Context())
				logger.Warn().
					Str("event", "rate_limit.exceeded").
					Str("client_ip", clientIP).
					Str("mode", mode).
					Str("path", r.URL.Path).
					Msg("rate limit exceeded")

				// Set rate limit headers
				w.Header().Set("X-RateLimit-Limit", "varies by mode")
				w.Header().Set("X-RateLimit-Remaining", "0")
				w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(time.Second).Unix(), 10))
				w.Header().Set("Retry-After", "1")

				http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// detectMode determines the mode based on request path/headers
func detectMode(r *http.Request) string {
	// Mode detection logic:
	// - Port 18000 = audio_proxy or gpu
	// - Port 8080 = standard
	// - Check X-Transcode header for gpu mode

	if r.Header.Get("X-Transcode") == "gpu" {
		return "gpu"
	}

	// Simple heuristic: audio proxy uses port 18000
	if r.Host == ":18000" || r.URL.Port() == "18000" {
		return "audio_proxy"
	}

	return "standard"
}
```

### 3. Integration in API Server

**Datei:** `internal/api/http.go` (erweitern)

```go
import (
	"github.com/ManuGH/xg2g/internal/ratelimit"
	apimiddleware "github.com/ManuGH/xg2g/internal/api/middleware"
)

// In NewServer():
func NewServer(deps Dependencies) (*Server, error) {
	// ... existing setup

	// Initialize rate limiter
	rateLimitConfig := ratelimit.DefaultConfig()
	// Override from environment if needed
	if os.Getenv("XG2G_RATE_LIMIT_GLOBAL") != "" {
		// Parse and set custom limits
	}
	rateLimiter := ratelimit.New(rateLimitConfig)

	// Add rate limit middleware
	mux := http.NewServeMux()
	handler := apimiddleware.RateLimitMiddleware(rateLimiter)(mux)

	// ... rest of setup
}
```

### 4. Environment Variables

**Datei:** `internal/config/config.go` (erweitern)

```go
type RateLimitConfig struct {
	Enabled     bool              `yaml:"enabled"`
	GlobalRate  float64           `yaml:"globalRate"`
	GlobalBurst int               `yaml:"globalBurst"`
	PerIPRate   float64           `yaml:"perIPRate"`
	PerIPBurst  int               `yaml:"perIPBurst"`
	ModeRates   map[string]float64 `yaml:"modeRates"`
}

// Environment variables:
// XG2G_RATE_LIMIT_ENABLED=true
// XG2G_RATE_LIMIT_GLOBAL_RATE=100
// XG2G_RATE_LIMIT_GLOBAL_BURST=200
// XG2G_RATE_LIMIT_PER_IP_RATE=10
// XG2G_RATE_LIMIT_PER_IP_BURST=20
// XG2G_RATE_LIMIT_MODE_STANDARD=50
// XG2G_RATE_LIMIT_MODE_AUDIO_PROXY=30
// XG2G_RATE_LIMIT_MODE_GPU=20
```

### 5. Tests

**Datei:** `internal/ratelimit/limiter_test.go` (neu)

```go
func TestRateLimiter(t *testing.T) {
	config := Config{
		GlobalRate:  10,
		GlobalBurst: 20,
		PerIPRate:   5,
		PerIPBurst:  10,
		ModeRates: map[string]rate.Limit{
			"gpu": 2,
		},
		ModeBurst: map[string]int{
			"gpu": 4,
		},
	}
	limiter := New(config)

	// Test per-IP rate limiting
	allowed := 0
	for i := 0; i < 100; i++ {
		if limiter.Allow("192.168.1.100", "standard") {
			allowed++
		}
	}
	assert.True(t, allowed < 20, "should enforce per-IP rate limit")

	// Test per-mode rate limiting (GPU)
	allowed = 0
	for i := 0; i < 100; i++ {
		if limiter.Allow("192.168.1.101", "gpu") {
			allowed++
		}
	}
	assert.True(t, allowed < 10, "should enforce GPU mode rate limit")
}

func TestGetClientIP(t *testing.T) {
	tests := []struct {
		name     string
		headers  map[string]string
		remoteAddr string
		want     string
	}{
		{
			name:     "X-Forwarded-For single IP",
			headers:  map[string]string{"X-Forwarded-For": "203.0.113.1"},
			remoteAddr: "192.168.1.1:12345",
			want:     "203.0.113.1",
		},
		{
			name:     "X-Forwarded-For multiple IPs",
			headers:  map[string]string{"X-Forwarded-For": "203.0.113.1, 192.168.1.1, 10.0.0.1"},
			remoteAddr: "127.0.0.1:12345",
			want:     "203.0.113.1",
		},
		{
			name:     "X-Real-IP",
			headers:  map[string]string{"X-Real-IP": "203.0.113.2"},
			remoteAddr: "192.168.1.1:12345",
			want:     "203.0.113.2",
		},
		{
			name:     "Fallback to RemoteAddr",
			remoteAddr: "192.168.1.100:54321",
			want:     "192.168.1.100",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", "/", nil)
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			req.RemoteAddr = tt.remoteAddr

			got := GetClientIP(req)
			assert.Equal(t, tt.want, got)
		})
	}
}
```

### 6. Monitoring

**Extend:** `internal/metrics/business.go`

```go
var (
	rateLimitExceeded = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "xg2g_rate_limit_exceeded_total",
			Help: "Total number of rate-limited requests",
		},
		[]string{"mode", "limit_type"}, // limit_type: "global|per_ip|per_mode"
	)
)

// In RateLimitMiddleware:
if !limiter.Allow(clientIP, mode) {
	metrics.rateLimitExceeded.WithLabelValues(mode, "per_ip").Inc()
	// ...
}
```

## Docker Compose Integration

```yaml
# docker-compose.yml
services:
  xg2g:
    environment:
      - XG2G_RATE_LIMIT_ENABLED=true
      - XG2G_RATE_LIMIT_GLOBAL_RATE=100
      - XG2G_RATE_LIMIT_PER_IP_RATE=10
      - XG2G_RATE_LIMIT_MODE_GPU=20
```

## Success Criteria

- ✅ Rate limiting funktioniert für alle 3 Modi
- ✅ 429 Too Many Requests bei Überschreitung
- ✅ Retry-After Header korrekt gesetzt
- ✅ Metrics zeigen Rate-Limited Requests
- ✅ Per-IP Cleanup verhindert Memory Leak
- ✅ Tests mit >95% Coverage
