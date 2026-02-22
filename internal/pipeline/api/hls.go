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

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/platform/fs"
	"github.com/ManuGH/xg2g/internal/platform/httpx"
	"github.com/rs/zerolog"
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

type hlsRequest struct {
	sessionID       string
	filename        string
	cleanName       string
	isPlaylist      bool
	isSegment       bool
	isLegacySegment bool
	isInit          bool
}

func validateRequest(w http.ResponseWriter, sessionID, filename string) (hlsRequest, bool) {
	req := hlsRequest{
		sessionID: sessionID,
		filename:  filename,
	}

	cleanName := filepath.Base(filename)
	if cleanName != filename || filename == "." || filename == ".." || strings.Contains(filename, "\\") {
		http.Error(w, "invalid filename path", http.StatusBadRequest)
		return hlsRequest{}, false
	}
	if !model.IsSafeSessionID(sessionID) {
		http.Error(w, "invalid session id", http.StatusBadRequest)
		return hlsRequest{}, false
	}

	req.cleanName = cleanName
	req.isPlaylist = filename == "index.m3u8" || filename == "stream.m3u8"
	req.isSegment = strings.HasPrefix(filename, "seg_") && (strings.HasSuffix(filename, ".ts") || strings.HasSuffix(filename, ".m4s"))
	req.isLegacySegment = strings.HasPrefix(filename, "stream") && strings.HasSuffix(filename, ".ts")
	req.isInit = filename == "init.mp4"

	if !req.isPlaylist && !req.isSegment && !req.isLegacySegment && !req.isInit {
		http.Error(w, "file type not allowed", http.StatusForbidden)
		return hlsRequest{}, false
	}

	return req, true
}

