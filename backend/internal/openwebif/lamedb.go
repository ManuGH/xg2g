// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package openwebif

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// Enigma2's service database is the receiver's own record of which physical
// transponder each transport stream lives on. OpenWebIF exposes the file directly,
// which is the only RF fact source that works across OpenWebIF versions: the
// /api/channelinfo endpoint this project previously relied on does not exist on
// OWIF 2.4.0 (OpenATV) and answers 404 there.
//
// Newer images write both formats and keep them in sync; older ones only have the
// unnumbered file, so both paths are tried.
var lamedbPaths = []string{
	"/etc/enigma2/lamedb5",
	"/etc/enigma2/lamedb",
}

// lamedbBanner opens every service database regardless of format version. OpenWebIF
// answers a missing or unreadable file with an HTML error page under HTTP 200 on
// some builds, so the payload has to be identified by content rather than status.
const lamedbBanner = "eDVB services /"

// GetLamedb fetches the receiver's service database and returns it verbatim.
//
// The payload is returned unparsed: the DVB semantics belong to the topology
// domain, and this client only has to prove that what it fetched is a service
// database and not an error page.
func (c *Client) GetLamedb(ctx context.Context) ([]byte, error) {
	var lastErr error

	for _, path := range lamedbPaths {
		body, err := c.get(ctx, "/file?file="+url.QueryEscape(path), "lamedb", nil)
		if err != nil {
			lastErr = fmt.Errorf("fetch %s: %w", path, err)
			continue
		}
		if !isLamedbPayload(body) {
			lastErr = fmt.Errorf("fetch %s: response is not an enigma2 service database", path)
			continue
		}
		return body, nil
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("no service database path configured")
	}
	return nil, lastErr
}

// isLamedbPayload reports whether a body opens with the service database banner,
// tolerating a leading byte order mark.
func isLamedbPayload(body []byte) bool {
	const probe = 64
	head := body
	if len(head) > probe {
		head = head[:probe]
	}
	return strings.Contains(string(head), lamedbBanner)
}
