package exec

import (
	"time"

	"github.com/ManuGH/xg2g/internal/infra/ffmpeg"
	"github.com/ManuGH/xg2g/internal/pipeline/exec/enigma2"
	"github.com/rs/zerolog"
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
	Logger          zerolog.Logger
}

// NewRealFactory creates a RealFactory.
func NewRealFactory(e2Client *enigma2.Client, tuneTimeout time.Duration, ffmpegBin, hlsRoot string, killTimeout time.Duration, analyzeDuration, probeSize string, logger zerolog.Logger) *RealFactory {
	return &RealFactory{
		E2Client:        e2Client,
		FFmpegBin:       ffmpegBin,
		HLSRoot:         hlsRoot,
		TuneTimeout:     tuneTimeout,
		KillTimeout:     killTimeout,
		AnalyzeDuration: analyzeDuration,
		ProbeSize:       probeSize,
		Logger:          logger,
	}
}

func (f *RealFactory) NewTuner(slot int) (Tuner, error) {
	return enigma2.NewTuner(f.E2Client, slot, f.TuneTimeout), nil
}

func (f *RealFactory) NewTranscoder() (Transcoder, error) {
	// Use new infra/ffmpeg.Executor with adapter
	executor := ffmpeg.NewExecutor(f.FFmpegBin, f.Logger)
	adapter := newTranscoderAdapter(executor)
	return adapter, nil
}
