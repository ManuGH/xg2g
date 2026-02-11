// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import (
	"fmt"
	"strings"
	"time"
)

// mergeFileConfig merges file configuration into jobs.Config.
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

	l.mergeFileCore(dst, src)
	if err := l.mergeFileEnigma2Aliases(dst, src); err != nil {
		return err
	}
	l.mergeFileBouquets(dst, src)
	l.mergeFileRecordingPlaybackCompat(dst, src)
	l.mergeFileEPG(dst, src)
	l.mergeFileRecordingRoots(dst, src)
	if err := l.mergeFileRecordingPlayback(dst, src); err != nil {
		return err
	}
	l.mergeFileAPI(dst, src)
	l.mergeFileNetwork(dst, src)
	l.mergeFileMetrics(dst, src)
	l.mergeFilePicons(dst, src)
	l.mergeFileHDHR(dst, src)
	l.mergeFileHLS(dst, src)
	l.mergeFileFFmpeg(dst, src)
	l.mergeFileEngine(dst, src)
	l.mergeFileTLS(dst, src)
	l.mergeFileLibrary(dst, src)
	l.mergeFileRootRecordingPathMappings(dst, src)
	if err := l.mergeFileVerification(dst, src); err != nil {
		return err
	}
	l.mergeFileResilience(dst, src)
	l.mergeFileVOD(dst, src)

	return nil
}

func (l *Loader) mergeFileCore(dst *AppConfig, src *FileConfig) {
	if src.DataDir != "" {
		dst.DataDir = expandEnv(src.DataDir)
	}
	if src.LogLevel != "" {
		dst.LogLevel = src.LogLevel
	}
}

func (l *Loader) mergeFileEnigma2Aliases(dst *AppConfig, src *FileConfig) error {
	// OpenWebIF and Enigma2 aliases map into the same runtime Enigma2 settings.
	// Preserve current precedence: openWebIF first, then enigma2.
	openPatch, err := enigma2FilePatchFromOpenWebIF(src.OpenWebIF)
	if err != nil {
		return err
	}
	applyEnigma2FilePatch(dst, openPatch)

	e2Patch, err := enigma2FilePatchFromEnigma2(src.Enigma2)
	if err != nil {
		return err
	}
	applyEnigma2FilePatch(dst, e2Patch)

	return nil
}

func (l *Loader) mergeFileBouquets(dst *AppConfig, src *FileConfig) {
	// Bouquets (join if multiple)
	if len(src.Bouquets) > 0 {
		dst.Bouquet = strings.Join(src.Bouquets, ",")
	}
}

func (l *Loader) mergeFileRecordingPlaybackCompat(dst *AppConfig, src *FileConfig) {
	// Recording Playback (Path Mappings)
	dst.RecordingPlaybackPolicy = src.RecordingPlayback.PlaybackPolicy
	if src.RecordingPlayback.StableWindow != "" {
		if d, err := time.ParseDuration(src.RecordingPlayback.StableWindow); err == nil {
			dst.RecordingStableWindow = d
		}
	}
	dst.RecordingPathMappings = src.RecordingPlayback.Mappings
}

func (l *Loader) mergeFileEPG(dst *AppConfig, src *FileConfig) {
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
}

func (l *Loader) mergeFileRecordingRoots(dst *AppConfig, src *FileConfig) {
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
}

func (l *Loader) mergeFileRecordingPlayback(dst *AppConfig, src *FileConfig) error {
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

	return nil
}

func (l *Loader) mergeFileAPI(dst *AppConfig, src *FileConfig) {
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
}

func (l *Loader) mergeFileNetwork(dst *AppConfig, src *FileConfig) {
	// Network outbound policy
	if src.Network.Outbound.Enabled != nil {
		dst.Network.Outbound.Enabled = *src.Network.Outbound.Enabled
	}
	if src.Network.Outbound.Allow.Hosts != nil {
		dst.Network.Outbound.Allow.Hosts = append([]string(nil), src.Network.Outbound.Allow.Hosts...)
	}
	if src.Network.Outbound.Allow.CIDRs != nil {
		dst.Network.Outbound.Allow.CIDRs = append([]string(nil), src.Network.Outbound.Allow.CIDRs...)
	}
	if src.Network.Outbound.Allow.Ports != nil {
		dst.Network.Outbound.Allow.Ports = append([]int(nil), src.Network.Outbound.Allow.Ports...)
	}
	if src.Network.Outbound.Allow.Schemes != nil {
		dst.Network.Outbound.Allow.Schemes = append([]string(nil), src.Network.Outbound.Allow.Schemes...)
	}
	if src.Network.LAN.Allow.CIDRs != nil {
		dst.Network.LAN.Allow.CIDRs = append([]string(nil), src.Network.LAN.Allow.CIDRs...)
	}
}

