// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import (
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	householddomain "github.com/ManuGH/xg2g/internal/household"
	platformnet "github.com/ManuGH/xg2g/internal/platform/net"
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
	{
		Name: "VOD_CACHE_TTL_NEGATIVE",
		Predicate: func(cfg AppConfig) bool {
			return cfg.VODCacheTTL < 0
		},
		ProblemDetails: "VODCacheTTL must be >= 0.",
	},
	{
		Name: "VOD_CACHE_MAX_ENTRIES_INVALID",
		Predicate: func(cfg AppConfig) bool {
			return cfg.VODCacheMaxEntries <= 0
		},
		ProblemDetails: "VODCacheMaxEntries must be > 0.",
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

	validateBasicSettings(v, cfg)
	validateNetworkSettings(v, cfg)
	validateAuthAndPlaybackDecision(v, cfg)
	validateMonetization(v, cfg)
	validateHousehold(v, cfg)
	validateEngineAndResilience(v, cfg)

	if !v.IsValid() {
		return v.Err()
	}

	return nil
}

func validateHousehold(v *validate.Validator, cfg AppConfig) {
	if strings.TrimSpace(cfg.Household.PinHash) == "" {
		if cfg.Household.UnlockTTL < 0 {
			v.AddError("Household.UnlockTTL", "must be >= 0", "")
		}
		return
	}
	if err := householddomain.ValidateStoredPINHash(cfg.Household.PinHash); err != nil {
		v.AddError("Household.PinHash", err.Error(), "")
	}
	if cfg.Household.UnlockTTL < 0 {
		v.AddError("Household.UnlockTTL", "must be >= 0", "")
	}
}

func validateBasicSettings(v *validate.Validator, cfg AppConfig) {
	// Enigma2 URL (Standardized)
	if strings.TrimSpace(cfg.Enigma2.BaseURL) != "" {
		v.URL("Enigma2.BaseURL", cfg.Enigma2.BaseURL, []string{"http", "https"})
	}

	// Stream port (0 = allowed, means use /web mechanism)
	if cfg.Enigma2.StreamPort != 0 {
		v.Port("Enigma2.StreamPort", cfg.Enigma2.StreamPort)
	}

	v.Directory("DataDir", cfg.DataDir, false)

	if cfg.LogLevel != "" {
		v.OneOf("LogLevel", strings.ToLower(cfg.LogLevel), []string{"debug", "info", "warn", "error", "fatal", "panic", "disabled", "trace"})
	}

	if cfg.Server.ReadTimeout < 0 {
		v.AddError("Server.ReadTimeout", "must be >= 0", cfg.Server.ReadTimeout)
	}
	if cfg.Server.WriteTimeout < 0 {
		v.AddError("Server.WriteTimeout", "must be >= 0", cfg.Server.WriteTimeout)
	}
	if cfg.Server.IdleTimeout < 0 {
		v.AddError("Server.IdleTimeout", "must be >= 0", cfg.Server.IdleTimeout)
	}
	if cfg.Server.MaxHeaderBytes < 0 {
		v.AddError("Server.MaxHeaderBytes", "must be >= 0", cfg.Server.MaxHeaderBytes)
	}
	if cfg.Server.ShutdownTimeout != 0 && cfg.Server.ShutdownTimeout < 3*time.Second {
		v.AddError("Server.ShutdownTimeout", "must be >= 3s", cfg.Server.ShutdownTimeout)
	}

	if cfg.EPGEnabled {
		v.Range("EPGDays", cfg.EPGDays, 1, 14)
		v.Range("EPGMaxConcurrency", cfg.EPGMaxConcurrency, 1, 10)
		v.Range("EPGTimeoutMS", cfg.EPGTimeoutMS, 100, 60000)
		v.Range("EPGRetries", cfg.EPGRetries, 0, 5)
		v.Range("FuzzyMax", cfg.FuzzyMax, 0, 10)
	}

	v.Range("Enigma2.Retries", cfg.Enigma2.Retries, 0, 10)

	if cfg.TLSEnabled {
		hasCert := strings.TrimSpace(cfg.TLSCert) != ""
		hasKey := strings.TrimSpace(cfg.TLSKey) != ""
		if hasCert != hasKey {
			v.AddError("TLS", "TLS enabled requires both cert and key, or none for autogen", "")
		}
	}

	v.Path("XMLTVPath", cfg.XMLTVPath)
}

