package v3

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/vod"
)

// StreamInfo contains codec details extracted via ffprobe
type StreamInfo struct {
	Video VideoStreamInfo
	Audio AudioStreamInfo
}

type VideoStreamInfo struct {
	CodecName string
	PixFmt    string
	Profile   string
	Level     int
	BitDepth  int
	StartTime float64
}

type AudioStreamInfo struct {
	CodecName     string
	SampleRate    int
	Channels      int
	ChannelLayout string
	TrackCount    int
	StartTime     float64
}

// ProbeStreams uses ffprobe to extract stream codec information
func ProbeStreams(ctx context.Context, ffprobeBin, inputPath string) (*StreamInfo, error) {
	if ffprobeBin == "" {
		ffprobeBin = "ffprobe"
	}

	cmd := exec.CommandContext(ctx, ffprobeBin,
		"-v", "quiet",
		"-fflags", "+discardcorrupt",
		"-print_format", "json",
		"-show_entries", "stream=codec_type,codec_name,profile,level,pix_fmt,bits_per_raw_sample,sample_rate,channels,channel_layout,start_time",
		"-show_streams",
		inputPath,
	)

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe failed (exit %d): %w\nOutput: %s",
			cmd.ProcessState.ExitCode(), err, truncateForLog(string(out), 500))
	}

	var probeData struct {
		Streams []struct {
			CodecType        string `json:"codec_type"`
			CodecName        string `json:"codec_name"`
			Profile          string `json:"profile"`
			Level            int    `json:"level"`
			PixFmt           string `json:"pix_fmt"`
			SampleRate       string `json:"sample_rate"`
			Channels         int    `json:"channels"`
			ChannelLayout    string `json:"channel_layout"`
			BitsPerRawSample string `json:"bits_per_raw_sample"`
			StartTime        string `json:"start_time"`
		} `json:"streams"`
	}

	if err := json.Unmarshal(out, &probeData); err != nil {
		return nil, fmt.Errorf("ffprobe JSON parse failed: %w\nRaw output: %s", err, truncateForLog(string(out), 500))
	}

	info := &StreamInfo{}
	audioCount := 0

	for _, s := range probeData.Streams {
		if s.CodecType == "video" && info.Video.CodecName == "" {
			info.Video.CodecName = s.CodecName
			info.Video.PixFmt = s.PixFmt
			info.Video.Profile = s.Profile
			info.Video.Level = s.Level
			if s.StartTime != "" {
				if start, err := strconv.ParseFloat(s.StartTime, 64); err == nil {
					info.Video.StartTime = start
				}
			}
			info.Video.BitDepth = inferBitDepthFromPixFmt(s.PixFmt)
			if info.Video.BitDepth == 0 && s.BitsPerRawSample != "" {
				fmt.Sscanf(s.BitsPerRawSample, "%d", &info.Video.BitDepth)
			}
			if info.Video.BitDepth == 0 {
				info.Video.BitDepth = 8
			}
		}
		if s.CodecType == "audio" {
			audioCount++
			if info.Audio.CodecName == "" {
				info.Audio.CodecName = s.CodecName
				fmt.Sscanf(s.SampleRate, "%d", &info.Audio.SampleRate)
				info.Audio.Channels = s.Channels
				info.Audio.ChannelLayout = s.ChannelLayout
				if s.StartTime != "" {
					if start, err := strconv.ParseFloat(s.StartTime, 64); err == nil {
						info.Audio.StartTime = start
					}
				}
			}
		}
	}

	info.Audio.TrackCount = audioCount
	if info.Video.CodecName == "" {
		return nil, errors.New("no video stream found")
	}

	return info, nil
}

func inferBitDepthFromPixFmt(pixFmt string) int {
	if pixFmt == "" {
		return 0
	}
	tenBitPattern := regexp.MustCompile(`(?i)p10(le|be)?`)
	if tenBitPattern.MatchString(pixFmt) {
		return 10
	}
	twelveBitPattern := regexp.MustCompile(`(?i)p12(le|be)?`)
	if twelveBitPattern.MatchString(pixFmt) {
		return 12
	}
	sixteenBitPattern := regexp.MustCompile(`(?i)p16(le|be)?`)
	if sixteenBitPattern.MatchString(pixFmt) {
		return 16
	}
	return 8
}

func truncateForLog(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + fmt.Sprintf("... (truncated, %d bytes total)", len(s))
}

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

