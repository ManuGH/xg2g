// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// SPDX-License-Identifier: MIT
package main

import "testing"

func TestMaskURL(t *testing.T) {
	tests := []struct {
		name    string
		rawURL  string
		want    string
		wantErr bool
	}{
		{
			name:   "http URL without credentials",
			rawURL: "http://example.com:8080",
			want:   "http://example.com:8080",
		},
		{
			name:   "https URL without credentials",
			rawURL: "https://example.com:443/path",
			want:   "https://example.com:443/path",
		},
		{
			name:   "URL with username and password",
			rawURL: "http://user:pass@example.com:8080",
			want:   "http://example.com:8080",
		},
		{
			name:   "URL with only username",
			rawURL: "http://user@example.com:8080/path",
			want:   "http://example.com:8080/path",
		},
		{
			name:   "URL with complex credentials",
			rawURL: "https://admin:secret123@192.168.1.100:8080/api",
			want:   "https://192.168.1.100:8080/api",
		},
		{
			name:   "URL with special characters in password",
			rawURL: "http://user:p@ss%20word@example.com",
			want:   "http://example.com",
		},
		{
			name:   "plain text (parsed as relative path)",
			rawURL: "not a url",
			want:   "not%20a%20url", // url.Parse encodes spaces but doesn't error
		},
		{
			name:   "empty URL",
			rawURL: "",
			want:   "", // Empty URLs parse successfully as relative URLs
		},
		{
			name:   "IPv6 address",
			rawURL: "http://[::1]:8080/path",
			want:   "http://[::1]:8080/path",
		},
		{
			name:   "URL with fragment",
			rawURL: "http://user:pass@example.com:8080/path#fragment",
			want:   "http://example.com:8080/path#fragment",
		},
		{
			name:   "URL with query parameters",
			rawURL: "http://user:pass@example.com:8080/path?key=value",
			want:   "http://example.com:8080/path?key=value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := maskURL(tt.rawURL)
			if got != tt.want {
				t.Errorf("maskURL(%q) = %q, want %q", tt.rawURL, got, tt.want)
			}
		})
	}
}
