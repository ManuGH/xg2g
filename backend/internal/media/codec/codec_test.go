package codec

import "testing"

func TestResolutionCompare(t *testing.T) {
	source := Resolution{Width: 3840, Height: 2160}
	limit := Resolution{Width: 1920, Height: 1080}
	if got := source.Compare(limit); got != Exceeds {
		t.Fatalf("expected %s to exceed %s, got %v", source, limit, got)
	}
	if got := source.Compare(Resolution{}); got != Uncertain {
		t.Fatalf("unknown limit must be uncertain, got %v", got)
	}
	if got := (Resolution{Width: 1280, Height: 720}).Compare(limit); got != Within {
		t.Fatalf("expected lower resolution to stay within limit, got %v", got)
	}
}

func TestFrameRateCompare(t *testing.T) {
	source := FrameRate{Numerator: 60000, Denominator: 1001}
	limit := FrameRate{Numerator: 30000, Denominator: 1001}
	if got := source.Compare(limit); got != Exceeds {
		t.Fatalf("expected %s to exceed %s, got %v", source, limit, got)
	}
	if got := source.Compare(FrameRate{}); got != Uncertain {
		t.Fatalf("unknown limit must be uncertain, got %v", got)
	}
	if got := (FrameRate{Numerator: 25, Denominator: 1}).Compare(FrameRate{Numerator: 50, Denominator: 1}); got != Within {
		t.Fatalf("expected lower frame rate to stay within limit, got %v", got)
	}
}

func TestCompatibilityResultDedupesReasons(t *testing.T) {
	var result CompatibilityResult
	result.Add(ReasonCodecMismatch)
	result.Add(ReasonCodecMismatch)
	result.Add(ReasonResolutionExceeded)

	if got, want := len(result.Reasons), 2; got != want {
		t.Fatalf("expected %d reasons, got %d", want, got)
	}
	if !result.Has(ReasonCodecMismatch) {
		t.Fatalf("expected codec mismatch reason to be present")
	}
	if result.Compatible() {
		t.Fatalf("non-empty reason set must not be marked compatible")
	}
}

func TestVideoCapabilityKnownHelpers(t *testing.T) {
	capability := VideoCapability{
		Codec:        IDHEVC,
		BitDepth:     10,
		Interlaced:   true,
		MaxRes:       Resolution{Width: 1920, Height: 1080},
		MaxFrameRate: FrameRate{Numerator: 50, Denominator: 1},
	}

	if !capability.HasKnownBitDepth() {
		t.Fatalf("expected bit depth to be known")
	}
	if !capability.HasKnownLimits() {
		t.Fatalf("expected capability limits to be known")
	}
}
