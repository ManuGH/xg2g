// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package net

import (
	"net/url"
	"strings"
)

// PointsAtReceiver reports whether rawURL addresses the same host as
// receiverBaseURL.
//
// It exists to close a back door in the live path. A stream-start intent may
// carry a direct HTTP URL as its service reference, which becomes a SourceURL
// session that hands the URL straight to FFmpeg - no tuner lease, no zap, no
// readiness check. Point that URL at the receiver's own stream port and a live
// transcode opens the receiver behind the back of shared ingest, under a source
// class that was meant for external IPTV.
//
// The comparison is deliberately on the host alone, not host:port. The receiver's
// stream endpoint is not a single known port: ResolveStreamURL asks
// /web/stream.m3u and uses whatever port the receiver names, the configured
// StreamPort is only the fallback, and the media path has its own 8001/8002
// retry on top. Any port-specific check would have holes on exactly the ports
// this guard exists to cover, so the rule is that a live SourceURL never points
// at the receiver box at all. External IPTV lives somewhere else by definition.
//
// A receiverBaseURL that is empty or unparseable disables the check: with no
// configured receiver there is nothing to protect, and refusing every URL would
// be a worse failure than the one being prevented.
func PointsAtReceiver(rawURL, receiverBaseURL string) bool {
	receiverHost, ok := hostOf(receiverBaseURL)
	if !ok {
		return false
	}
	candidateHost, ok := hostOf(rawURL)
	if !ok {
		return false
	}
	return candidateHost == receiverHost
}

// hostOf normalizes the host of a URL that may arrive without a scheme.
func hostOf(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return "", false
	}
	host, err := NormalizeHost(u.Hostname())
	if err != nil {
		return "", false
	}
	return host, true
}
