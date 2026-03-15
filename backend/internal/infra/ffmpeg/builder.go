package ffmpeg

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ManuGH/xg2g/internal/domain/playbackprofile/ports"
	"github.com/ManuGH/xg2g/internal/domain/vod"
)

// mapProfileToArgs converts the high-level intent into FFmpeg flags.
func mapProfileToArgs(spec vod.Spec) ([]string, error) {
	// Validation: OutputTemp must be a non-empty filename
	if strings.TrimSpace(spec.OutputTemp) == "" {
		return nil, fmt.Errorf("vod: OutputTemp is empty (workdir=%q)", spec.WorkDir)
	}

	// Build output path using filepath.Join for correct path handling
	outputPath := filepath.Join(spec.WorkDir, spec.OutputTemp)

	// Validation: Output must not be a directory
	if strings.HasSuffix(outputPath, string(filepath.Separator)) {
		return nil, fmt.Errorf("vod: output resolves to directory, expected file: %q", outputPath)
	}

	// Handle file:// URI scheme for local files
	inputPath := spec.Input
	if strings.HasPrefix(inputPath, "file://") {
		raw := strings.TrimPrefix(inputPath, "file://")
		if decoded, err := url.PathUnescape(raw); err == nil {
			inputPath = decoded
		}
	}

	args := []string{
		"-y", "-nostdin", "-hide_banner", "-progress", "pipe:2", "-loglevel", "warning",
		"-i", inputPath,
	}

	if spec.TargetProfile != nil {
		targetArgs, err := mapTargetProfileToArgs(*spec.TargetProfile)
		if err != nil {
			return nil, err
		}
		args = append(args, targetArgs...)
	} else {
		args = append(args, mapLegacyProfileToArgs(spec.Profile)...)
	}

	// Map only video and audio streams (exclude subtitles/teletext)
	args = append(args, "-map", "0:v", "-map", "0:a")

	args = append(args, hlsOutputArgs(spec.WorkDir, outputPath, spec.TargetProfile)...)
	return args, nil
}

func mapLegacyProfileToArgs(profile vod.Profile) []string {
	switch profile {
	case vod.ProfileHigh:
		return []string{"-c:v", "libx264", "-preset", "slow", "-crf", "18", "-c:a", "aac", "-b:a", "192k"}
	case vod.ProfileLow:
		return []string{"-c:v", "libx264", "-preset", "fast", "-crf", "23", "-c:a", "aac", "-b:a", "128k"}
	default:
		return []string{"-c:v", "copy", "-c:a", "aac", "-b:a", "192k", "-ac", "2", "-ar", "48000"}
	}
}

func mapTargetProfileToArgs(target ports.TargetPlaybackProfile) ([]string, error) {
	target = ports.CanonicalizeTarget(target)
	if !target.HLS.Enabled {
		return nil, fmt.Errorf("vod: target profile must enable hls for recording builds")
	}

	videoArgs, err := videoTargetArgs(target.Video)
	if err != nil {
		return nil, err
	}
	audioArgs, err := audioTargetArgs(target.Audio)
	if err != nil {
		return nil, err
	}

	args := make([]string, 0, len(videoArgs)+len(audioArgs))
	args = append(args, videoArgs...)
	args = append(args, audioArgs...)
	return args, nil
}

func videoTargetArgs(video ports.VideoTarget) ([]string, error) {
	switch video.Mode {
	case "", ports.MediaModeCopy:
		return []string{"-c:v", "copy"}, nil
	case ports.MediaModeTranscode:
		encoder, err := ffmpegVideoEncoder(video.Codec)
		if err != nil {
			return nil, err
		}
		args := []string{"-c:v", encoder}
		if video.BitrateKbps > 0 {
			return append(args, "-b:v", strconv.Itoa(video.BitrateKbps)+"k"), nil
		}
		return append(args, "-preset", "fast", "-crf", "23"), nil
	default:
		return nil, fmt.Errorf("vod: unsupported video mode %q", video.Mode)
	}
}

func audioTargetArgs(audio ports.AudioTarget) ([]string, error) {
	switch audio.Mode {
	case "", ports.MediaModeCopy:
		return []string{"-c:a", "copy"}, nil
	case ports.MediaModeTranscode:
		encoder, err := ffmpegAudioEncoder(audio.Codec)
		if err != nil {
			return nil, err
		}
		args := []string{"-c:a", encoder}
		if audio.BitrateKbps > 0 {
			args = append(args, "-b:a", strconv.Itoa(audio.BitrateKbps)+"k")
		}
		if audio.Channels > 0 {
			args = append(args, "-ac", strconv.Itoa(audio.Channels))
		}
		if audio.SampleRate > 0 {
			args = append(args, "-ar", strconv.Itoa(audio.SampleRate))
		}
		return args, nil
	default:
		return nil, fmt.Errorf("vod: unsupported audio mode %q", audio.Mode)
	}
}

func ffmpegVideoEncoder(codec string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "", "h264":
		return "libx264", nil
	case "hevc", "h265":
		return "libx265", nil
	default:
		return "", fmt.Errorf("vod: unsupported target video codec %q", codec)
	}
}

func ffmpegAudioEncoder(codec string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "", "aac":
		return "aac", nil
	case "ac3":
		return "ac3", nil
	case "mp3":
		return "libmp3lame", nil
	default:
		return "", fmt.Errorf("vod: unsupported target audio codec %q", codec)
	}
}

func hlsOutputArgs(workDir, outputPath string, target *ports.TargetPlaybackProfile) []string {
	segmentSeconds := 6
	segmentContainer := "mpegts"
	if target != nil {
		canonical := ports.CanonicalizeTarget(*target)
		if canonical.HLS.SegmentSeconds > 0 {
			segmentSeconds = canonical.HLS.SegmentSeconds
		}
		segmentContainer = resolveSegmentContainer(canonical)
	}

	args := []string{
		"-f", "hls",
		"-hls_time", strconv.Itoa(segmentSeconds),
		"-hls_list_size", "0",
	}

	switch segmentContainer {
	case "fmp4", "mp4":
		args = append(args,
			"-hls_segment_type", "fmp4",
			"-hls_fmp4_init_filename", "init.mp4",
			"-hls_segment_filename", filepath.Join(workDir, "seg_%05d.m4s"),
		)
	default:
		args = append(args, "-hls_segment_filename", filepath.Join(workDir, "seg_%05d.ts"))
	}

	return append(args, outputPath)
}

func resolveSegmentContainer(target ports.TargetPlaybackProfile) string {
	if target.HLS.SegmentContainer != "" {
		return target.HLS.SegmentContainer
	}
	switch target.Packaging {
	case ports.PackagingFMP4, ports.PackagingMP4:
		return "fmp4"
	default:
		return "mpegts"
	}
}