func validateNetworkSettings(v *validate.Validator, cfg AppConfig) {
	if cfg.TrustedProxies != "" {
		entries := strings.Split(cfg.TrustedProxies, ",")
		if err := validateCIDRList("XG2G_TRUSTED_PROXIES", entries); err != nil {
			v.AddError("TrustedProxies", err.Error(), "")
		}
	}

	if err := validateCIDRList("XG2G_RATE_LIMIT_WHITELIST", cfg.RateLimitWhitelist); err != nil {
		v.AddError("RateLimitWhitelist", err.Error(), "")
	}

	outbound := cfg.Network.Outbound
	if outbound.Enabled {
		if len(outbound.Allow.Hosts) == 0 && len(outbound.Allow.CIDRs) == 0 {
			v.AddError("Network.Outbound.Allow", "at least one host or CIDR must be configured", "")
		}
		if len(outbound.Allow.Schemes) == 0 {
			v.AddError("Network.Outbound.Allow.Schemes", "must include http and/or https", "")
		}
		if len(outbound.Allow.Ports) == 0 {
			v.AddError("Network.Outbound.Allow.Ports", "must include at least one port", "")
		}
	}
	for _, scheme := range outbound.Allow.Schemes {
		switch strings.ToLower(strings.TrimSpace(scheme)) {
		case "http", "https":
		default:
			v.AddError("Network.Outbound.Allow.Schemes", "unsupported scheme", scheme)
		}
	}
	for _, port := range outbound.Allow.Ports {
		v.Port("Network.Outbound.Allow.Ports", port)
	}
	if len(outbound.Allow.CIDRs) > 0 {
		if err := validateCIDRList("XG2G_OUTBOUND_ALLOW_CIDRS", outbound.Allow.CIDRs); err != nil {
			v.AddError("Network.Outbound.Allow.CIDRs", err.Error(), "")
		}
	}
	for _, host := range outbound.Allow.Hosts {
		if _, err := platformnet.NormalizeHost(host); err != nil {
			v.AddError("Network.Outbound.Allow.Hosts", err.Error(), host)
		}
	}
}

