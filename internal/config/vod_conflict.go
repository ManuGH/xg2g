// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package config

import (
	"fmt"
	"strings"
	"time"
)

// checkVODConflicts detects split-brain scenarios where both typed vod.* and legacy flat VOD* fields
// are set with divergent values (YAML or ENV). This is fail-closed governance: if both sources exist
// and differ, the config load must fail.
func (l *Loader) checkVODConflicts(src *FileConfig) error {
	// Only check if typed VOD block is present in YAML
	if src.VOD == nil {
		return nil
	}

	// Check each field for divergence (YAML typed vs ENV legacy)
	// We only fail if BOTH are explicitly set AND they differ

	// ProbeSize
	if src.VOD.ProbeSize != "" {
		if envVal, ok := l.envLookup("XG2G_VOD_PROBE_SIZE"); ok {
			if strings.TrimSpace(src.VOD.ProbeSize) != strings.TrimSpace(envVal) {
				return fmt.Errorf("vod.probeSize (%q) conflicts with XG2G_VOD_PROBE_SIZE env (%q). Remove one source", src.VOD.ProbeSize, envVal)
			}
		}
	}

	// AnalyzeDuration
	if src.VOD.AnalyzeDuration != "" {
		if envVal, ok := l.envLookup("XG2G_VOD_ANALYZE_DURATION"); ok {
			if strings.TrimSpace(src.VOD.AnalyzeDuration) != strings.TrimSpace(envVal) {
				return fmt.Errorf("vod.analyzeDuration (%q) conflicts with XG2G_VOD_ANALYZE_DURATION env (%q). Remove one source", src.VOD.AnalyzeDuration, envVal)
			}
		}
	}

	// StallTimeout (duration comparison)
	if src.VOD.StallTimeout != "" {
		if envVal, ok := l.envLookup("XG2G_VOD_STALL_TIMEOUT"); ok {
			if !equalDuration(src.VOD.StallTimeout, envVal) {
				return fmt.Errorf("vod.stallTimeout (%q) conflicts with XG2G_VOD_STALL_TIMEOUT env (%q). Remove one source", src.VOD.StallTimeout, envVal)
			}
		}
	}

	// MaxConcurrent
	if src.VOD.MaxConcurrent > 0 {
		if _, ok := l.envLookup("XG2G_VOD_MAX_CONCURRENT"); ok {
			envInt := ParseInt("XG2G_VOD_MAX_CONCURRENT", 0)
			if src.VOD.MaxConcurrent != envInt {
				return fmt.Errorf("vod.maxConcurrent (%d) conflicts with XG2G_VOD_MAX_CONCURRENT env (%d). Remove one source", src.VOD.MaxConcurrent, envInt)
			}
		}
	}

	// CacheTTL (duration comparison)
	if src.VOD.CacheTTL != "" {
		if envVal, ok := l.envLookup("XG2G_VOD_CACHE_TTL"); ok {
			if !equalDuration(src.VOD.CacheTTL, envVal) {
				return fmt.Errorf("vod.cacheTTL (%q) conflicts with XG2G_VOD_CACHE_TTL env (%q). Remove one source", src.VOD.CacheTTL, envVal)
			}
		}
	}

	return nil
}

// equalDuration compares two duration strings for equality
func equalDuration(a, b string) bool {
	da, erra := time.ParseDuration(a)
	db, errb := time.ParseDuration(b)
	if erra != nil || errb != nil {
		// If either can't parse, compare as strings (lenient fallback)
		return strings.TrimSpace(a) == strings.TrimSpace(b)
	}
	return da == db
}
