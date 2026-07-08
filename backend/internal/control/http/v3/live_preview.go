// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/ManuGH/xg2g/internal/platform/paths"
)

// livePreviewFilename is intercepted on the existing ServeHLS route
// (/sessions/{sessionID}/hls/{filename}) so the DVR scrub preview inherits the
// session's exact auth/scope/exposure — and never touches the session
// orchestrator. The FE fetches .../hls/preview.jpg?t=<seconds-from-window-start>.
const livePreviewFilename = "preview.jpg"

const (
	livePreviewBuildTimeout = 5 * time.Second
	livePreviewMaxWidth     = 320
	livePreviewCacheEntries = 256
)

// serveLivePreviewFrame extracts a single keyframe thumbnail from the live DVR
// segments at the requested window offset and returns it as JPEG. Each HLS
// segment starts with an IDR (independent_segments + force_key_frames), so we
// extract that one keyframe — near-free CPU. Results are cached per
// (session, segment) and deduped with singleflight, so repeated/adjacent hovers
// over the same 6s segment cost nothing after the first.
func (s *Server) serveLivePreviewFrame(w http.ResponseWriter, r *http.Request, hlsRoot string, segSeconds int, ffmpegBin, sessionID string) {
	dir := paths.LiveSessionDir(hlsRoot, sessionID)

	// Prefer the playlist's segment list over raw disk scan: the rolling DVR
	// window can leave expired segments on disk after FFmpeg's async cleanup,
	// and picking one of those would serve a thumbnail misaligned with the
	// player's seekable range. Fall back to a disk scan if the playlist is not
	// yet available (e.g. during session priming).
	segments, err := parsePlaylistSegmentNames(dir)
	if err != nil || len(segments) == 0 {
		segments, err = listLiveSegments(dir)
		if err != nil || len(segments) == 0 {
			writeProblem(w, r, http.StatusNotFound, "live_preview/not_available", "Live Preview Not Available", "LIVE_PREVIEW_NOT_AVAILABLE", "no preview available", nil)
			return
		}
	}

	offset := parsePreviewOffset(r.URL.Query().Get("t"))
	segName, ok := pickPreviewSegment(segments, offset, segSeconds)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "live_preview/not_available", "Live Preview Not Available", "LIVE_PREVIEW_NOT_AVAILABLE", "no preview available", nil)
		return
	}

	// Include the segment file's modification time in the cache key so that a
	// session restart/recycle — which writes new segments with the same names
	// into a cleaned directory — does not serve stale previews from the
	// previous generation. The in-memory cache is not cleared across restarts.
	segPath := filepath.Join(dir, segName)
	var mtimeSuffix string
	if fi, stErr := os.Stat(segPath); stErr == nil { // #nosec G703 -- segName is from the validated segment list
		mtimeSuffix = strconv.FormatInt(fi.ModTime().UnixNano(), 36)
	}
	key := sessionID + "/" + segName + "/" + mtimeSuffix

	img, err := livePreviewCache.getOrBuild(key, func() ([]byte, error) {
		return extractKeyframeJPEG(r.Context(), ffmpegBin, dir, segName)
	})
	if err != nil {
		writeProblem(w, r, http.StatusNotFound, "live_preview/unavailable", "Live Preview Unavailable", "LIVE_PREVIEW_UNAVAILABLE", "preview unavailable", nil)
		return
	}

	w.Header().Set("Content-Type", "image/jpeg")
	// Short cache: the segment for a given offset is immutable once written, but
	// the rolling DVR window keeps moving, so do not let it linger.
	w.Header().Set("Cache-Control", "private, no-cache") // no-cache so the same ?t= offset served for different segments as the DVR window rolls is never stale
	w.Header().Set("Content-Length", strconv.Itoa(len(img)))
	_, _ = w.Write(img) // #nosec G705 -- binary JPEG, Content-Type image/jpeg
}

// pickPreviewSegment maps an offset (seconds from the start of the seekable
// window) to a segment in the sorted list. Segments are uniform segSeconds long
// and the oldest available segment is the window start, so the list index is
// floor(offset/segSeconds), clamped into range. Coarse by design — a storyboard
// tile only needs the right ~6s.
func pickPreviewSegment(segments []string, offsetSeconds float64, segSeconds int) (string, bool) {
	if len(segments) == 0 {
		return "", false
	}
	if segSeconds <= 0 {
		segSeconds = 6
	}
	if offsetSeconds < 0 || math.IsNaN(offsetSeconds) || math.IsInf(offsetSeconds, 0) {
		offsetSeconds = 0
	}
	idx := int(offsetSeconds / float64(segSeconds))
	if idx < 0 {
		idx = 0
	}
	if idx >= len(segments) {
		idx = len(segments) - 1
	}
	return segments[idx], true
}

// parsePreviewOffset parses the ?t= query (seconds from window start). Any
// malformed/negative/non-finite value collapses to 0 (oldest segment).
func parsePreviewOffset(raw string) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || v < 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return v
}

