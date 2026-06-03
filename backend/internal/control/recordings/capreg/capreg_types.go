package capreg

import (
	"context"
	"time"

	"github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
	"github.com/ManuGH/xg2g/internal/control/recordings/runtimepolicy"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
)

type EncoderCapability struct {
	Codec          string `json:"codec"`
	Verified       bool   `json:"verified"`
	AutoEligible   bool   `json:"autoEligible"`
	ProbeElapsedMS int64  `json:"probeElapsedMs,omitempty"`
}

type HostSnapshot struct {
	Identity            HostIdentity
	Runtime             playbackprofile.HostRuntimeSnapshot
	EncoderCapabilities []EncoderCapability
	UpdatedAt           time.Time
}

type decisionFingerprintInput struct {
	Version        string                          `json:"version"`
	HostClass      string                          `json:"hostClass,omitempty"`
	BenchmarkClass string                          `json:"benchmarkClass,omitempty"`
	ProfileKeys    []decisionFingerprintProfileKey `json:"profileKeys,omitempty"`
	OSName         string                          `json:"osName,omitempty"`
	OSVersion      string                          `json:"osVersion,omitempty"`
	Architecture   string                          `json:"architecture,omitempty"`
	EncoderKeys    []decisionFingerprintEncoderKey `json:"encoderKeys,omitempty"`
}

type decisionFingerprintEncoderKey struct {
	Codec string `json:"codec"`
}

type decisionFingerprintProfileKey struct {
	ProfileID string `json:"profileId"`
	Class     string `json:"class,omitempty"`
}

type ReceiverContext struct {
	Platform            string `json:"platform,omitempty"`
	Brand               string `json:"brand,omitempty"`
	Model               string `json:"model,omitempty"`
	OSName              string `json:"osName,omitempty"`
	OSVersion           string `json:"osVersion,omitempty"`
	KernelVersion       string `json:"kernelVersion,omitempty"`
	EnigmaVersion       string `json:"enigmaVersion,omitempty"`
	WebInterfaceVersion string `json:"webInterfaceVersion,omitempty"`
}

type SourceSnapshot struct {
	SubjectKind       string
	Origin            string
	Container         string
	VideoCodec        string
	AudioCodec        string
	BitrateConfidence string
	BitrateBucket     string
	Width             int
	Height            int
	FPS               float64
	SignalFPS         float64
	Interlaced        bool
	ProblemFlags      []string
	ReceiverContext   *ReceiverContext
	UpdatedAt         time.Time
}

type PlaybackObservation struct {
	ObservedAt         time.Time
	RequestID          string
	ObservationKind    string
	Outcome            string
	SessionID          string
	SourceRef          string
	SourceFingerprint  string
	SubjectKind        string
	RequestedIntent    string
	ResolvedIntent     string
	Mode               string
	SelectedContainer  string
	SelectedVideoCodec string
	SelectedAudioCodec string
	SourceWidth        int
	SourceHeight       int
	SourceFPS          float64
	HostFingerprint    string
	DeviceFingerprint  string
	ClientCapsHash     string
	Network            *capabilities.NetworkContext
	FeedbackEvent      string
	FeedbackCode       int
	FeedbackMessage    string
}

type FeedbackSummaryLookup interface {
	LookupRecentFeedbackSummary(ctx context.Context, query FeedbackSummaryQuery) (FeedbackSummary, bool, error)
}

type FeedbackObservationLookup interface {
	LookupRecentFeedbackObservations(ctx context.Context, query FeedbackSummaryQuery) ([]PlaybackObservation, error)
}

type PlaybackPolicyStateLookup interface {
	LookupPlaybackPolicyState(ctx context.Context, query PlaybackPolicyStateQuery) (PlaybackPolicyState, bool, error)
}

type PlaybackPolicyStateStore interface {
	RememberPlaybackPolicyState(ctx context.Context, state PlaybackPolicyState) error
}

type FeedbackSummaryQuery struct {
	SubjectKind       string
	SourceFingerprint string
	DeviceFingerprint string
	HostFingerprint   string
	Since             time.Time
	Limit             int
}

type PlaybackPolicyStateQuery struct {
	SubjectKind       string
	SourceFingerprint string
	DeviceFingerprint string
	HostFingerprint   string
}

type PlaybackPolicyState struct {
	SubjectKind       string
	SourceFingerprint string
	DeviceFingerprint string
	HostFingerprint   string
	MaxQualityRung    playbackprofile.QualityRung
	Confidence        runtimepolicy.ConfidenceSnapshot
	UpdatedAt         time.Time
}

type FeedbackSummary struct {
	LastObservedAt             time.Time
	SampleCount                int
	StartedCount               int
	WarningCount               int
	FailedCount                int
	ConsecutiveWarnings        int
	ConsecutiveBufferWarnings  int
	ConsecutiveDecodeWarnings  int
	ConsecutiveNetworkWarnings int
	ConsecutiveFailures        int
	ConsecutiveDecodeFailures  int
	ConsecutiveStallFailures   int
	PriorStartedStreak         int
	PriorRecoveredStartStreak  int
	PriorRecoveryStartCode     int
}
