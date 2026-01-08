// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ffmpeg

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDVR_SegmentDurationPolicy validates Patch 1: 6s default for DVR
func TestDVR_SegmentDurationPolicy(t *testing.T) {
	tests := []struct {
		name             string
		dvrWindowSec     int
		llhls            bool
		expectedSegDur   int
		expectedListSize int
	}{
		{
			name:             "Standard DVR 3h",
			dvrWindowSec:     10800,
			llhls:            false,
			expectedSegDur:   6,
			expectedListSize: 1800, // DVR keeps window segments (10800/6)
		},
		{
			name:             "DVR minimum 30min",
			dvrWindowSec:     1800,
			llhls:            false,
			expectedSegDur:   6,
			expectedListSize: 300, // DVR keeps window segments (1800/6)
		},
		{
			name:             "LL-HLS DVR",
			dvrWindowSec:     10800,
			llhls:            true,
			expectedSegDur:   4,
			expectedListSize: 2700, // DVR keeps window segments (10800/4)
		},
		{
			name:             "No DVR (Live only)",
			dvrWindowSec:     0,
			llhls:            false,
			expectedSegDur:   6,
			expectedListSize: 3, // Minimum default
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := InputSpec{StreamURL: "http://test:8080/stream"}
			out := OutputSpec{
				HLSPlaylist:        "/tmp/index.m3u8",
				SegmentFilename:    "/tmp/seg_%06d.m4s",
				SegmentDuration:    tt.expectedSegDur,
				PlaylistWindowSize: tt.expectedListSize,
			}
			prof := model.ProfileSpec{
				DVRWindowSec: tt.dvrWindowSec,
				LLHLS:        tt.llhls,
				Container:    "fmp4",
			}

			args, err := BuildHLSArgs(in, out, prof)
			require.NoError(t, err)

			// Verify -hls_time
			hlsTimeIdx := indexOfArg(args, "-hls_time")
			require.NotEqual(t, -1, hlsTimeIdx, "Missing -hls_time")
			assert.Equal(t, intArg(t, args, hlsTimeIdx+1), tt.expectedSegDur)

			// Verify -hls_list_size
			listSizeIdx := indexOfArg(args, "-hls_list_size")
			require.NotEqual(t, -1, listSizeIdx, "Missing -hls_list_size")
			assert.Equal(t, intArg(t, args, listSizeIdx+1), tt.expectedListSize)
		})
	}
}

// TestDVR_PlaylistType validates EVENT vs VOD playlist types
func TestDVR_PlaylistType(t *testing.T) {
	tests := []struct {
		name            string
		dvrWindowSec    int
		vod             bool
		expectedType    string
		expectedFlags   []string
		unexpectedFlags []string
	}{
		{
			name:            "DVR Live (EVENT)",
			dvrWindowSec:    10800,
			vod:             false,
			expectedType:    "",                                                                       // Standard Live for Sliding Window
			expectedFlags:   []string{"program_date_time", "independent_segments", "delete_segments"}, // Segments deleted for retention
			unexpectedFlags: []string{},
		},
		{
			name:            "VOD Recording",
			dvrWindowSec:    0,
			vod:             true,
			expectedType:    "vod",
			expectedFlags:   []string{"independent_segments"},
			unexpectedFlags: []string{"delete_segments", "omit_endlist"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := InputSpec{StreamURL: "http://test:8080/stream"}
			out := OutputSpec{
				HLSPlaylist:        "/tmp/index.m3u8",
				SegmentFilename:    "/tmp/seg_%06d.m4s",
				SegmentDuration:    6,
				PlaylistWindowSize: 1800,
			}
			prof := model.ProfileSpec{
				DVRWindowSec: tt.dvrWindowSec,
				VOD:          tt.vod,
				Container:    "fmp4",
			}

			args, err := BuildHLSArgs(in, out, prof)
			require.NoError(t, err)

			// Verify -hls_playlist_type
			typeIdx := indexOfArg(args, "-hls_playlist_type")
			if tt.expectedType != "" {
				require.NotEqual(t, -1, typeIdx, "Missing -hls_playlist_type")
				assert.Equal(t, args[typeIdx+1], tt.expectedType)
			}

			// Verify -hls_flags
			flagsIdx := indexOfArg(args, "-hls_flags")
			require.NotEqual(t, -1, flagsIdx, "Missing -hls_flags")
			flags := args[flagsIdx+1]

			for _, expectedFlag := range tt.expectedFlags {
				assert.Contains(t, flags, expectedFlag, "Missing flag: %s", expectedFlag)
			}

			for _, unexpectedFlag := range tt.unexpectedFlags {
				assert.NotContains(t, flags, unexpectedFlag, "Unexpected flag: %s", unexpectedFlag)
			}
		})
	}
}

