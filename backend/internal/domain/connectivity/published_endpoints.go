// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package connectivity

import (
	"errors"
	"fmt"
	"math"
	"net/netip"
	"net/url"
	"slices"
	"sort"
	"strings"

	platformnet "github.com/ManuGH/xg2g/internal/platform/net"
)

type EndpointKind string

const (
	EndpointKindPublicHTTPS EndpointKind = "public_https"
	EndpointKindLocalHTTPS  EndpointKind = "local_https"
	EndpointKindLocalHTTP   EndpointKind = "local_http"
)

type TLSMode string

const (
	TLSModeRequired   TLSMode = "required"
	TLSModeOptional   TLSMode = "optional"
	TLSModeProhibited TLSMode = "prohibited"
)

type EndpointSource string

const (
	EndpointSourceConfig   EndpointSource = "config"
	EndpointSourceEnv      EndpointSource = "env"
	EndpointSourceOperator EndpointSource = "operator"
)

var (
	ErrInvalidEndpointURL        = errors.New("published endpoint url is invalid")
	ErrInvalidEndpointKind       = errors.New("published endpoint kind is invalid")
	ErrInvalidEndpointTLSMode    = errors.New("published endpoint tls_mode is invalid")
	ErrInvalidEndpointSource     = errors.New("published endpoint source is invalid")
	ErrInvalidEndpointPriority   = fmt.Errorf("published endpoint priority must be between 0 and %d", math.MaxInt32)
	ErrInvalidEndpointReason     = errors.New("published endpoint advertise_reason must not be empty")
	ErrInvalidEndpointCapability = errors.New("published endpoint capabilities are invalid")
	ErrEndpointHostRejected      = errors.New("published endpoint host is rejected")
	ErrEndpointDuplicate         = errors.New("published endpoint is duplicated after normalization")
	ErrEndpointLocalHTTPDisabled = errors.New("published local_http endpoint requires explicit opt-in")
)

var knownRejectedHosts = map[string]struct{}{
	"localhost":             {},
	"localhost.localdomain": {},
	"host.docker.internal":  {},
}

var rejectedIPPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/32"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.17.0.0/16"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("fe80::/10"),
}

// PublishedEndpoint is the normalized backend-truth connectivity candidate
// exposed to native and web clients. It is intentionally declarative.
type PublishedEndpoint struct {
	URL             string
	Kind            EndpointKind
	Priority        int
	TLSMode         TLSMode
	AllowPairing    bool
	AllowStreaming  bool
	AllowWeb        bool
	AllowNative     bool
	AdvertiseReason string
	Source          EndpointSource
}

type ProviderOptions struct {
	AllowLocalHTTP bool
}

// Provider canonicalizes and validates published endpoint truth.
// It never derives endpoints from request headers, container topology, or runtime probing.
type Provider struct {
	options ProviderOptions
}

func NewProvider(options ProviderOptions) Provider {
	return Provider{options: options}
}

// Build returns the stable, validated published endpoint set.
// Sorting is deterministic: priority ASC, then canonical URL ASC.
func (p Provider) Build(specs []PublishedEndpoint) ([]PublishedEndpoint, error) {
	if len(specs) == 0 {
		return []PublishedEndpoint{}, nil
	}

	out := make([]PublishedEndpoint, 0, len(specs))
	seen := make(map[string]struct{}, len(specs))

	for idx, spec := range specs {
		normalized, err := preparePublishedEndpoint(spec, p.options)
		if err != nil {
			return nil, fmt.Errorf("published endpoint %d: %w", idx, err)
		}
		if _, ok := seen[normalized.URL]; ok {
			return nil, fmt.Errorf("%w: %s", ErrEndpointDuplicate, normalized.URL)
		}
		seen[normalized.URL] = struct{}{}
		out = append(out, normalized)
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority < out[j].Priority
		}
		return out[i].URL < out[j].URL
	})

	return out, nil
}

