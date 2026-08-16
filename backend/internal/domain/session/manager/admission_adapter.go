// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package manager

import (
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/receiverusage"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
)

// OpenWebifActivity represents an observed activity running directly on the receiver via OpenWebif.
type OpenWebifActivity struct {
	ActivityID       string
	ServiceReference string
	IsProtected      bool
	IsRecording      bool
	StartedAt        time.Time
}

// BuildUsageRequest constructs a receiverusage.UsageRequest from session context parameters and channel protection metadata.
func BuildUsageRequest(sCtx *sessionContext, receiverID, owner string, channelProtectionKnown bool, isChannelProtected bool, now time.Time) receiverusage.UsageRequest {
	recID := receiverID
	if recID == "" {
		recID = "default-receiver"
	}

	intent := receiverusage.IntentLive
	if sCtx != nil && sCtx.Mode == model.ModeRecording {
		intent = receiverusage.IntentRecording
	}

	accessClass := receiverusage.AccessCapacityUnknown
	confidence := receiverusage.ConfidenceUnknown

	if channelProtectionKnown {
		confidence = receiverusage.ConfidenceVerified
		if isChannelProtected {
			accessClass = receiverusage.AccessCapacityRestricted
		} else {
			accessClass = receiverusage.AccessCapacityNone
		}
	}

	serviceRef := ""
	sessionID := ""
	if sCtx != nil {
		serviceRef = sCtx.ServiceRef
		sessionID = sCtx.SessionID
	}

	return receiverusage.UsageRequest{
		SessionID:  sessionID,
		ReceiverID: recID,
		Owner:      owner,
		Intent:     intent,
		Source: receiverusage.SourceIdentity{
			ReceiverID:       recID,
			ServiceReference: serviceRef,
		},
		Access: receiverusage.AccessClassification{
			Class:      accessClass,
			Confidence: confidence,
			Source:     "channel_metadata",
			ObservedAt: now,
		},
		RetroSource: receiverusage.RetroSourceReceiverFetch,
		RequestedAt: now,
	}
}

// BuildSystemSnapshot combines active xg2g session records and observed OpenWebif activities into a receiverusage.SystemSnapshot.
func BuildSystemSnapshot(receiverID string, activeSessions []*model.SessionRecord, openwebifActivities []OpenWebifActivity) receiverusage.SystemSnapshot {
	var snapshots []receiverusage.ActiveSessionSnapshot

	// 1. Convert active xg2g sessions
	for _, sess := range activeSessions {
		if sess == nil {
			continue
		}
		sessRecID := receiverID
		if sess.ContextData != nil {
			if rID := sess.ContextData["receiver_id"]; rID != "" {
				sessRecID = rID
			}
		}
		if sessRecID != receiverID {
			continue
		}

		intent := receiverusage.IntentLive
		if sess.ContextData != nil {
			if strings.EqualFold(sess.ContextData[model.CtxKeyMode], model.ModeRecording) {
				intent = receiverusage.IntentRecording
			}
		}

		isRestricted := false
		if sess.ContextData != nil {
			isRestricted = sess.ContextData["is_restricted"] == "true"
		}

		owner := "unknown-owner"
		if sess.ContextData != nil {
			if o := sess.ContextData["owner"]; o != "" {
				owner = o
			}
		}

		snapshots = append(snapshots, receiverusage.ActiveSessionSnapshot{
			SessionID:        sess.SessionID,
			ReceiverID:       sessRecID,
			Owner:            owner,
			Intent:           intent,
			ServiceReference: sess.ServiceRef,
			IsRestricted:     isRestricted,
			StartedAt:        time.Unix(sess.CreatedAtUnix, 0),
		})
	}

	// 2. Convert observed OpenWebif activities (direct on receiver)
	for _, act := range openwebifActivities {
		intent := receiverusage.IntentLive
		if act.IsRecording {
			intent = receiverusage.IntentRecording
		}

		snapshots = append(snapshots, receiverusage.ActiveSessionSnapshot{
			SessionID:        "openwebif-" + act.ActivityID,
			ReceiverID:       receiverID,
			Owner:            "openwebif-direct",
			Intent:           intent,
			ServiceReference: act.ServiceReference,
			IsRestricted:     act.IsProtected,
			StartedAt:        act.StartedAt,
		})
	}

	return receiverusage.SystemSnapshot{
		ActiveSessions: snapshots,
	}
}
