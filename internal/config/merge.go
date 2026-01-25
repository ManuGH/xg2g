// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/log"
)

// setDefaults sets default values for configuration
func (l *Loader) setDefaults(cfg *AppConfig) error {
	// P1.4 Mechanical Truth: Apply defaults from Registry
	registry, err := GetRegistry()
	if err != nil {
		return fmt.Errorf("get registry: %w", err)
	}
	if err := registry.ApplyDefaults(cfg); err != nil {
		return fmt.Errorf("apply defaults: %w", err)
	}

	// Fields not yet in Registry (internal state)
	cfg.ConfigVersion = V3ConfigVersion
	return nil
}

// mergeFileConfig merges file configuration into jobs.Config
func (l *Loader) mergeFileConfig(dst *AppConfig, src *FileConfig) error {
	if err := l.checkAliasConflicts(src); err != nil {
		return err
	}
	if err := l.checkAliasEnvConflicts(src); err != nil {
		return err
	}
	if err := l.checkVODConflicts(src); err != nil {
		return err
	}
	if src.DataDir != "" {
		dst.DataDir = expandEnv(src.DataDir)
	}
	if src.LogLevel != "" {
		dst.LogLevel = src.LogLevel
	}

	// OpenWebIF (Enigma2 structure)
	if src.OpenWebIF.BaseURL != "" {
		dst.Enigma2.BaseURL = expandEnv(src.OpenWebIF.BaseURL)
	}
	if src.OpenWebIF.Username != "" {
		dst.Enigma2.Username = expandEnv(src.OpenWebIF.Username)
	}
	if src.OpenWebIF.Password != "" {
		dst.Enigma2.Password = expandEnv(src.OpenWebIF.Password)
	}
	if src.OpenWebIF.StreamPort > 0 {
		dst.Enigma2.StreamPort = src.OpenWebIF.StreamPort
	}
	if src.OpenWebIF.UseWebIF != nil {
		dst.Enigma2.UseWebIFStreams = *src.OpenWebIF.UseWebIF
	}
	if src.Enigma2.UseWebIF != nil {
		dst.Enigma2.UseWebIFStreams = *src.Enigma2.UseWebIF
	}

	// Parse durations from strings
	if src.OpenWebIF.Timeout != "" {
		d, err := time.ParseDuration(src.OpenWebIF.Timeout)
		if err != nil {
			return fmt.Errorf("invalid openWebIF.timeout: %w", err)
		}
		dst.Enigma2.Timeout = d
	}
	if src.OpenWebIF.Backoff != "" {
		d, err := time.ParseDuration(src.OpenWebIF.Backoff)
		if err != nil {
			return fmt.Errorf("invalid openWebIF.backoff: %w", err)
		}
		dst.Enigma2.Backoff = d
	}
	if src.OpenWebIF.MaxBackoff != "" {
		d, err := time.ParseDuration(src.OpenWebIF.MaxBackoff)
		if err != nil {
			return fmt.Errorf("invalid openWebIF.maxBackoff: %w", err)
		}
		dst.Enigma2.MaxBackoff = d
	}
	if src.OpenWebIF.Retries > 0 {
		dst.Enigma2.Retries = src.OpenWebIF.Retries
	}

	// Bouquets (join if multiple)
	if len(src.Bouquets) > 0 {
		dst.Bouquet = strings.Join(src.Bouquets, ",")
	}

	// Recording Playback (Path Mappings)
	dst.RecordingPlaybackPolicy = src.RecordingPlayback.PlaybackPolicy
	if src.RecordingPlayback.StableWindow != "" {
		if d, err := time.ParseDuration(src.RecordingPlayback.StableWindow); err == nil {
			dst.RecordingStableWindow = d
		}
	}
	dst.RecordingPathMappings = src.RecordingPlayback.Mappings

	// EPG - use pointer types to allow false/0 values from YAML
	if src.EPG.Enabled != nil {
		dst.EPGEnabled = *src.EPG.Enabled
	}
	if src.EPG.Days != nil {
		dst.EPGDays = *src.EPG.Days
	}
	if src.EPG.MaxConcurrency != nil {
		dst.EPGMaxConcurrency = *src.EPG.MaxConcurrency
	}
	if src.EPG.TimeoutMS != nil {
		dst.EPGTimeoutMS = *src.EPG.TimeoutMS
	}
	if src.EPG.Retries != nil {
		dst.EPGRetries = *src.EPG.Retries
	}
	if src.EPG.FuzzyMax != nil {
		dst.FuzzyMax = *src.EPG.FuzzyMax
	}
	if src.EPG.XMLTVPath != "" {
		dst.XMLTVPath = src.EPG.XMLTVPath
	}
	if src.EPG.Source != "" {
		dst.EPGSource = src.EPG.Source
	}

	// Recording Roots
	if len(src.Recording) > 0 {
		// Initialize if map is nil
		if dst.RecordingRoots == nil {
			dst.RecordingRoots = make(map[string]string)
		}
		for k, v := range src.Recording {
			dst.RecordingRoots[k] = v
		}
	}

	// Recording Playback
	if src.RecordingPlayback.PlaybackPolicy != "" {
		dst.RecordingPlaybackPolicy = src.RecordingPlayback.PlaybackPolicy
	}
	if src.RecordingPlayback.StableWindow != "" {
		d, err := time.ParseDuration(src.RecordingPlayback.StableWindow)
		if err != nil {
			return fmt.Errorf("invalid recording_playback.stable_window: %w", err)
		}
		dst.RecordingStableWindow = d
	}
	if len(src.RecordingPlayback.Mappings) > 0 {
		dst.RecordingPathMappings = append([]RecordingPathMapping(nil), src.RecordingPlayback.Mappings...)
	}

	// API
	if src.API.Token != "" {
		dst.APIToken = expandEnv(src.API.Token)
	}
	if len(src.API.TokenScopes) > 0 {
		dst.APITokenScopes = append([]string(nil), src.API.TokenScopes...)
	}
	if len(src.API.Tokens) > 0 {
		dst.APITokens = append([]ScopedToken(nil), src.API.Tokens...)
	}
	if src.API.ListenAddr != "" {
		dst.APIListenAddr = expandEnv(src.API.ListenAddr)
	}
	if src.API.RateLimit.Enabled != nil {
		dst.RateLimitEnabled = *src.API.RateLimit.Enabled
	}
	if src.API.RateLimit.Global != nil {
		dst.RateLimitGlobal = *src.API.RateLimit.Global
	}
	if src.API.RateLimit.Auth != nil {
		dst.RateLimitAuth = *src.API.RateLimit.Auth
	}
	if src.API.RateLimit.Burst != nil {
		dst.RateLimitBurst = *src.API.RateLimit.Burst
	}
	if len(src.API.RateLimit.Whitelist) > 0 {
		dst.RateLimitWhitelist = src.API.RateLimit.Whitelist
	}
	if len(src.API.AllowedOrigins) > 0 {
		dst.AllowedOrigins = src.API.AllowedOrigins
	}

	// Metrics
	if src.Metrics.Enabled != nil {
		dst.MetricsEnabled = *src.Metrics.Enabled
	}
	if src.Metrics.ListenAddr != "" {
		dst.MetricsAddr = expandEnv(src.Metrics.ListenAddr)
	}

	// Picons
	if src.Picons.BaseURL != "" {
		dst.PiconBase = expandEnv(src.Picons.BaseURL)
	}

	// Enigma2 (Map FileConfig.Enigma2 to AppConfig.Enigma2)
	// This maps the flat YAML Enigma2Config to the nested Enigma2Config in AppConfig
	if src.Enigma2.BaseURL != "" {
		dst.Enigma2.BaseURL = expandEnv(src.Enigma2.BaseURL)
	}
	if src.Enigma2.Username != "" {
		dst.Enigma2.Username = expandEnv(src.Enigma2.Username)
	}
	if src.Enigma2.Password != "" {
		dst.Enigma2.Password = expandEnv(src.Enigma2.Password)
	}
	if src.Enigma2.Timeout != "" {
		d, err := time.ParseDuration(src.Enigma2.Timeout)
		if err != nil {
			return fmt.Errorf("invalid enigma2.timeout: %w", err)
		}
		dst.Enigma2.Timeout = d
	}
	if src.Enigma2.ResponseHeaderTimeout != "" {
		d, err := time.ParseDuration(src.Enigma2.ResponseHeaderTimeout)
		if err != nil {
			return fmt.Errorf("invalid enigma2.responseHeaderTimeout: %w", err)
		}
		dst.Enigma2.ResponseHeaderTimeout = d
	}
	if src.Enigma2.Backoff != "" {
		d, err := time.ParseDuration(src.Enigma2.Backoff)
		if err != nil {
			return fmt.Errorf("invalid enigma2.backoff: %w", err)
		}
		dst.Enigma2.Backoff = d
	}
	if src.Enigma2.MaxBackoff != "" {
		d, err := time.ParseDuration(src.Enigma2.MaxBackoff)
		if err != nil {
			return fmt.Errorf("invalid enigma2.maxBackoff: %w", err)
		}
		dst.Enigma2.MaxBackoff = d
	}
	if src.Enigma2.Retries > 0 {
		dst.Enigma2.Retries = src.Enigma2.Retries
	}
	if src.Enigma2.RateLimit > 0 {
		dst.Enigma2.RateLimit = src.Enigma2.RateLimit
	}
	if src.Enigma2.RateBurst > 0 {
		dst.Enigma2.RateBurst = src.Enigma2.RateBurst
	}
	if src.Enigma2.UserAgent != "" {
		dst.Enigma2.UserAgent = src.Enigma2.UserAgent
	}
	if src.Enigma2.AnalyzeDuration != "" {
		dst.Enigma2.AnalyzeDuration = src.Enigma2.AnalyzeDuration
	}
	if src.Enigma2.ProbeSize != "" {
		dst.Enigma2.ProbeSize = src.Enigma2.ProbeSize
	}
	if src.Enigma2.FallbackTo8001 != nil {
		dst.Enigma2.FallbackTo8001 = *src.Enigma2.FallbackTo8001
	}

	// HDHomeRun
	if src.HDHR.Enabled != nil {
		dst.HDHR.Enabled = src.HDHR.Enabled
	}
	if src.HDHR.DeviceID != "" {
		dst.HDHR.DeviceID = expandEnv(src.HDHR.DeviceID)
	}
	if src.HDHR.FriendlyName != "" {
		dst.HDHR.FriendlyName = expandEnv(src.HDHR.FriendlyName)
	}
	if src.HDHR.ModelNumber != "" {
		dst.HDHR.ModelNumber = expandEnv(src.HDHR.ModelNumber)
	}
	if src.HDHR.FirmwareName != "" {
		dst.HDHR.FirmwareName = expandEnv(src.HDHR.FirmwareName)
	}
	if src.HDHR.BaseURL != "" {
		dst.HDHR.BaseURL = expandEnv(src.HDHR.BaseURL)
	}
	if src.HDHR.TunerCount != nil {
		dst.HDHR.TunerCount = src.HDHR.TunerCount
	}
	if src.HDHR.PlexForceHLS != nil {
		dst.HDHR.PlexForceHLS = src.HDHR.PlexForceHLS
	}

	// HLS (Typed Config)
	if src.HLS != nil {
		if src.HLS.Root != "" {
			dst.HLS.Root = expandEnv(src.HLS.Root)
		}
		if src.HLS.DVRWindow > 0 {
			dst.HLS.DVRWindow = src.HLS.DVRWindow
		}
		if src.HLS.SegmentSeconds > 0 {
			dst.HLS.SegmentSeconds = src.HLS.SegmentSeconds
		}
	}

	// Engine mapping (P1.2 Harden)
	if src.Engine.Enabled {
		dst.Engine.Enabled = true
	}
	if src.Engine.Mode != "" {
		dst.Engine.Mode = src.Engine.Mode
	}
	if src.Engine.IdleTimeout > 0 {
		dst.Engine.IdleTimeout = src.Engine.IdleTimeout
	}
	if len(src.Engine.TunerSlots) > 0 {
		dst.Engine.TunerSlots = src.Engine.TunerSlots
	}

	// TLS mapping (P1.2 Harden)
	if src.TLS.Enabled != nil {
		dst.TLSEnabled = *src.TLS.Enabled
	}
	if src.TLS.Cert != "" {
		dst.TLSCert = expandEnv(src.TLS.Cert)
	}
	if src.TLS.Key != "" {
		dst.TLSKey = expandEnv(src.TLS.Key)
	}
	if src.TLS.ForceHTTPS != nil {
		dst.ForceHTTPS = *src.TLS.ForceHTTPS
	}

	// Library mapping
	if src.Library.Enabled {
		dst.Library.Enabled = true
		dst.Library.DBPath = expandEnv(src.Library.DBPath)
		dst.Library.Roots = nil // Reset defaults if YAML provided
		for _, r := range src.Library.Roots {
			dst.Library.Roots = append(dst.Library.Roots, LibraryRootConfig{
				ID:         r.ID,
				Path:       expandEnv(r.Path),
				Type:       r.Type,
				MaxDepth:   r.MaxDepth,
				IncludeExt: r.IncludeExt,
			})
		}
	}

	// Root-level RecordingPathMappings mapping (P3)
	if len(src.RecordingPathMappings) > 0 {
		dst.RecordingPathMappings = append([]RecordingPathMapping(nil), src.RecordingPathMappings...)
	}

	// VOD (Typed config - with backwards-compat fallback to legacy flat fields)
	if src.VOD != nil {
		// Typed config takes precedence
		if src.VOD.ProbeSize != "" {
			dst.VODProbeSize = src.VOD.ProbeSize
		}
		if src.VOD.AnalyzeDuration != "" {
			dst.VODAnalyzeDuration = src.VOD.AnalyzeDuration
		}
		if src.VOD.StallTimeout != "" {
			if d, err := time.ParseDuration(src.VOD.StallTimeout); err == nil {
				dst.VODStallTimeout = d
			}
		}
		if src.VOD.MaxConcurrent > 0 {
			dst.VODMaxConcurrent = src.VOD.MaxConcurrent
		}
		if src.VOD.CacheTTL != "" {
			if d, err := time.ParseDuration(src.VOD.CacheTTL); err == nil {
				dst.VODCacheTTL = d
			}
		}
		if src.VOD.CacheMaxEntries > 0 {
			dst.VODCacheMaxEntries = src.VOD.CacheMaxEntries
		}

		// Keep typed config in sync
		dst.VOD = VODConfig{
			ProbeSize:       dst.VODProbeSize,
			AnalyzeDuration: dst.VODAnalyzeDuration,
			StallTimeout:    dst.VODStallTimeout.String(),
			MaxConcurrent:   dst.VODMaxConcurrent,
			CacheTTL:        dst.VODCacheTTL.String(),
			CacheMaxEntries: dst.VODCacheMaxEntries,
		}
	}

	return nil
}