// TestDVR_SegmentDurationSafety validates safety defaults in args.go
func TestDVR_SegmentDurationSafety(t *testing.T) {
	t.Run("Zero segment duration fallback", func(t *testing.T) {
		in := InputSpec{StreamURL: "http://test:8080/stream"}
		out := OutputSpec{
			HLSPlaylist:        "/tmp/index.m3u8",
			SegmentFilename:    "/tmp/seg_%06d.m4s",
			SegmentDuration:    0, // Invalid - should fallback to 6
			PlaylistWindowSize: 0, // Invalid - should fallback to 3
		}
		prof := model.ProfileSpec{
			Container: "fmp4",
		}

		args, err := BuildHLSArgs(in, out, prof)
		require.NoError(t, err)

		// Verify safety fallback to 6s
		hlsTimeIdx := indexOfArg(args, "-hls_time")
		require.NotEqual(t, -1, hlsTimeIdx)
		assert.Equal(t, intArg(t, args, hlsTimeIdx+1), 6)

		// Verify safety fallback to 3 segments
		listSizeIdx := indexOfArg(args, "-hls_list_size")
		require.NotEqual(t, -1, listSizeIdx)
		assert.Equal(t, intArg(t, args, listSizeIdx+1), 3)
	})
}

// Helper: Find index of argument in args slice
func indexOfArg(args []string, target string) int {
	for i, arg := range args {
		if arg == target {
			return i
		}
	}
	return -1
}

// Helper: Parse integer argument
func intArg(t *testing.T, args []string, idx int) int {
	require.Less(t, idx, len(args), "Index out of bounds")
	var val int
	_, err := fmt.Sscanf(args[idx], "%d", &val)
	require.NoError(t, err, "Failed to parse int from: %s", args[idx])
	return val
}

// TestDeinterlace_ArgsPresent validates that yadif filter is present when Deinterlace=true
func TestDeinterlace_ArgsPresent(t *testing.T) {
	tests := []struct {
		name           string
		transcodeVideo bool
		deinterlace    bool
		expectYadif    bool
	}{
		{
			name:           "Transcode with Deinterlace",
			transcodeVideo: true,
			deinterlace:    true,
			expectYadif:    true,
		},
		{
			name:           "Transcode without Deinterlace",
			transcodeVideo: true,
			deinterlace:    false,
			expectYadif:    false,
		},
		{
			name:           "Remux (no transcode)",
			transcodeVideo: false,
			deinterlace:    false,
			expectYadif:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := InputSpec{StreamURL: "http://test:8080/stream"}
			out := OutputSpec{
				HLSPlaylist:     "/tmp/index.m3u8",
				SegmentFilename: "/tmp/seg_%06d.m4s",
				SegmentDuration: 6,
			}
			prof := model.ProfileSpec{
				TranscodeVideo: tt.transcodeVideo,
				Deinterlace:    tt.deinterlace,
				Container:      "fmp4",
			}

			args, err := BuildHLSArgs(in, out, prof)
			require.NoError(t, err)

			argsStr := strings.Join(args, " ")
			if tt.expectYadif {
				assert.Contains(t, argsStr, "yadif", "yadif filter MUST be present when Deinterlace=true")
			} else {
				assert.NotContains(t, argsStr, "yadif", "yadif filter should NOT be present")
			}
		})
	}
}
