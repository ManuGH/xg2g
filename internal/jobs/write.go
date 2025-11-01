// SPDX-License-Identifier: MIT

package jobs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ManuGH/xg2g/internal/epg"
	xglog "github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/playlist"
)

// writeM3U safely writes the playlist to a temporary file and renames it on success
// This ensures atomic writes - either the full file is written or nothing changes
func writeM3U(ctx context.Context, path string, items []playlist.Item) error {
	logger := xglog.FromContext(ctx)
	dir := filepath.Dir(path)
	tmpFile, err := os.CreateTemp(dir, "playlist-*.m3u.tmp")
	if err != nil {
		return fmt.Errorf("create temporary M3U file: %w", err)
	}
	// Defer a function to handle cleanup, logging any errors.
	closed := false
	defer func() {
		if !closed {
			if err := tmpFile.Close(); err != nil {
				logger.Warn().Err(err).Str("path", tmpFile.Name()).Msg("failed to close temporary file on error path")
			}
		}
		// Only remove the temp file if it still exists (i.e., rename failed).
		if _, statErr := os.Stat(tmpFile.Name()); !os.IsNotExist(statErr) {
			if err := os.Remove(tmpFile.Name()); err != nil {
				logger.Warn().Err(err).Str("path", tmpFile.Name()).Msg("failed to remove temporary file")
			}
		}
	}()

	if err := playlist.WriteM3U(tmpFile, items); err != nil {
		return fmt.Errorf("write to temporary M3U file: %w", err)
	}

	// Explicitly close before rename.
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temporary M3U file before rename: %w", err)
	}
	closed = true

	// Atomically rename the temporary file to the final destination
	if err := os.Rename(tmpFile.Name(), path); err != nil {
		return fmt.Errorf("rename temporary M3U file: %w", err)
	}

	return nil
}

// writeXMLTV writes XMLTV data atomically to the specified path
func writeXMLTV(ctx context.Context, path string, tv epg.TV) error {
	logger := xglog.FromContext(ctx)
	dir := filepath.Dir(path)
	tmpFile, err := os.CreateTemp(dir, "xmltv-*.xml.tmp")
	if err != nil {
		return fmt.Errorf("create temporary XMLTV file: %w", err)
	}

	closed := false
	defer func() {
		if !closed {
			if err := tmpFile.Close(); err != nil {
				logger.Warn().Err(err).Str("path", tmpFile.Name()).Msg("failed to close temporary XMLTV file")
			}
		}
		if _, statErr := os.Stat(tmpFile.Name()); !os.IsNotExist(statErr) {
			if err := os.Remove(tmpFile.Name()); err != nil {
				logger.Warn().Err(err).Str("path", tmpFile.Name()).Msg("failed to remove temporary XMLTV file")
			}
		}
	}()

	// Write XMLTV to temp file
	if err := epg.WriteXMLTV(tv, tmpFile.Name()); err != nil {
		return fmt.Errorf("write XMLTV data: %w", err)
	}

	// Close temp file
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temporary XMLTV file: %w", err)
	}
	closed = true

	// Atomically rename
	if err := os.Rename(tmpFile.Name(), path); err != nil {
		return fmt.Errorf("rename temporary XMLTV file: %w", err)
	}

	return nil
}

// makeStableIDFromSRef creates a deterministic, collision-resistant tvg-id from a service reference
// Using a hash ensures the ID is stable even if the channel name changes and avoids issues
// with special characters in the sRef.
func makeStableIDFromSRef(sref string) string {
	sum := sha256.Sum256([]byte(sref))
	return "sref-" + hex.EncodeToString(sum[:])
}
