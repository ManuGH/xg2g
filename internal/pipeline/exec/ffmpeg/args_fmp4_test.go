package ffmpeg

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildHLSArgs_FMP4(t *testing.T) {
	in := InputSpec{StreamURL: "http://test:8080/123"}
	out := OutputSpec{
		HLSPlaylist:        "/tmp/index.m3u8",
		SegmentFilename:    "/tmp/seg_%06d.m4s",
		SegmentDuration:    6,
		PlaylistWindowSize: 5,
		InitFilename:       "/tmp/init.mp4", // This triggers fMP4 logic
	}
	prof := model.ProfileSpec{Name: "safari_dvr", LLHLS: true}

	args, err := BuildHLSArgs(in, out, prof)
	require.NoError(t, err)

	// Verify Flags
	assert.Contains(t, args, "-hls_segment_type")
	assert.Contains(t, args, "fmp4")
	assert.Contains(t, args, "-hls_fmp4_init_filename")
	assert.Contains(t, args, "/tmp/init.mp4")

	// Ensure Extension logic used correctly
	// args should have segment filename with .m4s
	assert.Contains(t, args, "/tmp/seg_%06d.m4s")
}

func TestBuildHLSArgs_Legacy(t *testing.T) {
	in := InputSpec{StreamURL: "http://test:8080/123"}
	out := OutputSpec{
		HLSPlaylist:        "/tmp/index.m3u8",
		SegmentFilename:    "/tmp/seg_%06d.ts",
		SegmentDuration:    6,
		PlaylistWindowSize: 5,
		// InitFilename empty
	}
	prof := model.ProfileSpec{Name: "standard"}

	args, err := BuildHLSArgs(in, out, prof)
	require.NoError(t, err)

	// Verify absence of fMP4 flags
	assert.NotContains(t, args, "-hls_segment_type")
	assert.NotContains(t, args, "fmp4")
}
