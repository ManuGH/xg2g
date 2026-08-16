// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package matcher

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/epg"
	"github.com/ManuGH/xg2g/internal/epg/store"
)

var htmlTagRegex = regexp.MustCompile(`<[^>]*>`)

// StripHTML removes HTML markup from provider text summaries.
func StripHTML(input string) string {
	cleaned := htmlTagRegex.ReplaceAllString(input, "")
	cleaned = strings.TrimSpace(cleaned)
	return cleaned
}

// TVMazeShow represents a show object returned by the TVMaze REST API.
type TVMazeShow struct {
	ID        int      `json:"id"`
	Name      string   `json:"name"`
	Type      string   `json:"type"`
	Language  string   `json:"language"`
	Genres    []string `json:"genres"`
	Status    string   `json:"status"`
	Premiered string   `json:"premiered"` // e.g. "2008-01-20"
	Ended     string   `json:"ended"`     // e.g. "2013-09-29"
	Rating    struct {
		Average *float64 `json:"average"`
	} `json:"rating"`
	Image struct {
		Medium   string `json:"medium"`
		Original string `json:"original"`
	} `json:"image"`
	Summary string `json:"summary"`
}

// TVMazeSearchResult represents an item in the TVMaze show search response.
type TVMazeSearchResult struct {
	Score float64    `json:"score"`
	Show  TVMazeShow `json:"show"`
}

// TVMazeEpisode represents an episode object returned by TVMaze episode lookup.
type TVMazeEpisode struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Season int    `json:"season"`
	Number int    `json:"number"`
	Rating struct {
		Average *float64 `json:"average"`
	} `json:"rating"`
	Image struct {
		Medium   string `json:"medium"`
		Original string `json:"original"`
	} `json:"image"`
	Summary string `json:"summary"`
}

// MatchClassification indicates the deterministic quality of a candidate match.
type MatchClassification string

const (
	MatchExact     MatchClassification = "exact"     // Exact title match + matching year/episode
	MatchStrong    MatchClassification = "strong"    // Exact title match with corroborating DVB evidence
	MatchCandidate MatchClassification = "candidate" // Fuzzy title match with supporting metadata
	MatchNone      MatchClassification = "none"      // No match or rejected as ambiguous
)

// isCanonicalGenreIncompatible returns true if there is a hard, irreconcilable conflict
// between DVB canonical genre and TVMaze show type/genres.
func isCanonicalGenreIncompatible(dvbGenre string, show *TVMazeShow) bool {
	if show == nil {
		return true
	}
	showTypeLower := strings.ToLower(show.Type)
	showGenres := make([]string, len(show.Genres))
	for i, g := range show.Genres {
		showGenres[i] = strings.ToLower(g)
	}

	hasGenre := func(genre string) bool {
		for _, g := range showGenres {
			if g == genre {
				return true
			}
		}
		return false
	}

	switch dvbGenre {
	case "movie":
		// TVMaze is strictly for episodic/series content. Movies are completely rejected.
		return true

	case "sport":
		// Sports broadcasts never match fiction, drama, comedy, or anime
		if showTypeLower == "scripted" || showTypeLower == "animation" || hasGenre("drama") || hasGenre("comedy") {
			return true
		}

	case "news":
		// News broadcasts never match scripted or animation series
		if showTypeLower == "scripted" || showTypeLower == "animation" || showTypeLower == "reality" {
			return true
		}

	case "series":
		// Fiction series do not match standalone news or sports broadcasts
		if showTypeLower == "news" || showTypeLower == "sports" {
			return true
		}

	case "show":
		// Talk/game shows do not match scripted drama or animation
		if showTypeLower == "scripted" || showTypeLower == "animation" {
			return true
		}

	case "kids":
		if hasGenre("horror") || hasGenre("crime") || hasGenre("erotic") {
			return true
		}
	}

	return false
}

