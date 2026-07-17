package recordings

import (
	"strings"

	"github.com/ManuGH/xg2g/internal/control/playback"
	"github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
	"github.com/ManuGH/xg2g/internal/control/recordings/decision"
	"github.com/ManuGH/xg2g/internal/domain/playbackplanner"
)

type PlaybackSubjectKind string

const (
	PlaybackSubjectRecording PlaybackSubjectKind = "recording"
	PlaybackSubjectLive      PlaybackSubjectKind = "live"
)

const (
	PlaybackInfoContextHeader      = "X-XG2G-Playback-Info-Context"
	PlaybackInfoContextPlayerStart = "player_start"
	PlaybackInfoContextEpgBadge    = "epg_badge"
)

func NormalizePlaybackInfoRequestContext(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case PlaybackInfoContextPlayerStart:
		return PlaybackInfoContextPlayerStart
	case PlaybackInfoContextEpgBadge:
		return PlaybackInfoContextEpgBadge
	default:
		return ""
	}
}

func PlaybackInfoRequestContext(req PlaybackInfoRequest) string {
	if req.Headers == nil {
		return ""
	}
	for key, value := range req.Headers {
		if strings.EqualFold(key, PlaybackInfoContextHeader) {
			return NormalizePlaybackInfoRequestContext(value)
		}
	}
	return ""
}

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

// PlannerEvaluation holds the paired evidence snapshot and resulting plan.
type PlannerEvaluation struct {
	Evidence playbackplanner.PlaybackEvidence
	Result   playbackplanner.PlanningResult
}

// PlaybackInfoResult holds the domain-level inputs and outputs needed by the HTTP adapter.
type PlaybackInfoResult struct {
	SourceRef                 string
	Truth                     playback.MediaTruth
	ResolvedCapabilities      capabilities.PlaybackCapabilities
	Decision                  *decision.Decision
	ClientProfile             string
	OperatorRuleName          string
	OperatorRuleScope         string
	RuntimePolicyAction       string
	RuntimePolicyPhase        string
	RuntimeProbeCandidate     string
	RuntimePolicyReasons      []string
	RuntimePolicyConstraints  []string
	RuntimeProbeSuccessStreak int
	RuntimeProbeFailureStreak int
	// PlannerEvaluation holds the immutable evidence and plan evaluated by the new planner.
	PlannerEvaluation *PlannerEvaluation
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
