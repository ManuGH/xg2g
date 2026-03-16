// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package playbackprofile

import (
	"reflect"
	"testing"
)

func TestClientFixtureIDs_AreStable(t *testing.T) {
	got := ClientFixtureIDs()
	want := []string{
		ClientSafariNative,
		ClientIOSSafariNative,
		ClientFirefoxHLSJS,
		ClientChromiumHLSJS,
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected client fixture ids: got=%#v want=%#v", got, want)
	}

	got[0] = "mutated"
	if again := ClientFixtureIDs(); !reflect.DeepEqual(again, want) {
		t.Fatalf("client fixture ids must return a defensive copy: got=%#v want=%#v", again, want)
	}
}

func TestClientFixture_ReturnsCanonicalProfiles(t *testing.T) {
	cases := []struct {
		id               string
		wantDeviceType   string
		wantEngine       string
		wantVideoCodecs  []string
		wantAudioCodecs  []string
		wantHLSPackaging []string
	}{
		{
			id:               ClientSafariNative,
			wantDeviceType:   "safari",
			wantEngine:       "native_hls",
			wantVideoCodecs:  []string{"h264", "hevc"},
			wantAudioCodecs:  []string{"aac", "ac3", "mp3"},
			wantHLSPackaging: []string{"fmp4", "ts"},
		},
		{
			id:               ClientIOSSafariNative,
			wantDeviceType:   "ios_safari",
			wantEngine:       "native_hls",
			wantVideoCodecs:  []string{"h264", "hevc"},
			wantAudioCodecs:  []string{"aac", "ac3", "mp3"},
			wantHLSPackaging: []string{"fmp4", "ts"},
		},
		{
			id:               ClientFirefoxHLSJS,
			wantDeviceType:   "firefox",
			wantEngine:       "hls_js",
			wantVideoCodecs:  []string{"h264"},
			wantAudioCodecs:  []string{"aac", "mp3"},
			wantHLSPackaging: []string{"fmp4", "ts"},
		},
		{
			id:               ClientChromiumHLSJS,
			wantDeviceType:   "chromium",
			wantEngine:       "hls_js",
			wantVideoCodecs:  []string{"h264"},
			wantAudioCodecs:  []string{"aac", "mp3"},
			wantHLSPackaging: []string{"fmp4", "ts"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			got, ok := ClientFixture(tc.id)
			if !ok {
				t.Fatalf("expected client fixture %q", tc.id)
			}
			if !reflect.DeepEqual(got, CanonicalizeClient(got)) {
				t.Fatalf("client fixture %q must already be canonical: %#v", tc.id, got)
			}
			if got.DeviceType != tc.wantDeviceType {
				t.Fatalf("unexpected device type: got=%q want=%q", got.DeviceType, tc.wantDeviceType)
			}
			if got.PlaybackEngine != tc.wantEngine {
				t.Fatalf("unexpected playback engine: got=%q want=%q", got.PlaybackEngine, tc.wantEngine)
			}
			if !reflect.DeepEqual(got.VideoCodecs, tc.wantVideoCodecs) {
				t.Fatalf("unexpected video codecs: got=%#v want=%#v", got.VideoCodecs, tc.wantVideoCodecs)
			}
			if !reflect.DeepEqual(got.AudioCodecs, tc.wantAudioCodecs) {
				t.Fatalf("unexpected audio codecs: got=%#v want=%#v", got.AudioCodecs, tc.wantAudioCodecs)
			}
			if !reflect.DeepEqual(got.HLSPackaging, tc.wantHLSPackaging) {
				t.Fatalf("unexpected hls packaging: got=%#v want=%#v", got.HLSPackaging, tc.wantHLSPackaging)
			}
		})
	}
}

func TestClientFixture_SafariFamiliesRemainDistinct(t *testing.T) {
	safari := MustClientFixture(ClientSafariNative)
	ios := MustClientFixture(ClientIOSSafariNative)

	if safari.DeviceType == ios.DeviceType {
		t.Fatalf("desktop and ios safari fixtures must stay distinct: safari=%q ios=%q", safari.DeviceType, ios.DeviceType)
	}
	if safari.PlaybackEngine != "native_hls" || ios.PlaybackEngine != "native_hls" {
		t.Fatalf("expected native hls playback engine for safari families: safari=%q ios=%q", safari.PlaybackEngine, ios.PlaybackEngine)
	}
}

func TestSourceFixtureIDs_AreStable(t *testing.T) {
	got := SourceFixtureIDs()
	want := []string{
		SourceH264AAC,
		SourceH264AC3,
		SourceHEVCAAC,
		SourceMPEGTSInterlaced,
		SourceDVBDirty,
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected source fixture ids: got=%#v want=%#v", got, want)
	}

	got[0] = "mutated"
	if again := SourceFixtureIDs(); !reflect.DeepEqual(again, want) {
		t.Fatalf("source fixture ids must return a defensive copy: got=%#v want=%#v", again, want)
	}
}

func TestSourceFixture_ReturnsCanonicalProfiles(t *testing.T) {
	cases := []struct {
		id             string
		wantContainer  string
		wantVideoCodec string
		wantAudioCodec string
	}{
		{SourceH264AAC, "mp4", "h264", "aac"},
		{SourceH264AC3, "mpegts", "h264", "ac3"},
		{SourceHEVCAAC, "mp4", "hevc", "aac"},
		{SourceMPEGTSInterlaced, "mpegts", "h264", "aac"},
		{SourceDVBDirty, "mpegts", "h264", "aac"},
	}

	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			got, ok := SourceFixture(tc.id)
			if !ok {
				t.Fatalf("expected source fixture %q", tc.id)
			}
			if !reflect.DeepEqual(got, CanonicalizeSource(got)) {
				t.Fatalf("source fixture %q must already be canonical: %#v", tc.id, got)
			}
			if got.Container != tc.wantContainer || got.VideoCodec != tc.wantVideoCodec || got.AudioCodec != tc.wantAudioCodec {
				t.Fatalf("unexpected source fixture: got=%#v", got)
			}
		})
	}
}

func TestSourceFixture_DirtyDVBRemainsDirty(t *testing.T) {
	dirty := MustSourceFixture(SourceDVBDirty)

	if dirty.Width != 0 || dirty.Height != 0 || dirty.FPS != 0 {
		t.Fatalf("dirty dvb fixture must remain dimensionless: %#v", dirty)
	}
	if dirty.BitrateKbps != 0 || dirty.AudioChannels != 0 || dirty.AudioBitrateKbps != 0 {
		t.Fatalf("dirty dvb fixture must remain bitrate/channel-light: %#v", dirty)
	}
	if !dirty.Interlaced {
		t.Fatalf("dirty dvb fixture must stay interlaced: %#v", dirty)
	}
}

func TestMustFixtureHelpers_PanicOnUnknownIDs(t *testing.T) {
	assertPanics(t, func() {
		_ = MustClientFixture("nope")
	})
	assertPanics(t, func() {
		_ = MustSourceFixture("nope")
	})
}

func assertPanics(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	fn()
}