// mergeEnvConfig merges environment variables into jobs.Config
// ENV variables have the highest precedence
// Uses consistent ParseBool/ParseInt/ParseDuration helpers from env.go
func (l *Loader) mergeEnvConfig(cfg *AppConfig) {
	// String values (direct assignment)
	cfg.Version = l.envString("XG2G_VERSION", cfg.Version)
	cfg.DataDir = l.envString("XG2G_DATA", cfg.DataDir)
	cfg.LogLevel = l.envString("XG2G_LOG_LEVEL", cfg.LogLevel)
	cfg.LogService = l.envString("XG2G_LOG_SERVICE", cfg.LogService)

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

	// Global strict mode
	cfg.ConfigStrict = l.envBool("XG2G_CONFIG_STRICT", cfg.ConfigStrict)

	// Bouquet
	cfg.Bouquet = l.envString("XG2G_BOUQUET", cfg.Bouquet)

	// EPG
	cfg.EPGEnabled = l.envBool("XG2G_EPG_ENABLED", cfg.EPGEnabled)
	cfg.EPGDays = l.envInt("XG2G_EPG_DAYS", cfg.EPGDays)
	cfg.EPGMaxConcurrency = l.envInt("XG2G_EPG_MAX_CONCURRENCY", cfg.EPGMaxConcurrency)
	cfg.EPGTimeoutMS = l.envInt("XG2G_EPG_TIMEOUT_MS", cfg.EPGTimeoutMS)
	cfg.EPGSource = l.envString("XG2G_EPG_SOURCE", cfg.EPGSource)
	cfg.EPGRetries = l.envInt("XG2G_EPG_RETRIES", cfg.EPGRetries)
	cfg.FuzzyMax = l.envInt("XG2G_FUZZY_MAX", cfg.FuzzyMax)
	cfg.XMLTVPath = l.envString("XG2G_XMLTV", cfg.XMLTVPath)

	// API
	cfg.APIToken = l.envString("XG2G_API_TOKEN", cfg.APIToken)
	cfg.APITokenScopes = parseCommaSeparated(l.envString("XG2G_API_TOKEN_SCOPES", ""), cfg.APITokenScopes)
	if tokens, err := parseScopedTokens(l.envString("XG2G_API_TOKENS", ""), cfg.APITokens); err != nil {
		cfg.apiTokensParseErr = err
	} else {
		cfg.APITokens = tokens
	}
	cfg.APIListenAddr = l.envString("XG2G_LISTEN", cfg.APIListenAddr)

	// CORS: ENV overrides YAML if set
	if rawOrigins, ok := l.envLookup("XG2G_ALLOWED_ORIGINS"); ok {
		if strings.TrimSpace(rawOrigins) != "" {
			cfg.AllowedOrigins = parseCommaSeparated(rawOrigins, nil)
		}
	}

	// Metrics
	metricsAddr := l.envString("XG2G_METRICS_LISTEN", "")
	if metricsAddr != "" {
		cfg.MetricsAddr = metricsAddr
		cfg.MetricsEnabled = true
	}

	// Picons
	cfg.PiconBase = l.envString("XG2G_PICON_BASE", cfg.PiconBase)

	// TLS
	cfg.TLSEnabled = l.envBool("XG2G_TLS_ENABLED", cfg.TLSEnabled)
	cfg.TLSCert = l.envString("XG2G_TLS_CERT", cfg.TLSCert)
	cfg.TLSKey = l.envString("XG2G_TLS_KEY", cfg.TLSKey)
	cfg.ForceHTTPS = l.envBool("XG2G_FORCE_HTTPS", cfg.ForceHTTPS)

	// Feature Flags
	cfg.ReadyStrict = l.envBool("XG2G_READY_STRICT", cfg.ReadyStrict)

	// CANONICAL ENGINE CONFIG
	cfg.Engine.Enabled = l.envBool("XG2G_ENGINE_ENABLED", cfg.Engine.Enabled)
	cfg.Engine.Mode = l.envString("XG2G_ENGINE_MODE", cfg.Engine.Mode)
	cfg.Engine.IdleTimeout = l.envDuration("XG2G_ENGINE_IDLE_TIMEOUT", cfg.Engine.IdleTimeout)
	cfg.Engine.CPUThresholdScale = l.envFloat("XG2G_ENGINE_CPU_SCALE", cfg.Engine.CPUThresholdScale)
	cfg.Engine.MaxPool = l.envInt("XG2G_ENGINE_MAX_POOL", cfg.Engine.MaxPool)
	cfg.Engine.GPULimit = l.envInt("XG2G_ENGINE_GPU_LIMIT", cfg.Engine.GPULimit)

	// CANONICAL ENIGMA2 CONFIG (Move up for discovery)
	cfg.Enigma2.BaseURL = l.envString("XG2G_E2_HOST", cfg.Enigma2.BaseURL)
	cfg.Enigma2.Username = l.envString("XG2G_E2_USER", cfg.Enigma2.Username)
	cfg.Enigma2.Password = l.envString("XG2G_E2_PASS", cfg.Enigma2.Password)
	cfg.Enigma2.AuthMode = strings.ToLower(strings.TrimSpace(l.envString("XG2G_E2_AUTH_MODE", cfg.Enigma2.AuthMode)))
	cfg.Enigma2.Timeout = l.envDuration("XG2G_E2_TIMEOUT", cfg.Enigma2.Timeout)
	cfg.Enigma2.ResponseHeaderTimeout = l.envDuration("XG2G_E2_RESPONSE_HEADER_TIMEOUT", cfg.Enigma2.ResponseHeaderTimeout)
	cfg.Enigma2.Retries = l.envInt("XG2G_E2_RETRIES", cfg.Enigma2.Retries)
	cfg.Enigma2.Backoff = l.envDuration("XG2G_E2_BACKOFF", cfg.Enigma2.Backoff)
	cfg.Enigma2.FallbackTo8001 = l.envBool("XG2G_E2_FALLBACK_TO_8001", cfg.Enigma2.FallbackTo8001)
	cfg.Enigma2.PreflightTimeout = l.envDuration("XG2G_E2_PREFLIGHT_TIMEOUT", cfg.Enigma2.PreflightTimeout)

	// Tuner Slots: Auto-Discovery with Manual Override
	// LOGIC: Auto-discover by default, only skip if manually configured
	manuallyConfigured := false

	// Check environment variable XG2G_TUNER_SLOTS
	if rawSlots, ok := l.envLookup("XG2G_TUNER_SLOTS"); ok {
		logger := log.WithComponent("config")
		if strings.TrimSpace(rawSlots) == "" {
			logger.Warn().Str("key", "XG2G_TUNER_SLOTS").Msg("empty tuner slots env var, will use auto-discovery")
		} else if slots, err := ParseTunerSlots(rawSlots); err == nil {
			cfg.Engine.TunerSlots = slots
			manuallyConfigured = true
			logger.Info().
				Ints("tuner_slots", slots).
				Msg("using manually configured tuner slots from environment")
		} else {
			logger.Warn().Str("key", "XG2G_TUNER_SLOTS").Str("value", rawSlots).Err(err).Msg("invalid tuner slots, will use auto-discovery")
		}
	}

	// Check if tunerSlots was set in YAML config
	if !manuallyConfigured && len(cfg.Engine.TunerSlots) > 0 {
		manuallyConfigured = true
		logger := log.WithComponent("config")
		logger.Info().
			Ints("tuner_slots", cfg.Engine.TunerSlots).
			Msg("using manually configured tuner slots from YAML")
	}

	// Auto-Discovery: Run ONLY if not manually configured AND Engine is enabled
	if !manuallyConfigured && cfg.Engine.Enabled {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if discovered, err := DiscoverTunerSlots(ctx, *cfg); err == nil && len(discovered) > 0 {
			cfg.Engine.TunerSlots = discovered
			logger := log.WithComponent("config")
			logger.Info().
				Ints("discovered_slots", discovered).
				Msg("using auto-discovered tuner slots from receiver")
		} else {
			// Auto-discovery failed, use fallback
			logger := log.WithComponent("config")
			logger.Warn().
				Err(err).
				Msg("tuner slot auto-discovery failed, using fallback")

			// Fallback: Virtual mode gets [0], otherwise log critical error
			if cfg.Engine.Mode == "virtual" {
				cfg.Engine.TunerSlots = []int{0}
				logger.Info().
					Msg("auto-discovery failed, defaulting to [0] for virtual mode")
			} else {
				logger.Error().
					Msg("no tuner slots configured or discovered - streaming will fail with 503")
			}
		}
	}

	// Final validation
	if len(cfg.Engine.TunerSlots) == 0 {
		logger := log.WithComponent("config")
		logger.Error().
			Msg("CRITICAL: no tuner slots available - all streaming requests will fail with 503")
	}

	// CANONICAL STORE CONFIG
	cfg.Store.Backend = l.envString("XG2G_STORE_BACKEND", cfg.Store.Backend)
	cfg.Store.Path = l.envString("XG2G_STORE_PATH", cfg.Store.Path)

	// CANONICAL HLS CONFIG
	cfg.HLS.Root = l.envString("XG2G_HLS_ROOT", cfg.HLS.Root)
	cfg.HLS.DVRWindow = l.envDuration("XG2G_HLS_DVR_WINDOW", cfg.HLS.DVRWindow)
	cfg.HLS.SegmentSeconds = l.envInt("XG2G_HLS_SEGMENT_SECONDS", cfg.HLS.SegmentSeconds)

	// CANONICAL FFMPEG CONFIG
	cfg.FFmpeg.Bin = l.envString("XG2G_FFMPEG_BIN", cfg.FFmpeg.Bin)
	cfg.FFmpeg.KillTimeout = l.envDuration("XG2G_FFMPEG_KILL_TIMEOUT", cfg.FFmpeg.KillTimeout)

	// Rate Limiting
	cfg.RateLimitEnabled = l.envBool("XG2G_RATE_LIMIT_ENABLED", cfg.RateLimitEnabled)
	cfg.RateLimitGlobal = l.envInt("XG2G_RATE_LIMIT_GLOBAL", cfg.RateLimitGlobal)
	cfg.RateLimitAuth = l.envInt("XG2G_RATE_LIMIT_AUTH", cfg.RateLimitAuth)
	cfg.RateLimitBurst = l.envInt("XG2G_RATE_LIMIT_BURST", cfg.RateLimitBurst)
	if whitelist := l.envString("XG2G_RATE_LIMIT_WHITELIST", ""); whitelist != "" {
		cfg.RateLimitWhitelist = parseCommaSeparated(whitelist, cfg.RateLimitWhitelist)
	}

	// Trusted Proxies
	cfg.TrustedProxies = l.envString("XG2G_TRUSTED_PROXIES", cfg.TrustedProxies)

	// Streaming Config (Canonical)
	cfg.Streaming.DeliveryPolicy = l.envString("XG2G_STREAMING_POLICY", cfg.Streaming.DeliveryPolicy)
}

