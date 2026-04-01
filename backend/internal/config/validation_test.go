// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import (
	"strings"
	"testing"
	"time"
)

func baseValidationConfig() AppConfig {
	return AppConfig{
		DataDir: "/tmp",
		Enigma2: Enigma2Settings{
			StreamPort: 8001,
		},
		Limits: LimitsConfig{
			MaxSessions:   10,
			MaxTranscodes: 5,
		},
		Timeouts: TimeoutsConfig{
			TranscodeStart:      10 * time.Second,
			TranscodeNoProgress: 30 * time.Second,
			KillGrace:           5 * time.Second,
		},
		Breaker: BreakerConfig{
			Window:            60 * time.Second,
			MinAttempts:       10,
			FailuresThreshold: 5,
		},
		Streaming: StreamingConfig{
			DeliveryPolicy: "universal",
		},
		VODCacheMaxEntries: 256,
	}
}

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
			cfg := baseValidationConfig()
			cfg.TrustedProxies = tt.proxies
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
			cfg := baseValidationConfig()
			cfg.RateLimitWhitelist = tt.whitelist
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

func TestValidate_OutboundPolicy(t *testing.T) {
	t.Run("enabled without allowlist", func(t *testing.T) {
		cfg := baseValidationConfig()
		cfg.Network.Outbound.Enabled = true
		err := Validate(cfg)
		if err == nil {
			t.Fatal("expected error for missing outbound allowlist")
		}
	})

	t.Run("enabled with allowlist", func(t *testing.T) {
		cfg := baseValidationConfig()
		cfg.Network.Outbound.Enabled = true
		cfg.Network.Outbound.Allow.Hosts = []string{"192.0.2.10"}
		cfg.Network.Outbound.Allow.Ports = []int{80}
		cfg.Network.Outbound.Allow.Schemes = []string{"http"}
		err := Validate(cfg)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})
}

func TestValidate_PlaybackOperatorSourceRules(t *testing.T) {
	t.Run("valid exact rule", func(t *testing.T) {
		cfg := baseValidationConfig()
		cfg.Playback.Operator.SourceRules = []PlaybackOperatorRuleConfig{
			{
				Name:        "monk-live",
				Mode:        "live",
				ServiceRef:  "1:0:1:ABC",
				ForceIntent: "repair",
			},
		}
		if err := Validate(cfg); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("valid prefix rule with bool override", func(t *testing.T) {
		cfg := baseValidationConfig()
		disabled := true
		cfg.Playback.Operator.SourceRules = []PlaybackOperatorRuleConfig{
			{
				Name:                  "recordings-safe",
				Mode:                  "recording",
				ServiceRefPrefix:      "1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/",
				DisableClientFallback: &disabled,
			},
		}
		if err := Validate(cfg); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("missing name fails", func(t *testing.T) {
		cfg := baseValidationConfig()
		cfg.Playback.Operator.SourceRules = []PlaybackOperatorRuleConfig{
			{
				Mode:        "live",
				ServiceRef:  "1:0:1:ABC",
				ForceIntent: "repair",
			},
		}
		if err := Validate(cfg); err == nil {
			t.Fatal("expected error for missing rule name")
		}
	})

	t.Run("exact and prefix together fails", func(t *testing.T) {
		cfg := baseValidationConfig()
		cfg.Playback.Operator.SourceRules = []PlaybackOperatorRuleConfig{
			{
				Name:             "broken",
				Mode:             "live",
				ServiceRef:       "1:0:1:ABC",
				ServiceRefPrefix: "1:0:1:",
				ForceIntent:      "repair",
			},
		}
		if err := Validate(cfg); err == nil {
			t.Fatal("expected error for exact+prefix matcher")
		}
	})

	t.Run("invalid mode fails", func(t *testing.T) {
		cfg := baseValidationConfig()
		cfg.Playback.Operator.SourceRules = []PlaybackOperatorRuleConfig{
			{
				Name:        "broken",
				Mode:        "tv",
				ServiceRef:  "1:0:1:ABC",
				ForceIntent: "repair",
			},
		}
		if err := Validate(cfg); err == nil {
			t.Fatal("expected error for invalid mode")
		}
	})

	t.Run("unknown override values fail", func(t *testing.T) {
		cfg := baseValidationConfig()
		cfg.Playback.Operator.SourceRules = []PlaybackOperatorRuleConfig{
			{
				Name:           "broken",
				Mode:           "live",
				ServiceRef:     "1:0:1:ABC",
				ForceIntent:    "turbo",
				MaxQualityRung: "ultra",
			},
		}
		if err := Validate(cfg); err == nil {
			t.Fatal("expected error for unknown override values")
		}
	})

	t.Run("missing override fields fails", func(t *testing.T) {
		cfg := baseValidationConfig()
		cfg.Playback.Operator.SourceRules = []PlaybackOperatorRuleConfig{
			{
				Name:       "noop",
				Mode:       "any",
				ServiceRef: "1:0:1:ABC",
			},
		}
		if err := Validate(cfg); err == nil {
			t.Fatal("expected error for noop source rule")
		}
	})
}

func TestValidate_MonetizationRequiredScopes(t *testing.T) {
	cfg := baseValidationConfig()
	cfg.Monetization = MonetizationConfig{
		Enabled:        true,
		Model:          MonetizationModelOneTimeUnlock,
		Enforcement:    MonetizationEnforcementRequired,
		RequiredScopes: []string{"xg2g:unlock"},
	}
	cfg.APITokens = []ScopedToken{
		{
			Token:  "unlock-token",
			Scopes: []string{"v3:admin", "xg2g:unlock"},
		},
	}

	if err := Validate(cfg); err != nil {
		t.Fatalf("expected monetization required scopes to be accepted, got %v", err)
	}
}

func TestValidate_MonetizationRequiredScopesEmptyFails(t *testing.T) {
	cfg := baseValidationConfig()
	cfg.Monetization = MonetizationConfig{
		Enabled:        true,
		Model:          MonetizationModelOneTimeUnlock,
		Enforcement:    MonetizationEnforcementRequired,
		RequiredScopes: []string{},
	}

	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected monetization required scopes error")
	}
	if !strings.Contains(err.Error(), "Monetization.RequiredScopes") {
		t.Fatalf("expected required scopes validation error, got %v", err)
	}
}

func TestValidate_MonetizationRequiredScopesDuplicateFails(t *testing.T) {
	cfg := baseValidationConfig()
	cfg.Monetization = MonetizationConfig{
		Enabled:        true,
		Model:          MonetizationModelOneTimeUnlock,
		Enforcement:    MonetizationEnforcementRequired,
		RequiredScopes: []string{"xg2g:unlock", "XG2G:UNLOCK"},
	}

	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected duplicate monetization required scopes error")
	}
	if !strings.Contains(err.Error(), "entries must be unique") {
		t.Fatalf("expected duplicate scope validation error, got %v", err)
	}
}

func TestValidate_MonetizationRequiredScopesEmptyEntryFails(t *testing.T) {
	cfg := baseValidationConfig()
	cfg.Monetization = MonetizationConfig{
		Enabled:        true,
		Model:          MonetizationModelOneTimeUnlock,
		Enforcement:    MonetizationEnforcementRequired,
		RequiredScopes: []string{"xg2g:unlock", "   "},
	}

	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected empty monetization required scope entry error")
	}
	if !strings.Contains(err.Error(), "entries must not be empty") {
		t.Fatalf("expected empty scope validation error, got %v", err)
	}
}

func TestValidate_MonetizationRequiredNeedsPaidModel(t *testing.T) {
	cfg := baseValidationConfig()
	cfg.Monetization = MonetizationConfig{
		Enabled:     true,
		Model:       MonetizationModelFree,
		Enforcement: MonetizationEnforcementRequired,
	}

	if err := Validate(cfg); err == nil {
		t.Fatal("expected monetization enforcement error")
	}
}

func TestValidate_MonetizationGooglePlayMappingsRequireProviderConfig(t *testing.T) {
	cfg := baseValidationConfig()
	cfg.Monetization = MonetizationConfig{
		Enabled:        true,
		Model:          MonetizationModelOneTimeUnlock,
		Enforcement:    MonetizationEnforcementRequired,
		RequiredScopes: []string{"xg2g:unlock"},
		ProductMappings: []MonetizationProductMapping{
			{Provider: "google_play", ProductID: "xg2g.unlock", Scopes: []string{"xg2g:unlock"}},
		},
	}

	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error for missing google play config")
	}
	if !strings.Contains(err.Error(), "Monetization.GooglePlay.PackageName") {
		t.Fatalf("expected Monetization.GooglePlay.PackageName validation error, got %v", err)
	}
	if !strings.Contains(err.Error(), "Monetization.GooglePlay.ServiceAccountCredentialsFile") {
		t.Fatalf("expected Monetization.GooglePlay.ServiceAccountCredentialsFile validation error, got %v", err)
	}
}

