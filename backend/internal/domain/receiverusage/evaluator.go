// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package receiverusage

import (
	"context"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/receivertopology"
)

type Evaluator interface {
	Evaluate(ctx context.Context, policy ReceiverUsagePolicy, req UsageRequest, snapshot SystemSnapshot) (UsageDecision, error)
}

type ReceiverUsageEvaluator struct {
	topologyService *receivertopology.Service
}

func NewEvaluator() *ReceiverUsageEvaluator {
	return &ReceiverUsageEvaluator{}
}

func NewEvaluatorWithTopology(topologyService *receivertopology.Service) *ReceiverUsageEvaluator {
	return &ReceiverUsageEvaluator{
		topologyService: topologyService,
	}
}

func (e *ReceiverUsageEvaluator) Evaluate(ctx context.Context, policy ReceiverUsagePolicy, req UsageRequest, snapshot SystemSnapshot) (UsageDecision, error) {
	if err := policy.Validate(); err != nil {
		return UsageDecision{}, err
	}

	if policy.Mode == "" || policy.Mode == ReceiverUsageModeDisabled {
		return UsageDecision{
			Kind:           DecisionAllow,
			Classification: req.Access,
			Message:        "receiver usage policy disabled",
		}, nil
	}

	decision := e.evaluateInternal(policy, req, snapshot)

	if policy.Mode == ReceiverUsageModeAuditOnly {
		decision.Kind = DecisionAllow
		decision.Requirements = nil
		decision.Message = "audit-only: " + decision.Message
	}

	return decision, nil
}

func (e *ReceiverUsageEvaluator) evaluateInternal(policy ReceiverUsagePolicy, req UsageRequest, snapshot SystemSnapshot) UsageDecision {
	for _, sess := range snapshot.ActiveSessions {
		if sess.ReceiverID == req.ReceiverID &&
			sess.ServiceReference == req.Source.ServiceReference &&
			sess.Intent == req.Intent &&
			sess.Owner == req.Owner {
			if policy.ChannelChangeLimiter.DuplicateWindow > 0 && req.RequestedAt.Sub(sess.StartedAt) <= policy.ChannelChangeLimiter.DuplicateWindow {
				return UsageDecision{
					Kind:           DecisionReuseSession,
					ReuseSessionID: sess.SessionID,
					Classification: req.Access,
				}
			}
		}
	}

	// 2. Classification & Fail-Closed UNKNOWN Resolution
	isRestricted := false
	switch req.Access.Class {
	case AccessCapacityNone:
		isRestricted = false
	case AccessCapacityRestricted:
		isRestricted = true
	case AccessCapacityUnknown:
		switch policy.UnknownAccessHandling {
		case UnknownAccessCountAsRestricted:
			isRestricted = true
		case UnknownAccessCountAsNone:
			isRestricted = false
		case UnknownAccessReject:
			return UsageDecision{
				Kind:           DecisionReject,
				Reason:         model.RReceiverUsageAccessClassificationUnknown,
				Message:        "unknown access capacity classification rejected by policy",
				Classification: req.Access,
			}
		}
	}

	// 3. Retro-DVR Special Rules
	if req.Intent == IntentRetroDVR {
		if req.RetroSource == RetroSourceExistingBuffer {
			// Existing local buffer requires 0 new receiver capacity
			return UsageDecision{
				Kind:           DecisionAllow,
				Requirements:   nil,
				Classification: req.Access,
			}
		}
		if req.RetroSource == RetroSourceReceiverFetch && isRestricted && !policy.AllowRetroDVRRestricted {
			return UsageDecision{
				Kind:           DecisionReject,
				Reason:         model.RReceiverUsageIntentNotAllowed,
				Message:        "retro-dvr with receiver fetch is forbidden for restricted services under current policy",
				Classification: req.Access,
			}
		}
	}

	// 4. Session Count Summaries
	activeLiveCount := 0
	activeRecordingCount := 0
	activeRestrictedCount := 0

	for _, sess := range snapshot.ActiveSessions {
		if sess.ReceiverID != req.ReceiverID {
			continue
		}
		switch sess.Intent {
		case IntentLive:
			activeLiveCount++
		case IntentRecording:
			activeRecordingCount++
		}
		if sess.IsRestricted {
			activeRestrictedCount++
		}
	}

	// 5. Evaluate Limits
	if req.Intent == IntentLive && activeRecordingCount > 0 && !policy.AllowLiveWithRecording {
		return UsageDecision{
			Kind:           DecisionReject,
			Reason:         model.RReceiverUsageLiveWithRecordingForbidden,
			Message:        "live TV while recording is not permitted by policy",
			Classification: req.Access,
		}
	}

	if req.Intent == IntentLive {
		if activeLiveCount >= policy.MaxLiveSessions {
			return UsageDecision{
				Kind:           DecisionReject,
				Reason:         model.RReceiverUsageLiveLimitExceeded,
				Message:        "maximum simultaneous live sessions limit reached",
				Classification: req.Access,
			}
		}
	}

	if req.Intent == IntentRecording {
		if activeRecordingCount >= policy.MaxRecordingSessions {
			return UsageDecision{
				Kind:           DecisionReject,
				Reason:         model.RReceiverUsageRecordingLimitExceeded,
				Message:        "maximum simultaneous recording sessions limit reached",
				Classification: req.Access,
			}
		}
	}

	if isRestricted {
		if activeRestrictedCount >= policy.MaxRestrictedAccessSessions {
			return UsageDecision{
				Kind:           DecisionReject,
				Reason:         model.RReceiverUsageRestrictedAccessLimitExceeded,
				Message:        "maximum simultaneous restricted access sessions limit reached",
				Classification: req.Access,
			}
		}
	}

	// 5.5. Topology & RF Front-End Capacity Evaluation
	isMultiplexReuse := false
	if e.topologyService != nil && req.Source.ServiceReference != "" {
		priority := receivertopology.PriorityLive
		switch req.Intent {
		case IntentRecording:
			priority = receivertopology.PriorityActiveRecording
		case IntentPictureInPicture:
			priority = receivertopology.PriorityPiP
		case IntentFastChannelPreview:
			priority = receivertopology.PriorityPreview
		}

		topoDec, err := e.topologyService.CanStartStreamWithPriority(req.Source.ServiceReference, req.SessionID, priority)
		if err == nil {
			if !topoDec.Allowed {
				return UsageDecision{
					Kind:           DecisionReject,
					Reason:         model.RLeaseBusy,
					Message:        topoDec.Reason,
					Classification: req.Access,
				}
			}
			isMultiplexReuse = topoDec.ReusedDemod
		}
	}

	// 6. Build Lease Requirements Plan
	var reqs []LeaseRequirement
	if isRestricted {
		reqs = append(reqs, LeaseRequirement{
			Kind:       ReqRestrictedAccessSlot,
			ReceiverID: req.ReceiverID,
			Quantity:   1,
		})
	}

	if isMultiplexReuse {
		// Multiplex reuse consumes 0 additional physical demodulator tuner slots
		reqs = append(reqs, LeaseRequirement{
			Kind:       ReqReceiverMultiplex,
			ReceiverID: req.ReceiverID,
			Quantity:   1,
		})
	} else {
		reqs = append(reqs, LeaseRequirement{
			Kind:       ReqTunerSlot,
			ReceiverID: req.ReceiverID,
			Quantity:   1,
		})
	}

	return UsageDecision{
		Kind:           DecisionAllow,
		Requirements:   reqs,
		Classification: req.Access,
	}
}
