// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package receiverusage

import (
	"context"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
)

type Evaluator interface {
	Evaluate(ctx context.Context, policy ReceiverUsagePolicy, req UsageRequest, snapshot SystemSnapshot) (UsageDecision, error)
}

type ReceiverUsageEvaluator struct{}

func NewEvaluator() *ReceiverUsageEvaluator {
	return &ReceiverUsageEvaluator{}
}

func (e *ReceiverUsageEvaluator) Evaluate(ctx context.Context, policy ReceiverUsagePolicy, req UsageRequest, snapshot SystemSnapshot) (UsageDecision, error) {
	if err := policy.Validate(); err != nil {
		return UsageDecision{}, err
	}

	// 1. Check for Duplicate Request (Same receiver, service, intent, owner within DuplicateWindow)
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
				}, nil
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
				Message:        "access classification is unknown and policy requires rejection",
				Classification: req.Access,
			}, nil
		}
	}

	// 3. Retro-DVR Source Check
	if req.Intent == IntentRetroDVR {
		if req.RetroSource == RetroSourceExistingBuffer {
			// Existing local buffer consumes 0 new receiver access or tuner slots
			return UsageDecision{
				Kind:           DecisionAllow,
				Requirements:   []LeaseRequirement{},
				Classification: req.Access,
			}, nil
		}
		if req.RetroSource == RetroSourceReceiverFetch && isRestricted && !policy.AllowRetroDVRRestricted {
			return UsageDecision{
				Kind:           DecisionReject,
				Reason:         model.RReceiverUsageIntentNotAllowed,
				Message:        "retro-dvr with receiver fetch is forbidden for restricted services under current policy",
				Classification: req.Access,
			}, nil
		}
	}

	// 4. Count Active Sessions for Target Receiver
	activeLiveCount := 0
	activeRecordingCount := 0
	activeRestrictedCount := 0

	for _, sess := range snapshot.ActiveSessions {
		if sess.ReceiverID != req.ReceiverID {
			continue
		}
		if sess.Intent == IntentLive {
			activeLiveCount++
		}
		if sess.Intent == IntentRecording {
			activeRecordingCount++
		}
		if sess.IsRestricted {
			activeRestrictedCount++
		}
	}

	// 5. Evaluate Concurrency & Limits
	if req.Intent == IntentLive {
		if activeLiveCount >= policy.MaxLiveSessions {
			return UsageDecision{
				Kind:           DecisionReject,
				Reason:         model.RReceiverUsageLiveLimitExceeded,
				Message:        "maximum simultaneous live sessions limit reached",
				Classification: req.Access,
			}, nil
		}
		if activeRecordingCount > 0 && !policy.AllowLiveWithRecording {
			return UsageDecision{
				Kind:           DecisionReject,
				Reason:         model.RReceiverUsageLiveWithRecordingForbidden,
				Message:        "live session is forbidden while a recording session is active",
				Classification: req.Access,
			}, nil
		}
	}

	if req.Intent == IntentRecording {
		if activeRecordingCount >= policy.MaxRecordingSessions {
			return UsageDecision{
				Kind:           DecisionReject,
				Reason:         model.RReceiverUsageRecordingLimitExceeded,
				Message:        "maximum simultaneous recording sessions limit reached",
				Classification: req.Access,
			}, nil
		}
	}

	if isRestricted {
		if activeRestrictedCount >= policy.MaxRestrictedAccessSessions {
			return UsageDecision{
				Kind:           DecisionReject,
				Reason:         model.RReceiverUsageRestrictedAccessLimitExceeded,
				Message:        "maximum simultaneous restricted access sessions limit reached",
				Classification: req.Access,
			}, nil
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
	reqs = append(reqs, LeaseRequirement{
		Kind:       ReqTunerSlot,
		ReceiverID: req.ReceiverID,
		Quantity:   1,
	})

	return UsageDecision{
		Kind:           DecisionAllow,
		Requirements:   reqs,
		Classification: req.Access,
	}, nil
}
