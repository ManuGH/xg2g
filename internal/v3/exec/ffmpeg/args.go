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
		"-hls_flags", "delete_segments+omit_endlist",

		// Atomic writes handled by wrapper usually, but ffmpeg supports -hls_flags temp_file
		// Checks 8-5b says: "Atomic playlist strategy: FFmpeg writes to index.m3u8.tmp always... On first valid moment, rename".
		// If we tell ffmpeg to write to .tmp, then WE have to rename it to final.
		// Alternatively, we let ffmpeg write to `index.m3u8` and use `-hls_flags temp_file` which writes to `index.m3u8.tmp` and renames atomically?
		// User requirement 8-5b: "FFmpeg writes to index.m3u8.tmp always... On first 'playlist is valid' moment, rename tmp -> final".
		// This implies the *first* rename is special (to signal READY), ensuring clients don't see empty file.
		// After that, updates should be atomic too.
		// `temp_file` flag in ffmpeg does atomic updates.
		// BUT the user wants manual control for the FIRST write?
		// "Preflight... On first 'playlist is valid' moment, rename tmp -> final".
		// This suggests we point ffmpeg at `index.m3u8.tmp` PERMANENTLY, and we symlink or copy or rename?
		// No, if ffmpeg writes to .tmp, subsequent updates overwrite .tmp.
		// If we use .tmp as the *target*, then we need to sync it to .m3u8.

		// "Atomic playlist strategy: FFmpeg writes to index.m3u8.tmp always... On first playlist is valid moment, rename tmp -> final... simplest: after Start, poll... rename".
		// This implies we want the "final" file to appear ONLY when valid.

		// So:
		// 1. FFmpeg output path = `index.m3u8.tmp`
		// 2. Worker polls `index.m3u8.tmp`.
		// 3. When valid, Worker renames `index.m3u8.tmp` -> `index.m3u8` (one-off)?
		// 4. Wait, if FFmpeg continues writing to `.tmp`, and we renamed it... FFmpeg will recreate `.tmp` on next segment.
		// 5. User says: "On first... rename... simplest: after Start poll... rename".
		// This suggests the "rename" is actually a "promote" operation.
		// If we rename the file, FFmpeg loses it (unless it keeps fd open, but hls muxer opens/closes).

		// BETTER INTERPRETATION:
		// FFmpeg target = `index.m3u8.tmp`.
		// Worker *links* or *atomic copies* or *renames* it to `index.m3u8`?
		// If we rename `index.m3u8.tmp` to `index.m3u8`, then `index.m3u8.tmp` is gone.
		// FFmpeg writes next update to `index.m3u8.tmp`.
		// We need to continuously move it?

		// User requirement 8-5b details: "Atomic playlist strategy: FFmpeg writes to index.m3u8.tmp always... On first 'playlist is valid' moment, rename tmp -> final".
		// This sounds like a One-Time latch for the "Serving" state?
		// But subsequent updates must also reach `index.m3u8`.
		// If we rename, it's moved.
		// Maybe the user implies:
		// - FFmpeg target = `index.m3u8` (final path)
		// - BUT we use `-hls_flags temp_file` (so it writes `.tmp` then renames).
		// - AND we wait for `index.m3u8` to appear?

		// Re-reading user: "FFmpeg writes to index.m3u8.tmp always... On first... rename tmp -> final".
		// This strongly implies we target .tmp.
		// If we target .tmp, we must continuously sync it to final?

		// "Atomic playlist output" in XG2G context usually means:
		// 1. FFmpeg writes to `playlist.m3u8` using atomic replacement (or `playlist.m3u8.tmp` -> `playlist.m3u8`).
		// 2. WE want to hide the file until it has segments.

		// Let's assume Standard HLS Atomic Write: use `-hls_flags temp_file`.
		// AND to handle the "First Moment", we target `index.m3u8`.
		// BUT we poll until it exists to mark READY.

		// Let's stick to simple args first in 8-5a.

		"-hls_flags", "delete_segments+omit_endlist+temp_file",

		out.HLSPlaylist,
	}

	return args, nil
}
