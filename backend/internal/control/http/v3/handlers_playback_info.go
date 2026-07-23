// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	v3playbackinfo "github.com/ManuGH/xg2g/internal/control/http/v3/playbackinfo"
	v3recordings "github.com/ManuGH/xg2g/internal/control/http/v3/recordings"
	"github.com/ManuGH/xg2g/internal/control/read"
	"github.com/ManuGH/xg2g/internal/domain/playbackplanner"
	"github.com/ManuGH/xg2g/internal/household"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/problemcode"
)

// Responsibility: Handles truthful playback capability probing.
// Non-goals: Actual serving of media (see handlers_hls.go).

// GetRecordingPlaybackInfo implements ServerInterface (Legacy GET)
func (s *Server) GetRecordingPlaybackInfo(w http.ResponseWriter, r *http.Request, recordingId string) {
	if _, ok := s.requireHouseholdRecordingAccess(w, r, recordingId); !ok {
		return
	}
	s.handlePlaybackInfo(w, r, recordingId, nil, "v3", "legacy")
}

// PostRecordingPlaybackInfo implements ServerInterface (v3.1 POST)
func (s *Server) PostRecordingPlaybackInfo(w http.ResponseWriter, r *http.Request, recordingId string) {
	if _, ok := s.requireHouseholdRecordingAccess(w, r, recordingId); !ok {
		return
	}
	caps, problem := v3playbackinfo.ParseRecordingPlaybackPostInput(r)
	if problem != nil {
		v3playbackinfo.WritePlaybackInfoInputProblem(w, r, problem)
		return
	}
	s.handlePlaybackInfo(w, r, recordingId, caps, "v3.1", "compact")
}

// PostLivePlaybackInfo implements ServerInterface (Live Stream Info)
func (s *Server) PostLivePlaybackInfo(w http.ResponseWriter, r *http.Request) {
	input, problem := v3playbackinfo.ParseLivePlaybackPostInput(r)
	if problem != nil {
		v3playbackinfo.WritePlaybackInfoInputProblem(w, r, problem)
		return
	}
	profile := household.NormalizeProfile(s.currentHouseholdProfile(r.Context()))
	if household.HasServiceRestrictionsNormalized(profile) {
		visibleRefs, err := s.householdVisibleServiceRefSet(profile, s.systemModuleDeps())
		if err != nil {
			writeRegisteredProblem(w, r, http.StatusInternalServerError, "household/service_resolution_failed", "Household Service Resolution Failed", problemcode.CodeReadFailed, "Failed to resolve visible household services", nil)
			return
		}
		if _, ok := visibleRefs[read.CanonicalServiceRef(input.ServiceRef)]; !ok {
			writeHouseholdForbidden(w, r, "household/live_service_forbidden", "Live Service Forbidden", "The active household profile is not allowed to access this service")
			return
		}
	}

	// For live playback we pass the normalized serviceRef through the shared decision path.
	// The frontend uses the returned PlaybackInfo to check mode and obtain playbackDecisionToken.
	s.handlePlaybackInfo(w, r, input.ServiceRef, input.Capabilities, "v3.1", "live")
}

func (s *Server) handlePlaybackInfo(w http.ResponseWriter, r *http.Request, recordingId string, caps *v3playbackinfo.PlaybackCapabilities, apiVersion string, schemaType string) {
	deps := s.recordingsModuleDeps()
	serviceRequest := v3playbackinfo.BuildPlaybackInfoServiceRequest(r, recordingId, caps, apiVersion, schemaType)
	dto, err := s.buildPlaybackInfoHTTPResponse(r.Context(), deps, recordingId, caps, schemaType, serviceRequest)
	if err != nil {
		v3playbackinfo.WritePlaybackInfoServiceError(w, r, recordingId, schemaType, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(dto)
}

func (s *Server) buildPlaybackInfoHTTPResponse(ctx context.Context, deps recordingsModuleDeps, recordingID string, caps *v3playbackinfo.PlaybackCapabilities, schemaType string, serviceRequest v3recordings.PlaybackInfoRequest) (v3playbackinfo.PlaybackInfo, *v3recordings.PlaybackInfoError) {
	reqID := serviceRequest.RequestID
	if reqID == "" {
		reqID = uuid.NewString()
	}

	playbackInfo, err := s.recordingsProcessor().ResolvePlaybackInfo(ctx, serviceRequest)
	if err != nil {
		return v3playbackinfo.PlaybackInfo{}, err
	}

	plannerReceipt, receiptErr := s.issuePlannerReceipt(playbackInfo, serviceRequest, schemaType, reqID)
	if receiptErr != nil {
		log.L().Error().Err(receiptErr).Str("schemaType", schemaType).Msg("failed to issue planner receipt")
	}

	if schemaType == "live" && playbackInfo.PlannerEvaluation != nil {
		isInteractiveAllow := v3recordings.PlaybackInfoRequestContext(serviceRequest) != v3recordings.PlaybackInfoContextEpgBadge &&
			playbackInfo.PlannerEvaluation.Result.Plan.Decision == playbackplanner.DecisionAllow

		if isInteractiveAllow && (plannerReceipt == nil || receiptErr != nil) {
			return v3playbackinfo.PlaybackInfo{}, &v3recordings.PlaybackInfoError{
				Kind:    v3recordings.PlaybackInfoErrorInternal,
				Message: "planner receipt unavailable; refresh playback info",
				Cause:   receiptErr,
			}
		}

		response := s.playbackInfoProcessor().MapLivePlannerPlaybackInfo(recordingID, playbackInfo.PlannerEvaluation, playbackInfo.Truth, caps, playbackInfo.ResolvedCapabilities, plannerReceipt, reqID)
		if v3recordings.PlaybackInfoRequestContext(serviceRequest) == v3recordings.PlaybackInfoContextEpgBadge {
			response.PlaybackDecisionToken = nil
		}
		return response, nil
	}

	v3playbackinfo.LogPlaybackTargetProfile(recordingID, schemaType, playbackInfo.Decision)
	runtimeState := v3playbackinfo.ResolvePlaybackRuntimeState(ctx, deps.artifacts, deps.resumeStore, serviceRequest.PrincipalID, recordingID, playbackInfo.Decision.Mode)
	response := s.playbackInfoProcessor().MapPlaybackInfoV2(
		ctx,
		recordingID,
		playbackInfo.Decision,
		runtimeState.ResumeState,
		runtimeState.SegmentTruth,
		runtimeState.AttemptedSegmentTruth,
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
	if v3recordings.PlaybackInfoRequestContext(serviceRequest) == v3recordings.PlaybackInfoContextEpgBadge {
		response.PlaybackDecisionToken = nil
	}
	return response, nil
}
