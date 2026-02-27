package v3

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/control/recordings/decision"
	"github.com/stretchr/testify/assert"
)

func TestDerivePlaybackInfoMode(t *testing.T) {
	nativeOnly := &PlaybackCapabilities{
		HlsEngines: &[]PlaybackCapabilitiesHlsEngines{
			PlaybackCapabilitiesHlsEnginesNative,
		},
	}
	hlsjsOnly := &PlaybackCapabilities{
		HlsEngines: &[]PlaybackCapabilitiesHlsEngines{
			PlaybackCapabilitiesHlsEnginesHlsjs,
		},
	}
	both := &PlaybackCapabilities{
		HlsEngines: &[]PlaybackCapabilitiesHlsEngines{
			PlaybackCapabilitiesHlsEnginesNative,
			PlaybackCapabilitiesHlsEnginesHlsjs,
		},
	}
	noHints := &PlaybackCapabilities{
		CapabilitiesVersion: 1,
	}

	tests := []struct {
		name  string
		dec   *decision.Decision
		caps  *PlaybackCapabilities
		proto string
		want  PlaybackInfoMode
	}{
		{
			name:  "deny always deny",
			dec:   &decision.Decision{Mode: decision.ModeDeny},
			caps:  both,
			proto: "none",
			want:  PlaybackInfoModeDeny,
		},
		{
			name: "mp4 direct mode",
			dec: &decision.Decision{
				Mode:               decision.ModeDirectPlay,
				SelectedOutputKind: "file",
				Selected: decision.SelectedFormats{
					Container:  "mp4",
					VideoCodec: "h264",
					AudioCodec: "aac",
				},
			},
			caps:  both,
			proto: "mp4",
			want:  PlaybackInfoModeDirectMp4,
		},
		{
			name: "mp4 guard fails for unsupported video codec",
			dec: &decision.Decision{
				Mode:               decision.ModeDirectPlay,
				SelectedOutputKind: "file",
				Selected: decision.SelectedFormats{
					Container:  "mp4",
					VideoCodec: "hevc",
					AudioCodec: "aac",
				},
			},
			caps:  both,
			proto: "mp4",
			want:  PlaybackInfoModeDeny,
		},
		{
			name:  "direct stream native hls",
			dec:   &decision.Decision{Mode: decision.ModeDirectStream},
			caps:  nativeOnly,
			proto: "hls",
			want:  PlaybackInfoModeNativeHls,
		},
		{
			name:  "direct stream hlsjs",
			dec:   &decision.Decision{Mode: decision.ModeDirectStream},
			caps:  hlsjsOnly,
			proto: "hls",
			want:  PlaybackInfoModeHlsjs,
		},
		{
			name:  "transcode uses explicit transcode mode when hlsjs is available",
			dec:   &decision.Decision{Mode: decision.ModeTranscode},
			caps:  hlsjsOnly,
			proto: "hls",
			want:  PlaybackInfoModeTranscode,
		},
		{
			name:  "transcode falls back to native_hls if only native engine is available",
			dec:   &decision.Decision{Mode: decision.ModeTranscode},
			caps:  nativeOnly,
			proto: "hls",
			want:  PlaybackInfoModeNativeHls,
		},
		{
			name:  "legacy/no-hints defaults to hlsjs",
			dec:   &decision.Decision{Mode: decision.ModeDirectStream},
			caps:  nil,
			proto: "hls",
			want:  PlaybackInfoModeHlsjs,
		},
		{
			name:  "explicit caps without hls engine hints denies direct stream",
			dec:   &decision.Decision{Mode: decision.ModeDirectStream},
			caps:  noHints,
			proto: "hls",
			want:  PlaybackInfoModeDeny,
		},
		{
			name:  "explicit caps without hls engine hints keeps transcode explicit",
			dec:   &decision.Decision{Mode: decision.ModeTranscode},
			caps:  noHints,
			proto: "hls",
			want:  PlaybackInfoModeTranscode,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := derivePlaybackInfoMode(tc.dec, tc.caps, tc.proto)
			assert.Equal(t, tc.want, got)
		})
	}
}
