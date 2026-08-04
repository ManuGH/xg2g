// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package preemption

import (
	"context"
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

// FencedTeardownGateway performs atomic fencing token validation immediately before physical transport execution.
type FencedTeardownGateway interface {
	TeardownTargetFenced(
		ctx context.Context,
		claimIdentity ReceiverClaimIdentity,
		req TargetTeardownRequest,
	) (TargetTeardownResult, error)
}
