package playback

import "errors"

// --- Errors ---

var (
	ErrForbidden         = errors.New("forbidden")
	ErrNotFound          = errors.New("not found")
	ErrPreparing         = errors.New("preparing")
	ErrUpstream          = errors.New("upstream failed")
	ErrUnsupported       = errors.New("unsupported")
	ErrDecisionAmbiguous = errors.New("decision_ambiguous")
)

// --- Enums ---

// PlaybackMode defines the calculated strategy.
type PlaybackMode string

const (
	ModeDirectPlay   PlaybackMode = "direct_play"   // Client plays strict format directly
	ModeDirectStream PlaybackMode = "direct_stream" // Remux container only (no re-encode)
	ModeTranscode    PlaybackMode = "transcoder"    // Re-encode required
	ModeError        PlaybackMode = "error"         // Hard failure (e.g. not found)
)

// Protocol defines the transport protocol.
type Protocol string

const (
	ProtocolHLS Protocol = "hls"
	ProtocolMP4 Protocol = "mp4"
)

// ArtifactKind defines what kind of file/stream we point to.
type ArtifactKind string

const (
	ArtifactMP4  ArtifactKind = "mp4" // Single static file (e.g. stream.mp4)
	ArtifactHLS  ArtifactKind = "hls" // M3U8 Playlist (e.g. playlist.m3u8)
	ArtifactNone ArtifactKind = ""    // For errors
)

// ReasonCode defines strictly why a decision was made.
type ReasonCode string

const (
	ReasonDirectPlayMatch   ReasonCode = "directplay_supported"
	ReasonDirectStreamMatch ReasonCode = "directstream_remux"
	ReasonTranscodeVideo    ReasonCode = "transcode_video"
	ReasonTranscodeAudio    ReasonCode = "transcode_audio"
	ReasonTranscodeRequired ReasonCode = "transcode_required"
	ReasonProbeFailed       ReasonCode = "probe_failed"
	ReasonForceHLS          ReasonCode = "force_hls"
	ReasonSafariTSNeedsHLS  ReasonCode = "safari_ts_needs_hls"
	ReasonSafariDirectMP4   ReasonCode = "safari_direct_mp4"
	ReasonChromeDirectMP4   ReasonCode = "chrome_direct_mp4"
	ReasonUnknownContainer  ReasonCode = "unknown_container"
)

// --- Structs ---

type ResolveRequest struct {
	RecordingID  string
	ProtocolHint string            // "hls", "mp4", ""
	Headers      map[string]string // For ProfileResolver
}

type PlaybackPlan struct {
	Mode               PlaybackMode
	Protocol           Protocol
	Container          string
	VideoCodec         string
	AudioCodec         string
	DecisionReason     ReasonCode
	TruthReason        string
	Duration           float64
	DurationSource     string
	DurationConfidence string
	DurationReasons    []string
}

// MediaInfo represents pure facts about the recording (used inside PlaybackInfoResult domain).
// Note: PIDE uses MediaTruth internally, but Resolver exposes MediaInfo for DTO.
type MediaInfo struct {
	Container             string
	VideoCodec            string
	AudioCodec            string
	Duration              float64
	AbsPath               string
	IsMP4FastPathEligible bool
}

// MediaTruth represents the source of truth for the media.
type MediaTruth struct {
	State              string // "READY", "PREPARING", "FAILED"
	Container          string
	VideoCodec         string
	AudioCodec         string
	Duration           float64
	DurationSource     string
	DurationConfidence string
	DurationReasons    []string
	Width              int
	Height             int
	FPS                float64
	Interlaced         bool
}

// PlaybackCapabilities represents the core capability set for playback decisions.
// This struct is intended to be the domain truth, mapped to/from OpenAPI or shims.
type PlaybackCapabilities struct {
	CapabilitiesVersion int      `json:"capabilitiesVersion"`
	Containers          []string `json:"containers"`
	VideoCodecs         []string `json:"videoCodecs"`
	AudioCodecs         []string `json:"audioCodecs"`
	SupportsHLS         bool     `json:"supportsHls"`

	// DeviceType is optional but helpful for identity-bound profiles
	DeviceType string `json:"deviceType,omitempty"`

	// Allowed constraints ONLY (per ADR P7):
	AllowTranscode *bool     `json:"allowTranscode,omitempty"`
	MaxVideo       *MaxVideo `json:"maxVideo,omitempty"`
}

type MaxVideo struct {
	Width  int     `json:"width"`
	Height int     `json:"height"`
	FPS    float64 `json:"fps"`
}

const (
	StateReady     = "READY"
	StatePreparing = "PREPARING"
	StateFailed    = "FAILED"
)

// Decision represents the output of the engine.
type Decision struct {
	Mode     PlaybackMode
	Artifact ArtifactKind
	Reason   ReasonCode
}
