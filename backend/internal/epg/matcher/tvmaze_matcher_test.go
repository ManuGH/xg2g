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
	t.Run("ExactMatch_WithMatchingYearSourceTitle", func(t *testing.T) {
		fp := epg.ProgrammeFingerprint{
			NormalizedTitle: "castle",
			Year:            2009,
			YearSource:      epg.YearSourceTitle,
		}

		results := []TVMazeSearchResult{
			{
				Score: 0.95,
				Show: TVMazeShow{
					ID:        45,
					Name:      "Castle",
					Premiered: "2009-03-09",
					Ended:     "2016-05-16",
					Type:      "Scripted",
				},
			},
			{
				Score: 0.80,
				Show: TVMazeShow{
					ID:        999,
					Name:      "Castle",
					Premiered: "1983-05-10",
					Ended:     "1983-06-15",
					Type:      "Scripted",
				},
			},
		}

		matched, class := MatchTVMazeResults(fp, results)
		require.NotNil(t, matched)
		assert.Equal(t, MatchExact, class)
		assert.Equal(t, 45, matched.ID)
	})

	t.Run("YearSourceTitle_MismatchedYear_Rejected", func(t *testing.T) {
		fp := epg.ProgrammeFingerprint{
			NormalizedTitle: "castle",
			Year:            2015,
			YearSource:      epg.YearSourceTitle,
		}

		results := []TVMazeSearchResult{
			{
				Show: TVMazeShow{
					ID:        45,
					Name:      "Castle",
					Premiered: "2009-03-09",
				},
			},
		}

		matched, class := MatchTVMazeResults(fp, results)
		assert.Nil(t, matched)
		assert.Equal(t, MatchNone, class)
	})

	t.Run("FalsePositive_LadiesNight_Rejected", func(t *testing.T) {
		// WDR Ladies Night (2) has no year, no S/E, but had an episode counter (2)
		// TVMaze returns US BET docuseries (Type: Reality, Language: English, Genres: Music)
		fp := epg.ProgrammeFingerprint{
			NormalizedTitle:  "ladies night",
			HadEpisodeMarker: true,
			Year:             0,
			EventGenre:       "",
		}

		results := []TVMazeSearchResult{
			{
				Show: TVMazeShow{
					ID:       42013,
					Name:     "Ladies Night",
					Type:     "Reality",
					Language: "English",
					Genres:   []string{"Music"},
				},
			},
		}

		matched, class := MatchTVMazeResults(fp, results)
		assert.Nil(t, matched, "Ladies Night non-scripted English reality show must be rejected")
		assert.Equal(t, MatchNone, class)
	})

	t.Run("MovieGenre_StrictlyRejectedForTVMaze", func(t *testing.T) {
		fp := epg.ProgrammeFingerprint{
			NormalizedTitle: "resident evil",
			EventGenre:      "movie",
		}

		results := []TVMazeSearchResult{
			{
				Show: TVMazeShow{
					ID:        46273,
					Name:      "Resident Evil",
					Type:      "Scripted",
					Premiered: "2022-07-14",
				},
			},
		}

		matched, class := MatchTVMazeResults(fp, results)
		assert.Nil(t, matched, "DVB movie genre must be strictly rejected by TVMaze matcher")
		assert.Equal(t, MatchNone, class)
	})

	t.Run("StrongMatch_WithEpisodeMarkerAndScripted", func(t *testing.T) {
		fp := epg.ProgrammeFingerprint{
			NormalizedTitle:  "in aller freundschaft",
			HadEpisodeMarker: true,
			EventGenre:       "series",
		}

		results := []TVMazeSearchResult{
			{
				Show: TVMazeShow{
					ID:        16924,
					Name:      "In aller Freundschaft",
					Type:      "Scripted",
					Language:  "German",
					Premiered: "1998-10-26",
				},
			},
		}

		matched, class := MatchTVMazeResults(fp, results)
		require.NotNil(t, matched)
		assert.Equal(t, MatchStrong, class)
		assert.Equal(t, 16924, matched.ID)
	})

	t.Run("StrongMatch_WithXMLTVDate_InSeriesRuntimeWindow", func(t *testing.T) {
		// DVB XMLTV Date 2014 for Hubert und Staller (Premiered 2011, Ended 2018)
		fp := epg.ProgrammeFingerprint{
			NormalizedTitle: "hubert und staller",
			Year:            2014,
			YearSource:      epg.YearSourceXMLTVDate,
			EventGenre:      "series",
		}

		results := []TVMazeSearchResult{
			{
				Show: TVMazeShow{
					ID:        32709,
					Name:      "Hubert und Staller",
					Type:      "Scripted",
					Premiered: "2011-11-02",
					Ended:     "2018-12-19",
				},
			},
		}

		matched, class := MatchTVMazeResults(fp, results)
		require.NotNil(t, matched)
		assert.Equal(t, MatchStrong, class)
		assert.Equal(t, 32709, matched.ID)
	})

	t.Run("XMLTVDate_OutsideSeriesWindow_PrecedingPremiere_Rejected", func(t *testing.T) {
		fp := epg.ProgrammeFingerprint{
			NormalizedTitle: "hubert und staller",
			Year:            2005,
			YearSource:      epg.YearSourceXMLTVDate,
			EventGenre:      "series",
		}

		results := []TVMazeSearchResult{
			{
				Show: TVMazeShow{
					ID:        32709,
					Name:      "Hubert und Staller",
					Type:      "Scripted",
					Premiered: "2011-11-02",
					Ended:     "2018-12-19",
				},
			},
		}

		matched, class := MatchTVMazeResults(fp, results)
		assert.Nil(t, matched)
		assert.Equal(t, MatchNone, class)
	})

	t.Run("StrongMatch_CanonicalGenreShow", func(t *testing.T) {
		fp := epg.ProgrammeFingerprint{
			NormalizedTitle: "wer weiß denn sowas",
			EventGenre:      "show",
		}

		results := []TVMazeSearchResult{
			{
				Show: TVMazeShow{
					ID:       84943,
					Name:     "Wer weiß denn sowas?",
					Type:     "Game Show",
					Language: "German",
				},
			},
		}

		matched, class := MatchTVMazeResults(fp, results)
		require.NotNil(t, matched)
		assert.Equal(t, MatchStrong, class)
		assert.Equal(t, 84943, matched.ID)
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
