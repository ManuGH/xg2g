// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package net

import (
	"fmt"
	"net/url"
	"strings"
)

// SanitizeURL removes user info and query parameters for safe logging.
func SanitizeURL(rawURL string) string {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "invalid-url-redacted"
	}
	parsedURL.User = nil
	parsedURL.RawQuery = ""
	return parsedURL.String()
}

// ParseDirectHTTPURL validates if a string is a safe, direct HTTP/HTTPS URL.
// It enforces:
//   - Scheme must be "http" or "https"
//   - Host must be non-empty
//   - No embedded User/Password credentials
func ParseDirectHTTPURL(s string) (*url.URL, bool) {
	s = strings.TrimSpace(s)
	u, err := url.Parse(s)
	if err != nil {
		return nil, false
	}

	// strict scheme check (case-insensitive)
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, false
	}

	// require host
	if u.Host == "" {
		return nil, false
	}

	// reject credentials
	if u.User != nil {
		return nil, false
	}

	// reject fragments
	if u.Fragment != "" {
		return nil, false
	}

	return u, true
}

// NormalizeAuthority parses a host string (which may act as an authority)
// and returns the normalized hostname and port.
//
// If the input lacks a scheme, defaultScheme is prepended before parsing.
// The hostname relies on url.URL.Hostname() which strips brackets from IPv6 literals.
func NormalizeAuthority(s, defaultScheme string) (host, port string, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "", fmt.Errorf("empty input")
	}

	// Detect if scheme is missing using a naive heuristic "://" check.
	// We assume if "://" is missing, it's just a host or host:port.
	if !strings.Contains(s, "://") {
		if defaultScheme == "" {
			defaultScheme = "http"
		}
		s = defaultScheme + "://" + s
	}

	u, err := url.Parse(s)
	if err != nil {
		return "", "", fmt.Errorf("failed to parse authority: %w", err)
	}

	if u.Host == "" {
		return "", "", fmt.Errorf("empty host")
	}

	return u.Hostname(), u.Port(), nil
}
