// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// SPDX-License-Identifier: MIT

package openwebif

import (
	"fmt"
	"net/url"
	"strings"
)

// ConvertToWebAPI converts a direct Enigma2 streaming URL (e.g. :8001/:17999) to
// the OpenWebIF Web API URL (/web/stream.m3u?ref=...).
//
// If targetURL is already a Web API URL or no valid service ref can be determined,
// targetURL is returned unchanged.
func ConvertToWebAPI(targetURL, serviceRef string) string {
	if strings.Contains(targetURL, "/web/stream.m3u") {
		return targetURL
	}

	parsed, err := url.Parse(targetURL)
	if err != nil {
		return targetURL
	}

	host := strings.Split(parsed.Host, ":")[0]

	realRef := ""
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) > 0 {
		candidate := parts[len(parts)-1]
		if strings.HasPrefix(candidate, "1:0:") {
			realRef = candidate
		}
	}

	if realRef == "" && strings.HasPrefix(serviceRef, "1:0:") {
		realRef = serviceRef
	}

	if realRef == "" {
		return targetURL
	}

	escapedRef := url.QueryEscape(realRef)
	scheme := parsed.Scheme
	if scheme == "" {
		scheme = "http"
	}

	return fmt.Sprintf("%s://%s/web/stream.m3u?ref=%s&name=Stream&device=etc&fname=Stream", scheme, host, escapedRef)
}
