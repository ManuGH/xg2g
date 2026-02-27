// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package net

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"golang.org/x/net/idna"
)

var (
	// ErrOutboundDisabled indicates outbound HTTP(S) access is disabled by policy.
	ErrOutboundDisabled = errors.New("outbound http(s) disabled")
	// ErrOutboundNotAllowed indicates the URL did not match the allowlist.
	ErrOutboundNotAllowed = errors.New("outbound url not allowed")
)

// OutboundAllowlist defines the allowed outbound URL components.
type OutboundAllowlist struct {
	Hosts   []string
	CIDRs   []string
	Ports   []int
	Schemes []string
}

// OutboundPolicy defines the outbound access policy.
type OutboundPolicy struct {
	Enabled bool
	Allow   OutboundAllowlist
}

// NormalizeHost validates and normalizes a host for comparison.
func NormalizeHost(raw string) (string, error) {
	host := strings.TrimSpace(raw)
	if host == "" {
		return "", fmt.Errorf("host is empty")
	}
	if strings.Contains(host, "://") {
		return "", fmt.Errorf("host must not include scheme: %s", raw)
	}
	if strings.Contains(host, "/") {
		return "", fmt.Errorf("host must not include path: %s", raw)
	}
	if strings.Contains(host, "@") {
		return "", fmt.Errorf("host must not include userinfo: %s", raw)
	}
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	}
	if strings.Contains(host, ":") && net.ParseIP(host) == nil {
		return "", fmt.Errorf("host must not include port: %s", raw)
	}
	if strings.Contains(host, "%") {
		return "", fmt.Errorf("host must not include zone: %s", raw)
	}
	host = strings.TrimSuffix(host, ".")
	if host == "" {
		return "", fmt.Errorf("host is empty")
	}
	if ip := net.ParseIP(host); ip != nil {
		return strings.ToLower(ip.String()), nil
	}
	ascii, err := idna.Lookup.ToASCII(host)
	if err != nil {
		return "", fmt.Errorf("invalid host %q: %w", raw, err)
	}
	return strings.ToLower(ascii), nil
}

// ValidateOutboundURL verifies a URL against the outbound policy and returns a normalized URL string.
func ValidateOutboundURL(ctx context.Context, raw string, policy OutboundPolicy) (string, error) {
	if !policy.Enabled {
		return "", ErrOutboundDisabled
	}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("outbound url empty")
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme == "" {
		return "", fmt.Errorf("missing url scheme")
	}
	if u.Host == "" {
		return "", fmt.Errorf("missing url host")
	}
	if u.Fragment != "" {
		return "", fmt.Errorf("fragments not allowed")
	}

	scheme := strings.ToLower(u.Scheme)
	if !schemeAllowed(policy.Allow.Schemes, scheme) {
		return "", fmt.Errorf("scheme %q not allowed", scheme)
	}

	port, err := urlPort(u, scheme)
	if err != nil {
		return "", err
	}
	if !portAllowed(policy.Allow.Ports, port) {
		return "", fmt.Errorf("port %d not allowed", port)
	}

	host, err := NormalizeHost(u.Hostname())
	if err != nil {
		return "", err
	}

	allowedHosts, err := normalizeHostAllowlist(policy.Allow.Hosts)
	if err != nil {
		return "", err
	}
	allowedCIDRs, err := parseCIDRAllowlist(policy.Allow.CIDRs)
	if err != nil {
		return "", err
	}

	ips, err := resolveHostIPs(ctx, host)
	if err != nil {
		return "", err
	}

	_, hostAllowed := allowedHosts[host]
	ipAllowed := false
	for _, ip := range ips {
		if isBlockedIP(ip) && !ipInCIDRs(ip, allowedCIDRs) {
			return "", fmt.Errorf("blocked ip %s", ip.String())
		}
		if ipInCIDRs(ip, allowedCIDRs) {
			ipAllowed = true
		}
	}

	if !hostAllowed && !ipAllowed {
		return "", ErrOutboundNotAllowed
	}

	u.Host = joinHostPort(host, u.Port())
	return u.String(), nil
}

func schemeAllowed(allowed []string, scheme string) bool {
	for _, s := range allowed {
		if strings.EqualFold(strings.TrimSpace(s), scheme) {
			return true
		}
	}
	return false
}

func portAllowed(allowed []int, port int) bool {
	for _, p := range allowed {
		if p == port {
			return true
		}
	}
	return false
}

func urlPort(u *url.URL, scheme string) (int, error) {
	if u.Port() == "" {
		switch scheme {
		case "http":
			return 80, nil
		case "https":
			return 443, nil
		default:
			return 0, fmt.Errorf("unknown scheme %q", scheme)
		}
	}
	portStr := u.Port()
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return 0, fmt.Errorf("invalid port %q: %w", portStr, err)
	}
	return port, nil
}

func normalizeHostAllowlist(hosts []string) (map[string]struct{}, error) {
	allow := make(map[string]struct{})
	for _, host := range hosts {
		normalized, err := NormalizeHost(host)
		if err != nil {
			return nil, err
		}
		allow[normalized] = struct{}{}
	}
	return allow, nil
}

func parseCIDRAllowlist(entries []string) ([]*net.IPNet, error) {
	var nets []*net.IPNet
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		ip, ipnet, err := net.ParseCIDR(entry)
		if err == nil {
			ipnet.IP = ip
			nets = append(nets, ipnet)
			continue
		}
		ip = net.ParseIP(entry)
		if ip == nil {
			return nil, fmt.Errorf("invalid CIDR or IP: %s", entry)
		}
		bits := 32
		if ip.To4() == nil {
			bits = 128
		}
		nets = append(nets, &net.IPNet{
			IP:   ip,
			Mask: net.CIDRMask(bits, bits),
		})
	}
	return nets, nil
}

func resolveHostIPs(ctx context.Context, host string) ([]net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		return []net.IP{ip}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("resolve host %q: %w", host, err)
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("resolve host %q: no addresses", host)
	}
	ips := make([]net.IP, 0, len(addrs))
	for _, addr := range addrs {
		if addr.IP != nil {
			ips = append(ips, addr.IP)
		}
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("resolve host %q: no valid addresses", host)
	}
	return ips, nil
}

func isBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	return ip.IsLoopback() ||
		ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsMulticast()
}

func ipInCIDRs(ip net.IP, cidrs []*net.IPNet) bool {
	if ip == nil {
		return false
	}
	for _, n := range cidrs {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func joinHostPort(host, port string) string {
	if port == "" {
		if strings.Contains(host, ":") {
			return "[" + host + "]"
		}
		return host
	}
	return net.JoinHostPort(host, port)
}
