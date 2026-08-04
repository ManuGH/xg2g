// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package preemption

import (
	"context"
	"fmt"
	"time"
)

// TeardownStatus classifies the outcome of a target teardown request.
type TeardownStatus string

const (
	TeardownStatusStopConfirmed      TeardownStatus = "STOP_CONFIRMED"
	TeardownStatusAlreadyStopped     TeardownStatus = "ALREADY_STOPPED"
	TeardownStatusTimeout            TeardownStatus = "TIMEOUT"
	TeardownStatusTechnicalError     TeardownStatus = "TECHNICAL_ERROR"
	TeardownStatusTargetStateMutated TeardownStatus = "TARGET_STATE_MUTATED"
)

func (s TeardownStatus) IsValid() bool {
	switch s {
	case TeardownStatusStopConfirmed, TeardownStatusAlreadyStopped, TeardownStatusTimeout, TeardownStatusTechnicalError, TeardownStatusTargetStateMutated:
		return true
	default:
		return false
	}
}

// DiagnosticCode specifies structured, secret-free diagnostic reason codes.
type DiagnosticCode string

const (
	DiagnosticCodeNone              DiagnosticCode = "NONE"
	DiagnosticCodeTimeout           DiagnosticCode = "TIMEOUT"
	DiagnosticCodeHTTPError          DiagnosticCode = "HTTP_ERROR"
	DiagnosticCodeConnectionRefused DiagnosticCode = "CONNECTION_REFUSED"
	DiagnosticCodeStateMismatch     DiagnosticCode = "STATE_MISMATCH"
)

func (c DiagnosticCode) IsValid() bool {
	switch c {
	case DiagnosticCodeNone, DiagnosticCodeTimeout, DiagnosticCodeHTTPError, DiagnosticCodeConnectionRefused, DiagnosticCodeStateMismatch:
		return true
	default:
		return false
	}
}

// TransportKind identifies the physical/simulated transport mechanism.
type TransportKind string

const (
	TransportKindMock      TransportKind = "MOCK"
	TransportKindOpenWebif TransportKind = "OPENWEBIF"
	TransportKindInternal  TransportKind = "INTERNAL"
)

func (t TransportKind) IsValid() bool {
	switch t {
	case TransportKindMock, TransportKindOpenWebif, TransportKindInternal:
		return true
	default:
		return false
	}
}

// TeardownDiagnostic provides structured, secret-free diagnostic metadata.
type TeardownDiagnostic struct {
	Code      DiagnosticCode `json:"code"`
	Retryable bool           `json:"retryable"`
	Transport TransportKind  `json:"transport"`
}

// TargetTeardownRequest encapsulates a single target teardown attempt.
type TargetTeardownRequest struct {
	SagaID               string                    `json:"sagaId"`
	ReceiverID           string                    `json:"receiverId"`
	FencingToken         uint64                    `json:"fencingToken"`
	PreparedTeardownHash string                    `json:"preparedTeardownHash"`
	Target               TargetExecutionDescriptor `json:"target"`
	Deadline             time.Time                 `json:"deadline"`
}

// TargetTeardownResult represents the classified outcome of a target teardown attempt.
type TargetTeardownResult struct {
	Status             TeardownStatus     `json:"status"`
	TargetAllocationID string             `json:"targetAllocationId"`
	StoppedAt          time.Time          `json:"stoppedAt,omitempty"`
	Diagnostic         TeardownDiagnostic `json:"diagnostic"`
}

// ReceiverClaimIdentity defines the expected fencing identity for an execution attempt.
type ReceiverClaimIdentity struct {
	ReceiverID   string
	SagaID       string
	FencingToken uint64
}

// ReceiverClaimReader is the minimal read-only interface required by TeardownExecutor.
type ReceiverClaimReader interface {
	GetReceiverClaim(ctx context.Context, receiverID string) (ReceiverClaim, error)
}

// FencedMutationAuthorizer enforces authoritative fencing token revalidation immediately before physical mutation.
type FencedMutationAuthorizer interface {
	AuthorizeReceiverMutation(ctx context.Context, identity ReceiverClaimIdentity, now time.Time) error
}