// Helper to parse recording path mappings: "/receiver/path=/local/path;/other=/mount"
//
//nolint:unused
func parseRecordingMappings(envVal string, defaults []RecordingPathMapping) []RecordingPathMapping {
	if envVal == "" {
		return defaults
	}
	var out []RecordingPathMapping
	entries := strings.Split(envVal, ";")
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		kv := strings.SplitN(entry, "=", 2)
		if len(kv) != 2 {
			continue
		}
		receiverRoot := strings.TrimSpace(kv[0])
		localRoot := strings.TrimSpace(kv[1])
		if receiverRoot == "" || localRoot == "" {
			continue
		}
		out = append(out, RecordingPathMapping{
			ReceiverRoot: receiverRoot,
			LocalRoot:    localRoot,
		})
	}
	if len(out) == 0 {
		return defaults
	}
	return out
}

// Helper to parse scoped tokens from XG2G_API_TOKENS.
// JSON array format is canonical; legacy "token=scopes;token2=scopes2" remains supported.
func parseScopedTokens(envVal string, defaults []ScopedToken) ([]ScopedToken, error) {
	trimmed := strings.TrimSpace(envVal)
	if trimmed == "" {
		return defaults, nil
	}
	if strings.HasPrefix(trimmed, "[") {
		return parseScopedTokensJSON(trimmed)
	}
	if strings.HasPrefix(trimmed, "{") {
		return nil, fmt.Errorf("XG2G_API_TOKENS JSON must be an array of objects")
	}

	logger := log.WithComponent("config")
	logger.Warn().
		Str("key", "XG2G_API_TOKENS").
		Msg("legacy token format detected; JSON array is recommended")
	return parseScopedTokensLegacy(trimmed)
}

