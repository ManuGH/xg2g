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

		// Input robustness flags for unreliable/noisy streams
		"-fflags", "+genpts+nobuffer",  // Generate missing PTS, no input buffering
		"-err_detect", "ignore_err",    // Ignore decoder errors
		"-analyzeduration", "2000000",  // 2s to receive H.264 PPS/SPS headers
		"-probesize", "5000000",        // 5MB probe buffer for codec init
		"-max_delay", "0",              // No demux delay

		// HTTP input options for Enigma2/DVB receivers (VLC-compatible)
		// CRITICAL: Enigma2 receivers require VLC-compatible HTTP headers for reliable streaming
		// Analysis of VLC showed it uses HTTP/1.0 with specific User-Agent and Icy-MetaData headers
		// Without these headers, FFmpeg gets "Stream ends prematurely" errors from the receiver
		"-user_agent", "VLC/3.0.21 LibVLC/3.0.21",  // Identify as VLC for compatibility
		"-headers", "Icy-MetaData: 1",              // Request Icecast metadata
		"-reconnect", "1",                          // Enable automatic reconnection
		"-reconnect_streamed", "1",                 // Reconnect even for streamed protocols
		"-reconnect_delay_max", "5",                // Max 5s between reconnect attempts
		"-timeout", "10000000",                     // 10s timeout (in microseconds)

		// Input
		"-i", in.StreamURL,

		// Map all streams (basic pass-through logic for now, profile allows overrides)
		"-map", "0:v?", // Video if present
		"-map", "0:a?", // Audio if present

		// Video: copy stream as-is
		"-c:v", "copy",

		// Audio: Re-encode to AAC for best compatibility and quality
		// DVB/satellite streams often have incomplete codec parameters that fail with copy
		// Using high-quality AAC settings for best audio quality:
		//
		// CRITICAL: Safari requires explicit channel metadata in output
		// We use aresample to properly copy channel layout from source and set metadata
		// This handles dynamic changes (5.1 during movies, stereo during ads)
		// Safari on macOS supports AAC 5.1 surround natively
		"-filter:a", "aresample=async=1:first_pts=0",  // Properly handle channel layout
		"-c:a", "aac",
		"-b:a", "384k",  // High bitrate works for both stereo and 5.1 (auto-adjusts)
		"-ar", "48000",  // Force 48kHz sample rate
		"-profile:a", "aac_low",  // AAC-LC profile for best compatibility

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
