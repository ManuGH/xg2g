// SPDX-License-Identifier: MIT

//go:build windows
// +build windows

package jobs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ManuGH/xg2g/internal/epg"
	xglog "github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/playlist"
)

// writeM3U safely writes the playlist for Windows using temp file + rename
// Note: Windows doesn't support atomic rename with fsync like Unix
func writeM3U(ctx context.Context, path string, items []playlist.Item) error {
	logger := xglog.FromContext(ctx)

	// Create temp file in same directory for atomic rename
	dir := filepath.Dir(path)
	tmpFile, err := os.CreateTemp(dir, ".xg2g-m3u-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp M3U file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() {
		if tmpFile != nil {
			tmpFile.Close()
			os.Remove(tmpPath)
		}
	}()

	// Write playlist to temp file
	if err := playlist.WriteM3U(tmpFile, items); err != nil {
		return fmt.Errorf("write M3U data: %w", err)
	}

	// Close before rename (Windows requires this)
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp M3U file: %w", err)
	}
	tmpFile = nil // Prevent double close in defer

	// Rename temp file to target (best-effort atomic on Windows)
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename M3U file: %w", err)
	}

	logger.Debug().Str("path", path).Msg("wrote M3U file")
	return nil
}

// writeXMLTV writes XMLTV data for Windows using temp file + rename
// Note: Windows doesn't support atomic rename with fsync like Unix
func writeXMLTV(ctx context.Context, path string, tv epg.TV) error {
	logger := xglog.FromContext(ctx)

	// Create temp file in same directory for atomic rename
	dir := filepath.Dir(path)
	tmpFile, err := os.CreateTemp(dir, ".xg2g-xmltv-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp XMLTV file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() {
		if tmpFile != nil {
			tmpFile.Close()
			os.Remove(tmpPath)
		}
	}()

	// epg.WriteXMLTV needs a file path, so write to temp file path
	if err := epg.WriteXMLTV(tv, tmpPath); err != nil {
		return fmt.Errorf("write XMLTV data: %w", err)
	}

	// Close before rename (Windows requires this)
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp XMLTV file: %w", err)
	}
	tmpFile = nil // Prevent double close in defer

	// Rename temp file to target (best-effort atomic on Windows)
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename XMLTV file: %w", err)
	}

	logger.Debug().Str("path", path).Msg("wrote XMLTV file")
	return nil
}
