package policy

import (
	"context"
	"errors"
	"fmt"
	"time"

	domainPolicy "github.com/ManuGH/xg2g/internal/domain/policy"
	"github.com/rs/zerolog"
)

// PipelineAuditEvaluator implements ConflictAuditor.
// It evaluates resource conflicts pure-functionally in audit-only mode, emits structured audit events,
// and performs STRICTLY ZERO state mutations or side effects.
type PipelineAuditEvaluator struct {
	Config          Config
	Clock           Clock
	SnapshotBuilder SnapshotBuilder
	Engine          *domainPolicy.PolicyEngine
	IDGen           IDGenerator
	Logger          AuditLogger
	TechLogger      zerolog.Logger
}

// NewAuditEvaluator creates a PipelineAuditEvaluator.
func NewAuditEvaluator(
	cfg Config,
	clock Clock,
	builder SnapshotBuilder,
	engine *domainPolicy.PolicyEngine,
	idGen IDGenerator,
	logger AuditLogger,
	techLogger zerolog.Logger,
) *PipelineAuditEvaluator {
	return &PipelineAuditEvaluator{
		Config:          cfg,
		Clock:           clock,
		SnapshotBuilder: builder,
		Engine:          engine,
		IDGen:           idGen,
		Logger:          logger,
		TechLogger:      techLogger,
	}
}

