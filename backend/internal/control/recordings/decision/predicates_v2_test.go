package decision

import (
	"testing"

	mediacodec "github.com/ManuGH/xg2g/internal/media/codec"
)

func TestEvaluateVideoCompatibility_ExactMatch(t *testing.T) {
	result := EvaluateVideoCompatibility(
		mediacodec.VideoCapability{
			Codec:        mediacodec.IDH264,
			BitDepth:     8,
			MaxRes:       mediacodec.Resolution{Width: 1920, Height: 1080},
			MaxFrameRate: mediacodec.FrameRate{Numerator: 50, Denominator: 1},
		},
		mediacodec.VideoCapability{
			Codec:        mediacodec.IDH264,
			BitDepth:     8,
			MaxRes:       mediacodec.Resolution{Width: 1920, Height: 1080},
			MaxFrameRate: mediacodec.FrameRate{Numerator: 50, Denominator: 1},
		},
	)
	if !result.Compatible() {
		t.Fatalf("expected exact match to be compatible, got %+v", result.Reasons)
	}
}

func TestEvaluateVideoCompatibility_CodecMismatchShortCircuits(t *testing.T) {
	result := EvaluateVideoCompatibility(
		mediacodec.VideoCapability{Codec: mediacodec.IDHEVC, Interlaced: true},
		mediacodec.VideoCapability{Codec: mediacodec.IDH264, BitDepth: 8},
	)
	if !result.Has(mediacodec.ReasonCodecMismatch) {
		t.Fatalf("expected codec mismatch, got %+v", result.Reasons)
	}
	if got, want := len(result.Reasons), 1; got != want {
		t.Fatalf("expected codec mismatch to short-circuit, got %d reasons: %+v", got, result.Reasons)
	}
}

func TestEvaluateVideoCompatibility_ReportsInterlacedAndExceededLimits(t *testing.T) {
	result := EvaluateVideoCompatibility(
		mediacodec.VideoCapability{
			Codec:        mediacodec.IDH264,
			BitDepth:     10,
			Interlaced:   true,
			MaxRes:       mediacodec.Resolution{Width: 3840, Height: 2160},
			MaxFrameRate: mediacodec.FrameRate{Numerator: 60000, Denominator: 1001},
		},
		mediacodec.VideoCapability{
			Codec:        mediacodec.IDH264,
			BitDepth:     8,
			MaxRes:       mediacodec.Resolution{Width: 1920, Height: 1080},
			MaxFrameRate: mediacodec.FrameRate{Numerator: 30000, Denominator: 1001},
		},
	)
	for _, reason := range []mediacodec.CompatibilityReason{
		mediacodec.ReasonInterlacedSource,
		mediacodec.ReasonBitDepthExceeded,
		mediacodec.ReasonResolutionExceeded,
		mediacodec.ReasonFrameRateExceeded,
	} {
		if !result.Has(reason) {
			t.Fatalf("expected reason %q, got %+v", reason, result.Reasons)
		}
	}
}

func TestEvaluateVideoCompatibility_ReportsUncertainOnlyWhenClientAdvertisesALimit(t *testing.T) {
	result := EvaluateVideoCompatibility(
		mediacodec.VideoCapability{
			Codec:        mediacodec.IDH264,
			BitDepth:     8,
			MaxRes:       mediacodec.Resolution{},
			MaxFrameRate: mediacodec.FrameRate{},
		},
		mediacodec.VideoCapability{
			Codec:        mediacodec.IDH264,
			BitDepth:     8,
			MaxRes:       mediacodec.Resolution{Width: 1920, Height: 1080},
			MaxFrameRate: mediacodec.FrameRate{Numerator: 50, Denominator: 1},
		},
	)
	if !result.Has(mediacodec.ReasonResolutionUnknown) {
		t.Fatalf("expected resolution uncertainty, got %+v", result.Reasons)
	}
	if !result.Has(mediacodec.ReasonFrameRateUnknown) {
		t.Fatalf("expected frame rate uncertainty, got %+v", result.Reasons)
	}
}

func TestEvaluateVideoCompatibility_IgnoresUnknownSourceLimitsWhenClientHasNoLimit(t *testing.T) {
	result := EvaluateVideoCompatibility(
		mediacodec.VideoCapability{
			Codec:        mediacodec.IDH264,
			MaxRes:       mediacodec.Resolution{},
			MaxFrameRate: mediacodec.FrameRate{},
		},
		mediacodec.VideoCapability{
			Codec: mediacodec.IDH264,
		},
	)
	if !result.Compatible() {
		t.Fatalf("expected no limit on the client to stay compatible, got %+v", result.Reasons)
	}
}

func TestEvaluateVideoCompatibility_SurfacesUnknownBitDepth(t *testing.T) {
	result := EvaluateVideoCompatibility(
		mediacodec.VideoCapability{
			Codec: mediacodec.IDHEVC,
		},
		mediacodec.VideoCapability{
			Codec:    mediacodec.IDHEVC,
			BitDepth: 10,
		},
	)
	if !result.Has(mediacodec.ReasonBitDepthUnknown) {
		t.Fatalf("expected bit depth uncertainty, got %+v", result.Reasons)
	}
}
