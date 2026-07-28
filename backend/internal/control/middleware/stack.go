// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package middleware

import (
	"net"
	"net/http"

	"github.com/ManuGH/xg2g/internal/control/http/deadline"
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
	//
	// TrustedProxies (above) doubles as the limiter's trust boundary: it decides
	// whether a request is bucketed by its socket peer or by the client IP that
	// its proxy chain vouches for. See ResolveClientIP.
	EnableRateLimit    bool
	RateLimitEnabled   bool
	RateLimitGlobalRPS int
	RateLimitWhitelist []string

	// MaxRequestBodyBytes caps the size of inbound request bodies (0 disables).
	MaxRequestBodyBytes int64

	// Route-aware write deadlines. RuntimeDisabled preserves the historical
	// stack exactly; RuntimeEnforced installs the request lifecycle owner.
	DeadlineRuntimeMode RuntimeMode
	DeadlineTimeouts    deadline.DeadlineTimeouts
}

// NewRouter constructs a chi router with the canonical middleware stack applied.
func NewRouter(cfg StackConfig) *chi.Mux {
	r := chi.NewRouter()
	ApplyStack(r, cfg)
	return r
}

// ApplyStack applies the canonical middleware stack to r.
//
// Ordering is load-bearing, in particular around CORS and rate limiting:
//
//   - Security headers and the request ID are stamped before anything can
//     reject a request, so a 429 is as well-formed and as traceable as a 200.
//   - CORS response headers are stamped before both limiters, so a rate-limit
//     rejection reaches the browser as a readable 429 rather than an opaque
//     CORS failure that hides the real cause from the UI.
//   - Preflights are metered by their own generous limiter and answered
//     afterwards. Previously CORS answered OPTIONS and returned at this point in
//     the chain, so preflights never reached the limiter at all — an unmetered
//     path anyone could amplify.
//   - The API limiter stays innermost, metering only what actually continues on
//     to the router, so browser preflight volume cannot consume the budget that
//     real API calls depend on.
//
// If r is a *chi.Mux the preflight responder is bound to it and advertises the
// methods each path genuinely accepts; otherwise it falls back to a static list.
func ApplyStack(r chi.Router, cfg StackConfig) {
	// 1. Route-deadline lifecycle (outermost network writer). It must wrap
	// compression and response capture so their final writes happen before the
	// keep-alive deadline reset.
	if cfg.DeadlineRuntimeMode == RuntimeEnforced {
		r.Use(WriteTimeoutMiddleware(cfg.DeadlineTimeouts, cfg.DeadlineRuntimeMode))
	}
	// 2. Recoverer (outermost application safety net)
	r.Use(Recoverer)
	// 3. RequestID (correlation early)
	r.Use(RequestID)
	// 4. Body-size backstop (bound memory before any handler reads the body)
	if cfg.MaxRequestBodyBytes > 0 {
		r.Use(MaxBodyBytes(cfg.MaxRequestBodyBytes))
	}
	// 5. Compression (response transform; only touches text-ish content types,
	// never media segments, so it sits outside the per-request logic below).
	if cfg.EnableCompression {
		r.Use(Compression())
	}
	// 6. Security headers (before any rejection, so 4xx/5xx carry them too)
	if cfg.EnableSecurityHeaders {
		r.Use(SecurityHeaders(cfg.CSP, cfg.TrustedProxies))
	}
	// 7. CORS response headers (stamp only; never short-circuits)
	if cfg.EnableCORS {
		r.Use(CORSHeaders(cfg.AllowedOrigins, cfg.CORSAllowCredentials))
	}
	// 8. Preflight rate limit (OPTIONS only, separate generous budget)
	if cfg.EnableRateLimit {
		r.Use(OnlyMethod(http.MethodOptions, PreflightRateLimit(
			cfg.RateLimitEnabled, cfg.RateLimitGlobalRPS, cfg.RateLimitWhitelist, cfg.TrustedProxies,
		)))
	}
	// 9. Preflight responder (answers OPTIONS, route-aware where possible)
	if cfg.EnableCORS {
		r.Use(Preflight(routeMatcherFor(r)))
	}
	// 10. CSRF (fail-closed for state-changing requests)
	r.Use(CSRFProtection(cfg.AllowedOrigins))
	// 11. Metrics (track all requests)
	if cfg.EnableMetrics {
		r.Use(Metrics())
	}
	// 12. Tracing (distributed tracing with OpenTelemetry)
	if cfg.TracingService != "" {
		r.Use(Tracing(cfg.TracingService))
	}
	// 13. Logging (wraps handlers, captures full latency)
	if cfg.EnableLogging {
		r.Use(xglog.Middleware())
	}
	// 14. Rate limit (global protection, keyed by resolved client IP)
	if cfg.EnableRateLimit {
		r.Use(APIRateLimit(
			cfg.RateLimitEnabled, cfg.RateLimitGlobalRPS, cfg.RateLimitWhitelist, cfg.TrustedProxies,
		))
	}
}

// routeMatcherFor returns r as a RouteMatcher when it can answer route queries,
// or nil to make the preflight responder fall back to its static method list.
func routeMatcherFor(r chi.Router) RouteMatcher {
	if m, ok := r.(RouteMatcher); ok {
		return m
	}
	return nil
}
