// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ffmpeg

import (
	"fmt"
	"path/filepath"
)

// SessionOutputDir returns the root directory for a specific session.
// e.g., {root}/sessions/{sessionID}
func SessionOutputDir(root, sessionID string) string {
	return filepath.Join(root, "sessions", sessionID)
}

// PlaylistPaths returns the temporary and final paths for the master playlist.
// tmp: index.m3u8.tmp
// final: index.m3u8
func PlaylistPaths(dir string) (tmp, final string) {
	return filepath.Join(dir, "index.m3u8.tmp"), filepath.Join(dir, "index.m3u8")
}

// InitPath returns the path for the fMP4 initialization segment (map).
func InitPath(dir string) string {
	return filepath.Join(dir, "init.mp4")
}

// SegmentPattern returns the filename pattern for segments.
// e.g., seg_%06d.ts (or .m4s in future)
// SegmentPattern returns the filename pattern for segments.
// e.g., seg_%06d.ts (or .m4s in future)
func SegmentPattern(dir, ext string) string {
	if ext == "" {
		ext = ".ts"
	}
	return filepath.Join(dir, fmt.Sprintf("seg_%%06d%s", ext))
}
