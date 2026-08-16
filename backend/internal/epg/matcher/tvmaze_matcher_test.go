// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package matcher

import (
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/epg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMatchTVMazeResults(t *testing.T) {
	t.Run("ExactMatch_WithMatchingYear", func(t *testing.T) {
		fp := epg.ProgrammeFingerprint{
			NormalizedTitle: "breaking bad",
			Year:            2008,
		}

		results := []TVMazeSearchResult{
			{
				Score: 0.95,
				Show: TVMazeShow{
					ID:        169,
					Name:      "Breaking Bad",
					Premiered: "2008-01-20",
				},
			},
		}

		matched, class := MatchTVMazeResults(fp, results)
		require.NotNil(t, matched)
		assert.Equal(t, MatchExact, class)
		assert.Equal(t, 169, matched.ID)
	})

	t.Run("StrongMatch_WithoutFingerprintYear", func(t *testing.T) {
		fp := epg.ProgrammeFingerprint{
			NormalizedTitle: "better call saul",
			Year:            0,
		}

		results := []TVMazeSearchResult{
			{
				Score: 0.92,
				Show: TVMazeShow{
					ID:        618,
					Name:      "Better Call Saul",
					Premiered: "2015-02-08",
				},
			},
		}

		matched, class := MatchTVMazeResults(fp, results)
		require.NotNil(t, matched)
		assert.Equal(t, MatchStrong, class)
		assert.Equal(t, 618, matched.ID)
	})

	t.Run("DisambiguationByYear_MultipleShowsSameName", func(t *testing.T) {
		// "The Office" UK (2001) vs "The Office" US (2005)
		results := []TVMazeSearchResult{
			{
				Show: TVMazeShow{ID: 101, Name: "The Office", Premiered: "2001-07-09"},
			},
			{
				Show: TVMazeShow{ID: 526, Name: "The Office", Premiered: "2005-03-24"},
			},
		}

		// When year is 2005 -> resolves US version
		fpUS := epg.ProgrammeFingerprint{NormalizedTitle: "the office", Year: 2005}
		matchedUS, classUS := MatchTVMazeResults(fpUS, results)
		require.NotNil(t, matchedUS)
		assert.Equal(t, MatchExact, classUS)
		assert.Equal(t, 526, matchedUS.ID)

		// When year is missing -> rejects as ambiguous (false negative > false positive)
		fpAmbiguous := epg.ProgrammeFingerprint{NormalizedTitle: "the office", Year: 0}
		matchedAmb, classAmb := MatchTVMazeResults(fpAmbiguous, results)
		assert.Nil(t, matchedAmb)
		assert.Equal(t, MatchNone, classAmb)
	})

	t.Run("EmptyResults_ReturnsNone", func(t *testing.T) {
		fp := epg.ProgrammeFingerprint{NormalizedTitle: "nonexistent show 999"}
		matched, class := MatchTVMazeResults(fp, nil)
		assert.Nil(t, matched)
		assert.Equal(t, MatchNone, class)
	})
}

func TestBuildEnrichmentFromShow(t *testing.T) {
	fp := epg.ProgrammeFingerprint{
		NormalizedTitle:    "dark",
		Season:             1,
		Episode:            3,
		FingerprintVersion: epg.CurrentFingerprintVersion,
	}

	ratingVal := 8.7
	show := &TVMazeShow{
		ID:        1784,
		Name:      "Dark",
		Premiered: "2017-12-01",
		Summary:   "<p>A missing child sets four families on a frantic hunt for answers.</p>",
	}
	show.Rating.Average = &ratingVal
	show.Image.Medium = "https://static.tvmaze.com/uploads/images/medium/1784.jpg"

	epRating := 8.9
	episode := &TVMazeEpisode{
		ID:      554433,
		Name:    "Past and Present",
		Season:  1,
		Number:  3,
		Summary: "<b>1986.</b> Mads Nielsen has been missing for a month.",
	}
	episode.Rating.Average = &epRating
	episode.Image.Medium = "https://static.tvmaze.com/uploads/images/medium/ep554433.jpg"

	now := time.Now()
	data := BuildEnrichmentFromShow(fp, show, episode, now)

	require.NotNil(t, data)
	assert.Equal(t, fp.Key(), data.FingerprintKey)
	assert.Equal(t, epg.MatchStatusFound, data.Status)
	assert.Equal(t, "tvmaze", data.Identity.Provider)
	assert.Equal(t, "episode", data.Identity.Type)
	assert.Equal(t, "554433", data.Identity.ID)
	assert.Equal(t, "1986. Mads Nielsen has been missing for a month.", data.Summary, "HTML must be stripped")
	assert.Equal(t, "https://static.tvmaze.com/uploads/images/medium/ep554433.jpg", data.PosterURL)

	require.NotNil(t, data.Rating)
	assert.Equal(t, 8.9, data.Rating.Score)
	assert.Equal(t, 10.0, data.Rating.Scale)
	assert.Equal(t, "tvmaze", data.Rating.Source)
}
