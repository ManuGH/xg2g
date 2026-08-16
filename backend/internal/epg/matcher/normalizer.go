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

// TitleNormalizationEvidence captures both normalized title text and structural broadcast markers.
type TitleNormalizationEvidence struct {
	NormalizedTitle   string `json:"normalizedTitle"`
	HadEpisodeCounter bool   `json:"hadEpisodeCounter"`
	HadFractionalPart bool   `json:"hadFractionalPart"`
	ExtractedYear     int    `json:"extractedYear"`
}

// NormalizeTitleWithEvidence analyzes the title, extracts year or episode counters,
// and produces a clean NormalizedTitle with all year/episode brackets removed.
func NormalizeTitleWithEvidence(title string) TitleNormalizationEvidence {
	ev := TitleNormalizationEvidence{}
	cleaned := title

	// 1. Check & strip fractional episode counters in parentheses: (1/2), (101/116)
	if fractionalEpisodeParenRegex.MatchString(cleaned) {
		ev.HadFractionalPart = true
		cleaned = fractionalEpisodeParenRegex.ReplaceAllString(cleaned, " ")
	}

	// 2. Check & strip integer parentheses: (98), (533), (1144), (2009)
	cleaned = integerParenRegex.ReplaceAllStringFunc(cleaned, func(match string) string {
		sub := integerParenRegex.FindStringSubmatch(match)
		if len(sub) > 1 {
			val, err := strconv.Atoi(sub[1])
			if err == nil && val >= 1900 && val <= 2099 {
				// Record extracted 4-digit production year
				ev.ExtractedYear = val
				// Strip year from normalized title so "Castle (2009)" becomes "castle"
				return " "
			}
			// Standalone episode counter (e.g. (98), (533), (1144))
			ev.HadEpisodeCounter = true
			return " "
		}
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
	ev.NormalizedTitle = cleaned
	return ev
}

// NormalizeTitle strips broadcast noise, resolution markers, bracketed tags,
// and parenthesized episode/year counters from programme titles.
func NormalizeTitle(title string) string {
	return NormalizeTitleWithEvidence(title).NormalizedTitle
}

// BuildFingerprint constructs a deterministic ProgrammeFingerprint from title and canonical metadata.
func BuildFingerprint(title string, year int, yearSource epg.YearSource, episode *epg.EpisodeInfo, genre string, hadEpisodeMarker bool) epg.ProgrammeFingerprint {
	ev := NormalizeTitleWithEvidence(title)
	if ev.ExtractedYear > 0 && year == 0 {
		year = ev.ExtractedYear
		yearSource = epg.YearSourceTitle
	}
	if ev.HadEpisodeCounter || ev.HadFractionalPart {
		hadEpisodeMarker = true
	}

	fp := epg.ProgrammeFingerprint{
		NormalizedTitle:    ev.NormalizedTitle,
		Year:               year,
		YearSource:         yearSource,
		EventGenre:         genre,
		HadEpisodeMarker:   hadEpisodeMarker,
		FingerprintVersion: epg.CurrentFingerprintVersion,
	}

	if episode != nil {
		fp.Season = episode.SeasonNumber
		fp.Episode = episode.EpisodeNumber
	}

	return fp
}

// ExtractFingerprint constructs a ProgrammeFingerprint directly from an epg.Programme,
// automatically extracting 4-digit production years from parenthesized titles or XMLTV Date.
func ExtractFingerprint(prog *epg.Programme) epg.ProgrammeFingerprint {
	if prog == nil {
		return epg.ProgrammeFingerprint{FingerprintVersion: epg.CurrentFingerprintVersion}
	}

	ev := NormalizeTitleWithEvidence(prog.Title.Text)

	var (
		year       int
		yearSource epg.YearSource
		episode    *epg.EpisodeInfo
		genre      string
	)

	if ev.ExtractedYear > 0 {
		year = ev.ExtractedYear
		yearSource = epg.YearSourceTitle
	} else if prog.Date != "" {
		year = parseYear(prog.Date)
		if year > 0 {
			yearSource = epg.YearSourceXMLTVDate
		}
	}

	if prog.Canonical != nil {
		episode = prog.Canonical.EpisodeInfo
		genre = prog.Canonical.Genre
	}

	hadEpisodeMarker := ev.HadEpisodeCounter || ev.HadFractionalPart

	var seasonNum, episodeNum int
	if episode != nil {
		seasonNum = episode.SeasonNumber
		episodeNum = episode.EpisodeNumber
	}

	return epg.ProgrammeFingerprint{
		NormalizedTitle:    ev.NormalizedTitle,
		Year:               year,
		YearSource:         yearSource,
		Season:             seasonNum,
		Episode:            episodeNum,
		EventGenre:         genre,
		HadEpisodeMarker:   hadEpisodeMarker,
		FingerprintVersion: epg.CurrentFingerprintVersion,
	}
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
