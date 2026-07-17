package playbackplanner

const (
	DecisionAllow = "allow"
	DecisionDeny  = "deny"

	ReasonVideoCodecUnsupportedForCopy = "video_codec_unsupported_for_copy"
	ReasonStaleOrPartialTruth          = "stale_or_partial_truth"
	ReasonScopeNotSeekable             = "scope_not_seekable"
	ReasonClientLacksRangeSupport      = "client_lacks_range_support"
	ReasonContainerIncompatible        = "container_incompatible"
	ReasonCodecIncompatible            = "codec_incompatible"
	ReasonInterlaceRepairRequired      = "interlace_repair_required"
	ReasonExceedsClientLimits          = "exceeds_client_limits"
	ReasonClientLacksHLSSupport        = "client_lacks_hls_support"
	ReasonPolicyDeniesTranscode        = "policy_denies_transcode"
	ReasonHLSNotSupported              = "hls_not_supported_by_client"
)

// PlaybackPlan represents an immutable playback decision.
type PlaybackPlan struct {
	Decision       string // DecisionAllow, DecisionDeny
	ReasonCode     string // E.g., ReasonVideoCodecUnsupportedForCopy
	Outcome        string // "allow", "deny" (legacy compatible)
	Mode           string // "copy", "remux", "transcode"
	DeliveryEngine string // "hls", "dash", etc.
	Video          TrackPlan
	Audio          TrackPlan
	Packaging      Packaging
	RateControl    RateControl
	Filters        Filters
	ProbeReqs      ProbeReqs
	Startup        StartupPlan
	Guardrails     Guardrails
}

type TrackPlan struct {
	Mode        string // "copy", "transcode", "disabled"
	Codec       string // e.g. "h264", "aac"
	BitrateKbps int
	Channels    int
	SampleRate  int
}

// PlanTrace records the sequence of rules and decisions that led to the plan.
type PlanTrace struct {
	PlannerVersion string
	PolicyVersion  string
	EvidenceHash   string
	Log            []RuleHit
}

// RuleHit represents a single policy evaluation outcome.
type RuleHit struct {
	Rule   string
	Result string
	Reason string
}

// Packaging defines how the media is segmented/muxed.
type Packaging struct {
	Container string // "ts", "fmp4", "mpegts"
}

// RateControl defines bandwidth/quality targets.
type RateControl struct {
	TargetVideoBitrateKbps int
	MaxVideoBitrateKbps    int
}

// Filters defines any necessary video/audio filters (e.g. deinterlace, scale).
type Filters struct {
	Deinterlace bool
	ScaleWidth  int
	ScaleHeight int
}

// ProbeReqs defines requirements for media probing before starting.
type ProbeReqs struct {
	RequireFullProbe bool
}

// StartupPlan contains immutable session-start semantics that must survive the
// playback-info to intent handoff without consulting mutable configuration.
type StartupPlan struct {
	DVRWindowSeconds int
}

// Guardrails defines static permitted runtime transitions for the session manager.
type Guardrails struct {
	PermittedAlternativePlans []string
	MinQualityRung            string
	MaxQualityRung            string
	AllowProbeUp              bool
	DecodeRisk                string // "soft", "hard"
}

func (p PlaybackPlan) cloneNormalized() PlaybackPlan {
	p.Guardrails.PermittedAlternativePlans = cloneDeduplicateSort(p.Guardrails.PermittedAlternativePlans)
	return p
}
