// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package ffmpeg

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

func TestCapAV1VideoRate(t *testing.T) {
	planner := ports.ProfileSpec{VideoMaxRateK: 32000, VideoBufSizeK: 64000}

	cases := []struct {
		name    string
		env     string
		codec   string
		profile ports.ProfileSpec
		want    ports.ProfileSpec
	}{
		{name: "unset keeps the planner ceiling", codec: "av1", profile: planner, want: planner},
		{name: "cap lowers maxrate and bufsize", env: "12000", codec: "av1", profile: planner,
			want: ports.ProfileSpec{VideoMaxRateK: 12000, VideoBufSizeK: 24000}},
		{name: "cap also bounds an explicit target", env: "12000", codec: "av1",
			profile: ports.ProfileSpec{VideoTargetRateK: 20000, VideoMaxRateK: 32000, VideoBufSizeK: 64000},
			want:    ports.ProfileSpec{VideoTargetRateK: 12000, VideoMaxRateK: 12000, VideoBufSizeK: 24000}},
		{name: "a smaller bufsize survives", env: "12000", codec: "av1",
			profile: ports.ProfileSpec{VideoMaxRateK: 32000, VideoBufSizeK: 16000},
			want:    ports.ProfileSpec{VideoMaxRateK: 12000, VideoBufSizeK: 16000}},
		{name: "never raises a lower ceiling", env: "12000", codec: "av1",
			profile: ports.ProfileSpec{VideoMaxRateK: 8000, VideoBufSizeK: 16000},
			want:    ports.ProfileSpec{VideoMaxRateK: 8000, VideoBufSizeK: 16000}},
		{name: "other codecs are untouched", env: "12000", codec: "hevc", profile: planner, want: planner},
		{name: "values below the floor clamp to it", env: "500", codec: "av1", profile: planner,
			want: ports.ProfileSpec{VideoMaxRateK: 2000, VideoBufSizeK: 4000}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("XG2G_AV1_MAXRATE_CAP_K", tc.env)
			adapter := &LocalAdapter{Logger: zerolog.Nop(), Config: LoadAdapterConfig("", "")}
			got := adapter.capAV1VideoRate(ports.StreamSpec{Profile: tc.profile}, tc.codec)
			assert.Equal(t, tc.want, got)
		})
	}
}

// The cap must reach the ffmpeg arguments through the live planner, not only
// when capAV1VideoRate is called directly.
func TestCapAV1VideoRate_AppliesInLivePlan(t *testing.T) {
	spec := ladderSpec("chrome")

	t.Setenv("XG2G_AV1_MAXRATE_CAP_K", "")
	uncapped, _ := valueAfter(buildLadderArgs(t, ladderOneTrack, spec), "-maxrate")
	assert.Equal(t, "32000k", uncapped, "without the cap the planner ceiling stays")

	t.Setenv("XG2G_AV1_MAXRATE_CAP_K", "12000")
	args := buildLadderArgs(t, ladderOneTrack, spec)
	maxrate, _ := valueAfter(args, "-maxrate")
	bufsize, _ := valueAfter(args, "-bufsize")
	assert.Equal(t, "12000k", maxrate)
	assert.Equal(t, "24000k", bufsize)
}

func TestCapAV1VideoRate_DrivesVaapiArgs(t *testing.T) {
	t.Setenv("XG2G_AV1_MAXRATE_CAP_K", "12000")
	cfg := withProbedQVBR(t, false)
	adapter := &LocalAdapter{Logger: zerolog.Nop(), Config: cfg}
	prof := adapter.capAV1VideoRate(ports.StreamSpec{Profile: ports.ProfileSpec{VideoMaxRateK: 32000, VideoBufSizeK: 64000}}, "av1")

	args := appendVaapiRateControlArgs(nil, prof, "av1", cfg)
	bv, _ := valueAfter(args, "-b:v")
	maxrate, _ := valueAfter(args, "-maxrate")
	bufsize, _ := valueAfter(args, "-bufsize")
	assert.Equal(t, "12000k", bv, "without verified QVBR b:v follows the capped ceiling")
	assert.Equal(t, "12000k", maxrate)
	assert.Equal(t, "24000k", bufsize)
}
