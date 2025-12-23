// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ffmpeg

import (
	"fmt"
	"strconv"

	"github.com/ManuGH/xg2g/internal/v3/model"
)

// InputSpec defines the source stream parameters.
type InputSpec struct {
	StreamURL string
}

// OutputSpec defines the destination paths.
type OutputSpec struct {
	HLSPlaylist        string // Final playlist path (index.m3u8)
	SegmentFilename    string // Segment pattern (seg_%06d.ts)
	SegmentDuration    int    // Target duration in seconds
	PlaylistWindowSize int    // Number of segments in playlist
	InitFilename       string // Path for init.mp4 (indicates fMP4 mode)
}

// BuildHLSArgs constructs the ffmpeg arguments for HLS transcoding.
// It enforces safe defaults and prevents command injection by avoiding shell usage.
func BuildHLSArgs(in InputSpec, out OutputSpec, prof model.ProfileSpec) ([]string, error) {
	if in.StreamURL == "" {
		return nil, fmt.Errorf("missing stream URL")
	}
	if out.HLSPlaylist == "" {
		return nil, fmt.Errorf("missing playlist path")
	}

	args := []string{
		"-hide_banner",
		"-loglevel", "error", // We capture stderr
		"-nostats",

		// Input
		"-i", in.StreamURL,

		// Map all streams (basic pass-through logic for now, profile allows overrides)
		"-map", "0:v?", // Video if present
		"-map", "0:a?", // Audio if present

		// Video Transcoding (Stub: copy for now unless profile says otherwise)
		"-c:v", "copy",
		"-c:a", "copy", // Stub: copy

		// HLS Output Options
		"-f", "hls",
		"-hls_time", strconv.Itoa(out.SegmentDuration),
		"-hls_list_size", strconv.Itoa(out.PlaylistWindowSize),

		// Segment naming
		"-hls_segment_filename", out.SegmentFilename,

		// Flags:
		// delete_segments: clean up old segments
		// append_list: no (we want rolling)
		// omit_endlist: yes (live stream)
		"-hls_flags", "delete_segments+omit_endlist+temp_file",
	}

	// fMP4 Special Handling
	if out.InitFilename != "" {
		args = append(args,
			"-hls_segment_type", "fmp4",
			"-hls_fmp4_init_filename", out.InitFilename,
			// independent_segments helps with seeking/scrubbing in some players, but uses more bits.
			// Safari usually fine without it if keys are present.
			// We skip it for now unless requested.
		)
	}

	args = append(args, out.HLSPlaylist)
	return args, nil
}
