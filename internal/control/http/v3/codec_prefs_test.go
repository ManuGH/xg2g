package v3

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
	"github.com/stretchr/testify/require"
)

func TestParseCodecList(t *testing.T) {
	t.Parallel()

	require.Equal(t, []string(nil), parseCodecList(""))
	require.Equal(t, []string{"av1", "hevc", "h264"}, parseCodecList("av1,hevc,h264"))
	require.Equal(t, []string{"hevc", "h264"}, parseCodecList("h265; avc"))
	require.Equal(t, []string{"av1"}, parseCodecList("av01"))
	require.Equal(t, []string{"h264"}, parseCodecList("AVC1"))
	require.Equal(t, []string{"av1", "h264"}, parseCodecList("av1,unknown,h264"))
}

func TestPickProfileForCodecs(t *testing.T) {
	t.Parallel()

	// AV1 only when GPU is available + hwaccel not off.
	require.Equal(t, profiles.ProfileAV1HW, pickProfileForCodecs("av1,hevc,h264", true, profiles.HWAccelAuto))
	require.Equal(t, profiles.ProfileSafariHEVCHW, pickProfileForCodecs("av1,hevc,h264", false, profiles.HWAccelAuto))
	require.Equal(t, profiles.ProfileSafariHEVCHW, pickProfileForCodecs("av1,hevc,h264", true, profiles.HWAccelOff))

	// HEVC wins when listed first.
	require.Equal(t, profiles.ProfileSafariHEVCHW, pickProfileForCodecs("hevc,h264", true, profiles.HWAccelAuto))

	// H264 falls back.
	require.Equal(t, profiles.ProfileSafari, pickProfileForCodecs("h264", true, profiles.HWAccelAuto))
}

