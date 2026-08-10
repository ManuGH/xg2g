package ports

import (
	"time"
)

// StreamMode defines the intent of the stream.
type StreamMode string

const (
	ModeLive      StreamMode = "live"
	ModeRecording StreamMode = "recording"
)

// StreamFormat defines the output packaging format.
type StreamFormat string

const (
	FormatHLS  StreamFormat = "hls"
	FormatDASH StreamFormat = "dash" // Future proofing
)

// QualityProfile defines the logical quality tier.
type QualityProfile string

const (
	QualityLow         QualityProfile = "low"
	QualityStandard    QualityProfile = "standard"
	QualityHigh        QualityProfile = "high"
	QualityPassthrough QualityProfile = "passthrough"
)

// RuntimeMode describes the effective playback strategy for a session.
type RuntimeMode string

const (
	RuntimeModeUnknown      RuntimeMode = "unknown"
	RuntimeModeCopy         RuntimeMode = "copy"
	RuntimeModeCopyHardened RuntimeMode = "copy_hardened"
	RuntimeModeHQ25         RuntimeMode = "hq25"
	RuntimeModeHQ50         RuntimeMode = "hq50"
	RuntimeModeSafe         RuntimeMode = "safe"
)

// RuntimeModeSource explains which layer selected the effective runtime mode.
type RuntimeModeSource string

const (
	RuntimeModeSourceUnknown          RuntimeModeSource = "unknown"
	RuntimeModeSourceResolve          RuntimeModeSource = "resolve"
	RuntimeModeSourceEnvOverride      RuntimeModeSource = "env_override"
	RuntimeModeSourceFeedbackFallback RuntimeModeSource = "feedback_fallback"
	RuntimeModeSourceRuntimeHardening RuntimeModeSource = "runtime_hardening"
)

// SourceType defines the nature of the media source.
type SourceType string

const (
	SourceTuner SourceType = "tuner"
	SourceFile  SourceType = "file"
	SourceURL   SourceType = "url"
)

// StreamSource represents the abstract source of the media.
// It hides the details of whether it's a tuner, a file, or a network stream.
type StreamSource struct {
	// ID is the unique identifier for the source (e.g. Channel Reference).
	ID string
	// Type indicates the nature of the source.
	Type SourceType
	// TunerSlot is the hardware slot info (Lease), or -1 if not applicable.
	TunerSlot int
}

// StreamSpec fully describes a media session request without implementation details.
type StreamSpec struct {
	SessionID    string
	ClientFamily string
	Mode         StreamMode
	Format       StreamFormat
	Quality      QualityProfile
	Source       StreamSource
	Profile      ProfileSpec // Transcoding profile (GPU, codec, quality knobs)

	// PrepareDeadline bounds the work an adapter does BEFORE it spawns the media
	// process: tuning, URL resolution, preflight, and the plan probes. Zero means
	// unbounded.
	//
	// It never bounds the process itself. The spawned pipeline keeps the caller's
	// long-lived context, because a deadline that reached the process would kill
	// the very stream the preparation was for.
	PrepareDeadline time.Time

	// ReadyDeadline is when the caller stops waiting for this start to produce a
	// playable stream. Zero means it does not stop.
	//
	// Adapters clamp their own startup watchdogs to it so the watchdog — which
	// knows WHY nothing arrived — fires before the caller's timeout, which only
	// knows THAT nothing arrived. A watchdog that can only fire after the caller
	// has already given up is unreachable code that costs the session its
	// diagnosis.
	ReadyDeadline time.Time
}

