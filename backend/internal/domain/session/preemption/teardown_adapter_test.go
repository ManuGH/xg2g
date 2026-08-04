// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package preemption

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type mockClaimReader struct {
	claim ReceiverClaim
	err   error
}

func (m *mockClaimReader) GetReceiverClaim(ctx context.Context, receiverID string) (ReceiverClaim, error) {
	if m.err != nil {
		return ReceiverClaim{}, m.err
	}
	return m.claim, nil
}

type mockFencedGateway struct {
	claimReader ReceiverClaimReader
	calls       int32
	result      TargetTeardownResult
	err         error
}

func (g *mockFencedGateway) TeardownTargetFenced(ctx context.Context, claimIdent ReceiverClaimIdentity, req TargetTeardownRequest) (TargetTeardownResult, error) {
	// Atomic TOCTOU fencing revalidation at gateway entry!
	claim, err := g.claimReader.GetReceiverClaim(ctx, claimIdent.ReceiverID)
	if err != nil {
		return TargetTeardownResult{}, err
	}
	if claim.Status != ClaimStatusClaimed || claim.SagaID != claimIdent.SagaID || claim.FencingToken != claimIdent.FencingToken || claim.Status == ClaimStatusQuarantined {
		return TargetTeardownResult{}, ErrFencingTokenMismatch
	}

	select {
	case <-ctx.Done():
		return TargetTeardownResult{}, ctx.Err()
	default:
	}

	atomic.AddInt32(&g.calls, 1)

	if g.err != nil {
		return TargetTeardownResult{}, g.err
	}
	return g.result, nil
}

func setupTeardownTestContext(t *testing.T) (context.Context, *PreemptionExecutionContract, ResourceSnapshot, ConflictResolutionProof, time.Time) {
	now := time.Now().UTC()
	reqClaim := []ResourceClaim{{Kind: ResourceKindTunerSlot, Resource: "tuner-1", Quantity: 1}}

	contract := &PreemptionExecutionContract{
		ContractID:              "c-1",
		ReceiverID:              "rec-1",
		RequestID:               "req-1",
		RequesterOwner:          "client-A",
		RequesterAllocationID:   "alloc-req-1",
		RequesterRevision:       "rev-req-1",
		TargetAllocationIDs:     []string{"alloc-1"},
		RequestedResources:      reqClaim,
		ExpectedFreedResources:  reqClaim,
		SnapshotRevision:        "rev-100",
		HardwareProfileRevision: "hw-rev-1",
		ConflictProofRevision:   "rev-100",
		CreatedAt:               now.Add(-5 * time.Second),
		ExpiresAt:               now.Add(30 * time.Second),
	}
	cHash, err := ComputeContractHash(contract)
	require.NoError(t, err)
	contract.ContractHash = cHash

	snapshot := ResourceSnapshot{
		ReceiverID:       "rec-1",
		SnapshotRevision: "rev-100",
		ObservedAt:       now,
		Allocations: []ActiveAllocation{
			{
				AllocationID: "alloc-1",
				Owner:        "client-B",
				Revision:     "alloc-rev-100",
				Priority:     PriorityAttributes{BasePriority: 50},
				Claims:       reqClaim,
			},
		},
	}

	proof := ConflictResolutionProof{
		ReceiverID:              "rec-1",
		SnapshotRevision:        "rev-100",
		HardwareProfileRevision: "hw-rev-1",
		HardwareProfileStatus:   HardwareProfileValid,
		EvidenceClassification:  EvidenceDirectObservation,
		RequestedResources:      reqClaim,
		AllocationMappings: []AllocationResourceMapping{
			{AllocationID: "alloc-1", FreedResources: reqClaim},
		},
	}

	return context.Background(), contract, snapshot, proof, now
}

func TestTeardownExecutor_PreparedTeardownDescriptorBinding(t *testing.T) {
	_, contract, snapshot, proof, now := setupTeardownTestContext(t)
	preparer := NewTeardownPreparer()

	prepRes, err := preparer.PrepareTeardown(contract, snapshot, proof, now)
	require.NoError(t, err)
	require.Equal(t, DecisionApproved, prepRes.Decision)

	prepared := prepRes.Prepared
	require.Len(t, prepared.TargetDescriptors, 1)
	require.NotEmpty(t, prepared.TargetDescriptorsHash)

	// Verify PreparedTeardown hash validation succeeds
	err = ValidatePreparedTeardown(prepared, now)
	require.NoError(t, err)

	// Tamper descriptor owner -> Hash validation MUST fail!
	tampered := *prepared
	tamperedDescriptors := append([]TargetExecutionDescriptor(nil), prepared.TargetDescriptors...)
	tamperedDescriptors[0].ExpectedOwner = "MALICIOUS_OWNER"
	tampered.TargetDescriptors = tamperedDescriptors

	err = ValidatePreparedTeardown(&tampered, now)
	require.Error(t, err)
}

