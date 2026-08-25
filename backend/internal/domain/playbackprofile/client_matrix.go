// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package playbackprofile

import "github.com/ManuGH/xg2g/internal/normalize"

// The client families the policy matrix is keyed on.
//
// Browser and native are separate identities on every platform that has both.
// `ios_safari_native` used to be one identifier for two of them — Safari on
// iOS, and the native iOS app, which reported the same string — so the app got
// browser capability defaults and browser packaging policy. Splitting them is
// what lets each carry the profile that is true of it.
const (
	// Browser surfaces.
	ClientSafariNative     = "safari_native"      // Safari on macOS
	ClientIOSSafari        = "ios_safari"         // Safari (and every browser) on iOS/iPadOS
	ClientFirefoxHLSJS     = "firefox_hlsjs"      //
	ClientAndroidTVBrowser = "android_tv_browser" //
	ClientChromiumHLSJS    = "chromium_hlsjs"     //

	// Native surfaces: applications that enumerate their own decoders.
	ClientIOSNative       = "ios_native"
	ClientAppleTVNative   = "apple_tv_native"
	ClientAndroidNative   = "android_native"
	ClientAndroidTVNative = "android_tv_native"
)

var clientFixtureOrder = []string{
	ClientSafariNative,
	ClientIOSSafari,
	ClientIOSNative,
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
	ClientIOSSafari: CanonicalizeClient(ClientPlaybackProfile{
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
	// The native iOS app carries its own transport-stream demuxer and hardware
	// decoder, so mpegts is a first-class container for it rather than
	// something to remux away, and Dolby audio is real decoder truth rather
	// than a browser's claim.
	ClientIOSNative: CanonicalizeClient(ClientPlaybackProfile{
		DeviceType:     "ios_native",
		PlaybackEngine: "native_ts",
		Containers:     []string{"mp4", "mpegts"},
		VideoCodecs:    []string{"h264", "hevc", "mpeg2video"},
		AudioCodecs:    []string{"aac", "mp3", "ac3", "eac3"},
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
	// Android TV boxes decode 4K and pass Dolby through to a receiver; the
	// browser profile on the same hardware cannot, which is exactly why the two
	// are separate families.
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
