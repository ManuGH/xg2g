package policy

import (
	"context"
	"fmt"
	"time"

	domainPolicy "github.com/ManuGH/xg2g/internal/domain/policy"
	"github.com/rs/zerolog"
)

// PipelineAuditEvaluator implements AuditEvaluator.
// It evaluates resource conflicts pure-functionally in audit-only mode, emits structured audit events,
// and performs STRICTLY ZERO state mutations or side effects.
type PipelineAuditEvaluator struct {
	Config          Config
	SnapshotBuilder SnapshotBuilder
	Engine          *domainPolicy.PolicyEngine
	IDGen           IDGenerator
	Logger          AuditLogger
	TechLogger      zerolog.Logger
}

// NewAuditEvaluator creates a PipelineAuditEvaluator.
func NewAuditEvaluator(
	cfg Config,
	builder SnapshotBuilder,
	engine *domainPolicy.PolicyEngine,
	idGen IDGenerator,
	logger AuditLogger,
	techLogger zerolog.Logger,
) *PipelineAuditEvaluator {
	return &PipelineAuditEvaluator{
		Config:          cfg,
		SnapshotBuilder: builder,
		Engine:          engine,
		IDGen:           idGen,
		Logger:          logger,
		TechLogger:      techLogger,
	}
}

// EvaluateConflict evaluates a tuner conflict in audit-only mode without mutating state.
// In "disabled" mode, it bypasses snapshot reading, policy evaluation, and audit logging completely.
// On audit pipeline failure (snapshot build, evaluation, or audit emit), it logs technical errors structurally,
// emits a failure audit event with ReasonCode "POLICY_EVALUATION_FAILED", and returns a typed error.
// The caller ALWAYS retains its original conflict error unchanged.
func (e *PipelineAuditEvaluator) EvaluateConflict(
	ctx context.Context,
	requestID string,
	req domainPolicy.EvaluationRequest,
	origErr error,
) (EvaluationAuditEvent, error) {
	// 1. Mode "disabled" -> Absolute Bypass (Zero overhead, zero reads, zero evaluation)
	if e.Config.Mode == PreemptionModeDisabled {
		return EvaluationAuditEvent{}, nil
	}

	if requestID == "" {
		e.TechLogger.Error().Msg("requestID cannot be empty for policy audit evaluation")
		return EvaluationAuditEvent{}, ErrMissingRequestID
	}

	if e.IDGen == nil {
		e.TechLogger.Error().Msg("IDGen generator function is nil")
		return EvaluationAuditEvent{}, ErrInvalidEventID
	}

	// 2. Generate Event ID
	eventID, err := e.IDGen()
	if err != nil || eventID == "" {
		e.TechLogger.Error().Err(err).Str("request_id", requestID).Msg("event ID generator failed or produced empty ID")
		failEvent := EvaluationAuditEvent{
			EvaluatedAt:         req.EvaluatedAt,
			RequestID:           requestID,
			RequestOwner:        req.Owner,
			Consumer:            req.Consumer,
			ResourceKind:        req.ResourceKind,
			ScopeMode:           req.ScopeMode,
			TargetScope:         req.TargetScope,
			Decision:            domainPolicy.DecisionReject,
			ReasonCode:          domainPolicy.ReasonCode("POLICY_EVALUATION_FAILED"),
			EnforcementMode:     PreemptionModeAuditOnly,
			EvaluationSucceeded: false,
			ErrorMessage:        "event ID generation failed",
		}
		if e.Logger != nil {
			_ = e.Logger.Emit(ctx, failEvent)
		}
		return failEvent, ErrInvalidEventID
	}

	evalTime := req.EvaluatedAt
	if evalTime.IsZero() {
		evalTime = time.Now()
		req.EvaluatedAt = evalTime
	}

	// 3. Build Resource Snapshot from Read-Only Providers
	if e.SnapshotBuilder == nil {
		e.TechLogger.Error().Str("request_id", requestID).Msg("SnapshotBuilder is nil")
		failEvent := e.makeFailureEvent(eventID, requestID, req, "SnapshotBuilder is nil")
		e.emitFailureEvent(ctx, failEvent)
		return failEvent, ErrSnapshotBuildFailed
	}

	snap, rev, err := e.SnapshotBuilder.BuildSnapshot(ctx, req)
	if err != nil {
		e.TechLogger.Error().Err(err).Str("request_id", requestID).Msg("failed to build resource snapshot for audit evaluation")
		failEvent := e.makeFailureEvent(eventID, requestID, req, fmt.Sprintf("snapshot build failed: %v", err))
		e.emitFailureEvent(ctx, failEvent)
		return failEvent, fmt.Errorf("%w: %v", ErrSnapshotBuildFailed, err)
	}

	// 4. Policy Engine Functional Evaluation
	if e.Engine == nil {
		e.TechLogger.Error().Str("request_id", requestID).Msg("PolicyEngine is nil")
		failEvent := e.makeFailureEvent(eventID, requestID, req, "PolicyEngine is nil")
		failEvent.SnapshotRevision = rev
		e.emitFailureEvent(ctx, failEvent)
		return failEvent, ErrPolicyEvaluationFailed
	}

	res, evalErr := e.Engine.Evaluate(req, snap)
	if evalErr != nil {
		e.TechLogger.Error().Err(evalErr).Str("request_id", requestID).Str("snapshot_rev", rev).Msg("policy engine evaluation returned an error")
		failEvent := e.makeFailureEvent(eventID, requestID, req, fmt.Sprintf("policy engine evaluation failed: %v", evalErr))
		failEvent.SnapshotRevision = rev
		e.emitFailureEvent(ctx, failEvent)
		return failEvent, fmt.Errorf("%w: %v", ErrPolicyEvaluationFailed, evalErr)
	}

	// 5. Construct Audit Event
	event := EvaluationAuditEvent{
		EventID:               eventID,
		EvaluatedAt:           req.EvaluatedAt,
		RequestID:             requestID,
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
			e.TechLogger.Error().Err(emitErr).Str("event_id", eventID).Str("request_id", requestID).Msg("failed to emit evaluation audit event")
			return event, fmt.Errorf("%w: %v", ErrAuditEmissionFailed, emitErr)
		}
	}

	return event, nil
}

func (e *PipelineAuditEvaluator) makeFailureEvent(eventID, requestID string, req domainPolicy.EvaluationRequest, errMsg string) EvaluationAuditEvent {
	return EvaluationAuditEvent{
		EventID:             eventID,
		EvaluatedAt:         req.EvaluatedAt,
		RequestID:           requestID,
		RequestOwner:        req.Owner,
		Consumer:            req.Consumer,
		ResourceKind:        req.ResourceKind,
		ScopeMode:           req.ScopeMode,
		TargetScope:         req.TargetScope,
		Decision:            domainPolicy.DecisionReject,
		ReasonCode:          domainPolicy.ReasonCode("POLICY_EVALUATION_FAILED"),
		EnforcementMode:     PreemptionModeAuditOnly,
		EvaluationSucceeded: false,
		ErrorMessage:        errMsg,
	}
}

func (e *PipelineAuditEvaluator) emitFailureEvent(ctx context.Context, event EvaluationAuditEvent) {
	if e.Logger != nil {
		_ = e.Logger.Emit(ctx, event)
	}
}
