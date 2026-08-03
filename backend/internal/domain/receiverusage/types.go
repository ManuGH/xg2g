// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package receiverusage

import (
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
)

type AccessCapacityClass string

const (
	AccessCapacityNone       AccessCapacityClass = "NONE"
	AccessCapacityRestricted AccessCapacityClass = "RESTRICTED"
	AccessCapacityUnknown    AccessCapacityClass = "UNKNOWN"
)

type EvidenceConfidence string

const (
	ConfidenceVerified   EvidenceConfidence = "VERIFIED"
	ConfidenceUnverified EvidenceConfidence = "UNVERIFIED"
	ConfidenceUnknown    EvidenceConfidence = "UNKNOWN"
)

type AccessClassification struct {
	Class      AccessCapacityClass
	Confidence EvidenceConfidence
	Source     string
	ObservedAt time.Time
}

type SessionIntent string

const (
	IntentLive               SessionIntent = "LIVE"
	IntentRecording          SessionIntent = "RECORDING"
	IntentTimeshift          SessionIntent = "TIMESHIFT"
	IntentRetroDVR           SessionIntent = "RETRO_DVR"
	IntentPictureInPicture   SessionIntent = "PIP"
	IntentFastChannelPreview SessionIntent = "FAST_CHANNEL_PREVIEW"
)

type UnknownAccessHandling string

const (
	UnknownAccessCountAsRestricted UnknownAccessHandling = "COUNT_AS_RESTRICTED"
	UnknownAccessCountAsNone       UnknownAccessHandling = "COUNT_AS_NONE"
	UnknownAccessReject            UnknownAccessHandling = "REJECT"
)

type RetroDVRSourceType string

const (
	RetroSourceExistingBuffer RetroDVRSourceType = "EXISTING_BUFFER"
	RetroSourceReceiverFetch  RetroDVRSourceType = "RECEIVER_FETCH"
)

type SourceIdentity struct {
	ReceiverID       string
	ServiceReference string
	TransponderID    string
	AccessContextID  string
}

type LeaseRequirementKind string

const (
	ReqRestrictedAccessSlot LeaseRequirementKind = "RESTRICTED_ACCESS_SLOT"
	ReqTunerSlot            LeaseRequirementKind = "TUNER_SLOT"
)

type LeaseRequirement struct {
	Kind       LeaseRequirementKind
	ReceiverID string
	Quantity   int
}

type UsageDecisionKind string

const (
	DecisionAllow        UsageDecisionKind = "ALLOW"
	DecisionReuseSession UsageDecisionKind = "REUSE_SESSION"
	DecisionReject       UsageDecisionKind = "REJECT"
)

type UsageDecision struct {
	Kind           UsageDecisionKind
	Reason         model.ReasonCode
	Message        string
	ReuseSessionID string
	Requirements   []LeaseRequirement
	Classification AccessClassification
}

type UsageRequest struct {
	ReceiverID  string
	Owner       string
	Intent      SessionIntent
	Source      SourceIdentity
	Access      AccessClassification
	RetroSource RetroDVRSourceType
	RequestedAt time.Time
}

type ActiveSessionSnapshot struct {
	SessionID        string
	ReceiverID       string
	Owner            string
	Intent           SessionIntent
	ServiceReference string
	IsRestricted     bool
	StartedAt        time.Time
}

type SystemSnapshot struct {
	ActiveSessions []ActiveSessionSnapshot
}
