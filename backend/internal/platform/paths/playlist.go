// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

package paths

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ManuGH/xg2g/internal/platform/fs"
)

var allowedPlaylistExt = map[string]struct{}{
	".m3u":  {},
	".m3u8": {},
}

// ValidatePlaylistPath validates a playlist filename and returns a safe absolute path under baseDir.
// It rejects absolute paths, traversal attempts, symlink escapes, and disallowed extensions.
func ValidatePlaylistPath(baseDir, userValue string) (string, error) {
	baseDir = strings.TrimSpace(baseDir)
	if baseDir == "" {
		return "", fmt.Errorf("playlist base directory is empty")
	}

	raw := strings.TrimSpace(userValue)
	if raw == "" {
		return "", fmt.Errorf("playlist path is empty")
	}

	clean := filepath.Clean(raw)
	if clean == "." || clean == string(filepath.Separator) {
		return "", fmt.Errorf("playlist path is empty")
	}
	if filepath.IsAbs(clean) {
		return "", fmt.Errorf("playlist path must be relative: %s", userValue)
	}

	ext := strings.ToLower(filepath.Ext(clean))
	if _, ok := allowedPlaylistExt[ext]; !ok {
		return "", fmt.Errorf("playlist path must end with .m3u or .m3u8: %s", userValue)
	}

	if base := filepath.Base(clean); base == "." || base == string(filepath.Separator) {
		return "", fmt.Errorf("playlist path has no filename: %s", userValue)
	}

	path, err := fs.ConfineRelPath(baseDir, clean)
	if err != nil {
		return "", fmt.Errorf("playlist path rejected: %w", err)
	}

	return path, nil
}
