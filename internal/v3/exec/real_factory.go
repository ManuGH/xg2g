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
	E2Host      string
	FFmpegBin   string
	HLSRoot     string
	TuneTimeout time.Duration
}

// NewRealFactory creates a RealFactory.
func NewRealFactory(e2Host string, timeout time.Duration, ffmpegBin, hlsRoot string) *RealFactory {
	return &RealFactory{
		E2Client:    enigma2.NewClient(e2Host, timeout),
		E2Host:      e2Host,
		FFmpegBin:   ffmpegBin,
		HLSRoot:     hlsRoot,
		TuneTimeout: timeout,
	}
}

func (f *RealFactory) NewTuner(slot int) (Tuner, error) {
	return enigma2.NewTuner(f.E2Client, slot, f.TuneTimeout), nil
}

func (f *RealFactory) NewTranscoder() (Transcoder, error) {
	// E2Host is private in Client?
	// RealFactory struct has E2Client.
	// I need E2Host string.
	// I passed e2Host to NewRealFactory. I should store it?
	// Ah, I need to add E2Host string to RealFactory struct or expose it from Client.
	// Let's look at real_factory.go again.
	// It has E2Client, FFmpegBin, HLSRoot.
	// I also passed e2Host to constructor.
	// I'll add E2Host field to struct.
	return ffmpeg.NewRunner(f.FFmpegBin, f.HLSRoot, f.E2Client), nil
}
