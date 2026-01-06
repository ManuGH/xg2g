// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package netutil

import (
	"net/url"

	pnet "github.com/ManuGH/xg2g/internal/platform/net"
)

// Deprecated: Use internal/platform/net.ParseDirectHTTPURL instead.
func ParseDirectHTTPURL(s string) (*url.URL, bool) {
	return pnet.ParseDirectHTTPURL(s)
}

// Deprecated: Use internal/platform/net.NormalizeAuthority instead.
func NormalizeAuthority(s, defaultScheme string) (host, port string, err error) {
	return pnet.NormalizeAuthority(s, defaultScheme)
}
