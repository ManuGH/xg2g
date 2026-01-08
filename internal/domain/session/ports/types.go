package ports

import "time"

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
}

// RunHandle is an opaque token for a running pipeline.
type RunHandle string

// HealthStatus indicates the operational state of the pipeline.
type HealthStatus struct {
	Healthy   bool
	Message   string
	LastCheck time.Time
}
