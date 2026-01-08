// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import (
	"testing"
)

// TestValidateCIDRList_TrustedProxies_MustFail tests forbidden CIDRs for TrustedProxies
func TestValidateCIDRList_TrustedProxies_MustFail(t *testing.T) {
	tests := []struct {
		name  string
		entry string
	}{
		{"IPv4 any", "0.0.0.0/0"},
		{"IPv6 any", "::/0"},
		{"IPv4 unspecified single", "0.0.0.0"},
		{"IPv4 unspecified CIDR", "0.0.0.0/32"},
		{"IPv6 unspecified single", "::"},
		{"IPv6 unspecified CIDR", "::/128"},
		{"invalid entry", "not-a-cidr"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCIDRList("XG2G_TRUSTED_PROXIES", []string{tt.entry})
			if err == nil {
				t.Errorf("expected error for %q, got nil", tt.entry)
			}
		})
	}
}

// TestValidateCIDRList_TrustedProxies_MustPass tests allowed CIDRs for TrustedProxies
func TestValidateCIDRList_TrustedProxies_MustPass(t *testing.T) {
	tests := []struct {
		name  string
		entry string
	}{
		{"localhost IPv4", "127.0.0.1"},
		{"localhost IPv4 CIDR", "127.0.0.1/32"},
		{"private class A", "10.0.0.0/8"},
		{"private class C", "192.168.0.0/16"},
		{"IPv6 ULA", "fd00::/8"},
		{"IPv6 documentation", "2001:db8::/32"},
		{"empty entry", ""},
		{"whitespace entry", "   "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCIDRList("XG2G_TRUSTED_PROXIES", []string{tt.entry})
			if err != nil {
				t.Errorf("expected no error for %q, got %v", tt.entry, err)
			}
		})
	}
}

// TestValidateCIDRList_Whitelist_MustFail tests forbidden CIDRs for RateLimitWhitelist
func TestValidateCIDRList_Whitelist_MustFail(t *testing.T) {
	tests := []struct {
		name  string
		entry string
	}{
		{"IPv4 any", "0.0.0.0/0"},
		{"IPv6 any", "::/0"},
		{"IPv4 unspecified", "0.0.0.0"},
		{"IPv6 unspecified", "::"},
		{"invalid entry", "foo.bar"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCIDRList("XG2G_RATE_LIMIT_WHITELIST", []string{tt.entry})
			if err == nil {
				t.Errorf("expected error for %q, got nil", tt.entry)
			}
		})
	}
}

// TestValidateCIDRList_Whitelist_MustPass tests allowed CIDRs for RateLimitWhitelist
func TestValidateCIDRList_Whitelist_MustPass(t *testing.T) {
	tests := []struct {
		name  string
		entry string
	}{
		{"private subnet", "192.168.1.0/24"},
		{"class A", "10.0.0.0/8"},
		{"localhost", "127.0.0.1"},
		{"IPv6 ULA", "fd00::/8"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCIDRList("XG2G_RATE_LIMIT_WHITELIST", []string{tt.entry})
			if err != nil {
				t.Errorf("expected no error for %q, got %v", tt.entry, err)
			}
		})
	}
}

// TestValidate_TrustedProxies_Integration tests TrustedProxies validation via Validate()
func TestValidate_TrustedProxies_Integration(t *testing.T) {
	tests := []struct {
		name      string
		proxies   string
		shouldErr bool
	}{
		{"valid single IP", "127.0.0.1", false},
		{"valid CIDR", "10.0.0.0/8", false},
		{"valid multiple", "127.0.0.1,10.0.0.0/8", false},
		{"forbidden any IPv4", "0.0.0.0/0", true},
		{"forbidden any IPv6", "::/0", true},
		{"forbidden unspecified", "0.0.0.0", true},
		{"mixed valid and invalid", "127.0.0.1,0.0.0.0/0", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := AppConfig{
				TrustedProxies: tt.proxies,
				DataDir:        "/tmp",
				Enigma2: Enigma2Settings{
					StreamPort: 8001,
				},
			}
			err := Validate(cfg)
			if tt.shouldErr && err == nil {
				t.Errorf("expected error for %q, got nil", tt.proxies)
			}
			if !tt.shouldErr && err != nil {
				t.Errorf("expected no error for %q, got %v", tt.proxies, err)
			}
		})
	}
}

// TestValidate_RateLimitWhitelist_Integration tests RateLimitWhitelist validation via Validate()
func TestValidate_RateLimitWhitelist_Integration(t *testing.T) {
	tests := []struct {
		name      string
		whitelist []string
		shouldErr bool
	}{
		{"valid single IP", []string{"192.168.1.1"}, false},
		{"valid CIDR", []string{"10.0.0.0/8"}, false},
		{"valid multiple", []string{"127.0.0.1", "10.0.0.0/8"}, false},
		{"forbidden any", []string{"0.0.0.0/0"}, true},
		{"forbidden unspecified", []string{"::"}, true},
		{"mixed valid and invalid", []string{"127.0.0.1", "0.0.0.0/0"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := AppConfig{
				RateLimitWhitelist: tt.whitelist,
				DataDir:            "/tmp",
				Enigma2: Enigma2Settings{
					StreamPort: 8001,
				},
			}
			err := Validate(cfg)
			if tt.shouldErr && err == nil {
				t.Errorf("expected error for %v, got nil", tt.whitelist)
			}
			if !tt.shouldErr && err != nil {
				t.Errorf("expected no error for %v, got %v", tt.whitelist, err)
			}
		})
	}
}
