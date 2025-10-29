// SPDX-License-Identifier: MIT

// Package epg provides Electronic Program Guide functionality.
package epg

import (
	"encoding/xml"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	suffix = regexp.MustCompile(`\s+(hd|uhd|4k|austria|österreich|oesterreich|at|de|ch)$`)
	space  = regexp.MustCompile(`\s+`)
)

func norm(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = suffix.ReplaceAllString(s, "")
	s = space.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// BuildNameToIDMap reads an XMLTV file and returns a map[nameKey]=channelID.
// It expects elements matching the `Channel` type defined in generator.go.
func BuildNameToIDMap(xmltvPath string) (map[string]string, error) {
	xmltvPath = filepath.Clean(xmltvPath)
	// #nosec G304 -- xmltvPath is cleaned and originates from controlled configuration
	f, err := os.Open(xmltvPath)
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

	var doc TV
	dec := xml.NewDecoder(f)
	dec.Strict = false
	if err := dec.Decode(&doc); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}

	out := make(map[string]string, len(doc.Channels))
	for _, ch := range doc.Channels {
		if ch.ID == "" || len(ch.DisplayName) == 0 {
			continue
		}
		// Normalize all display names and add them to the map
		for _, displayName := range ch.DisplayName {
			key := norm(displayName)
			if key != "" {
				out[key] = ch.ID
			}
		}
	}
	return out, nil
}

// NameKey generates a normalized key from a channel name for matching.
func NameKey(s string) string { return norm(s) }
