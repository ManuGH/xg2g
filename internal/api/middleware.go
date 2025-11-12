// SPDX-License-Identifier: MIT

// Package api provides HTTP server functionality for the xg2g application.
package api

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/time/rate"

	"github.com/ManuGH/xg2g/internal/api/middleware"
	"github.com/ManuGH/xg2g/internal/log"
)

var (
	trustedCIDRs     []*net.IPNet
	trustedCIDRsOnce sync.Once
)

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (sr *statusRecorder) WriteHeader(code int) {
	sr.status = code
	sr.ResponseWriter.WriteHeader(code)
}

func loadTrustedCIDRs() {
	trustedCIDRsOnce.Do(func() {
		csv := os.Getenv("XG2G_TRUSTED_PROXIES")
		if csv == "" {
			return
		}
		for _, part := range strings.Split(csv, ",") {
			p := strings.TrimSpace(part)
			if p == "" {
				continue
			}
			if _, ipnet, err := net.ParseCIDR(p); err == nil {
				trustedCIDRs = append(trustedCIDRs, ipnet)
			}
		}
	})
}

func remoteIsTrusted(remote string) bool {
	loadTrustedCIDRs()
	if len(trustedCIDRs) == 0 {
		return false
	}
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, n := range trustedCIDRs {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

type visitor struct {
	lim      *rate.Limiter
	lastSeen time.Time
}

// RateLimitAuditor provides audit logging for rate limit events
type RateLimitAuditor interface {
	RateLimitExceeded(remoteAddr, endpoint string)
}

type rateLimiter struct {
	visitors     map[string]*visitor
	mtx          sync.RWMutex
	rate         rate.Limit
	burst        int
	enabled      bool
	whitelistIPs []string
	auditLogger  RateLimitAuditor // Optional: for audit logging
}

func newRateLimiter(r rate.Limit, b int) *rateLimiter {
	rl := &rateLimiter{
		visitors: make(map[string]*visitor),
		rate:     r,
		burst:    b,
		enabled:  true,
	}
	go rl.janitor(1*time.Hour, 10*time.Minute)
	return rl
}

func (rl *rateLimiter) janitor(maxIdle time.Duration, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		cutoff := time.Now().Add(-maxIdle)
		rl.mtx.Lock()
		for ip, v := range rl.visitors {
			if v.lastSeen.Before(cutoff) {
				delete(rl.visitors, ip)
			}
		}
		rl.mtx.Unlock()
	}
}

// clientIP determines the originating IP address (X-Forwarded-For / X-Real-IP / RemoteAddr)
func clientIP(r *http.Request) string {
	// Only trust proxy headers if the direct peer is in XG2G_TRUSTED_PROXIES
	if remoteIsTrusted(r.RemoteAddr) {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			ip := strings.TrimSpace(parts[0])
			if ip != "" {
				return ip
			}
		}
		if xr := r.Header.Get("X-Real-IP"); xr != "" {
			return xr
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	return r.RemoteAddr
}

func (rl *rateLimiter) getLimiter(ip string) *rate.Limiter {
	rl.mtx.Lock()
	defer rl.mtx.Unlock()

	v, exists := rl.visitors[ip]
	if !exists {
		v = &visitor{lim: rate.NewLimiter(rl.rate, rl.burst)}
		rl.visitors[ip] = v
	}
	v.lastSeen = time.Now()
	return v.lim
}

// rateLimitMiddleware limits the number of requests per IP
func (rl *rateLimiter) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !rl.enabled {
			next.ServeHTTP(w, r)
			return
		}
		ip := clientIP(r)
		// Whitelist check
		for _, wip := range rl.whitelistIPs {
			if wip == ip {
				next.ServeHTTP(w, r)
				return
			}
		}
		limiter := rl.getLimiter(ip)

		// Best-effort RateLimit headers (limit per second, remaining tokens now)
		w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%g/s", float64(rl.rate)))
		w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%.0f", limiter.Tokens()))

		if !limiter.Allow() {
			// Audit log: rate limit exceeded
			if rl.auditLogger != nil {
				rl.auditLogger.RateLimitExceeded(ip, r.URL.Path)
			}

			w.Header().Set("Retry-After", "1")
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip metrics middleware for /metrics endpoint to avoid interference
		if r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}

		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()

		next.ServeHTTP(recorder, r)

		duration := time.Since(start)
		// Ensure Prometheus labels are valid UTF-8; replace invalid bytes
		pathLabel := r.URL.Path
		if !utf8.ValidString(pathLabel) {
			pathLabel = strings.ToValidUTF8(pathLabel, "�")
		}
		recordHTTPMetric(pathLabel, recorder.status)

		// Log metrics with context if available
		logger := log.WithComponentFromContext(r.Context(), "api")
		logger.Debug().
			Str("event", "http.metrics").
			Str("path", pathLabel).
			Int("status", recorder.status).
			Int64("duration_ms", duration.Milliseconds()).
			Msg("http metrics recorded")
	})
}

