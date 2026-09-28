// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package net

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

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

// ParseValidatedOutboundURL verifies a URL against the outbound policy and returns a normalized URL.
func ParseValidatedOutboundURL(ctx context.Context, raw string, policy OutboundPolicy) (*url.URL, error) {
	if !policy.Enabled {
		return nil, ErrOutboundDisabled
	}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, fmt.Errorf("outbound url empty")
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return nil, fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme == "" {
		return nil, fmt.Errorf("missing url scheme")
	}
	if u.Host == "" {
		return nil, fmt.Errorf("missing url host")
	}
	if u.User != nil {
		return nil, fmt.Errorf("userinfo not allowed")
	}
	if u.Fragment != "" {
		return nil, fmt.Errorf("fragments not allowed")
	}

	scheme := strings.ToLower(u.Scheme)
	if !schemeAllowed(policy.Allow.Schemes, scheme) {
		return nil, fmt.Errorf("scheme %q not allowed", scheme)
	}

	port, err := urlPort(u, scheme)
	if err != nil {
		return nil, err
	}
	if !portAllowed(policy.Allow.Ports, port) {
		return nil, fmt.Errorf("port %d not allowed", port)
	}

	host, err := NormalizeHost(u.Hostname())
	if err != nil {
		return nil, err
	}

	allowedHosts, err := normalizeHostAllowlist(policy.Allow.Hosts)
	if err != nil {
		return nil, err
	}
	allowedCIDRs, err := parseCIDRAllowlist(policy.Allow.CIDRs)
	if err != nil {
		return nil, err
	}

	ips, err := resolveHostIPs(ctx, host)
	if err != nil {
		return nil, err
	}

	_, hostAllowed := allowedHosts[host]
	isExplicitIP := net.ParseIP(host) != nil

	ipAllowed := false
	for _, ip := range ips {
		if isBlockedIP(ip) && !ipInCIDRs(ip, allowedCIDRs) {
			return nil, fmt.Errorf("blocked ip %s", ip.String())
		}
		if isPrivateOrInternalIP(ip) && !ipInCIDRs(ip, allowedCIDRs) && !isExplicitIP && !hostAllowed {
			return nil, fmt.Errorf("blocked private ip %s for host %s", ip.String(), host)
		}
		if ipInCIDRs(ip, allowedCIDRs) {
			ipAllowed = true
		}
	}

	if !hostAllowed && !ipAllowed {
		return nil, ErrOutboundNotAllowed
	}

	normalized := *u
	normalized.Scheme = scheme
	normalized.Host = joinHostPort(host, u.Port())
	return &normalized, nil
}

// SafeDialContext returns a dialer function that validates destination IP addresses at dial time,
// neutralizing Time-of-Check to Time-of-Use (TOCTOU) DNS rebinding vulnerabilities.
func SafeDialContext(policy OutboundPolicy, baseDialer *net.Dialer) func(ctx context.Context, network, addr string) (net.Conn, error) {
	if baseDialer == nil {
		baseDialer = &net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}
	}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		if !policy.Enabled {
			return nil, ErrOutboundDisabled
		}
		host, portStr, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("invalid address %q: %w", addr, err)
		}
		port, err := strconv.Atoi(portStr)
		if err != nil {
			return nil, fmt.Errorf("invalid port %q: %w", portStr, err)
		}
		if !portAllowed(policy.Allow.Ports, port) {
			return nil, fmt.Errorf("port %d not allowed", port)
		}

		normalizedHost, err := NormalizeHost(host)
		if err != nil {
			return nil, err
		}

		allowedHosts, err := normalizeHostAllowlist(policy.Allow.Hosts)
		if err != nil {
			return nil, err
		}
		allowedCIDRs, err := parseCIDRAllowlist(policy.Allow.CIDRs)
		if err != nil {
			return nil, err
		}

		ips, err := resolveHostIPs(ctx, normalizedHost)
		if err != nil {
			return nil, err
		}

		_, hostAllowed := allowedHosts[normalizedHost]
		isExplicitIP := net.ParseIP(normalizedHost) != nil

		var dialableIPs []net.IP
		for _, ip := range ips {
			if isBlockedIP(ip) && !ipInCIDRs(ip, allowedCIDRs) {
				return nil, fmt.Errorf("blocked ip %s", ip.String())
			}
			if isPrivateOrInternalIP(ip) && !ipInCIDRs(ip, allowedCIDRs) && !isExplicitIP && !hostAllowed {
				return nil, fmt.Errorf("blocked private ip %s for host %s", ip.String(), normalizedHost)
			}
			if hostAllowed || ipInCIDRs(ip, allowedCIDRs) {
				dialableIPs = append(dialableIPs, ip)
			}
		}

		if len(dialableIPs) == 0 {
			return nil, ErrOutboundNotAllowed
		}

		var lastErr error
		for _, ip := range dialableIPs {
			targetAddr := net.JoinHostPort(ip.String(), portStr)
			conn, err := baseDialer.DialContext(ctx, network, targetAddr)
			if err == nil {
				return conn, nil
			}
			lastErr = err
		}
		return nil, lastErr
	}
}

// NewSafeTransport returns a hardened http.Transport configured with SafeDialContext.
func NewSafeTransport(policy OutboundPolicy) *http.Transport {
	dialer := &net.Dialer{
		Timeout:   5 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	return &http.Transport{
		DialContext:           SafeDialContext(policy, dialer),
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		IdleConnTimeout:       30 * time.Second,
		DisableKeepAlives:     true,
		ForceAttemptHTTP2:     false,
	}
}

// ValidateOutboundURL verifies a URL against the outbound policy and returns a normalized URL string.
func ValidateOutboundURL(ctx context.Context, raw string, policy OutboundPolicy) (string, error) {
	u, err := ParseValidatedOutboundURL(ctx, raw, policy)
	if err != nil {
		return "", err
	}
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
	return slices.Contains(allowed, port)
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

var cgnatNet = &net.IPNet{
	IP:   net.IPv4(100, 64, 0, 0),
	Mask: net.CIDRMask(10, 32),
}

func isBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip4 := ip.To4(); ip4 != nil && ip4[0] == 0 {
		return true
	}
	return ip.IsLoopback() ||
		ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsMulticast()
}

func isPrivateOrInternalIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsPrivate() {
		return true
	}
	return cgnatNet.Contains(ip)
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
