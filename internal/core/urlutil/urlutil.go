// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

package urlutil

import (
	"net/url"
)

// SanitizeURL removes user info from a URL string for safe logging.
func SanitizeURL(rawURL string) string {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "invalid-url-redacted"
	}
	parsedURL.User = nil
	parsedURL.RawQuery = ""
	return parsedURL.String()
}