func validateAuthAndPlaybackDecision(v *validate.Validator, cfg AppConfig) {
	if cfg.apiTokensParseErr != nil {
		v.AddError("APITokens", cfg.apiTokensParseErr.Error(), "")
	}

	monetization := cfg.Monetization.Normalized()
	validScopes := map[string]struct{}{
		"*":         {},
		"v3:*":      {},
		"v3:read":   {},
		"v3:write":  {},
		"v3:admin":  {},
		"v3:status": {},
	}
	for _, scope := range monetization.RequiredScopes {
		if scope == "" {
			continue
		}
		validScopes[scope] = struct{}{}
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

	if secret := strings.TrimSpace(cfg.PlaybackDecisionSecret); secret != "" && len(secret) < 32 {
		v.AddError("PlaybackDecisionSecret", "must be at least 32 characters", "")
	}
	if kid := normalizePlaybackDecisionKeyID(cfg.PlaybackDecisionKeyID); strings.TrimSpace(cfg.PlaybackDecisionKeyID) != "" && kid == "" {
		v.AddError("PlaybackDecisionKeyID", "must match [a-z0-9._-]+", cfg.PlaybackDecisionKeyID)
	}
	if cfg.PlaybackDecisionRotationWindow < 0 {
		v.AddError("PlaybackDecisionRotationWindow", "must be >= 0", cfg.PlaybackDecisionRotationWindow)
	}
	if len(cfg.PlaybackDecisionPreviousKeys) > 0 && cfg.PlaybackDecisionRotationWindow <= 0 {
		v.AddError("PlaybackDecisionRotationWindow", "must be > 0 when PlaybackDecisionPreviousKeys are configured", cfg.PlaybackDecisionRotationWindow)
	}
	for _, raw := range cfg.PlaybackDecisionPreviousKeys {
		keyID, secret := parsePlaybackDecisionPreviousKey(raw)
		if len(secret) == 0 {
			v.AddError("PlaybackDecisionPreviousKeys", "entry must be <kid>:<secret> or <secret>", raw)
			continue
		}
		if keyID != "" && normalizePlaybackDecisionKeyID(keyID) == "" {
			v.AddError("PlaybackDecisionPreviousKeys", "key id must match [a-z0-9._-]+", raw)
			continue
		}
		if len(secret) < 32 {
			v.AddError("PlaybackDecisionPreviousKeys", "secret must be at least 32 characters", raw)
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

	validatePlaybackOperatorRules(v, cfg.Playback.Operator.SourceRules)
}

func validateMonetization(v *validate.Validator, cfg AppConfig) {
	monetization := cfg.Monetization.Normalized()

	switch monetization.Model {
	case MonetizationModelFree, MonetizationModelOneTimeUnlock:
	default:
		v.AddError("Monetization.Model", "must be free or one_time_unlock", monetization.Model)
	}

	switch monetization.Enforcement {
	case MonetizationEnforcementNone, MonetizationEnforcementRequired:
	default:
		v.AddError("Monetization.Enforcement", "must be none or required", monetization.Enforcement)
	}

	if monetization.Enforcement == MonetizationEnforcementRequired && !monetization.RequiresUnlock() {
		v.AddError("Monetization.Enforcement", "required enforcement needs monetization enabled with one_time_unlock model", monetization.Enforcement)
	}

	if monetization.RequiresUnlock() && len(monetization.RequiredScopes) == 0 {
		v.AddError("Monetization.RequiredScopes", "must include at least one scope when one_time_unlock is enabled", monetization.RequiredScopes)
	}

	seenScopes := make(map[string]struct{}, len(monetization.RequiredScopes))
	for _, scope := range monetization.RequiredScopes {
		if scope == "" {
			v.AddError("Monetization.RequiredScopes", "entries must not be empty", monetization.RequiredScopes)
			continue
		}
		if _, ok := seenScopes[scope]; ok {
			v.AddError("Monetization.RequiredScopes", "entries must be unique", monetization.RequiredScopes)
			continue
		}
		seenScopes[scope] = struct{}{}
	}

	if monetization.PurchaseURL != "" {
		v.URL("Monetization.PurchaseURL", monetization.PurchaseURL, []string{"http", "https"})
	}

	seenMappings := make(map[string]struct{}, len(monetization.ProductMappings))
	requiredScopes := make(map[string]struct{}, len(monetization.RequiredScopes))
	for _, scope := range monetization.RequiredScopes {
		requiredScopes[scope] = struct{}{}
	}
	requiresGooglePlay := false
	requiresAmazon := false
	for _, mapping := range monetization.ProductMappings {
		switch mapping.Provider {
		case "google_play":
			requiresGooglePlay = true
		case "amazon_appstore":
			requiresAmazon = true
		case "":
			v.AddError("Monetization.ProductMappings", "provider must not be empty", mapping)
			continue
		default:
			v.AddError("Monetization.ProductMappings", "provider must be google_play or amazon_appstore", mapping.Provider)
			continue
		}

		if mapping.ProductID == "" {
			v.AddError("Monetization.ProductMappings", "productId must not be empty", mapping)
		}
		if len(mapping.Scopes) == 0 {
			v.AddError("Monetization.ProductMappings", "scopes must include at least one entry", mapping)
		}

		key := mapping.Provider + "\x00" + mapping.ProductID
		if _, ok := seenMappings[key]; ok {
			v.AddError("Monetization.ProductMappings", "provider/productId pairs must be unique", mapping)
		}
		seenMappings[key] = struct{}{}

		seenMappingScopes := make(map[string]struct{}, len(mapping.Scopes))
		for _, scope := range mapping.Scopes {
			if scope == "" {
				v.AddError("Monetization.ProductMappings", "scopes entries must not be empty", mapping)
				continue
			}
			if _, ok := seenMappingScopes[scope]; ok {
				v.AddError("Monetization.ProductMappings", "scopes entries must be unique", mapping)
				continue
			}
			if _, ok := requiredScopes[scope]; !ok {
				v.AddError("Monetization.ProductMappings", "scopes must be declared in monetization.requiredScopes", mapping)
			}
			seenMappingScopes[scope] = struct{}{}
		}
	}

	if monetization.GooglePlay.PackageName != "" || monetization.GooglePlay.ServiceAccountCredentialsFile != "" {
		if monetization.GooglePlay.PackageName == "" {
			v.AddError("Monetization.GooglePlay.PackageName", "must be set when Google Play receipt verification is configured", monetization.GooglePlay.PackageName)
		}
		if monetization.GooglePlay.ServiceAccountCredentialsFile == "" {
			v.AddError("Monetization.GooglePlay.ServiceAccountCredentialsFile", "must be set when Google Play receipt verification is configured", monetization.GooglePlay.ServiceAccountCredentialsFile)
		}
	}
	if requiresGooglePlay {
		if monetization.GooglePlay.PackageName == "" {
			v.AddError("Monetization.GooglePlay.PackageName", "must be set when google_play product mappings exist", monetization.GooglePlay.PackageName)
		}
		if monetization.GooglePlay.ServiceAccountCredentialsFile == "" {
			v.AddError("Monetization.GooglePlay.ServiceAccountCredentialsFile", "must be set when google_play product mappings exist", monetization.GooglePlay.ServiceAccountCredentialsFile)
		}
	}
	if monetization.Amazon.SharedSecretFile != "" || monetization.Amazon.UseSandbox {
		if monetization.Amazon.SharedSecretFile == "" {
			v.AddError("Monetization.Amazon.SharedSecretFile", "must be set when Amazon Appstore receipt verification is configured", monetization.Amazon.SharedSecretFile)
		}
	}
	if requiresAmazon {
		if monetization.Amazon.SharedSecretFile == "" {
			v.AddError("Monetization.Amazon.SharedSecretFile", "must be set when amazon_appstore product mappings exist", monetization.Amazon.SharedSecretFile)
		}
	}
}

func validatePlaybackOperatorRules(v *validate.Validator, rules []PlaybackOperatorRuleConfig) {
	for i, rule := range rules {
		prefix := "Playback.Operator.SourceRules"
		if strings.TrimSpace(rule.Name) == "" {
			v.AddError(prefix, "rule name must not be empty", i)
		}

		mode := normalizePlaybackOperatorRuleMode(rule.Mode)
		if mode == "" {
			v.AddError(prefix, "mode must be one of live, recording, any", rule.Mode)
		}

		hasExact := strings.TrimSpace(rule.ServiceRef) != ""
		hasPrefix := strings.TrimSpace(rule.ServiceRefPrefix) != ""
		if hasExact == hasPrefix {
			v.AddError(prefix, "exactly one of service_ref or service_ref_prefix must be set", rule.Name)
		}

		if force := strings.TrimSpace(rule.ForceIntent); force != "" && playbackprofile.NormalizeRequestedIntent(force) == playbackprofile.IntentUnknown {
			v.AddError(prefix, "force_intent must be one of direct, compatible, quality, repair", force)
		}
		if rung := strings.TrimSpace(rule.MaxQualityRung); rung != "" && playbackprofile.NormalizeQualityRung(rung) == playbackprofile.RungUnknown {
			v.AddError(prefix, "max_quality_rung must be a known playback quality rung", rung)
		}
		if strings.TrimSpace(rule.ForceIntent) == "" && strings.TrimSpace(rule.MaxQualityRung) == "" && rule.DisableClientFallback == nil {
			v.AddError(prefix, "rule must define at least one override field", rule.Name)
		}
	}
}

func normalizePlaybackOperatorRuleMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "any":
		return "any"
	case "live":
		return "live"
	case "recording":
		return "recording"
	default:
		return ""
	}
}

func validateEngineAndResilience(v *validate.Validator, cfg AppConfig) {
	if cfg.Engine.Enabled {
		v.WritableDirectory("Store.Path", cfg.Store.Path, false)
		v.WritableDirectory("HLS.Root", cfg.HLS.Root, false)

		switch cfg.HLS.SegmentSeconds {
		case 1:
			if cfg.HLS.DVRWindow < 10*time.Second {
				v.AddError("HLS.DVRWindow", "must be >= 10s for low latency", cfg.HLS.DVRWindow)
			}
		case QuickStartHLSSegmentSeconds:
			if cfg.HLS.DVRWindow < 20*time.Second {
				v.AddError("HLS.DVRWindow", "must be >= 20s for quick-start profile", cfg.HLS.DVRWindow)
			}
		case DefaultHLSSegmentSeconds:
			if cfg.HLS.DVRWindow < 1*time.Minute {
				v.AddError("HLS.DVRWindow", "must be >= 1m for standard profile", cfg.HLS.DVRWindow)
			}
		default:
			v.AddError("HLS.SegmentSeconds", "must be 1 (Low Latency), 2 (Quick Start), or 6 (Standard Profile)", cfg.HLS.SegmentSeconds)
		}
		if cfg.HLS.ReadySegments < 1 {
			v.AddError("HLS.ReadySegments", "must be >= 1", cfg.HLS.ReadySegments)
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
		case "memory", "sqlite":
		default:
			v.AddError("Store.Backend", "must be memory or sqlite", cfg.Store.Backend)
		}

		v.URL("Enigma2.BaseURL", cfg.Enigma2.BaseURL, []string{"http", "https"})
	}

	if cfg.Streaming.DeliveryPolicy != "universal" {
		v.AddError("Streaming.DeliveryPolicy", "only 'universal' policy is supported (ADR-00X)", cfg.Streaming.DeliveryPolicy)
	}

	if cfg.Limits.MaxSessions < 1 {
		v.AddError("Limits.MaxSessions", "must be >= 1", cfg.Limits.MaxSessions)
	}
	if cfg.Limits.MaxTranscodes < 0 {
		v.AddError("Limits.MaxTranscodes", "must be >= 0", cfg.Limits.MaxTranscodes)
	}

	if cfg.Timeouts.TranscodeStart <= 0 {
		v.AddError("Timeouts.TranscodeStart", "must be > 0", cfg.Timeouts.TranscodeStart)
	}
	if cfg.Timeouts.TranscodeNoProgress <= cfg.Timeouts.TranscodeStart {
		v.AddError("Timeouts.TranscodeNoProgress", "must be > TranscodeStart", cfg.Timeouts.TranscodeNoProgress)
	}
	if cfg.Timeouts.KillGrace <= 0 {
		v.AddError("Timeouts.KillGrace", "must be > 0", cfg.Timeouts.KillGrace)
	}
	if cfg.Timeouts.KillGrace >= cfg.Timeouts.TranscodeNoProgress {
		v.AddError("Timeouts.KillGrace", "must be < TranscodeNoProgress", cfg.Timeouts.KillGrace)
	}

	if cfg.Breaker.Window <= 0 {
		v.AddError("Breaker.Window", "must be > 0", cfg.Breaker.Window)
	}
	if cfg.Breaker.MinAttempts < 1 {
		v.AddError("Breaker.MinAttempts", "must be >= 1", cfg.Breaker.MinAttempts)
	}
	if cfg.Breaker.FailuresThreshold < 1 {
		v.AddError("Breaker.FailuresThreshold", "must be >= 1", cfg.Breaker.FailuresThreshold)
	}
}

func parsePlaybackDecisionPreviousKey(raw string) (keyID string, secret string) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", ""
	}
	idx := strings.Index(value, ":")
	if idx < 0 {
		return "", strings.TrimSpace(value)
	}
	keyID = strings.TrimSpace(value[:idx])
	secret = strings.TrimSpace(value[idx+1:])
	return keyID, secret
}

func normalizePlaybackDecisionKeyID(raw string) string {
	keyID := strings.ToLower(strings.TrimSpace(raw))
	if keyID == "" {
		return ""
	}
	for _, ch := range keyID {
		if ch >= 'a' && ch <= 'z' {
			continue
		}
		if ch >= '0' && ch <= '9' {
			continue
		}
		if ch == '_' || ch == '-' || ch == '.' {
			continue
		}
		return ""
	}
	return keyID
}
