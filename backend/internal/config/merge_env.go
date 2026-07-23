// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import (
	"encoding/json"
	"strings"
)

// mergeEnvConfig merges environment variables into jobs.Config.
// ENV variables have the highest precedence.
// Uses consistent ParseBool/ParseInt/ParseDuration helpers from env.go.
func (l *Loader) mergeEnvConfig(cfg *AppConfig) {
	l.mergeEnvConfigGenerated(cfg)
	l.mergeEnvAPI(cfg)
	l.mergeEnvMetrics(cfg)
	l.mergeEnvConnectivity(cfg)
	l.mergeEnvRecordings(cfg)
	l.mergeEnvVOD(cfg)
	l.mergeEnvTunerSlots(cfg)
}

// mergeEnvVOD applies the XG2G_VOD_* environment overrides to the flat VOD fields (the
// representation the runtime reads). It was missing entirely (merge_env had no VOD case), so
// env-only VOD configuration was never applied — yet checkVODConflicts already errored on it
// (M14). Per-key via the standard helpers, so it inherits the canonical precedence: env >
// file (this runs after mergeFileConfig), per-key (only set keys override), and empty==unset
// (an empty var falls back to the file/default via the SAME envPresent predicate the conflict
// check uses).
//
// Scope (A+): it deliberately does NOT re-sync the typed cfg.VOD. That representation is not
// seeded from the registry defaults without a YAML vod: block (separate finding M14b), so
// rebuilding it from these flat fields would wipe its defaults. NOTE: of the five fields only
// VODCacheTTL has a runtime reader today; VODProbeSize/AnalyzeDuration/StallTimeout/
// MaxConcurrent are not read anywhere (the real probe params come from cfg.Enigma2.*) —
// separate finding M14c. Merging the four is for env-contract completeness, not a runtime
// effect; the tests label that honestly.
func (l *Loader) mergeEnvVOD(cfg *AppConfig) {
	cfg.VODProbeSize = l.envString("XG2G_VOD_PROBE_SIZE", cfg.VODProbeSize)
	cfg.VODAnalyzeDuration = l.envString("XG2G_VOD_ANALYZE_DURATION", cfg.VODAnalyzeDuration)
	cfg.VODStallTimeout = l.envDuration("XG2G_VOD_STALL_TIMEOUT", cfg.VODStallTimeout)
	cfg.VODMaxConcurrent = l.envInt("XG2G_VOD_MAX_CONCURRENT", cfg.VODMaxConcurrent)
	cfg.VODCacheTTL = l.envDuration("XG2G_VOD_CACHE_TTL", cfg.VODCacheTTL)
}

func (l *Loader) mergeEnvAPI(cfg *AppConfig) {
	// API
	cfg.APIToken = l.envString("XG2G_API_TOKEN", cfg.APIToken)
	cfg.APITokenScopes = parseCommaSeparated(l.envString("XG2G_API_TOKEN_SCOPES", ""), cfg.APITokenScopes)
	if tokens, err := parseScopedTokens(l.envString("XG2G_API_TOKENS", ""), cfg.APITokens); err != nil {
		cfg.apiTokensParseErr = err
	} else {
		cfg.APITokens = tokens
	}
	cfg.APIDisableLegacyTokenSources = l.envBool("XG2G_API_DISABLE_LEGACY_TOKEN_SOURCES", cfg.APIDisableLegacyTokenSources)
	cfg.APILegacyEnabled = l.envBool("XG2G_API_LEGACY_ENABLED", cfg.APILegacyEnabled)
	if value, ok := decisionSecretValueFromLookup(l.envLookup); ok {
		cfg.PlaybackDecisionSecret = value
	}
	cfg.PlaybackDecisionKeyID = l.envString("XG2G_PLAYBACK_DECISION_KID", cfg.PlaybackDecisionKeyID)
	cfg.PlaybackDecisionPreviousKeys = parseCommaSeparated(l.envString("XG2G_PLAYBACK_DECISION_PREVIOUS_KEYS", ""), cfg.PlaybackDecisionPreviousKeys)
	cfg.PlaybackDecisionRotationWindow = l.envDuration("XG2G_PLAYBACK_DECISION_ROTATION_WINDOW", cfg.PlaybackDecisionRotationWindow)
	cfg.APIListenAddr = l.envString("XG2G_LISTEN", cfg.APIListenAddr)

	// CORS: ENV overrides YAML if set
	if rawOrigins, ok := l.envLookup("XG2G_ALLOWED_ORIGINS"); ok {
		if strings.TrimSpace(rawOrigins) != "" {
			cfg.AllowedOrigins = parseCommaSeparated(rawOrigins, nil)
		}
	}
}

func (l *Loader) mergeEnvMetrics(cfg *AppConfig) {
	// Metrics
	metricsAddr := l.envString("XG2G_METRICS_LISTEN", "")
	if metricsAddr != "" {
		cfg.MetricsAddr = metricsAddr
		cfg.MetricsEnabled = true
	}
}

func (l *Loader) mergeEnvConnectivity(cfg *AppConfig) {
	cfg.Connectivity.Profile = l.envString("XG2G_CONNECTIVITY_PROFILE", cfg.Connectivity.Profile)
	cfg.Connectivity.AllowLocalHTTP = l.envBool("XG2G_CONNECTIVITY_ALLOW_LOCAL_HTTP", cfg.Connectivity.AllowLocalHTTP)

	raw, ok := l.envLookup("XG2G_PUBLISHED_ENDPOINTS")
	if !ok || strings.TrimSpace(raw) == "" {
		return
	}

	var parsed []PublishedEndpointConfig
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		cfg.connectivityParseErr = err
		return
	}

	for i := range parsed {
		if strings.TrimSpace(parsed[i].Source) == "" {
			parsed[i].Source = "env"
		}
	}
	cfg.connectivityParseErr = nil
	cfg.Connectivity.PublishedEndpoints = clonePublishedEndpointConfigs(parsed)
}

func (l *Loader) mergeEnvTunerSlots(cfg *AppConfig) {
	// Tuner Slots: Manual Override only (Auto-Discovery moved to runtime bootstrap)
	if rawSlots, ok := l.envLookup("XG2G_TUNER_SLOTS"); ok && strings.TrimSpace(rawSlots) != "" {
		if slots, err := ParseTunerSlots(rawSlots); err == nil {
			cfg.Engine.TunerSlots = slots
		}
	}
}

func (l *Loader) mergeEnvRecordings(cfg *AppConfig) {
	cfg.RecordingPlaybackPolicy = l.envString("XG2G_RECORDING_PLAYBACK_POLICY", cfg.RecordingPlaybackPolicy)
	cfg.RecordingStableWindow = l.envDuration("XG2G_RECORDING_STABLE_WINDOW", cfg.RecordingStableWindow)
	cfg.RecordingPathMappings = parseRecordingMappings(
		l.envString("XG2G_RECORDINGS_MAP", ""),
		cfg.RecordingPathMappings,
	)
	cfg.RecordingStrictTargetRequired = l.envBool("XG2G_RECORDINGS_STRICT_TARGET_REQUIRED", cfg.RecordingStrictTargetRequired)
	cfg.RecordingTargetSigningKey = l.envString("XG2G_RECORDINGS_TARGET_SIGNING_KEY", cfg.RecordingTargetSigningKey)
	cfg.RecordingTargetSigningKeyPrevious = l.envString("XG2G_RECORDINGS_TARGET_SIGNING_KEY_PREVIOUS", cfg.RecordingTargetSigningKeyPrevious)
}
