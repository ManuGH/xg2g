package recordings

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/ManuGH/xg2g/internal/platform/fs"
)

// RecordingLivePlaylistReady checks if a valid progressive playlist exists.
// Criteria: index.live.m3u8 exists AND references at least one existing segment file.
func RecordingLivePlaylistReady(cacheDir string) (string, bool) {
	livePath := filepath.Join(cacheDir, "index.live.m3u8")

	// 1. Check Playlist Existence
	info, err := os.Stat(livePath)
	if err != nil || info.IsDir() {
		return "", false
	}

	// 2. Parse Playlist for valid segment reference
	// #nosec G304
	data, err := os.ReadFile(filepath.Clean(livePath))
	if err != nil {
		return "", false
	}

	content := string(data)
	lines := strings.Split(content, "\n")

	// The playlist may reference TS-HLS segments or CMAF/fMP4 segments.
	// We only consider URI lines (non-#), which correspond to media segment filenames.
	hasSegment := false
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}

		// Found a URI line (segment)
		// Security: confine segment path to cache dir
		// Validate segment name BEFORE path confinement/resolution to prevent bypass
		if !IsAllowedVideoSegment(l) {
			continue
		}

		safeSeg, err := fs.ConfineRelPath(cacheDir, l)
		if err != nil {
			continue
		}
		// Double check file extension on resolved path (Canonical check)
		if !IsAllowedVideoSegment(safeSeg) {
			continue
		}

		if _, err := os.Stat(safeSeg); err == nil {
			hasSegment = true
			break
		}
	}

	if hasSegment {
		return livePath, true
	}
	return "", false
}

func RewritePlaylistType(content, playlistType string) string {
	if playlistType == "" {
		return content
	}
	lines := strings.Split(content, "\n")
	newLines := make([]string, 0, len(lines)+1)
	inserted := false
	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, "#EXT-X-PLAYLIST-TYPE:") {
			continue
		}
		newLines = append(newLines, line)
		if line == "#EXTM3U" && !inserted {
			newLines = append(newLines, "#EXT-X-PLAYLIST-TYPE:"+playlistType)
			inserted = true
		}
	}
	if !inserted {
		newLines = append([]string{"#EXT-X-PLAYLIST-TYPE:" + playlistType}, newLines...)
	}

	// Fix: VOD Playlists MUST have EXT-X-ENDLIST to be treated as finite/seekable by Safari
	if playlistType == "VOD" {
		hasEndlist := false
		for _, line := range lines {
			if strings.TrimSpace(line) == "#EXT-X-ENDLIST" {
				hasEndlist = true
				break
			}
		}
		if !hasEndlist {
			newLines = append(newLines, "#EXT-X-ENDLIST")
		}
	}

	return strings.Join(newLines, "\n")
}
