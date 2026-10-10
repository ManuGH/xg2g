package read

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractServiceRef(t *testing.T) {
	tests := []struct {
		name     string
		rawURL   string
		fallback string
		want     string
		desc     string
	}{
		{
			name:   "Test 18: Query Ref Wins",
			rawURL: "http://host/stream/ignored?ref=1:0:1:TEST",
			want:   "1:0:1:TEST",
			desc:   "Query parameter 'ref' must take precedence over path",
		},
		{
			name:   "Test 19: Path Fallback",
			rawURL: "http://host/stream/1:0:1:PATH",
			want:   "1:0:1:PATH",
			desc:   "Last path segment used when ref query is missing",
		},
		{
			name:   "Test 20: Unparseable URL Fallback",
			rawURL: "://invalid-url/some/path/1:0:1:RAW",
			want:   "1:0:1:RAW",
			desc:   "Should split by slash even if URL parsing fails (best effort)",
		},
		{
			name:     "Test 21: Empty Returns Fallback",
			rawURL:   "http://host/stream/", // Last segment is empty
			fallback: "FALLBACK_ID",
			want:     "FALLBACK_ID",
			desc:     "Empty extraction result should return fallback",
		},
		{
			name:   "Test 22: Trims Trailing Colon",
			rawURL: "http://host/stream/1:0:1:COLON:",
			want:   "1:0:1:COLON",
			desc:   "Trailing colon must be removed (Enigma2 drift)",
		},
		{
			name:   "Complex Query Precedence",
			rawURL: "http://host/stream/pathref?other=1&ref=QUERY_REF",
			want:   "QUERY_REF",
			desc:   "Query ref still wins amidst other params",
		},
		{
			name:   "Simple Filename",
			rawURL: "stream.mp4",
			want:   "stream.mp4",
			desc:   "Simple filename treated as path",
		},
		{
			name:   "Enigma Opaque Ref",
			rawURL: "1:0:1:ABCD:1:1:0:0:0:0:",
			want:   "1:0:1:ABCD:1:1:0:0:0:0", // Trims colon, treats as raw path
			desc:   "Opaque ref without scheme should be treated as raw string (not parsed as URL scheme)",
		},
		{
			name:   "Enigma IPTV Stream URL With Slashes",
			rawURL: "http://127.0.0.1:8001/4097:0:1:0:0:0:0:0:0:0:http%3a//canary.invalid/SECRET-CANARY-1/live/token-xyz-987/channel_prime.ts:Canary Channel 1",
			want:   "4097:0:1:0:0:0:0:0:0:0:http%3a//canary.invalid/SECRET-CANARY-1/live/token-xyz-987/channel_prime.ts:Canary Channel 1",
			desc:   "IPTV reference in URL path with nested slashes must not be truncated to last path segment",
		},
		{
			name:   "Proxy IPTV Stream URL",
			rawURL: "http://xg2g:8088/stream/5001:0:1:0:0:0:0:0:0:0:http%3a//provider.com/live/ch1.m3u8:Ch 1",
			want:   "5001:0:1:0:0:0:0:0:0:0:http%3a//provider.com/live/ch1.m3u8:Ch 1",
			desc:   "Proxy IPTV URL preserves full 5001 reference",
		},
		{
			name:   "Opaque IPTV Stream URL",
			rawURL: "http://xg2g:8088/api/v3/stream/live/iptv_test123",
			want:   "iptv_test123",
			desc:   "Opaque IPTV URL extracts iptv_ ID cleanly",
		},
		{
			name:   "IPTV Stream URL With Query Parameters",
			rawURL: "http://127.0.0.1:8001/4097:0:1:0:0:0:0:0:0:0:http%3a//canary.invalid/live/stream.m3u8" + "?" + "token=xyz123&exp=456:Canary Channel",
			want:   "4097:0:1:0:0:0:0:0:0:0:http%3a//canary.invalid/live/stream.m3u8" + "?" + "token=xyz123&exp=456:Canary Channel",
			desc:   "IPTV stream URL with query parameters must preserve query strings and not strip token",
		},
		{
			name:   "Raw IPTV Reference With Token Query",
			rawURL: "4097:0:1:0:0:0:0:0:0:0:http%3a//provider.com/live/ch1.m3u8" + "?" + "token=secret_abc:Ch 1",
			want:   "4097:0:1:0:0:0:0:0:0:0:http%3a//provider.com/live/ch1.m3u8" + "?" + "token=secret_abc:Ch 1",
			desc:   "Raw IPTV reference starting with 4097: must preserve query string parameters",
		},
		{
			name:   "Outer URL Escaped IPTV Stream URL From StreamURL Builder",
			rawURL: "http://receiver.invalid:8001/4097:0:1:0:0:0:0:0:0:0:http%253a//canary.invalid/SECRET-CANARY-1/live/token-xyz-987/2171.ts:DE:%20RTL%20HD",
			want:   "4097:0:1:0:0:0:0:0:0:0:http%3a//canary.invalid/SECRET-CANARY-1/live/token-xyz-987/2171.ts:DE: RTL HD",
			desc:   "Outer URL path escaping (%253a and %20 from url.URL.String) must be unescaped once to match raw Enigma2 IPTV serviceRef",
		},
		{
			name:   "Outer URL Escaped IPTV Stream URL With Encoded Query",
			rawURL: "http://receiver.invalid:8001/5002:0:1:0:0:0:0:0:0:0:https%253a//canary.invalid/live/stream.m3u8%3Ftoken=xyz123&exp=456:Canary%20Channel",
			want:   "5002:0:1:0:0:0:0:0:0:0:https%3a//canary.invalid/live/stream.m3u8" + "?" + "token=xyz123&exp=456:Canary Channel",
			desc:   "Outer URL path escaping with %253a and %3F must unescape to single-encoded IPTV reference",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractServiceRef(tt.rawURL, tt.fallback)
			assert.Equal(t, tt.want, got, tt.desc)
		})
	}
}

func TestCanonicalServiceRef(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "already canonical",
			in:   "1:0:1:ABCD:1:1:0:0:0:0",
			want: "1:0:1:ABCD:1:1:0:0:0:0",
		},
		{
			name: "single trailing colon",
			in:   "1:0:1:ABCD:1:1:0:0:0:0:",
			want: "1:0:1:ABCD:1:1:0:0:0:0",
		},
		{
			name: "double trailing colon",
			in:   "1:0:1:ABCD:1:1:0:0:0:0::",
			want: "1:0:1:ABCD:1:1:0:0:0:0",
		},
		{
			name: "whitespace trimmed",
			in:   "  1:0:1:ABCD:1:1:0:0:0:0:  ",
			want: "1:0:1:ABCD:1:1:0:0:0:0",
		},
		{
			name: "dvb refs are uppercased",
			in:   "1:0:1:ef11:abcd:1:c00000:0:0:0:",
			want: "1:0:1:EF11:ABCD:1:C00000:0:0:0",
		},
		{
			name: "url refs keep case",
			in:   "http://example.com/AbCd/stream.ts:",
			want: "http://example.com/AbCd/stream.ts",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, CanonicalServiceRef(tt.in))
		})
	}
}