func touchPlaylistAccessTime(ctx context.Context, store HLSStore, req hlsRequest, rec *model.SessionRecord) {
	if !req.isPlaylist || rec == nil {
		return
	}
	const minAccessUpdateIntervalSec = int64(5)

	now := time.Now()
	nowUnix := now.Unix()
	if rec.LastAccessUnix != 0 && nowUnix-rec.LastAccessUnix < minAccessUpdateIntervalSec {
		return
	}

	updater, ok := store.(hlsSessionUpdater)
	if !ok {
		return
	}
	_, _ = updater.UpdateSession(ctx, req.sessionID, func(r *model.SessionRecord) error {
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

func resolveArtifact(hlsRoot string, req hlsRequest) (filePath, legacyFilePath string, err error) {
	relPath := filepath.Join("sessions", req.sessionID, req.filename)
	var legacyRelPath string
	if req.isLegacySegment || req.filename == "stream.m3u8" {
		relPath = filepath.Join(req.sessionID, req.filename)
	} else if req.filename == "index.m3u8" {
		legacyRelPath = filepath.Join(req.sessionID, "stream.m3u8")
	}

	filePath, err = fs.ConfineRelPath(hlsRoot, relPath)
	if err != nil {
		return "", "", err
	}
	if legacyRelPath == "" {
		return filePath, "", nil
	}

	legacyPath, legacyErr := fs.ConfineRelPath(hlsRoot, legacyRelPath)
	if legacyErr != nil {
		log.L().Warn().Err(legacyErr).Str("sid", req.sessionID).Str("file", req.filename).Msg("legacy hls path confinement failed")
		return filePath, "", nil
	}
	return filePath, legacyPath, nil
}

func awaitPlaylist(ctx context.Context, filePath string, req hlsRequest, rec *model.SessionRecord, logger zerolog.Logger) (os.FileInfo, error) {
	info, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		if req.isPlaylist && (rec.State == model.SessionNew || rec.State == model.SessionStarting || rec.State == model.SessionPriming) {
			logger.Info().Msg("playlist missing during start, polling...")

			ticker := time.NewTicker(250 * time.Millisecond)
			defer ticker.Stop()

			timeout := time.NewTimer(hlsPlaylistWaitTimeout)
			defer timeout.Stop()

		PollLoop:
			for {
				select {
				case <-ctx.Done():
					logger.Info().Msg("polling cancelled by request context")
					break PollLoop
				case <-timeout.C:
					logger.Info().Msg("polling finished without success (timeout)")
					break PollLoop
				case <-ticker.C:
					info, err = os.Stat(filePath)
					if err == nil {
						logger.Info().Msg("playlist appeared during polling")
						break PollLoop
					}
					if !os.IsNotExist(err) {
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

	return info, err
}

func rewritePlaylist(source io.Reader, rec *model.SessionRecord, logger zerolog.Logger) (*bytes.Reader, error) {
	forcePlaylistType := ""
	var insertStartTag string
	if rec.Profile.VOD {
		forcePlaylistType = "VOD"
	} else if rec.Profile.DVRWindowSec > 0 {
		forcePlaylistType = "EVENT"
		insertStartTag = fmt.Sprintf("#EXT-X-START:TIME-OFFSET=-%d,PRECISE=YES", rec.Profile.DVRWindowSec)
	}

	raw, err := io.ReadAll(io.LimitReader(source, 1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read playlist: %w", err)
	}

	insertedPlaylistType := false
	insertedStartTag := false
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
		if strings.HasPrefix(line, "#EXT-X-DISCONTINUITY") {
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan playlist: %w", err)
	}

	if insertStartTag != "" && insertedStartTag {
		logger.Debug().
			Int("dvr_window_sec", rec.Profile.DVRWindowSec).
			Str("start_tag", insertStartTag).
			Msg("injected EXT-X-START tag for Safari DVR")
	}

	return bytes.NewReader(b.Bytes()), nil
}

func serveArtifact(w http.ResponseWriter, r *http.Request, req hlsRequest, rec *model.SessionRecord, info os.FileInfo, filePath string, logger zerolog.Logger) {
	if req.isPlaylist {
		w.Header().Set("Content-Type", httpx.ContentTypeHLSPlaylist)
		w.Header().Set("Cache-Control", "no-store")
	} else if req.isSegment || req.isLegacySegment {
		if strings.HasSuffix(req.filename, ".m4s") {
			w.Header().Set("Content-Type", httpx.ContentTypeFMP4Segment)
		} else {
			w.Header().Set("Content-Type", httpx.ContentTypeHLSSegment)
		}
		w.Header().Set("Cache-Control", "public, max-age=60")
		w.Header().Set("Content-Encoding", "identity")
	} else if req.isInit {
		w.Header().Set("Content-Type", httpx.ContentTypeFMP4Segment)
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.Header().Set("Content-Encoding", "identity")
	}

	f, err := os.Open(filePath) // #nosec G304 -- filePath constructed from safe session dir + validated filename
	if err != nil {
		http.Error(w, "failed to open file", http.StatusInternalServerError)
		return
	}
	defer func() { _ = f.Close() }()

	if req.isPlaylist {
		playlist, rewriteErr := rewritePlaylist(f, rec, logger)
		if rewriteErr != nil {
			log.L().Error().Err(rewriteErr).Msg("failed to process playlist")
			http.Error(w, "failed to process file", http.StatusInternalServerError)
			return
		}
		http.ServeContent(w, r, req.cleanName, info.ModTime(), playlist)
		return
	}

	http.ServeContent(w, r, req.cleanName, info.ModTime(), f)
}

// ServeHLS handles requests for HLS playlists and segments.
// It enforces strict session validity and path security.
func ServeHLS(w http.ResponseWriter, r *http.Request, store HLSStore, hlsRoot, sessionID, filename string) {
	req, ok := validateRequest(w, sessionID, filename)
	if !ok {
		return
	}

	rec, err := store.GetSession(r.Context(), req.sessionID)
	if err != nil || rec == nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	if rec.ExpiresAtUnix > 0 && time.Now().Unix() > rec.ExpiresAtUnix {
		w.Header().Set("Cache-Control", "no-store")
		http.Error(w, "session expired", http.StatusGone)
		return
	}

	validState := rec.State == model.SessionReady ||
		rec.State == model.SessionDraining ||
		rec.State == model.SessionStarting ||
		rec.State == model.SessionNew ||
		rec.State == model.SessionPriming

	if !validState {
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

	touchPlaylistAccessTime(r.Context(), store, req, rec)

	filePath, legacyFilePath, err := resolveArtifact(hlsRoot, req)
	if err != nil {
		log.L().Warn().Err(err).Str("sid", req.sessionID).Str("file", req.filename).Msg("hls path confinement failed")
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}

	logger := log.L().With().Str("sid", req.sessionID).Str("file", req.filename).Str("path", filePath).Str("state", string(rec.State)).Logger()

	info, err := awaitPlaylist(r.Context(), filePath, req, rec, logger)

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

	serveArtifact(w, r, req, rec, info, filePath, logger)
}
