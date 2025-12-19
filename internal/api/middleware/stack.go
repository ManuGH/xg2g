// SPDX-License-Identifier: MIT

package middleware

import (
	xglog "github.com/ManuGH/xg2g/internal/log"
	"github.com/go-chi/chi/v5"
)

// StackConfig configures the canonical HTTP ingress middleware stack.
// It is used by both the API server and the proxy server to prevent drift in cross-cutting concerns.
type StackConfig struct {
	// CORS
	EnableCORS     bool
	AllowedOrigins []string

	// Security headers
	EnableSecurityHeaders bool
	CSP                   string

	// Observability
	EnableMetrics  bool
	TracingService string // empty disables tracing
	EnableLogging  bool

	// Rate limiting (API)
	EnableRateLimit    bool
	RateLimitEnabled   bool
	RateLimitGlobalRPS int
	RateLimitBurst     int
	RateLimitWhitelist []string
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
	// 3. CORS (so OPTIONS and browser clients behave)
	if cfg.EnableCORS {
		r.Use(CORS(cfg.AllowedOrigins))
	}
	// 4. Security headers
	if cfg.EnableSecurityHeaders {
		r.Use(SecurityHeaders(cfg.CSP))
	}
	// 5. Metrics (track all requests)
	if cfg.EnableMetrics {
		r.Use(Metrics())
	}
	// 6. Tracing (distributed tracing with OpenTelemetry)
	if cfg.TracingService != "" {
		r.Use(Tracing(cfg.TracingService))
	}
	// 7. Logging (wraps handlers, captures full latency)
	if cfg.EnableLogging {
		r.Use(xglog.Middleware())
	}
	// 8. Rate limit (global protection)
	if cfg.EnableRateLimit {
		r.Use(APIRateLimit(cfg.RateLimitEnabled, cfg.RateLimitGlobalRPS, cfg.RateLimitBurst, cfg.RateLimitWhitelist))
	}
}
