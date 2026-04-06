// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package manager

import (
	"context"
	"os"
	"strings"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
	platformnet "github.com/ManuGH/xg2g/internal/platform/net"
)

func ensurePlaybackTrace(rec *model.SessionRecord) *model.PlaybackTrace {
	if rec.PlaybackTrace == nil {
		rec.PlaybackTrace = &model.PlaybackTrace{}
	}
	return rec.PlaybackTrace
}

func sessionInputKind(sessionCtx *sessionContext) string {
	if sessionCtx == nil {
		return ""
	}
	if sessionCtx.Mode == model.ModeRecording {
		return string(ports.SourceFile)
	}
	if _, ok := platformnet.ParseDirectHTTPURL(sessionCtx.ServiceRef); ok {
		return string(ports.SourceURL)
	}
	return string(ports.SourceTuner)
}

func sessionInputKindFromRecord(rec *model.SessionRecord) string {
	if rec == nil {
		return ""
	}
	if rec.ContextData != nil {
		if inputKind := strings.TrimSpace(rec.ContextData[model.CtxKeySourceType]); inputKind != "" {
			return inputKind
		}
	}
	return string(ports.SourceTuner)
}

func (o *Orchestrator) updatePlaybackTraceBestEffort(ctx context.Context, sessionID string, fn func(*model.SessionRecord, *model.PlaybackTrace)) {
	if o == nil || o.Store == nil || strings.TrimSpace(sessionID) == "" {
		return
	}
	if _, err := o.Store.UpdateSession(ctx, sessionID, func(rec *model.SessionRecord) error {
		trace := ensurePlaybackTrace(rec)
		fn(rec, trace)
		return nil
	}); err != nil {
		log.L().Warn().Err(err).Str("session_id", sessionID).Msg("playback trace update skipped")
	}
}

func firstFrameUnixFromArtifacts(hlsRoot, sessionID string) int64 {
	markerPath := model.SessionFirstFrameMarkerPath(hlsRoot, sessionID)
	if strings.TrimSpace(markerPath) == "" {
		return 0
	}
	info, err := os.Stat(markerPath)
	if err != nil || info.IsDir() {
		return 0
	}
	return info.ModTime().Unix()
}

func tracePolicyModeHint(profile model.ProfileSpec) ports.RuntimeMode {
	if profile.PolicyModeHint != "" && profile.PolicyModeHint != ports.RuntimeModeUnknown {
		return profile.PolicyModeHint
	}
	return profiles.RuntimeModeHintFromProfile(profile)
}

func traceEffectiveRuntimeMode(profile model.ProfileSpec) ports.RuntimeMode {
	if profile.EffectiveRuntimeMode != "" && profile.EffectiveRuntimeMode != ports.RuntimeModeUnknown {
		return profile.EffectiveRuntimeMode
	}
	return tracePolicyModeHint(profile)
}

func traceEffectiveModeSource(profile model.ProfileSpec) ports.RuntimeModeSource {
	if profile.EffectiveModeSource != "" && profile.EffectiveModeSource != ports.RuntimeModeSourceUnknown {
		return profile.EffectiveModeSource
	}
	return ports.RuntimeModeSourceResolve
}

func applyTracePolicyProfile(trace *model.PlaybackTrace, profile model.ProfileSpec) {
	if trace == nil {
		return
	}
	trace.PolicyModeHint = tracePolicyModeHint(profile)
}

func applyTraceEffectiveProfile(trace *model.PlaybackTrace, profile model.ProfileSpec, inputKind string) {
	if trace == nil {
		return
	}
	trace.PolicyModeHint = tracePolicyModeHint(profile)
	trace.EffectiveRuntimeMode = traceEffectiveRuntimeMode(profile)
	trace.EffectiveModeSource = traceEffectiveModeSource(profile)
	trace.TargetProfile = model.TraceTargetProfileFromProfile(profile)
	if trace.TargetProfile != nil {
		trace.TargetProfileHash = trace.TargetProfile.Hash()
	} else {
		trace.TargetProfileHash = ""
	}
	trace.FFmpegPlan = model.TraceFFmpegPlanFromProfile(profile, inputKind, 0)
}