// ProfileSpec is data-driven and future-proof (VisionOS, embedded clients, etc.).
type ProfileSpec struct {
	Name                   string            `json:"name"`
	PolicyModeHint         RuntimeMode       `json:"policyModeHint,omitempty"`
	EffectiveRuntimeMode   RuntimeMode       `json:"effectiveRuntimeMode,omitempty"`
	EffectiveModeSource    RuntimeModeSource `json:"effectiveModeSource,omitempty"`
	LLHLS                  bool              `json:"llhls"`
	DVRWindowSec           int               `json:"dvrWindowSec"`
	VOD                    bool              `json:"vod,omitempty"`
	DisableSafariForceCopy bool              `json:"disableSafariForceCopy,omitempty"`
	ForceSafariHQ25        bool              `json:"forceSafariHq25,omitempty"`
	// PlannerBound marks profiles materialized from a verified immutable
	// PlaybackPlan. Execution may translate them into encoder arguments but must
	// not run legacy profile-selection or runtime-hardening policy again.
	PlannerBound   bool   `json:"plannerBound,omitempty"`
	TranscodeVideo bool   `json:"transcodeVideo"`
	VideoCodec     string `json:"videoCodec,omitempty"` // "h264" (default) or "hevc"
	// AudioMode is explicit for planner-issued profiles. Empty preserves the
	// legacy rule: audio transcodes whenever video transcodes, otherwise copies.
	AudioMode         string `json:"audioMode,omitempty"`  // "copy" or "transcode"
	AudioCodec        string `json:"audioCodec,omitempty"` // target codec when AudioMode=transcode
	HWAccel           string `json:"hwAccel,omitempty"`    // "vaapi", "vaapi_encode_only", "qsv", "nvenc", etc.
	Deinterlace       bool   `json:"deinterlace,omitempty"`
	VideoCRF          int    `json:"videoCrf,omitempty"`
	VideoQP           int    `json:"videoQp,omitempty"`
	VideoMaxWidth     int    `json:"videoMaxWidth,omitempty"`
	VideoSourceHeight int    `json:"videoSourceHeight,omitempty"` // scanned source height; drives resolution-aware bitrate budgeting
	VideoSourceCodec  string `json:"videoSourceCodec,omitempty"`  // scanned source codec; decides whether GPU decode is available for this input
	VideoTargetRateK  int    `json:"videoTargetRateK,omitempty"`
	VideoMaxRateK     int    `json:"videoMaxRateK,omitempty"`
	VideoBufSizeK     int    `json:"videoBufSizeK,omitempty"`
	BFrames           int    `json:"bframes,omitempty"`
	AudioBitrateK     int    `json:"audioBitrateK,omitempty"`
	Preset            string `json:"preset,omitempty"`
	Container         string `json:"container,omitempty"` // "ts" (default) or "fmp4"
}

func (p ProfileSpec) TranscodesAudio() bool {
	switch p.AudioMode {
	case "transcode":
		return true
	case "copy":
		return false
	default:
		return p.TranscodeVideo
	}
}

func (p ProfileSpec) ResolvedAudioCodec() string {
	if !p.TranscodesAudio() {
		return ""
	}
	if p.AudioCodec != "" {
		return p.AudioCodec
	}
	return "aac"
}

// RunHandle is an opaque token for a running pipeline.
type RunHandle string

// RuntimeDiagnostics is a live snapshot of encoder/source health reported by
// the active media pipeline.
type RuntimeDiagnostics struct {
	FrameCount           int     `json:"frameCount,omitempty"`
	FPS                  float64 `json:"fps,omitempty"`
	DropFrames           int     `json:"dropFrames,omitempty"`
	DupFrames            int     `json:"dupFrames,omitempty"`
	Speed                float64 `json:"speed,omitempty"`
	CorruptDecodedFrames int     `json:"corruptDecodedFrames,omitempty"`
	LastWarning          string  `json:"lastWarning,omitempty"`
	UpdatedAtUnix        int64   `json:"updatedAtUnix,omitempty"`
}

func (d RuntimeDiagnostics) IsZero() bool {
	return d.FrameCount == 0 &&
		d.FPS == 0 &&
		d.DropFrames == 0 &&
		d.DupFrames == 0 &&
		d.Speed == 0 &&
		d.CorruptDecodedFrames == 0 &&
		d.LastWarning == "" &&
		d.UpdatedAtUnix == 0
}

// HealthStatus indicates the operational state of the pipeline.
type HealthStatus struct {
	Healthy     bool
	Message     string
	LastCheck   time.Time
	Diagnostics RuntimeDiagnostics
}
