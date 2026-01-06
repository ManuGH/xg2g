// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package net

import (
	"testing"
)

func TestParseDirectHTTPURL(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"http://example.com", true},
		{"https://example.com/stream", true},
		{"http://127.0.0.1:8080", true},
		{"ftp://example.com", false},
		{"file:///etc/passwd", false},
		{"/local/path", false},
		{"", false},
		{"http://user:pass@example.com", false}, // No credentials allowed
		{"http://example.com#fragment", false},  // No fragments allowed
	}

	for _, tt := range tests {
		_, ok := ParseDirectHTTPURL(tt.input)
		if ok != tt.want {
			t.Errorf("ParseDirectHTTPURL(%q) = %v; want %v", tt.input, ok, tt.want)
		}
	}
}

func TestNormalizeAuthority(t *testing.T) {
	tests := []struct {
		input      string
		defaultSch string
		wantHost   string
		wantPort   string
		wantErr    bool
	}{
		{"example.com", "http", "example.com", "", false},
		{"example.com:8080", "http", "example.com", "8080", false},
		{"http://example.com", "", "example.com", "", false},
		{"https://127.0.0.1:9090", "", "127.0.0.1", "9090", false},
		{"[::1]:8001", "http", "::1", "8001", false}, // IPv6
		{"", "http", "", "", true},
		{"http://", "", "", "", true},
	}

	for _, tt := range tests {
		h, p, err := NormalizeAuthority(tt.input, tt.defaultSch)
		if (err != nil) != tt.wantErr {
			t.Errorf("NormalizeAuthority(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if !tt.wantErr {
			if h != tt.wantHost {
				t.Errorf("NormalizeAuthority(%q) host = %q, want %q", tt.input, h, tt.wantHost)
			}
			if p != tt.wantPort {
				t.Errorf("NormalizeAuthority(%q) port = %q, want %q", tt.input, p, tt.wantPort)
			}
		}
	}
}
