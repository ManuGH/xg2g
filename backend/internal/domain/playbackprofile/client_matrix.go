// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package playbackprofile

import "github.com/ManuGH/xg2g/internal/normalize"

const (
	ClientSafariNative     = "safari_native"
	ClientIOSSafariNative  = "ios_safari_native"
	ClientFirefoxHLSJS     = "firefox_hlsjs"
	ClientAndroidTVBrowser = "android_tv_browser"
	ClientChromiumHLSJS    = "chromium_hlsjs"
)

var clientFixtureOrder = []string{
	ClientSafariNative,
	ClientIOSSafariNative,
	ClientFirefoxHLSJS,
	ClientAndroidTVBrowser,
	ClientChromiumHLSJS,
}

var clientFixtures = map[string]ClientPlaybackProfile{
	ClientSafariNative: CanonicalizeClient(ClientPlaybackProfile{
		DeviceType:     "safari",
		PlaybackEngine: "native_hls",
		Containers:     []string{"mp4", "mpegts"},
		VideoCodecs:    []string{"h264", "hevc"},
		// Browser capability probes are claims, not verified decoder truth.
		// Dolby audio remains excluded until a family/device route has an
		// explicit verified rule in playbackcompat.
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
	ClientIOSSafariNative: CanonicalizeClient(ClientPlaybackProfile{
		DeviceType:     "ios_safari",
		PlaybackEngine: "native_hls",
		Containers:     []string{"mp4", "mpegts"},
		VideoCodecs:    []string{"h264", "hevc"},
		AudioCodecs:    []string{"aac", "mp3"},
		HLSPackaging:   []string{"fmp4", "ts"},
		SupportsHLS:    true,
		SupportsRange:  true,
		AllowTranscode: boolPtr(true),
		// Fallback only — the client now reports a probed maxVideo. Modern iPhones/
		// iPads HW-decode 4K HEVC (A10+), so the iOS default must match macOS at
		// 2160p; the old 1080p cap wrongly forced 4K-HEVC copies into transcode.
		MaxVideo: &VideoConstraints{
			Width:  3840,
			Height: 2160,
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
			Width:  1920,
			Height: 1080,
			FPS:    60,
		},
	}),
	ClientAndroidTVBrowser: CanonicalizeClient(ClientPlaybackProfile{
		DeviceType:     "android_tv",
		PlaybackEngine: "hls_js",
		Containers:     []string{"mp4", "mpegts"},
		VideoCodecs:    []string{"h264"},
		AudioCodecs:    []string{"aac", "mp3"},
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
			Width:  1920,
			Height: 1080,
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