// panicRecoveryMiddleware ensures that panics inside any downstream handler
// do not crash the process. It logs the panic with context and returns a 500 JSON.
func panicRecoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				// Build stack trace
				buf := make([]byte, 8192)
				n := runtime.Stack(buf, false)
				stack := string(buf[:n])

				// Correlate with request ID if present
				reqID := log.RequestIDFromContext(r.Context())

				// Sanitize path label for metrics/logging
				pathLabel := r.URL.Path
				if !utf8.ValidString(pathLabel) {
					pathLabel = strings.ToValidUTF8(pathLabel, "�")
				}

				// Log with structured fields
				logger := log.WithComponentFromContext(r.Context(), "panic-recovery")
				logger.Error().
					Str("event", "panic.recovered").
					Str("method", r.Method).
					Str("path", pathLabel).
					Str("remote_addr", clientIP(r)).
					Str("request_id", reqID).
					Interface("panic_value", rec).
					Str("stack_trace", stack).
					Msg("panic recovered in HTTP handler")

				// Record metric
				recordHTTPPanic(pathLabel)

				// Best-effort JSON error response
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error":      "Internal server error",
					"request_id": reqID,
					"message":    "An unexpected error occurred. Please try again later.",
				})
			}
		}()

		next.ServeHTTP(w, r)
	})
}

// corsMiddleware adds CORS headers with strict origin validation
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		// Strict whitelist: exact match with common dev ports
		allowedOrigins := map[string]bool{
			"http://localhost:3000":  true,
			"http://localhost:8080":  true,
			"http://localhost:5173":  true, // Vite default
			"https://localhost:3000": true,
			"https://localhost:8080": true,
			"http://127.0.0.1:3000":  true,
			"http://127.0.0.1:8080":  true,
			"https://127.0.0.1:3000": true,
			"https://127.0.0.1:8080": true,
		}

		if origin != "" && allowedOrigins[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		} else if origin == "" {
			// Allow direct API access (curl, tests, same-origin)
			// This is safe for APIs that don't rely on cookies
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}
		// If origin is present but not allowed, no CORS header is set
		// Browser will block the response

		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Request-ID, X-API-Token")
		w.Header().Set("Access-Control-Max-Age", "600")
		w.Header().Set("Vary", "Origin, Access-Control-Request-Method, Access-Control-Request-Headers")

		if r.Method == http.MethodOptions {
			w.Header().Set("Allow", "GET, POST, OPTIONS")
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// requestIDMiddleware generates or uses existing X-Request-ID header and propagates it through context and response
func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Get request ID from header or generate new one
		reqID := r.Header.Get("X-Request-ID")
		if reqID == "" {
			reqID = uuid.New().String()
		}

		// Add request ID to context
		ctx := log.ContextWithRequestID(r.Context(), reqID)

		// Set response header
		w.Header().Set("X-Request-ID", reqID)

		// Create logger with request ID and component
		logger := log.WithComponentFromContext(ctx, "api")
		start := time.Now()

		next.ServeHTTP(w, r.WithContext(ctx))

		// Log request completion with standardized fields
		duration := time.Since(start).Milliseconds()
		logger.Info().
			Str("event", "request.complete").
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Int64("duration_ms", duration).
			Str("remote_addr", clientIP(r)).
			// Duplicate request ID under key req_id for compatibility with other log middleware/tests
			Str("req_id", reqID).
			Msg("request completed")
	})
}

// securityHeaders sets common security headers for API responses
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cross-Origin-Resource-Policy", "same-site")
		// HSTS only sensible via HTTPS; harmless when HTTP
		w.Header().Set("Strict-Transport-Security", "max-age=15552000; includeSubDomains")
		next.ServeHTTP(w, r)
	})
}

// chain applies middlewares in order left→right
func chain(h http.Handler, mws ...func(http.Handler) http.Handler) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

func withMiddlewares(h http.Handler, auditLogger ...RateLimitAuditor) http.Handler {
	// Make rate limiter configurable via environment
	rpsStr := os.Getenv("XG2G_RATELIMIT_RPS")
	burstStr := os.Getenv("XG2G_RATELIMIT_BURST")
	enabledStr := os.Getenv("XG2G_RATELIMIT_ENABLED")
	whitelistStr := os.Getenv("XG2G_RATELIMIT_WHITELIST")

	rps := 10.0
	if rpsStr != "" {
		if v, err := strconv.ParseFloat(rpsStr, 64); err == nil && v > 0 {
			rps = v
		}
	}
	burst := 20
	if burstStr != "" {
		if v, err := strconv.Atoi(burstStr); err == nil && v > 0 {
			burst = v
		}
	}
	rl := newRateLimiter(rate.Limit(rps), burst)
	rl.enabled = strings.ToLower(enabledStr) != "false"
	if whitelistStr != "" {
		parts := strings.Split(whitelistStr, ",")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				rl.whitelistIPs = append(rl.whitelistIPs, p)
			}
		}
	}

	// Set audit logger if provided
	if len(auditLogger) > 0 {
		rl.auditLogger = auditLogger[0]
	}

	// Order matters:
	// 1. Panic recovery first (catch all panics)
	// 2. OTel tracing (creates root span, must be early for trace context propagation)
	// 3. Request ID middleware (uses trace_id if available)
	// 4. Metrics (can use trace context)
	// 5. CORS, security headers, rate limiting
	otelMiddleware := middleware.OTelHTTP("xg2g-api")
	return chain(h, panicRecoveryMiddleware, otelMiddleware, requestIDMiddleware, metricsMiddleware, corsMiddleware, securityHeaders, rl.middleware)
}
