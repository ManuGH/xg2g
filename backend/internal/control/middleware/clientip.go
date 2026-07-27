// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package middleware

import (
	"net"
	"net/http"
	"slices"
	"strings"
)

// ParseCIDRs converts a list of CIDR blocks or bare IP addresses into IPNets.
// A bare IP becomes a host route (/32 or /128). Empty entries are skipped.
func ParseCIDRs(cidrs []string) ([]*net.IPNet, error) {
	var nets []*net.IPNet
	for _, c := range cidrs {
		trimmed := strings.TrimSpace(c)
		if trimmed == "" {
			continue
		}
		if _, n, err := net.ParseCIDR(trimmed); err == nil {
			nets = append(nets, n)
			continue
		}

		ip := net.ParseIP(trimmed)
		if ip == nil {
			return nil, &net.ParseError{Type: "CIDR or IP address", Text: trimmed}
		}
		bits := 32
		if ip.To4() == nil {
			bits = 128
		}
		nets = append(nets, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
	}
	return nets, nil
}

// IsIPAllowed reports whether ip falls inside any of the given subnets.
func IsIPAllowed(ip net.IP, subnets []*net.IPNet) bool {
	ip = ip.To16()
	if ip == nil {
		return false
	}
	for _, n := range subnets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// ResolveClientIP determines the originating client IP for a request and is the
// single source of truth for "which IP is this request actually from".
//
// The trust model is deliberately strict, because getting this wrong in either
// direction is a real vulnerability:
//
//   - Trusting X-Forwarded-For unconditionally lets any client pick its own
//     identity — it can evade rate limits and forge audit trails.
//   - Ignoring X-Forwarded-For entirely collapses every client behind a reverse
//     proxy onto the proxy's IP, so one abusive client rate-limits everyone else.
//
// The rules:
//
//  1. Start from the socket peer (RemoteAddr). If it cannot be parsed, give up
//     and return nil — a caller must treat that as "unidentifiable".
//  2. If no trusted proxies are configured, or the peer is not one of them,
//     return the peer and ignore all forwarding headers. A direct client cannot
//     talk its way into a different identity.
//  3. Otherwise walk X-Forwarded-For right-to-left. Each entry that is itself a
//     trusted proxy is another hop and is skipped; the first entry that is not
//     ends the trusted chain and IS the client.
//  4. An unparseable entry breaks the chain: everything to its left was appended
//     by something we can no longer vouch for, so fall back to the socket peer.
//  5. Only if the walk runs out of entries — i.e. every entry including the
//     leftmost is configured trusted infrastructure — does the header name no
//     client at all, and the socket peer is the conservative answer.
//
// Rule 5 is narrow, and worth being precise about because getting it wrong
// reintroduces the very collapse this function exists to prevent. A chain whose
// intermediate hops are all trusted is the NORMAL case for a CDN deployment:
//
//	Client 203.0.113.50 -> Cloudflare 198.51.100.20 -> Nginx 10.0.0.10 -> xg2g
//	RemoteAddr:      10.0.0.10        (trusted)
//	X-Forwarded-For: 203.0.113.50, 198.51.100.20
//
// Here rule 3 fires, not rule 5: the walk skips Cloudflare and stops at
// 203.0.113.50, because that entry is not trusted infrastructure. Rule 5 would
// only apply if 203.0.113.50 were itself in the trusted list. Returning the peer
// for the chain above would put every user of the deployment back into one
// Nginx-shaped bucket. See TestResolveClientIP_MultiHopCDNChain.
//
// X-Real-IP is intentionally NOT consulted. It carries a single value with no
// chain, so it cannot be validated hop-by-hop: a proxy that appends to
// X-Forwarded-For but passes a client-supplied X-Real-IP through unchanged would
// hand every client a free identity of its choosing.
//
// Operator caveat inherent to any trusted-hop model: an entry vouched for by a
// trusted proxy is only as good as that proxy's own hygiene. A proxy that
// appends to X-Forwarded-For without sanitizing what the client sent lets a
// client prepend an arbitrary IP and be identified as it — so only put proxies
// that normalize the header into TrustedProxies.
func ResolveClientIP(r *http.Request, trustedProxies []*net.IPNet) net.IP {
	if r == nil {
		return nil
	}

	peer := remoteAddrIP(r.RemoteAddr)
	if peer == nil {
		return nil
	}

	// Rule 2: forwarding headers are only meaningful from a trusted peer.
	if len(trustedProxies) == 0 || !IsIPAllowed(peer, trustedProxies) {
		return peer
	}

	xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For"))
	if xff == "" {
		return peer
	}

	// Rules 3-4: right-to-left along the trusted chain.
	for _, raw := range slices.Backward(strings.Split(xff, ",")) {
		hop := net.ParseIP(strings.TrimSpace(raw))
		if hop == nil {
			// Rule 4: chain integrity is gone, trust only the socket.
			return peer
		}
		if !IsIPAllowed(hop, trustedProxies) {
			return hop
		}
	}

	// Rule 5: the whole chain was proxies we trust.
	return peer
}

// remoteAddrIP parses the IP out of a "host:port" RemoteAddr, tolerating a bare
// host (httptest and some transports omit the port).
func remoteAddrIP(remoteAddr string) net.IP {
	addr := strings.TrimSpace(remoteAddr)
	if addr == "" {
		return nil
	}
	if host, _, err := net.SplitHostPort(addr); err == nil {
		addr = host
	}
	return net.ParseIP(strings.Trim(addr, "[]"))
}

// ClientIPKey renders an IP as a stable rate-limit bucket key.
//
// IPv4 (including IPv4-mapped IPv6 such as ::ffff:203.0.113.7) collapses to the
// dotted-quad form, so the same host cannot occupy two buckets by switching
// representation. IPv6 is masked to its /64 prefix, because a single subscriber
// is routinely handed a whole /64 and per-address buckets would make the limiter
// trivially evadable. This matches httprate's own canonicalization, so switching
// the key function does not silently change IPv6 bucketing.
func ClientIPKey(ip net.IP) string {
	if ip == nil {
		return "unknown"
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	return ip.Mask(net.CIDRMask(64, 128)).String()
}

// KeyByTrustedProxyChain builds an httprate key function that buckets by the
// resolved client IP rather than by the socket peer. Behind a reverse proxy this
// is the difference between per-client limits and one shared bucket for the
// entire internet.
func KeyByTrustedProxyChain(trustedProxies []*net.IPNet) func(*http.Request) (string, error) {
	return func(r *http.Request) (string, error) {
		return ClientIPKey(ResolveClientIP(r, trustedProxies)), nil
	}
}
