// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package matcher

import (
	"regexp"
	"strings"

	"github.com/ManuGH/xg2g/internal/epg"
)

var (
	// Noise patterns commonly found in broadcast EPG titles
	noiseRegexes = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(?:HD|UHD|4K|2K|SD)\b`),
		regexp.MustCompile(`(?i)\b(?:Live|Direkt|Wdh|Wiederholung|Erstausstrahlung|Neu)\b`),
		regexp.MustCompile(`[\[\]\(\)\{\}\<\>\.]`),
		regexp.MustCompile(`\s+`),
	}
)

// NormalizeTitle strips broadcast noise, resolution markers, and bracketed tags from programme titles.
func NormalizeTitle(title string) string {
	cleaned := title
	for _, re := range noiseRegexes {
		cleaned = re.ReplaceAllString(cleaned, " ")
	}
	cleaned = strings.TrimSpace(cleaned)
	cleaned = strings.ToLower(cleaned)
	// Replace consecutive spaces with a single space
	cleaned = strings.Join(strings.Fields(cleaned), " ")
	return cleaned
}

// BuildFingerprint constructs a deterministic ProgrammeFingerprint from title and canonical metadata.
func BuildFingerprint(title string, year int, episode *epg.EpisodeInfo, genre string) epg.ProgrammeFingerprint {
	norm := NormalizeTitle(title)
	fp := epg.ProgrammeFingerprint{
		NormalizedTitle:    norm,
		Year:               year,
		EventGenre:         genre,
		FingerprintVersion: epg.CurrentFingerprintVersion,
	}

	if episode != nil {
		fp.Season = episode.SeasonNumber
		fp.Episode = episode.EpisodeNumber
	}

	return fp
}
