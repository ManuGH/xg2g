// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package matcher

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/epg"
	"github.com/stretchr/testify/assert"
)

func TestNormalizeTitle(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{
			input: "Tatort: Das Team (HD)",
			want:  "tatort: das team",
		},
		{
			input: "[Live] Sportschau Bundesliga UHD",
			want:  "sportschau bundesliga",
		},
		{
			input: "Tagesschau 20:00 (Wdh.)",
			want:  "tagesschau 20:00",
		},
		{
			input: "Breaking Bad S02E05",
			want:  "breaking bad",
		},
		{
			input: "   Dokumentation:   Die Alpen  [Neu]   ",
			want:  "dokumentation: die alpen",
		},
		{
			input: "Hubert und Staller (98)",
			want:  "hubert und staller",
		},
		{
			input: "In aller Freundschaft (1144)",
			want:  "in aller freundschaft",
		},
		{
			input: "Alles über Maria (1/2)",
			want:  "alles über maria",
		},
		{
			input: "Wildes Großbritannien (2/4)",
			want:  "wildes großbritannien",
		},
		{
			input: "Hubert und Staller (101/116)",
			want:  "hubert und staller",
		},
		{
			input: "CSI: Miami (HD)",
			want:  "csi: miami",
		},
		{
			input: "Castle (2009)",
			want:  "castle",
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := NormalizeTitle(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestExtractFingerprint_ExtractsYearAndEpisodeMarkers(t *testing.T) {
	prog := &epg.Programme{
		Title: epg.Title{Text: "Castle (2009)"},
	}

	fp := ExtractFingerprint(prog)
	assert.Equal(t, "castle", fp.NormalizedTitle)
	assert.Equal(t, 2009, fp.Year)
	assert.Equal(t, epg.YearSourceTitle, fp.YearSource)
	assert.False(t, fp.HadEpisodeMarker)
	assert.Equal(t, 3, fp.FingerprintVersion)

	prog2 := &epg.Programme{
		Title: epg.Title{Text: "Hubert und Staller (98)"},
	}
	fp2 := ExtractFingerprint(prog2)
	assert.Equal(t, "hubert und staller", fp2.NormalizedTitle)
	assert.Equal(t, 0, fp2.Year)
	assert.True(t, fp2.HadEpisodeMarker)
	assert.Equal(t, 3, fp2.FingerprintVersion)
}

func TestBuildFingerprint(t *testing.T) {
	ep := &epg.EpisodeInfo{
		SeasonNumber:  3,
		EpisodeNumber: 12,
		SourcePattern: "SxxExx",
	}

	fp := BuildFingerprint("Der Bergdoktor (HD)", 2021, epg.YearSourceXMLTVDate, ep, "series", false)

	assert.Equal(t, "der bergdoktor", fp.NormalizedTitle)
	assert.Equal(t, 2021, fp.Year)
	assert.Equal(t, epg.YearSourceXMLTVDate, fp.YearSource)
	assert.Equal(t, 3, fp.Season)
	assert.Equal(t, 12, fp.Episode)
	assert.Equal(t, "series", fp.EventGenre)
	assert.Equal(t, 3, fp.FingerprintVersion)

	key1 := fp.Key()
	assert.NotEmpty(t, key1)

	// Idempotency: same fingerprint must produce exact same Key
	fp2 := BuildFingerprint("Der Bergdoktor [HD]", 2021, epg.YearSourceXMLTVDate, ep, "series", false)
	assert.Equal(t, key1, fp2.Key())
}
