// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import (
	"strings"
	"time"
)

// mergeEnvConfig merges environment variables into jobs.Config.
// ENV variables have the highest precedence.
// Uses consistent ParseBool/ParseInt/ParseDuration helpers from env.go.
func (l *Loader) mergeEnvConfig(cfg *AppConfig) {
	l.mergeEnvCore(cfg)
	l.mergeEnvLegacyOWI(cfg)
	l.mergeEnvGlobal(cfg)
	l.mergeEnvBouquet(cfg)
	l.mergeEnvEPG(cfg)
	l.mergeEnvAPI(cfg)
	l.mergeEnvServer(cfg)
	l.mergeEnvMetrics(cfg)
	l.mergeEnvPicons(cfg)
	l.mergeEnvTLS(cfg)
	l.mergeEnvNetwork(cfg)
	l.mergeEnvFeatureFlags(cfg)
	l.mergeEnvCanonicalEngine(cfg)
	l.mergeEnvCanonicalEnigma2(cfg)
	l.mergeEnvTunerSlots(cfg)
	l.mergeEnvResilience(cfg)
	l.mergeEnvCanonicalStore(cfg)
	l.mergeEnvCanonicalHLS(cfg)
	l.mergeEnvCanonicalFFmpeg(cfg)
	l.mergeEnvRateLimiting(cfg)
	l.mergeEnvTrustedProxies(cfg)
	l.mergeEnvStreaming(cfg)
	l.mergeEnvVerification(cfg)
}

func (l *Loader) mergeEnvCore(cfg *AppConfig) {
	// String values (direct assignment)
	cfg.Version = l.envString("XG2G_VERSION", cfg.Version)
	cfg.DataDir = l.envString("XG2G_DATA", cfg.DataDir)
	cfg.LogLevel = l.envString("XG2G_LOG_LEVEL", cfg.LogLevel)
	cfg.LogService = l.envString("XG2G_LOG_SERVICE", cfg.LogService)
}

func (l *Loader) mergeEnvLegacyOWI(cfg *AppConfig) {
	// Enigma2 (Legacy OWI compatibility)
	cfg.Enigma2.BaseURL = l.envString("XG2G_OWI_BASE", cfg.Enigma2.BaseURL)

	// Username: XG2G_OWI_USER
	if v := l.envString("XG2G_OWI_USER", ""); v != "" {
		cfg.Enigma2.Username = v
	}

	// Password: XG2G_OWI_PASS
	if v := l.envString("XG2G_OWI_PASS", ""); v != "" {
		cfg.Enigma2.Password = v
	}
	cfg.Enigma2.StreamPort = l.envInt("XG2G_STREAM_PORT", cfg.Enigma2.StreamPort)
	cfg.Enigma2.UseWebIFStreams = l.envBool("XG2G_USE_WEBIF_STREAMS", cfg.Enigma2.UseWebIFStreams)

	// OpenWebIF timeouts/retries
	if ms := l.envInt("XG2G_OWI_TIMEOUT_MS", 0); ms > 0 {
		cfg.Enigma2.Timeout = time.Duration(ms) * time.Millisecond
	}
	cfg.Enigma2.Retries = l.envInt("XG2G_OWI_RETRIES", cfg.Enigma2.Retries)
	if ms := l.envInt("XG2G_OWI_BACKOFF_MS", 0); ms > 0 {
		cfg.Enigma2.Backoff = time.Duration(ms) * time.Millisecond
	}
	if ms := l.envInt("XG2G_OWI_MAX_BACKOFF_MS", 0); ms > 0 {
		cfg.Enigma2.MaxBackoff = time.Duration(ms) * time.Millisecond
	}
}

func (l *Loader) mergeEnvGlobal(cfg *AppConfig) {
	// Global strict mode
	cfg.ConfigStrict = l.envBool("XG2G_CONFIG_STRICT", cfg.ConfigStrict)
}

func (l *Loader) mergeEnvBouquet(cfg *AppConfig) {
	// Bouquet
	cfg.Bouquet = l.envString("XG2G_BOUQUET", cfg.Bouquet)
}