// AuditTunerConflict evaluates a tuner conflict in audit-only mode without mutating state or error objects.
// In "disabled" mode, it bypasses snapshot reading, policy evaluation, and audit logging completely.
func (e *PipelineAuditEvaluator) AuditTunerConflict(
	ctx context.Context,
	req ConflictAuditRequest,
) error {
	// 1. Mode "disabled" -> Absolute Bypass (Zero overhead, zero reads, zero evaluation)
	if e.Config.Mode == PreemptionModeDisabled {
		return nil
	}

	if req.RequestID == "" {
		e.TechLogger.Error().Msg("requestID cannot be empty for policy audit evaluation")
		return ErrMissingRequestID
	}

	if e.Clock == nil {
		e.TechLogger.Error().Msg("Clock is nil")
		return errors.New("clock is required")
	}

	evalTime := e.Clock.Now()

	domainReq := domainPolicy.EvaluationRequest{
		Consumer:     req.Consumer,
		ResourceKind: req.ResourceKind,
		Owner:        req.Owner,
		TargetScope:  req.TargetScope,
		ScopeMode:    req.ScopeMode,
		EvaluatedAt:  evalTime,
	}

	if e.IDGen == nil {
		e.TechLogger.Error().Msg("IDGen generator is nil")
		failEvent := e.makeFailureEvent("", req.RequestID, domainReq, evalTime, "ID_GENERATION", "IDGen generator is nil")
		e.emitFailureEvent(ctx, failEvent)
		return ErrInvalidEventID
	}

	// 2. Generate Event ID
	eventID, err := e.IDGen.NewID()
	if err != nil || eventID == "" {
		e.TechLogger.Error().Err(err).Str("request_id", req.RequestID).Msg("event ID generator failed or produced empty ID")
		failEvent := e.makeFailureEvent("", req.RequestID, domainReq, evalTime, "ID_GENERATION", "event ID generation failed")
		e.emitFailureEvent(ctx, failEvent)
		return ErrInvalidEventID
	}

	// 3. Build Resource Snapshot from Read-Only Providers
	if e.SnapshotBuilder == nil {
		e.TechLogger.Error().Str("request_id", req.RequestID).Msg("SnapshotBuilder is nil")
		failEvent := e.makeFailureEvent(eventID, req.RequestID, domainReq, evalTime, "SNAPSHOT_BUILD", "SnapshotBuilder is nil")
		e.emitFailureEvent(ctx, failEvent)
		return ErrSnapshotBuildFailed
	}

	snap, rev, err := e.SnapshotBuilder.BuildSnapshot(ctx, domainReq)
	if err != nil {
		e.TechLogger.Error().Err(err).Str("request_id", req.RequestID).Msg("failed to build resource snapshot for audit evaluation")
		failEvent := e.makeFailureEvent(eventID, req.RequestID, domainReq, evalTime, "SNAPSHOT_BUILD", fmt.Sprintf("snapshot build failed: %v", err))
		e.emitFailureEvent(ctx, failEvent)
		return fmt.Errorf("%w: %v", ErrSnapshotBuildFailed, err)
	}

	// 4. Policy Engine Functional Evaluation
	if e.Engine == nil {
		e.TechLogger.Error().Str("request_id", req.RequestID).Msg("PolicyEngine is nil")
		failEvent := e.makeFailureEvent(eventID, req.RequestID, domainReq, evalTime, "POLICY_EVALUATION", "PolicyEngine is nil")
		failEvent.SnapshotRevision = rev
		e.emitFailureEvent(ctx, failEvent)
		return ErrPolicyEvaluationFailed
	}

	res, evalErr := e.Engine.Evaluate(domainReq, snap)
	if evalErr != nil {
		e.TechLogger.Error().Err(evalErr).Str("request_id", req.RequestID).Str("snapshot_rev", rev).Msg("policy engine evaluation returned an error")
		failEvent := e.makeFailureEvent(eventID, req.RequestID, domainReq, evalTime, "POLICY_EVALUATION", fmt.Sprintf("policy engine evaluation failed: %v", evalErr))
		failEvent.SnapshotRevision = rev
		e.emitFailureEvent(ctx, failEvent)
		return fmt.Errorf("%w: %v", ErrPolicyEvaluationFailed, evalErr)
	}

	// 5. Construct Audit Event
	event := EvaluationAuditEvent{
		EventID:               eventID,
		EvaluatedAt:           evalTime,
		RequestID:             req.RequestID,
		RequestOwner:          req.Owner,
		Consumer:              req.Consumer,
		ResourceKind:          req.ResourceKind,
		ScopeMode:             req.ScopeMode,
		TargetScope:           req.TargetScope,
		Decision:              res.Decision,
		ReasonCode:            res.ReasonCode,
		SelectedScope:         res.SelectedScope,
		TargetAllocationID:    res.TargetAllocationID,
		BlockingAllocationIDs: res.BlockingAllocationIDs,
		EnforcementMode:       PreemptionModeAuditOnly,
		SnapshotRevision:      rev,
		EvaluationSucceeded:   true,
	}

	// 6. Emit Audit Event
	if e.Logger != nil {
		if emitErr := e.Logger.Emit(ctx, event); emitErr != nil {
			e.TechLogger.Error().Err(emitErr).Str("event_id", eventID).Str("request_id", req.RequestID).Msg("failed to emit evaluation audit event")
			return fmt.Errorf("%w: %v", ErrAuditEmissionFailed, emitErr)
		}
	}

	return nil
}

func (e *PipelineAuditEvaluator) makeFailureEvent(eventID, requestID string, req domainPolicy.EvaluationRequest, evalTime time.Time, stage, errMsg string) EvaluationAuditEvent {
	return EvaluationAuditEvent{
		EventID:             eventID,
		EvaluatedAt:         evalTime,
		RequestID:           requestID,
		RequestOwner:        req.Owner,
		Consumer:            req.Consumer,
		ResourceKind:        req.ResourceKind,
		ScopeMode:           req.ScopeMode,
		TargetScope:         req.TargetScope,
		EnforcementMode:     PreemptionModeAuditOnly,
		EvaluationSucceeded: false,
		FailureStage:        stage,
		ErrorCode:           "POLICY_EVALUATION_FAILED",
		ErrorMessage:        errMsg,
	}
}

func (e *PipelineAuditEvaluator) emitFailureEvent(ctx context.Context, event EvaluationAuditEvent) {
	if e.Logger != nil {
		_ = e.Logger.Emit(ctx, event)
	}
}