// StoreFencedMutationAuthorizer implements FencedMutationAuthorizer backed by authoritative Store claim reading.
type StoreFencedMutationAuthorizer struct {
	reader ReceiverClaimReader
}

// NewStoreFencedMutationAuthorizer creates a new StoreFencedMutationAuthorizer.
func NewStoreFencedMutationAuthorizer(reader ReceiverClaimReader) *StoreFencedMutationAuthorizer {
	return &StoreFencedMutationAuthorizer{reader: reader}
}

// AuthorizeReceiverMutation revalidates active claim status, SagaID, FencingToken, quarantine status, and lease expiration.
func (a *StoreFencedMutationAuthorizer) AuthorizeReceiverMutation(ctx context.Context, identity ReceiverClaimIdentity, now time.Time) error {
	if a.reader == nil {
		return fmt.Errorf("ReceiverClaimReader is nil")
	}
	claim, err := a.reader.GetReceiverClaim(ctx, identity.ReceiverID)
	if err != nil {
		return fmt.Errorf("failed to read receiver claim: %w", err)
	}

	switch claim.Status {
	case ClaimStatusQuarantined:
		return fmt.Errorf("%w: receiver claim is quarantined", ErrFencingTokenMismatch)
	case ClaimStatusClaimed:
		// continue validation
	default:
		return fmt.Errorf("%w: claim status '%s' != CLAIMED", ErrFencingTokenMismatch, claim.Status)
	}

	if claim.SagaID != identity.SagaID {
		return fmt.Errorf("%w: claim saga ID '%s' != expected '%s'", ErrFencingTokenMismatch, claim.SagaID, identity.SagaID)
	}
	if claim.FencingToken != identity.FencingToken {
		return fmt.Errorf("%w: claim fencing token %d != expected %d", ErrFencingTokenMismatch, claim.FencingToken, identity.FencingToken)
	}
	if !now.IsZero() && !now.Before(claim.LeaseUntil) {
		return fmt.Errorf("%w: claim lease expired at %s", ErrReceiverClaimExpired, claim.LeaseUntil.Format(time.RFC3339))
	}
	return nil
}

// TeardownTransport specifies the low-level physical/RPC transport interface without fencing logic.
type TeardownTransport interface {
	TeardownTarget(ctx context.Context, req TargetTeardownRequest) (TargetTeardownResult, error)
}

// FencedTeardownGateway performs atomic fencing token validation immediately before physical transport execution.
type FencedTeardownGateway interface {
	TeardownTargetFenced(
		ctx context.Context,
		claimIdentity ReceiverClaimIdentity,
		req TargetTeardownRequest,
	) (TargetTeardownResult, error)
}

// AuthorizingTeardownGateway is the mandatory wrapper enforcing FencedMutationAuthorizer revalidation before transport invocation.
type AuthorizingTeardownGateway struct {
	authorizer FencedMutationAuthorizer
	transport  TeardownTransport
	nowFunc    func() time.Time
}

// NewAuthorizingTeardownGateway creates an AuthorizingTeardownGateway. Both authorizer and transport MUST be non-nil.
func NewAuthorizingTeardownGateway(authorizer FencedMutationAuthorizer, transport TeardownTransport) (*AuthorizingTeardownGateway, error) {
	if authorizer == nil {
		return nil, fmt.Errorf("FencedMutationAuthorizer is nil")
	}
	if transport == nil {
		return nil, fmt.Errorf("TeardownTransport is nil")
	}
	return &AuthorizingTeardownGateway{
		authorizer: authorizer,
		transport:  transport,
		nowFunc:    func() time.Time { return time.Now().UTC() },
	}, nil
}

// TeardownTargetFenced revalidates fencing token via authorizer immediately prior to calling physical transport.
func (g *AuthorizingTeardownGateway) TeardownTargetFenced(ctx context.Context, claimIdentity ReceiverClaimIdentity, req TargetTeardownRequest) (TargetTeardownResult, error) {
	now := g.nowFunc()
	if err := g.authorizer.AuthorizeReceiverMutation(ctx, claimIdentity, now); err != nil {
		return TargetTeardownResult{}, err
	}

	select {
	case <-ctx.Done():
		return TargetTeardownResult{}, ctx.Err()
	default:
	}

	return g.transport.TeardownTarget(ctx, req)
}