func preparePublishedEndpoint(spec PublishedEndpoint, options ProviderOptions) (PublishedEndpoint, error) {
	kind := normalizeEndpointKind(spec.Kind)
	if !isKnownEndpointKind(kind) {
		return PublishedEndpoint{}, fmt.Errorf("%w: %q", ErrInvalidEndpointKind, spec.Kind)
	}

	tlsMode := normalizeTLSMode(spec.TLSMode)
	if !isKnownTLSMode(tlsMode) {
		return PublishedEndpoint{}, fmt.Errorf("%w: %q", ErrInvalidEndpointTLSMode, spec.TLSMode)
	}

	source := normalizeEndpointSource(spec.Source)
	if !isKnownEndpointSource(source) {
		return PublishedEndpoint{}, fmt.Errorf("%w: %q", ErrInvalidEndpointSource, spec.Source)
	}

	if spec.Priority < 0 || spec.Priority > math.MaxInt32 {
		return PublishedEndpoint{}, ErrInvalidEndpointPriority
	}

	reason := strings.TrimSpace(spec.AdvertiseReason)
	if reason == "" {
		return PublishedEndpoint{}, ErrInvalidEndpointReason
	}

	if !spec.AllowPairing && !spec.AllowStreaming && !spec.AllowWeb && !spec.AllowNative {
		return PublishedEndpoint{}, fmt.Errorf("%w: endpoint must enable at least one usage", ErrInvalidEndpointCapability)
	}

	parsed, err := parseEndpointURL(spec.URL)
	if err != nil {
		return PublishedEndpoint{}, err
	}

	scheme := strings.ToLower(parsed.Scheme)
	host, err := normalizeEndpointHost(parsed.Hostname())
	if err != nil {
		return PublishedEndpoint{}, err
	}

	addr, hasIP := parseHostAddr(host)
	if err := validateEndpointHost(kind, host, hasIP, addr); err != nil {
		return PublishedEndpoint{}, err
	}

	if err := validateTransport(kind, tlsMode, scheme, spec.AllowWeb, options.AllowLocalHTTP, hasIP, addr); err != nil {
		return PublishedEndpoint{}, err
	}

	canonicalURL, err := canonicalizeEndpointURL(parsed, host, scheme)
	if err != nil {
		return PublishedEndpoint{}, err
	}

	return PublishedEndpoint{
		URL:             canonicalURL,
		Kind:            kind,
		Priority:        spec.Priority,
		TLSMode:         tlsMode,
		AllowPairing:    spec.AllowPairing,
		AllowStreaming:  spec.AllowStreaming,
		AllowWeb:        spec.AllowWeb,
		AllowNative:     spec.AllowNative,
		AdvertiseReason: reason,
		Source:          source,
	}, nil
}

func parseEndpointURL(raw string) (*url.URL, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, fmt.Errorf("%w: empty url", ErrInvalidEndpointURL)
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidEndpointURL, err)
	}
	if parsed.Scheme == "" {
		return nil, fmt.Errorf("%w: missing scheme", ErrInvalidEndpointURL)
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("%w: missing host", ErrInvalidEndpointURL)
	}
	if parsed.User != nil {
		return nil, fmt.Errorf("%w: userinfo is not allowed", ErrInvalidEndpointURL)
	}
	if parsed.RawQuery != "" {
		return nil, fmt.Errorf("%w: query is not allowed", ErrInvalidEndpointURL)
	}
	if parsed.Fragment != "" {
		return nil, fmt.Errorf("%w: fragment is not allowed", ErrInvalidEndpointURL)
	}
	if path := parsed.EscapedPath(); path != "" && path != "/" {
		return nil, fmt.Errorf("%w: only origin URLs are allowed", ErrInvalidEndpointURL)
	}
	return parsed, nil
}

func normalizeEndpointHost(raw string) (string, error) {
	host, err := platformnet.NormalizeHost(raw)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrEndpointHostRejected, err)
	}
	if _, ok := knownRejectedHosts[host]; ok {
		return "", fmt.Errorf("%w: %s is not publishable", ErrEndpointHostRejected, host)
	}
	if strings.HasSuffix(host, ".docker.internal") {
		return "", fmt.Errorf("%w: %s is not publishable", ErrEndpointHostRejected, host)
	}
	if !strings.Contains(host, ".") {
		if _, hasIP := parseHostAddr(host); !hasIP {
			return "", fmt.Errorf("%w: single-label host %q is not allowed", ErrEndpointHostRejected, host)
		}
	}
	return host, nil
}

func validateEndpointHost(kind EndpointKind, host string, hasIP bool, addr netip.Addr) error {
	if !hasIP {
		return nil
	}
	for _, prefix := range rejectedIPPrefixes {
		if prefix.Contains(addr) {
			return fmt.Errorf("%w: ip %s is not publishable", ErrEndpointHostRejected, addr.String())
		}
	}
	if addr.IsMulticast() {
		return fmt.Errorf("%w: multicast ip %s is not publishable", ErrEndpointHostRejected, addr.String())
	}
	if kind == EndpointKindPublicHTTPS && addr.IsPrivate() {
		return fmt.Errorf("%w: public_https must not use private ip %s", ErrEndpointHostRejected, addr.String())
	}
	return nil
}