func TestValidate_MonetizationGooglePlayMappingsDuplicateFails(t *testing.T) {
	cfg := baseValidationConfig()
	cfg.Monetization = MonetizationConfig{
		Enabled:        true,
		Model:          MonetizationModelOneTimeUnlock,
		Enforcement:    MonetizationEnforcementRequired,
		RequiredScopes: []string{"xg2g:unlock"},
		GooglePlay: MonetizationGooglePlayConfig{
			PackageName:                   "io.github.manugh.xg2g.android",
			ServiceAccountCredentialsFile: "/tmp/google-play-service-account.json",
		},
		ProductMappings: []MonetizationProductMapping{
			{Provider: "google_play", ProductID: "xg2g.unlock", Scopes: []string{"xg2g:unlock"}},
			{Provider: "google_play", ProductID: "xg2g.unlock", Scopes: []string{"xg2g:dvr"}},
		},
	}

	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error for duplicate provider/product mapping")
	}
	if !strings.Contains(err.Error(), "Monetization.ProductMappings") {
		t.Fatalf("expected Monetization.ProductMappings validation error, got %v", err)
	}
}

func TestValidate_MonetizationAmazonMappingsRequireProviderConfig(t *testing.T) {
	cfg := baseValidationConfig()
	cfg.Monetization = MonetizationConfig{
		Enabled:        true,
		Model:          MonetizationModelOneTimeUnlock,
		Enforcement:    MonetizationEnforcementRequired,
		RequiredScopes: []string{"xg2g:unlock"},
		ProductMappings: []MonetizationProductMapping{
			{Provider: "amazon_appstore", ProductID: "xg2g.unlock.firetv", Scopes: []string{"xg2g:unlock"}},
		},
	}

	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error for missing Amazon config")
	}
	if !strings.Contains(err.Error(), "Monetization.Amazon.SharedSecretFile") {
		t.Fatalf("expected Monetization.Amazon.SharedSecretFile validation error, got %v", err)
	}
}
