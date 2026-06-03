package manager

import (
	"context"
	"errors"
	"github.com/ManuGH/xg2g/internal/domain/session/lifecycle"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
	platformnet "github.com/ManuGH/xg2g/internal/platform/net"
	"github.com/rs/zerolog"
	"strings"
	"time"
)

func (o *Orchestrator) acquireLeases(
	ctx context.Context,
	sessionCtx *sessionContext,
	event model.StartSessionEvent,
	leaseOwner string,
	logger zerolog.Logger,
) (*leaseAcquisition, error) {
	res := &leaseAcquisition{
		Slot:         -1,
		ReleaseDedup: func() {},
		HBCancel:     func() {},
	}

	if sessionCtx.Mode == model.ModeLive {
		dedupKey := o.LeaseKeyFunc(event)
		dedupLease, ok, err := o.Store.TryAcquireLease(ctx, dedupKey, leaseOwner, o.LeaseTTL)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, newReasonError(model.RLeaseBusy, DedupLeaseHeldDetail, nil)
		}
		res.DedupLease = dedupLease
		res.ReleaseDedup = func() {
			// Use parent context with timeout instead of Background to respect cancellation
			ctxRel, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			if err := o.Store.ReleaseLease(ctxRel, dedupLease.Key(), dedupLease.Owner()); err != nil {
				logger.Error().Err(err).
					Str("lease_key", dedupLease.Key()).
					Str("owner", dedupLease.Owner()).
					Msg("failed to release dedup lease")
			}
		}
	}

	if sessionCtx.Mode == model.ModeLive {
		slot, tunerLease, ok, err := o.acquireTunerLease(ctx, o.TunerSlots, leaseOwner)
		if err != nil {
			res.ReleaseDedup()
			return nil, err
		}
		if !ok {
			res.ReleaseDedup()
			tunerBusyTotal.WithLabelValues().Inc()
			return nil, newReasonError(model.RLeaseBusy, "no tuner slots available", nil)
		}
		res.Slot = slot
		res.TunerLease = tunerLease
	}

	hbCtx, hbCancel := context.WithCancel(ctx)
	res.HBCancel = hbCancel
	res.HBCtx = hbCtx

	o.registerActive(event.SessionID, hbCancel)

	if sessionCtx.Mode == model.ModeLive && o.HeartbeatEvery > 0 {
		started := o.goSessionWorker(func() {
			t := time.NewTicker(o.HeartbeatEvery)
			defer t.Stop()
			for {
				select {
				case <-hbCtx.Done():
					return
				case <-t.C:
					_, ok, err := o.Store.RenewLease(hbCtx, res.TunerLease.Key(), res.TunerLease.Owner(), o.LeaseTTL)
					if err != nil {
						logger.Warn().Err(err).Msg("heartbeat renewal error")
					} else if !ok {
						logger.Warn().Str("lease", res.TunerLease.Key()).Str("sid", event.SessionID).Msg("tuner lease lost, aborting")
						leaseLostTotalLegacy.WithLabelValues().Inc()
						_, _ = o.Store.UpdateSession(hbCtx, event.SessionID, func(r *model.SessionRecord) error {
							if !r.State.IsTerminal() {
								cause := lifecycle.NewReasonError(model.RLeaseExpired, "", nil)
								_, _ = lifecycle.Dispatch(r, lifecycle.PhaseFromState(r.State), lifecycle.Event{Kind: lifecycle.EvTerminalize}, cause, false, time.Now())
							}
							return nil
						})
						hbCancel()
						return
					}
				}
			}
		})
		if !started {
			res.HBCancel()
			res.ReleaseDedup()
			if res.Slot >= 0 {
				ctxRel, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
				defer cancel()
				_ = o.Store.ReleaseLease(ctxRel, res.TunerLease.Key(), res.TunerLease.Owner())
			}
			return nil, newReasonError(model.RCancelled, "orchestrator shutting down", nil)
		}
	}

	return res, nil
}

