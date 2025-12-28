// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/fsutil"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/v3/model"
)

const hlsPlaylistWaitTimeout = 5 * time.Second

// HLSStore defines the subset of Store operations needed for HLS serving.
type HLSStore interface {
	GetSession(ctx context.Context, id string) (*model.SessionRecord, error)
}

type hlsSessionUpdater interface {
	UpdateSession(ctx context.Context, id string, fn func(*model.SessionRecord) error) (*model.SessionRecord, error)
}

// ServeHLS handles requests for HLS playlists and segments.
// It enforces strict session validity and path security.
func ServeHLS(w http.ResponseWriter, r *http.Request, store HLSStore, hlsRoot, sessionID, filename string) {
	// 1. Path Security (Allowlist + Base)
	// User Requirement: "Erlaube nur Basenames... Erlaube nur Whitelist"
	cleanName := filepath.Base(filename)
	if cleanName != filename || filename == "." || filename == ".." || strings.Contains(filename, "\\") {
		http.Error(w, "invalid filename path", http.StatusBadRequest)
		return
	}
	if !model.IsSafeSessionID(sessionID) {
		http.Error(w, "invalid session id", http.StatusBadRequest)
		return
	}

	isPlaylist := filename == "index.m3u8"
	isSegment := strings.HasPrefix(filename, "seg_") && (strings.HasSuffix(filename, ".ts") || strings.HasSuffix(filename, ".m4s"))
	isInit := filename == "init.mp4"

	if !isPlaylist && !isSegment && !isInit {
		http.Error(w, "file type not allowed", http.StatusForbidden)
		return
	}

	// 2. Session Check
	rec, err := store.GetSession(r.Context(), sessionID)
	if err != nil || rec == nil {
		// Store error or Not Found logic
		// BoltStore returns specialized errors or we check for nil?
		// Assuming GetSession returns error if not found or system fail.
		// Detailed error handling would check typical "not found" semantics.
		// For 8-6 MVP: 404
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	// Validate Expiry
	// rec.ExpiresAtUnix (int64)
	if rec.ExpiresAtUnix > 0 && time.Now().Unix() > rec.ExpiresAtUnix {
		http.Error(w, "session expired", http.StatusNotFound)
		return
	}

	// Validate State
	// User Req: "rec.State == READY (oder READY + DRAINING)"
	// Modified: Allow STARTING/NEW to proceed to file check/polling loop.
	validState := rec.State == model.SessionReady ||
		rec.State == model.SessionDraining ||
		rec.State == model.SessionStarting ||
		rec.State == model.SessionNew

	if !validState {
		// User Req: "NEW/STARTING: 404 (Client retry)" -> Now handled by polling loop later
		// If FAILED/CANCELLED: 404
		http.Error(w, "session not ready", http.StatusNotFound)
		return
	}

	// 2b. Touch access time for idle timeout tracking (playlist requests only, throttled).
	if isPlaylist {
		const minAccessUpdateIntervalSec = int64(5)
		now := time.Now().Unix()
		if rec.LastAccessUnix == 0 || now-rec.LastAccessUnix >= minAccessUpdateIntervalSec {
			if updater, ok := store.(hlsSessionUpdater); ok {
				_, _ = updater.UpdateSession(r.Context(), sessionID, func(r *model.SessionRecord) error {
					if r == nil {
						return nil
					}
					if r.LastAccessUnix != 0 && now-r.LastAccessUnix < minAccessUpdateIntervalSec {
						return nil
					}
					r.LastAccessUnix = now
					return nil
				})
			}
		}
	}

	// 3. Resolve File Path
	// Layout: <root>/sessions/<sessionID>/<filename>
	relPath := filepath.Join("sessions", sessionID, filename)
	filePath, err := fsutil.ConfineRelPath(hlsRoot, relPath)
	if err != nil {
		log.L().Warn().Err(err).Str("sid", sessionID).Str("file", filename).Msg("hls path confinement failed")
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}

	// Debug Logging
	logger := log.L().With().Str("sid", sessionID).Str("file", filename).Str("path", filePath).Str("state", string(rec.State)).Logger()

	// 4. Check File Existence
	info, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		// If playlist is missing but session is potentially starting, wait a bit.
		if isPlaylist && (rec.State == model.SessionNew || rec.State == model.SessionStarting) {
			logger.Info().Msg("playlist missing during start, polling...")
			deadline := time.Now().Add(hlsPlaylistWaitTimeout)
			for time.Now().Before(deadline) {
				time.Sleep(250 * time.Millisecond)
				info, err = os.Stat(filePath)
				if err == nil {
					logger.Info().Msg("playlist appeared during polling")
					break
				}
				if !os.IsNotExist(err) {
					// Real error
					logger.Error().Err(err).Msg("playlist stat error during polling")
					break
				}
			}
		}
	}

	if os.IsNotExist(err) {
		logger.Warn().Err(err).Msg("file not found (final)")
		// Normal during startup for segments or if playlist not yet promoted
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	if err != nil {
		logger.Error().Err(err).Msg("hls file stat failed")
		http.Error(w, "internal check failed", http.StatusInternalServerError)
		return
	}
	if info.IsDir() {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}

	// 5. Set Headers
	if isPlaylist {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Cache-Control", "no-store")
	} else if isSegment {
		// TS segments: video/MP2T
		// fMP4 segments (.m4s): video/iso.segment
		if strings.HasSuffix(filename, ".m4s") {
			w.Header().Set("Content-Type", "video/iso.segment")
		} else {
			w.Header().Set("Content-Type", "video/MP2T")
		}
		// User Req: "Cache-Control: public, max-age=60"
		w.Header().Set("Cache-Control", "public, max-age=60")
	} else if isInit {
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Cache-Control", "public, max-age=3600")
	}

	// 6. Serve Content (Supports Range)
	f, err := os.Open(filePath) // #nosec G304 -- filePath constructed from safe session dir + validated filename
	if err != nil {
		http.Error(w, "failed to open file", http.StatusInternalServerError)
		return
	}
	defer func() { _ = f.Close() }()

	http.ServeContent(w, r, cleanName, info.ModTime(), f)
}
