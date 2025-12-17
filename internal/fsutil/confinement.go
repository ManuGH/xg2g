package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ConfineRelPath ensures that joining root and relTarget results in a path that is physically
// underneath the resolved path of root. It protects against symlink traversal and backslash bypass.
// The target MUST be relative.
func ConfineRelPath(root, relTarget string) (string, error) {
	// Block backslashes to prevent OS-specific bypasses on non-Windows systems
	// or ambiguity in generic parsing.
	if strings.Contains(relTarget, "\\") {
		return "", fmt.Errorf("path contains backslash: %s", relTarget)
	}

	// Clean the relative target
	cleanRel := filepath.Clean(relTarget)
	if filepath.IsAbs(cleanRel) || strings.HasPrefix(cleanRel, "/") {
		return "", fmt.Errorf("target path must be relative: %s", relTarget)
	}

	// Traversal Check: Segment-based to allow ".." in filenames
	// cleanRel handles "a/../b" -> "b", but if it starts with "..", it's outside.
	if cleanRel == ".." || strings.HasPrefix(cleanRel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path traversal attempt: %s", relTarget)
	}

	// Resolve the root
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("invalid root path: %w", err)
	}

	realRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return "", err
		}
		realRoot = absRoot
	}

	// Construct full potential path
	fullPath := filepath.Join(realRoot, cleanRel)

	return resolveAndCheck(realRoot, fullPath)
}

// ConfineAbsPath ensures that targetAbs is physically underneath the resolved path of root.
// The target must be absolute.
func ConfineAbsPath(rootAbs, targetAbs string) (string, error) {
	if strings.Contains(targetAbs, "\\") {
		return "", fmt.Errorf("path contains backslash: %s", targetAbs)
	}

	// Ensure input is roughly absolute before processing
	if !filepath.IsAbs(targetAbs) {
		return "", fmt.Errorf("target path must be absolute: %s", targetAbs)
	}

	// Canonicalize input path
	targetAbs = filepath.Clean(targetAbs)

	absRoot, err := filepath.Abs(rootAbs)
	if err != nil {
		return "", fmt.Errorf("invalid root path: %w", err)
	}

	realRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return "", err
		}
		realRoot = absRoot
	}

	// We do NOT Join. We take the targetAbs as the full path candidate.
	// But we must resolve it.

	return resolveAndCheck(realRoot, targetAbs)
}

// resolveAndCheck resolves realPath symlinks and ensures it is within realRoot.
func resolveAndCheck(realRoot, fullPath string) (string, error) {
	var realPath string
	if info, err := os.Lstat(fullPath); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			if rp, err := filepath.EvalSymlinks(fullPath); err == nil {
				realPath = rp
			} else {
				// If resolving fails for an existing symlink, we should be conservative
				return "", fmt.Errorf("failed to resolve symlink: %w", err)
			}
		} else {
			if rp, err := filepath.EvalSymlinks(fullPath); err == nil {
				realPath = rp
			} else {
				// If resolving fails for an existing regular file, deny access to be safe
				return "", fmt.Errorf("failed to resolve path: %w", err)
			}
		}
	} else {
		// File does not exist? Check parent.
		dir := filepath.Dir(fullPath)
		if rp, err := filepath.EvalSymlinks(dir); err == nil {
			realPath = filepath.Join(rp, filepath.Base(fullPath))
		} else {
			// Parent exists?
			if _, statErr := os.Stat(dir); statErr == nil {
				// Parent exists but EvalSymlinks failed (permissions/loop?) -> Fail Closed
				return "", fmt.Errorf("failed to resolve parent path: %v", err)
			}
			// Parent doesn't exist either?
			// Conservative: use fullPath and rely on Rel check.
			realPath = fullPath
		}
	}

	// Finally, verify realPath starts with realRoot
	rel, err := filepath.Rel(realRoot, realPath)
	if err != nil {
		return "", fmt.Errorf("rel computation failed: %w", err)
	}

	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes root via symlinks: %s", realPath)
	}

	return realPath, nil
}

// IsRegularFile checks if path exists and is a regular file (not directory, device, etc).
// Returns error if not.
func IsRegularFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("not a regular file: %s", path)
	}
	return nil
}
