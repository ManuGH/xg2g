// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package smoother

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"

	"github.com/ManuGH/xg2g/internal/log"
)

// TranscoderReadCloser wraps an FFmpeg stdout pipe and guarantees clean process termination on Close.
type TranscoderReadCloser struct {
	pipe io.ReadCloser
	cmd  *exec.Cmd
	once sync.Once
}

func (t *TranscoderReadCloser) Read(p []byte) (int, error) {
	return t.pipe.Read(p)
}

func (t *TranscoderReadCloser) Close() error {
	var err error
	t.once.Do(func() {
		_ = t.pipe.Close()
		if t.cmd != nil && t.cmd.Process != nil {
			_ = t.cmd.Process.Kill()
			_ = t.cmd.Wait()
		}
	})
	return err
}

// StartTranscoder spawns an FFmpeg hardware or software pipeline converting an incoming stream
// to DVB-compliant HEVC or H.264 MPEG-TS suitable for Vu+ Broadcom decoders.
func StartTranscoder(ctx context.Context, targetURL string, mode string, userAgent string) (io.ReadCloser, error) {
	if userAgent == "" {
		userAgent = "IPTVSmartersPro/1.0.0 (Linux; Android)"
	}

	// Detect if Intel VAAPI GPU device is available
	hasVAAPI := false
	if _, err := os.Stat("/dev/dri/renderD128"); err == nil {
		hasVAAPI = true
	}

	var args []string
	args = append(args, "-hide_banner", "-loglevel", "warning")
	args = append(args, "-user_agent", userAgent)

	if hasVAAPI && (mode == "" || mode == "hevc" || mode == "auto") {
		// Hardware accelerated H.264/AVC 4K -> HEVC 4K transcode via Intel Xe GPU
		args = append(args,
			"-init_hw_device", "vaapi=va:/dev/dri/renderD128",
			"-filter_hw_device", "va",
			"-hwaccel", "vaapi",
			"-hwaccel_output_format", "vaapi",
			"-i", targetURL,
			"-c:v", "hevc_vaapi",
			"-qp", "25",
			"-c:a", "copy",
			"-f", "mpegts",
			"pipe:1",
		)
		log.L().Info().Str("targetURL", targetURL).Str("encoder", "hevc_vaapi").Msg("started hardware HEVC live transcode")
	} else if hasVAAPI && mode == "1080p" {
		// Hardware accelerated downscale to 1080p H.264
		args = append(args,
			"-init_hw_device", "vaapi=va:/dev/dri/renderD128",
			"-filter_hw_device", "va",
			"-hwaccel", "vaapi",
			"-hwaccel_output_format", "vaapi",
			"-i", targetURL,
			"-vf", "scale_vaapi=w=1920:h=1080",
			"-c:v", "h264_vaapi",
			"-qp", "23",
			"-c:a", "copy",
			"-f", "mpegts",
			"pipe:1",
		)
		log.L().Info().Str("targetURL", targetURL).Str("encoder", "h264_vaapi").Msg("started hardware 1080p live downscale")
	} else {
		// Software CPU fallback (e.g. Mac development or systems without Intel GPU)
		args = append(args,
			"-i", targetURL,
			"-c:v", "libx264",
			"-preset", "veryfast",
			"-vf", "scale=1920:-2",
			"-c:a", "copy",
			"-f", "mpegts",
			"pipe:1",
		)
		log.L().Info().Str("targetURL", targetURL).Str("encoder", "libx264").Msg("started software CPU live transcode fallback")
	}

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to open stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start ffmpeg transcoder: %w", err)
	}

	return &TranscoderReadCloser{pipe: stdout, cmd: cmd}, nil
}
