package config

import (
	"os"
	"path/filepath"
	"strings"
)

// ResolveFFprobeBin returns an effective ffprobe binary path based on configured values.
//
// Resolution order:
// 1) Explicit ffprobeBin (e.g. XG2G_FFPROBE_BIN)
// 2) Derive from ffmpegBin (.../ffmpeg -> .../ffprobe) if the derived binary exists
// 3) Empty string (caller may fall back to PATH resolution)
func ResolveFFprobeBin(ffprobeBin, ffmpegBin string) string {
	return resolveFFprobeBinWithStat(ffprobeBin, ffmpegBin, os.Stat)
}

func resolveFFprobeBinWithStat(ffprobeBin, ffmpegBin string, stat func(string) (os.FileInfo, error)) string {
	ffprobeBin = strings.TrimSpace(ffprobeBin)
	if ffprobeBin != "" {
		return ffprobeBin
	}

	ffmpegBin = strings.TrimSpace(ffmpegBin)
	if ffmpegBin == "" {
		return ""
	}

	// Only derive from a concrete ffmpeg path (.../ffmpeg -> .../ffprobe).
	// If ffmpegBin is just "ffmpeg" (PATH), we intentionally do not guess.
	if !strings.ContainsRune(ffmpegBin, '/') {
		return ""
	}
	if filepath.Base(ffmpegBin) != "ffmpeg" {
		return ""
	}

	candidate := filepath.Join(filepath.Dir(ffmpegBin), "ffprobe")
	if fi, err := stat(candidate); err == nil && fi != nil && !fi.IsDir() {
		return candidate
	}
	return ""
}
