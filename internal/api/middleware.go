package api

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
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

type rateLimiter struct {
	visitors map[string]*visitor
	mtx      sync.RWMutex
	rate     rate.Limit
	burst    int
}

func newRateLimiter(r rate.Limit, b int) *rateLimiter {
	rl := &rateLimiter{
		visitors: make(map[string]*visitor),
		rate:     r,
		burst:    b,
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
		ip := clientIP(r)
		limiter := rl.getLimiter(ip)

		// Best-effort RateLimit headers (limit per second, remaining tokens now)
		w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%g/s", float64(rl.rate)))
		w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%.0f", limiter.Tokens()))

		if !limiter.Allow() {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		recordHTTPMetric(r.URL.Path, recorder.status)
	})
}

// corsMiddleware fügt CORS-Header hinzu
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Max-Age", "600")
		w.Header().Set("Vary", "Origin, Access-Control-Request-Method, Access-Control-Request-Headers")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// securityHeaders setzt gängige Sicherheitsheader für API-Responses
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cross-Origin-Resource-Policy", "same-site")
		// HSTS nur sinnvoll über HTTPS; harmless wenn HTTP
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

func withMiddlewares(h http.Handler) http.Handler {
	rl := newRateLimiter(rate.Limit(10), 20)
	return chain(h, metricsMiddleware, corsMiddleware, securityHeaders, rl.middleware)
}
