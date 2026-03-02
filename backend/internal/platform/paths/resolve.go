package paths

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ResolveDataFilePath resolves a relative path inside the given data directory while
// protecting against path traversal and symlink escapes.
// If allowMissing is true, the file does not need to exist, but its parent directory must be safe.
func ResolveDataFilePath(dataDir, relPath string, allowMissing bool) (string, error) {
	clean := filepath.Clean(relPath)
	if filepath.IsAbs(clean) {
		return "", fmt.Errorf("data file path must be relative: %s", relPath)
	}
	if strings.Contains(clean, "..") {
		return "", fmt.Errorf("data file path contains traversal: %s", relPath)
	}

	root, err := filepath.Abs(dataDir)
	if err != nil {
		return "", fmt.Errorf("resolve data directory: %w", err)
	}

	full := filepath.Join(root, clean)

	// Resolve root symlinks to establish true base
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		// If dataDir invalid, we can't secure it.
		// However, it might be that dataDir hasn't been created yet?
		// Usually dataDir should exist at startup.
		resolvedRoot = root
	}

	resolved := full
	info, statErr := os.Stat(full)
	if statErr == nil {
		if info.IsDir() {
			return "", fmt.Errorf("data file path points to directory: %s", relPath)
		}
		if resolvedPath, evalErr := filepath.EvalSymlinks(full); evalErr == nil {
			resolved = resolvedPath
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return "", fmt.Errorf("stat data file: %w", statErr)
	} else {
		// File does not exist
		if !allowMissing {
			return "", fmt.Errorf("data file not found: %s", relPath)
		}
		// Ensure parent exists and is safe
		dir := filepath.Dir(full)
		if realDir, evalErr := filepath.EvalSymlinks(dir); evalErr == nil {
			resolved = filepath.Join(realDir, filepath.Base(full))
		}
	}

	relToRoot, err := filepath.Rel(resolvedRoot, resolved)
	if err != nil {
		return "", fmt.Errorf("resolve relative path: %w", err)
	}
	// Check traversal again after symlink resolution
	if strings.HasPrefix(relToRoot, "..") || filepath.IsAbs(relToRoot) {
		return "", fmt.Errorf("data file escapes data directory: %s", relPath)
	}

	return resolved, nil
}
