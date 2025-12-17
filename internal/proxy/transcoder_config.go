// SPDX-License-Identifier: MIT

package proxy

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ManuGH/xg2g/internal/config"
)

func buildTranscoderConfigFromRuntime(rt config.TranscoderRuntime) TranscoderConfig {
	codec := strings.TrimSpace(rt.Codec)
	if codec == "" {
		codec = "aac"
	}

	bitrate := strings.TrimSpace(rt.Bitrate)
	if bitrate == "" {
		bitrate = "192k"
	}

	channels := rt.Channels
	if channels <= 0 {
		channels = 2
	}

	useRust := rt.UseRustRemuxer

	ffmpegPath, ffmpegFound := resolveFFmpegPath(strings.TrimSpace(rt.FFmpegPath))
	h264Repair := rt.H264RepairEnabled
	videoTranscode := rt.VideoTranscode
	audioEnabled := rt.AudioEnabled

	// If we can't run FFmpeg, disable features that depend on it.
	if !ffmpegFound {
		h264Repair = false
		videoTranscode = false
		if !useRust {
			audioEnabled = false
		}
	}

	transcoderURL := strings.TrimSpace(rt.TranscoderURL)
	if transcoderURL == "" {
		transcoderURL = "http://localhost:8085"
	}

	return TranscoderConfig{
		Enabled:           audioEnabled,
		Codec:             codec,
		Bitrate:           bitrate,
		Channels:          channels,
		FFmpegPath:        ffmpegPath,
		GPUEnabled:        rt.GPUEnabled,
		TranscoderURL:     transcoderURL,
		UseRustRemuxer:    useRust,
		H264RepairEnabled: h264Repair,
		VideoTranscode:    videoTranscode,
		VideoCodec:        strings.TrimSpace(rt.VideoCodec),
	}
}

func resolveFFmpegPath(configured string) (string, bool) {
	configured = strings.TrimSpace(configured)
	if configured != "" {
		if path, err := exec.LookPath(configured); err == nil {
			return path, true
		}
		if filepath.IsAbs(configured) {
			if _, err := os.Stat(configured); err == nil {
				return configured, true
			}
		}
		return configured, false
	}

	if path, err := exec.LookPath("ffmpeg"); err == nil {
		return path, true
	}
	if _, err := os.Stat("/usr/bin/ffmpeg"); err == nil {
		return "/usr/bin/ffmpeg", true
	}
	return "/usr/bin/ffmpeg", false
}
