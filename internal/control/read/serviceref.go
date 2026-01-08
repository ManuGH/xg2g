package read

import (
	"net/url"
	"strings"
)

// ExtractServiceRef extracts a stable service reference from a stream URL.
// Contract:
// 1. If parseable URL:
//   - If "ref" query param exists -> return it.
//   - Else -> return last path segment.
//
// 2. If not parseable -> split by "/" and return last segment.
// 3. If result is empty -> return fallback.
// 4. Always trim trailing ":" (Enigma2 drift).
func ExtractServiceRef(rawURL string, fallback string) string {
	var candidate string

	// 1. Try parsing as URL (Heuristic: Must have "://" to be treated as URL)
	// This avoids "1:0:1..." being parsed as scheme "1" with opaque content.
	isURL := strings.Contains(rawURL, "://")
	if isURL {
		u, err := url.Parse(rawURL)
		if err == nil {
			// Priority: Query Param "ref"
			if qRef := u.Query().Get("ref"); qRef != "" {
				candidate = qRef
			} else {
				// Fallback: Last path segment
				path := u.Path
				parts := strings.Split(path, "/")
				if len(parts) > 0 {
					candidate = parts[len(parts)-1]
				}
			}
		} else {
			// Fallback if parse fails but has :// (weird edge case)
			isURL = false
		}
	}

	if !isURL {
		// 2. Not parseable: split raw string by /
		parts := strings.Split(rawURL, "/")
		if len(parts) > 0 {
			candidate = parts[len(parts)-1]
		}
	}

	// Sanitize candidate (trim spaces just in case)
	candidate = strings.TrimSpace(candidate)

	// 3. If empty, use fallback
	if candidate == "" {
		candidate = fallback
	}

	// 4. Always trim trailing ":"
	// Repeatedly? contract just says "trim trailing :". Usually one suffix.
	// But Enigma2 refs often look like "1:0:1...:"
	return strings.TrimSuffix(candidate, ":")
}
