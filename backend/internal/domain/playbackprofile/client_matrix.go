// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package playbackprofile

import "github.com/ManuGH/xg2g/internal/normalize"

const (
	ClientSafariNative    = "safari_native"
	ClientIOSSafariNative = "ios_safari_native"
	ClientFirefoxHLSJS    = "firefox_hlsjs"
	ClientChromiumHLSJS   = "chromium_hlsjs"
)

var clientFixtureOrder = []string{
	ClientSafariNative,
	ClientIOSSafariNative,
	ClientFirefoxHLSJS,
	ClientChromiumHLSJS,
}

var clientFixtures = map[string]ClientPlaybackProfile{
	ClientSafariNative: CanonicalizeClient(ClientPlaybackProfile{
		DeviceType:     "safari",
		PlaybackEngine: "native_hls",
		Containers:     []string{"mp4", "mpegts"},
		VideoCodecs:    []string{"h264", "hevc"},
		AudioCodecs:    []string{"aac", "ac3", "mp3"},
		HLSPackaging:   []string{"fmp4", "ts"},
		SupportsHLS:    true,
		SupportsRange:  true,
		AllowTranscode: boolPtr(true),
		MaxVideo: &VideoConstraints{
			Width:  3840,
			Height: 2160,
			FPS:    60,
		},
	}),
	ClientIOSSafariNative: CanonicalizeClient(ClientPlaybackProfile{
		DeviceType:     "ios_safari",
		PlaybackEngine: "native_hls",
		Containers:     []string{"mp4", "mpegts"},
		VideoCodecs:    []string{"h264", "hevc"},
		AudioCodecs:    []string{"aac", "ac3", "mp3"},
		HLSPackaging:   []string{"fmp4", "ts"},
		SupportsHLS:    true,
		SupportsRange:  true,
		AllowTranscode: boolPtr(true),
		MaxVideo: &VideoConstraints{
			Width:  1920,
			Height: 1080,
			FPS:    60,
		},
	}),
	ClientFirefoxHLSJS: CanonicalizeClient(ClientPlaybackProfile{
		DeviceType:     "firefox",
		PlaybackEngine: "hls_js",
		Containers:     []string{"mp4", "mpegts"},
		VideoCodecs:    []string{"h264"},
		AudioCodecs:    []string{"aac", "mp3"},
		HLSPackaging:   []string{"fmp4", "ts"},
		SupportsHLS:    true,
		SupportsRange:  true,
		AllowTranscode: boolPtr(true),
		MaxVideo: &VideoConstraints{
			Width:  3840,
			Height: 2160,
			FPS:    60,
		},
	}),
	ClientChromiumHLSJS: CanonicalizeClient(ClientPlaybackProfile{
		DeviceType:     "chromium",
		PlaybackEngine: "hls_js",
		Containers:     []string{"mp4", "mpegts"},
		VideoCodecs:    []string{"h264"},
		AudioCodecs:    []string{"aac", "mp3"},
		HLSPackaging:   []string{"fmp4", "ts"},
		SupportsHLS:    true,
		SupportsRange:  true,
		AllowTranscode: boolPtr(true),
		MaxVideo: &VideoConstraints{
			Width:  3840,
			Height: 2160,
			FPS:    60,
		},
	}),
}

func ClientFixtureIDs() []string {
	return append([]string(nil), clientFixtureOrder...)
}

func ClientFixture(id string) (ClientPlaybackProfile, bool) {
	fixture, ok := clientFixtures[normalize.Token(id)]
	return fixture, ok
}

func MustClientFixture(id string) ClientPlaybackProfile {
	fixture, ok := ClientFixture(id)
	if !ok {
		panic("unknown client matrix fixture: " + id)
	}
	return fixture
}

func boolPtr(v bool) *bool {
	return &v
}
