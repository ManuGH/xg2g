// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import (
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/validate"
)

// ForbiddenRule defines a rule that rejects semantically invalid combinations of options.
type ForbiddenRule struct {
	Name           string
	Predicate      func(cfg AppConfig) bool
	ProblemDetails string
}

var forbiddenRules = []ForbiddenRule{
	{
		Name: "HTTPS_WITHOUT_TLS",
		Predicate: func(cfg AppConfig) bool {
			// Fix 1: Proxy-Aware HTTPS. Allow if TrustedProxies is set, as TLS might be upstream.
			return cfg.ForceHTTPS && !cfg.TLSEnabled && strings.TrimSpace(cfg.TrustedProxies) == ""
		},
		ProblemDetails: "ForceHTTPS is enabled but TLSEnabled is false and no TrustedProxies are configured. HTTPS redirect will fail in non-proxy environments.",
	},
	{
		Name: "EPG_ENABLED_WITHOUT_DAYS",
		Predicate: func(cfg AppConfig) bool {
			return cfg.EPGEnabled && cfg.EPGDays <= 0
		},
		ProblemDetails: "EPG is enabled but EPGDays is <= 0.",
	},
	{
		Name: "DATA_COLLISION",
		Predicate: func(cfg AppConfig) bool {
			return cfg.HLS.Root != "" && cfg.Store.Path != "" && cfg.HLS.Root == cfg.Store.Path
		},
		ProblemDetails: "HLS Root and Store Path must not be the same directory.",
	},
	{
		Name: "VOD_CONCURRENCY_NEGATIVE",
		Predicate: func(cfg AppConfig) bool {
			return cfg.VODMaxConcurrent < 0
		},
		ProblemDetails: "VODMaxConcurrent must be >= 0 (0 = unlimited).",
	},
}

