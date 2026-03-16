// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"context"

	v3recordings "github.com/ManuGH/xg2g/internal/control/http/v3/recordings"
	"github.com/ManuGH/xg2g/internal/control/recordings/decision"
	"github.com/ManuGH/xg2g/internal/log"
)

func (s *Server) buildPlaybackInfoHTTPResponse(ctx context.Context, deps recordingsModuleDeps, recordingID string, caps *PlaybackCapabilities, schemaType string, serviceRequest v3recordings.PlaybackInfoRequest) (PlaybackInfo, *v3recordings.PlaybackInfoError) {
	playbackInfo, err := s.recordingsProcessor().ResolvePlaybackInfo(ctx, serviceRequest)
	if err != nil {
		return PlaybackInfo{}, err
	}

	logPlaybackTargetProfile(recordingID, schemaType, playbackInfo.Decision)
	runtimeState := resolvePlaybackRuntimeState(ctx, deps, serviceRequest.PrincipalID, recordingID, playbackInfo.Decision.Mode)

	return s.mapPlaybackInfoV2(
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
	), nil
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
