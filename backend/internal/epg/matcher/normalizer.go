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
		regexp.MustCompile(`(?i)\b(?:S\d{1,2}E\d{1,2}|\d{1,2}x\d{1,2}|Staffel\s+\d+|Folge\s+\d+|Episode\s+\d+)\b`),
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

// ExtractFingerprint constructs a ProgrammeFingerprint directly from an epg.Programme.
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
