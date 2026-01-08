// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/log"
)

const (
	EnvHLSRoot       = "XG2G_HLS_ROOT"
	EnvLegacyHLSRoot = "XG2G_V3_HLS_ROOT"

	// LegacyDirName is the directory name used in v3.0/v3.1
	LegacyDirName = "v3-hls"
	// TargetDirName is the clean directory name for pipeline/v3.2+
	TargetDirName = "hls"

	// MarkerFile is created in the NEW root after a successful migration
	MarkerFile = ".xg2g_migrated_from_v3"
)

// ResolutionResult describes the outcome of HLS root resolution.
// It is authoritative for reporting and testing.
type ResolutionResult struct {
	EffectiveRoot string

	// Flags for observability
	UsedLegacyEnv    bool // True if XG2G_V3_HLS_ROOT was used
	UsedExplicitEnv  bool // True if any ENV var was used (disables auto-migration)
	Migrated         bool // True if a filesystem migration was performed
	MigrationSkipped bool // True if migration was needed but skipped (e.g. symlink)
	LegacySymlink    bool // True if legacy path is a symlink
	TargetExists     bool // True if target path already existed
	LegacyExists     bool // True if legacy path existed
}

// ResolveHLSRoot determines the HLS root directory based on configuration, environment, and filesystem state.
// It implements a safe, idempotent migration from 'v3-hls' to 'hls'.
func ResolveHLSRoot(configDataDir, newEnv, legacyEnv string) (ResolutionResult, error) {
	var res ResolutionResult

	// 0. Validate base directory
	configDataDir = strings.TrimSpace(configDataDir)
	if configDataDir == "" {
		return res, fmt.Errorf("configDataDir cannot be empty")
	}
	configDataDir = filepath.Clean(configDataDir)

	// 1. Environment Variable Precedence
	if newEnv != "" {
		res.EffectiveRoot = normalizePath(newEnv)
		res.UsedExplicitEnv = true
		if legacyEnv != "" {
			log.L().Warn().
				Str("new", newEnv).
				Str("legacy", legacyEnv).
				Msg("Both XG2G_HLS_ROOT and XG2G_V3_HLS_ROOT are set; ignoring legacy")
		}
		return res, validateRoot(res.EffectiveRoot)
	}

	if legacyEnv != "" {
		res.EffectiveRoot = normalizePath(legacyEnv)
		res.UsedExplicitEnv = true
		res.UsedLegacyEnv = true
		log.L().Warn().
			Str("legacy", legacyEnv).
			Msg("DEPRECATED: XG2G_V3_HLS_ROOT is set. Please migrate to XG2G_HLS_ROOT.")
		return res, validateRoot(res.EffectiveRoot)
	}

	// 2. Default Path Construction
	targetPath := filepath.Join(configDataDir, TargetDirName)
	legacyPath := filepath.Join(configDataDir, LegacyDirName)

	// Check existence
	// We use Lstat for legacy to detect symlinks specifically
	legacyInfo, err := os.Lstat(legacyPath)
	res.LegacyExists = err == nil
	if err == nil && (legacyInfo.Mode()&os.ModeSymlink != 0) {
		res.LegacySymlink = true
	}

	// Check target (Stat is used to follow symlinks to directories)
	targetInfo, err := os.Stat(targetPath)
	res.TargetExists = err == nil

	// 2b. Edge Case: Target exists but is a file
	if res.TargetExists && !targetInfo.IsDir() {
		return res, fmt.Errorf("hls target path %q exists but is a file", targetPath)
	}

	// 3. Selection Logic

	// Case A: Target already exists. Use it.
	if res.TargetExists {
		res.EffectiveRoot = targetPath

		// Check for potentially confusing state: Legacy also exists and has content
		if res.LegacyExists {
			if isActiveLegacyDir(legacyPath) {
				log.L().Warn().
					Str("target", targetPath).
					Str("legacy", legacyPath).
					Msg("Migration: Using target HLS root, but legacy directory also exists and is not empty. Manual cleanup may be required.")
			}
		}
		return res, nil
	}

	// Case B: Target missing, Legacy missing.
	// Defaults to new path.
	if !res.LegacyExists {
		res.EffectiveRoot = targetPath
		return res, nil
	}

	// Case C: Target missing, Legacy exists.
	// Migration candidate.

	// Safety Check 1: Symlinks
	if res.LegacySymlink {
		res.EffectiveRoot = legacyPath // Fallback to legacy
		res.MigrationSkipped = true
		log.L().Warn().
			Str("legacy", legacyPath).
			Msg("Migration: Legacy HLS root is a symlink. Skipping migration and using legacy path.")
		return res, validateRoot(res.EffectiveRoot)
	}

	// Perform Migration (Rename)
	logger := log.L().With().Str("from", legacyPath).Str("to", targetPath).Logger()
	logger.Info().Msg("Migration: Renaming legacy HLS root to new location")

	if err := os.Rename(legacyPath, targetPath); err != nil {
		// Fallback on failure
		logger.Warn().Err(err).Msg("Migration: Rename failed. Falling back to legacy path.")
		res.EffectiveRoot = legacyPath
		res.MigrationSkipped = true
		return res, validateRoot(res.EffectiveRoot)
	}

	// Migration Successful
	res.EffectiveRoot = targetPath
	res.Migrated = true

	// Create Marker
	writeMarker(targetPath, legacyPath)

	return res, nil
}

// normalizePath ensures paths are trimmed and cleaned
func normalizePath(p string) string {
	return filepath.Clean(strings.TrimSpace(p))
}

// validateRoot performs basic validation on explicit paths
func validateRoot(path string) error {
	if path == "" || path == "." || path == string(filepath.Separator) {
		return fmt.Errorf("invalid hls root path: %q", path)
	}
	return nil
}

// isActiveLegacyDir checks if the dir contains anything other than markers or emptiness
func isActiveLegacyDir(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	// Read a few entries
	entries, _ := f.ReadDir(5)
	for _, e := range entries {
		// Ignore hidden files or markers
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		// Found real content
		return true
	}
	return false
}

// writeMarker writes the migration marker file
func writeMarker(dir, oldPath string) {
	markerPath := filepath.Join(dir, MarkerFile)
	content := fmt.Sprintf("Migrated from: %s\nDate: %s\n", oldPath, time.Now().UTC().Format(time.RFC3339))

	if err := os.WriteFile(markerPath, []byte(content), 0600); err != nil {
		log.L().Warn().Err(err).Str("path", markerPath).Msg("Migration: Failed to write marker file")
	}
}
