package api

import (
	"net"
	"net/http"
	"strings"
	"sync"
)

var (
	trustedIPNets     []*net.IPNet
	trustedIPNetsOnce sync.Once
)

// SetTrustedProxies configures the list of trusted proxies/CIDRs.
// This must be called at startup with configuration values.
func SetTrustedProxies(csv string) {
	trustedIPNetsOnce.Do(func() {
		if csv == "" {
			return
		}
		for _, part := range strings.Split(csv, ",") {
			p := strings.TrimSpace(part)
			if p == "" {
				continue
			}
			if _, ipnet, err := net.ParseCIDR(p); err == nil {
				trustedIPNets = append(trustedIPNets, ipnet)
			}
		}
	})
}

// remoteIsTrusted checks if the remote IP is trusted
func remoteIsTrusted(remote string) bool {
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

// clientIP determines the originating IP address (X-Forwarded-For / X-Real-IP / RemoteAddr)
func clientIP(r *http.Request) string {
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
