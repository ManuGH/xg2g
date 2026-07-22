// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"context"
	"time"

	"github.com/ManuGH/xg2g/internal/control/http/v3/sessions"
	"github.com/ManuGH/xg2g/internal/control/recordings/runtimepolicy"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
)

type sessionRuntimeTransitionResult = sessions.SessionRuntimeTransitionResult

func applySessionRuntimePolicyTransition(rec *model.SessionRecord, transition runtimepolicy.SessionTransition, now time.Time, profileResolver profiles.Resolver) (sessionRuntimeTransitionResult, error) {
	return sessions.ApplySessionRuntimePolicyTransition(rec, transition, now, profileResolver)
}

func applyRuntimeTransitionProfile(rec *model.SessionRecord, nextProfile model.ProfileSpec, transition runtimepolicy.SessionTransition, now time.Time) {
	sessions.ApplyRuntimeTransitionProfile(rec, nextProfile, transition, now)
}

func sessionRuntimeProfileForStepWithResolver(current model.ProfileSpec, step runtimepolicy.PlaybackLadderStep, profileResolver profiles.Resolver) (model.ProfileSpec, bool) {
	return sessions.SessionRuntimeProfileForStepWithResolver(current, step, profileResolver)
}

func (s *Server) publishSessionRuntimeTransition(ctx context.Context, store SessionStateStore, sess *model.SessionRecord, transition runtimepolicy.SessionTransition) {
	if s == nil {
		return
	}
	s.sessionsProcessor().PublishSessionRuntimeTransition(ctx, sess, transition)
}