// Validate validates a AppConfig using the centralized validation package
func Validate(cfg AppConfig) error {
	v := validate.New()

	// Check Forbidden Combinations (P1.2)
	for _, rule := range forbiddenRules {
		if rule.Predicate(cfg) {
			v.AddError(rule.Name, rule.ProblemDetails, "")
		}
	}

	// Enigma2 URL (Standardized)
	if strings.TrimSpace(cfg.Enigma2.BaseURL) != "" {
		v.URL("Enigma2.BaseURL", cfg.Enigma2.BaseURL, []string{"http", "https"})
	}

	// Stream port
	// Stream port (0 = allowed, means use /web mechanism)
	if cfg.Enigma2.StreamPort != 0 {
		v.Port("Enigma2.StreamPort", cfg.Enigma2.StreamPort)
	}

	// Data directory
	v.Directory("DataDir", cfg.DataDir, false)

	// EPG settings (if enabled)
	if cfg.EPGEnabled {
		v.Range("EPGDays", cfg.EPGDays, 1, 14)
		v.Range("EPGMaxConcurrency", cfg.EPGMaxConcurrency, 1, 10)
		// EPGTimeoutMS must be between 100ms and 60s for safety
		v.Range("EPGTimeoutMS", cfg.EPGTimeoutMS, 100, 60000)
		// EPGRetries should be reasonable (0-5)
		v.Range("EPGRetries", cfg.EPGRetries, 0, 5)
		// FuzzyMax for fuzzy matching (0-10 is reasonable)
		v.Range("FuzzyMax", cfg.FuzzyMax, 0, 10)
	}

	// Enigma2 retries
	v.Range("Enigma2.Retries", cfg.Enigma2.Retries, 0, 10)

	// Validate TLS Configuration
	if cfg.TLSEnabled {
		// If TLS is enabled, require both cert and key, or neither (autogen)
		hasCert := strings.TrimSpace(cfg.TLSCert) != ""
		hasKey := strings.TrimSpace(cfg.TLSKey) != ""

		if hasCert != hasKey {
			v.AddError("TLS", "TLS enabled requires both cert and key, or none for autogen", "")
		}
	}

	// Validate file paths for security
	v.Path("XMLTVPath", cfg.XMLTVPath)

	// Validate Trusted Proxies (CIDR list)
	if cfg.TrustedProxies != "" {
		entries := strings.Split(cfg.TrustedProxies, ",")
		if err := validateCIDRList("XG2G_TRUSTED_PROXIES", entries); err != nil {
			v.AddError("TrustedProxies", err.Error(), "")
		}
	}

	// Validate Rate Limit Whitelist (CIDR list)
	if err := validateCIDRList("XG2G_RATE_LIMIT_WHITELIST", cfg.RateLimitWhitelist); err != nil {
		v.AddError("RateLimitWhitelist", err.Error(), "")
	}

	if cfg.apiTokensParseErr != nil {
		v.AddError("APITokens", cfg.apiTokensParseErr.Error(), "")
	}

	validScopes := map[string]struct{}{
		"*":         {},
		"v3:*":      {},
		"v3:read":   {},
		"v3:write":  {},
		"v3:admin":  {},
		"v3:status": {},
	}

	isValidScope := func(scope string) bool {
		scope = strings.ToLower(strings.TrimSpace(scope))
		_, ok := validScopes[scope]
		return ok
	}

	if cfg.APIToken != "" && len(cfg.APITokenScopes) == 0 {
		v.AddError("APITokenScopes", "must be set when APIToken is configured", "")
	}
	for _, scope := range cfg.APITokenScopes {
		if !isValidScope(scope) {
			v.AddError("APITokenScopes", "unknown scope", scope)
		}
	}
	seenTokens := map[string]struct{}{}
	for _, token := range cfg.APITokens {
		tokenVal := strings.TrimSpace(token.Token)
		if tokenVal == "" {
			v.AddError("APITokens", "token must not be empty", "")
			continue
		}
		if _, ok := seenTokens[tokenVal]; ok {
			v.AddError("APITokens", "duplicate token", tokenVal)
			continue
		}
		seenTokens[tokenVal] = struct{}{}
		if len(token.Scopes) == 0 {
			v.AddError("APITokens", "scopes must be set for token", tokenVal)
			continue
		}
		for _, scope := range token.Scopes {
			if !isValidScope(scope) {
				v.AddError("APITokens", "unknown scope", scope)
			}
		}
	}

	// Validate V3 Engine paths if enabled (Fail Fast)
	if cfg.Engine.Enabled {
		v.WritableDirectory("Store.Path", cfg.Store.Path, false)
		v.WritableDirectory("HLS.Root", cfg.HLS.Root, false)

		// HLS Segment Integrity (Best Practice 2026)
		switch cfg.HLS.SegmentSeconds {
		case 1: // Low Latency Profile
			if cfg.HLS.DVRWindow < 10*time.Second {
				v.AddError("HLS.DVRWindow", "must be >= 10s for low latency", cfg.HLS.DVRWindow)
			}
		case 6: // Standard Profile
			if cfg.HLS.DVRWindow < 1*time.Minute {
				v.AddError("HLS.DVRWindow", "must be >= 1m for standard profile", cfg.HLS.DVRWindow)
			}
		default:
			v.AddError("HLS.SegmentSeconds", "must be 1 (Low Latency) or 6 (Standard Profile)", cfg.HLS.SegmentSeconds)
		}

		if cfg.Engine.IdleTimeout < 0 {
			v.AddError("Engine.IdleTimeout", "must be >= 0", cfg.Engine.IdleTimeout)
		}
		if cfg.Enigma2.TuneTimeout < 0 {
			v.AddError("Enigma2.TuneTimeout", "must be >= 0", cfg.Enigma2.TuneTimeout)
		}
		if cfg.Enigma2.Timeout < 0 {
			v.AddError("Enigma2.Timeout", "must be >= 0", cfg.Enigma2.Timeout)
		}
		if cfg.Enigma2.ResponseHeaderTimeout < 0 {
			v.AddError("Enigma2.ResponseHeaderTimeout", "must be >= 0", cfg.Enigma2.ResponseHeaderTimeout)
		}
		v.Range("Enigma2.Retries", cfg.Enigma2.Retries, 0, 10)
		if cfg.Enigma2.Backoff < 0 {
			v.AddError("Enigma2.Backoff", "must be >= 0", cfg.Enigma2.Backoff)
		}
		if cfg.Enigma2.MaxBackoff < 0 {
			v.AddError("Enigma2.MaxBackoff", "must be >= 0", cfg.Enigma2.MaxBackoff)
		}
		if cfg.Enigma2.Backoff > 0 && cfg.Enigma2.MaxBackoff > 0 && cfg.Enigma2.Backoff > cfg.Enigma2.MaxBackoff {
			v.AddError("Enigma2.MaxBackoff", "must be >= Enigma2.Backoff", cfg.Enigma2.MaxBackoff)
		}
		if cfg.Enigma2.RateLimit < 0 {
			v.AddError("Enigma2.RateLimit", "must be >= 0", cfg.Enigma2.RateLimit)
		}
		if cfg.Enigma2.RateBurst < 0 {
			v.AddError("Enigma2.RateBurst", "must be >= 0", cfg.Enigma2.RateBurst)
		}
	}

	if cfg.ConfigStrict {
		switch cfg.Engine.Mode {
		case "standard", "virtual":
		default:
			v.AddError("Engine.Mode", "must be standard or virtual", cfg.Engine.Mode)
		}

		switch cfg.Store.Backend {
		case "memory", "bolt":
		default:
			v.AddError("Store.Backend", "must be memory or bolt", cfg.Store.Backend)
		}

		v.URL("Enigma2.BaseURL", cfg.Enigma2.BaseURL, []string{"http", "https"})
	}

	if !v.IsValid() {
		return v.Err()
	}

	// Validate Streaming Config (ADR-00X: universal policy only)
	if cfg.Streaming.DeliveryPolicy != "universal" {
		v.AddError("Streaming.DeliveryPolicy",
			"only 'universal' policy is supported (ADR-00X)",
			cfg.Streaming.DeliveryPolicy)
	}

	if !v.IsValid() {
		return v.Err()
	}

	return nil
}
