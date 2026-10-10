package read

import (
	"net/url"
	"strings"
)

// CanonicalServiceRef normalizes service references to one stable form.
func CanonicalServiceRef(ref string) string {
	ref = strings.TrimSpace(ref)
	ref = strings.TrimRight(ref, ":")
	if isHexColonServiceRef(ref) {
		return strings.ToUpper(ref)
	}
	return ref
}

func isHexColonServiceRef(ref string) bool {
	if ref == "" || !strings.Contains(ref, ":") {
		return false
	}
	for _, ch := range ref {
		switch {
		case ch == ':':
		case ch >= '0' && ch <= '9':
		case ch >= 'a' && ch <= 'f':
		case ch >= 'A' && ch <= 'F':
		default:
			return false
		}
	}
	return true
}

func normalizeExtractedIPTVRef(ref string) string {
	ref = strings.TrimSpace(ref)
	lower := strings.ToLower(ref)
	if strings.Contains(lower, "%25") || (strings.Contains(ref, "%") && !strings.Contains(lower, "%3a")) {
		if unescaped, err := url.PathUnescape(ref); err == nil && unescaped != "" {
			return unescaped
		}
	}
	return ref
}

func extractIPTVFromURL(rawURL string) (string, bool) {
	rawURL = strings.TrimSpace(rawURL)
	prefixes := []string{"/4097:", "/5001:", "/5002:", "/iptv_", "/IPTV_"}
	for _, pfx := range prefixes {
		if idx := strings.Index(rawURL, pfx); idx != -1 {
			return normalizeExtractedIPTVRef(rawURL[idx+1:]), true
		}
	}
	rawPrefixes := []string{"4097:", "5001:", "5002:", "iptv_", "IPTV_"}
	for _, pfx := range rawPrefixes {
		if strings.HasPrefix(rawURL, pfx) {
			return normalizeExtractedIPTVRef(rawURL), true
		}
	}
	return "", false
}

// ExtractServiceRef extracts a stable service reference from a stream URL.
// Contract:
// 1. If parseable URL:
//   - If "ref" query param exists -> return it.
//   - Else -> check if path embeds an IPTV service reference.
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
			} else if iptvRef, ok := extractIPTVFromURL(rawURL); ok {
				// IPTV service references embed nested URLs with slashes (e.g. /4097:0:...:http%3a//host/path/stream.ts),
				// which must NOT be split by slash.
				candidate = iptvRef
			} else {
				// Fallback: Last path segment
				parts := strings.Split(u.Path, "/")
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
		if iptvRef, ok := extractIPTVFromURL(rawURL); ok {
			candidate = iptvRef
		} else {
			// 2. Not parseable: split raw string by /
			parts := strings.Split(rawURL, "/")
			if len(parts) > 0 {
				candidate = parts[len(parts)-1]
			}
		}
	}

	// 3. If empty, use fallback
	if CanonicalServiceRef(candidate) == "" {
		candidate = fallback
	}

	// 4. Canonicalize to prevent ref drift across consumers.
	return CanonicalServiceRef(candidate)
}
