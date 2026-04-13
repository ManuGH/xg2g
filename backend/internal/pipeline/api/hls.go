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

	"github.com/ManuGH/xg2g/internal/domain/session/lifecycle"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/platform/fs"
	"github.com/ManuGH/xg2g/internal/platform/httpx"
	platformpaths "github.com/ManuGH/xg2g/internal/platform/paths"
	"github.com/rs/zerolog"
)

const hlsStartupArtifactWaitTimeout = 5 * time.Second

const (
	hlsReasonHeader           = "X-XG2G-Reason"
	hlsReasonTranscodeStalled = "transcode_stalled"
	hlsReasonPlaylistMissing  = "playlist_missing"
	hlsReasonSegmentMissing   = "segment_missing"
)

var pdtRe = regexp.MustCompile(`^#EXT-X-PROGRAM-DATE-TIME:(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?)(Z|[+-]\d{4})\s*$`)
var safeHLSSessionIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
var safeHLSSegmentRe = regexp.MustCompile(`^seg_[A-Za-z0-9_-]+\.(?:ts|m4s)$`)
var safeHLSLegacySegmentRe = regexp.MustCompile(`^stream[A-Za-z0-9_-]*\.ts$`)

const (
	minPlaylistAccessUpdateInterval = time.Second
	minSegmentAccessUpdateInterval  = time.Second
	hlsRecentPlaylistWindow         = 8 * time.Second
	hlsSegmentStaleAfter            = 8 * time.Second
	hlsProducerSlowLag              = 8 * time.Second
	hlsProducerLateLag              = 15 * time.Second
	hlsPlaylistOnlyPollThreshold    = 2
)

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
	// Keep the path-component allowlist local so static analyzers can see that both
	// URL params are constrained before they ever participate in path construction.
	if !safeHLSSessionIDRe.MatchString(sessionID) || !model.IsSafeSessionID(sessionID) {
		http.Error(w, "invalid session id", http.StatusBadRequest)
		return hlsRequest{}, false
	}

	req.cleanName = cleanName
	switch {
	case filename == "index.m3u8" || filename == "stream.m3u8":
		req.isPlaylist = true
	case safeHLSSegmentRe.MatchString(filename):
		req.isSegment = true
	case safeHLSLegacySegmentRe.MatchString(filename):
		req.isLegacySegment = true
	case filename == "init.mp4":
		req.isInit = true
	}

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

	now := time.Now()
	// The first successful playlist GET after READY must always win, even if the
	// session just transitioned to READY and LastAccessUnix was set during startup.
	if !rec.LastPlaylistAccessAt.IsZero() &&
		now.Sub(rec.LastPlaylistAccessAt) < minPlaylistAccessUpdateInterval {
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
		if !r.LastPlaylistAccessAt.IsZero() &&
			now.Sub(r.LastPlaylistAccessAt) < minPlaylistAccessUpdateInterval {
			return nil
		}
		trace := ensureHLSAccessTrace(r)
		if !r.LastPlaylistAccessAt.IsZero() {
			trace.LastPlaylistIntervalMs = durationToMilliseconds(now.Sub(r.LastPlaylistAccessAt))
		}
		r.LastAccessUnix = now.Unix()
		r.LastPlaylistAccessAt = now // PR-P3-2: Deterministic idle truth
		trace.PlaylistRequestCount++
		trace.LastPlaylistAtUnix = now.Unix()
		updateHLSStallRisk(r, trace, now)
		return nil
	})
}