// listLiveSegments returns the session's media segments (seg_*.ts / seg_*.m4s),
// sorted. Zero-padded filenames sort lexically into numeric/playout order.
func listLiveSegments(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var segs []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if strings.HasPrefix(n, "seg_") && (strings.HasSuffix(n, ".ts") || strings.HasSuffix(n, ".m4s")) {
			segs = append(segs, n)
		}
	}
	sort.Strings(segs)
	return segs, nil
}

// parsePlaylistSegmentNames reads the live HLS playlist (index.m3u8) and
// returns the segment filenames currently referenced in it. Segments that have
// rolled out of the DVR window can linger on disk after FFmpeg's async cleanup;
// this function returns only the segments the player's seekable range actually
// covers, so hover previews are never aliased to expired content.
func parsePlaylistSegmentNames(dir string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(dir, "index.m3u8")) // #nosec G304,G703
	if err != nil {
		return nil, err
	}
	contentStr := string(data)
	if strings.Contains(contentStr, "#EXT-X-STREAM-INF:") {
		for _, l := range strings.Split(contentStr, "\n") {
			lc := strings.TrimSpace(l)
			if lc != "" && !strings.HasPrefix(lc, "#") && strings.HasSuffix(lc, ".m3u8") {
				if vData, err := os.ReadFile(filepath.Join(dir, lc)); err == nil { // #nosec G304,G703 -- lc is a parsed variant playlist name from a trusted local m3u8; err is checked in condition
					contentStr = string(vData)
					break
				}
			}
		}
	}
	lines := strings.Split(contentStr, "\n")
	var segs []string
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "#EXTINF:") {
			// The segment filename is the next non-empty, non-comment line.
			for j := i + 1; j < len(lines); j++ {
				next := strings.TrimSpace(lines[j])
				if next == "" || strings.HasPrefix(next, "#") {
					continue
				}
				segs = append(segs, next)
				i = j
				break
			}
		}
	}
	if len(segs) == 0 {
		return nil, fmt.Errorf("no segments found in playlist")
	}
	return segs, nil
}

// extractKeyframeJPEG decodes the first (key)frame of a single segment and
// encodes it as a small JPEG on stdout. fMP4 (.m4s) media segments are not
// standalone-decodable, so the init segment is prepended via the concat protocol
// (ftyp+moov || moof+mdat is a valid fMP4 byte stream).
func extractKeyframeJPEG(ctx context.Context, ffmpegBin, dir, segName string) ([]byte, error) {
	if strings.TrimSpace(ffmpegBin) == "" {
		ffmpegBin = "ffmpeg"
	}
	segPath := filepath.Join(dir, segName)
	input := segPath
	if strings.HasSuffix(segName, ".m4s") {
		input = "concat:" + filepath.Join(dir, "init.mp4") + "|" + segPath
	}

	cctx, cancel := context.WithTimeout(ctx, livePreviewBuildTimeout)
	defer cancel()

	args := []string{
		"-hide_banner",
		"-loglevel", "error",
		"-skip_frame", "nokey", // decode keyframes only -> near-free
		"-i", input,
		"-frames:v", "1",
		"-vf", fmt.Sprintf("scale='min(%d,iw)':-2", livePreviewMaxWidth),
		"-q:v", "5",
		"-an",
		"-f", "mjpeg",
		"pipe:1",
	}

	cmd := exec.CommandContext(cctx, ffmpegBin, args...) // #nosec G204,G702 -- ffmpeg binary is trusted config/default; args are built from a regex-validated session id and resolver-confined paths.
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg live preview: %w (%s)", err, strings.TrimSpace(errBuf.String()))
	}
	if out.Len() == 0 {
		return nil, errors.New("ffmpeg live preview: empty output")
	}
	return out.Bytes(), nil
}

// livePreviewCache is a small FIFO-bounded in-memory cache of rendered preview
// JPEGs, deduped with singleflight. It is intentionally not on disk: ffmpeg's
// delete_segments rolls the DVR window, and a disk cache would leak preview files
// the muxer never cleans up.
var livePreviewCache = newPreviewCache(livePreviewCacheEntries)

type previewCache struct {
	mu      sync.Mutex
	sf      singleflight.Group
	entries map[string][]byte
	order   []string
	cap     int
}

func newPreviewCache(capacity int) *previewCache {
	return &previewCache{entries: make(map[string][]byte), cap: capacity}
}

func (c *previewCache) get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, ok := c.entries[key]
	return b, ok
}

func (c *previewCache) put(key string, val []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.entries[key]; ok {
		return
	}
	if c.cap > 0 && len(c.order) >= c.cap {
		oldest := c.order[0]
		c.order = c.order[1:]
		delete(c.entries, oldest)
	}
	c.entries[key] = val
	c.order = append(c.order, key)
}

func (c *previewCache) getOrBuild(key string, build func() ([]byte, error)) ([]byte, error) {
	if b, ok := c.get(key); ok {
		return b, nil
	}
	v, err, _ := c.sf.Do(key, func() (any, error) {
		if b, ok := c.get(key); ok {
			return b, nil
		}
		b, err := build()
		if err != nil {
			return nil, err
		}
		c.put(key, b)
		return b, nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]byte), nil
}
