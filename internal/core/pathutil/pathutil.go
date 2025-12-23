// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

package pathutil

import (
	"fmt"
	"path/filepath"
	"strings"
)

// SanitizeServiceRef converts a service reference to a safe directory name.
func SanitizeServiceRef(ref string) string {
	safe := strings.ReplaceAll(ref, ":", "_")
	safe = strings.ReplaceAll(safe, "/", "_")
	return safe
}

// SecureJoin safely joins a root directory with a user-provided path component.
// It prevents path traversal attacks by ensuring the result stays within root.
func SecureJoin(root, userPath string) (string, error) {
	cleaned := filepath.Clean(userPath)

	// Reject absolute paths
	if filepath.IsAbs(cleaned) {
		return "", fmt.Errorf("absolute paths are not allowed: %q", userPath)
	}

	// Reject paths starting with ..
	if strings.HasPrefix(cleaned, "..") {
		return "", fmt.Errorf("path traversal not allowed: %q", userPath)
	}

	full := filepath.Join(root, cleaned)

	// Ensure result is within root (defense in depth)
	rootClean := filepath.Clean(root) + string(filepath.Separator)
	fullClean := filepath.Clean(full) + string(filepath.Separator)
	if !strings.HasPrefix(fullClean, rootClean) {
		return "", fmt.Errorf("path escapes root directory: %q", userPath)
	}

	return full, nil
}
