// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package ffmpeg

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/stretchr/testify/assert"
)

func TestAdaptiveQualityLadder_ScalesWithSourceHeight(t *testing.T) {
	cases := []struct {
		name     string
		codec    string
		height   int
		wantMaxK int
	}{
		{"unknown -> prior default (av1)", "av1", 0, 14000},
		{"unknown -> prior default (h264)", "h264", 0, 16000},
		{"SD 576 av1 is frugal", "av1", 576, 5000},
		{"SD 576 hevc", "hevc", 576, 6000},
		{"SD 576 h264 highest of the three", "h264", 576, 7000},
		{"720 av1", "av1", 720, 8000},
		{"1080 av1 keeps the high ceiling", "av1", 1080, 14000},
		{"1080 h264", "h264", 1080, 16000},
		{"UHD av1", "av1", 2160, 22000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			maxK, bufK := adaptiveQualityLadder(tc.codec, tc.height)
			assert.Equal(t, tc.wantMaxK, maxK)
			assert.Equal(t, tc.wantMaxK*2, bufK, "bufsize is 2x maxrate")
		})
	}
}

func TestAdaptiveQualityLadder_SDLessThanHD(t *testing.T) {
	for _, codec := range []string{"av1", "hevc", "h264"} {
		sd, _ := adaptiveQualityLadder(codec, 576)
		hd720, _ := adaptiveQualityLadder(codec, 720)
		hd1080, _ := adaptiveQualityLadder(codec, 1080)
		assert.Less(t, sd, hd720, "%s: SD must get less than 720", codec)
		assert.Less(t, hd720, hd1080, "%s: 720 must get less than 1080", codec)
	}
}

func TestAdaptiveTranscodeQualityBudget_UsesSourceHeight(t *testing.T) {
	// An AV1 SD profile must resolve to the frugal SD ceiling, not the flat default.
	budget, ok := adaptiveTranscodeQualityBudgetFor(ports.ProfileSpec{
		VideoCodec:        "av1",
		VideoSourceHeight: 576,
	})
	assert.True(t, ok)
	assert.Equal(t, 5000, budget.maxRateK)

	// Same codec at 1080 keeps the high ceiling.
	hd, ok := adaptiveTranscodeQualityBudgetFor(ports.ProfileSpec{
		VideoCodec:        "av1",
		VideoSourceHeight: 1080,
	})
	assert.True(t, ok)
	assert.Equal(t, 14000, hd.maxRateK)
}
