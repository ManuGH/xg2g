package ffmpeg

import (
	"github.com/ManuGH/xg2g/internal/control/vod"
)

// mapProfileToArgs converts the high-level intent into FFmpeg flags.
func mapProfileToArgs(spec vod.Spec) []string {
	// TODO: Port full logic from internal/vod/ffmpeg_builder.go
	// For now, minimal implementation to pass verification.

	args := []string{
		"-y", "-nostdin", "-hide_banner", "-loglevel", "error",
		"-i", spec.Input,
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

	// Output to temp
	args = append(args, spec.WorkDir+"/"+spec.OutputTemp)
	return args
}