func parseScopedTokensJSON(raw string) ([]ScopedToken, error) {
	var entries []scopedTokenJSON
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		return nil, fmt.Errorf("XG2G_API_TOKENS JSON parse failed: %w", err)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("XG2G_API_TOKENS JSON array is empty")
	}
	seen := make(map[string]struct{}, len(entries))
	out := make([]ScopedToken, 0, len(entries))
	for _, entry := range entries {
		token := strings.TrimSpace(entry.Token)
		if token == "" {
			return nil, fmt.Errorf("XG2G_API_TOKENS token is empty")
		}
		if _, ok := seen[token]; ok {
			return nil, fmt.Errorf("XG2G_API_TOKENS duplicate token %q", token)
		}
		seen[token] = struct{}{}

		scopes := make([]string, 0, len(entry.Scopes))
		for _, scope := range entry.Scopes {
			scope = strings.TrimSpace(scope)
			if scope == "" {
				return nil, fmt.Errorf("XG2G_API_TOKENS scopes must not be empty for token %q", token)
			}
			scopes = append(scopes, scope)
		}
		if len(scopes) == 0 {
			return nil, fmt.Errorf("XG2G_API_TOKENS scopes must be set for token %q", token)
		}
		out = append(out, ScopedToken{
			Token:  token,
			Scopes: scopes,
		})
	}
	return out, nil
}

