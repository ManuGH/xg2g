// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package preemption

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrDeadlineExceeded          = errors.New("teardown deadline exceeded or invalid remaining duration")
	ErrReceiverClaimExpired      = errors.New("receiver claim has expired or is invalid")
	ErrTargetDescriptorNotFound  = errors.New("target execution descriptor not found in prepared teardown")
	ErrInvalidTeardownResult     = errors.New("invalid target teardown result returned by gateway")
	ErrDisallowedTransportInProd = errors.New("TransportKindMock is disallowed outside test environment")
)

// TeardownExecutor coordinates target teardown requests using read-only claim validation and mandatory AuthorizingTeardownGateway.
type TeardownExecutor struct {
	claimReader ReceiverClaimReader
	gateway     *AuthorizingTeardownGateway
	nowFunc     func() time.Time
	allowMock   bool
}

// NewTeardownExecutor creates a production TeardownExecutor instance.
// It requires ReceiverClaimReader, FencedMutationAuthorizer, and TeardownTransport, constructing an AuthorizingTeardownGateway internally.
func NewTeardownExecutor(reader ReceiverClaimReader, authorizer FencedMutationAuthorizer, transport TeardownTransport) (*TeardownExecutor, error) {
	if reader == nil {
		return nil, fmt.Errorf("ReceiverClaimReader is nil")
	}
	authGateway, err := NewAuthorizingTeardownGateway(authorizer, transport)
	if err != nil {
		return nil, fmt.Errorf("failed to create AuthorizingTeardownGateway: %w", err)
	}
	return &TeardownExecutor{
		claimReader: reader,
		gateway:     authGateway,
		nowFunc:     func() time.Time { return time.Now().UTC() },
		allowMock:   false,
	}, nil
}

// ExecuteTargetTeardown executes a single target teardown attempt from an authoritative PreparedTeardown dataset.
func (e *TeardownExecutor) ExecuteTargetTeardown(
	ctx context.Context,
	prepared *PreparedTeardown,
	sagaID string,
	fencingToken uint64,
	targetAllocationID string,
	deadline time.Time,
) (TargetTeardownResult, error) {
	now := e.nowFunc()

	// 1. Authoritative PreparedTeardown validation
	if err := ValidatePreparedTeardown(prepared, now); err != nil {
		return TargetTeardownResult{}, fmt.Errorf("invalid prepared teardown: %w", err)
	}

	if strings.TrimSpace(sagaID) == "" {
		return TargetTeardownResult{}, fmt.Errorf("empty saga ID")
	}
	if fencingToken == 0 {
		return TargetTeardownResult{}, fmt.Errorf("zero fencing token")
	}

	// 2. Select descriptor directly from prepared.TargetDescriptors
	var targetDesc *TargetExecutionDescriptor
	for i := range prepared.TargetDescriptors {
		if prepared.TargetDescriptors[i].AllocationID == targetAllocationID {
			targetDesc = &prepared.TargetDescriptors[i]
			break
		}
	}
	if targetDesc == nil {
		return TargetTeardownResult{}, fmt.Errorf("%w: allocation ID '%s'", ErrTargetDescriptorNotFound, targetAllocationID)
	}

	if err := ValidateDescriptor(*targetDesc); err != nil {
		return TargetTeardownResult{}, fmt.Errorf("target descriptor invalid: %w", err)
	}

	// 3. Read-only claim verification via ReceiverClaimReader
	if e.claimReader == nil {
		return TargetTeardownResult{}, fmt.Errorf("ReceiverClaimReader is nil")
	}
	claim, err := e.claimReader.GetReceiverClaim(ctx, prepared.ReceiverID)
	if err != nil {
		return TargetTeardownResult{}, fmt.Errorf("failed to read receiver claim: %w", err)
	}

	switch claim.Status {
	case ClaimStatusQuarantined:
		return TargetTeardownResult{}, fmt.Errorf("%w: receiver claim is quarantined", ErrFencingTokenMismatch)
	case ClaimStatusClaimed:
		// continue validation
	default:
		return TargetTeardownResult{}, fmt.Errorf("%w: claim status '%s' != CLAIMED", ErrFencingTokenMismatch, claim.Status)
	}

	if claim.SagaID != sagaID {
		return TargetTeardownResult{}, fmt.Errorf("%w: claim saga ID '%s' != request saga ID '%s'", ErrFencingTokenMismatch, claim.SagaID, sagaID)
	}
	if claim.FencingToken != fencingToken {
		return TargetTeardownResult{}, fmt.Errorf("%w: claim fencing token %d != request token %d", ErrFencingTokenMismatch, claim.FencingToken, fencingToken)
	}
	if claim.ReceiverID != prepared.ReceiverID {
		return TargetTeardownResult{}, fmt.Errorf("%w: claim receiver ID '%s' != prepared receiver ID '%s'", ErrFencingTokenMismatch, claim.ReceiverID, prepared.ReceiverID)
	}
	if !now.Before(claim.LeaseUntil) {
		return TargetTeardownResult{}, fmt.Errorf("%w: claim expired at %s (now: %s)", ErrReceiverClaimExpired, claim.LeaseUntil.Format(time.RFC3339), now.Format(time.RFC3339))
	}

	// 4. Clamped deadline enforcement
	if deadline.IsZero() {
		return TargetTeardownResult{}, fmt.Errorf("%w: zero deadline", ErrDeadlineExceeded)
	}
	effectiveDeadline := deadline
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(effectiveDeadline) {
		effectiveDeadline = ctxDeadline
	}
	if !now.Before(effectiveDeadline) {
		return TargetTeardownResult{}, fmt.Errorf("%w: effective deadline %s expired at %s", ErrDeadlineExceeded, effectiveDeadline.Format(time.RFC3339), now.Format(time.RFC3339))
	}

	// 5. Derived context with clamped deadline
	derivedCtx, cancel := context.WithDeadline(ctx, effectiveDeadline)
	defer cancel()

	if e.gateway == nil {
		return TargetTeardownResult{}, fmt.Errorf("AuthorizingTeardownGateway is nil")
	}

	claimIdent := ReceiverClaimIdentity{
		ReceiverID:   prepared.ReceiverID,
		SagaID:       sagaID,
		FencingToken: fencingToken,
	}

	req := TargetTeardownRequest{
		SagaID:               sagaID,
		ReceiverID:           prepared.ReceiverID,
		FencingToken:         fencingToken,
		PreparedTeardownHash: prepared.PreparedTeardownHash,
		Target:               *targetDesc,
		Deadline:             effectiveDeadline,
	}

	// 6. Synchronous call to mandatory AuthorizingTeardownGateway
	res, err := e.gateway.TeardownTargetFenced(derivedCtx, claimIdent, req)
	if err != nil {
		return TargetTeardownResult{}, err
	}

	// 7. Validate Gateway Result & Matrix Compliance
	if err := e.validateResultMatrix(res, targetAllocationID); err != nil {
		return TargetTeardownResult{}, fmt.Errorf("%w: %v", ErrInvalidTeardownResult, err)
	}

	return res, nil
}

