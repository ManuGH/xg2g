// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package matcher

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/ManuGH/xg2g/internal/epg"
)

var (
	// Fractional parts/episodes in parentheses: (1/2), (2/4), (101/116)
	fractionalEpisodeParenRegex = regexp.MustCompile(`\(\s*\d+\s*/\s*\d+\s*\)`)
	// Single integer in parentheses: (98), (533), (1144), (2009)
	integerParenRegex = regexp.MustCompile(`\(\s*(\d+)\s*\)`)
	// Plausible 4-digit production year in parentheses: (1900) - (2099)
	yearInParenRegex = regexp.MustCompile(`\(\s*(19\d\d|20\d\d)\s*\)`)

	// Noise patterns commonly found in broadcast EPG titles
	noiseRegexes = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(?:HD|UHD|4K|2K|SD)\b`),
		regexp.MustCompile(`(?i)\b(?:Live|Direkt|Wdh|Wiederholung|Erstausstrahlung|Neu)\b`),
		regexp.MustCompile(`(?i)\b(?:S\d{1,2}E\d{1,2}|\d{1,2}x\d{1,2}|Staffel\s+\d+|Folge\s+\d+|Episode\s+\d+)\b`),
		regexp.MustCompile(`[\[\]\(\)\{\}\<\>\.]`),
		regexp.MustCompile(`\s+`),
	}
)

// NormalizeTitle strips broadcast noise, resolution markers, bracketed tags,
// and parenthesized episode counters (e.g. (98), (1/2), (1144)) from programme titles,
// while strictly preserving plausible 4-digit production years (1900-2099, e.g. (2009))
// from being stripped as episode counters.
func NormalizeTitle(title string) string {
	cleaned := title

	// 1. Remove fractional episode counters in parentheses: (1/2), (101/116)
	cleaned = fractionalEpisodeParenRegex.ReplaceAllString(cleaned, " ")

	// 2. Remove integer episode numbers in parentheses ONLY if NOT a plausible production year (1900-2099)
	cleaned = integerParenRegex.ReplaceAllStringFunc(cleaned, func(match string) string {
		sub := integerParenRegex.FindStringSubmatch(match)
		if len(sub) > 1 {
			val, err := strconv.Atoi(sub[1])
			if err == nil && val >= 1900 && val <= 2099 {
				// Keep year in parentheses as is (e.g. (2009)) so noiseRegexes can clean parens while year is extracted
				return match
			}
		}
		// Strip episode counter (e.g. (98), (533), (1144))
		return " "
	})

	// 3. Remove broadcast noise and punctuation
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

// ExtractFingerprint constructs a ProgrammeFingerprint directly from an epg.Programme,
// automatically extracting 4-digit production years from parenthesized titles if not present in Date.
func ExtractFingerprint(prog *epg.Programme) epg.ProgrammeFingerprint {
	if prog == nil {
		return epg.ProgrammeFingerprint{FingerprintVersion: epg.CurrentFingerprintVersion}
	}

	var (
		year    int
		episode *epg.EpisodeInfo
		genre   string
	)

	if prog.Date != "" {
		year = parseYear(prog.Date)
	}

	// If no year was in Date, extract plausible production year from title if present in parentheses e.g. "Castle (2009)"
	if year == 0 {
		if sub := yearInParenRegex.FindStringSubmatch(prog.Title.Text); len(sub) > 1 {
			if y, err := strconv.Atoi(sub[1]); err == nil && y >= 1900 && y <= 2099 {
				year = y
			}
		}
	}

	if prog.Canonical != nil {
		episode = prog.Canonical.EpisodeInfo
		genre = prog.Canonical.Genre
	}

	return BuildFingerprint(prog.Title.Text, year, episode, genre)
}

// AttachEnrichment decorates an epg.Programme with external provider enrichment data without mutating E1 observed ratings.
func AttachEnrichment(prog *epg.Programme, data *epg.EnrichmentData) {
	if prog == nil || data == nil || data.Status != epg.MatchStatusFound {
		return
	}

	if prog.Canonical == nil {
		prog.Canonical = &epg.CanonicalMetadata{}
	}

	prog.Canonical.RatingScore = data.Rating
	prog.Canonical.ProviderAgeRatings = data.ProviderAgeRatings
	prog.Canonical.PosterURL = data.PosterURL
	prog.Canonical.ProviderSummary = data.Summary
}
