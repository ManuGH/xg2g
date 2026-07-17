// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"context"

	"github.com/google/uuid"

	v3recordings "github.com/ManuGH/xg2g/internal/control/http/v3/recordings"
	"github.com/ManuGH/xg2g/internal/control/recordings/decision"
	"github.com/ManuGH/xg2g/internal/domain/playbackplanner"
	"github.com/ManuGH/xg2g/internal/log"
)

func (s *Server) buildPlaybackInfoHTTPResponse(ctx context.Context, deps recordingsModuleDeps, recordingID string, caps *PlaybackCapabilities, schemaType string, serviceRequest v3recordings.PlaybackInfoRequest) (PlaybackInfo, *v3recordings.PlaybackInfoError) {
	reqID := serviceRequest.RequestID
	if reqID == "" {
		reqID = uuid.NewString()
	}

	playbackInfo, err := s.recordingsProcessor().ResolvePlaybackInfo(ctx, serviceRequest)
	if err != nil {
		return PlaybackInfo{}, err
	}

	plannerReceipt, receiptErr := s.issuePlannerReceipt(playbackInfo, serviceRequest, schemaType, reqID)
	if receiptErr != nil {
		log.L().Error().Err(receiptErr).Str("schemaType", schemaType).Msg("failed to issue planner receipt")
	}

	if schemaType == "live" && playbackInfo.PlannerEvaluation != nil {
		isInteractiveAllow := v3recordings.PlaybackInfoRequestContext(serviceRequest) != v3recordings.PlaybackInfoContextEpgBadge &&
			playbackInfo.PlannerEvaluation.Result.Plan.Decision == playbackplanner.DecisionAllow

		if isInteractiveAllow && (plannerReceipt == nil || receiptErr != nil) {
			return PlaybackInfo{}, &v3recordings.PlaybackInfoError{
				Kind:    v3recordings.PlaybackInfoErrorInternal,
				Message: "planner receipt unavailable; refresh playback info",
				Cause:   receiptErr,
			}
		}

		response := s.mapLivePlannerPlaybackInfo(recordingID, playbackInfo.PlannerEvaluation, playbackInfo.Truth, caps, playbackInfo.ResolvedCapabilities, plannerReceipt, reqID)
		if v3recordings.PlaybackInfoRequestContext(serviceRequest) == v3recordings.PlaybackInfoContextEpgBadge {
			response.PlaybackDecisionToken = nil
		}
		return response, nil
	}

	logPlaybackTargetProfile(recordingID, schemaType, playbackInfo.Decision)
	runtimeState := resolvePlaybackRuntimeState(ctx, deps, serviceRequest.PrincipalID, recordingID, playbackInfo.Decision.Mode)
	response := s.mapPlaybackInfoV2(
		ctx,
		recordingID,
		playbackInfo.Decision,
		runtimeState.resumeState,
		runtimeState.segmentTruth,
		runtimeState.attemptedSegmentTruth,
		playbackInfo.Truth,
		schemaType,
		caps,
		playbackInfo.ResolvedCapabilities,
		playbackInfo.ClientProfile,
		playbackInfo.OperatorRuleName,
		playbackInfo.OperatorRuleScope,
		playbackInfo.RuntimePolicyAction,
		playbackInfo.RuntimePolicyPhase,
		playbackInfo.RuntimeProbeCandidate,
		playbackInfo.RuntimePolicyReasons,
		playbackInfo.RuntimePolicyConstraints,
		playbackInfo.RuntimeProbeSuccessStreak,
		playbackInfo.RuntimeProbeFailureStreak,
		plannerReceipt,
	)
	// The request-context header is client controlled. Passive EPG responses may
	// skip planner work only because they are made non-authorizing here: without
	// a decision token they cannot be replayed as a stream.start request even
	// when planner receipts are configured as optional.
	if v3recordings.PlaybackInfoRequestContext(serviceRequest) == v3recordings.PlaybackInfoContextEpgBadge {
		response.PlaybackDecisionToken = nil
	}
	return response, nil
}

func logPlaybackTargetProfile(recordingID string, schemaType string, dec *decision.Decision) {
	if dec == nil || dec.TargetProfile == nil {
		return
	}

	log.L().Debug().
		Str("recordingId", recordingID).
		Str("schemaType", schemaType).
		Str("decisionMode", string(dec.Mode)).
		Str("targetProfileHash", dec.TargetProfile.Hash()).
		Interface("targetProfile", dec.TargetProfile).
		Msg("resolved recording target playback profile")
}
