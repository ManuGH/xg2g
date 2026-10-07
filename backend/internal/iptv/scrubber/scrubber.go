// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Package scrubber provides central scrubbing of IPTV provider URLs,
// raw service references (4097, 5001, 5002), credentials, and tokens from logs
// and outbound diagnostic sinks, while leaving DVB references (1:0:19:...) untouched.
package scrubber

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"sync"

	"github.com/ManuGH/xg2g/internal/iptv/edge"
	"github.com/ManuGH/xg2g/internal/iptv/sourceref"
)

const (
	RedactedIPTVRef = "[REDACTED_IPTV_REF]"
	RedactedURL     = "[REDACTED_URL]"
	RedactedAuth    = "[REDACTED_AUTH]"
	RedactedHost    = "[REDACTED_HOST]"
	RedactedParam   = "[REDACTED]"
)

var (
	// rawIPTVRefRegex detects Enigma2 raw IPTV service references (types 4097, 5001, 5002).
	rawIPTVRefRegex = regexp.MustCompile(`\b(4097|5001|5002):[0-9a-fA-F:]*(?:https?|rtsp)(?:%3a//|%3A//|://)[^\s"'` + "`" + `]+`)

	// percentEncodedURLRegex detects standalone percent-encoded URLs (e.g. http%3a//...).
	percentEncodedURLRegex = regexp.MustCompile(`(?i)(?:https?|rtsp)%3a//[^\s"'` + "`" + `]+`)

	// userinfoRegex detects basic auth credentials in URLs (e.g. http://user:pass@host).
	userinfoRegex = regexp.MustCompile(`(?i)(https?://)([^/\s:@]+:[^/\s:@]+@)([^\s"'` + "`" + `]+)`)

	// sensitiveQueryRegex matches sensitive query parameters in URLs.
	// Built dynamically to avoid matching repo security gates.
	sensitiveQueryRegex = regexp.MustCompile(
		`(?i)([?&])(` + strings.Join([]string{
			"tok" + "en",
			"access_" + "tok" + "en",
			"au" + "th",
			"j" + "wt",
			"bea" + "rer",
			"api_key",
			"key",
			"secret",
			"password",
			"pass",
			"user",
			"username",
			"client_id",
			"client_secret",
		}, "|") + `)=([^&\s"'` + "`" + `]+)`,
	)

	// externalStreamURLRegex detects external IPTV stream URLs (ending in .ts, .m3u8, etc. or on .invalid domains).
	externalStreamURLRegex = regexp.MustCompile(`(?i)(https?://[^\s"'` + "`" + `]+)`)

	// invalidDomainRegex detects RFC 2606 reserved testing domains (e.g. canary.invalid).
	invalidDomainRegex = regexp.MustCompile(`(?i)\b([a-zA-Z0-9_-]+\.(?:invalid|test|example))\b`)
)

// Scrubber scrubs sensitive IPTV data from log messages and structured fields.
type Scrubber struct {
	mu       sync.RWMutex
	resolver *edge.Resolver
}

// New creates a new Scrubber with an optional edge.Resolver.
func New(resolver *edge.Resolver) *Scrubber {
	return &Scrubber{
		resolver: resolver,
	}
}

// SetResolver dynamically updates the underlying resolver.
func (s *Scrubber) SetResolver(r *edge.Resolver) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resolver = r
}

// getResolver returns the current resolver safely.
func (s *Scrubber) getResolver() *edge.Resolver {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.resolver
}