func (e *TeardownExecutor) validateResultMatrix(res TargetTeardownResult, expectedAllocID string) error {
	if !res.Status.IsValid() {
		return fmt.Errorf("unknown teardown status '%s'", res.Status)
	}
	if !res.Diagnostic.Code.IsValid() {
		return fmt.Errorf("unknown diagnostic code '%s'", res.Diagnostic.Code)
	}
	if !res.Diagnostic.Transport.IsValid() {
		return fmt.Errorf("unknown transport kind '%s'", res.Diagnostic.Transport)
	}

	if res.Diagnostic.Transport == TransportKindMock && !e.allowMock {
		return ErrDisallowedTransportInProd
	}

	if res.TargetAllocationID != expectedAllocID {
		return fmt.Errorf("result target allocation ID '%s' != expected '%s'", res.TargetAllocationID, expectedAllocID)
	}

	switch res.Status {
	case TeardownStatusStopConfirmed:
		if res.Diagnostic.Code != DiagnosticCodeNone {
			return fmt.Errorf("STOP_CONFIRMED must have DiagnosticCodeNone, got %s", res.Diagnostic.Code)
		}
		if res.Diagnostic.Retryable {
			return fmt.Errorf("STOP_CONFIRMED must not be retryable")
		}
		if res.StoppedAt.IsZero() {
			return fmt.Errorf("STOP_CONFIRMED requires non-zero StoppedAt timestamp")
		}

	case TeardownStatusAlreadyStopped:
		if res.Diagnostic.Code != DiagnosticCodeNone {
			return fmt.Errorf("ALREADY_STOPPED must have DiagnosticCodeNone, got %s", res.Diagnostic.Code)
		}
		if res.Diagnostic.Retryable {
			return fmt.Errorf("ALREADY_STOPPED must not be retryable")
		}
		if res.StoppedAt.IsZero() {
			return fmt.Errorf("ALREADY_STOPPED requires non-zero StoppedAt timestamp")
		}

	case TeardownStatusTimeout:
		if res.Diagnostic.Code != DiagnosticCodeTimeout {
			return fmt.Errorf("TIMEOUT requires DiagnosticCodeTimeout, got %s", res.Diagnostic.Code)
		}
		if !res.Diagnostic.Retryable {
			return fmt.Errorf("TIMEOUT must be retryable")
		}
		if !res.StoppedAt.IsZero() {
			return fmt.Errorf("TIMEOUT requires zero StoppedAt timestamp")
		}

	case TeardownStatusTargetStateMutated:
		if res.Diagnostic.Code != DiagnosticCodeStateMismatch {
			return fmt.Errorf("TARGET_STATE_MUTATED requires DiagnosticCodeStateMismatch, got %s", res.Diagnostic.Code)
		}
		if res.Diagnostic.Retryable {
			return fmt.Errorf("TARGET_STATE_MUTATED must not be retryable")
		}
		if !res.StoppedAt.IsZero() {
			return fmt.Errorf("TARGET_STATE_MUTATED requires zero StoppedAt timestamp")
		}

	case TeardownStatusTechnicalError:
		if res.Diagnostic.Code != DiagnosticCodeHTTPError && res.Diagnostic.Code != DiagnosticCodeConnectionRefused {
			return fmt.Errorf("TECHNICAL_ERROR requires HTTP_ERROR or CONNECTION_REFUSED, got %s", res.Diagnostic.Code)
		}
		if !res.Diagnostic.Retryable {
			return fmt.Errorf("TECHNICAL_ERROR must be retryable")
		}
		if !res.StoppedAt.IsZero() {
			return fmt.Errorf("TECHNICAL_ERROR requires zero StoppedAt timestamp")
		}

	default:
		return fmt.Errorf("unhandled status '%s'", res.Status)
	}

	return nil
}
