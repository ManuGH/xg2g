// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

// Package config provides configuration management for xg2g.
package config

import (
	"net"
	"strings"

	"github.com/ManuGH/xg2g/internal/validate"
)

// Validate validates a AppConfig using the centralized validation package
func Validate(cfg AppConfig) error {
	v := validate.New()

	// OpenWebIF URL (optional for setup mode)
	if strings.TrimSpace(cfg.OWIBase) != "" {
		v.URL("OWIBase", cfg.OWIBase, []string{"http", "https"})
	}

	// Stream port
	v.Port("StreamPort", cfg.StreamPort)

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

	// OWI retries
	v.Range("OWIRetries", cfg.OWIRetries, 0, 10)

	// Validate file paths for security
	v.Path("XMLTVPath", cfg.XMLTVPath)

	// Rate limit whitelist entries must be valid IPs or CIDRs
	for _, entry := range cfg.RateLimitWhitelist {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if net.ParseIP(entry) != nil {
			continue
		}
		if _, _, err := net.ParseCIDR(entry); err == nil {
			continue
		}
		v.AddError("RateLimitWhitelist", "must be a valid IP or CIDR", entry)
	}

	if cfg.apiTokensParseErr != nil {
		v.AddError("APITokens", cfg.apiTokensParseErr.Error(), "")
	}

	validScopes := map[string]struct{}{
		"*":        {},
		"v3:*":     {},
		"v3:read":  {},
		"v3:write": {},
		"v3:admin": {},
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

	// Validate V3 Worker paths if enabled (Fail Fast)
	if cfg.WorkerEnabled {
		v.WritableDirectory("StorePath", cfg.StorePath, false)
		v.WritableDirectory("HLSRoot", cfg.HLSRoot, false)
		if cfg.V3IdleTimeout < 0 {
			v.AddError("V3IdleTimeout", "must be >= 0", cfg.V3IdleTimeout)
		}
		if cfg.E2TuneTimeout < 0 {
			v.AddError("E2TuneTimeout", "must be >= 0", cfg.E2TuneTimeout)
		}
		if cfg.E2Timeout < 0 {
			v.AddError("E2Timeout", "must be >= 0", cfg.E2Timeout)
		}
		if cfg.E2RespTimeout < 0 {
			v.AddError("E2RespTimeout", "must be >= 0", cfg.E2RespTimeout)
		}
		v.Range("E2Retries", cfg.E2Retries, 0, 10)
		if cfg.E2Backoff < 0 {
			v.AddError("E2Backoff", "must be >= 0", cfg.E2Backoff)
		}
		if cfg.E2MaxBackoff < 0 {
			v.AddError("E2MaxBackoff", "must be >= 0", cfg.E2MaxBackoff)
		}
		if cfg.E2Backoff > 0 && cfg.E2MaxBackoff > 0 && cfg.E2Backoff > cfg.E2MaxBackoff {
			v.AddError("E2MaxBackoff", "must be >= E2Backoff", cfg.E2MaxBackoff)
		}
		if cfg.E2RateLimit < 0 {
			v.AddError("E2RateLimit", "must be >= 0", cfg.E2RateLimit)
		}
		if cfg.E2RateBurst < 0 {
			v.AddError("E2RateBurst", "must be >= 0", cfg.E2RateBurst)
		}

		if cfg.ConfigStrict {
			switch cfg.WorkerMode {
			case "standard", "virtual":
			default:
				v.AddError("WorkerMode", "must be standard or virtual", cfg.WorkerMode)
			}

			switch cfg.StoreBackend {
			case "memory", "bolt":
			default:
				v.AddError("StoreBackend", "must be memory or bolt", cfg.StoreBackend)
			}

			v.URL("E2Host", cfg.E2Host, []string{"http", "https"})
		}
	}

	if !v.IsValid() {
		return v.Err()
	}

	return nil
}
