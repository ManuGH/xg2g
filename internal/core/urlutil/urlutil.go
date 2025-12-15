// SPDX-License-Identifier: MIT

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
