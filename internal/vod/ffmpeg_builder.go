package vod

import (
	"fmt"
)

// StreamInfo is a simplified version of what we need for decision making
// For Phase A, we pass minimal fields in BuildArgsInput.
// But we need to keep the logic structure.

// RemuxStrategy represents the remux approach to use
type RemuxStrategy string

const (
	StrategyDefault     RemuxStrategy = "default"     // copy/copy or copy/transcode
	StrategyFallback    RemuxStrategy = "fallback"    // alternate flags for timestamp issues
	StrategyTranscode   RemuxStrategy = "transcode"   // full transcode (HEVC or remux failed)
	StrategyUnsupported RemuxStrategy = "unsupported" // fail fast
)

// RemuxDecision contains the strategy and ffmpeg arguments
type RemuxDecision struct {
	Strategy RemuxStrategy
	Args     []string
	Reason   string
}

// BuildArgsInput contains all dependencies to build FFmpeg arguments
type BuildArgsInput struct {
	InputPath  string
	OutputPath string
	StartTime  string // "1" or precision calculated

	// Stream Properties (from Probe)
	VideoCodec    string
	VideoPixFmt   string
	VideoBitDepth int
	AudioCodec    string
	AudioTracks   int

	// Calculated Properties
	AudioDelayMs int

	// Optional Overrides via "Gate" logic inside builder?
	// The original builder contained the decision tree based on codec.
	// We preserve that here.
}

// BuildRemuxArgs constructs ffmpeg arguments based on stream info and logic
// Moved verbatim from recordings_remux.go (adapted for input struct)
func BuildRemuxArgs(in BuildArgsInput) *RemuxDecision {
	// Video codec decision tree
	// Chrome Desktop (70-80% primary client) → most restrictive policy wins
	switch in.VideoCodec {
	case "hevc", "h265":
		// HEVC (<5% of recordings) -> Chrome incompatible -> transcode to H.264
		return &RemuxDecision{
			Strategy: StrategyTranscode,
			Reason:   "HEVC detected - Chrome incompatible (Strict x264 Policy)",
			Args:     buildTranscodeArgs(in),
		}
	case "h264":
		// Check pixel format and bit depth
		if in.VideoPixFmt == "yuv420p10le" || in.VideoBitDepth >= 10 {
			// 10-bit H.264: Chrome incompatible -> transcode to 8-bit
			return &RemuxDecision{
				Strategy: StrategyTranscode,
				Reason:   "10-bit H.264 detected - Chrome incompatible",
				Args:     buildTranscodeArgs(in),
			}
		}
		// 8-bit H.264 yuv420p: safe for copy
		// Fall through to audio check
	case "mpeg2video":
		// MPEG2: Browser compatibility concern -> transcode
		return &RemuxDecision{
			Strategy: StrategyTranscode,
			Reason:   "MPEG2 detected - browser compatibility concern",
			Args:     buildTranscodeArgs(in),
		}
	default:
		// Unknown codec: fail fast or try default?
		// Original code returned StrategyUnsupported
		return &RemuxDecision{
			Strategy: StrategyUnsupported,
			Reason:   fmt.Sprintf("unsupported video codec: %s", in.VideoCodec),
		}
	}

	// Audio codec decision tree
	// POLICY: Audio is always transcoded to AAC for predictable browser playback.
	transcodeAudio := true

	// Decision: Use Default (Smart Copy) or Transcode
	return &RemuxDecision{
		Strategy: StrategyDefault,
		Reason:   "H.264 8-bit detected - Safe for Smart Copy",
		Args:     BuildDefaultRemuxArgs(in, transcodeAudio),
	}
}

// BuildDefaultRemuxArgs constructs the default remux command
// Strategy: Video Copy + Audio Transcode + V8 Precision Cutting
func BuildDefaultRemuxArgs(in BuildArgsInput, transcodeAudio bool) []string {
	args := []string{
		"-y",
		"-nostdin",
		"-hide_banner",
		"-loglevel", "error",
		"-fflags", "+genpts+discardcorrupt+igndts",
		"-err_detect", "ignore_err",
		// V8: Precision Cutting (Skip to Keyframe)
		"-ss", in.StartTime,
		"-i", in.InputPath,
		// Stream selection (video + first audio only)
		"-map", "0:v:0?",
		"-map", "0:a:0?",
		"-c:v", "copy", // H.264 8-bit yuv420p only
		// Bitstream filter to reset timestamps in copy mode (Perfect Sync)
		"-bsf:v", "setts=pts=PTS-STARTPTS:dts=DTS-STARTPTS",
	}

	if transcodeAudio {
		audioFilter := BuildAudioFilterChain(in.AudioDelayMs, true)
		// Chrome-first policy → AAC stereo (AC3 5.1 incompatible)
		// Most TV recordings have AC3 → must transcode
		args = append(args,
			"-c:a", "aac",
			"-b:a", "192k",
			"-profile:a", "aac_low",
			"-ar", "48000",
			"-ac", "2", // Stereo (consistent with HLS path)
			// Audio filter chain: PTS reset + async resample + stereo downmix for corrupted DVB
			"-filter:a", audioFilter,
			// Audio sync flags for corrupted input (DVB-T2/Sat recordings)
			"-async", "1",
		)
	} else {
		args = append(args, "-c:a", "copy")
	}

	args = append(args,
		"-avoid_negative_ts", "make_zero", // Safety: Ensure no negative timestamps in output
		"-movflags", "+faststart", // Move moov atom (enable seek before download)
		"-sn", // Strip subtitles (DVB subs not browser-compatible)
		"-dn", // Strip data streams
		"-f", "mp4",
		in.OutputPath,
	)

	return args
}

