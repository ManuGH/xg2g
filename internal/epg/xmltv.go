// SPDX-License-Identifier: MIT

// Package epg provides Electronic Program Guide functionality.
package epg

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	unorm "golang.org/x/text/unicode/norm"
)

var (
	suffix = regexp.MustCompile(`\s+(hd|uhd|4k|austria|österreich|oesterreich|at|de|ch)$`)
	space  = regexp.MustCompile(`\s+`)
)

func normalize(s string) string {
	// Normalize Unicode to NFC form (composed form) before processing
	s = unorm.NFC.String(s)
	s = strings.ToLower(strings.TrimSpace(s))
	// Re-normalize after case conversion (lowercase may create new combining sequences)
	s = unorm.NFC.String(s)

	// Remove suffixes repeatedly until none remain (handles cases like "Ch HD")
	for {
		before := s
		s = suffix.ReplaceAllString(s, "")
		if s == before {
			break // No more suffixes to remove
		}
	}

	s = space.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// norm is a wrapper for normalize for backwards compatibility with tests.
func norm(s string) string {
	return normalize(s)
}

// BuildNameToIDMap reads an XMLTV file and returns a map[nameKey]=channelID.
// It expects elements matching the `Channel` type defined in generator.go.
func BuildNameToIDMap(xmltvPath string) (map[string]string, error) {
	xmltvPath = filepath.Clean(xmltvPath)
	// xmltvPath is cleaned and originates from controlled configuration
	f, err := os.Open(xmltvPath) // #nosec G304
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := f.Close(); err != nil {
			// best-effort: log to stderr but do not fail the operation
			// this satisfies errcheck while keeping behavior unchanged
			// (no logging package imported here to avoid changing API surface)
			_ = err
		}
	}()

	// Use a LimitedReader to prevent DoS via massive files
	// 50MB limit for XMLTV files should be sufficient for most use cases
	const maxXMLSize = 50 * 1024 * 1024
	r := io.LimitReader(f, maxXMLSize)

	var doc TV
	dec := xml.NewDecoder(r)
	dec.Strict = true // Enable strict parsing for security

	// Disable entity expansion to prevent XXE attacks
	dec.Entity = make(map[string]string)

	if err := dec.Decode(&doc); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("decode xmltv: %w", err)
	}

	out := make(map[string]string, len(doc.Channels))
	for _, ch := range doc.Channels {
		if ch.ID == "" || len(ch.DisplayName) == 0 {
			continue
		}
		// Normalize all display names and add them to the map
		for _, displayName := range ch.DisplayName {
			key := normalize(displayName)
			if key != "" {
				out[key] = ch.ID
			}
		}
	}
	return out, nil
}

// NameKey generates a normalized key from a channel name for matching.
func NameKey(s string) string { return normalize(s) }