func touchSegmentAccessTime(ctx context.Context, store HLSStore, req hlsRequest, rec *model.SessionRecord) {
	if rec == nil || (!req.isSegment && !req.isLegacySegment) {
		return
	}

	updater, ok := store.(hlsSessionUpdater)
	if !ok {
		return
	}

	now := time.Now()
	_, _ = updater.UpdateSession(ctx, req.sessionID, func(r *model.SessionRecord) error {
		if r == nil {
			return nil
		}
		trace := ensureHLSAccessTrace(r)
		if trace.LastSegmentName == req.cleanName && trace.LastSegmentAtUnix > 0 {
			lastSegmentAt := time.Unix(trace.LastSegmentAtUnix, 0)
			if now.Sub(lastSegmentAt) < minSegmentAccessUpdateInterval {
				return nil
			}
		}
		if trace.LastSegmentAtUnix > 0 {
			trace.LastSegmentGapMs = durationToMilliseconds(now.Sub(time.Unix(trace.LastSegmentAtUnix, 0)))
		}
		trace.SegmentRequestCount++
		trace.LastSegmentAtUnix = now.Unix()
		trace.LastSegmentName = req.cleanName
		updateHLSStallRisk(r, trace, now)
		return nil
	})
}

func ensureHLSAccessTrace(rec *model.SessionRecord) *model.HLSAccessTrace {
	if rec.PlaybackTrace == nil {
		rec.PlaybackTrace = &model.PlaybackTrace{}
	}
	if rec.PlaybackTrace.HLS == nil {
		rec.PlaybackTrace.HLS = &model.HLSAccessTrace{}
	}
	return rec.PlaybackTrace.HLS
}

func durationToMilliseconds(d time.Duration) int {
	if d <= 0 {
		return 0
	}
	return int(d / time.Millisecond)
}

func latestProducerActivity(rec *model.SessionRecord) time.Time {
	if rec == nil {
		return time.Time{}
	}
	latest := rec.LatestSegmentAt
	if rec.PlaylistPublishedAt.After(latest) {
		latest = rec.PlaylistPublishedAt
	}
	return latest
}

func updateHLSStallRisk(rec *model.SessionRecord, trace *model.HLSAccessTrace, now time.Time) {
	if rec == nil || trace == nil {
		return
	}

	producerAt := latestProducerActivity(rec)
	if !producerAt.IsZero() {
		trace.LatestSegmentLagMs = durationToMilliseconds(now.Sub(producerAt))
	} else {
		trace.LatestSegmentLagMs = 0
	}

	lastPlaylistAt := rec.LastPlaylistAccessAt
	var lastSegmentAt time.Time
	if trace.LastSegmentAtUnix > 0 {
		lastSegmentAt = time.Unix(trace.LastSegmentAtUnix, 0)
	}

	switch {
	case trace.LatestSegmentLagMs >= durationToMilliseconds(hlsProducerLateLag):
		trace.StallRisk = "producer_late"
	case trace.LatestSegmentLagMs >= durationToMilliseconds(hlsProducerSlowLag):
		trace.StallRisk = "producer_slow"
	case !lastPlaylistAt.IsZero() && trace.PlaylistRequestCount >= hlsPlaylistOnlyPollThreshold && lastSegmentAt.IsZero():
		trace.StallRisk = "playlist_only"
	case !lastPlaylistAt.IsZero() && !lastSegmentAt.IsZero() &&
		now.Sub(lastPlaylistAt) <= hlsRecentPlaylistWindow &&
		now.Sub(lastSegmentAt) >= hlsSegmentStaleAfter:
		trace.StallRisk = "segment_stale"
	default:
		trace.StallRisk = "low"
	}
}

