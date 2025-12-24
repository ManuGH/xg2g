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

	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/v3/exec/ffmpeg"
	"github.com/ManuGH/xg2g/internal/v3/model"
)

// HLSStore defines the subset of Store operations needed for HLS serving.
type HLSStore interface {
	GetSession(ctx context.Context, id string) (*model.SessionRecord, error)
}

// ServeHLS handles requests for HLS playlists and segments.
// It enforces strict session validity and path security.
func ServeHLS(w http.ResponseWriter, r *http.Request, store HLSStore, hlsRoot, sessionID, filename string) {
	// 1. Path Security (Allowlist + Base)
	// User Requirement: "Erlaube nur Basenames... Erlaube nur Whitelist"
	cleanName := filepath.Base(filename)
	if cleanName != filename || filename == "." || filename == ".." {
		http.Error(w, "invalid filename path", http.StatusBadRequest)
		return
	}
	if !model.IsSafeSessionID(sessionID) {
		http.Error(w, "invalid session id", http.StatusBadRequest)
		return
	}

	isPlaylist := filename == "index.m3u8"
	isSegment := strings.HasPrefix(filename, "seg_") && strings.HasSuffix(filename, ".ts")

	if !isPlaylist && !isSegment {
		http.Error(w, "file type not allowed", http.StatusForbidden)
		return
	}

	// 2. Session Check
	rec, err := store.GetSession(r.Context(), sessionID)
	if err != nil {
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
	if rec.State != model.SessionReady && rec.State != model.SessionDraining {
		// User Req: "NEW/STARTING: 404 (Client retry)"
		// If FAILED/CANCELLED: 404
		http.Error(w, "session not ready", http.StatusNotFound)
		return
	}

	// 3. Resolve File Path
	// Layout: <root>/sessions/<sessionID>/<filename>
	// We reuse ffmpeg.SessionOutputDir to be consistent.
	// But api package imports exec/ffmpeg?
	// User said: "internal/v3/hls/layout.go als shared... Für 8-6 kannst du direkt ffmpeg/layout.go importieren, wenn kein Cycle entsteht".
	// exec imports model. api imports model.
	// api -> exec/ffmpeg check:
	// exec/ffmpeg imports model, log.
	// No cycle.
	sessionDir := ffmpeg.SessionOutputDir(hlsRoot, sessionID)
	filePath := filepath.Join(sessionDir, filename)

	// 4. Check File Existence
	info, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		// Normal during startup for segments or if playlist not yet promoted
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	if err != nil {
		log.L().Error().Err(err).Str("path", filePath).Msg("hls file stat failed")
		http.Error(w, "internal check failed", http.StatusInternalServerError)
		return
	}

	// 5. Set Headers
	if isPlaylist {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Cache-Control", "no-store")
	} else if isSegment {
		w.Header().Set("Content-Type", "video/MP2T")
		// User Req: "Cache-Control: public, max-age=60"
		w.Header().Set("Cache-Control", "public, max-age=60")
	}

	// 6. Serve Content (Supports Range)
	f, err := os.Open(filePath)
	if err != nil {
		http.Error(w, "failed to open file", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	http.ServeContent(w, r, cleanName, info.ModTime(), f)
}
