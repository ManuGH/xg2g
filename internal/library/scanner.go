// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package library

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/recordings"
)

// Scanner walks filesystem and indexes media files.
type Scanner struct {
	store *Store
}

// NewScanner creates a new filesystem scanner.
func NewScanner(store *Store) *Scanner {
	return &Scanner{store: store}
}

// ScanRoot performs a full scan of a library root.
// Per P0+ Gate #1: Symlink-safe path confinement via EvalSymlinks.
// Per P0-Amendment #3: Single TX for atomic upserts.
func (sc *Scanner) ScanRoot(ctx context.Context, cfg RootConfig) (*ScanResult, error) {
	result := &ScanResult{
		RootID:      cfg.ID,
		Started:     time.Now(),
		FinalStatus: RootStatusOK,
	}

	// P0+ Gate #1: Resolve root path to handle symlinks
	rootResolved, err := filepath.EvalSymlinks(cfg.Path)
	if err != nil {
		// Root not accessible
		result.Finished = time.Now()
		result.FinalStatus = RootStatusFailed
		result.LastError = fmt.Sprintf("root path unresolvable: %v", err)
		return result, fmt.Errorf("resolve root path: %w", err)
	}

	// Ensure root is absolute and clean
	rootResolved = filepath.Clean(rootResolved)

	// Start transaction
	tx, err := sc.store.BeginTx(ctx)
	if err != nil {
		result.Finished = time.Now()
		result.FinalStatus = RootStatusFailed
		result.LastError = fmt.Sprintf("begin transaction: %v", err)
		return result, fmt.Errorf("begin tx: %w", err)
	}

	// Ensure rollback on error
	defer func() {
		if result.FinalStatus == RootStatusFailed || result.ErrorCount > 0 {
			_ = tx.Rollback()
		}
	}()

	// Walk filesystem
	scanTime := time.Now()
	err = filepath.WalkDir(cfg.Path, func(path string, d fs.DirEntry, walkErr error) error {
		// Check context cancellation
		select {
		case <-ctx.Done():
			result.FinalStatus = RootStatusFailed
			result.LastError = "scan cancelled"
			return ctx.Err()
		default:
		}

		// Handle walk errors
		if walkErr != nil {
			result.ErrorCount++
			logScanError(cfg.ID, "walk", walkErr, path)
			// Skip this subtree but continue scan
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		// Skip directories
		if d.IsDir() {
			// Check depth limit
			rel, err := filepath.Rel(cfg.Path, path)
			if err != nil {
				result.ErrorCount++
				return nil
			}
			depth := strings.Count(rel, string(os.PathSeparator))
			if cfg.MaxDepth > 0 && depth >= cfg.MaxDepth {
				return fs.SkipDir
			}
			return nil
		}

		// P0+ Gate #1: Symlink-safe path confinement
		fileResolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			// Cannot resolve symlink - skip
			result.ItemsSkipped++
			logScanError(cfg.ID, "symlink", err, path)
			return nil
		}

		// Validate file is within root bounds
		rel, err := filepath.Rel(rootResolved, fileResolved)
		if err != nil || strings.HasPrefix(rel, "..") {
			// Path escape attempt
			result.ErrorCount++
			logScanError(cfg.ID, "confinement", fmt.Errorf("path escape: %s", rel), path)
			return nil
		}

		// Check file extension
		ext := strings.ToLower(filepath.Ext(d.Name()))
		if !isAllowedExtension(ext, cfg.IncludeExt) {
			result.ItemsSkipped++
			return nil
		}

		// Stat file
		info, err := os.Stat(fileResolved)
		if err != nil {
			result.ErrorCount++
			logScanError(cfg.ID, "stat", err, path)
			return nil
		}

		// P0.1 Gate: Only index finished recordings (ADR-ENG-003)
		// Library is the sole authority for "finished" state (no DVR changes)
		lifecycleState := recordings.ClassifyLibrary(fileResolved, info, recordings.ClassifierConfig{
			StableWindow: 30 * time.Second, // NAS-safe default per ADR-ENG-003
			MinSizeBytes: 1 * 1024 * 1024,  // 1MB minimum
			AllowedExt:   []string{".ts", ".mp4", ".mkv"},
		})

		if lifecycleState != recordings.StateFinished {
			result.ItemsSkipped++
			// Scrubbed logging per P0 security
			hash := sha256.Sum256([]byte(rel))
			log.L().Debug().
				Str("root_id", cfg.ID).
				Str("rel_path_hash", fmt.Sprintf("%x", hash[:5])).
				Str("lifecycle_state", string(lifecycleState)).
				Msg("library scan: skip non-finished file")
			return nil
		}

		// Create item
		item := Item{
			RootID:    cfg.ID,
			RelPath:   rel,
			Filename:  d.Name(),
			SizeBytes: info.Size(),
			ModTime:   info.ModTime(),
			ScanTime:  scanTime,
			Status:    ItemStatusOK,
		}

		// Upsert item within TX
		if err := sc.store.UpsertItem(ctx, tx, item); err != nil {
			result.ErrorCount++
			logScanError(cfg.ID, "db", err, path)
			return nil
		}

		result.TotalScanned++
		return nil
	})

	// Handle walk completion
	if err != nil && err != context.Canceled {
		result.FinalStatus = RootStatusFailed
		result.LastError = err.Error()
		result.Finished = time.Now()
		return result, err
	}

	// Determine final status
	if result.ErrorCount > 0 {
		result.FinalStatus = RootStatusDegraded
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		result.FinalStatus = RootStatusFailed
		result.LastError = fmt.Sprintf("commit failed: %v", err)
		result.Finished = time.Now()
		return result, fmt.Errorf("commit tx: %w", err)
	}

	result.Finished = time.Now()
	return result, nil
}

// isAllowedExtension checks if a file extension is in the allowed list.
func isAllowedExtension(ext string, allowed []string) bool {
	if len(allowed) == 0 {
		return true // No filter
	}
	for _, a := range allowed {
		if strings.EqualFold(ext, a) {
			return true
		}
	}
	return false
}

// logScanError logs scan errors with scrubbed paths per P0-Amendment #6.
// Logs: root_id, event, error_code, rel_path_hash (NOT full paths).
func logScanError(rootID, event string, err error, path string) {
	// Hash relative path for privacy
	hash := sha256.Sum256([]byte(path))
	pathHash := fmt.Sprintf("%x", hash[:5]) // First 10 hex chars

	log.L().Warn().
		Str("root_id", rootID).
		Str("event", event).
		Str("rel_path_hash", pathHash).
		Err(err).
		Msg("library scan error")
}
