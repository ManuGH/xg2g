// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

//go:build !windows

package jobs

import (
	"context"
	"fmt"

	"github.com/ManuGH/xg2g/internal/epg"
	xglog "github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/playlist"
	"github.com/google/renameio/v2"
)

// writeM3U safely writes the playlist with full durability guarantees using renameio
// This ensures atomic + durable writes: fsync before rename prevents data loss on power failure
func writeM3U(ctx context.Context, path string, items []playlist.Item, publicURL string, xTvgURL string) error {
	logger := xglog.FromContext(ctx)

	// renameio handles: temp file creation, fsync, atomic rename, cleanup on error
	pendingFile, err := renameio.NewPendingFile(path)
	if err != nil {
		return WrapPlaylistWriteError(fmt.Errorf("create pending M3U file: %w", err))
	}
	defer func() {
		// Cleanup on error - renameio removes temp file if not committed
		if err := pendingFile.Cleanup(); err != nil {
			logger.Debug().Err(err).Msg("cleanup pending M3U file")
		}
	}()

	// Write playlist to pending file
	if err := playlist.WriteM3U(pendingFile, items, publicURL, xTvgURL); err != nil {
		return WrapPlaylistWriteError(fmt.Errorf("write M3U data: %w", err))
	}

	// CloseAtomicallyReplace: fsync + rename (durable + atomic)
	if err := pendingFile.CloseAtomicallyReplace(); err != nil {
		return WrapPlaylistWriteError(fmt.Errorf("atomically replace M3U file: %w", err))
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
		return WrapXMLTVWriteError(fmt.Errorf("create pending XMLTV file: %w", err))
	}
	defer func() {
		// Cleanup on error - renameio removes temp file if not committed
		if err := pendingFile.Cleanup(); err != nil {
			logger.Debug().Err(err).Msg("cleanup pending XMLTV file")
		}
	}()

	// Write the XMLTV content directly into renameio's pending file (mirrors
	// writeM3U above), so CloseAtomicallyReplace's fsync covers the real data.
	// The old path called epg.WriteXMLTV(tv, pendingFile.Name()), which did its
	// OWN temp+rename onto that name — orphaning the inode renameio holds open
	// and fsyncs. A power loss could then leave a 0-byte XMLTV despite the
	// "durable write" promise.
	if err := epg.WriteXMLTVTo(pendingFile, tv); err != nil {
		return WrapXMLTVWriteError(fmt.Errorf("write XMLTV data: %w", err))
	}

	// CloseAtomicallyReplace: fsync + rename (durable + atomic)
	if err := pendingFile.CloseAtomicallyReplace(); err != nil {
		return WrapXMLTVWriteError(fmt.Errorf("atomically replace XMLTV file: %w", err))
	}

	return nil
}
