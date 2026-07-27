// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package middleware

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func mustCIDRs(t *testing.T, entries ...string) []*net.IPNet {
	t.Helper()
	nets, err := ParseCIDRs(entries)
	if err != nil {
		t.Fatalf("ParseCIDRs(%v): %v", entries, err)
	}
	return nets
}

func reqFrom(remoteAddr, xff string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.RemoteAddr = remoteAddr
	if xff != "" {
		r.Header.Set("X-Forwarded-For", xff)
	}
	return r
}

func TestResolveClientIP(t *testing.T) {
	proxies := []string{"172.20.0.0/16", "10.8.0.1"}

	tests := []struct {
		name       string
		remoteAddr string
		xff        string
		xRealIP    string
		trusted    []string
		want       string
	}{
		{
			// The core anti-spoofing invariant: a client talking to us directly
			// cannot rename itself via a header.
			name:       "direct client with forged XFF stays at RemoteAddr",
			remoteAddr: "203.0.113.9:44321",
			xff:        "1.2.3.4",
			trusted:    proxies,
			want:       "203.0.113.9",
		},
		{
			name:       "no trusted proxies configured ignores XFF entirely",
			remoteAddr: "172.20.0.5:1234",
			xff:        "198.51.100.7",
			trusted:    nil,
			want:       "172.20.0.5",
		},
		{
			name:       "trusted proxy honors the client it forwarded",
			remoteAddr: "172.20.0.5:1234",
			xff:        "198.51.100.7",
			trusted:    proxies,
			want:       "198.51.100.7",
		},
		{
			name:       "multiple trusted hops peel back to the real client",
			remoteAddr: "172.20.0.5:1234",
			xff:        "198.51.100.7, 10.8.0.1, 172.20.0.9",
			trusted:    proxies,
			want:       "198.51.100.7",
		},
		{
			// Everything left of an untrusted hop was appended by something we
			// do not vouch for, so the untrusted hop is where trust stops.
			name:       "unknown hop terminates the trust chain",
			remoteAddr: "172.20.0.5:1234",
			xff:        "1.1.1.1, 203.0.113.50, 172.20.0.9",
			trusted:    proxies,
			want:       "203.0.113.50",
		},
		{
			name:       "malformed XFF entry falls back to the socket peer",
			remoteAddr: "172.20.0.5:1234",
			xff:        "198.51.100.7, not-an-ip",
			trusted:    proxies,
			want:       "172.20.0.5",
		},
		{
			name:       "empty XFF entry falls back to the socket peer",
			remoteAddr: "172.20.0.5:1234",
			xff:        "198.51.100.7, ",
			trusted:    proxies,
			want:       "172.20.0.5",
		},
		{
			name:       "chain of only trusted proxies falls back to the socket peer",
			remoteAddr: "172.20.0.5:1234",
			xff:        "10.8.0.1, 172.20.0.9",
			trusted:    proxies,
			want:       "172.20.0.5",
		},
		{
			// X-Real-IP carries no chain and cannot be validated hop by hop, so
			// it must never be able to override the socket peer.
			name:       "X-Real-IP is ignored even from a trusted proxy",
			remoteAddr: "172.20.0.5:1234",
			xRealIP:    "198.51.100.7",
			trusted:    proxies,
			want:       "172.20.0.5",
		},
		{
			name:       "IPv6 peer without XFF",
			remoteAddr: "[2001:db8::1]:9999",
			trusted:    proxies,
			want:       "2001:db8::1",
		},
		{
			name:       "IPv6 client behind a trusted proxy",
			remoteAddr: "172.20.0.5:1234",
			xff:        "2001:db8:abcd:1234::5",
			trusted:    proxies,
			want:       "2001:db8:abcd:1234::5",
		},
		{
			name:       "RemoteAddr without a port is still parsed",
			remoteAddr: "203.0.113.9",
			trusted:    proxies,
			want:       "203.0.113.9",
		},
		{
			name:       "IPv4-mapped IPv6 peer resolves to its IPv4 form",
			remoteAddr: "[::ffff:203.0.113.9]:44321",
			trusted:    proxies,
			want:       "203.0.113.9",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := reqFrom(tc.remoteAddr, tc.xff)
			if tc.xRealIP != "" {
				r.Header.Set("X-Real-IP", tc.xRealIP)
			}
			got := ResolveClientIP(r, mustCIDRs(t, tc.trusted...))
			if got == nil {
				t.Fatalf("ResolveClientIP returned nil, want %s", tc.want)
			}
			if !got.Equal(net.ParseIP(tc.want)) {
				t.Errorf("ResolveClientIP = %s, want %s", got, tc.want)
			}
		})
	}
}

