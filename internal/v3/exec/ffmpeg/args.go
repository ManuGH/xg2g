// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ffmpeg

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ManuGH/xg2g/internal/v3/model"
)

// InputSpec defines the source stream parameters.
type InputSpec struct {
	StreamURL        string
	RealtimeThrottle bool   // Add -re input flag (read at native rate)
	StartOffset      string // Optional: Seek offset (e.g. "00:01:30" or seconds)
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

	audioBitrate := "384k"
	if prof.AudioBitrateK > 0 {
		audioBitrate = fmt.Sprintf("%dk", prof.AudioBitrateK)
	}
	audioFilter := "aresample=async=1:first_pts=0,aformat=channel_layouts=5.1|stereo"
	if prof.AudioBitrateK > 0 && prof.AudioBitrateK <= 192 {
		audioFilter = "aresample=async=1:first_pts=0,aformat=channel_layouts=stereo"
	}

	args := []string{
		"-hide_banner",
		"-loglevel", "error", // We capture stderr
		"-nostats",

		// Input robustness flags for unreliable/noisy streams
		"-fflags", "+genpts+nobuffer", // Generate missing PTS, no input buffering
		"-err_detect", "ignore_err", // Ignore decoder errors
		"-analyzeduration", "2000000", // 2s to receive H.264 PPS/SPS headers
		"-probesize", "5000000", // 5MB probe buffer for codec init
		"-max_delay", "0", // No demux delay
	}

	// HTTP input options - only apply if NOT using a pipe
	if strings.HasPrefix(in.StreamURL, "http") {
		args = append(args,
			"-user_agent", "curl/8.14.1", // Identify as curl for compatibility (VLC blocked)
			"-headers", "Icy-MetaData: 1", // Request Icecast metadata
			"-reconnect", "1", // Enable automatic reconnection
			"-reconnect_streamed", "1", // Reconnect even for streamed protocols
			"-reconnect_delay_max", "5", // Max 5s between reconnect attempts
			"-timeout", "10000000", // 10s timeout (in microseconds)
		)
	}

	if in.RealtimeThrottle {
		args = append(args, "-re")
	}

	if in.StartOffset != "" {
		args = append(args, "-ss", in.StartOffset)
	}

	args = append(args,
		// Input
		"-i", in.StreamURL,

		// Map video and first audio stream only
		// Safari has issues with multiple audio streams and unknown channel layouts
		"-map", "0:v:0?", // First video stream if present
		"-map", "0:a:0?", // First audio stream only

		// Video:
	)

	// Video Configuration
	videoCodec := "copy"
	if prof.TranscodeVideo {
		videoCodec = "libx264"
	}
	args = append(args, "-c:v", videoCodec)

	if prof.TranscodeVideo {
		args = append(args, "-pix_fmt", "yuv420p") // CRITICAL: Safari compatibility - ensure YUV 4:2:0
	}

	args = append(args,

		// Audio: Re-encode to AAC for best compatibility and quality
		// DVB/satellite streams often have incomplete codec parameters that fail with copy
		// Using high-quality AAC settings for best audio quality:
		//
		// CRITICAL: Safari requires explicit channel metadata in output
		// We explicitly set channel layout to prevent "unknown" layout issues
		// aformat ensures proper channel layout metadata is written to output
		"-filter:a", audioFilter,
		"-c:a", "aac",
		"-b:a", audioBitrate,
		"-ar", "48000", // Force 48kHz sample rate
		"-profile:a", "aac_low", // AAC-LC profile for best compatibility

		// HLS Output Options
		"-f", "hls",
		"-hls_time", strconv.Itoa(out.SegmentDuration),
		"-hls_list_size", strconv.Itoa(out.PlaylistWindowSize),

		// Segment naming
		"-hls_segment_filename", out.SegmentFilename,
	)

	// HLS Flags - DVR/VOD vs Live
	// DVR: Keep segments, no temp_file (Safari seeking needs stable files)
	// Live: Delete old segments to save disk
	hlsFlags := "delete_segments+omit_endlist+temp_file"

	if prof.VOD {
		// VOD:
		// - Keep ALL segments (-hls_list_size 0)
		// - Allow ENDLIST tag (do not omit) so player sees it as finite
		hlsFlags = "temp_file+independent_segments+program_date_time"
	} else if prof.DVRWindowSec > 0 {
		// Rolling DVR mode: NO delete_segments, NO temp_file for stable seeking
		// CRITICAL Safari DVR flags (based on Apple HLS spec):
		// - append_list: Required for EVENT playlists to properly append segments
		// - independent_segments: Indicates keyframe-aligned segments for seeking
		// - program_date_time: Adds absolute timestamps for accurate DVR navigation
		hlsFlags = "omit_endlist+append_list+independent_segments+program_date_time"
	} else if prof.LLHLS {
		hlsFlags = hlsFlags + "+independent_segments"
	}
	args = append(args, "-hls_flags", hlsFlags)

	if prof.TranscodeVideo {
		crf := prof.VideoCRF
		if crf == 0 {
			crf = 23
		}
		gop := out.SegmentDuration * 25
		if gop <= 0 {
			gop = 50
		}
		keyframeInterval := strconv.FormatFloat(float64(out.SegmentDuration), 'f', 3, 64)
		if keyframeInterval == "0.000" {
			keyframeInterval = "2.000"
		}

		args = append(args,
			"-preset", "faster", // Better quality than veryfast, still efficient
			"-profile:v", "high",
			"-level", "4.0",
			"-crf", strconv.Itoa(crf),
			"-g", strconv.Itoa(gop),
			"-keyint_min", strconv.Itoa(gop),
			"-sc_threshold", "0",
			"-force_key_frames", fmt.Sprintf("expr:gte(t,n_forced*%s)", keyframeInterval),
		)

		// Video Filters (Deinterlacing + Scaling)
		var filters []string

		// Always deinterlace DVB content (yadif=1: output one frame per field aka 50i->50p)
		// This preserves temporal resolution (smooth motion) which is critical for TV/Sports.
		filters = append(filters, "yadif=1:-1:0")

		if prof.VideoMaxWidth > 0 {
			filters = append(filters, fmt.Sprintf("scale=w=%d:h=-2:flags=lanczos", prof.VideoMaxWidth))
		}

		if len(filters) > 0 {
			args = append(args, "-vf", strings.Join(filters, ","))
		}
	}

	// Playlist type:
	// - DVR (timeshift): EVENT
	// - Recording/VOD: VOD
	if prof.VOD {
		args = append(args, "-hls_playlist_type", "vod")
	} else if prof.DVRWindowSec > 0 {
		args = append(args, "-hls_playlist_type", "event")
	}

	// fMP4 Special Handling
	if out.InitFilename != "" {
		args = append(args,
			"-hls_segment_type", "fmp4",
			"-hls_fmp4_init_filename", out.InitFilename,
		)
	}
	if prof.LLHLS {
		partSize := float64(out.SegmentDuration) / 4.0
		if partSize <= 0 {
			partSize = 0.5
		}
		args = append(args, "-hls_part_size", strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.3f", partSize), "0"), "."))
	}

	args = append(args, out.HLSPlaylist)
	return args, nil
}