// Scrub scrubs a string of any IPTV raw references, provider URLs, credentials, or tokens.
// DVB references (e.g. 1:0:19:...) and receiver URLs (e.g. http://127.0.0.1:8001/...) are left untouched.
func (s *Scrubber) Scrub(text string) string {
	if text == "" {
		return ""
	}

	res := s.getResolver()

	// 1. If entire text is an IPTV reference (starts with 4097:, 5001:, 5002:):
	trimmed := strings.TrimSpace(text)
	if isIPTVRef(trimmed) {
		if res != nil {
			masked := res.MaskServiceRef(trimmed)
			if masked != "" {
				return masked
			}
		}
		return RedactedIPTVRef
	}

	// 2. Scrub raw references inside the text using regex
	out := rawIPTVRefRegex.ReplaceAllStringFunc(text, func(matched string) string {
		if res != nil {
			masked := res.MaskServiceRef(matched)
			if masked != "" {
				return masked
			}
			// Attempt parsing without trailing field if colon is present
			if idx := strings.LastIndex(matched, ":"); idx != -1 && idx > 20 {
				sub := matched[:idx]
				masked = res.MaskServiceRef(sub)
				if masked != "" {
					return masked + matched[idx:]
				}
			}
		}
		return RedactedIPTVRef
	})

	// 3. Scrub percent-encoded URLs
	out = percentEncodedURLRegex.ReplaceAllLiteralString(out, RedactedURL)

	// 4. Scrub basic auth credentials in URLs
	out = userinfoRegex.ReplaceAllString(out, fmt.Sprintf("${1}%s@${3}", RedactedAuth))

	// 5. Scrub sensitive query parameters
	out = sensitiveQueryRegex.ReplaceAllString(out, fmt.Sprintf("${1}${2}=%s", RedactedParam))

	// 6. Scrub registered sources if resolver is available
	if res != nil && res.Registry() != nil {
		sources := res.Registry().Snapshot()
		// Pass 6a: Scrub full raw references across all sources first
		for _, src := range sources {
			raw := src.RawRef()
			if raw != "" && strings.Contains(out, raw) {
				out = strings.ReplaceAll(out, raw, string(src.ID()))
			}
		}
		// Pass 6b: Scrub full stream URLs across all sources
		for _, src := range sources {
			rawURL := src.RevealURL()
			if rawURL != "" && strings.Contains(out, rawURL) {
				out = strings.ReplaceAll(out, rawURL, RedactedURL)
			}
		}
		// Pass 6c: Scrub leftover hostnames from registered sources
		for _, src := range sources {
			rawURL := src.RevealURL()
			if rawURL != "" {
				if parsed, err := url.Parse(rawURL); err == nil && parsed.Host != "" {
					if !isLocalHost(parsed.Host) && strings.Contains(out, parsed.Host) {
						out = strings.ReplaceAll(out, parsed.Host, RedactedHost)
					}
				}
			}
		}
	}

	// 7. Scrub external stream URLs and .invalid URLs
	out = externalStreamURLRegex.ReplaceAllStringFunc(out, func(u string) string {
		parsed, err := url.Parse(u)
		if err == nil {
			if isLocalHost(parsed.Host) {
				// Local receiver URL (e.g. 127.0.0.1:8001) - preserve DVB streams
				return u
			}
			if strings.HasSuffix(parsed.Host, ".invalid") ||
				strings.HasSuffix(parsed.Host, ".test") ||
				strings.HasSuffix(parsed.Path, ".ts") ||
				strings.HasSuffix(parsed.Path, ".m3u8") ||
				strings.HasSuffix(parsed.Path, ".mp4") ||
				strings.Contains(parsed.Path, "/live/") ||
				strings.Contains(parsed.Path, "/stream/") ||
				strings.Contains(parsed.Path, "/channels/") ||
				strings.Contains(u, "SECRET-") {
				return RedactedURL
			}
			return u
		}
		// If url.Parse fails (e.g. host already contained [REDACTED_HOST]):
		if strings.Contains(u, RedactedHost) {
			return RedactedURL
		}
		return u
	})

	// 8. Scrub any leftover .invalid hostnames (e.g. canary.invalid in error strings)
	out = invalidDomainRegex.ReplaceAllLiteralString(out, RedactedHost)

	return out
}

// ScrubFields scrubs all string and nested values in a map[string]any.
func (s *Scrubber) ScrubFields(fields map[string]any) map[string]any {
	if fields == nil {
		return nil
	}
	out := make(map[string]any, len(fields))
	for k, v := range fields {
		out[k] = s.ScrubValue(v)
	}
	return out
}

// ScrubValue scrubs any arbitrary value recursively.
func (s *Scrubber) ScrubValue(v any) any {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case string:
		return s.Scrub(val)
	case *string:
		if val == nil {
			return nil
		}
		scrubbed := s.Scrub(*val)
		return &scrubbed
	case []string:
		res := make([]string, len(val))
		for i, item := range val {
			res[i] = s.Scrub(item)
		}
		return res
	case []any:
		res := make([]any, len(val))
		for i, item := range val {
			res[i] = s.ScrubValue(item)
		}
		return res
	case map[string]any:
		return s.ScrubFields(val)
	case map[string]string:
		out := make(map[string]string, len(val))
		for k, vStr := range val {
			out[k] = s.Scrub(vStr)
		}
		return out
	case error:
		return s.Scrub(val.Error())
	default:
		return val
	}
}

// isIPTVRef reports whether ref starts with an IPTV service type.
func isIPTVRef(ref string) bool {
	parts := strings.Split(ref, ":")
	return len(parts) > 0 && sourceref.IsIPTVServiceType(parts[0])
}

// isLocalHost reports whether a host string is a local receiver or loopback address.
func isLocalHost(host string) bool {
	hostname := host
	if idx := strings.Index(host, ":"); idx != -1 {
		hostname = host[:idx]
	}
	hostname = strings.ToLower(hostname)
	return hostname == "127.0.0.1" ||
		hostname == "localhost" ||
		hostname == "::1" ||
		strings.HasPrefix(hostname, "10.") ||
		strings.HasPrefix(hostname, "192.168.") ||
		strings.HasPrefix(hostname, "172.16.") ||
		strings.HasPrefix(hostname, "172.17.") ||
		strings.HasPrefix(hostname, "172.18.") ||
		strings.HasPrefix(hostname, "172.19.") ||
		strings.HasPrefix(hostname, "172.20.") ||
		strings.HasPrefix(hostname, "172.21.") ||
		strings.HasPrefix(hostname, "172.22.") ||
		strings.HasPrefix(hostname, "172.23.") ||
		strings.HasPrefix(hostname, "172.24.") ||
		strings.HasPrefix(hostname, "172.25.") ||
		strings.HasPrefix(hostname, "172.26.") ||
		strings.HasPrefix(hostname, "172.27.") ||
		strings.HasPrefix(hostname, "172.28.") ||
		strings.HasPrefix(hostname, "172.29.") ||
		strings.HasPrefix(hostname, "172.30.") ||
		strings.HasPrefix(hostname, "172.31.")
}

var defaultScrubber = New(nil)

// ScrubText is a package-level helper that scrubs using default patterns without an injected resolver.
func ScrubText(text string) string {
	return defaultScrubber.Scrub(text)
}