func TestTeardownGateway_TOCTOUFencingRevalidation(t *testing.T) {
	ctx, contract, snapshot, proof, now := setupTeardownTestContext(t)
	preparer := NewTeardownPreparer()

	prepRes, err := preparer.PrepareTeardown(contract, snapshot, proof, now)
	require.NoError(t, err)

	validClaim := ReceiverClaim{
		ReceiverID:   "rec-1",
		SagaID:       "saga-1",
		FencingToken: 10,
		Status:       ClaimStatusClaimed,
		LeaseUntil:   now.Add(30 * time.Second),
	}

	reader := &mockClaimReader{claim: validClaim}
	gateway := &mockFencedGateway{
		claimReader: reader,
		result: TargetTeardownResult{
			Status:             TeardownStatusStopConfirmed,
			TargetAllocationID: "alloc-1",
			StoppedAt:          now,
			Diagnostic:         TeardownDiagnostic{Code: DiagnosticCodeNone, Retryable: false, Transport: TransportKindMock},
		},
	}

	executor := NewTeardownExecutor(reader, gateway)
	executor.SetAllowMockForTesting(true)

	// 1. Valid execution succeeds
	res, err := executor.ExecuteTargetTeardown(ctx, prepRes.Prepared, "saga-1", 10, "alloc-1", now.Add(10*time.Second))
	require.NoError(t, err)
	require.Equal(t, TeardownStatusStopConfirmed, res.Status)
	require.Equal(t, int32(1), atomic.LoadInt32(&gateway.calls))

	// 2. TOCTOU Revocation: Worker claim is revoked right before gateway entry -> Fencing revalidation rejects execution!
	revokedClaim := validClaim
	revokedClaim.FencingToken = 99 // Token bumped!
	reader.claim = revokedClaim

	_, err = executor.ExecuteTargetTeardown(ctx, prepRes.Prepared, "saga-1", 10, "alloc-1", now.Add(10*time.Second))
	require.Error(t, err)
	require.Contains(t, err.Error(), "fencing token")
	// Gateway transport calls counter must NOT increase!
	require.Equal(t, int32(1), atomic.LoadInt32(&gateway.calls))
}

func TestTeardownGateway_NativeContextCancellation(t *testing.T) {
	ctx, contract, snapshot, proof, now := setupTeardownTestContext(t)
	preparer := NewTeardownPreparer()
	prepRes, err := preparer.PrepareTeardown(contract, snapshot, proof, now)
	require.NoError(t, err)

	validClaim := ReceiverClaim{
		ReceiverID:   "rec-1",
		SagaID:       "saga-1",
		FencingToken: 10,
		Status:       ClaimStatusClaimed,
		LeaseUntil:   now.Add(30 * time.Second),
	}

	reader := &mockClaimReader{claim: validClaim}
	gateway := &mockFencedGateway{claimReader: reader}
	executor := NewTeardownExecutor(reader, gateway)
	executor.SetAllowMockForTesting(true)

	// Cancel context prior to execution
	cancelledCtx, cancel := context.WithCancel(ctx)
	cancel() // Cancelled!

	_, err = executor.ExecuteTargetTeardown(cancelledCtx, prepRes.Prepared, "saga-1", 10, "alloc-1", now.Add(10*time.Second))
	require.Error(t, err)
	require.Equal(t, int32(0), atomic.LoadInt32(&gateway.calls), "Transport call MUST NOT be launched when context is cancelled")
}

