// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package epg

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractAgeRating_ExplicitFSK(t *testing.T) {
	tests := []struct {
		name       string
		title      string
		desc       string
		wantVal    int
		wantScheme string
		wantCntry  string
	}{
		{
			name:       "FSK 12 in description",
			title:      "Tatort: Murot",
			desc:       "Ein spannender Fall. FSK: 12. Regie: Schmidt.",
			wantVal:    12,
			wantScheme: "FSK",
			wantCntry:  "DE",
		},
		{
			name:       "FSK 16 in brackets",
			title:      "Action Movie",
			desc:       "Spannender Thriller [FSK 16]. Mit Tom Hanks.",
			wantVal:    16,
			wantScheme: "FSK",
			wantCntry:  "DE",
		},
		{
			name:       "FSK 18 in parentheses",
			title:      "Horror Night",
			desc:       "Blutige Szenen (FSK 18).",
			wantVal:    18,
			wantScheme: "FSK",
			wantCntry:  "DE",
		},
		{
			name:       "FSK 6 plain text",
			title:      "Familienfilm",
			desc:       "Freigegeben ab 6 Jahren gemäß FSK. Viel Spaß!",
			wantVal:    6,
			wantScheme: "FSK",
			wantCntry:  "DE",
		},
		{
			name:       "FSK o.A. (0 years)",
			title:      "Kinderfilm",
			desc:       "Freigabe: FSK o.A.",
			wantVal:    0,
			wantScheme: "FSK",
			wantCntry:  "DE",
		},
		{
			name:       "FSK 0 plain",
			title:      "Märchen",
			desc:       "FSK: 0. Ein schönes Märchen.",
			wantVal:    0,
			wantScheme: "FSK",
			wantCntry:  "DE",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractAgeRating(tt.title, tt.desc, "")
			require.NotNil(t, got)
			assert.Equal(t, tt.wantVal, got.Value)
			assert.Equal(t, tt.wantScheme, got.Scheme)
			assert.Equal(t, tt.wantCntry, got.Country)
			assert.Equal(t, RatingSourceDVBText, got.Source)
			assert.Equal(t, RatingConfidenceObserved, got.Confidence)
		})
	}
}

func TestExtractAgeRating_BroadcasterAgeNonFSK(t *testing.T) {
	tests := []struct {
		name    string
		title   string
		desc    string
		wantVal int
	}{
		{
			name:    "ab 12 Jahren without FSK mention",
			title:   "ORF Universum",
			desc:    "Dokumentation. Für Jugendliche ab 12 Jahren geeignet.",
			wantVal: 12,
		},
		{
			name:    "ab 16 in description",
			title:   "ServusTV Spielfilm",
			desc:    "Spannung ab 16 Jahren.",
			wantVal: 16,
		},
		{
			name:    "Altersfreigabe: 18",
			title:   "Late Night Cinema",
			desc:    "Altersfreigabe: 18.",
			wantVal: 18,
		},
		{
			name:    "geeignet ab 6",
			title:   "Kinderserie",
			desc:    "Sendung geeignet ab 6 Jahren.",
			wantVal: 6,
		},
		{
			name:    "18+ symbol",
			title:   "Midnight Special",
			desc:    "18+ Nur für Erwachsene.",
			wantVal: 18,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractAgeRating(tt.title, tt.desc, "")
			require.NotNil(t, got)
			assert.Equal(t, tt.wantVal, got.Value)
			// Strict Rule: Non-FSK text MUST NOT produce Scheme="FSK" or Country="DE"!
			assert.Equal(t, "broadcaster_age", got.Scheme)
			assert.Equal(t, "unknown", got.Country)
			assert.Equal(t, RatingSourceDVBText, got.Source)
			assert.Equal(t, RatingConfidenceObserved, got.Confidence)
		})
	}
}

func TestExtractAgeRating_UnratedReturnsNil(t *testing.T) {
	unrated := []struct {
		name  string
		title string
		desc  string
	}{
		{
			name:  "News show",
			title: "Tagesschau",
			desc:  "Nachrichten des Tages aus Politik und Wirtschaft.",
		},
		{
			name:  "Talk show",
			title: "Markus Lanz",
			desc:  "Gäste diskutieren über aktuelle Themen.",
		},
		{
			name:  "False positive numbers in title (Room 12)",
			title: "Zimmer 12",
			desc:  "Krimi im Hotel.",
		},
		{
			name:  "False positive numbers in text (180 Grad)",
			title: "Reportage",
			desc:  "Eine 180 Grad Wende in der Politik.",
		},
		{
			name:  "Route 66",
			title: "Reisebericht",
			desc:  "Fahrt entlang der Route 66.",
		},
		{
			name:  "Empty strings",
			title: "",
			desc:  "",
		},
	}

	for _, tt := range unrated {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractAgeRating(tt.title, tt.desc, "")
			assert.Nil(t, got, "Expected nil for unrated programme: %s", tt.name)
		})
	}
}

