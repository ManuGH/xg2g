// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package exec

import (
	"time"

	"github.com/ManuGH/xg2g/internal/pipeline/exec/enigma2"
	"github.com/ManuGH/xg2g/internal/pipeline/exec/ffmpeg"
)

// RealFactory produces real execution components (Enigma2 Tuner, Real/Stub Transcoder).
type RealFactory struct {
	E2Client        *enigma2.Client
	FFmpegBin       string
	HLSRoot         string
	TuneTimeout     time.Duration
	KillTimeout     time.Duration
	AnalyzeDuration string
	ProbeSize       string
}

// NewRealFactory creates a RealFactory.
func NewRealFactory(e2Client *enigma2.Client, tuneTimeout time.Duration, ffmpegBin, hlsRoot string, killTimeout time.Duration, analyzeDuration, probeSize string) *RealFactory {
	return &RealFactory{
		E2Client:        e2Client,
		FFmpegBin:       ffmpegBin,
		HLSRoot:         hlsRoot,
		TuneTimeout:     tuneTimeout,
		KillTimeout:     killTimeout,
		AnalyzeDuration: analyzeDuration,
		ProbeSize:       probeSize,
	}
}

func (f *RealFactory) NewTuner(slot int) (Tuner, error) {
	return enigma2.NewTuner(f.E2Client, slot, f.TuneTimeout), nil
}

func (f *RealFactory) NewTranscoder() (Transcoder, error) {
	runner := ffmpeg.NewRunner(f.FFmpegBin, f.HLSRoot, f.E2Client, f.KillTimeout)
	runner.AnalyzeDuration = f.AnalyzeDuration
	runner.ProbeSize = f.ProbeSize
	return runner, nil
}
