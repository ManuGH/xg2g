package ffmpeg

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
)

func TestAppendLiveAudioArgsSupportsPlannerAudioOnlyTranscode(t *testing.T) {
	spec := ports.StreamSpec{Profile: model.ProfileSpec{
		TranscodeVideo: false,
		AudioMode:      "transcode",
		AudioCodec:     "aac",
		AudioBitrateK:  160,
	}}

	require.Equal(t,
		[]string{"-c:a", "aac", "-b:a", "160k", "-ac", "2", "-ar", "48000", "-sn"},
		appendLiveAudioArgs(nil, spec, 2),
	)
}

func TestAppendLiveAudioArgsPreservesLegacyCouplingWhenModeUnset(t *testing.T) {
	copySpec := ports.StreamSpec{Profile: model.ProfileSpec{TranscodeVideo: false}}
	transcodeSpec := ports.StreamSpec{Profile: model.ProfileSpec{TranscodeVideo: true}}

	require.Equal(t, []string{"-c:a", "copy", "-sn"}, appendLiveAudioArgs(nil, copySpec, 2))
	require.Equal(t,
		[]string{"-c:a", "aac", "-b:a", "192k", "-ac", "2", "-ar", "48000", "-sn"},
		appendLiveAudioArgs(nil, transcodeSpec, 2),
	)
}

func TestAppendLiveAudioArgsAllowsExplicitCopyDuringVideoTranscode(t *testing.T) {
	spec := ports.StreamSpec{Profile: model.ProfileSpec{
		TranscodeVideo: true,
		AudioMode:      "copy",
	}}

	require.Equal(t, []string{"-c:a", "copy", "-sn"}, appendLiveAudioArgs(nil, spec, 2))
}
