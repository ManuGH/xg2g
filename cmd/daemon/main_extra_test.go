// SPDX-License-Identifier: MIT
package main

import (
	"os"
	"testing"
)

func TestMaskURL(t *testing.T) {
	tests := []struct {
		in  string
		out string
	}{
		{"http://user:pass@example.com:8080/path?x=1", "http://example.com:8080/path?x=1"},
		{"https://example.com", "https://example.com"},
		{"://%%%", "invalid-url-redacted"},
	}
	for _, tt := range tests {
		got := maskURL(tt.in)
		if got != tt.out {
			t.Fatalf("maskURL(%q) = %q, want %q", tt.in, got, tt.out)
		}
	}
}

func TestEnvBehavior(t *testing.T) {
	const key = "XG2G_TEST_KEY"
	// Ensure clean env
	_ = os.Unsetenv(key)
	if got := env(key, "default"); got != "default" {
		t.Fatalf("env unset: got %q, want default", got)
	}

	// Set non-empty
	if err := os.Setenv(key, "value"); err != nil {
		t.Fatal(err)
	}
	if got := env(key, "default"); got != "value" {
		t.Fatalf("env set: got %q, want value", got)
	}

	// Set empty → should return default
	if err := os.Setenv(key, ""); err != nil {
		t.Fatal(err)
	}
	if got := env(key, "default"); got != "default" {
		t.Fatalf("env empty: got %q, want default", got)
	}

	// Sensitive variable name should still return the set value
	const tok = "XG2G_API_TOKEN"
	if err := os.Setenv(tok, "secret"); err != nil {
		t.Fatal(err)
	}
	if got := env(tok, ""); got != "secret" {
		t.Fatalf("env token: got %q, want secret", got)
	}
}
