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
	Name           string `json:"name"`
	LLHLS          bool   `json:"llhls"`
	DVRWindowSec   int    `json:"dvrWindowSec"`
	VOD            bool   `json:"vod,omitempty"`
	TranscodeVideo bool   `json:"transcodeVideo"`
	VideoCodec     string `json:"videoCodec,omitempty"` // "h264" (default) or "hevc"
	HWAccel        string `json:"hwAccel,omitempty"`    // "vaapi", "vaapi_encode_only", "qsv", "nvenc", etc.
	Deinterlace    bool   `json:"deinterlace,omitempty"`
	VideoCRF       int    `json:"videoCrf,omitempty"`
	VideoMaxWidth  int    `json:"videoMaxWidth,omitempty"`
	VideoMaxRateK  int    `json:"videoMaxRateK,omitempty"`
	VideoBufSizeK  int    `json:"videoBufSizeK,omitempty"`
	BFrames        int    `json:"bframes,omitempty"`
	AudioBitrateK  int    `json:"audioBitrateK,omitempty"`
	Preset         string `json:"preset,omitempty"`
	Container      string `json:"container,omitempty"` // "ts" (default) or "fmp4"
}

// RunHandle is an opaque token for a running pipeline.
type RunHandle string

// HealthStatus indicates the operational state of the pipeline.
type HealthStatus struct {
	Healthy   bool
	Message   string
	LastCheck time.Time
}
