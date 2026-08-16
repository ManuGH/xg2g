package manager

import (
	"context"
	"errors"
	"strings"
	"time"

	domainPolicy "github.com/ManuGH/xg2g/internal/domain/policy"
	"github.com/ManuGH/xg2g/internal/domain/receiverusage"
	"github.com/ManuGH/xg2g/internal/domain/session/lifecycle"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/log"
	pipelineLease "github.com/ManuGH/xg2g/internal/pipeline/lease"
	pipelinePolicy "github.com/ManuGH/xg2g/internal/pipeline/policy"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
	platformnet "github.com/ManuGH/xg2g/internal/platform/net"
	"github.com/ManuGH/xg2g/internal/receivertopology"
	"github.com/rs/zerolog"
)

func (o *Orchestrator) acquireLeases(
	ctx context.Context,
	sessionCtx *sessionContext,
	event model.StartSessionEvent,
	leaseOwner string,
	logger zerolog.Logger,
) (*leaseAcquisition, error) {
	res := &leaseAcquisition{
		Slot:                    -1,
		ReleaseDedup:            func() {},
		ReleaseTuner:            func() {},
		ReleaseRestrictedAccess: func() error { return nil },
		HBCancel:                func() {},
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
			ctxRel, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			if err := o.Store.ReleaseLease(ctxRel, dedupLease.Key(), dedupLease.Owner()); err != nil {
				logger.Error().Err(err).
					Str("lease_key", dedupLease.Key()).
					Str("owner", dedupLease.Owner()).
					Msg("failed to release dedup lease")
			}
		}
	}

	requiresTunerSlot := true
	// E2.5c: Receiver Usage Policy Evaluation & Multi-Resource Plan Execution
	if o.UsageEvaluator != nil {
		req := BuildUsageRequest(sessionCtx, o.ReceiverID, leaseOwner, true, true, time.Now())
		activeSessions, _ := o.Store.ListSessions(ctx)
		snap := BuildSystemSnapshot(o.ReceiverID, activeSessions, nil)

		policy := o.UsagePolicy

		decision, err := o.UsageEvaluator.Evaluate(ctx, policy, req, snap)
		if err != nil {
			res.ReleaseDedup()
			return nil, err
		}

		switch decision.Kind {
		case receiverusage.DecisionReuseSession:
			return res, nil
		case receiverusage.DecisionReject:
			res.ReleaseDedup()
			return nil, newReasonError(decision.Reason, decision.Message, nil)
		case receiverusage.DecisionAllow:
			if len(decision.Requirements) > 0 {
				requiresTunerSlot = false
				for _, reqItem := range decision.Requirements {
					if reqItem.Kind == receiverusage.ReqTunerSlot {
						requiresTunerSlot = true
					}
					if reqItem.Kind == receiverusage.ReqRestrictedAccessSlot {
						ctrl := o.RestrictedAccessCtrl
						if ctrl == nil {
							ctrl = receiverusage.NewRestrictedAccessController(o.Store)
						}
						cap := policy.MaxRestrictedAccessSessions
						if cap <= 0 {
							cap = 1
						}
						h, err := ctrl.Acquire(ctx, o.ReceiverID, leaseOwner, cap, o.LeaseTTL)
						if err != nil {
							res.ReleaseDedup()
							return nil, newReasonError(model.RReceiverUsageRestrictedAccessLimitExceeded, "restricted access slot limit exceeded", err)
						}
						res.RestrictedAccessHandle = h
						res.ReleaseRestrictedAccess = func() error {
							ctxRel, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
							defer cancel()
							if err := ctrl.Release(ctxRel, h); err != nil {
								logger.Error().Err(err).
									Str("scope", h.Scope).
									Str("owner", h.Owner).
									Msg("failed to release restricted access lease")
								return err
							}
							return nil
						}
					}
				}
			}
		}
	}

	if sessionCtx.Mode == model.ModeLive && requiresTunerSlot {
		slot, tunerLease, tunerHandle, err := o.acquireTunerLease(ctx, o.TunerSlots, leaseOwner)
		if err != nil {
			relErr := res.ReleaseRestrictedAccess()
			res.ReleaseDedup()

			tunerErr := err
			if relErr != nil {
				tunerErr = errors.Join(err, relErr)
			}

			if errors.Is(err, pipelineLease.ErrScopeConflict) {
				tunerBusyTotal.WithLabelValues().Inc()
				publicErr := newReasonError(model.RLeaseBusy, "no tuner slots available", tunerErr)

				if o.ConflictAuditor != nil {
					consumer := domainPolicy.ConsumerLiveTV
					if sessionCtx.Mode == model.ModeRecording {
						consumer = domainPolicy.ConsumerScheduledRecording
					}
					auditReq := pipelinePolicy.ConflictAuditRequest{
						RequestID:    event.SessionID,
						Consumer:     consumer,
						ResourceKind: domainPolicy.ResourceTuner,
						Owner:        leaseOwner,
						ScopeMode:    domainPolicy.ScopeSelectionAnyCompatible,
					}
					if auditErr := o.ConflictAuditor.AuditTunerConflict(ctx, auditReq); auditErr != nil {
						logger.Error().Err(auditErr).Str("session_id", event.SessionID).Msg("policy audit evaluation failed")
					}
				}

				return nil, publicErr
			}
			return nil, err
		}
		res.Slot = slot
		res.TunerLease = tunerLease
		res.TunerHandle = tunerHandle
		res.ReleaseTuner = func() {
			ctxRel, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			controller := o.TunerLeaseController
			if controller == nil {
				controller = pipelineLease.NewSessionStoreTunerLeaseController(o.Store)
			}
			if err := controller.Release(ctxRel, res.TunerHandle, pipelineLease.ReasonReleasedByOwner); err != nil {
				logger.Error().Err(err).
					Str("lease_key", string(res.TunerHandle.Scope)).
					Str("owner", string(res.TunerHandle.Owner)).
					Int("slot", res.TunerHandle.Slot).
					Msg("failed to release tuner lease")
			}
		}
	}

	// Topology Service Stream Registration & Lifecycle Hook
	if o.TopologyService != nil && sessionCtx.ServiceRef != "" {
		_, topoErr := o.TopologyService.RegisterStream(sessionCtx.ServiceRef, event.SessionID)
		if topoErr != nil && o.TopologyService.Mode() == receivertopology.EvaluationModeEnforce && o.TopologyService.Topology().Confidence == receivertopology.ConfidenceVerified {
			_ = res.ReleaseRestrictedAccess()
			res.ReleaseTuner()
			res.ReleaseDedup()
			return nil, newReasonError(model.RLeaseBusy, "receiver physical topology allocation failed: "+topoErr.Error(), topoErr)
		}
		prevReleaseTuner := res.ReleaseTuner
		res.ReleaseTuner = func() {
			if prevReleaseTuner != nil {
				prevReleaseTuner()
			}
			o.TopologyService.ReleaseStream(event.SessionID)
		}
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
					if o.TopologyService != nil {
						o.TopologyService.HeartbeatStream(event.SessionID, o.LeaseTTL)
					}
					if res.TunerHandle != nil {
						controller := o.TunerLeaseController
						if controller == nil {
							controller = pipelineLease.NewSessionStoreTunerLeaseController(o.Store)
						}
						err := controller.Renew(hbCtx, res.TunerHandle, o.LeaseTTL)
						if err != nil {
							if errors.Is(err, pipelineLease.ErrLeaseInactive) || errors.Is(err, pipelineLease.ErrNotFound) {
								logger.Warn().Str("lease", string(res.TunerHandle.Scope)).Str("sid", event.SessionID).Msg("tuner lease lost, aborting")
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
							logger.Warn().Err(err).Msg("heartbeat renewal error")
						}
					}
				}
			}
		})
		if !started {
			res.HBCancel()
			res.ReleaseDedup()
			if res.TunerHandle != nil {
				ctxRel, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
				defer cancel()
				controller := pipelineLease.NewSessionStoreTunerLeaseController(o.Store)
				if err := controller.Release(ctxRel, res.TunerHandle, pipelineLease.ReasonReleasedByOwner); err != nil {
					logger.Error().Err(err).
						Str("lease_key", string(res.TunerHandle.Scope)).
						Str("owner", string(res.TunerHandle.Owner)).
						Int("slot", res.TunerHandle.Slot).
						Msg("failed to release tuner lease on worker start failure")
				}
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
	attempt startupAttempt,
) (ports.RunHandle, model.ProfileSpec, error) {
	// Build StreamSpec (Domain Object)
	spec := ports.StreamSpec{
		SessionID:    e.SessionID,
		ClientFamily: sessionCtx.ClientFamily,
		Mode:         ports.ModeLive, // Default
		Format:       ports.FormatHLS,
		Quality:      streamQualityForProfileSpec(currentProfileSpec),
		Profile:      currentProfileSpec, // Pass through resolved profile (GPU, codec, quality)
		Source: ports.StreamSource{
			ID:        sessionCtx.ServiceRef,
			Type:      ports.SourceTuner, // Default assumes Tuner/Ref
			TunerSlot: tunerSlot,
		},
	}

	// Hand the attempt's phase boundaries to the adapter so its preparation and
	// its startup watchdog are bounded by the same clock this orchestrator gives
	// up on, instead of by constants that can sit either side of it.
	now := time.Now()
	if prepareDeadline, bounded := attempt.prepareDeadline(now); bounded {
		spec.PrepareDeadline = prepareDeadline
	}
	if readyDeadline, bounded := attempt.deadline(); bounded {
		spec.ReadyDeadline = readyDeadline
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