func FindKeyframeStart(ctx context.Context, ffprobeBin, inputPath string) (string, error) {
	cmd := exec.CommandContext(ctx, ffprobeBin,
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "frame=pkt_pts_time,pict_type",
		"-of", "csv=p=0",
		"-read_intervals", "%+10",
		inputPath,
	)

	out, err := cmd.Output()
	if err != nil {
		return "1", err
	}

	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		parts := strings.Split(strings.TrimSpace(line), ",")
		if len(parts) >= 2 {
			timestamp := parts[0]
			frameType := parts[1]
			if frameType == "I" {
				if timestamp != "" && !strings.HasPrefix(timestamp, "-") {
					return timestamp, nil
				}
			}
		}
	}
	return "1", nil
}

func ComputeAudioDelayMs(info *StreamInfo) int {
	return 0
}

// Delegation: buildRemuxArgs calls vod.BuildRemuxArgs
// This function signature MUST match recordings_vod.go expectations (4 args)
func buildRemuxArgs(info *StreamInfo, inputPath, outputPath, startTime string) *RemuxDecision {
	audioDelayMs := ComputeAudioDelayMs(info)
	in := vod.BuildArgsInput{
		InputPath:     inputPath,
		OutputPath:    outputPath,
		StartTime:     startTime,
		VideoCodec:    info.Video.CodecName,
		VideoPixFmt:   info.Video.PixFmt,
		VideoBitDepth: info.Video.BitDepth,
		AudioCodec:    info.Audio.CodecName,
		AudioTracks:   info.Audio.TrackCount,
		AudioDelayMs:  audioDelayMs,
	}

	d := vod.BuildRemuxArgs(in)

	// Map returned Strategy string to our local enum
	// Note: internal/vod uses string constants that match yours
	return &RemuxDecision{
		Strategy: RemuxStrategy(d.Strategy),
		Args:     d.Args,
		Reason:   d.Reason,
	}
}

// Delegation wrapper for fallback (must match signature used in vod.go if called directly)
func buildFallbackRemuxArgs(inputPath, outputPath, startTime string, audioDelayMs int) []string {
	in := vod.BuildArgsInput{
		InputPath:    inputPath,
		OutputPath:   outputPath,
		StartTime:    startTime,
		AudioDelayMs: audioDelayMs,
	}
	return vod.BuildFallbackRemuxArgs(in)
}

// Delegation wrapper for transcode
func buildTranscodeArgs(inputPath, outputPath, startTime string, audioDelayMs int) []string {
	in := vod.BuildArgsInput{
		InputPath:    inputPath,
		OutputPath:   outputPath,
		StartTime:    startTime,
		AudioDelayMs: audioDelayMs,
	}
	return vod.BuildTranscodeArgs(in)
}

// Dummy wrapper for default (internal use by buildRemuxArgs, which we replaced)
// But we keep it to satisfy linking if anything else calls it.
func buildDefaultRemuxArgs(inputPath, outputPath string, transcodeAudio bool, startTime string, audioDelayMs int) []string {
	in := vod.BuildArgsInput{
		InputPath:    inputPath,
		OutputPath:   outputPath,
		StartTime:    startTime,
		AudioDelayMs: audioDelayMs,
	}
	return vod.BuildDefaultRemuxArgs(in, transcodeAudio)
}

// Helpers for vod.go compilation compatibility

func logRemuxDecision(d *RemuxDecision, recordingID string) {
	log.L().Info().
		Str("strategy", string(d.Strategy)).
		Str("reason", d.Reason).
		Str("recording", recordingID).
		Msg("remux decision calculated")
}

var (
	ErrNonMonotonousDTS = errors.New("non-monotonous dts")
	ErrInvalidDuration  = errors.New("invalid duration")
	ErrTimestampUnset   = errors.New("timestamp unset")
)

// ErrFFmpegStalled is defined in recordings.go

func classifyRemuxError(stderr string, exitCode int) error {
	if exitCode == 0 {
		return nil
	}
	if strings.Contains(stderr, "Non-monotonous DTS") {
		return ErrNonMonotonousDTS
	}
	if strings.Contains(stderr, "Packet with invalid duration") {
		return ErrInvalidDuration
	}
	if strings.Contains(stderr, "timestamps are unset") {
		return ErrTimestampUnset
	}
	return fmt.Errorf("ffmpeg failed with exit code %d", exitCode)
}

func shouldRetryWithFallback(err error) bool {
	if errors.Is(err, ErrNonMonotonousDTS) ||
		errors.Is(err, ErrTimestampUnset) {
		return true
	}
	return false
}

func shouldRetryWithTranscode(err error) bool {
	return true
}

// OMITTED: insertArgsBefore (Exists in recordings.go)
// OMITTED: runFFmpegWithProgress (Exists in recordings_vod.go)