// BuildFallbackRemuxArgs constructs the fallback remux command for timestamp issues
// Uses max_interleave_delta + vsync cfr to handle broken DTS
func BuildFallbackRemuxArgs(in BuildArgsInput) []string {
	audioFilter := BuildAudioFilterChain(in.AudioDelayMs, true)
	args := []string{
		"-y",
		"-nostdin",
		"-hide_banner",
		"-loglevel", "error",
		// Same robustness as DEFAULT, but igndts already in fflags
		"-fflags", "+genpts+discardcorrupt+igndts",
		"-err_detect", "ignore_err",
		"-avoid_negative_ts", "make_zero",
		// V8: Precision Cutting (Skip to Keyframe)
		"-ss", in.StartTime,
		"-i", in.InputPath,
		"-map", "0:v:0?",
		"-map", "0:a:0?",
		"-c:v", "copy",
		// Fallback-specific: force constant frame rate
		"-vsync", "cfr",
		// Max interleave delta (helps with broken muxing)
		"-max_interleave_delta", "0",
		// Audio: always transcode (fallback = broken stream)
		"-c:a", "aac",
		"-b:a", "192k",
		"-profile:a", "aac_low",
		"-ar", "48000",
		"-ac", "2",
		"-filter:a", audioFilter,
		"-start_at_zero", // Force output timestamps to start at zero (prevents negative PTS)
		"-movflags", "+faststart",
		"-sn",
		"-dn",
		"-f", "mp4",
		in.OutputPath,
	}
	return args
}

// BuildTranscodeArgs constructs full transcode args (V8 Strategy)
// Uses H.264 VAAPI (x264 track) for max compatibility (Safari/Chrome/Firefox)
func BuildTranscodeArgs(in BuildArgsInput) []string {
	return buildTranscodeArgs(in)
}

func buildTranscodeArgs(in BuildArgsInput) []string {
	// Calculate audio sync offset filters
	audioFilter := BuildAudioFilterChain(in.AudioDelayMs, false)

	args := []string{
		"-y",
		"-nostdin",
		"-hide_banner",
		"-loglevel", "error",
		"-fflags", "+genpts+discardcorrupt",
		// Hardware Init (VAAPI)
		"-init_hw_device", "vaapi=va:/dev/dri/renderD128",
		"-filter_hw_device", "va",
		// V8: Precision Cutting
		"-ss", in.StartTime,
		"-i", in.InputPath,
		"-map", "0:v:0?",
		"-map", "0:a:0?",
		// Helper: Hardware Upload
		"-filter:v", "format=nv12,hwupload,scale_vaapi=format=nv12",
		// Codec: H.264 VAAPI (x264 Track)
		"-c:v", "h264_vaapi",
		"-level", "41", // Level 4.1 for max compatibility
		"-qp", "24", // CQP (Constant Quality)
		// Audio: AAC Transcode
		"-c:a", "aac",
		"-b:a", "192k",
		"-ar", "48000",
		"-ac", "2",
		"-filter:a", audioFilter,
		// Structure
		"-start_at_zero",
		"-avoid_negative_ts", "make_zero",
		"-movflags", "+faststart",
		"-sn", "-dn",
		"-f", "mp4",
		in.OutputPath,
	}
	return args
}

// BuildAudioFilterChain constructs the complex audio filter for sync correction
func BuildAudioFilterChain(delayMs int, enableResample bool) string {
	filter := "aresample=async=1" // Default simple resample

	// If delay detected (e.g. -1200ms or +500ms), we shift timestamps
	// But in recent builds we disabled computeAudioDelayMs logic (returning 0)
	// So this might be effectively static.
	if delayMs != 0 {
		// Example: "adelay=1000|1000" adds 1s silence
		// "asetpts=PTS-1.0/TB" shifts
		// Keeping simple for now as per current logic
	}

	// Force stereo downmix to handle 5.1 -> 2.0 safely
	filter += ",pan=stereo|FL=FC+0.30*FL+0.30*BL|FR=FC+0.30*FR+0.30*BR"
	return filter
}
