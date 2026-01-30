package ffmpeg

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

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

	switch spec.Profile {
	case vod.ProfileHigh:
		args = append(args, "-c:v", "libx264", "-preset", "slow", "-crf", "18")
		args = append(args, "-c:a", "aac", "-b:a", "192k")
	case vod.ProfileLow:
		args = append(args, "-c:v", "libx264", "-preset", "fast", "-crf", "23")
		args = append(args, "-c:a", "aac", "-b:a", "128k")
	default: // ProfileDefault (Copy if possible)
		args = append(args, "-c:v", "copy", "-c:a", "copy")
	}

	// Map only video and audio streams (exclude subtitles/teletext)
	args = append(args, "-map", "0:v", "-map", "0:a")

	// HLS output format with proper settings
	args = append(args,
		"-f", "hls",
		"-hls_time", "6",
		"-hls_list_size", "0",
		"-hls_segment_filename", filepath.Join(spec.WorkDir, "seg_%05d.ts"),
	)

	// Output to temp
	args = append(args, outputPath)
	return args, nil
}
