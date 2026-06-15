// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package middleware

import (
	"net"

	xglog "github.com/ManuGH/xg2g/internal/log"
	"github.com/go-chi/chi/v5"
)

// StackConfig configures the canonical HTTP ingress middleware stack.
// It is used by both the API server and the proxy server to prevent drift in cross-cutting concerns.
type StackConfig struct {
	// CORS
	EnableCORS           bool
	AllowedOrigins       []string
	CORSAllowCredentials bool

	// Security headers
	EnableSecurityHeaders bool
	CSP                   string

	// EnableCompression gzips text-ish responses (HTML/CSS/JS/JSON/SVG + HLS
	// playlists). Media segments are never compressed (see compress.go).
	EnableCompression bool

	// TrustedProxies defines which IPs are trusted to set X-Forwarded-Proto.
	TrustedProxies []*net.IPNet

	// Observability
	EnableMetrics  bool
	TracingService string // empty disables tracing
	EnableLogging  bool

	// Rate limiting (API). No burst field: the API limiter is a window counter (httprate)
	// with no burst capacity; the api.rateLimit.burst knob is deprecated/inert.
	EnableRateLimit    bool
	RateLimitEnabled   bool
	RateLimitGlobalRPS int
	RateLimitWhitelist []string

	// MaxRequestBodyBytes caps the size of inbound request bodies (0 disables).
	MaxRequestBodyBytes int64
}

// NewRouter constructs a chi router with the canonical middleware stack applied.
func NewRouter(cfg StackConfig) *chi.Mux {
	r := chi.NewRouter()
	ApplyStack(r, cfg)
	return r
}

// ApplyStack applies the canonical middleware stack to r.
func ApplyStack(r chi.Router, cfg StackConfig) {
	// 1. Recoverer (outermost safety net)
	r.Use(Recoverer)
	// 2. RequestID (correlation early)
	r.Use(RequestID)
	// 2b. Body-size backstop (bound memory before any handler reads the body)
	if cfg.MaxRequestBodyBytes > 0 {
		r.Use(MaxBodyBytes(cfg.MaxRequestBodyBytes))
	}
	// 2c. Compression (response transform; only touches text-ish content types,
	// never media segments, so it sits outside the per-request logic below).
	if cfg.EnableCompression {
		r.Use(Compression())
	}
	// 3. CORS (so OPTIONS and browser clients behave)
	if cfg.EnableCORS {
		r.Use(CORS(cfg.AllowedOrigins, cfg.CORSAllowCredentials))
	}
	// 4. CSRF (fail-closed for state-changing requests)
	r.Use(CSRFProtection(cfg.AllowedOrigins))
	// 5. Security headers
	if cfg.EnableSecurityHeaders {
		r.Use(SecurityHeaders(cfg.CSP, cfg.TrustedProxies))
	}
	// 6. Metrics (track all requests)
	if cfg.EnableMetrics {
		r.Use(Metrics())
	}
	// 7. Tracing (distributed tracing with OpenTelemetry)
	if cfg.TracingService != "" {
		r.Use(Tracing(cfg.TracingService))
	}
	// 8. Logging (wraps handlers, captures full latency)
	if cfg.EnableLogging {
		r.Use(xglog.Middleware())
	}
	// 9. Rate limit (global protection)
	if cfg.EnableRateLimit {
		r.Use(APIRateLimit(cfg.RateLimitEnabled, cfg.RateLimitGlobalRPS, cfg.RateLimitWhitelist))
	}
}
