// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/authz"
	"github.com/ManuGH/xg2g/internal/control/middleware"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/problemcode"
)

const exposureRateLimitWindow = time.Minute

const (
	exposureAuditEventV1       = "security.exposure.audit.v1"
	exposureRateLimitedEventV1 = "security.exposure.rate_limited.v1"
	exposureAuditSchemaV1      = "xg2g.public_exposure.v1"
)

type exposureRateLimiter struct {
	mu      sync.Mutex
	windows map[string]exposureWindow
	now     func() time.Time
}

type exposureWindow struct {
	ResetAt time.Time
	Count   int
}

func newExposureRateLimiter() *exposureRateLimiter {
	return &exposureRateLimiter{
		windows: make(map[string]exposureWindow),
		now:     time.Now,
	}
}

func (l *exposureRateLimiter) allow(key string, limit int, window time.Duration) (bool, time.Duration) {
	if l == nil || limit <= 0 || window <= 0 {
		return true, 0
	}

	now := l.now().UTC()
	l.mu.Lock()
	defer l.mu.Unlock()

	entry := l.windows[key]
	if entry.ResetAt.IsZero() || !now.Before(entry.ResetAt) {
		l.windows[key] = exposureWindow{ResetAt: now.Add(window), Count: 1}
		l.cleanupLocked(now)
		return true, 0
	}
	if entry.Count >= limit {
		return false, time.Until(entry.ResetAt)
	}
	entry.Count++
	l.windows[key] = entry
	return true, 0
}

func (l *exposureRateLimiter) cleanupLocked(now time.Time) {
	for key, entry := range l.windows {
		if !entry.ResetAt.IsZero() && !now.Before(entry.ResetAt) {
			delete(l.windows, key)
		}
	}
}

func (s *Server) ExposureSecurityMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		operationID, _ := r.Context().Value(operationIDKey).(string)
		policy, _ := r.Context().Value(exposurePolicyKey).(authz.ExposurePolicy)
		applyExposureResponsePolicy(w, policy)

		if policy.RateLimitClass != "" && policy.HasDedicatedRateLimit() {
			allowed, retryAfter := s.allowExposureRequest(r, operationID, policy)
			if !allowed {
				s.logExposureRateLimit(r, operationID, policy)
				retryAfterSeconds := int(math.Ceil(retryAfter.Seconds()))
				if retryAfterSeconds < 1 {
					retryAfterSeconds = 1
				}
				w.Header().Set("Retry-After", strconv.Itoa(retryAfterSeconds))
				w.Header().Set("X-RateLimit-Class", string(policy.RateLimitClass))
				writeRegisteredProblem(w, r, http.StatusTooManyRequests, "security/rate_limited", "Rate Limit Exceeded", problemcode.CodeRateLimitExceeded, "Too many requests for this endpoint exposure class.", map[string]any{
					"operationId":    operationID,
					"exposureClass":  string(policy.Class),
					"rateLimitClass": string(policy.RateLimitClass),
				})
				return
			}
		}

		if !policy.AuditRequired {
			next.ServeHTTP(w, r)
			return
		}

		wrapped, tracker := wrapResponseWriter(w)
		started := time.Now()
		next.ServeHTTP(wrapped, r)
		status := http.StatusOK
		if statusTracker, ok := tracker.(StatusTracker); ok {
			status = statusTracker.StatusCode()
		}
		s.logExposureAudit(r, operationID, policy, status, time.Since(started))
	})
}

func (s *Server) allowExposureRequest(r *http.Request, operationID string, policy authz.ExposurePolicy) (bool, time.Duration) {
	cfg := s.GetConfig()
	if !cfg.RateLimitEnabled || s.exposureLimiter == nil || s.exposureRateLimitWhitelisted(r, cfg) {
		return true, 0
	}
	limit := exposureLimit(policy.RateLimitClass, cfg)
	if limit <= 0 {
		return true, 0
	}
	key := strings.Join([]string{string(policy.RateLimitClass), operationID, s.exposureClientKey(r, cfg)}, "|")
	return s.exposureLimiter.allow(key, limit, exposureRateLimitWindow)
}

