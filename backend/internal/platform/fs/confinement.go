// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package fs

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
	realPath, err := resolveWithExistingPrefix(fullPath)
	if err != nil {
		return "", err
	}

	return confineLexicalPath(realRoot, realPath)
}

func resolveWithExistingPrefix(path string) (string, error) {
	cleanPath := filepath.Clean(path)

	probe := cleanPath
	missingParts := make([]string, 0, 4)
	for {
		_, err := os.Lstat(probe)
		if err == nil {
			resolved, err := filepath.EvalSymlinks(probe)
			if err != nil {
				return "", fmt.Errorf("failed to resolve path: %w", err)
			}
			for i := len(missingParts) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missingParts[i])
			}
			return resolved, nil
		}
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("failed to resolve path: %w", err)
		}

		parent := filepath.Dir(probe)
		if parent == probe {
			return cleanPath, nil
		}

		missingParts = append(missingParts, filepath.Base(probe))
		probe = parent
	}
}

func confineLexicalPath(realRoot, candidate string) (string, error) {
	cleanRoot := filepath.Clean(realRoot)
	cleanCandidate := filepath.Clean(candidate)

	rel, err := filepath.Rel(cleanRoot, cleanCandidate)
	if err != nil {
		return "", fmt.Errorf("rel computation failed: %w", err)
	}

	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes root: %s", cleanCandidate)
	}

	return cleanCandidate, nil
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