func parseScopedTokensLegacy(raw string) ([]ScopedToken, error) {
	entries := strings.Split(raw, ";")
	var out []ScopedToken
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		kv := strings.SplitN(entry, "=", 2)
		if len(kv) != 2 {
			return nil, fmt.Errorf("XG2G_API_TOKENS legacy entry must be token=scopes: %q", entry)
		}
		token := strings.TrimSpace(kv[0])
		scopesRaw := strings.TrimSpace(kv[1])
		if token == "" {
			return nil, fmt.Errorf("XG2G_API_TOKENS token is empty")
		}
		if _, ok := seen[token]; ok {
			return nil, fmt.Errorf("XG2G_API_TOKENS duplicate token %q", token)
		}
		seen[token] = struct{}{}
		if scopesRaw == "" {
			return nil, fmt.Errorf("XG2G_API_TOKENS scopes must be set for token %q", token)
		}
		scopes := parseCommaSeparated(scopesRaw, nil)
		if len(scopes) == 0 {
			return nil, fmt.Errorf("XG2G_API_TOKENS scopes must be set for token %q", token)
		}
		out = append(out, ScopedToken{
			Token:  token,
			Scopes: scopes,
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("XG2G_API_TOKENS has no valid token entries")
	}
	return out, nil
}

// Helper to parse comma-separated list
func parseCommaSeparated(envVal string, defaults []string) []string {
	if envVal == "" {
		return defaults
	}
	var out []string
	parts := strings.Split(envVal, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
