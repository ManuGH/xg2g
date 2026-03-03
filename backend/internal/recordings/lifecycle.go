// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package recordings

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// LifecycleState represents recording lifecycle state per ADR-ENG-003
type LifecycleState string

const (
	StateRecording LifecycleState = "recording" // Active or unstable
	StateFinished  LifecycleState = "finished"  // Stable, ready for Library
)

// ClassifierConfig holds classification parameters
type ClassifierConfig struct {
	StableWindow time.Duration // default 30s (NAS-safe per ADR-ENG-003)
	MinSizeBytes int64         // default 1MB (avoid stub files)
	AllowedExt   []string      // default [".ts", ".mp4", ".mkv"]
}

// ClassifyLibrary determines if a file is finished (Library-only, requires abs path + stat).
// Returns StateFinished only if ALL criteria met:
//   - File stable (mod_time >= StableWindow ago)
//   - Size >= MinSizeBytes
//   - Extension in AllowedExt
//   - No lock markers (.partial/.lock/.tmp suffix or sibling .lock file)
//
// Per ADR-ENG-003 P0.1: This is Library-only authority. DVR has no lifecycle logic.
func ClassifyLibrary(absPath string, info os.FileInfo, cfg ClassifierConfig) LifecycleState {
	// 1. File Stability Check (Conservative 30s default for NFS attribute caching)
	if time.Since(info.ModTime()) < cfg.StableWindow {
		return StateRecording
	}

	// 2. Size Check (Avoid stub/fake files)
	if info.Size() < cfg.MinSizeBytes {
		return StateRecording
	}

	// 3. Extension Check (Whitelist only)
	ext := strings.ToLower(filepath.Ext(absPath))
	validExt := false
	for _, allowed := range cfg.AllowedExt {
		if ext == strings.ToLower(allowed) {
			validExt = true
			break
		}
	}
	if !validExt {
		return StateRecording
	}

	// 4. Lock Marker Check (Suffix + sibling lock file)
	if hasLockMarker(absPath) {
		return StateRecording
	}

	return StateFinished
}

// hasLockMarker checks for .partial/.lock/.tmp suffix or sibling lock file.
// Per P0.1 Review: absPath-based ONLY (safe for Library, NOT for DVR).
func hasLockMarker(absPath string) bool {
	lower := strings.ToLower(absPath)

	// Suffix markers
	if strings.HasSuffix(lower, ".partial") ||
		strings.HasSuffix(lower, ".lock") ||
		strings.HasSuffix(lower, ".tmp") {
		return true
	}

	// Sibling lock file check (e.g., movie.ts + movie.ts.lock)
	// Safe because absPath is verified by Library scanner path confinement
	lockPath := absPath + ".lock"
	if _, err := os.Stat(lockPath); err == nil {
		return true
	}

	return false
}
