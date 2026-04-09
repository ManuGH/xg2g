package recordings

import (
	"github.com/ManuGH/xg2g/internal/control/playback"
	"github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
	"github.com/ManuGH/xg2g/internal/control/recordings/decision"
)

type PlaybackSubjectKind string

const (
	PlaybackSubjectRecording PlaybackSubjectKind = "recording"
	PlaybackSubjectLive      PlaybackSubjectKind = "live"
)

// PlaybackInfoRequest is the transport-neutral request payload for recording/live playback decisions.
type PlaybackInfoRequest struct {
	SubjectID        string
	SubjectKind      PlaybackSubjectKind
	APIVersion       string
	SchemaType       string
	RequestedProfile string
	PrincipalID      string
	RequestID        string
	ClientProfile    string
	Headers          map[string]string
	Capabilities     *capabilities.PlaybackCapabilities
}

// PlaybackInfoResult holds the domain-level inputs and outputs needed by the HTTP adapter.
type PlaybackInfoResult struct {
	SourceRef            string
	Truth                playback.MediaTruth
	ResolvedCapabilities capabilities.PlaybackCapabilities
	Decision             *decision.Decision
	ClientProfile        string
	OperatorRuleName     string
	OperatorRuleScope    string
}

type PlaybackInfoProblem struct {
	Status int
	Type   string
	Title  string
	Code   string
	Detail string
}

type PlaybackInfoErrorKind uint8

const (
	PlaybackInfoErrorUnavailable PlaybackInfoErrorKind = iota
	PlaybackInfoErrorInvalidInput
	PlaybackInfoErrorForbidden
	PlaybackInfoErrorNotFound
	PlaybackInfoErrorPreparing
	PlaybackInfoErrorUnverified
	PlaybackInfoErrorUnsupported
	PlaybackInfoErrorUpstreamUnavailable
	PlaybackInfoErrorInternal
	PlaybackInfoErrorProblem
)

// PlaybackInfoError captures transport-neutral failures from playback info resolution.
type PlaybackInfoError struct {
	Kind              PlaybackInfoErrorKind
	Message           string
	RetryAfterSeconds int
	ProbeState        string
	TruthState        string
	TruthReason       string
	TruthOrigin       string
	ProblemFlags      []string
	Problem           *PlaybackInfoProblem
	Cause             error
}

func (e *PlaybackInfoError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Problem != nil && e.Problem.Detail != "" {
		return e.Problem.Detail
	}
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return "playback info error"
}
