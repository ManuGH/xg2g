// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package net

import "testing"

func TestPointsAtReceiver_RejectsEveryPortOnTheReceiver(t *testing.T) {
	const base = "http://192.168.1.50"

	// The receiver's stream endpoint is not one known port: the receiver names it
	// via /web/stream.m3u, StreamPort is only a fallback, and the media path
	// retries on 8001/8002. All of them are the receiver.
	urls := []string{
		"http://192.168.1.50:8001/1:0:19:83:6:85:C00000:0:0:0:",
		"http://192.168.1.50:8002/1:0:19:83:6:85:C00000:0:0:0:",
		"http://192.168.1.50:17999/1:0:19:83:6:85:C00000:0:0:0:",
		"http://192.168.1.50/web/stream.m3u?ref=1:0:19:83",
		"https://192.168.1.50:8001/stream",
	}

	for _, raw := range urls {
		t.Run(raw, func(t *testing.T) {
			if !PointsAtReceiver(raw, base) {
				t.Errorf("PointsAtReceiver(%q, %q) = false, want true", raw, base)
			}
		})
	}
}

func TestPointsAtReceiver_AllowsExternalIPTV(t *testing.T) {
	const base = "http://192.168.1.50:8080"

	urls := []string{
		"http://provider.example.com/live/channel.m3u8",
		"http://192.168.1.51:8001/stream", // the neighbour, not the receiver
		"http://10.0.0.1:8001/stream",
		"https://cdn.example.net:443/hls/index.m3u8",
	}

	for _, raw := range urls {
		t.Run(raw, func(t *testing.T) {
			if PointsAtReceiver(raw, base) {
				t.Errorf("PointsAtReceiver(%q, %q) = true, want false", raw, base)
			}
		})
	}
}

// The receiver is configured with a base URL that carries a scheme, a port and a
// path; the candidate arrives as whatever the client sent. Neither shape may
// change the answer.
func TestPointsAtReceiver_IgnoresSchemePortAndPath(t *testing.T) {
	tests := []struct {
		name      string
		candidate string
		base      string
	}{
		{"base with port and path", "http://vuplus.local:8001/x", "http://vuplus.local:8080/web/"},
		{"base without scheme", "http://vuplus.local:8001/x", "vuplus.local:8080"},
		{"candidate without scheme", "vuplus.local:8001/x", "http://vuplus.local:8080"},
		{"differing case", "http://VUPLUS.LOCAL:8001/x", "http://vuplus.local:8080"},
		{"trailing dot on the candidate", "http://vuplus.local.:8001/x", "http://vuplus.local:8080"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if !PointsAtReceiver(tc.candidate, tc.base) {
				t.Errorf("PointsAtReceiver(%q, %q) = false, want true", tc.candidate, tc.base)
			}
		})
	}
}

// With no receiver configured there is nothing to protect, and refusing every URL
// would be a worse failure than the one this guard prevents.
func TestPointsAtReceiver_DisabledWithoutAUsableReceiver(t *testing.T) {
	tests := []struct {
		name      string
		candidate string
		base      string
	}{
		{"empty base", "http://192.168.1.50:8001/x", ""},
		{"whitespace base", "http://192.168.1.50:8001/x", "   "},
		{"base with no host", "http://192.168.1.50:8001/x", "http://"},
		{"empty candidate", "", "http://192.168.1.50"},
		{"candidate with no host", "http://", "http://192.168.1.50"},
		{"unparseable candidate", "http://[::1", "http://192.168.1.50"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if PointsAtReceiver(tc.candidate, tc.base) {
				t.Errorf("PointsAtReceiver(%q, %q) = true, want false", tc.candidate, tc.base)
			}
		})
	}
}

// An IPv6 receiver must match through the bracket form as well.
func TestPointsAtReceiver_IPv6(t *testing.T) {
	if !PointsAtReceiver("http://[fe80::1]:8001/x", "http://[fe80::1]:8080") {
		t.Error("IPv6 receiver not recognised through the bracket form")
	}
	if PointsAtReceiver("http://[fe80::2]:8001/x", "http://[fe80::1]:8080") {
		t.Error("a different IPv6 host was treated as the receiver")
	}
}
