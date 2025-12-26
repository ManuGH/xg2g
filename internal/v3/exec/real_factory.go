// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package exec

import (
	"time"

	"github.com/ManuGH/xg2g/internal/v3/exec/enigma2"
	"github.com/ManuGH/xg2g/internal/v3/exec/ffmpeg"
)

// RealFactory produces real execution components (Enigma2 Tuner, Real/Stub Transcoder).
type RealFactory struct {
	E2Client    *enigma2.Client
	FFmpegBin   string
	HLSRoot     string
	TuneTimeout time.Duration
}

// NewRealFactory creates a RealFactory.
func NewRealFactory(e2Host string, tuneTimeout time.Duration, ffmpegBin, hlsRoot string, e2Opts enigma2.Options) *RealFactory {
	return &RealFactory{
		E2Client:    enigma2.NewClientWithOptions(e2Host, e2Opts),
		FFmpegBin:   ffmpegBin,
		HLSRoot:     hlsRoot,
		TuneTimeout: tuneTimeout,
	}
}

func (f *RealFactory) NewTuner(slot int) (Tuner, error) {
	return enigma2.NewTuner(f.E2Client, slot, f.TuneTimeout), nil
}

func (f *RealFactory) NewTranscoder() (Transcoder, error) {
	return ffmpeg.NewRunner(f.FFmpegBin, f.HLSRoot, f.E2Client), nil
}
