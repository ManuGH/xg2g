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
	return RewritePlaylist(content, playlistType, "")
}

// RewritePlaylist applies playlist-type normalization and appends the variant query to media URIs.
func RewritePlaylist(content, playlistType, variant string) string {
	lines := strings.Split(content, "\n")
	newLines := make([]string, 0, len(lines)+1)
	inserted := false
	shouldInsertType := playlistType != ""
	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		if shouldInsertType && strings.HasPrefix(line, "#EXT-X-PLAYLIST-TYPE:") {
			continue
		}
		if variant != "" && strings.HasPrefix(line, "#EXT-X-MAP:") {
			line = appendVariantQueryToMapDirective(line, variant)
		}
		if variant != "" && line != "" && !strings.HasPrefix(line, "#") {
			line = appendVariantQuery(line, variant)
		}
		newLines = append(newLines, line)
		if shouldInsertType && line == "#EXTM3U" && !inserted {
			newLines = append(newLines, "#EXT-X-PLAYLIST-TYPE:"+playlistType)
			inserted = true
		}
	}
	if shouldInsertType && !inserted {
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

func appendVariantQuery(uri, variant string) string {
	if variant == "" || strings.Contains(uri, "variant=") {
		return uri
	}
	if strings.Contains(uri, "?") {
		return uri + "&variant=" + variant
	}
	return uri + "?variant=" + variant
}

func appendVariantQueryToMapDirective(line, variant string) string {
	const marker = `URI="`
	if variant == "" || strings.Contains(line, "variant=") {
		return line
	}
	start := strings.Index(line, marker)
	if start < 0 {
		return line
	}
	start += len(marker)
	end := strings.Index(line[start:], `"`)
	if end < 0 {
		return line
	}
	end += start
	return line[:start] + appendVariantQuery(line[start:end], variant) + line[end:]
}