// isCanonicalGenreCompatible returns true if DVB canonical genre positively corroborates TVMaze show.
func isCanonicalGenreCompatible(dvbGenre string, show *TVMazeShow) bool {
	if show == nil || dvbGenre == "" {
		return false
	}
	showTypeLower := strings.ToLower(show.Type)
	showGenres := make([]string, len(show.Genres))
	for i, g := range show.Genres {
		showGenres[i] = strings.ToLower(g)
	}

	hasGenre := func(genre string) bool {
		for _, g := range showGenres {
			if g == genre {
				return true
			}
		}
		return false
	}

	switch dvbGenre {
	case "series":
		return showTypeLower == "scripted" || showTypeLower == "animation" || showTypeLower == "reality"
	case "kids":
		return showTypeLower == "animation" || hasGenre("children") || hasGenre("family") || hasGenre("anime")
	case "show":
		return showTypeLower == "game show" || showTypeLower == "variety" || showTypeLower == "talk show" || showTypeLower == "reality"
	case "docu":
		return showTypeLower == "documentary" || hasGenre("nature") || hasGenre("history") || hasGenre("medical")
	case "sport":
		return showTypeLower == "sports"
	case "music":
		return showTypeLower == "music" || hasGenre("music")
	}

	return false
}

// evaluateStrongMatchEvidence validates that an exact-title candidate has cross-source DVB evidence and no hard conflict.
func evaluateStrongMatchEvidence(fp epg.ProgrammeFingerprint, show *TVMazeShow) bool {
	if show == nil {
		return false
	}

	// 1. Hard Conflict Check: Canonical Genre Incompatibility
	if isCanonicalGenreIncompatible(fp.EventGenre, show) {
		return false
	}

	// 2. Hard Conflict Check: Non-Scripted Foreign Format Clash (e.g. Ladies Night BET docu-series)
	showTypeLower := strings.ToLower(show.Type)
	showLangLower := strings.ToLower(show.Language)
	isScriptedOrAnim := showTypeLower == "scripted" || showTypeLower == "animation"

	if !isScriptedOrAnim && showLangLower == "english" {
		// Non-scripted English show (Reality, Music, Talk) requires explicit DVB S/E or DVB genre match
		if fp.Season == 0 && fp.Episode == 0 && fp.EventGenre != "music" && fp.EventGenre != "show" {
			return false
		}
	}

	// 3. Positive DVB Evidence Verification (At least 1 required)
	// Evidence 1: DVB Season or Episode number
	if fp.Season > 0 || fp.Episode > 0 {
		return true
	}

	// Evidence 2: Parenthesized episode counter on episodic format (Scripted / Animation)
	if fp.HadEpisodeMarker && isScriptedOrAnim {
		return true
	}

	// Evidence 3: Canonical DVB Genre positive agreement
	if isCanonicalGenreCompatible(fp.EventGenre, show) {
		return true
	}

	// Evidence 4: XMLTV date falling within valid show runtime window
	if fp.YearSource == epg.YearSourceXMLTVDate && fp.Year > 0 {
		showPremieredYear := parseYear(show.Premiered)
		showEndedYear := parseYear(show.Ended)
		if showPremieredYear > 0 && fp.Year >= showPremieredYear && (showEndedYear == 0 || fp.Year <= showEndedYear+1) {
			return true
		}
	}

	return false
}

