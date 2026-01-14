// Package testutil provides test utilities for the api package.
package testutil

import (
	"net"
	"net/http"
	"strings"
)

// trustedIPNets is exposed for testing purposes only.
// In production, this is populated via SetTrustedProxies in the api package.
var trustedIPNets []*net.IPNet

// SetTrustedIPNets allows tests to configure trusted IP networks.
func SetTrustedIPNets(nets []*net.IPNet) {
	trustedIPNets = nets
}

// RemoteIsTrusted checks if the remote IP is trusted.
// This is a test helper that replicates production logic for testing.
func RemoteIsTrusted(remote string) bool {
	if len(trustedIPNets) == 0 {
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
	for _, n := range trustedIPNets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// ClientIP determines the originating IP address from the request.
// This is a test helper that replicates production IP extraction logic.
func ClientIP(r *http.Request) string {
	if RemoteIsTrusted(r.RemoteAddr) {
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
