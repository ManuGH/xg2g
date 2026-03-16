package decision

import "github.com/ManuGH/xg2g/internal/domain/playbackprofile"

// Mode represents the playback mode decision.
type Mode string

const (
	ModeDirectPlay   Mode = "direct_play"
	ModeDirectStream Mode = "direct_stream"
	ModeTranscode    Mode = "transcode"
	ModeDeny         Mode = "deny"
)

// DecisionInput contains all data needed for the decision engine.
// ADR-009.2: Uses compact tags by default, but supports verbose tags via UnmarshalJSON.
type DecisionInput struct {
	Source          Source                         `json:"source"`
	Capabilities    Capabilities                   `json:"caps"`
	Policy          Policy                         `json:"policy"`
	RequestedIntent playbackprofile.PlaybackIntent `json:"intent,omitempty"`
	APIVersion      string                         `json:"api"`
	RequestID       string                         `json:"rid,omitempty"`
}

// Source represents media truth (known container, codecs, etc.).
type Source struct {
	Container   string  `json:"c"`
	VideoCodec  string  `json:"v"`
	AudioCodec  string  `json:"a"`
	BitrateKbps int     `json:"br"`
	Width       int     `json:"w"`
	Height      int     `json:"h"`
	FPS         float64 `json:"fps"`
}

// MaxVideoDimensions defines video resolution limits.
type MaxVideoDimensions struct {
	Width  int `json:"w"`
	Height int `json:"h"`
}

// Capabilities represents client capabilities.
type Capabilities struct {
	Version       int                 `json:"v"`
	Containers    []string            `json:"c"`
	VideoCodecs   []string            `json:"vc"`
	AudioCodecs   []string            `json:"ac"`
	SupportsHLS   bool                `json:"hls"`
	SupportsRange *bool               `json:"rng,omitempty"`
	MaxVideo      *MaxVideoDimensions `json:"mv,omitempty"`
	DeviceType    string              `json:"dev"`
}

// Policy represents server policy constraints.
type Policy struct {
	AllowTranscode bool           `json:"tx"`
	Operator       OperatorPolicy `json:"operator,omitempty"`
	Host           HostPolicy     `json:"host,omitempty"`
}

type OperatorPolicy struct {
	ForceIntent    playbackprofile.PlaybackIntent `json:"forceIntent,omitempty"`
	MaxQualityRung playbackprofile.QualityRung    `json:"maxQualityRung,omitempty"`
}

type HostPolicy struct {
	PressureBand playbackprofile.HostPressureBand `json:"pressureBand,omitempty"`
}

// Decision represents a successful playback decision (HTTP 200).
type Decision struct {
	Mode               Mode                                   `json:"mode"`
	Selected           SelectedFormats                        `json:"selected"`
	Outputs            []Output                               `json:"outputs"`
	TargetProfile      *playbackprofile.TargetPlaybackProfile `json:"targetProfile,omitempty"`
	Constraints        []string                               `json:"constraints"`
	Reasons            []ReasonCode                           `json:"reasons"`
	Trace              Trace                                  `json:"trace"`
	SelectedOutputURL  string                                 `json:"selectedOutputUrl"`
	SelectedOutputKind string                                 `json:"selectedOutputKind"`
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
// Structured to ensure low-cardinality observability.
type Trace struct {
	RequestID           string   `json:"requestId"`
	InputHash           string   `json:"inputHash"` // SHA-256 of canonical input
	RequestedIntent     string   `json:"requestedIntent,omitempty"`
	ResolvedIntent      string   `json:"resolvedIntent,omitempty"`
	QualityRung         string   `json:"qualityRung,omitempty"`
	AudioQualityRung    string   `json:"audioQualityRung,omitempty"`
	VideoQualityRung    string   `json:"videoQualityRung,omitempty"`
	DegradedFrom        string   `json:"degradedFrom,omitempty"`
	ForcedIntent        string   `json:"forcedIntent,omitempty"`
	MaxQualityRung      string   `json:"maxQualityRung,omitempty"`
	OverrideApplied     bool     `json:"overrideApplied,omitempty"`
	HostPressureBand    string   `json:"hostPressureBand,omitempty"`
	HostOverrideApplied bool     `json:"hostOverrideApplied,omitempty"`
	RuleHits            []string `json:"ruleHits"` // Ordered list of rules evaluated
	Why                 []Reason `json:"why"`      // Structured explanation
}

// Reason provides structured explanation for decisions.
type Reason struct {
	Code ReasonCode        `json:"code"`
	Meta map[string]string `json:"meta,omitempty"`
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