// startPipeline uses the new MediaPipeline Port (Step 4.2).
func (o *Orchestrator) startPipeline(
	hbCtx context.Context,
	e model.StartSessionEvent,
	sessionCtx *sessionContext,
	currentProfileSpec model.ProfileSpec,
	tunerSlot int,
) (ports.RunHandle, model.ProfileSpec, error) {
	// Build StreamSpec (Domain Object)
	spec := ports.StreamSpec{
		SessionID: e.SessionID,
		Mode:      ports.ModeLive, // Default
		Format:    ports.FormatHLS,
		Quality:   streamQualityForProfileSpec(currentProfileSpec),
		Profile:   currentProfileSpec, // Pass through resolved profile (GPU, codec, quality)
		Source: ports.StreamSource{
			ID:        sessionCtx.ServiceRef,
			Type:      ports.SourceTuner, // Default assumes Tuner/Ref
			TunerSlot: tunerSlot,
		},
	}

	if sessionCtx.Mode == model.ModeRecording {
		spec.Mode = ports.ModeRecording
		spec.Source.Type = ports.SourceFile // Recording builds from file source usually? Or Tuner?
		// "Recording Mode" in Orchestrator meant processing a recording (viewing).
		// Wait, "ModeRecording" in Orchestrator logic meant "Viewing a Recording".
		// In that case SourceType is File.
		spec.Source.Type = ports.SourceFile
	} else if u, ok := platformnet.ParseDirectHTTPURL(sessionCtx.ServiceRef); ok {
		normalized, err := platformnet.ValidateOutboundURL(hbCtx, u.String(), o.OutboundPolicy)
		if err != nil {
			return "", model.ProfileSpec{}, newReasonError(model.RBadRequest, "outbound url rejected by policy", err)
		}
		spec.Source.Type = ports.SourceURL
		spec.Source.ID = normalized
	}

	// Profiles: map currentProfileSpec to Quality?
	// For now, Adapter builder handles details (or ignores quality spec).
	startupLogger := log.WithContext(hbCtx, log.WithComponent("worker")).
		With().
		Str("sid", e.SessionID).
		Logger()
	o.updatePlaybackTraceBestEffort(hbCtx, e.SessionID, func(r *model.SessionRecord, trace *model.PlaybackTrace) {
		if trace.RequestProfile == "" {
			trace.RequestProfile = profiles.PublicProfileName(e.ProfileID)
		}
		if trace.ClientPath == "" && r.ContextData != nil {
			trace.ClientPath = strings.TrimSpace(r.ContextData[model.CtxKeyClientPath])
		}
		trace.InputKind = string(spec.Source.Type)
		applyTracePolicyProfile(trace, currentProfileSpec)
		trace.TargetProfile = model.TraceTargetProfileFromProfile(currentProfileSpec)
		if trace.TargetProfile != nil {
			trace.TargetProfileHash = trace.TargetProfile.Hash()
		}
		trace.FFmpegPlan = model.TraceFFmpegPlanFromProfile(currentProfileSpec, string(spec.Source.Type), 0)
	})
	startupLogger.Info().
		Str("session_id", e.SessionID).
		Str("startup_phase", "pipeline_start_requested").
		Str("source_type", string(spec.Source.Type)).
		Str("source_id", spec.Source.ID).
		Msg("pipeline start requested")

	handle, err := o.Pipeline.Start(hbCtx, spec)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return "", model.ProfileSpec{}, newReasonErrorWithDetail(model.RCancelled, model.DContextCanceled, "", err)
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return "", model.ProfileSpec{}, newReasonErrorWithDetail(model.RTuneTimeout, model.DDeadlineExceeded, "", err)
		}
		if errors.Is(err, ports.ErrNoValidTS) {
			pErr, ok, mappedErr := preflightStartReasonError(err)
			if ok {
				result := pErr.StructuredResult()
				o.updatePlaybackTraceBestEffort(hbCtx, e.SessionID, func(_ *model.SessionRecord, trace *model.PlaybackTrace) {
					trace.PreflightReason = string(result.Reason)
					trace.PreflightDetail = result.FailureDetail()
				})
				return "", model.ProfileSpec{}, mappedErr
			}
			return "", model.ProfileSpec{}, newReasonError(model.RPipelineStartFailed, "preflight failed no valid ts", err)
		}
		return "", model.ProfileSpec{}, newReasonError(model.RPipelineStartFailed, "pipeline start failed", err)
	}
	startupLogger.Info().
		Str("session_id", e.SessionID).
		Str("startup_phase", "pipeline_start_returned").
		Str("run_handle", string(handle)).
		Msg("pipeline start returned")

	effectiveProfile := currentProfileSpec
	if provider, ok := o.Pipeline.(ports.FinalizedProfileProvider); ok {
		if finalized, found := provider.FinalizedProfile(handle); found {
			effectiveProfile = finalized
		}
	}
	var executedPlan *ports.ExecutedFFmpegPlan
	if execProvider, ok := o.Pipeline.(ports.ExecutedFFmpegPlanProvider); ok {
		if executed, found := execProvider.ExecutedFFmpegPlan(handle); found {
			executedPlan = &executed
		}
	}
	o.updatePlaybackTraceBestEffort(hbCtx, e.SessionID, func(r *model.SessionRecord, trace *model.PlaybackTrace) {
		r.Profile = effectiveProfile
		trace.InputKind = string(spec.Source.Type)
		applyTraceEffectiveProfile(trace, effectiveProfile, string(spec.Source.Type))
		// Statistics never lie: replace the profile prediction with the plan parsed
		// from the real argv. A mismatch means the finalized profile no longer
		// describes what ffmpeg runs — surface it loudly rather than display a lie.
		if executedPlan != nil {
			if mismatch := applyTraceExecutedFFmpegPlan(trace, *executedPlan, string(spec.Source.Type)); mismatch != "" {
				startupLogger.Warn().
					Str("session_id", e.SessionID).
					Str("run_handle", string(handle)).
					Str("plan_mismatch", mismatch).
					Msg("ffmpeg executed plan diverges from finalized profile prediction")
			}
		}
	})

	return handle, effectiveProfile, nil
}

func streamQualityForProfileSpec(profile model.ProfileSpec) ports.QualityProfile {
	if !profile.TranscodeVideo {
		return ports.QualityStandard
	}
	if profile.PolicyModeHint == ports.RuntimeModeHQ50 {
		return ports.QualityHigh
	}
	if profiles.PublicProfileName(profile.Name) == profiles.PublicProfileQuality {
		return ports.QualityHigh
	}
	return ports.QualityStandard
}

func (o *Orchestrator) stopPipelineHandle(ctx context.Context, handle ports.RunHandle) {
	if handle == "" {
		return
	}

	stopCtx, stopCancel := context.WithTimeout(context.WithoutCancel(ctx), o.PipelineStopTimeout)
	defer stopCancel()
	_ = o.Pipeline.Stop(stopCtx, handle)
}
