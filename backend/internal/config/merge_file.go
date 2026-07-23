// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import (
	"fmt"
	"maps"
	"strings"
	"time"

	householddomain "github.com/ManuGH/xg2g/internal/household"
)

// mergeFileConfig merges file configuration into jobs.Config.
func (l *Loader) mergeFileConfig(dst *AppConfig, src *FileConfig) error {
	if err := rejectLegacyOpenWebIFYAML(l.filePresence); err != nil {
		return err
	}
	if legacyKeys := legacyOpenWebIFKeysFromConfig(src); len(legacyKeys) > 0 {
		return legacyOpenWebIFYAMLError(legacyKeys)
	}
	if err := l.checkAliasConflicts(src); err != nil {
		return err
	}
	if err := l.checkVODConflicts(src); err != nil {
		return err
	}

	l.mergeFileConfigGenerated(dst, src)

	if err := l.mergeFileEnigma2Aliases(dst, src); err != nil {
		return err
	}
	l.mergeFileBouquets(dst, src)
	l.mergeFileRecordingRoots(dst, src)
	if err := l.mergeFileRecordingPlayback(dst, src); err != nil {
		return err
	}
	if err := l.mergeFileAPI(dst, src); err != nil {
		return err
	}
	l.mergeFileLibrary(dst, src)
	l.mergeFileRootRecordingPathMappings(dst, src)
	if err := l.mergeFileVerification(dst, src); err != nil {
		return err
	}
	if err := l.mergeFileHousehold(dst, src); err != nil {
		return err
	}
	l.mergeFileVOD(dst, src)

	return nil
}

func (l *Loader) mergeFileEnigma2Aliases(dst *AppConfig, src *FileConfig) error {
	// YAML file config is canonical-only: openWebIF.* is rejected before merge.
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

func (l *Loader) mergeFileRecordingRoots(dst *AppConfig, src *FileConfig) {
	// Recording Roots
	if len(src.Recording) > 0 {
		// Initialize if map is nil
		if dst.RecordingRoots == nil {
			dst.RecordingRoots = make(map[string]string)
		}
		maps.Copy(dst.RecordingRoots, src.Recording)
	}
}

func (l *Loader) mergeFileHousehold(dst *AppConfig, src *FileConfig) error {
	if src.Household == nil {
		return nil
	}

	if strings.TrimSpace(src.Household.UnlockTTL) != "" {
		d, err := time.ParseDuration(strings.TrimSpace(src.Household.UnlockTTL))
		if err != nil {
			return fmt.Errorf("invalid household.unlockTTL: %w", err)
		}
		if d <= 0 {
			return fmt.Errorf("invalid household.unlockTTL: must be greater than 0")
		}
		dst.Household.UnlockTTL = d
	}

	if strings.TrimSpace(src.Household.Pin) != "" {
		hash, err := householddomain.HashPIN(src.Household.Pin)
		if err != nil {
			return fmt.Errorf("invalid household.pin: %w", err)
		}
		dst.Household.PinHash = hash
		return nil
	}

	if strings.TrimSpace(src.Household.PinHash) != "" {
		dst.Household.PinHash = strings.TrimSpace(src.Household.PinHash)
	}

	return nil
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

func (l *Loader) mergeFileAPI(dst *AppConfig, src *FileConfig) error {
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
	if src.API.DisableLegacyTokenSources != nil {
		dst.APIDisableLegacyTokenSources = *src.API.DisableLegacyTokenSources
	}
	if src.API.LegacyEnabled != nil {
		dst.APILegacyEnabled = *src.API.LegacyEnabled
	}
	if src.API.PlaybackDecisionSecret != "" {
		dst.PlaybackDecisionSecret = expandEnv(src.API.PlaybackDecisionSecret)
	}
	if src.API.PlaybackDecisionKeyID != "" {
		dst.PlaybackDecisionKeyID = expandEnv(src.API.PlaybackDecisionKeyID)
	}
	if len(src.API.PlaybackDecisionPreviousKeys) > 0 {
		dst.PlaybackDecisionPreviousKeys = append([]string(nil), src.API.PlaybackDecisionPreviousKeys...)
	}
	if src.API.PlaybackDecisionRotationWindow != "" {
		d, err := time.ParseDuration(src.API.PlaybackDecisionRotationWindow)
		if err != nil {
			return fmt.Errorf("invalid api.playbackDecisionRotationWindow: %w", err)
		}
		dst.PlaybackDecisionRotationWindow = d
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
	return nil
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
