// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// SPDX-License-Identifier: MIT

package epg

import (
	"regexp"
	"strings"
)

// Metadata holds extracted EPG metadata
type Metadata struct {
	Year      string
	Country   string
	Directors []string
	Actors    []string
	Genre     string
}

var (
	// Regex patterns for common EPG formats

	// "USA 2023", "DE 1999", "France 2000"
	// Look for "Country YYYY" pattern
	yearCountryRegex = regexp.MustCompile(`\b([A-Z][a-zA-Z\.]+)\s+((?:19|20)\d{2})\b`)

	// "Regie: Name Name", "Director: Name Name"
	directorRegex = regexp.MustCompile(`(?i)(?:Regie|Director|Directed by)[:\s]+([^,.\n]+)`)

	// "Darsteller: Name, Name", "Cast: Name, Name", "Mit: Name"
	castRegex = regexp.MustCompile(`(?i)(?:Darsteller|Cast|Actors|Mit)[:\s]+([^.\n]+)`)
)

// ParseDescription extracts metadata from the description text
func ParseDescription(desc string) Metadata {
	meta := Metadata{}

	// Extract Year and Country
	if match := yearCountryRegex.FindStringSubmatch(desc); len(match) == 3 {
		meta.Country = strings.TrimSpace(match[1])
		meta.Year = match[2]
	}

	// Extract Director
	if match := directorRegex.FindStringSubmatch(desc); len(match) > 1 {
		// Split by comma if multiple
		dirs := strings.Split(match[1], ",")
		for _, d := range dirs {
			if trimmed := strings.TrimSpace(d); trimmed != "" {
				meta.Directors = append(meta.Directors, trimmed)
			}
		}
	}

	// Extract Cast
	if match := castRegex.FindStringSubmatch(desc); len(match) > 1 {
		actors := strings.Split(match[1], ",")
		for _, a := range actors {
			// Clean up actor names (remove role names in brackets if present)
			// e.g. "Tom Hanks (Forrest)" -> "Tom Hanks"
			name := strings.Split(a, "(")[0]
			if trimmed := strings.TrimSpace(name); trimmed != "" {
				meta.Actors = append(meta.Actors, trimmed)
			}
		}
	}

	return meta
}
