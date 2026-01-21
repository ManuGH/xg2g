// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	xg2ghttp "github.com/ManuGH/xg2g/internal/control/http"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/platform/fs"
)

const hlsPlaylistWaitTimeout = 5 * time.Second

var pdtRe = regexp.MustCompile(`^#EXT-X-PROGRAM-DATE-TIME:(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?)(Z|[+-]\d{4})\s*$`)

func normalizeProgramDateTimeLine(line string) string {
	m := pdtRe.FindStringSubmatch(line)
	if m == nil {
		return line
	}
	base := m[1]
	off := m[2]

	if off == "Z" {
		return line
	}
	// off like +0000, -0130
	if len(off) == 5 {
		if off == "+0000" {
			return "#EXT-X-PROGRAM-DATE-TIME:" + base + "Z"
		}
		return "#EXT-X-PROGRAM-DATE-TIME:" + base + off[:3] + ":" + off[3:]
	}
	return line
}

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

	isPlaylist := filename == "index.m3u8" || filename == "stream.m3u8"
	isSegment := strings.HasPrefix(filename, "seg_") && (strings.HasSuffix(filename, ".ts") || strings.HasSuffix(filename, ".m4s"))
	isLegacySegment := strings.HasPrefix(filename, "stream") && strings.HasSuffix(filename, ".ts")
	isInit := filename == "init.mp4"

	if !isPlaylist && !isSegment && !isLegacySegment && !isInit {
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
		// 410 Gone: Session explicitly ended (better than 404 for Safari retry logic)
		w.Header().Set("Cache-Control", "no-store")
		http.Error(w, "session expired", http.StatusGone)
		return
	}

	// Validate State
	// User Req: "rec.State == READY (oder READY + DRAINING)"
	// Modified: Allow STARTING/NEW/PRIMING to proceed to file check/polling loop.
	validState := rec.State == model.SessionReady ||
		rec.State == model.SessionDraining ||
		rec.State == model.SessionStarting ||
		rec.State == model.SessionNew ||
		rec.State == model.SessionPriming

	if !validState {
		// Safari Fix: Use 410 Gone for terminal states (FAILED/CANCELLED/STOPPED)
		// This signals to Safari that the resource is intentionally unavailable and
		// reduces aggressive retry behavior during teardown.
		// For NEW/STARTING: Still use 404 (client should retry)
		statusCode := http.StatusNotFound
		message := "session not ready"
		if rec.State.IsTerminal() {
			statusCode = http.StatusGone
			message = "stream ended"
			w.Header().Set("Cache-Control", "no-store")
		}
		http.Error(w, message, statusCode)
		return
	}

	// 2b. Touch access time for idle timeout tracking (playlist requests only, throttled).
	if isPlaylist {
		const minAccessUpdateIntervalSec = int64(5)
		now := time.Now()
		nowUnix := now.Unix()
		if rec.LastAccessUnix == 0 || nowUnix-rec.LastAccessUnix >= minAccessUpdateIntervalSec {
			if updater, ok := store.(hlsSessionUpdater); ok {
				_, _ = updater.UpdateSession(r.Context(), sessionID, func(r *model.SessionRecord) error {
					if r == nil {
						return nil
					}
					if r.LastAccessUnix != 0 && nowUnix-r.LastAccessUnix < minAccessUpdateIntervalSec {
						return nil
					}
					r.LastAccessUnix = nowUnix
					r.LastPlaylistAccessAt = now // PR-P3-2: Deterministic idle truth
					return nil
				})
			}
		}
	}

	// 3. Resolve File Path
	// Layout (current): <root>/sessions/<sessionID>/<filename>
	// Legacy fallback: <root>/<sessionID>/<filename>
	relPath := filepath.Join("sessions", sessionID, filename)
	var legacyRelPath string
	if isLegacySegment || filename == "stream.m3u8" {
		relPath = filepath.Join(sessionID, filename)
	} else if filename == "index.m3u8" {
		legacyRelPath = filepath.Join(sessionID, "stream.m3u8")
	}

	filePath, err := fs.ConfineRelPath(hlsRoot, relPath)
	if err != nil {
		log.L().Warn().Err(err).Str("sid", sessionID).Str("file", filename).Msg("hls path confinement failed")
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	legacyFilePath := ""
	if legacyRelPath != "" {
		if legacyPath, err := fs.ConfineRelPath(hlsRoot, legacyRelPath); err == nil {
			legacyFilePath = legacyPath
		} else {
			log.L().Warn().Err(err).Str("sid", sessionID).Str("file", filename).Msg("legacy hls path confinement failed")
		}
	}

	// Debug Logging
	logger := log.L().With().Str("sid", sessionID).Str("file", filename).Str("path", filePath).Str("state", string(rec.State)).Logger()

	// 4. Check File Existence
	info, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		// If playlist is missing but session is potentially starting, wait a bit.
		if isPlaylist && (rec.State == model.SessionNew || rec.State == model.SessionStarting || rec.State == model.SessionPriming) {
			logger.Info().Msg("playlist missing during start, polling...")

			// Poll with Context + Timeout + Ticker
			ticker := time.NewTicker(250 * time.Millisecond)
			defer ticker.Stop()

			timeout := time.NewTimer(hlsPlaylistWaitTimeout)
			defer timeout.Stop()

		PollLoop:
			for {
				select {
				case <-r.Context().Done():
					// Context cancelled
					logger.Info().Msg("polling cancelled by request context")
					break PollLoop
				case <-timeout.C:
					// Timeout
					logger.Info().Msg("polling finished without success (timeout)")
					break PollLoop
				case <-ticker.C:
					info, err = os.Stat(filePath)
					if err == nil {
						logger.Info().Msg("playlist appeared during polling")
						break PollLoop
					}
					if !os.IsNotExist(err) {
						// Real error
						logger.Error().Err(err).Msg("playlist stat error during polling")
						break PollLoop
					}
				}
			}
		} else {
			logger.Info().Str("state", string(rec.State)).Msg("playlist missing, not polling (state mismatch)")
		}
	} else if err != nil {
		logger.Error().Err(err).Msg("initial stat failed")
	}

	if os.IsNotExist(err) && legacyFilePath != "" {
		legacyInfo, legacyErr := os.Stat(legacyFilePath)
		if legacyErr == nil {
			filePath = legacyFilePath
			info = legacyInfo
			err = nil
			logger = logger.With().Str("path", filePath).Bool("legacy", true).Logger()
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
		w.Header().Set("Content-Type", xg2ghttp.ContentTypeHLSPlaylist)
		w.Header().Set("Cache-Control", "no-store")
	} else if isSegment || isLegacySegment {
		// Set segment headers based on artifact kind (TS vs fMP4)
		if strings.HasSuffix(filename, ".m4s") {
			w.Header().Set("Content-Type", xg2ghttp.ContentTypeFMP4Segment)
		} else {
			w.Header().Set("Content-Type", xg2ghttp.ContentTypeHLSSegment)
		}
		// User Req: "Cache-Control: public, max-age=60"
		w.Header().Set("Cache-Control", "public, max-age=60")
		// CRITICAL: Disable compression for video segments (proxy-safe)
		// Safari cannot decode gzip-compressed fMP4 segments
		w.Header().Set("Content-Encoding", "identity")
	} else if isInit {
		w.Header().Set("Content-Type", xg2ghttp.ContentTypeFMP4Segment)
		w.Header().Set("Cache-Control", "public, max-age=3600")
		// CRITICAL: Disable compression for init segment (proxy-safe)
		w.Header().Set("Content-Encoding", "identity")
	}

	// 6. Serve Content (Supports Range)
	f, err := os.Open(filePath) // #nosec G304 -- filePath constructed from safe session dir + validated filename
	if err != nil {
		http.Error(w, "failed to open file", http.StatusInternalServerError)
		return
	}
	defer func() { _ = f.Close() }()

	// Special handling for playlists: normalize timestamps
	if isPlaylist {
		forcePlaylistType := ""
		var insertStartTag string
		if rec.Profile.VOD {
			forcePlaylistType = "VOD"
		} else if rec.Profile.DVRWindowSec > 0 {
			forcePlaylistType = "EVENT"

			// Safari DVR Scrubber Hint: EXT-X-START tells Safari the seekable range for DVR UI
			// TIME-OFFSET = negative value from live edge, PRECISE=YES for better seeking accuracy
			offset := rec.Profile.DVRWindowSec
			insertStartTag = fmt.Sprintf("#EXT-X-START:TIME-OFFSET=-%d,PRECISE=YES", offset)
		}
		insertedPlaylistType := false
		insertedStartTag := false

		// Read entire file, normalize lines, serve from buffer
		// Limit playlist size to avoid memory issues (e.g. 1MB is plenty for live/event HLS)
		raw, err := io.ReadAll(io.LimitReader(f, 1024*1024))
		if err != nil {
			log.L().Error().Err(err).Msg("failed to read playlist for normalization")
			http.Error(w, "failed to read file", http.StatusInternalServerError)
			return
		}

		var b bytes.Buffer
		scanner := bufio.NewScanner(bytes.NewReader(raw))
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "#EXT-X-PLAYLIST-TYPE:") && forcePlaylistType != "" {
				continue
			}
			if line == "#EXTM3U" && forcePlaylistType != "" && !insertedPlaylistType {
				b.WriteString(line)
				b.WriteByte('\n')
				b.WriteString("#EXT-X-PLAYLIST-TYPE:" + forcePlaylistType)
				b.WriteByte('\n')
				insertedPlaylistType = true

				// Insert EXT-X-START after PLAYLIST-TYPE for EVENT playlists (Safari DVR)
				if insertStartTag != "" && !insertedStartTag {
					b.WriteString(insertStartTag)
					b.WriteByte('\n')
					insertedStartTag = true
				}
				continue
			}
			if strings.HasPrefix(line, "#EXT-X-PROGRAM-DATE-TIME:") {
				line = normalizeProgramDateTimeLine(line)
			}
			// Sanitize: Remove EXT-X-DISCONTINUITY tags which can cause MediaError on Safari (Code 4)
			// especially when generated by FFmpeg's append_list behavior at the start of VOD playlists.
			if strings.HasPrefix(line, "#EXT-X-DISCONTINUITY") {
				continue
			}
			b.WriteString(line)
			b.WriteByte('\n')
		}

		// Log EXT-X-START injection for debugging
		if insertStartTag != "" && insertedStartTag {
			logger.Debug().
				Str("sid", sessionID).
				Int("dvr_window_sec", rec.Profile.DVRWindowSec).
				Str("start_tag", insertStartTag).
				Msg("injected EXT-X-START tag for Safari DVR")
		}

		if err := scanner.Err(); err != nil {
			log.L().Error().Err(err).Msg("failed to scan playlist for normalization")
			http.Error(w, "failed to process file", http.StatusInternalServerError)
			return
		}

		rdr := bytes.NewReader(b.Bytes())
		http.ServeContent(w, r, cleanName, info.ModTime(), rdr)
		return
	}

	http.ServeContent(w, r, cleanName, info.ModTime(), f)
}
