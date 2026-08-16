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
	MatchStrong    MatchClassification = "strong"    // Exact title match without conflicting year
	MatchCandidate MatchClassification = "candidate" // Fuzzy title match with supporting metadata
	MatchNone      MatchClassification = "none"      // No match or rejected as ambiguous
)

// MatchTVMazeResults evaluates TVMaze search candidates against a ProgrammeFingerprint.
// Invariant: False negatives are strictly preferred over false positives. Ambiguous candidates are rejected.
func MatchTVMazeResults(fp epg.ProgrammeFingerprint, results []TVMazeSearchResult) (*TVMazeShow, MatchClassification) {
	if len(results) == 0 || fp.NormalizedTitle == "" {
		return nil, MatchNone
	}

	var (
		exactMatches  []*TVMazeShow
		strongMatches []*TVMazeShow
	)

	for i := range results {
		show := &results[i].Show
		normShowName := NormalizeTitle(show.Name)
		showYear := parseYear(show.Premiered)

		if normShowName == fp.NormalizedTitle {
			if fp.Year > 0 && showYear > 0 {
				if fp.Year == showYear {
					exactMatches = append(exactMatches, show)
				}
			} else {
				strongMatches = append(strongMatches, show)
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

	// 3. Single strong match (exact normalized title, no conflicting year)
	if len(strongMatches) == 1 {
		return strongMatches[0], MatchStrong
	}

	// If multiple strong candidates exist without differentiating year, reject as ambiguous
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
