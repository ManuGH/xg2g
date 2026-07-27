// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package middleware

import (
	"net"
	"net/http"

	"github.com/ManuGH/xg2g/internal/log"
)

// LANGuardConfig configures a client-IP allowlist gate.
//
// NOTE: LANGuard is currently exercised only by tests (internal/control/http/v3
// auth_strict_test.go); no production route mounts RequireLAN. Wiring it would
// add a LAN restriction the daemon does not have today, so that decision is
// deliberately left to the operator rather than made here.
type LANGuardConfig struct {
	// AllowedCIDRs defines which CLIENT IPs can access the protected resource.
	// If empty, defaults to RFC1918 ranges plus loopback.
	AllowedCIDRs []string

	// TrustedProxyCIDRs defines which UPSTREAM IPs (RemoteAddr) are trusted to
	// set X-Forwarded-For. If empty, X-Forwarded-For is ALWAYS IGNORED.
	TrustedProxyCIDRs []string
}

type LANGuard struct {
	allowedClient []*net.IPNet
	trustedProxy  []*net.IPNet
}

func NewLANGuard(cfg LANGuardConfig) (*LANGuard, error) {
	clientCIDRs := cfg.AllowedCIDRs
	if len(clientCIDRs) == 0 {
		clientCIDRs = []string{
			"10.0.0.0/8",
			"172.16.0.0/12",
			"192.168.0.0/16",
			"127.0.0.0/8", // Localhost
		}
	}
	allowedClients, err := ParseCIDRs(clientCIDRs)
	if err != nil {
		return nil, err
	}

	trustedProxies, err := ParseCIDRs(cfg.TrustedProxyCIDRs)
	if err != nil {
		return nil, err
	}

	return &LANGuard{allowedClient: allowedClients, trustedProxy: trustedProxies}, nil
}

func (g *LANGuard) RequireLAN(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Client-IP resolution is shared with the rate limiter so the two can
		// never disagree about who a request came from — see ResolveClientIP.
		ip := ResolveClientIP(r, g.trustedProxy)
		logger := log.WithComponentFromContext(r.Context(), "authz.lan")

		if ip == nil {
			logger.Warn().Str("remote_addr", r.RemoteAddr).Msg("failed to parse remote ip")
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		if !IsIPAllowed(ip, g.allowedClient) {
			logger.Warn().Str("ip", ip.String()).Msg("access denied: not in allowed LAN CIDRs")
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
