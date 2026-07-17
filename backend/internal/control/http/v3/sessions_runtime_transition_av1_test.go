package v3

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/control/recordings/runtimepolicy"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
	"github.com/stretchr/testify/require"
)

func TestSessionRuntimeProfileForStep_AV1UsesFMP4AndGPU(t *testing.T) {
	current := model.ProfileSpec{
		Name:           profiles.ProfileHigh,
		TranscodeVideo: true,
		VideoCodec:     "libx264",
		Container:      "mpegts",
		Deinterlace:    true,
	}

	next, ok := sessionRuntimeProfileForStepWithResolver(current, runtimepolicy.PlaybackStepAV11080p, profiles.Resolver{})
	require.True(t, ok)
	require.Equal(t, profiles.ProfileAV1HW, next.Name)
	require.True(t, next.TranscodeVideo)
	require.Equal(t, "av1", next.VideoCodec)
	require.Equal(t, "fmp4", next.Container)
	require.NotEmpty(t, next.HWAccel)
	require.True(t, next.Deinterlace)
}