func TestExtractEpisodeInfo_PrecisionPatterns(t *testing.T) {
	tests := []struct {
		name        string
		title       string
		desc        string
		wantSeason  int
		wantEpisode int
		wantPattern string
	}{
		{
			name:        "S02E05 standard",
			title:       "Breaking Bad S02E05",
			desc:        "Walter White plant seinen nächsten Schritt.",
			wantSeason:  2,
			wantEpisode: 5,
			wantPattern: "SxxExx",
		},
		{
			name:        "s1e3 lowercase",
			title:       "The Wire (s1e3)",
			desc:        "Ermittlungen in West Baltimore.",
			wantSeason:  1,
			wantEpisode: 3,
			wantPattern: "SxxExx",
		},
		{
			name:        "2x05 NxNN format",
			title:       "Game of Thrones",
			desc:        "Staffelfinale 2x05: Krieg der Könige.",
			wantSeason:  2,
			wantEpisode: 5,
			wantPattern: "NxNN",
		},
		{
			name:        "Staffel 3, Folge 12 German text",
			title:       "Der Bergdoktor",
			desc:        "Staffel 3, Folge 12: Neue Herausforderungen in den Bergen.",
			wantSeason:  3,
			wantEpisode: 12,
			wantPattern: "Staffel N, Folge M",
		},
		{
			name:        "Isolated Folge 42",
			title:       "Lindenstraße",
			desc:        "Folge 42. Nachbarschaftsgeschichten.",
			wantSeason:  0,
			wantEpisode: 42,
			wantPattern: "Folge N",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractEpisodeInfo(tt.title, tt.desc)
			require.NotNil(t, got)
			assert.Equal(t, tt.wantSeason, got.SeasonNumber)
			assert.Equal(t, tt.wantEpisode, got.EpisodeNumber)
			assert.Equal(t, tt.wantPattern, got.SourcePattern)
		})
	}
}

func TestExtractEpisodeInfo_AmbiguousReturnsNil(t *testing.T) {
	ambiguous := []struct {
		name  string
		title string
		desc  string
	}{
		{
			name:  "Slash number (#12/24)",
			title: "Dokumentation",
			desc:  "Teil #12/24 der Reihe.",
		},
		{
			name:  "Date string (12.05.2023)",
			title: "Gottesdienst",
			desc:  "Aufzeichnung vom 12.05.2023.",
		},
		{
			name:  "Time string (20:15)",
			title: "Primetime",
			desc:  "Start um 20:15 Uhr.",
		},
		{
			name:  "Empty text",
			title: "",
			desc:  "",
		},
	}

	for _, tt := range ambiguous {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractEpisodeInfo(tt.title, tt.desc)
			assert.Nil(t, got, "Expected nil for ambiguous episode pattern: %s", tt.name)
		})
	}
}

func TestExtractDVBGenre(t *testing.T) {
	// 1. DVB Content Descriptors (Table 28)
	g, src := ExtractDVBGenre(0x10, "")
	assert.Equal(t, "movie", g)
	assert.Equal(t, "dvb_descriptor", src)

	g, src = ExtractDVBGenre(0x20, "")
	assert.Equal(t, "news", g)
	assert.Equal(t, "dvb_descriptor", src)

	g, src = ExtractDVBGenre(0x40, "")
	assert.Equal(t, "sport", g)
	assert.Equal(t, "dvb_descriptor", src)

	g, src = ExtractDVBGenre(0x50, "")
	assert.Equal(t, "kids", g)
	assert.Equal(t, "dvb_descriptor", src)

	g, src = ExtractDVBGenre(0x90, "")
	assert.Equal(t, "docu", g)
	assert.Equal(t, "dvb_descriptor", src)

	// 2. Category Text Fallback
	g, src = ExtractDVBGenre(0, "Spielfilm")
	assert.Equal(t, "movie", g)
	assert.Equal(t, "dvb_category", src)

	g, src = ExtractDVBGenre(0, "Fernsehserie")
	assert.Equal(t, "series", g)
	assert.Equal(t, "dvb_category", src)

	g, src = ExtractDVBGenre(0, "Naturdokumentation")
	assert.Equal(t, "docu", g)
	assert.Equal(t, "dvb_category", src)

	// 3. Unknown
	g, src = ExtractDVBGenre(0, "Sonstiges")
	assert.Equal(t, "", g)
	assert.Equal(t, "", src)
}

func TestEnrichProgramme(t *testing.T) {
	prog := &Programme{
		Title:    Title{Text: "Babylon Berlin S03E01"},
		Desc:     &Description{Text: "Krimi im Berlin der 1920er Jahre. FSK: 12. Regie: Tom Tykwer."},
		Category: []string{"Krimiserie"},
	}

	EnrichProgramme(prog)

	require.NotNil(t, prog.Canonical)
	assert.Equal(t, "series", prog.Canonical.Genre)
	assert.Equal(t, "dvb_category", prog.Canonical.GenreSource)

	require.NotNil(t, prog.Canonical.AgeRating)
	assert.Equal(t, 12, prog.Canonical.AgeRating.Value)
	assert.Equal(t, "FSK", prog.Canonical.AgeRating.Scheme)
	assert.Equal(t, "DE", prog.Canonical.AgeRating.Country)
	assert.Equal(t, RatingConfidenceObserved, prog.Canonical.AgeRating.Confidence)

	require.NotNil(t, prog.Canonical.EpisodeInfo)
	assert.Equal(t, 3, prog.Canonical.EpisodeInfo.SeasonNumber)
	assert.Equal(t, 1, prog.Canonical.EpisodeInfo.EpisodeNumber)
	assert.Equal(t, "SxxExx", prog.Canonical.EpisodeInfo.SourcePattern)

	assert.Contains(t, prog.Canonical.Directors, "Tom Tykwer")
}
