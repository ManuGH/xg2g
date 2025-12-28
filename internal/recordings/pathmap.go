// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package recordings

import (
	"path"
	"path/filepath"
	"strings"

	"github.com/ManuGH/xg2g/internal/config"
)

// PathMapper resolves receiver recording paths to local filesystem paths
type PathMapper struct {
	mappings []config.RecordingPathMapping
}

// NewPathMapper creates a new PathMapper with configured mappings
func NewPathMapper(mappings []config.RecordingPathMapping) *PathMapper {
	// Validate and normalize mappings
	var valid []config.RecordingPathMapping
	for _, m := range mappings {
		// Both roots must be absolute
		if !filepath.IsAbs(m.ReceiverRoot) || !filepath.IsAbs(m.LocalRoot) {
			continue
		}
		// Normalize: remove trailing slashes for consistent matching
		receiverRoot := strings.TrimSuffix(m.ReceiverRoot, "/")
		localRoot := strings.TrimSuffix(m.LocalRoot, "/")

		// Skip root-only or invalid mappings
		if receiverRoot == "" || receiverRoot == "/" || localRoot == "" || localRoot == "/" {
			continue
		}

		valid = append(valid, config.RecordingPathMapping{
			ReceiverRoot: receiverRoot,
			LocalRoot:    localRoot,
		})
	}

	return &PathMapper{mappings: valid}
}

// ResolveLocal attempts to map receiver path to local filesystem path.
// Returns (localPath, true) if mapping exists.
// Returns ("", false) if no mapping found or path is invalid.
//
// Security:
// - Blocks path traversal (e.g., "../")
// - Requires absolute paths
// - Uses longest-prefix matching to avoid collisions
func (pm *PathMapper) ResolveLocal(receiverPath string) (string, bool) {
	// 1. POSIX clean and validate
	clean := path.Clean(receiverPath)

	// Must be absolute
	if !strings.HasPrefix(clean, "/") {
		return "", false
	}

	// Block root-only
	if clean == "/" {
		return "", false
	}

	// Block traversal (path.Clean should handle this, but explicit check)
	if strings.Contains(clean, "..") {
		return "", false
	}

	// 2. Find matching mapping (longest prefix wins)
	var bestMatch *config.RecordingPathMapping
	var bestRel string
	longestLen := 0

	for i := range pm.mappings {
		m := &pm.mappings[i]
		root := m.ReceiverRoot

		var rel string

		// Check for exact match
		if clean == root {
			rel = ""
		} else if strings.HasPrefix(clean, root+"/") {
			// Prefix match - extract relative path
			rel = strings.TrimPrefix(clean, root+"/")
		} else {
			// No match
			continue
		}

		// Longest prefix wins (to handle /media/hdd/movie vs /media/hdd/movie2)
		if len(root) > longestLen {
			longestLen = len(root)
			bestMatch = m
			bestRel = rel
		}
	}

	// No mapping found
	if bestMatch == nil {
		return "", false
	}

	// 3. Build local path
	var localPath string
	if bestRel == "" {
		// Exact root match
		localPath = bestMatch.LocalRoot
	} else {
		// Join with relative path using OS-specific separator
		localPath = filepath.Join(bestMatch.LocalRoot, filepath.FromSlash(bestRel))
	}

	return localPath, true
}
