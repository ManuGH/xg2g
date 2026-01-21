package decision

import "context"

// Mode represents the playback mode decision.
type Mode string

const (
	ModeDirectPlay   Mode = "direct_play"
	ModeDirectStream Mode = "direct_stream"
	ModeTranscode    Mode = "transcode"
	ModeDeny         Mode = "deny"
)

// Input contains all data needed for the decision engine.
type Input struct {
	Source       Source
	Capabilities Capabilities
	Policy       Policy
	APIVersion   string
	RequestID    string
}

// Source represents media truth (known container, codecs, etc.).
type Source struct {
	Container   string  `json:"container"`
	VideoCodec  string  `json:"videoCodec"`
	AudioCodec  string  `json:"audioCodec"`
	BitrateKbps int     `json:"bitrateKbps"`
	Width       int     `json:"width"`
	Height      int     `json:"height"`
	FPS         float64 `json:"fps"`
}

// MaxVideoDimensions defines video resolution limits.
type MaxVideoDimensions struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// Capabilities represents client capabilities.
type Capabilities struct {
	Version       int                 `json:"version"`
	Containers    []string            `json:"containers"`
	VideoCodecs   []string            `json:"videoCodecs"`
	AudioCodecs   []string            `json:"audioCodecs"`
	SupportsHLS   bool                `json:"supportsHls"`
	SupportsRange *bool               `json:"supportsRange"`
	MaxVideo      *MaxVideoDimensions `json:"maxVideo"`
	DeviceType    string              `json:"deviceType"`
}

// Policy represents server policy constraints.
type Policy struct {
	AllowTranscode bool `json:"allowTranscode"`
}

// Decision represents a successful playback decision (HTTP 200).
type Decision struct {
	Mode               Mode            `json:"mode"`
	Selected           SelectedFormats `json:"selected"`
	Outputs            []Output        `json:"outputs"`
	Constraints        []string        `json:"constraints"`
	Reasons            []ReasonCode    `json:"reasons"`
	Trace              Trace           `json:"trace"`
	SelectedOutputURL  string          `json:"selectedOutputUrl"`
	SelectedOutputKind string          `json:"selectedOutputKind"`
}

// SelectedFormats indicates the chosen container/codecs.
// For mode=deny, all fields MUST be "none" (sentinel, not null).
type SelectedFormats struct {
	Container  string `json:"container"`
	VideoCodec string `json:"videoCodec"`
	AudioCodec string `json:"audioCodec"`
}

// Output represents a playable output URL.
type Output struct {
	Kind string `json:"kind"` // "file", "hls"
	URL  string `json:"url"`
}

// Trace contains request tracing metadata.
type Trace struct {
	RequestID string `json:"requestId"`
}

// Problem represents an RFC7807 problem detail (non-200 responses).
type Problem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

// ProblemCode represents RFC7807 error codes.
type ProblemCode string

const (
	ProblemCapabilitiesMissing ProblemCode = "capabilities_missing"
	ProblemCapabilitiesInvalid ProblemCode = "capabilities_invalid"
	ProblemDecisionAmbiguous   ProblemCode = "decision_ambiguous"
	ProblemInvariantViolation  ProblemCode = "invariant_violation"
)

// Predicates contains pure boolean compatibility checks.
type Predicates struct {
	CanContainer         bool
	CanVideo             bool
	CanAudio             bool
	DirectPlayPossible   bool
	DirectStreamPossible bool
	TranscodeNeeded      bool
	TranscodePossible    bool
}

// Decide is the pure decision engine entry point.
// Returns (httpStatus, decision, problem). Exactly one of decision/problem is non-nil.
func Decide(ctx context.Context, input Input) (int, *Decision, *Problem) {
	// Start Decision Span (Correction 2: Owned by Decide)
	ctx, span := StartDecisionSpan(ctx)
	defer span.End()

	// Phase 1: Input validation (fail-closed)
	if prob := validateInput(input); prob != nil {
		// Observability (Phase 6a: Input Failure)
		EmitDecisionObs(ctx, input, nil, prob)
		return prob.Status, nil, prob
	}

	// Phase 2: Compute compatibility predicates
	pred := computePredicates(input.Source, input.Capabilities, input.Policy)

	// Phase 3: Decision table evaluation (first match wins)
	// (Returns Mode and ReasonCodes per ADR-P8)
	mode, reasons := evaluateDecision(pred, input.Capabilities, input.Policy)

	// Phase 4: Build decision response
	decision := buildDecision(mode, pred, input, reasons)

	// Phase 5: Output Invariants Enforcement (P8-3)
	// Stop-the-line: Normalize and validate to prevent semantic lies.
	normalizeDecision(decision)
	if err := validateOutputInvariants(decision, input); err != nil {
		prob := &Problem{
			Type:   "recordings/invariant-violation",
			Title:  "Invariant Violation",
			Status: 500,
			Code:   string(ProblemInvariantViolation),
			Detail: err.Error(),
		}
		// Observability (Phase 6b: Invariant Violation)
		EmitDecisionObs(ctx, input, nil, prob)
		return 500, nil, prob
	}

	// Phase 6: Observability (Success)
	EmitDecisionObs(ctx, input, decision, nil)

	return 200, decision, nil
}