// TestResolveClientIP_MultiHopCDNChain pins the exact topology where a wrong
// fallback would silently recreate the proxy collapse this resolver exists to
// fix: a real CDN chain where EVERY hop that appears in X-Forwarded-For is
// trusted infrastructure, and only the leftmost entry is the actual client.
//
//	Client 203.0.113.50 -> Cloudflare 198.51.100.20 -> Nginx 10.0.0.10 -> xg2g
//	RemoteAddr:      10.0.0.10
//	X-Forwarded-For: 203.0.113.50, 198.51.100.20
//
// The answer must be the client. Falling back to the socket peer here would put
// every user of the deployment back into a single Nginx-shaped bucket.
func TestResolveClientIP_MultiHopCDNChain(t *testing.T) {
	trusted := mustCIDRs(t, "10.0.0.10", "198.51.100.20")

	r := reqFrom("10.0.0.10:41234", "203.0.113.50, 198.51.100.20")
	got := ResolveClientIP(r, trusted)

	if got == nil || !got.Equal(net.ParseIP("203.0.113.50")) {
		t.Fatalf("ResolveClientIP = %v, want 203.0.113.50 (client behind CDN+reverse proxy)", got)
	}

	// And the collapse itself: distinct clients over the identical hop chain
	// must not share a bucket.
	keyFn := KeyByTrustedProxyChain(trusted)
	a, _ := keyFn(reqFrom("10.0.0.10:1", "203.0.113.50, 198.51.100.20"))
	b, _ := keyFn(reqFrom("10.0.0.10:2", "203.0.113.51, 198.51.100.20"))
	if a == b {
		t.Errorf("two clients over the same CDN chain share a bucket: %q", a)
	}
	if a != "203.0.113.50" {
		t.Errorf("bucket key = %q, want the client IP", a)
	}
}

// TestResolveClientIP_ChainWithoutAnyClient covers the one case that does fall
// back to the socket peer: every X-Forwarded-For entry, including the leftmost,
// is configured trusted infrastructure, so the header names no client at all.
// There is nothing to identify, and the conservative answer is the peer.
//
// This is deliberately NOT the same as TestResolveClientIP_MultiHopCDNChain,
// where the leftmost entry is an untrusted client and must win.
func TestResolveClientIP_ChainWithoutAnyClient(t *testing.T) {
	trusted := mustCIDRs(t, "10.0.0.10", "198.51.100.20")

	r := reqFrom("10.0.0.10:41234", "198.51.100.20, 198.51.100.20")
	got := ResolveClientIP(r, trusted)

	if got == nil || !got.Equal(net.ParseIP("10.0.0.10")) {
		t.Fatalf("ResolveClientIP = %v, want the socket peer 10.0.0.10", got)
	}
}

// TestResolveClientIP_AdversarialChainFailsClosed covers a trusted peer with an
// X-Forwarded-For an attacker has stuffed with junk. The chain cannot be
// interpreted safely, so identification falls back to the socket peer rather
// than guessing at an entry.
func TestResolveClientIP_AdversarialChainFailsClosed(t *testing.T) {
	trusted := mustCIDRs(t, "10.0.0.10", "198.51.100.20")

	adversarial := []string{
		"::1, <script>, 198.51.100.20",
		"203.0.113.50, 999.999.999.999, 198.51.100.20",
		"203.0.113.50, , 198.51.100.20",
		"203.0.113.50, 10.0.0.10/8, 198.51.100.20",
		"' OR 1=1--, 198.51.100.20",
	}

	for _, xff := range adversarial {
		t.Run(xff, func(t *testing.T) {
			got := ResolveClientIP(reqFrom("10.0.0.10:41234", xff), trusted)
			if got == nil || !got.Equal(net.ParseIP("10.0.0.10")) {
				t.Errorf("ResolveClientIP(%q) = %v, want the socket peer 10.0.0.10", xff, got)
			}
		})
	}
}