func exposureLimit(class authz.ExposureRateLimitClass, cfg config.AppConfig) int {
	authLimit := cfg.RateLimitAuth
	if authLimit <= 0 {
		authLimit = 10
	}
	switch class {
	case authz.ExposureRateLimitPairingPoll:
		return maxInt(authLimit*6, 60)
	case authz.ExposureRateLimitAuth,
		authz.ExposureRateLimitPairingStart,
		authz.ExposureRateLimitPairingSecret,
		authz.ExposureRateLimitDeviceGrant,
		authz.ExposureRateLimitBootstrap:
		return authLimit
	default:
		return 0
	}
}

func (s *Server) exposureRateLimitWhitelisted(r *http.Request, cfg config.AppConfig) bool {
	entries := splitCSVNonEmpty(strings.Join(cfg.RateLimitWhitelist, ","))
	if len(entries) == 0 {
		return false
	}
	allowed, err := middleware.ParseCIDRs(entries)
	if err != nil {
		return false
	}
	ip := net.ParseIP(s.exposureClientKey(r, cfg))
	return ip != nil && middleware.IsIPAllowed(ip, allowed)
}

func (s *Server) exposureClientKey(r *http.Request, cfg config.AppConfig) string {
	remoteIP := requestRemoteIP(r)
	if remoteIP == nil {
		return "unknown"
	}

	trusted, err := middleware.ParseCIDRs(splitCSVNonEmpty(cfg.TrustedProxies))
	if err != nil || !middleware.IsIPAllowed(remoteIP, trusted) {
		return remoteIP.String()
	}

	for _, candidate := range forwardedForIPs(r.Header.Get("X-Forwarded-For")) {
		if !middleware.IsIPAllowed(candidate, trusted) {
			return candidate.String()
		}
	}
	for _, candidate := range forwardedForIPs(r.Header.Get("X-Forwarded-For")) {
		return candidate.String()
	}
	return remoteIP.String()
}

func forwardedForIPs(raw string) []net.IP {
	parts := strings.Split(raw, ",")
	out := make([]net.IP, 0, len(parts))
	for i := len(parts) - 1; i >= 0; i-- {
		candidate := net.ParseIP(strings.TrimSpace(parts[i]))
		if candidate == nil {
			continue
		}
		out = append(out, candidate)
	}
	return out
}

func (s *Server) logExposureRateLimit(r *http.Request, operationID string, policy authz.ExposurePolicy) {
	log.FromContext(r.Context()).Warn().
		Str("event", exposureRateLimitedEventV1).
		Str("audit_schema", exposureAuditSchemaV1).
		Str("severity", "warning").
		Str("operation_id", operationID).
		Str("exposure_class", string(policy.Class)).
		Str("auth_kind", string(policy.AuthKind)).
		Str("rate_limit_class", string(policy.RateLimitClass)).
		Str("browser_trust", string(policy.BrowserTrust)).
		Str("method", r.Method).
		Str("decision", "block").
		Str("reason", "rate_limited").
		Str("request_id", log.RequestIDFromContext(r.Context())).
		Str("client_ip", s.exposureClientKey(r, s.GetConfig())).
		Msg("public exposure rate limit exceeded")
}

func (s *Server) logExposureAudit(r *http.Request, operationID string, policy authz.ExposurePolicy, status int, duration time.Duration) {
	outcome := "ok"
	if status >= 500 {
		outcome = "error"
	} else if status >= 400 {
		outcome = "denied"
	}
	decision := "allow"
	if outcome != "ok" {
		decision = outcome
	}

	log.FromContext(r.Context()).Info().
		Str("event", exposureAuditEventV1).
		Str("audit_schema", exposureAuditSchemaV1).
		Str("severity", exposureAuditSeverity(outcome)).
		Str("operation_id", operationID).
		Str("exposure_class", string(policy.Class)).
		Str("auth_kind", string(policy.AuthKind)).
		Str("rate_limit_class", string(policy.RateLimitClass)).
		Str("browser_trust", string(policy.BrowserTrust)).
		Str("method", r.Method).
		Int("status", status).
		Str("outcome", outcome).
		Str("decision", decision).
		Str("request_id", log.RequestIDFromContext(r.Context())).
		Str("client_ip", s.exposureClientKey(r, s.GetConfig())).
		Dur("duration", duration).
		Msg("security exposure event")
}

func applyExposureResponsePolicy(w http.ResponseWriter, policy authz.ExposurePolicy) {
	if !policy.RequiresNoStore() {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
}

func exposureAuditSeverity(outcome string) string {
	switch outcome {
	case "error":
		return "error"
	case "denied":
		return "warning"
	default:
		return "info"
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
