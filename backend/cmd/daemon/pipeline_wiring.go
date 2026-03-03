// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package main

import (
	"github.com/ManuGH/xg2g/internal/config"
	sessionports "github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/infra/media/ffmpeg"
	"github.com/ManuGH/xg2g/internal/infra/media/stub"
	"github.com/ManuGH/xg2g/internal/pipeline/exec/enigma2"
	"github.com/rs/zerolog"
)

//nolint:unused // retained for focused daemon wiring tests.
func buildMediaPipeline(cfg config.AppConfig, e2Client *enigma2.Client, logger zerolog.Logger) sessionports.MediaPipeline {
	if cfg.Engine.Mode == "virtual" {
		return stub.NewAdapter()
	}

	adapter := ffmpeg.NewLocalAdapter(
		cfg.FFmpeg.Bin,
		cfg.FFmpeg.FFprobeBin,
		cfg.HLS.Root,
		e2Client,
		logger,
		cfg.Enigma2.AnalyzeDuration,
		cfg.Enigma2.ProbeSize,
		cfg.HLS.DVRWindow,
		cfg.FFmpeg.KillTimeout,
		cfg.Enigma2.FallbackTo8001,
		cfg.Enigma2.PreflightTimeout,
		cfg.HLS.SegmentSeconds,
		cfg.Timeouts.TranscodeStart,
		cfg.Timeouts.TranscodeNoProgress,
		cfg.FFmpeg.VaapiDevice,
	)

	if cfg.FFmpeg.VaapiDevice != "" {
		if err := adapter.PreflightVAAPI(); err != nil {
			logger.Warn().Err(err).Str("device", cfg.FFmpeg.VaapiDevice).
				Msg("VAAPI preflight failed; GPU transcoding will be unavailable for sessions requesting it")
		}
	}

	return adapter
}