func validateTransport(kind EndpointKind, tlsMode TLSMode, scheme string, allowWeb bool, allowLocalHTTP bool, hasIP bool, addr netip.Addr) error {
	switch kind {
	case EndpointKindPublicHTTPS, EndpointKindLocalHTTPS:
		if scheme != "https" {
			return fmt.Errorf("%w: %s endpoints require https urls", ErrInvalidEndpointURL, kind)
		}
		if tlsMode != TLSModeRequired {
			return fmt.Errorf("%w: %s endpoints require tls_mode=required", ErrInvalidEndpointTLSMode, kind)
		}
	case EndpointKindLocalHTTP:
		if !allowLocalHTTP {
			return ErrEndpointLocalHTTPDisabled
		}
		if scheme != "http" {
			return fmt.Errorf("%w: local_http endpoints require http urls", ErrInvalidEndpointURL)
		}
		if tlsMode != TLSModeProhibited {
			return fmt.Errorf("%w: local_http endpoints require tls_mode=prohibited", ErrInvalidEndpointTLSMode)
		}
		if allowWeb {
			return fmt.Errorf("%w: allow_web requires HTTPS", ErrInvalidEndpointCapability)
		}
	}

	if allowWeb && tlsMode != TLSModeRequired {
		return fmt.Errorf("%w: allow_web requires tls_mode=required", ErrInvalidEndpointCapability)
	}
	if kind == EndpointKindPublicHTTPS && hasIP && addr.IsPrivate() {
		return fmt.Errorf("%w: public_https must not use private ip %s", ErrEndpointHostRejected, addr.String())
	}
	if tlsMode == TLSModeOptional {
		return fmt.Errorf("%w: tls_mode=optional is not allowed for published endpoint truth", ErrInvalidEndpointTLSMode)
	}
	return nil
}

func canonicalizeEndpointURL(parsed *url.URL, host, scheme string) (string, error) {
	port := parsed.Port()
	switch scheme {
	case "http":
		if port == "80" {
			port = ""
		}
	case "https":
		if port == "443" {
			port = ""
		}
	default:
		return "", fmt.Errorf("%w: unsupported scheme %q", ErrInvalidEndpointURL, scheme)
	}

	canonical := *parsed
	canonical.Scheme = scheme
	canonical.Host = host
	if port != "" {
		canonical.Host = joinHostPort(host, port)
	}
	canonical.Path = ""
	canonical.RawPath = ""
	canonical.RawQuery = ""
	canonical.Fragment = ""
	canonical.User = nil
	return canonical.String(), nil
}

func normalizeEndpointKind(value EndpointKind) EndpointKind {
	return EndpointKind(strings.ToLower(strings.TrimSpace(string(value))))
}

func normalizeTLSMode(value TLSMode) TLSMode {
	return TLSMode(strings.ToLower(strings.TrimSpace(string(value))))
}

func normalizeEndpointSource(value EndpointSource) EndpointSource {
	return EndpointSource(strings.ToLower(strings.TrimSpace(string(value))))
}

func isKnownEndpointKind(value EndpointKind) bool {
	switch value {
	case EndpointKindPublicHTTPS, EndpointKindLocalHTTPS, EndpointKindLocalHTTP:
		return true
	default:
		return false
	}
}

func isKnownTLSMode(value TLSMode) bool {
	switch value {
	case TLSModeRequired, TLSModeOptional, TLSModeProhibited:
		return true
	default:
		return false
	}
}

func isKnownEndpointSource(value EndpointSource) bool {
	switch value {
	case EndpointSourceConfig, EndpointSourceEnv, EndpointSourceOperator:
		return true
	default:
		return false
	}
}

func parseHostAddr(host string) (netip.Addr, bool) {
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}

func joinHostPort(host, port string) string {
	if port == "" {
		return host
	}
	addr, ok := parseHostAddr(host)
	if ok && addr.Is6() {
		return "[" + host + "]:" + port
	}
	return host + ":" + port
}

func ClonePublishedEndpoints(values []PublishedEndpoint) []PublishedEndpoint {
	if len(values) == 0 {
		return []PublishedEndpoint{}
	}
	cloned := make([]PublishedEndpoint, len(values))
	copy(cloned, values)
	slices.SortFunc(cloned, func(a, b PublishedEndpoint) int {
		if a.Priority != b.Priority {
			if a.Priority < b.Priority {
				return -1
			}
			return 1
		}
		return strings.Compare(a.URL, b.URL)
	})
	return cloned
}
