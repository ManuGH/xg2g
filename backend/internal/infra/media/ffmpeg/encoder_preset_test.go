package ffmpeg

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEncoderPreset_SVTAV1RejectsNames(t *testing.T) {
	// The names the CPU path shares with libx264 must become numbers, or libsvtav1
	// fails to open and the session dies during startup with exit 234.
	for name, want := range map[string]string{
		"ultrafast": "12",
		"superfast": "11",
		"veryfast":  "10",
		"medium":    "7",
		"veryslow":  "4",
		"SuperFast": "11", // case-insensitive: profiles are operator-supplied
	} {
		assert.Equal(t, want, encoderPreset("libsvtav1", name), "preset %q", name)
	}

	// Numeric presets pass through so an operator can pin an exact speed level.
	assert.Equal(t, "6", encoderPreset("libsvtav1", "6"))
	assert.Equal(t, "0", encoderPreset("libsvtav1", "0"))

	// Anything unrecognized must still be a valid SVT-AV1 value, never a name.
	assert.Equal(t, svtAV1DefaultPreset, encoderPreset("libsvtav1", "turbo"))
	assert.Equal(t, svtAV1DefaultPreset, encoderPreset("libsvtav1", ""))
}

func TestEncoderPreset_OtherEncodersKeepNames(t *testing.T) {
	for _, codec := range []string{"libx264", "libx265", "h264_vaapi", "av1_vaapi", ""} {
		assert.Equal(t, "veryfast", encoderPreset(codec, "veryfast"), "codec %q", codec)
	}
}