func (l *Loader) mergeFileMetrics(dst *AppConfig, src *FileConfig) {
	// Metrics
	if src.Metrics.Enabled != nil {
		dst.MetricsEnabled = *src.Metrics.Enabled
	}
	if src.Metrics.ListenAddr != "" {
		dst.MetricsAddr = expandEnv(src.Metrics.ListenAddr)
	}
}

func (l *Loader) mergeFilePicons(dst *AppConfig, src *FileConfig) {
	// Picons
	if src.Picons.BaseURL != "" {
		dst.PiconBase = expandEnv(src.Picons.BaseURL)
	}
}

func (l *Loader) mergeFileHDHR(dst *AppConfig, src *FileConfig) {
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
}

func (l *Loader) mergeFileHLS(dst *AppConfig, src *FileConfig) {
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
}

func (l *Loader) mergeFileFFmpeg(dst *AppConfig, src *FileConfig) {
	// FFmpeg (Typed Config)
	if src.FFmpeg != nil {
		if src.FFmpeg.Bin != "" {
			dst.FFmpeg.Bin = expandEnv(src.FFmpeg.Bin)
		}
		if src.FFmpeg.FFprobeBin != "" {
			dst.FFmpeg.FFprobeBin = expandEnv(src.FFmpeg.FFprobeBin)
		}
		if src.FFmpeg.KillTimeout > 0 {
			dst.FFmpeg.KillTimeout = src.FFmpeg.KillTimeout
		}
		if src.FFmpeg.VaapiDevice != "" {
			dst.FFmpeg.VaapiDevice = expandEnv(src.FFmpeg.VaapiDevice)
		}
	}
}

func (l *Loader) mergeFileEngine(dst *AppConfig, src *FileConfig) {
	// Engine mapping (P1.2 Harden)
	if src.Engine.Enabled != nil {
		dst.Engine.Enabled = *src.Engine.Enabled
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
}

func (l *Loader) mergeFileTLS(dst *AppConfig, src *FileConfig) {
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
}

func (l *Loader) mergeFileLibrary(dst *AppConfig, src *FileConfig) {
	// Library mapping (symmetric, fail-open-safe)
	if src.Library.Enabled != nil {
		dst.Library.Enabled = *src.Library.Enabled
	}

	if dst.Library.Enabled {
		if src.Library.DBPath != "" {
			dst.Library.DBPath = expandEnv(src.Library.DBPath)
		}
		if src.Library.Roots != nil {
			dst.Library.Roots = nil
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
	}
}

func (l *Loader) mergeFileRootRecordingPathMappings(dst *AppConfig, src *FileConfig) {
	// Root-level RecordingPathMappings mapping (P3)
	if len(src.RecordingPathMappings) > 0 {
		dst.RecordingPathMappings = append([]RecordingPathMapping(nil), src.RecordingPathMappings...)
	}
}

func (l *Loader) mergeFileVerification(dst *AppConfig, src *FileConfig) error {
	// Verification (Drift)
	if src.Verification != nil {
		if src.Verification.Enabled != nil {
			dst.Verification.Enabled = *src.Verification.Enabled
		}
		if src.Verification.Interval != "" {
			d, err := time.ParseDuration(src.Verification.Interval)
			if err != nil {
				return fmt.Errorf("invalid verification.interval: %w", err)
			}
			dst.Verification.Interval = d
		}
	}

	return nil
}

func (l *Loader) mergeFileResilience(dst *AppConfig, src *FileConfig) {
	// Sprint 1: Resilience Core (YAML Mapping)
	if src.Limits != nil {
		if src.Limits.MaxSessions > 0 {
			dst.Limits.MaxSessions = src.Limits.MaxSessions
		}
		if src.Limits.MaxTranscodes >= 0 { // 0 is valid "disable transcodes"
			dst.Limits.MaxTranscodes = src.Limits.MaxTranscodes
		}
	}

	if src.Timeouts != nil {
		if src.Timeouts.TranscodeStart > 0 {
			dst.Timeouts.TranscodeStart = src.Timeouts.TranscodeStart
		}
		if src.Timeouts.TranscodeNoProgress > 0 {
			dst.Timeouts.TranscodeNoProgress = src.Timeouts.TranscodeNoProgress
		}
		if src.Timeouts.KillGrace > 0 {
			dst.Timeouts.KillGrace = src.Timeouts.KillGrace
		}
	}

	if src.Breaker != nil {
		if src.Breaker.Window > 0 {
			dst.Breaker.Window = src.Breaker.Window
		}
		if src.Breaker.MinAttempts > 0 {
			dst.Breaker.MinAttempts = src.Breaker.MinAttempts
		}
		if src.Breaker.FailuresThreshold > 0 {
			dst.Breaker.FailuresThreshold = src.Breaker.FailuresThreshold
		}
		if src.Breaker.ConsecutiveThreshold > 0 {
			dst.Breaker.ConsecutiveThreshold = src.Breaker.ConsecutiveThreshold
		}
	}
}

func (l *Loader) mergeFileVOD(dst *AppConfig, src *FileConfig) {
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
}
