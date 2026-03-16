// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package playbackprofile

import "github.com/ManuGH/xg2g/internal/normalize"

const (
	SourceH264AAC          = "h264_aac"
	SourceH264AC3          = "h264_ac3"
	SourceHEVCAAC          = "hevc_aac"
	SourceMPEGTSInterlaced = "mpegts_interlaced"
	SourceDVBDirty         = "dvb_dirty"
)

var sourceFixtureOrder = []string{
	SourceH264AAC,
	SourceH264AC3,
	SourceHEVCAAC,
	SourceMPEGTSInterlaced,
	SourceDVBDirty,
}

var sourceFixtures = map[string]SourceProfile{
	SourceH264AAC: CanonicalizeSource(SourceProfile{
		Container:        "mp4",
		VideoCodec:       "h264",
		AudioCodec:       "aac",
		BitrateKbps:      8000,
		Width:            1920,
		Height:           1080,
		FPS:              25,
		AudioChannels:    2,
		AudioBitrateKbps: 256,
	}),
	SourceH264AC3: CanonicalizeSource(SourceProfile{
		Container:        "mpegts",
		VideoCodec:       "h264",
		AudioCodec:       "ac3",
		BitrateKbps:      10000,
		Width:            1920,
		Height:           1080,
		FPS:              25,
		AudioChannels:    6,
		AudioBitrateKbps: 448,
	}),
	SourceHEVCAAC: CanonicalizeSource(SourceProfile{
		Container:        "mp4",
		VideoCodec:       "hevc",
		AudioCodec:       "aac",
		BitrateKbps:      12000,
		Width:            1920,
		Height:           1080,
		FPS:              25,
		AudioChannels:    2,
		AudioBitrateKbps: 256,
	}),
	SourceMPEGTSInterlaced: CanonicalizeSource(SourceProfile{
		Container:        "mpegts",
		VideoCodec:       "h264",
		AudioCodec:       "aac",
		BitrateKbps:      4500,
		Width:            720,
		Height:           576,
		FPS:              25,
		Interlaced:       true,
		AudioChannels:    2,
		AudioBitrateKbps: 192,
	}),
	SourceDVBDirty: CanonicalizeSource(SourceProfile{
		Container:  "mpegts",
		VideoCodec: "h264",
		AudioCodec: "aac",
		Interlaced: true,
	}),
}

func SourceFixtureIDs() []string {
	return append([]string(nil), sourceFixtureOrder...)
}

func SourceFixture(id string) (SourceProfile, bool) {
	fixture, ok := sourceFixtures[normalize.Token(id)]
	return fixture, ok
}

func MustSourceFixture(id string) SourceProfile {
	fixture, ok := SourceFixture(id)
	if !ok {
		panic("unknown source matrix fixture: " + id)
	}
	return fixture
}
