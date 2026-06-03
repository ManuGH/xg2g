package manager

import (
	"context"
	"github.com/ManuGH/xg2g/internal/domain/session/lifecycle"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
	"strings"
	"time"
)

func isStartupRecoveryProfile(profileName string) bool {
	normalized := strings.TrimSpace(profileName)
	switch {
	case strings.EqualFold(normalized, profiles.ProfileSafariDirty):
		return true
	case strings.EqualFold(normalized, profiles.ProfileRepair):
		return true
	default:
		return false
	}
}

func shouldRetryStartupWaitFailure(reason model.ReasonCode, detail string, attempt int) bool {
	if attempt >= defaultStartupProcessRetryLimit {
		return false
	}
	if reason != model.RProcessEnded {
		return false
	}

	lower := strings.ToLower(strings.TrimSpace(detail))
	switch {
	case strings.Contains(lower, "upstream stream ended prematurely"):
		return true
	case strings.Contains(lower, "failed to open upstream input"):
		return true
	case strings.Contains(lower, "invalid upstream input data"):
		return true
	default:
		return false
	}
}

func startupRecoveryProfile(current model.ProfileSpec, reason model.ReasonCode, detail string) (model.ProfileSpec, bool) {
	lower := strings.ToLower(strings.TrimSpace(detail))
	if current.EffectiveRuntimeMode == ports.RuntimeModeHQ50 {
		if reason == model.RPackagerFailed && strings.Contains(lower, "playlist not ready timeout") {
			next := current
			next.ForceSafariHQ25 = true
			next.EffectiveRuntimeMode = ports.RuntimeModeHQ25
			next.EffectiveModeSource = ports.RuntimeModeSourceRuntimeHardening
			return next, true
		}
		if reason == model.RProcessEnded && strings.Contains(lower, "transcode stalled - no progress detected") {
			next := current
			next.ForceSafariHQ25 = true
			next.EffectiveRuntimeMode = ports.RuntimeModeHQ25
			next.EffectiveModeSource = ports.RuntimeModeSourceRuntimeHardening
			return next, true
		}
	}
	if current.EffectiveRuntimeMode == ports.RuntimeModeHQ25 {
		if reason == model.RPackagerFailed && strings.Contains(lower, "playlist not ready timeout") {
			next := profiles.Resolve(profiles.ProfileRepair, "", current.DVRWindowSec, nil, profiles.GPUBackendNone, profiles.HWAccelOff)
			if current.DVRWindowSec > 0 {
				next.DVRWindowSec = current.DVRWindowSec
			}
			next.EffectiveModeSource = ports.RuntimeModeSourceRuntimeHardening
			return next, true
		}
		if reason == model.RProcessEnded && strings.Contains(lower, "transcode stalled - no progress detected") {
			next := profiles.Resolve(profiles.ProfileRepair, "", current.DVRWindowSec, nil, profiles.GPUBackendNone, profiles.HWAccelOff)
			if current.DVRWindowSec > 0 {
				next.DVRWindowSec = current.DVRWindowSec
			}
			next.EffectiveModeSource = ports.RuntimeModeSourceRuntimeHardening
			return next, true
		}
	}

	if reason != model.RProcessEnded {
		return model.ProfileSpec{}, false
	}

	if !strings.Contains(lower, "copy output missing codec parameters") {
		return model.ProfileSpec{}, false
	}

	withDVR := func(next model.ProfileSpec) model.ProfileSpec {
		if current.DVRWindowSec > 0 {
			next.DVRWindowSec = current.DVRWindowSec
		}
		next.EffectiveModeSource = ports.RuntimeModeSourceRuntimeHardening
		return next
	}

	switch strings.ToLower(strings.TrimSpace(current.Name)) {
	case profiles.ProfileSafari:
		return withDVR(profiles.Resolve(profiles.ProfileSafariDirty, "", current.DVRWindowSec, nil, profiles.GPUBackendNone, profiles.HWAccelOff)), true
	case profiles.ProfileSafariDirty:
		return withDVR(profiles.Resolve(profiles.ProfileRepair, "", current.DVRWindowSec, nil, profiles.GPUBackendNone, profiles.HWAccelOff)), true
	default:
		return model.ProfileSpec{}, false
	}
}

func (o *Orchestrator) persistStartupRecoveryProfile(ctx context.Context, sessionID string, from, to model.ProfileSpec) error {
	now := time.Now()
	fromTarget := model.TraceTargetProfileFromProfile(from)
	toTarget := model.TraceTargetProfileFromProfile(to)
	fromHash := ""
	if fromTarget != nil {
		fromHash = fromTarget.Hash()
	}
	toHash := ""
	if toTarget != nil {
		toHash = toTarget.Hash()
	}
	fallbackReason := "startup_recovery:" + strings.TrimSpace(to.Name)

	_, err := o.Store.UpdateSession(ctx, sessionID, func(r *model.SessionRecord) error {
		r.Profile = to
		r.FallbackReason = fallbackReason
		r.FallbackAtUnix = now.Unix()
		r.UpdatedAtUnix = now.Unix()

		trace := ensurePlaybackTrace(r)
		applyTraceEffectiveProfile(trace, to, sessionInputKindFromRecord(r))
		trace.TargetProfile = toTarget
		trace.TargetProfileHash = toHash
		trace.StopReason = ""
		trace.StopClass = model.PlaybackStopClassNone
		trace.Fallbacks = append(trace.Fallbacks, model.PlaybackFallbackTrace{
			AtUnix:          now.Unix(),
			Trigger:         "startup_recovery",
			Reason:          fallbackReason,
			FromProfileHash: fromHash,
			ToProfileHash:   toHash,
		})
		return nil
	})
	return err
}

func canRestartTerminalFallback(r *model.SessionRecord) bool {
	if r == nil {
		return false
	}
	return r.FallbackReason != "" || r.FallbackAtUnix > 0
}

func resetForFallbackRestart(r *model.SessionRecord, now time.Time) {
	baseline := lifecycle.NewSessionRecord(now)
	createdAtUnix := r.CreatedAtUnix

	r.State = baseline.State
	r.PipelineState = baseline.PipelineState
	r.Reason = baseline.Reason
	r.ReasonDetailCode = baseline.ReasonDetailCode
	r.ReasonDetailDebug = ""
	r.UpdatedAtUnix = baseline.UpdatedAtUnix
	if createdAtUnix > 0 {
		r.CreatedAtUnix = createdAtUnix
	} else {
		r.CreatedAtUnix = baseline.CreatedAtUnix
	}
	r.LastAccessUnix = 0
	r.LastHeartbeatUnix = 0
	r.StopReason = ""
	r.LatestSegmentAt = time.Time{}
	r.LastPlaylistAccessAt = time.Time{}
	r.PlaylistPublishedAt = time.Time{}
	if r.PlaybackTrace != nil {
		r.PlaybackTrace.PolicyModeHint = ports.RuntimeModeUnknown
		r.PlaybackTrace.EffectiveRuntimeMode = ports.RuntimeModeUnknown
		r.PlaybackTrace.EffectiveModeSource = ports.RuntimeModeSourceUnknown
		r.PlaybackTrace.StopReason = ""
		r.PlaybackTrace.StopClass = model.PlaybackStopClassNone
		r.PlaybackTrace.FirstFrameAtUnix = 0
		r.PlaybackTrace.FFmpegPlan = nil
		r.PlaybackTrace.RuntimeDiagnostics = nil
	}
}
