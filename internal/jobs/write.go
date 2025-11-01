// SPDX-License-Identifier: MIT

package jobs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/ManuGH/xg2g/internal/epg"
	xglog "github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/playlist"
	"github.com/google/renameio/v2"
)

// writeM3U safely writes the playlist with full durability guarantees using renameio
// This ensures atomic + durable writes: fsync before rename prevents data loss on power failure
func writeM3U(ctx context.Context, path string, items []playlist.Item) error {
	logger := xglog.FromContext(ctx)

	// renameio handles: temp file creation, fsync, atomic rename, cleanup on error
	pendingFile, err := renameio.NewPendingFile(path)
	if err != nil {
		return fmt.Errorf("create pending M3U file: %w", err)
	}
	defer func() {
		// Cleanup on error - renameio removes temp file if not committed
		if err := pendingFile.Cleanup(); err != nil {
			logger.Debug().Err(err).Msg("cleanup pending M3U file")
		}
	}()

	// Write playlist to pending file
	if err := playlist.WriteM3U(pendingFile, items); err != nil {
		return fmt.Errorf("write M3U data: %w", err)
	}

	// CloseAtomicallyReplace: fsync + rename (durable + atomic)
	if err := pendingFile.CloseAtomicallyReplace(); err != nil {
		return fmt.Errorf("atomically replace M3U file: %w", err)
	}

	return nil
}

// writeXMLTV writes XMLTV data with full durability guarantees using renameio
// This ensures atomic + durable writes: fsync before rename prevents data loss on power failure
func writeXMLTV(ctx context.Context, path string, tv epg.TV) error {
	logger := xglog.FromContext(ctx)

	// renameio handles: temp file creation, fsync, atomic rename, cleanup on error
	pendingFile, err := renameio.NewPendingFile(path)
	if err != nil {
		return fmt.Errorf("create pending XMLTV file: %w", err)
	}
	defer func() {
		// Cleanup on error - renameio removes temp file if not committed
		if err := pendingFile.Cleanup(); err != nil {
			logger.Debug().Err(err).Msg("cleanup pending XMLTV file")
		}
	}()

	// epg.WriteXMLTV needs a file path, so write to pending file path
	tmpPath := pendingFile.Name()
	if err := epg.WriteXMLTV(tv, tmpPath); err != nil {
		return fmt.Errorf("write XMLTV data: %w", err)
	}

	// CloseAtomicallyReplace: fsync + rename (durable + atomic)
	if err := pendingFile.CloseAtomicallyReplace(); err != nil {
		return fmt.Errorf("atomically replace XMLTV file: %w", err)
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
