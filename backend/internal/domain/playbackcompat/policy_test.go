// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package playbackcompat

import (
	"slices"
	"testing"
)

func TestResolve_BrowserDolbyClaimsAreNotEffective(t *testing.T) {
	for _, tc := range []struct {
		name   string
		family string
		engine string
	}{
		{name: "desktop safari", family: "safari_native", engine: "native"},
		{name: "ios safari", family: "ios_safari_native", engine: "native"},
		{name: "firefox", family: "firefox_hlsjs", engine: "hlsjs"},
		{name: "chromium", family: "chromium_hlsjs", engine: "hlsjs"},
		{name: "android tv browser", family: "android_tv_browser", engine: "hlsjs"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resolved := Resolve(Claims{
				Scope:           "live",
				Family:          tc.family,
				PreferredEngine: tc.engine,
				AudioCodecs:     []string{"aac", "AC-3", "ec-3"},
			})
			if !slices.Equal(resolved.Raw.AudioCodecs, []string{"aac", "ac-3", "ec-3"}) {
				t.Fatalf("raw claims changed: %#v", resolved.Raw.AudioCodecs)
			}
			if !slices.Equal(resolved.Effective.AudioCodecs, []string{"aac"}) {
				t.Fatalf("unexpected effective audio codecs: %#v", resolved.Effective.AudioCodecs)
			}
			if len(resolved.Adjustments) != 2 {
				t.Fatalf("expected two policy adjustments, got %#v", resolved.Adjustments)
			}
		})
	}
}

func TestResolve_NativeAppDolbyClaimRemainsEffective(t *testing.T) {
	for _, family := range []string{"android_native", "android_tv_native", "ios_native"} {
		t.Run(family, func(t *testing.T) {
			resolved := Resolve(Claims{
				Scope:           "live",
				Family:          family,
				PreferredEngine: "native_app",
				AudioCodecs:     []string{"aac", "ac3"},
			})
			if !slices.Equal(resolved.Effective.AudioCodecs, []string{"aac", "ac3"}) {
				t.Fatalf("native app claim was narrowed: %#v", resolved.Effective.AudioCodecs)
			}
			if len(resolved.Adjustments) != 0 {
				t.Fatalf("unexpected native app adjustments: %#v", resolved.Adjustments)
			}
		})
	}
}

func TestResolve_NeverWidensClaimSets(t *testing.T) {
	raw := Claims{
		Family:          "safari_native",
		PreferredEngine: "native",
		Containers:      []string{"mpegts", "mp4"},
		VideoCodecs:     []string{"hevc", "h264"},
		AudioCodecs:     []string{"aac", "ac3"},
	}
	resolved := Resolve(raw)
	for name, pair := range map[string][2][]string{
		"containers": {resolved.Raw.Containers, resolved.Effective.Containers},
		"video":      {resolved.Raw.VideoCodecs, resolved.Effective.VideoCodecs},
		"audio":      {resolved.Raw.AudioCodecs, resolved.Effective.AudioCodecs},
	} {
		for _, effective := range pair[1] {
			if !slices.Contains(pair[0], effective) {
				t.Fatalf("%s capability %q was added by policy: raw=%#v effective=%#v", name, effective, pair[0], pair[1])
			}
		}
	}
}
