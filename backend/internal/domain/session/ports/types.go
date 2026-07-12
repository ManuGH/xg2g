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
	SessionID string
	Mode      StreamMode
	Format    StreamFormat
	Quality   QualityProfile
	Source    StreamSource
	Profile   ProfileSpec // Transcoding profile (GPU, codec, quality knobs)
}

// ProfileSpec is data-driven and future-proof (VisionOS, embedded clients, etc.).
type ProfileSpec struct {
	Name                   string            `json:"name"`
	PolicyModeHint         RuntimeMode       `json:"policyModeHint,omitempty"`
	EffectiveRuntimeMode   RuntimeMode       `json:"effectiveRuntimeMode,omitempty"`
	EffectiveModeSource    RuntimeModeSource `json:"effectiveModeSource,omitempty"`
	LLHLS                  bool              `json:"llhls"`
	DVRWindowSec           int               `json:"dvrWindowSec"`
	EnableMultiAudio       bool              `json:"enableMultiAudio"`
	VOD                    bool              `json:"vod,omitempty"`
	DisableSafariForceCopy bool              `json:"disableSafariForceCopy,omitempty"`
	ForceSafariHQ25        bool              `json:"forceSafariHq25,omitempty"`
	TranscodeVideo         bool              `json:"transcodeVideo"`
	VideoCodec             string            `json:"videoCodec,omitempty"` // "h264" (default) or "hevc"
	HWAccel                string            `json:"hwAccel,omitempty"`    // "vaapi", "vaapi_encode_only", "qsv", "nvenc", etc.
	Deinterlace            bool              `json:"deinterlace,omitempty"`
	VideoCRF               int               `json:"videoCrf,omitempty"`
	VideoQP                int               `json:"videoQp,omitempty"`
	VideoMaxWidth          int               `json:"videoMaxWidth,omitempty"`
	VideoSourceHeight      int               `json:"videoSourceHeight,omitempty"` // scanned source height; drives resolution-aware bitrate budgeting
	VideoMaxRateK          int               `json:"videoMaxRateK,omitempty"`
	VideoBufSizeK          int               `json:"videoBufSizeK,omitempty"`
	BFrames                int               `json:"bframes,omitempty"`
	AudioBitrateK          int               `json:"audioBitrateK,omitempty"`
	Preset                 string            `json:"preset,omitempty"`
	Container              string            `json:"container,omitempty"` // "ts" (default) or "fmp4"
	SourceTruthVerified    bool              `json:"sourceTruthVerified,omitempty"`
	SourceVideoCodec       string            `json:"sourceVideoCodec,omitempty"`
	SourceAudioCodec       string            `json:"sourceAudioCodec,omitempty"`
	SourceFPS              float64           `json:"sourceFps,omitempty"`
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
