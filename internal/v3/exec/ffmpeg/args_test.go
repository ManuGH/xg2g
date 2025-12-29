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
	assert.Contains(t, str, "-hls_flags delete_segments+append_list+omit_endlist+temp_file")
}

func TestBuildHLSArgs_MissingInput(t *testing.T) {
	_, err := BuildHLSArgs(InputSpec{}, OutputSpec{HLSPlaylist: "foo"}, model.ProfileSpec{})
	assert.Error(t, err)
}
