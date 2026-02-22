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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, CanonicalServiceRef(tt.in))
		})
	}
}
