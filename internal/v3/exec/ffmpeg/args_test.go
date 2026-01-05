// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ffmpeg

import (
	"strings"
	"testing"

	"github.com/ManuGH/xg2g/internal/v3/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildHLSArgs(t *testing.T) {
	in := InputSpec{StreamURL: "http://127.0.0.1:8001/1:0:1..."}
	out := OutputSpec{
		HLSPlaylist:        "/tmp/sess/index.m3u8",
		SegmentFilename:    "/tmp/sess/seg_%06d.ts",
		SegmentDuration:    6,
		PlaylistWindowSize: 5,
	}
	prof := model.ProfileSpec{Name: "default"}

	args, err := BuildHLSArgs(in, out, prof)
	require.NoError(t, err)

	str := strings.Join(args, " ")
	assert.Contains(t, str, "-i http://127.0.0.1:8001/1:0:1...")
	assert.Contains(t, str, "/tmp/sess/index.m3u8")
	assert.Contains(t, str, "-hls_time 6")
	assert.Contains(t, str, "-hls_list_size 5")
	assert.Contains(t, str, "-hls_segment_filename /tmp/sess/seg_%06d.ts")
	assert.Contains(t, str, "-hls_flags append_list+omit_endlist+temp_file")
}

func TestBuildHLSArgs_MissingInput(t *testing.T) {
	_, err := BuildHLSArgs(InputSpec{}, OutputSpec{HLSPlaylist: "foo"}, model.ProfileSpec{})
	assert.Error(t, err)
}

func TestBuildHLSArgs_HEVC(t *testing.T) {
	in := InputSpec{StreamURL: "http://127.0.0.1:8001/1:0:1..."}
	out := OutputSpec{
		HLSPlaylist:        "/tmp/sess/index.m3u8",
		SegmentFilename:    "/tmp/sess/seg_%06d.ts", // Should be coerced to .m4s or fMP4 logic
		SegmentDuration:    6,
		PlaylistWindowSize: 5,
	}

	prof := model.ProfileSpec{
		Name:           "safari_hevc",
		TranscodeVideo: true,
		VideoCodec:     "hevc",
		VideoCRF:       22,
		VideoMaxRateK:  5000,
		VideoBufSizeK:  10000,
		BFrames:        2,
		Deinterlace:    true,
	}

	args, err := BuildHLSArgs(in, out, prof)
	require.NoError(t, err)

	str := strings.Join(args, " ")

	// Codec & Tag
	assert.Contains(t, str, "-c:v libx265")
	assert.Contains(t, str, "-tag:v hvc1")

	// fMP4 Container
	assert.Contains(t, str, "-hls_segment_type fmp4")
	assert.Contains(t, str, "-hls_fmp4_init_filename init.mp4")
	assert.Contains(t, str, "/tmp/sess/seg_%06d.m4s") // Auto-fixed extension

	// x265 strict params
	assert.Contains(t, str, "-x265-params")
	assert.Contains(t, str, "vbv-maxrate=5000:vbv-bufsize=10000") // VBV Discipline
	assert.Contains(t, str, "scenecut=0")                         // GOP Discipline
	assert.Contains(t, str, "keyint=")                            // GOP Discipline

	// Independent segments
	assert.Contains(t, str, "independent_segments")
}
