// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package preemption

import (
	"context"
	"sync"
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

type synchronizedTOCTOUClaimReader struct {
	mu           sync.Mutex
	callCount    int
	initialClaim ReceiverClaim
	bumpedClaim  ReceiverClaim
}

func (s *synchronizedTOCTOUClaimReader) GetReceiverClaim(ctx context.Context, receiverID string) (ReceiverClaim, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.callCount++
	if s.callCount == 1 {
		return s.initialClaim, nil
	}
	return s.bumpedClaim, nil
}

type mockTeardownTransport struct {
	calls  int32
	result TargetTeardownResult
	err    error
}

func (t *mockTeardownTransport) TeardownTarget(ctx context.Context, req TargetTeardownRequest) (TargetTeardownResult, error) {
	atomic.AddInt32(&t.calls, 1)
	if t.err != nil {
		return TargetTeardownResult{}, t.err
	}
	return t.result, nil
}

func newTestTeardownExecutor(reader ReceiverClaimReader, authorizer FencedMutationAuthorizer, transport TeardownTransport) *TeardownExecutor {
	exec, err := NewTeardownExecutor(reader, authorizer, transport)
	if err != nil {
		panic(err)
	}
	exec.allowMock = true
	return exec
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

func TestTeardownExecutor_ConstructorRejectsNilDependencies(t *testing.T) {
	reader := &mockClaimReader{}
	authorizer := NewStoreFencedMutationAuthorizer(reader)
	transport := &mockTeardownTransport{}

	// Nil reader
	_, err := NewTeardownExecutor(nil, authorizer, transport)
	require.Error(t, err)
	require.Contains(t, err.Error(), "ReceiverClaimReader is nil")

	// Nil authorizer
	_, err = NewTeardownExecutor(reader, nil, transport)
	require.Error(t, err)
	require.Contains(t, err.Error(), "FencedMutationAuthorizer is nil")

	// Nil transport
	_, err = NewTeardownExecutor(reader, authorizer, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "TeardownTransport is nil")

	// Valid creation
	exec, err := NewTeardownExecutor(reader, authorizer, transport)
	require.NoError(t, err)
	require.NotNil(t, exec)
	require.NotNil(t, exec.gateway)
}

func TestPrepareTeardown_RejectsMissingAllocationRevision(t *testing.T) {
	_, contract, snapshot, proof, now := setupTeardownTestContext(t)
	preparer := NewTeardownPreparer()

	// Mutate snapshot: ActiveAllocation.Revision is empty -> MUST fail-closed!
	snapshotNoRev := snapshot
	snapshotNoRev.Allocations = []ActiveAllocation{
		{
			AllocationID: "alloc-1",
			Owner:        "client-B",
			Revision:     "", // Empty revision!
			Claims:       contract.RequestedResources,
		},
	}

	prepRes, err := preparer.PrepareTeardown(contract, snapshotNoRev, proof, now)
	require.NoError(t, err)
	require.Equal(t, DecisionRejected, prepRes.Decision)
	require.Equal(t, ReasonTeardownTargetStateMutated, prepRes.Reason)
}

func TestPrepareTeardown_RejectsEmptyAllocationIDsAndMissingMappings(t *testing.T) {
	_, contract, snapshot, proof, now := setupTeardownTestContext(t)
	preparer := NewTeardownPreparer()

	// 1. Empty allocation ID in snapshot -> Rejection
	snapEmptyID := snapshot
	snapEmptyID.Allocations = []ActiveAllocation{
		{AllocationID: "", Owner: "client-B", Revision: "rev-1", Claims: contract.RequestedResources},
	}
	resSnapEmpty, err := preparer.PrepareTeardown(contract, snapEmptyID, proof, now)
	require.NoError(t, err)
	require.Equal(t, DecisionRejected, resSnapEmpty.Decision)
	require.Equal(t, ReasonTeardownTargetStateMutated, resSnapEmpty.Reason)

	// 2. Empty allocation ID in proof mapping -> Rejection
	proofEmptyID := proof
	proofEmptyID.AllocationMappings = []AllocationResourceMapping{
		{AllocationID: "", FreedResources: contract.RequestedResources},
	}
	resProofEmpty, err := preparer.PrepareTeardown(contract, snapshot, proofEmptyID, now)
	require.NoError(t, err)
	require.Equal(t, DecisionRejected, resProofEmpty.Decision)
	require.Equal(t, ReasonTeardownInvalidProof, resProofEmpty.Reason)

	// 3. Missing proof mapping for contract target -> Rejection
	proofMissingMapping := proof
	proofMissingMapping.AllocationMappings = []AllocationResourceMapping{} // Empty mappings!
	resMissingMap, err := preparer.PrepareTeardown(contract, snapshot, proofMissingMapping, now)
	require.NoError(t, err)
	require.Equal(t, DecisionRejected, resMissingMap.Decision)
	require.Equal(t, ReasonTeardownTargetStateMutated, resMissingMap.Reason)
}

func TestPrepareTeardown_RejectsDuplicateSnapshotAllocationsAndProofMappings(t *testing.T) {
	_, contract, snapshot, proof, now := setupTeardownTestContext(t)
	preparer := NewTeardownPreparer()

	// 1. Duplicate allocation in snapshot -> Rejection
	snapDup := snapshot
	snapDup.Allocations = []ActiveAllocation{
		{AllocationID: "alloc-1", Owner: "client-B", Revision: "rev-1", Claims: contract.RequestedResources},
		{AllocationID: "alloc-1", Owner: "client-B", Revision: "rev-1", Claims: contract.RequestedResources},
	}
	resDup, err := preparer.PrepareTeardown(contract, snapDup, proof, now)
	require.NoError(t, err)
	require.Equal(t, DecisionRejected, resDup.Decision)
	require.Equal(t, ReasonTeardownTargetStateMutated, resDup.Reason)

	// 2. Duplicate mapping in proof -> Rejection
	proofDup := proof
	proofDup.AllocationMappings = []AllocationResourceMapping{
		{AllocationID: "alloc-1", FreedResources: contract.RequestedResources},
		{AllocationID: "alloc-1", FreedResources: contract.RequestedResources},
	}
	resProofDup, err := preparer.PrepareTeardown(contract, snapshot, proofDup, now)
	require.NoError(t, err)
	require.Equal(t, DecisionRejected, resProofDup.Decision)
	require.Equal(t, ReasonTeardownInvalidProof, resProofDup.Reason)
}

func TestAuthorizingTeardownGateway_ConstructorRejectsNil(t *testing.T) {
	reader := &mockClaimReader{}
	authorizer := NewStoreFencedMutationAuthorizer(reader)
	transport := &mockTeardownTransport{}

	_, err := NewAuthorizingTeardownGateway(nil, transport)
	require.Error(t, err)
	require.Contains(t, err.Error(), "FencedMutationAuthorizer is nil")

	_, err = NewAuthorizingTeardownGateway(authorizer, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "TeardownTransport is nil")
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
	bumpedClaim := validClaim
	bumpedClaim.FencingToken = 99 // Token bumped while in flight!

	syncedReader := &synchronizedTOCTOUClaimReader{
		initialClaim: validClaim,
		bumpedClaim:  bumpedClaim,
	}

	authorizer := NewStoreFencedMutationAuthorizer(syncedReader)
	transport := &mockTeardownTransport{
		result: TargetTeardownResult{
			Status:             TeardownStatusStopConfirmed,
			TargetAllocationID: "alloc-1",
			StoppedAt:          now,
			Diagnostic:         TeardownDiagnostic{Code: DiagnosticCodeNone, Retryable: false, Transport: TransportKindMock},
		},
	}

	executor := newTestTeardownExecutor(syncedReader, authorizer, transport)

	// Executor pre-check sees initialClaim (Token 10) -> Passes Executor pre-check!
	// Right before transport start, AuthorizingTeardownGateway calls Authorizer, which reads bumpedClaim (Token 99).
	// Authorizer detects mismatch (10 != 99) and rejects execution before calling transport!
	_, err = executor.ExecuteTargetTeardown(ctx, prepRes.Prepared, "saga-1", 10, "alloc-1", now.Add(10*time.Second))
	require.Error(t, err)
	require.Contains(t, err.Error(), "fencing token")

	// Verify Authorizer was called during gateway execution
	require.Equal(t, 2, syncedReader.callCount, "Reader must be called twice: once for Executor pre-check, once for AuthorizingGateway")

	// Verify Transport was NEVER called (calls counter == 0)
	require.Equal(t, int32(0), atomic.LoadInt32(&transport.calls), "Physical transport MUST NOT be launched when FencedMutationAuthorizer fails")
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
	authorizer := NewStoreFencedMutationAuthorizer(reader)
	transport := &mockTeardownTransport{}
	executor := newTestTeardownExecutor(reader, authorizer, transport)

	// Cancel context prior to execution
	cancelledCtx, cancel := context.WithCancel(ctx)
	cancel() // Cancelled!

	_, err = executor.ExecuteTargetTeardown(cancelledCtx, prepRes.Prepared, "saga-1", 10, "alloc-1", now.Add(10*time.Second))
	require.Error(t, err)
	require.Equal(t, int32(0), atomic.LoadInt32(&transport.calls), "Transport call MUST NOT be launched when context is cancelled")
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
	authorizer := NewStoreFencedMutationAuthorizer(reader)
	transport := &mockTeardownTransport{}
	executor := newTestTeardownExecutor(reader, authorizer, transport)

	// 1. TIMEOUT status with non-zero StoppedAt -> Rejection
	transport.result = TargetTeardownResult{
		Status:             TeardownStatusTimeout,
		TargetAllocationID: "alloc-1",
		StoppedAt:          now, // INVALID for TIMEOUT!
		Diagnostic:         TeardownDiagnostic{Code: DiagnosticCodeTimeout, Retryable: true, Transport: TransportKindMock},
	}
	_, err = executor.ExecuteTargetTeardown(ctx, prepRes.Prepared, "saga-1", 10, "alloc-1", now.Add(10*time.Second))
	require.Error(t, err)
	require.Contains(t, err.Error(), "TIMEOUT requires zero StoppedAt")

	// 2. TARGET_STATE_MUTATED status with non-zero StoppedAt -> Rejection
	transport.result = TargetTeardownResult{
		Status:             TeardownStatusTargetStateMutated,
		TargetAllocationID: "alloc-1",
		StoppedAt:          now, // INVALID for TARGET_STATE_MUTATED!
		Diagnostic:         TeardownDiagnostic{Code: DiagnosticCodeStateMismatch, Retryable: false, Transport: TransportKindMock},
	}
	_, err = executor.ExecuteTargetTeardown(ctx, prepRes.Prepared, "saga-1", 10, "alloc-1", now.Add(10*time.Second))
	require.Error(t, err)
	require.Contains(t, err.Error(), "TARGET_STATE_MUTATED requires zero StoppedAt")

	// 3. Valid TIMEOUT with zero StoppedAt -> Approved by Matrix
	transport.result = TargetTeardownResult{
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

	transport := &mockTeardownTransport{
		result: TargetTeardownResult{
			Status:             TeardownStatusStopConfirmed,
			TargetAllocationID: "alloc-1",
			StoppedAt:          now,
			Diagnostic:         TeardownDiagnostic{Code: DiagnosticCodeNone, Retryable: false, Transport: TransportKindMock},
		},
	}
	authorizer := NewStoreFencedMutationAuthorizer(memoryStore)
	executor := newTestTeardownExecutor(memoryStore, authorizer, transport)

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