func TestResolveClientIP_UnparseablePeer(t *testing.T) {
	r := reqFrom("garbage-peer", "198.51.100.7")
	if got := ResolveClientIP(r, mustCIDRs(t, "172.20.0.0/16")); got != nil {
		t.Errorf("expected nil for unparseable RemoteAddr, got %s", got)
	}
}

func TestClientIPKey_Canonicalization(t *testing.T) {
	tests := []struct {
		name string
		ip   string
		want string
	}{
		{"ipv4", "203.0.113.9", "203.0.113.9"},
		// Both spellings of the same host must land in one bucket, otherwise a
		// client gets two rate-limit budgets for free.
		{"ipv4-mapped ipv6 collapses to ipv4", "::ffff:203.0.113.9", "203.0.113.9"},
		// A subscriber typically owns a whole /64, so per-address buckets would
		// make the limiter trivially evadable.
		{"ipv6 masked to /64", "2001:db8:abcd:1234:5678::1", "2001:db8:abcd:1234::"},
		{"ipv6 same /64 shares a bucket", "2001:db8:abcd:1234:ffff::9", "2001:db8:abcd:1234::"},
		{"ipv6 different /64 is a distinct bucket", "2001:db8:abcd:9999::1", "2001:db8:abcd:9999::"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClientIPKey(net.ParseIP(tc.ip)); got != tc.want {
				t.Errorf("ClientIPKey(%s) = %q, want %q", tc.ip, got, tc.want)
			}
		})
	}

	if got := ClientIPKey(nil); got != "unknown" {
		t.Errorf("ClientIPKey(nil) = %q, want %q", got, "unknown")
	}
}

func TestKeyByTrustedProxyChain_DistinctKeysBehindOneProxy(t *testing.T) {
	keyFn := KeyByTrustedProxyChain(mustCIDRs(t, "172.20.0.0/16"))

	a, _ := keyFn(reqFrom("172.20.0.5:1", "198.51.100.7"))
	b, _ := keyFn(reqFrom("172.20.0.5:2", "198.51.100.8"))
	if a == b {
		t.Fatalf("two clients behind one proxy share a bucket: %q", a)
	}

	// Same client, different proxy source ports: one bucket.
	c, _ := keyFn(reqFrom("172.20.0.5:3", "198.51.100.7"))
	if a != c {
		t.Errorf("same client got two buckets: %q vs %q", a, c)
	}
}

func TestKeyByTrustedProxyChain_SpoofedXFFCannotSplitBucket(t *testing.T) {
	// No trusted proxies: a direct client must not be able to mint a new bucket
	// per request by rotating X-Forwarded-For.
	keyFn := KeyByTrustedProxyChain(nil)

	first, _ := keyFn(reqFrom("203.0.113.9:1", "1.1.1.1"))
	second, _ := keyFn(reqFrom("203.0.113.9:2", "2.2.2.2"))
	if first != second {
		t.Errorf("spoofed XFF split the bucket: %q vs %q", first, second)
	}
	if first != "203.0.113.9" {
		t.Errorf("expected socket peer as key, got %q", first)
	}
}

func TestParseCIDRs_RejectsGarbage(t *testing.T) {
	if _, err := ParseCIDRs([]string{"not-a-cidr"}); err == nil {
		t.Fatal("expected an error for a malformed entry")
	}
	nets, err := ParseCIDRs([]string{"10.0.0.0/8", "", "  ", "192.168.1.1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nets) != 2 {
		t.Fatalf("expected 2 nets (blank entries skipped), got %d", len(nets))
	}
	if !IsIPAllowed(net.ParseIP("192.168.1.1"), nets) {
		t.Error("bare IP should become a host route")
	}
	if IsIPAllowed(net.ParseIP("192.168.1.2"), nets) {
		t.Error("bare IP must not widen into a subnet")
	}
}
