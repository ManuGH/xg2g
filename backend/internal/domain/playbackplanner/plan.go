package playbackplanner

// PlaybackPlan represents an immutable playback decision.
type PlaybackPlan struct {
	Mode           string // "direct_stream", "transcode", "deny"
	DeliveryEngine string // "hls", "dash", etc.
	Codecs         Codecs
	Packaging      Packaging
	RateControl    RateControl
	Filters        Filters
	ProbeReqs      ProbeReqs
	Guardrails     Guardrails
}

// PlanTrace records the sequence of rules and decisions that led to the plan.
type PlanTrace struct {
	PlannerVersion string
	PolicyVersion  string
	EvidenceHash   string
	Log            []string
}

// Codecs defines the chosen codecs for the playback session.
type Codecs struct {
	Video string // "copy" or specific codec like "h264"
	Audio string // "copy" or specific codec like "aac"
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

// Guardrails defines static permitted runtime transitions for the session manager.
type Guardrails struct {
	PermittedAlternativePlans []string
	MinQualityRung            string
	MaxQualityRung            string
	AllowProbeUp              bool
	DecodeRisk                string // "soft", "hard"
}