// MatchTVMazeResults evaluates TVMaze search candidates against a ProgrammeFingerprint.
// Invariant: False negatives are strictly preferred over false positives. Ambiguous candidates are rejected.
func MatchTVMazeResults(fp epg.ProgrammeFingerprint, results []TVMazeSearchResult) (*TVMazeShow, MatchClassification) {
	if len(results) == 0 || fp.NormalizedTitle == "" {
		return nil, MatchNone
	}

	// 1. Movie Exclusion: TVMaze is strictly for episodic/series content.
	if fp.EventGenre == "movie" {
		return nil, MatchNone
	}

	var (
		exactMatches  []*TVMazeShow
		strongMatches []*TVMazeShow
	)

	for i := range results {
		show := &results[i].Show
		normShowName := NormalizeTitle(show.Name)
		showPremieredYear := parseYear(show.Premiered)
		showEndedYear := parseYear(show.Ended)

		if normShowName == fp.NormalizedTitle {
			if fp.Year > 0 {
				if fp.YearSource == epg.YearSourceTitle || fp.YearSource == "" {
					// Title year or direct year (e.g. "Castle (2009)" / year 2008): exact match with show Premiered year
					if showPremieredYear > 0 && fp.Year == showPremieredYear {
						if !isCanonicalGenreIncompatible(fp.EventGenre, show) {
							exactMatches = append(exactMatches, show)
						}
					} else if fp.YearSource == "" && evaluateStrongMatchEvidence(fp, show) {
						strongMatches = append(strongMatches, show)
					}
					// If title year explicitly set and mismatches show premiered year, do not add to strongMatches (hard mismatch)
				} else if fp.YearSource == epg.YearSourceXMLTVDate {
					// XMLTV Date (episode production date): must fall within valid series runtime window
					if showPremieredYear > 0 {
						if fp.Year < showPremieredYear-1 {
							// Episode cannot precede series premiere -> hard reject
							continue
						}
						if showEndedYear > 0 && fp.Year > showEndedYear+1 {
							// Episode cannot succeed series end -> hard reject
							continue
						}
					}
					if evaluateStrongMatchEvidence(fp, show) {
						strongMatches = append(strongMatches, show)
					}
				}
			} else {
				// No year in DVB: evaluate strong match evidence
				if evaluateStrongMatchEvidence(fp, show) {
					strongMatches = append(strongMatches, show)
				}
			}
		}
	}

	// 1. Single exact match with year verification
	if len(exactMatches) == 1 {
		return exactMatches[0], MatchExact
	}
	// 2. If multiple exact matches exist, resolve by year if available
	if len(exactMatches) > 1 {
		// Ambiguous exact matches
		return nil, MatchNone
	}

	// 3. Single strong match with cross-source DVB evidence and no hard conflict
	if len(strongMatches) == 1 {
		return strongMatches[0], MatchStrong
	}

	// If multiple strong candidates exist without differentiating evidence, reject as ambiguous
	return nil, MatchNone
}

// BuildEnrichmentFromShow maps a matched TVMazeShow to canonical EnrichmentData.
func BuildEnrichmentFromShow(fp epg.ProgrammeFingerprint, show *TVMazeShow, episode *TVMazeEpisode, now time.Time) *epg.EnrichmentData {
	if show == nil {
		return &epg.EnrichmentData{
			FingerprintKey:     fp.Key(),
			FingerprintVersion: fp.FingerprintVersion,
			MatcherVersion:     epg.CurrentMatcherVersion,
			Status:             epg.MatchStatusNoMatch,
			FetchedAt:          now,
			ExpiresAt:          now.Add(store.DefaultNoMatchTTL),
		}
	}

	data := &epg.EnrichmentData{
		FingerprintKey:     fp.Key(),
		FingerprintVersion: fp.FingerprintVersion,
		MatcherVersion:     epg.CurrentMatcherVersion,
		Status:             epg.MatchStatusFound,
		Identity: epg.ProviderIdentity{
			Provider: "tvmaze",
			Type:     "show",
			ID:       strconv.Itoa(show.ID),
		},
		PosterURL: show.Image.Medium,
		Summary:   StripHTML(show.Summary),
		FetchedAt: now,
		ExpiresAt: now.Add(store.DefaultMatchTTL),
	}

	if show.Rating.Average != nil && *show.Rating.Average > 0 {
		data.Rating = &epg.RatingScore{
			Score:  *show.Rating.Average,
			Scale:  10.0,
			Source: "tvmaze",
		}
	}

	// If episode-level enrichment is available, refine identity and poster/summary
	if episode != nil {
		data.Identity.Type = "episode"
		data.Identity.ID = strconv.Itoa(episode.ID)
		if episode.Image.Medium != "" {
			data.PosterURL = episode.Image.Medium
		}
		if episode.Summary != "" {
			data.Summary = StripHTML(episode.Summary)
		}
		if episode.Rating.Average != nil && *episode.Rating.Average > 0 {
			data.Rating = &epg.RatingScore{
				Score:  *episode.Rating.Average,
				Scale:  10.0,
				Source: "tvmaze",
			}
		}
	}

	return data
}

func parseYear(premiered string) int {
	if len(premiered) < 4 {
		return 0
	}
	year, err := strconv.Atoi(premiered[:4])
	if err != nil {
		return 0
	}
	return year
}
