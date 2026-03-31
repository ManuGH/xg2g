package codec

import "fmt"

// ID identifies a normalized media codec token.
type ID string

const (
	IDUnknown ID = ""

	IDH264  ID = "h264"
	IDHEVC  ID = "hevc"
	IDAV1   ID = "av1"
	IDMPEG2 ID = "mpeg2"
	IDVP9   ID = "vp9"

	IDAAC  ID = "aac"
	IDAC3  ID = "ac3"
	IDEAC3 ID = "eac3"
	IDMP2  ID = "mp2"
	IDMP3  ID = "mp3"
)

// Resolution models a concrete width/height pair.
// A zero value means "unknown".
type Resolution struct {
	Width  int
	Height int
}

// CompareResult models a limit comparison without baking policy into the type.
// The caller decides whether Uncertain should be handled optimistically or
// conservatively.
type CompareResult uint8

const (
	Within CompareResult = iota
	Exceeds
	Uncertain
)

func (r Resolution) Known() bool {
	return r.Width > 0 && r.Height > 0
}

func (r Resolution) Compare(limit Resolution) CompareResult {
	if !r.Known() || !limit.Known() {
		return Uncertain
	}
	if r.Width > limit.Width || r.Height > limit.Height {
		return Exceeds
	}
	return Within
}

func (r Resolution) String() string {
	if !r.Known() {
		return "unknown"
	}
	return fmt.Sprintf("%dx%d", r.Width, r.Height)
}

// FrameRate models a rational FPS value.
// A zero value means "unknown".
type FrameRate struct {
	Numerator   int
	Denominator int
}

func (f FrameRate) Known() bool {
	return f.Numerator > 0 && f.Denominator > 0
}

func (f FrameRate) Compare(limit FrameRate) CompareResult {
	if !f.Known() || !limit.Known() {
		return Uncertain
	}
	if f.Numerator*limit.Denominator > limit.Numerator*f.Denominator {
		return Exceeds
	}
	return Within
}

func (f FrameRate) String() string {
	if !f.Known() {
		return "unknown"
	}
	if f.Denominator == 1 {
		return fmt.Sprintf("%dfps", f.Numerator)
	}
	return fmt.Sprintf("%d/%dfps", f.Numerator, f.Denominator)
}

// VideoCapability is the first typed replacement for the current string-only
// decision inputs. Keep this intentionally small until the parsers can fill it
// completely on every relevant path.
type VideoCapability struct {
	Codec        ID
	BitDepth     uint8
	Interlaced   bool
	MaxRes       Resolution
	MaxFrameRate FrameRate
}

func (v VideoCapability) HasKnownBitDepth() bool {
	return v.BitDepth > 0
}

func (v VideoCapability) HasKnownLimits() bool {
	return v.MaxRes.Known() || v.MaxFrameRate.Known()
}

// CompatibilityReason explains one concrete incompatibility or uncertainty.
// Multiple reasons may be present for a single comparison.
type CompatibilityReason string

const (
	ReasonCodecMismatch      CompatibilityReason = "codec_mismatch"
	ReasonBitDepthExceeded   CompatibilityReason = "bit_depth_exceeded"
	ReasonInterlacedSource   CompatibilityReason = "interlaced_source"
	ReasonResolutionExceeded CompatibilityReason = "resolution_exceeded"
	ReasonFrameRateExceeded  CompatibilityReason = "frame_rate_exceeded"

	ReasonBitDepthUnknown   CompatibilityReason = "bit_depth_unknown"
	ReasonResolutionUnknown CompatibilityReason = "resolution_unknown"
	ReasonFrameRateUnknown  CompatibilityReason = "frame_rate_unknown"
)

// CompatibilityResult models a comparison result as a set of reasons instead of
// a single boolean. The decision layer can derive the minimal repair plan from
// this structure in a later migration step.
type CompatibilityResult struct {
	Reasons []CompatibilityReason
}

func (r CompatibilityResult) Compatible() bool {
	return len(r.Reasons) == 0
}

func (r CompatibilityResult) Has(reason CompatibilityReason) bool {
	for _, existing := range r.Reasons {
		if existing == reason {
			return true
		}
	}
	return false
}

func (r *CompatibilityResult) Add(reason CompatibilityReason) {
	if r == nil || reason == "" || r.Has(reason) {
		return
	}
	r.Reasons = append(r.Reasons, reason)
}
