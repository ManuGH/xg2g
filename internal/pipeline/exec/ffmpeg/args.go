// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ffmpeg

import (
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/pipeline/model"
)

// InputSpec defines the source stream parameters.
type InputSpec struct {
	StreamURL                string
	RealtimeThrottle         bool    // Add -re input flag (read at native rate)
	StartOffset              string  // Optional: Seek offset (e.g. "00:01:30" or seconds)
	AvgFrameRate             float64 // Optional: input FPS if known
	AnalyzeDuration          string  // Optional: FFmpeg -analyzeduration (e.g. "10s", default: "2s")
	ProbeSize                string  // Optional: FFmpeg -probesize (e.g. "10M", default: "10M")
	UseWallclockAsTimestamps bool    // Experimental: FFmpeg -use_wallclock_as_timestamps 1
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

	// Force fMP4 for HEVC (Apple compatibility) or explicit container request
	if prof.VideoCodec == "hevc" || prof.Container == "fmp4" {
		if out.InitFilename == "" {
			out.InitFilename = "init.mp4"
		}
		// Deterministic extension override: force .m4s
		// Use filepath.Ext to be safe against dots in directory names
		ext := filepath.Ext(out.SegmentFilename)
		out.SegmentFilename = strings.TrimSuffix(out.SegmentFilename, ext) + ".m4s"
	}

	audioBitrate := "384k"
	if prof.AudioBitrateK > 0 {
		audioBitrate = fmt.Sprintf("%dk", prof.AudioBitrateK)
	}
	transcodeAudio := prof.AudioBitrateK > 0
	audioFilter := "aresample=async=1:first_pts=0,aformat=channel_layouts=stereo"
	if transcodeAudio && prof.AudioBitrateK <= 192 {
		audioFilter = "aresample=async=1:first_pts=0,aformat=channel_layouts=stereo"
	}

	args := []string{
		"-nostdin",
		"-hide_banner",
		"-loglevel", "error", // We capture stderr
		"-nostats",

		// Input robustness flags for unreliable/noisy sources (ADR-004)
		"-ignore_unknown",                                               // Ignore unknown stream types (decoder robustness)
		"-fflags", "+genpts+discardcorrupt+igndts+flush_packets+ignidx", // Generate PTS, discard corrupt, ignore DTS/Index, flush on segments
		"-err_detect", "ignore_err", // Ignore decoder errors at input level
		"-avoid_negative_ts", "make_zero", // Keep timestamps monotonically increasing for Safari
	}

	// Probing configuration (configurable for OSCam-emu relay compatibility)
	analyzeDur := in.AnalyzeDuration
	if analyzeDur == "" {
		analyzeDur = "2000000" // 2s default (microseconds)
	} else {
		analyzeDur = normalizeDuration(analyzeDur)
	}
	probeSize := in.ProbeSize
	if probeSize == "" {
		probeSize = "10000000" // 10MB default
	} else {
		probeSize = normalizeSize(probeSize)
	}
	args = append(args,
		"-analyzeduration", analyzeDur,
		"-probesize", probeSize,
		"-threads", "0", // Allow multi-threading
	)

	if in.UseWallclockAsTimestamps {
		// Experimental: Use with caution. Can cause jumps on source stall.
		args = append(args, "-use_wallclock_as_timestamps", "1")
	}

	// Hardware Acceleration Setup (must be before input)
	if prof.HWAccel == "vaapi" {
		args = append(args,
			"-init_hw_device", "vaapi=gpu:/dev/dri/renderD128",
			"-filter_hw_device", "gpu",
		)
	}

	// HTTP input options are handled by curl pipe in the runner.
	// We do not add ffmpeg-native http flags here to avoid confusion.

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
		if prof.HWAccel == "vaapi" {
			// GPU-accelerated encoding
			if prof.VideoCodec == "hevc" {
				videoCodec = "hevc_vaapi"
			} else {
				videoCodec = "h264_vaapi"
			}
		} else {
			// CPU encoding
			if prof.VideoCodec == "hevc" {
				videoCodec = "libx265"
			} else {
				videoCodec = "libx264"
			}
		}
	}
	args = append(args, "-c:v", videoCodec)
	if prof.VideoCodec == "hevc" {
		if prof.HWAccel == "vaapi" {
			// Prefer encoder-level AUD insertion for VAAPI output.
			args = append(args, "-aud", "1")
		} else {
			// Insert AUD NALs for Safari HEVC compatibility.
			args = append(args, "-bsf:v", "hevc_metadata=aud=insert")
		}
	}

	if prof.TranscodeVideo {
		if prof.HWAccel != "vaapi" {
			args = append(args, "-pix_fmt", "yuv420p") // CRITICAL: Safari compatibility - ensure YUV 4:2:0
		}
		if prof.VideoCodec == "hevc" {
			args = append(args, "-tag:v", "hvc1") // Apple compatibility
			// Ensure VPS/SPS/PPS are written into init.mp4 (Safari requires this with hvc1).
			args = append(args, "-flags", "+global_header")
			// Explicit level for Apple decoders (VAAPI uses numeric levels).
			args = append(args, "-level:v", "4")
			if prof.HWAccel == "vaapi" {
				args = append(args, "-profile:v", "main") // Safari prefers 8-bit HEVC Main
			}
		}
	}

	// Patch 1: Ensure segment duration has safe default (runner.go should pass this, but validate)
	segDur := out.SegmentDuration
	if segDur <= 0 {
		segDur = 6 // Default for DVR (matches runner.go policy)
	}

	// Patch 1: Playlist size calculation (runner.go calculates this, but we validate here)
	playlistSize := out.PlaylistWindowSize
	if prof.VOD {
		playlistSize = 0 // VOD keeps all segments
	} else if prof.DVRWindowSec > 0 {
		// LIVE-DVR: Sliding Window (Retention Policy)
		// We calculate the number of segments to keep based on the requested window.
		// hls_flags includes "delete_segments" to prune old files.
		playlistSize = prof.DVRWindowSec / segDur
		if playlistSize < 3 {
			playlistSize = 3 // Minimum safety
		}
	} else if playlistSize <= 0 {
		// Safety: If runner didn't calculate (shouldn't happen), use minimal default
		playlistSize = 3
	}

	args = append(args,
		// HLS Output Options
		"-f", "hls",
		"-hls_time", strconv.Itoa(segDur),
		"-hls_list_size", strconv.Itoa(playlistSize),

		// Segment naming
		"-hls_segment_filename", out.SegmentFilename,
	)

	// MPEG-TS-specific muxer tuning is unnecessary for fMP4 outputs.
	if out.InitFilename == "" {
		args = append(args,
			"-mpegts_flags", "+resend_headers+pat_pmt_at_frames", // Ensure PAT/PMT + headers repeat for Safari
			"-muxdelay", "0",
			"-muxpreload", "0",
		)
	}

	if transcodeAudio {
		args = append(args,
			// Audio: Re-encode to AAC for compatibility
			// Safari requires explicit channel metadata in output.
			"-filter:a", audioFilter,
			"-c:a", "aac",
			"-b:a", audioBitrate,
			"-ar", "48000", // Force 48kHz sample rate
			"-ac", "2", // Force stereo
			"-profile:a", "aac_low", // AAC-LC profile for best compatibility
		)
	} else {
		args = append(args, "-c:a", "copy")
	}

	// HLS Flags - DVR/VOD vs Live
	// LIVE-DVR: Standard Live (Sliding Window) + retention via list_size + delete_segments
	// VOD: Complete playlist with ENDLIST
	hlsFlags := "append_list+omit_endlist+temp_file"
	playlistType := ""
	if prof.VOD {
		// VOD:
		// - Keep ALL segments (-hls_list_size 0)
		// - Allow ENDLIST tag so player sees it as finite
		hlsFlags = "independent_segments+temp_file"
		playlistType = "vod"
	} else if prof.DVRWindowSec > 0 {
		// LIVE-DVR mode: Sliding Window (Retention)
		// We use standard "Live" behavior (no playlist type) with delete_segments to enforce retention.
		// Note from ADR-001: We avoid "event" type here because FFmpeg ignores list_size (retention) for event playlists.
		// Safari detects DVR capability via PROGRAM-DATE-TIME.
		hlsFlags = "delete_segments+independent_segments+program_date_time+temp_file"
		playlistType = "" // Standard Live (Sliding)

		// Debug logging for DVR configuration
		log.L().Debug().
			Int("dvr_window_sec", prof.DVRWindowSec).
			Int("playlist_size", playlistSize).
			Str("hls_flags", hlsFlags).
			Msg("live-dvr mode ffmpeg configuration (sliding window)")
	} else if prof.LLHLS {
		hlsFlags = hlsFlags + "+independent_segments"
	}

	// Force independent segments for HEVC to ensure clean entry points
	if prof.VideoCodec == "hevc" && !strings.Contains(hlsFlags, "independent_segments") {
		hlsFlags += "+independent_segments"
	}

	args = append(args, "-hls_flags", hlsFlags)

	useDeinterlace := prof.TranscodeVideo && prof.Deinterlace
	// Use single-rate deinterlacing to avoid doubled FPS on progressive sources.
	gopDeinterlace := false
	if prof.TranscodeVideo {
		crf := prof.VideoCRF
		if crf == 0 {
			crf = 23
		}
		gop := gopForSegment(segDur, in.AvgFrameRate, gopDeinterlace)
		keyframeInterval := strconv.FormatFloat(float64(segDur), 'f', 3, 64)
		if keyframeInterval == "0.000" {
			keyframeInterval = "2.000"
		}

		if prof.HWAccel == "vaapi" {
			// VAAPI Quality Settings
			// Quality range: 1-51 (lower = better), default ~23
			quality := 23
			if prof.VideoCRF > 0 {
				quality = prof.VideoCRF
			}
			args = append(args,
				"-qp", strconv.Itoa(quality), // Constant QP mode for VAAPI
				"-compression_level", "1", // Speed/quality tradeoff (1=fast, 7=slow)
			)

			// Bitrate control for VAAPI
			if prof.VideoMaxRateK > 0 {
				args = append(args,
					"-maxrate", fmt.Sprintf("%dk", prof.VideoMaxRateK),
					"-bufsize", fmt.Sprintf("%dk", prof.VideoBufSizeK),
				)
			}

			// GOP settings for VAAPI
			args = append(args,
				"-g", strconv.Itoa(gop),
				"-keyint_min", strconv.Itoa(gop),
				"-idr_interval", strconv.Itoa(gop),
				// Stability flags for Safari/VideoToolbox
				"-aud", "1", // Insert Access Unit Delimiters
				"-sei", "1", // Insert SEI messages
				"-flags", "+cgop+global_header", // Strict Closed GOP + Global Headers (HLS Joinability)
			)

			if prof.VideoCodec == "hevc" {
				args = append(args, "-tag:v", "hvc1")
			}
		} else {
			// CPU encoding settings
			// Preset Selection
			preset := "faster"
			if prof.VideoCodec == "hevc" {
				// x265 is CPU heavy, use ultrafast for Live TV to ensure 1.0x speed
				preset = "ultrafast"
			}

			args = append(args,
				"-preset", preset,
			)
		}

		if prof.VideoCodec == "hevc" {
			if prof.HWAccel != "vaapi" {
				// HEVC (x265) Strict constraints
				// No -profile:v high for HEVC (uses main by default usually, or manually set)
				// x265 params for strict VBV and GOP compliance

				var x265Params []string

				// VBV
				if prof.VideoMaxRateK > 0 {
					// x265 uses vbv-maxrate (kbps) and vbv-bufsize (kbps)
					x265Params = append(x265Params, fmt.Sprintf("vbv-maxrate=%d:vbv-bufsize=%d", prof.VideoMaxRateK, prof.VideoBufSizeK))
				}

				// GOP / Keyint
				// keyint=<gop>:min-keyint=<gop>:scenecut=0
				x265Params = append(x265Params, fmt.Sprintf("keyint=%d:min-keyint=%d:scenecut=0", gop, gop))

				// "open-gop=0" ensures IDR frames.
				x265Params = append(x265Params, "open-gop=0")

				// Strict GOP enforcement for x265 (Ingest-Boundary-Spec)
				args = append(args, "-sc_threshold", "0")

				if prof.BFrames > 0 {
					x265Params = append(x265Params, fmt.Sprintf("bframes=%d", prof.BFrames))
				}

				// Append strict params
				args = append(args, "-x265-params", strings.Join(x265Params, ":"))
				args = append(args, "-crf", strconv.Itoa(crf))
			}
		} else {
			// H.264 (x264)
			args = append(args,
				"-profile:v", "high",
				"-level", "4.0",
				"-crf", strconv.Itoa(crf),
				"-g", strconv.Itoa(gop),
				"-keyint_min", strconv.Itoa(gop),
				"-sc_threshold", "0",
				"-force_key_frames", fmt.Sprintf("expr:gte(t,n_forced*%s)", keyframeInterval),
			)
			// Ensure every segment starts with SPS/PPS and closed GOPs for Safari.
			// Explicit keyint/min-keyint in x264-params overrides defaults and ensures strict GOP structure.
			args = append(args, "-x264-params", fmt.Sprintf("open-gop=0:repeat-headers=1:aud=1:scenecut=0:keyint=%d:min-keyint=%d", gop, gop))

			// x264 Rate Control
			if prof.VideoMaxRateK > 300 {
				maxRate := prof.VideoMaxRateK
				bufSize := prof.VideoBufSizeK

				// Clamp MaxRate to reasonable ceiling
				if maxRate > 50000 {
					maxRate = 50000
				}

				// Default BufSize rule: 2 * MaxRate if unset or invalid (too small)
				if bufSize < maxRate {
					bufSize = maxRate * 2
				}

				args = append(args,
					"-maxrate", fmt.Sprintf("%dk", maxRate),
					"-bufsize", fmt.Sprintf("%dk", bufSize),
				)
			}

			if prof.BFrames > 0 {
				// Clamp B-Frames [0..4]
				bf := prof.BFrames
				if bf > 4 {
					bf = 4
				}
				args = append(args, "-bf", strconv.Itoa(bf))
			}
		}
	}

	// Video Filters (Deinterlacing + Scaling)
	var filters []string

	if prof.HWAccel == "vaapi" {
		// VAAPI encode path: software pre-processing, then upload to GPU for encode.
		if useDeinterlace {
			if prof.VideoCodec == "hevc" {
				filters = append(filters, "yadif=0:-1:1")
			} else {
				filters = append(filters, "yadif=0:-1:1")
			}
		}
		filters = append(filters, "format=nv12")
		filters = append(filters, "hwupload")
		if prof.VideoMaxWidth > 0 {
			filters = append(filters, fmt.Sprintf("scale_vaapi=w=%d:h=-2", prof.VideoMaxWidth))
		}
	} else {
		// CPU software filters
		if useDeinterlace {
			// Deinterlace only if explicitly enabled.
			if prof.VideoCodec == "hevc" {
				filters = append(filters, "yadif=0:-1:1") // 25p for HEVC speed
			} else {
				filters = append(filters, "yadif=0:-1:1") // single-rate for stability on Safari
			}
		}

		if prof.VideoMaxWidth > 0 {
			filters = append(filters, fmt.Sprintf("scale=w=%d:h=-2:flags=lanczos", prof.VideoMaxWidth))
		}
	}

	if len(filters) > 0 {
		args = append(args, "-vf", strings.Join(filters, ","))
	}

	// Playlist type:
	// - DVR (timeshift): EVENT
	// - Recording/VOD: VOD
	if playlistType != "" {
		args = append(args, "-hls_playlist_type", playlistType)
	}

	// fMP4 Special Handling
	if out.InitFilename != "" {
		args = append(args,
			"-hls_segment_type", "fmp4",
			"-hls_fmp4_init_filename", out.InitFilename,
			"-movflags", "frag_keyframe+empty_moov+default_base_moof",
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

func gopForSegment(segmentDuration int, avgFrameRate float64, deinterlace bool) int {
	if segmentDuration <= 0 {
		segmentDuration = 2
	}
	if avgFrameRate <= 0 {
		avgFrameRate = 25
	}
	outFps := avgFrameRate
	if deinterlace {
		outFps = avgFrameRate * 2
	}
	gop := int(math.Round(outFps * float64(segmentDuration)))
	if gop <= 0 {
		return 50
	}
	return gop
}

// normalizeDuration converts duration strings (e.g. "10s", "5000ms") to microseconds for FFmpeg
func normalizeDuration(s string) string {
	s = strings.TrimSpace(s)
	// If already numeric (microseconds), return as-is
	if _, err := strconv.ParseInt(s, 10, 64); err == nil {
		return s
	}
	// Parse common formats
	if strings.HasSuffix(s, "s") {
		if val, err := strconv.ParseFloat(strings.TrimSuffix(s, "s"), 64); err == nil {
			return strconv.FormatInt(int64(val*1000000), 10) // seconds to microseconds
		}
	}
	if strings.HasSuffix(s, "ms") {
		if val, err := strconv.ParseFloat(strings.TrimSuffix(s, "ms"), 64); err == nil {
			return strconv.FormatInt(int64(val*1000), 10) // milliseconds to microseconds
		}
	}
	// Fallback to 2s default
	return "2000000"
}

// normalizeSize converts size strings (e.g. "10M", "5000K") to bytes for FFmpeg
func normalizeSize(s string) string {
	s = strings.TrimSpace(s)
	// If already numeric (bytes), return as-is
	if _, err := strconv.ParseInt(s, 10, 64); err == nil {
		return s
	}
	// Parse common formats
	s = strings.ToUpper(s)
	if strings.HasSuffix(s, "M") {
		if val, err := strconv.ParseFloat(strings.TrimSuffix(s, "M"), 64); err == nil {
			return strconv.FormatInt(int64(val*1024*1024), 10) // MB to bytes
		}
	}
	if strings.HasSuffix(s, "K") {
		if val, err := strconv.ParseFloat(strings.TrimSuffix(s, "K"), 64); err == nil {
			return strconv.FormatInt(int64(val*1024), 10) // KB to bytes
		}
	}
	if strings.HasSuffix(s, "G") {
		if val, err := strconv.ParseFloat(strings.TrimSuffix(s, "G"), 64); err == nil {
			return strconv.FormatInt(int64(val*1024*1024*1024), 10) // GB to bytes
		}
	}
	// Fallback to 10MB default
	return "10485760"
}

// cspell:ignore analyzeduration probesize aresample aformat nostdin loglevel nostats fflags genpts discardcorrupt igndts libx muxer mpegts muxdelay muxpreload endlist ENDLIST LLHLS Deinterlace deinterlacing maxrate keyint ultrafast kbps Keyint bframes scenecut yadif hwupload lanczos timeshift movflags moov moof