func persistHLSStartupPolicy(ctx context.Context, store HLSStore, sessionID string, policy hlsStartupPolicy) {
	updater, ok := store.(hlsSessionUpdater)
	if !ok || strings.TrimSpace(sessionID) == "" || policy.StartupHeadroomSec <= 0 {
		return
	}
	_, _ = updater.UpdateSession(ctx, sessionID, func(r *model.SessionRecord) error {
		if r == nil {
			return nil
		}
		trace := ensureHLSAccessTrace(r)
		if trace.StartupHeadroomSec == policy.StartupHeadroomSec &&
			trace.StartupMode == policy.Mode &&
			stringSlicesEqual(trace.StartupReasons, policy.Reasons) {
			return nil
		}
		trace.StartupHeadroomSec = policy.StartupHeadroomSec
		trace.StartupMode = policy.Mode
		trace.StartupReasons = append([]string(nil), policy.Reasons...)
		return nil
	})
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func resolveArtifact(hlsRoot string, req hlsRequest) (filePath, legacyFilePath string, err error) {
	relPath := platformpaths.LiveSessionArtifactRelPath(req.sessionID, req.filename)
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

func isStartupHLSState(state model.SessionState) bool {
	return state == model.SessionNew || state == model.SessionStarting || state == model.SessionPriming
}

func shouldPollMissingArtifact(req hlsRequest, rec *model.SessionRecord) bool {
	if rec == nil || !isStartupHLSState(rec.State) {
		return false
	}
	return req.isPlaylist || req.isSegment || req.isLegacySegment || req.isInit
}

func artifactKind(req hlsRequest) string {
	switch {
	case req.isPlaylist:
		return "playlist"
	case req.isInit:
		return "init"
	case req.isSegment || req.isLegacySegment:
		return "segment"
	default:
		return "artifact"
	}
}

func awaitArtifact(ctx context.Context, filePath string, req hlsRequest, rec *model.SessionRecord, logger zerolog.Logger) (os.FileInfo, error) {
	info, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		if shouldPollMissingArtifact(req, rec) {
			logger.Info().Str("artifact", artifactKind(req)).Msg("artifact missing during start, polling")

			ticker := time.NewTicker(250 * time.Millisecond)
			defer ticker.Stop()

			timeout := time.NewTimer(hlsStartupArtifactWaitTimeout)
			defer timeout.Stop()

		PollLoop:
			for {
				select {
				case <-ctx.Done():
					logger.Info().Str("artifact", artifactKind(req)).Msg("artifact polling cancelled by request context")
					break PollLoop
				case <-timeout.C:
					logger.Info().Str("artifact", artifactKind(req)).Msg("artifact polling finished without success (timeout)")
					break PollLoop
				case <-ticker.C:
					info, err = os.Stat(filePath)
					if err == nil {
						logger.Info().Str("artifact", artifactKind(req)).Msg("artifact appeared during polling")
						break PollLoop
					}
					if !os.IsNotExist(err) {
						logger.Error().Err(err).Str("artifact", artifactKind(req)).Msg("artifact stat error during polling")
						break PollLoop
					}
				}
			}
		} else if req.isPlaylist && rec != nil {
			logger.Info().Str("state", string(rec.State)).Msg("playlist missing, not polling (state mismatch)")
		}
	} else if err != nil {
		logger.Error().Err(err).Str("artifact", artifactKind(req)).Msg("initial stat failed")
	}

	return info, err
}

func rewritePlaylist(source io.Reader, rec *model.SessionRecord, logger zerolog.Logger) (*bytes.Reader, *hlsStartupPolicy, error) {
	forcePlaylistType := ""
	insertStartTag := ""
	var startupPolicy *hlsStartupPolicy
	if rec.Profile.VOD {
		forcePlaylistType = "VOD"
	} else if rec.Profile.DVRWindowSec > 0 {
		forcePlaylistType = "EVENT"
	}

	raw, err := io.ReadAll(io.LimitReader(source, 1024*1024))
	if err != nil {
		return nil, nil, fmt.Errorf("read playlist: %w", err)
	}
	if forcePlaylistType == "EVENT" {
		// Start Safari with explicit headroom behind live instead of at the full DVR window head.
		// The reserve absorbs playlist polling and segment timing jitter that otherwise shows up as
		// immediate rebuffering after a seemingly clean start on fragile/native HLS clients.
		policy := deriveHLSStartupPolicy(rec, raw)
		startupPolicy = &policy
		insertStartTag = fmt.Sprintf("#EXT-X-START:TIME-OFFSET=-%d,PRECISE=YES", startupPolicy.StartupHeadroomSec)
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
		return nil, startupPolicy, fmt.Errorf("scan playlist: %w", err)
	}

	if insertStartTag != "" && insertedStartTag && startupPolicy != nil {
		logger.Debug().
			Int("dvr_window_sec", rec.Profile.DVRWindowSec).
			Str("startup_mode", startupPolicy.Mode).
			Str("client_family", startupPolicy.ClientFamily).
			Int("startup_headroom_sec", startupPolicy.StartupHeadroomSec).
			Strs("startup_reasons", startupPolicy.Reasons).
			Str("start_tag", insertStartTag).
			Msg("injected EXT-X-START tag from HLS startup policy")
	}

	return bytes.NewReader(b.Bytes()), startupPolicy, nil
}

func serveArtifact(w http.ResponseWriter, r *http.Request, store HLSStore, req hlsRequest, rec *model.SessionRecord, info os.FileInfo, filePath string, logger zerolog.Logger) {
	if req.isPlaylist {
		w.Header().Set("Content-Type", httpx.ContentTypeHLSPlaylist)
		w.Header().Set("Cache-Control", "no-store")
	} else if req.isSegment || req.isLegacySegment {
		if strings.HasSuffix(req.filename, ".m4s") {
			w.Header().Set("Content-Type", httpx.ContentTypeFMP4Segment)
		} else {
			w.Header().Set("Content-Type", httpx.ContentTypeHLSSegment)
		}
		// Live session restarts reuse the same session directory and segment names.
		// After a client-triggered fallback, cached seg_000000.ts / seg_000001.ts from
		// the failed attempt can poison Safari recovery unless every live artifact is
		// treated as volatile.
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Encoding", "identity")
	} else if req.isInit {
		w.Header().Set("Content-Type", httpx.ContentTypeFMP4Segment)
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Encoding", "identity")
	}

	f, err := os.Open(filePath) // #nosec G304 -- filePath constructed from safe session dir + validated filename
	if err != nil {
		http.Error(w, "failed to open file", http.StatusInternalServerError)
		return
	}
	defer func() { _ = f.Close() }()

	if req.isPlaylist {
		playlist, startupPolicy, rewriteErr := rewritePlaylist(f, rec, logger)
		if rewriteErr != nil {
			log.L().Error().Err(rewriteErr).Msg("failed to process playlist")
			http.Error(w, "failed to process file", http.StatusInternalServerError)
			return
		}
		if startupPolicy != nil {
			persistHLSStartupPolicy(r.Context(), store, req.sessionID, *startupPolicy)
		}
		http.ServeContent(w, r, req.cleanName, info.ModTime(), playlist)
		return
	}

	http.ServeContent(w, r, req.cleanName, info.ModTime(), f)
}

func setHLSFailureHintHeader(w http.ResponseWriter, rec *model.SessionRecord) {
	if rec == nil {
		return
	}
	if lifecycle.PublicOutcomeFromRecord(rec).DetailCode == model.DTranscodeStalled {
		w.Header().Set(hlsReasonHeader, hlsReasonTranscodeStalled)
	}
}

func setHLSMissingArtifactHintHeader(w http.ResponseWriter, req hlsRequest, rec *model.SessionRecord) {
	if rec == nil || rec.State.IsTerminal() {
		return
	}
	if req.isPlaylist {
		w.Header().Set(hlsReasonHeader, hlsReasonPlaylistMissing)
		return
	}
	if req.isSegment || req.isLegacySegment || req.isInit {
		w.Header().Set(hlsReasonHeader, hlsReasonSegmentMissing)
	}
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
			setHLSFailureHintHeader(w, rec)
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

	info, err := awaitArtifact(r.Context(), filePath, req, rec, logger)

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
		setHLSMissingArtifactHintHeader(w, req, rec)
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

	touchSegmentAccessTime(r.Context(), store, req, rec)
	serveArtifact(w, r, store, req, rec, info, filePath, logger)
}
