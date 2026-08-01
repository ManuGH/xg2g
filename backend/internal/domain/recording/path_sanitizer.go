// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package recording

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var (
	ErrPathOutsideBackendRoot = errors.New("relative path resolves outside backend root boundary")
	ErrIllegalPathCharacter   = errors.New("relative path contains illegal characters")
	ErrAbsolutePathNotAllowed = errors.New("absolute or volume path not allowed for relative target")
)

// Illegal characters for POSIX/Windows filenames: \ : * ? " < > |
var illegalChars = []string{"\\", ":", "*", "?", "\"", "<", ">", "|"}

// SanitizeAndValidateRelativePath performs a component-based bounds check ensuring relPath stays within backendRoot.
func SanitizeAndValidateRelativePath(backendRoot string, relPath string) (string, error) {
	if backendRoot == "" {
		return "", fmt.Errorf("backendRoot cannot be empty")
	}

	// 1. Absolute or Volume path check
	if filepath.IsAbs(relPath) || strings.HasPrefix(relPath, "/") || (len(relPath) >= 2 && relPath[1] == ':') {
		return "", fmt.Errorf("%w: '%s'", ErrAbsolutePathNotAllowed, relPath)
	}

	// 2. Component illegal character & real NUL byte check
	cleanRel := filepath.Clean(relPath)
	parts := strings.Split(cleanRel, "/")
	for _, part := range parts {
		if part == "." || part == ".." {
			return "", fmt.Errorf("%w: contains '%s'", ErrPathOutsideBackendRoot, part)
		}
		if strings.ContainsRune(part, '\x00') {
			return "", fmt.Errorf("%w: contains real NUL byte", ErrIllegalPathCharacter)
		}
		for _, ch := range illegalChars {
			if strings.Contains(part, ch) {
				return "", fmt.Errorf("%w: '%s' in '%s'", ErrIllegalPathCharacter, ch, part)
			}
		}
	}

	// 3. Resolve canonical backend root with strict error handling
	realBackendRoot, err := filepath.EvalSymlinks(backendRoot)
	if err != nil {
		if os.IsNotExist(err) {
			realBackendRoot = filepath.Clean(backendRoot)
		} else {
			return "", fmt.Errorf("failed to resolve backend root symlinks: %w", err)
		}
	}

	// 4. Construct candidate target path
	fullTargetPath := filepath.Join(realBackendRoot, cleanRel)

	// 5. Bounds check: ensure target path is prefixed by realBackendRoot
	rel, err := filepath.Rel(realBackendRoot, fullTargetPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: '%s'", ErrPathOutsideBackendRoot, relPath)
	}

	// 6. Resolve longest existing parent directory for symlink escape pre-check
	parentDir := filepath.Dir(fullTargetPath)
	for {
		if info, err := filepath.EvalSymlinks(parentDir); err == nil {
			parentRel, err := filepath.Rel(realBackendRoot, info)
			if err != nil || parentRel == ".." || strings.HasPrefix(parentRel, ".."+string(filepath.Separator)) {
				return "", fmt.Errorf("%w: parent symlink resolves outside backend root", ErrPathOutsideBackendRoot)
			}
			break
		}
		nextParent := filepath.Dir(parentDir)
		if nextParent == parentDir {
			break
		}
		parentDir = nextParent
	}

	return cleanRel, nil
}
