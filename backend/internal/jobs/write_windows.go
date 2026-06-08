// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

//go:build windows

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
func writeM3U(ctx context.Context, path string, items []playlist.Item, publicURL string, xTvgURL string) error {
	logger := xglog.FromContext(ctx)

	// Create temp file in same directory for atomic rename
	dir := filepath.Dir(path)
	tmpFile, err := os.CreateTemp(dir, ".xg2g-m3u-*.tmp")
	if err != nil {
		return WrapPlaylistWriteError(fmt.Errorf("create temp M3U file: %w", err))
	}
	tmpPath := tmpFile.Name()
	defer func() {
		if tmpFile != nil {
			tmpFile.Close()
			os.Remove(tmpPath)
		}
	}()

	// Write playlist to temp file
	if err := playlist.WriteM3U(tmpFile, items, publicURL, xTvgURL); err != nil {
		return WrapPlaylistWriteError(fmt.Errorf("write M3U data: %w", err))
	}

	// Close before rename (Windows requires this)
	if err := tmpFile.Close(); err != nil {
		return WrapPlaylistWriteError(fmt.Errorf("close temp M3U file: %w", err))
	}
	tmpFile = nil // Prevent double close in defer

	// Rename temp file to target (best-effort atomic on Windows)
	if err := os.Rename(tmpPath, path); err != nil {
		return WrapPlaylistWriteError(fmt.Errorf("rename M3U file: %w", err))
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
		return WrapXMLTVWriteError(fmt.Errorf("create temp XMLTV file: %w", err))
	}
	tmpPath := tmpFile.Name()
	defer func() {
		if tmpFile != nil {
			tmpFile.Close()
			os.Remove(tmpPath)
		}
	}()

	// Write the XMLTV content directly into the temp file we hold (no nested
	// temp+rename), so the data lands in the file we actually rename into place
	// and gets fsync'd — not into an orphaned inode.
	if err := epg.WriteXMLTVTo(tmpFile, tv); err != nil {
		return WrapXMLTVWriteError(fmt.Errorf("write XMLTV data: %w", err))
	}
	if err := tmpFile.Sync(); err != nil {
		return WrapXMLTVWriteError(fmt.Errorf("sync temp XMLTV file: %w", err))
	}

	// Close before rename (Windows requires this)
	if err := tmpFile.Close(); err != nil {
		return WrapXMLTVWriteError(fmt.Errorf("close temp XMLTV file: %w", err))
	}
	tmpFile = nil // Prevent double close in defer

	// Rename temp file to target (best-effort atomic on Windows)
	if err := os.Rename(tmpPath, path); err != nil {
		return WrapXMLTVWriteError(fmt.Errorf("rename XMLTV file: %w", err))
	}

	logger.Debug().Str("path", path).Msg("wrote XMLTV file")
	return nil
}