func TestTeardownExecutor_ResultMatrixValidation(t *testing.T) {
	ctx, contract, snapshot, proof, now := setupTeardownTestContext(t)
	preparer := NewTeardownPreparer()
	prepRes, err := preparer.PrepareTeardown(contract, snapshot, proof, now)
	require.NoError(t, err)

	validClaim := ReceiverClaim{
		ReceiverID:   "rec-1",
		SagaID:       "saga-1",
		FencingToken: 10,
		Status:       ClaimStatusClaimed,
		LeaseUntil:   now.Add(30 * time.Second),
	}

	reader := &mockClaimReader{claim: validClaim}
	gateway := &mockFencedGateway{claimReader: reader}
	executor := NewTeardownExecutor(reader, gateway)
	executor.SetAllowMockForTesting(true)

	// 1. TIMEOUT status with non-zero StoppedAt -> Rejection
	gateway.result = TargetTeardownResult{
		Status:             TeardownStatusTimeout,
		TargetAllocationID: "alloc-1",
		StoppedAt:          now, // INVALID for TIMEOUT!
		Diagnostic:         TeardownDiagnostic{Code: DiagnosticCodeTimeout, Retryable: true, Transport: TransportKindMock},
	}
	_, err = executor.ExecuteTargetTeardown(ctx, prepRes.Prepared, "saga-1", 10, "alloc-1", now.Add(10*time.Second))
	require.Error(t, err)
	require.Contains(t, err.Error(), "TIMEOUT requires zero StoppedAt")

	// 2. TARGET_STATE_MUTATED status with non-zero StoppedAt -> Rejection
	gateway.result = TargetTeardownResult{
		Status:             TeardownStatusTargetStateMutated,
		TargetAllocationID: "alloc-1",
		StoppedAt:          now, // INVALID for TARGET_STATE_MUTATED!
		Diagnostic:         TeardownDiagnostic{Code: DiagnosticCodeStateMismatch, Retryable: false, Transport: TransportKindMock},
	}
	_, err = executor.ExecuteTargetTeardown(ctx, prepRes.Prepared, "saga-1", 10, "alloc-1", now.Add(10*time.Second))
	require.Error(t, err)
	require.Contains(t, err.Error(), "TARGET_STATE_MUTATED requires zero StoppedAt")

	// 3. Valid TIMEOUT with zero StoppedAt -> Approved by Matrix
	gateway.result = TargetTeardownResult{
		Status:             TeardownStatusTimeout,
		TargetAllocationID: "alloc-1",
		StoppedAt:          time.Time{}, // Valid zero time
		Diagnostic:         TeardownDiagnostic{Code: DiagnosticCodeTimeout, Retryable: true, Transport: TransportKindMock},
	}
	res, err := executor.ExecuteTargetTeardown(ctx, prepRes.Prepared, "saga-1", 10, "alloc-1", now.Add(10*time.Second))
	require.NoError(t, err)
	require.Equal(t, TeardownStatusTimeout, res.Status)
}

func TestTeardownExecutor_ZeroStateMutations(t *testing.T) {
	ctx, contract, snapshot, proof, now := setupTeardownTestContext(t)
	preparer := NewTeardownPreparer()
	prepRes, err := preparer.PrepareTeardown(contract, snapshot, proof, now)
	require.NoError(t, err)

	memoryStore := NewMemorySagaStore()

	// Pre-create saga in store
	engine := NewPreemptionSagaEngine(memoryStore)
	saga, claim, err := engine.CreateSaga(ctx, prepRes.Prepared, PreemptionModeEnforce, now)
	require.NoError(t, err)

	sagaClaimed, claimedClaim, err := engine.ClaimExecution(ctx, saga.SagaID, "rec-1", 30*time.Second, now)
	require.NoError(t, err)

	gateway := &mockFencedGateway{
		claimReader: memoryStore,
		result: TargetTeardownResult{
			Status:             TeardownStatusStopConfirmed,
			TargetAllocationID: "alloc-1",
			StoppedAt:          now,
			Diagnostic:         TeardownDiagnostic{Code: DiagnosticCodeNone, Retryable: false, Transport: TransportKindMock},
		},
	}

	executor := NewTeardownExecutor(memoryStore, gateway)
	executor.SetAllowMockForTesting(true)

	// Execute teardown
	res, err := executor.ExecuteTargetTeardown(ctx, prepRes.Prepared, saga.SagaID, claimedClaim.FencingToken, "alloc-1", now.Add(10*time.Second))
	require.NoError(t, err)
	require.Equal(t, TeardownStatusStopConfirmed, res.Status)

	// Assert SagaStore state is COMPLETELY UNMUTATED (State remains StateExecutionClaimed, Version remains 2)
	sagaAfter, getErr := memoryStore.GetSaga(ctx, saga.SagaID)
	require.NoError(t, getErr)
	require.Equal(t, StateExecutionClaimed, sagaAfter.State)
	require.Equal(t, sagaClaimed.Version, sagaAfter.Version)

	// Assert ReceiverClaim is COMPLETELY UNMUTATED (Status remains ClaimStatusClaimed, FencingToken remains claimedClaim.FencingToken)
	claimAfter, claimErr := memoryStore.GetReceiverClaim(ctx, "rec-1")
	require.NoError(t, claimErr)
	require.Equal(t, ClaimStatusClaimed, claimAfter.Status)
	require.Equal(t, claimedClaim.FencingToken, claimAfter.FencingToken)
	_ = claim
}
