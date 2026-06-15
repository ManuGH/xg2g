// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package config

import (
	"fmt"
	"strconv"
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

	// A field conflicts only if BOTH sources are EFFECTIVE and differ. "Effective" for env is
	// the SAME predicate the env-merge uses (envPresent: set AND non-empty), so an empty
	// XG2G_VOD_* var — which the merge ignores — cannot trigger a phantom conflict against a
	// non-empty file value. Reads go through l.envLookup (not the process env) so the check
	// and the merge observe one source of truth.

	// ProbeSize
	if src.VOD.ProbeSize != "" {
		if envVal, present := envPresent(l.envLookup, "XG2G_VOD_PROBE_SIZE"); present {
			if strings.TrimSpace(src.VOD.ProbeSize) != strings.TrimSpace(envVal) {
				return fmt.Errorf("vod.probeSize (%q) conflicts with XG2G_VOD_PROBE_SIZE env (%q). Remove one source", src.VOD.ProbeSize, envVal)
			}
		}
	}

	// AnalyzeDuration
	if src.VOD.AnalyzeDuration != "" {
		if envVal, present := envPresent(l.envLookup, "XG2G_VOD_ANALYZE_DURATION"); present {
			if strings.TrimSpace(src.VOD.AnalyzeDuration) != strings.TrimSpace(envVal) {
				return fmt.Errorf("vod.analyzeDuration (%q) conflicts with XG2G_VOD_ANALYZE_DURATION env (%q). Remove one source", src.VOD.AnalyzeDuration, envVal)
			}
		}
	}

	// StallTimeout (duration comparison)
	if src.VOD.StallTimeout != "" {
		if envVal, present := envPresent(l.envLookup, "XG2G_VOD_STALL_TIMEOUT"); present {
			if !equalDuration(src.VOD.StallTimeout, envVal) {
				return fmt.Errorf("vod.stallTimeout (%q) conflicts with XG2G_VOD_STALL_TIMEOUT env (%q). Remove one source", src.VOD.StallTimeout, envVal)
			}
		}
	}

	// MaxConcurrent
	if src.VOD.MaxConcurrent > 0 {
		if envVal, present := envPresent(l.envLookup, "XG2G_VOD_MAX_CONCURRENT"); present {
			// Parse the value envPresent returned (NOT ParseInt, which reads the process env
			// and would bypass l.envLookup). A non-empty-but-unparseable env is ignored by
			// the merge too, so it cannot conflict.
			if envInt, err := strconv.Atoi(strings.TrimSpace(envVal)); err == nil && src.VOD.MaxConcurrent != envInt {
				return fmt.Errorf("vod.maxConcurrent (%d) conflicts with XG2G_VOD_MAX_CONCURRENT env (%d). Remove one source", src.VOD.MaxConcurrent, envInt)
			}
		}
	}

	// CacheTTL (duration comparison)
	if src.VOD.CacheTTL != "" {
		if envVal, present := envPresent(l.envLookup, "XG2G_VOD_CACHE_TTL"); present {
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