func (l *Loader) mergeEnvEPG(cfg *AppConfig) {
	// EPG
	cfg.EPGEnabled = l.envBool("XG2G_EPG_ENABLED", cfg.EPGEnabled)
	cfg.EPGDays = l.envInt("XG2G_EPG_DAYS", cfg.EPGDays)
	cfg.EPGMaxConcurrency = l.envInt("XG2G_EPG_MAX_CONCURRENCY", cfg.EPGMaxConcurrency)
	cfg.EPGTimeoutMS = l.envInt("XG2G_EPG_TIMEOUT_MS", cfg.EPGTimeoutMS)
	cfg.EPGSource = l.envString("XG2G_EPG_SOURCE", cfg.EPGSource)
	cfg.EPGRetries = l.envInt("XG2G_EPG_RETRIES", cfg.EPGRetries)
	cfg.FuzzyMax = l.envInt("XG2G_FUZZY_MAX", cfg.FuzzyMax)
	cfg.XMLTVPath = l.envString("XG2G_XMLTV", cfg.XMLTVPath)
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
	cfg.PlaybackDecisionSecret = l.envString("XG2G_PLAYBACK_DECISION_SECRET", cfg.PlaybackDecisionSecret)
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

func (l *Loader) mergeEnvServer(cfg *AppConfig) {
	cfg.Server.ReadTimeout = l.envDuration("XG2G_SERVER_READ_TIMEOUT", cfg.Server.ReadTimeout)
	cfg.Server.WriteTimeout = l.envDuration("XG2G_SERVER_WRITE_TIMEOUT", cfg.Server.WriteTimeout)
	cfg.Server.IdleTimeout = l.envDuration("XG2G_SERVER_IDLE_TIMEOUT", cfg.Server.IdleTimeout)
	cfg.Server.MaxHeaderBytes = l.envInt("XG2G_SERVER_MAX_HEADER_BYTES", cfg.Server.MaxHeaderBytes)
	cfg.Server.ShutdownTimeout = l.envDuration("XG2G_SERVER_SHUTDOWN_TIMEOUT", cfg.Server.ShutdownTimeout)
}

func (l *Loader) mergeEnvMetrics(cfg *AppConfig) {
	// Metrics
	metricsAddr := l.envString("XG2G_METRICS_LISTEN", "")
	if metricsAddr != "" {
		cfg.MetricsAddr = metricsAddr
		cfg.MetricsEnabled = true
	}
}

func (l *Loader) mergeEnvPicons(cfg *AppConfig) {
	// Picons
	cfg.PiconBase = l.envString("XG2G_PICON_BASE", cfg.PiconBase)
}

func (l *Loader) mergeEnvTLS(cfg *AppConfig) {
	// TLS
	cfg.TLSEnabled = l.envBool("XG2G_TLS_ENABLED", cfg.TLSEnabled)
	cfg.TLSCert = l.envString("XG2G_TLS_CERT", cfg.TLSCert)
	cfg.TLSKey = l.envString("XG2G_TLS_KEY", cfg.TLSKey)
	cfg.ForceHTTPS = l.envBool("XG2G_FORCE_HTTPS", cfg.ForceHTTPS)
}

func (l *Loader) mergeEnvNetwork(cfg *AppConfig) {
	// Network outbound policy
	cfg.Network.Outbound.Enabled = l.envBool("XG2G_OUTBOUND_ENABLED", cfg.Network.Outbound.Enabled)
	cfg.Network.Outbound.Allow.Hosts = parseCommaSeparated(l.envString("XG2G_OUTBOUND_ALLOW_HOSTS", ""), cfg.Network.Outbound.Allow.Hosts)
	cfg.Network.Outbound.Allow.CIDRs = parseCommaSeparated(l.envString("XG2G_OUTBOUND_ALLOW_CIDRS", ""), cfg.Network.Outbound.Allow.CIDRs)
	cfg.Network.Outbound.Allow.Ports = parseCommaSeparatedInts(l.envString("XG2G_OUTBOUND_ALLOW_PORTS", ""), cfg.Network.Outbound.Allow.Ports)
	cfg.Network.Outbound.Allow.Schemes = parseCommaSeparated(l.envString("XG2G_OUTBOUND_ALLOW_SCHEMES", ""), cfg.Network.Outbound.Allow.Schemes)
	cfg.Network.LAN.Allow.CIDRs = parseCommaSeparated(l.envString("XG2G_LAN_ALLOW_CIDRS", ""), cfg.Network.LAN.Allow.CIDRs)
}

func (l *Loader) mergeEnvFeatureFlags(cfg *AppConfig) {
	// Feature Flags
	cfg.ReadyStrict = l.envBool("XG2G_READY_STRICT", cfg.ReadyStrict)
}

func (l *Loader) mergeEnvCanonicalEngine(cfg *AppConfig) {
	// CANONICAL ENGINE CONFIG
	cfg.Engine.Enabled = l.envBool("XG2G_ENGINE_ENABLED", cfg.Engine.Enabled)
	cfg.Engine.Mode = l.envString("XG2G_ENGINE_MODE", cfg.Engine.Mode)
	cfg.Engine.IdleTimeout = l.envDuration("XG2G_ENGINE_IDLE_TIMEOUT", cfg.Engine.IdleTimeout)
	cfg.Engine.CPUThresholdScale = l.envFloat("XG2G_ENGINE_CPU_SCALE", cfg.Engine.CPUThresholdScale)
	cfg.Engine.MaxPool = l.envInt("XG2G_ENGINE_MAX_POOL", cfg.Engine.MaxPool)
	cfg.Engine.GPULimit = l.envInt("XG2G_ENGINE_GPU_LIMIT", cfg.Engine.GPULimit)
}

func (l *Loader) mergeEnvCanonicalEnigma2(cfg *AppConfig) {
	// CANONICAL ENIGMA2 CONFIG (Move up for discovery)
	cfg.Enigma2.BaseURL = l.envString("XG2G_E2_HOST", cfg.Enigma2.BaseURL)
	cfg.Enigma2.Username = l.envString("XG2G_E2_USER", cfg.Enigma2.Username)
	cfg.Enigma2.Password = l.envString("XG2G_E2_PASS", cfg.Enigma2.Password)
	cfg.Enigma2.AuthMode = strings.ToLower(strings.TrimSpace(l.envString("XG2G_E2_AUTH_MODE", cfg.Enigma2.AuthMode)))
	cfg.Enigma2.Timeout = l.envDuration("XG2G_E2_TIMEOUT", cfg.Enigma2.Timeout)
	cfg.Enigma2.ResponseHeaderTimeout = l.envDuration("XG2G_E2_RESPONSE_HEADER_TIMEOUT", cfg.Enigma2.ResponseHeaderTimeout)
	cfg.Enigma2.TuneTimeout = l.envDuration("XG2G_E2_TUNE_TIMEOUT", cfg.Enigma2.TuneTimeout)
	cfg.Enigma2.Retries = l.envInt("XG2G_E2_RETRIES", cfg.Enigma2.Retries)
	cfg.Enigma2.Backoff = l.envDuration("XG2G_E2_BACKOFF", cfg.Enigma2.Backoff)
	cfg.Enigma2.MaxBackoff = l.envDuration("XG2G_E2_MAX_BACKOFF", cfg.Enigma2.MaxBackoff)
	cfg.Enigma2.StreamPort = l.envInt("XG2G_E2_STREAM_PORT", cfg.Enigma2.StreamPort)
	cfg.Enigma2.UseWebIFStreams = l.envBool("XG2G_E2_USE_WEBIF_STREAMS", cfg.Enigma2.UseWebIFStreams)
	cfg.Enigma2.RateLimit = l.envInt("XG2G_E2_RATE_LIMIT", cfg.Enigma2.RateLimit)
	cfg.Enigma2.RateBurst = l.envInt("XG2G_E2_RATE_BURST", cfg.Enigma2.RateBurst)
	cfg.Enigma2.UserAgent = l.envString("XG2G_E2_USER_AGENT", cfg.Enigma2.UserAgent)
	cfg.Enigma2.AnalyzeDuration = l.envString("XG2G_E2_ANALYZE_DURATION", cfg.Enigma2.AnalyzeDuration)
	cfg.Enigma2.ProbeSize = l.envString("XG2G_E2_PROBE_SIZE", cfg.Enigma2.ProbeSize)
	cfg.Enigma2.FallbackTo8001 = l.envBool("XG2G_E2_FALLBACK_TO_8001", cfg.Enigma2.FallbackTo8001)
	cfg.Enigma2.PreflightTimeout = l.envDuration("XG2G_E2_PREFLIGHT_TIMEOUT", cfg.Enigma2.PreflightTimeout)
}

func (l *Loader) mergeEnvTunerSlots(cfg *AppConfig) {
	// Tuner Slots: Manual Override only (Auto-Discovery moved to runtime bootstrap)
	if rawSlots, ok := l.envLookup("XG2G_TUNER_SLOTS"); ok && strings.TrimSpace(rawSlots) != "" {
		if slots, err := ParseTunerSlots(rawSlots); err == nil {
			cfg.Engine.TunerSlots = slots
		}
	}
}

func (l *Loader) mergeEnvResilience(cfg *AppConfig) {
	// Sprint 1: Resilience Core ENV overrides
	if v := l.envInt("XG2G_MAX_SESSIONS", 0); v > 0 {
		cfg.Limits.MaxSessions = v
	}
	// 0 is a valid value for XG2G_MAX_TRANSCODES (disable transcodes)
	if v := l.envInt("XG2G_MAX_TRANSCODES", -1); v >= 0 {
		cfg.Limits.MaxTranscodes = v
	}
}

func (l *Loader) mergeEnvCanonicalStore(cfg *AppConfig) {
	// CANONICAL STORE CONFIG
	cfg.Store.Backend = l.envString("XG2G_STORE_BACKEND", cfg.Store.Backend)
	cfg.Store.Path = l.envString("XG2G_STORE_PATH", cfg.Store.Path)
}

func (l *Loader) mergeEnvCanonicalHLS(cfg *AppConfig) {
	// CANONICAL HLS CONFIG
	cfg.HLS.Root = l.envString("XG2G_HLS_ROOT", cfg.HLS.Root)
	cfg.HLS.DVRWindow = l.envDuration("XG2G_HLS_DVR_WINDOW", cfg.HLS.DVRWindow)
	cfg.HLS.SegmentSeconds = l.envInt("XG2G_HLS_SEGMENT_SECONDS", cfg.HLS.SegmentSeconds)
}

func (l *Loader) mergeEnvCanonicalFFmpeg(cfg *AppConfig) {
	// CANONICAL FFMPEG CONFIG
	cfg.FFmpeg.Bin = l.envString("XG2G_FFMPEG_BIN", cfg.FFmpeg.Bin)
	cfg.FFmpeg.FFprobeBin = l.envString("XG2G_FFPROBE_BIN", cfg.FFmpeg.FFprobeBin)
	cfg.FFmpeg.KillTimeout = l.envDuration("XG2G_FFMPEG_KILL_TIMEOUT", cfg.FFmpeg.KillTimeout)
	cfg.FFmpeg.VaapiDevice = l.envString("XG2G_VAAPI_DEVICE", cfg.FFmpeg.VaapiDevice)
}

func (l *Loader) mergeEnvRateLimiting(cfg *AppConfig) {
	// Rate Limiting
	cfg.RateLimitEnabled = l.envBool("XG2G_RATE_LIMIT_ENABLED", cfg.RateLimitEnabled)
	cfg.RateLimitGlobal = l.envInt("XG2G_RATE_LIMIT_GLOBAL", cfg.RateLimitGlobal)
	cfg.RateLimitAuth = l.envInt("XG2G_RATE_LIMIT_AUTH", cfg.RateLimitAuth)
	cfg.RateLimitBurst = l.envInt("XG2G_RATE_LIMIT_BURST", cfg.RateLimitBurst)
	if whitelist := l.envString("XG2G_RATE_LIMIT_WHITELIST", ""); whitelist != "" {
		cfg.RateLimitWhitelist = parseCommaSeparated(whitelist, cfg.RateLimitWhitelist)
	}
}

func (l *Loader) mergeEnvTrustedProxies(cfg *AppConfig) {
	// Trusted Proxies
	cfg.TrustedProxies = l.envString("XG2G_TRUSTED_PROXIES", cfg.TrustedProxies)
}

func (l *Loader) mergeEnvStreaming(cfg *AppConfig) {
	// Streaming Config (Canonical)
	cfg.Streaming.DeliveryPolicy = l.envString("XG2G_STREAMING_POLICY", cfg.Streaming.DeliveryPolicy)
}

func (l *Loader) mergeEnvVerification(cfg *AppConfig) {
	// Verification (Drift Detection)
	cfg.Verification.Enabled = l.envBool("XG2G_VERIFY_ENABLED", cfg.Verification.Enabled)
	cfg.Verification.Interval = l.envDuration("XG2G_VERIFY_INTERVAL", cfg.Verification.Interval)
}
