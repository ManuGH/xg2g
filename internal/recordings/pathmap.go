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

// ExtractPathFromServiceRef extracts the filesystem path from an Enigma2 service reference.
// Enigma2 service references have the format: "1:0:0:0:0:0:0:0:0:0:/path/to/file.ts"
// Returns the path part (everything after the last colon) if it starts with "/",
// otherwise returns the original string unchanged (defensive).
func ExtractPathFromServiceRef(serviceRef string) string {
	parts := strings.Split(serviceRef, ":")
	if len(parts) > 0 {
		p := parts[len(parts)-1]
		if strings.HasPrefix(p, "/") {
			return p
		}
	}
	return serviceRef
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

// hasPathPrefix checks if p is within root, handling cross-platform separators and exact matches.
func hasPathPrefix(p, root string) bool {
	p = filepath.Clean(p)
	root = filepath.Clean(root)

	// Ensure root ends with a separator so /mnt/root2 doesn't match /mnt/root
	rootWithSep := root
	if !strings.HasSuffix(rootWithSep, string(filepath.Separator)) {
		rootWithSep += string(filepath.Separator)
	}

	// Also allow exact match to root
	return p == root || strings.HasPrefix(p, rootWithSep)
}

// ResolveLocalExisting maps a receiver path to a local path and ENSURES it exists and is confined.
// It resolves all symlinks and strictly enforces that the target remains within the mapped local root.
// Use this for playback and file access.
func (pm *PathMapper) ResolveLocalExisting(receiverPath string) (string, bool) {
	// 1. POSIX clean and validate
	clean := path.Clean(receiverPath)
	if !strings.HasPrefix(clean, "/") || clean == "/" || strings.Contains(clean, "..") {
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
		if clean == root {
			rel = ""
		} else if strings.HasPrefix(clean, root+"/") {
			rel = strings.TrimPrefix(clean, root+"/")
		} else {
			continue
		}

		if len(root) > longestLen {
			longestLen = len(root)
			bestMatch = m
			bestRel = rel
		}
	}

	if bestMatch == nil {
		return "", false
	}

	// 3. Build local path (pre-resolution)
	var localPath string
	if bestRel == "" {
		localPath = bestMatch.LocalRoot
	} else {
		localPath = filepath.Join(bestMatch.LocalRoot, filepath.FromSlash(bestRel))
	}

	// 4. Resolve symlinks for root and target
	// Root should exist; if it doesn't, config is broken.
	rootResolved, err := filepath.EvalSymlinks(bestMatch.LocalRoot)
	if err != nil {
		return "", false
	}
	rootResolved = filepath.Clean(rootResolved)

	// Target must exist for playback; if it doesn't, treat as unmapped/not found.
	targetResolved, err := filepath.EvalSymlinks(localPath)
	if err != nil {
		return "", false
	}
	targetResolved = filepath.Clean(targetResolved)

	// 5. Enforce boundary
	if !hasPathPrefix(targetResolved, rootResolved) {
		return "", false
	}

	return targetResolved, true
}

// ResolveLocalUnsafe attempts to map receiver path to local filesystem path WITHOUT filesystem checks.
// DEPRECATED: Use ResolveLocalExisting instead for all file access.
// This method is only for mapping logic tests or rare non-access cases.
//
// Security:
// - Blocks path traversal (e.g., "../")
// - Requires absolute paths
// - Uses longest-prefix matching to avoid collisions
func (pm *PathMapper) ResolveLocalUnsafe(receiverPath string) (string, bool) {
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
