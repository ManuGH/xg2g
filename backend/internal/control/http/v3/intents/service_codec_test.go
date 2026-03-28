package intents

import (
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/control/http/v3/autocodec"
	"github.com/ManuGH/xg2g/internal/pipeline/hardware"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
)

func TestPickProfileForCodecs_AutoUsesMeasuredHostRanking(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		raw          string
		hwaccel      profiles.HWAccelMode
		capabilities map[string]hardware.VAAPIEncoderCapability
		want         string
	}{
		{
			name:    "picks the fastest measured heavy codec instead of input order",
			raw:     "hevc,av1,h264",
			hwaccel: profiles.HWAccelAuto,
			capabilities: map[string]hardware.VAAPIEncoderCapability{
				"h264_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 90 * time.Millisecond},
				"hevc_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 70 * time.Millisecond},
				"av1_vaapi":  {Verified: true, AutoEligible: true, ProbeElapsed: 50 * time.Millisecond},
			},
			want: profiles.ProfileAV1HW,
		},
		{
			name:    "falls through to h264 when heavy codecs are not auto eligible",
			raw:     "hevc,h264",
			hwaccel: profiles.HWAccelAuto,
			capabilities: map[string]hardware.VAAPIEncoderCapability{
				"h264_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 90 * time.Millisecond},
				"hevc_vaapi": {Verified: true, AutoEligible: false, ProbeElapsed: 40 * time.Millisecond},
			},
			want: profiles.ProfileH264FMP4,
		},
		{
			name:    "does not auto-promote hevc cpu when only hevc is hinted",
			raw:     "hevc",
			hwaccel: profiles.HWAccelAuto,
			capabilities: map[string]hardware.VAAPIEncoderCapability{
				"hevc_vaapi": {Verified: true, AutoEligible: false, ProbeElapsed: 40 * time.Millisecond},
			},
			want: "",
		},
		{
			name:    "hwaccel off disables heavy hw auto-promotion",
			raw:     "av1,hevc,h264",
			hwaccel: profiles.HWAccelOff,
			capabilities: map[string]hardware.VAAPIEncoderCapability{
				"h264_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 90 * time.Millisecond},
				"hevc_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 40 * time.Millisecond},
				"av1_vaapi":  {Verified: true, AutoEligible: true, ProbeElapsed: 30 * time.Millisecond},
			},
			want: profiles.ProfileH264FMP4,
		},
		{
			name:    "uses h264 cpu fallback when it is the only acceptable codec",
			raw:     "h264",
			hwaccel: profiles.HWAccelAuto,
			want:    profiles.ProfileH264FMP4,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := autocodec.PickProfileForCodecsWithCapabilities(tt.raw, tt.hwaccel, tt.capabilities)
			if got != tt.want {
				t.Fatalf("pickProfileForCodecs(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}
