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
			want:  "breaking bad s02e05",
		},
		{
			input: "   Dokumentation:   Die Alpen  [Neu]   ",
			want:  "dokumentation: die alpen",
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := NormalizeTitle(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBuildFingerprint(t *testing.T) {
	ep := &epg.EpisodeInfo{
		SeasonNumber:  3,
		EpisodeNumber: 12,
		SourcePattern: "SxxExx",
	}

	fp := BuildFingerprint("Der Bergdoktor (HD)", 2021, ep, "series")

	assert.Equal(t, "der bergdoktor", fp.NormalizedTitle)
	assert.Equal(t, 2021, fp.Year)
	assert.Equal(t, 3, fp.Season)
	assert.Equal(t, 12, fp.Episode)
	assert.Equal(t, "series", fp.EventGenre)
	assert.Equal(t, epg.CurrentFingerprintVersion, fp.FingerprintVersion)

	key1 := fp.Key()
	assert.NotEmpty(t, key1)

	// Idempotency: same fingerprint must produce exact same Key
	fp2 := BuildFingerprint("Der Bergdoktor [HD]", 2021, ep, "series")
	assert.Equal(t, key1, fp2.Key())
}
