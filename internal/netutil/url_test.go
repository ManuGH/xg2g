// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package netutil

import (
	"testing"
)

func TestParseDirectHTTPURL(t *testing.T) {
	tests := []struct {
		input  string
		wantOK bool
	}{
		{"http://10.10.55.64:8001/foo", true},
		{"https://example.com/bar", true},
		{"https://[2001:db8::1]:8443/a", true},
		{" HTTP://example.com ", true},          // whitespace and case
		{"http://example.com#frag", false},      // fragment
		{"ftp://example.com", false},            // wrong scheme
		{"http://user:pass@example.com", false}, // credentials
		{"10.10.55.64:8001/foo", false},         // no scheme
		{"http:///a", false},                    // empty host
		{"http://", false},                      // empty host
		{"javascript:alert(1)", false},          // wrong scheme
		// {"file:///etc/passwd", false},           // wrong scheme
		{"", false}, // empty
	}

	for _, tt := range tests {
		u, ok := ParseDirectHTTPURL(tt.input)
		if ok != tt.wantOK {
			t.Errorf("ParseDirectHTTPURL(%q) ok = %v, want %v", tt.input, ok, tt.wantOK)
		}
		if ok && u == nil {
			t.Errorf("ParseDirectHTTPURL(%q) returned nil url but ok=true", tt.input)
		}
	}
}

func TestNormalizeAuthority(t *testing.T) {
	tests := []struct {
		input     string
		wantHost  string
		wantPort  string
		wantError bool
	}{
		{"http://10.10.55.64", "10.10.55.64", "", false},
		{"http://10.10.55.64:80", "10.10.55.64", "80", false},
		{"10.10.55.64:80", "10.10.55.64", "80", false},
		{"10.10.55.64", "10.10.55.64", "", false},
		{"[2001:db8::1]:80", "2001:db8::1", "80", false},
		{"https://[2001:db8::1]:8443", "2001:db8::1", "8443", false},
		{"", "", "", true},
	}

	for _, tt := range tests {
		host, port, err := NormalizeAuthority(tt.input, "http")
		if (err != nil) != tt.wantError {
			t.Errorf("NormalizeAuthority(%q) error = %v, wantError %v", tt.input, err, tt.wantError)
			continue
		}
		if host != tt.wantHost {
			t.Errorf("NormalizeAuthority(%q) host = %q, want %q", tt.input, host, tt.wantHost)
		}
		if port != tt.wantPort {
			t.Errorf("NormalizeAuthority(%q) port = %q, want %q", tt.input, port, tt.wantPort)
		}
	}
}
