package policy

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

// 1. Matrix Completeness & Rule Test: Tests all 36 entries directly against ConflictRuleFor.
func TestConflictMatrix_Rules(t *testing.T) {
	if len(defaultConflictMatrix) != 6 {
		t.Fatalf("expected 6 existing consumer rows in matrix, got %d", len(defaultConflictMatrix))
	}

	tests := []struct {
		existing       ConsumerType
		incoming       ConsumerType
		wantDecision   PreemptionDecision
		wantReasonCode ReasonCode
		wantLossClass  LossClass
	}{
		// 1. Existing: SCHEDULED_RECORDING (Sacrosanct - Unconditionally Protects Recording)
		{ConsumerScheduledRecording, ConsumerScheduledRecording, DecisionReject, ReasonPolicyRejectedProtectedActivity, LossScheduled},
		{ConsumerScheduledRecording, ConsumerManualRecording, DecisionReject, ReasonPolicyRejectedProtectedActivity, LossScheduled},
		{ConsumerScheduledRecording, ConsumerLiveTV, DecisionReject, ReasonPolicyRejectedProtectedActivity, LossScheduled},
		{ConsumerScheduledRecording, ConsumerRetroDVR, DecisionReject, ReasonPolicyRejectedProtectedActivity, LossScheduled},
		{ConsumerScheduledRecording, ConsumerChannelScan, DecisionReject, ReasonPolicyRejectedProtectedActivity, LossScheduled},
		{ConsumerScheduledRecording, ConsumerBackgroundTransfer, DecisionReject, ReasonPolicyRejectedProtectedActivity, LossScheduled},

		// 2. Existing: MANUAL_RECORDING
		{ConsumerManualRecording, ConsumerScheduledRecording, DecisionPreemptionRequired, ReasonPolicyPreemptionRequired, LossManual},
		{ConsumerManualRecording, ConsumerManualRecording, DecisionReject, ReasonPolicyRejectedEqualOrLowerPriority, LossManual},
		{ConsumerManualRecording, ConsumerLiveTV, DecisionReject, ReasonPolicyRejectedEqualOrLowerPriority, LossManual},
		{ConsumerManualRecording, ConsumerRetroDVR, DecisionReject, ReasonPolicyRejectedEqualOrLowerPriority, LossManual},
		{ConsumerManualRecording, ConsumerChannelScan, DecisionReject, ReasonPolicyRejectedEqualOrLowerPriority, LossManual},
		{ConsumerManualRecording, ConsumerBackgroundTransfer, DecisionReject, ReasonPolicyRejectedEqualOrLowerPriority, LossManual},

		// 3. Existing: LIVE_TV
		{ConsumerLiveTV, ConsumerScheduledRecording, DecisionPreemptionRequired, ReasonPolicyPreemptionRequired, LossLiveTV},
		{ConsumerLiveTV, ConsumerManualRecording, DecisionPreemptionRequired, ReasonPolicyPreemptionRequired, LossLiveTV},
		{ConsumerLiveTV, ConsumerLiveTV, DecisionReject, ReasonPolicyRejectedEqualOrLowerPriority, LossLiveTV},
		{ConsumerLiveTV, ConsumerRetroDVR, DecisionReject, ReasonPolicyRejectedEqualOrLowerPriority, LossLiveTV},
		{ConsumerLiveTV, ConsumerChannelScan, DecisionReject, ReasonPolicyRejectedEqualOrLowerPriority, LossLiveTV},
		{ConsumerLiveTV, ConsumerBackgroundTransfer, DecisionReject, ReasonPolicyRejectedEqualOrLowerPriority, LossLiveTV},

		// 4. Existing: RETRO_DVR
		{ConsumerRetroDVR, ConsumerScheduledRecording, DecisionPreemptionRequired, ReasonPolicyPreemptionRequired, LossRetroDVR},
		{ConsumerRetroDVR, ConsumerManualRecording, DecisionPreemptionRequired, ReasonPolicyPreemptionRequired, LossRetroDVR},
		{ConsumerRetroDVR, ConsumerLiveTV, DecisionPreemptionRequired, ReasonPolicyPreemptionRequired, LossRetroDVR},
		{ConsumerRetroDVR, ConsumerRetroDVR, DecisionReject, ReasonPolicyRejectedEqualOrLowerPriority, LossRetroDVR},
		{ConsumerRetroDVR, ConsumerChannelScan, DecisionReject, ReasonPolicyRejectedEqualOrLowerPriority, LossRetroDVR},
		{ConsumerRetroDVR, ConsumerBackgroundTransfer, DecisionReject, ReasonPolicyRejectedEqualOrLowerPriority, LossRetroDVR},

		// 5. Existing: CHANNEL_SCAN
		{ConsumerChannelScan, ConsumerScheduledRecording, DecisionPreemptionRequired, ReasonPolicyPreemptionRequired, LossScan},
		{ConsumerChannelScan, ConsumerManualRecording, DecisionPreemptionRequired, ReasonPolicyPreemptionRequired, LossScan},
		{ConsumerChannelScan, ConsumerLiveTV, DecisionPreemptionRequired, ReasonPolicyPreemptionRequired, LossScan},
		{ConsumerChannelScan, ConsumerRetroDVR, DecisionPreemptionRequired, ReasonPolicyPreemptionRequired, LossScan},
		{ConsumerChannelScan, ConsumerChannelScan, DecisionReject, ReasonPolicyRejectedEqualOrLowerPriority, LossScan},
		{ConsumerChannelScan, ConsumerBackgroundTransfer, DecisionReject, ReasonPolicyRejectedEqualOrLowerPriority, LossScan},

		// 6. Existing: BACKGROUND_TRANSFER
		{ConsumerBackgroundTransfer, ConsumerScheduledRecording, DecisionPreemptionRequired, ReasonPolicyPreemptionRequired, LossBackground},
		{ConsumerBackgroundTransfer, ConsumerManualRecording, DecisionPreemptionRequired, ReasonPolicyPreemptionRequired, LossBackground},
		{ConsumerBackgroundTransfer, ConsumerLiveTV, DecisionPreemptionRequired, ReasonPolicyPreemptionRequired, LossBackground},
		{ConsumerBackgroundTransfer, ConsumerRetroDVR, DecisionPreemptionRequired, ReasonPolicyPreemptionRequired, LossBackground},
		{ConsumerBackgroundTransfer, ConsumerChannelScan, DecisionPreemptionRequired, ReasonPolicyPreemptionRequired, LossBackground},
		{ConsumerBackgroundTransfer, ConsumerBackgroundTransfer, DecisionReject, ReasonPolicyRejectedEqualOrLowerPriority, LossBackground},
	}

	if len(tests) != 36 {
		t.Fatalf("expected exactly 36 test rules, got %d", len(tests))
	}

	for _, tt := range tests {
		t.Run(string(tt.existing)+"_vs_"+string(tt.incoming), func(t *testing.T) {
			rule, ok := ConflictRuleFor(tt.existing, tt.incoming)
			if !ok {
				t.Fatalf("missing matrix rule for (%s, %s)", tt.existing, tt.incoming)
			}
			if rule.Decision != tt.wantDecision {
				t.Errorf("expected Decision %s, got %s", tt.wantDecision, rule.Decision)
			}
			if rule.ReasonCode != tt.wantReasonCode {
				t.Errorf("expected ReasonCode %s, got %s", tt.wantReasonCode, rule.ReasonCode)
			}
			if rule.LossClass != tt.wantLossClass {
				t.Errorf("expected LossClass %d, got %d", tt.wantLossClass, rule.LossClass)
			}
		})
	}
}

// 2. Engine Integration & Evaluation Suite: Tests Engine across candidate and active snapshot states.
func TestPolicyEngine_Evaluate(t *testing.T) {
	engine := NewPolicyEngine()
	now := time.Date(2026, 8, 2, 21, 0, 0, 0, time.UTC)

	t.Run("AvailableCandidate_GrantedWithScope", func(t *testing.T) {
		req := EvaluationRequest{
			Consumer:     ConsumerLiveTV,
			ResourceKind: ResourceTuner,
			Owner:        "user-1",
			EvaluatedAt:  now,
		}
		snap := ResourceSnapshot{
			Kind:     ResourceTuner,
			Capacity: 2,
			Candidates: []ResourceCandidate{
				{Scope: "tuner:0", Compatible: true, Available: true},
				{Scope: "tuner:1", Compatible: true, Available: false},
			},
			Active: []ResourceAllocation{
				{AllocationID: "alloc-1", Consumer: ConsumerLiveTV, Owner: "user-2", Scope: "tuner:1", AcquiredAt: now.Add(-5 * time.Minute)},
			},
		}

		res, err := engine.Evaluate(req, snap)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Decision != DecisionGrant {
			t.Errorf("expected DecisionGrant, got %s", res.Decision)
		}
		if res.SelectedScope != "tuner:0" {
			t.Errorf("expected SelectedScope tuner:0, got %s", res.SelectedScope)
		}
	})

	t.Run("FreeCapacityNoAvailableCandidate_RejectedNoCompatibleCandidate", func(t *testing.T) {
		req := EvaluationRequest{
			Consumer:     ConsumerLiveTV,
			ResourceKind: ResourceTuner,
			Owner:        "user-1",
			EvaluatedAt:  now,
		}
		snap := ResourceSnapshot{
			Kind:     ResourceTuner,
			Capacity: 2,
			Candidates: []ResourceCandidate{
				{Scope: "tuner:0", Compatible: false, Available: true}, // Incompatible tuner
				{Scope: "tuner:1", Compatible: true, Available: false},
			},
			Active: []ResourceAllocation{
				{AllocationID: "alloc-1", Consumer: ConsumerLiveTV, Owner: "user-2", Scope: "tuner:1", AcquiredAt: now.Add(-5 * time.Minute)},
			},
		}

		res, err := engine.Evaluate(req, snap)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Decision != DecisionReject {
			t.Errorf("expected DecisionReject when no candidate available, got %s", res.Decision)
		}
		if res.ReasonCode != ReasonPolicyRejectedNoCompatibleCandidate {
			t.Errorf("expected ReasonCode %s, got %s", ReasonPolicyRejectedNoCompatibleCandidate, res.ReasonCode)
		}
	})

	t.Run("PreemptionRequired_ReturnsTargetAndBlockingAllocations", func(t *testing.T) {
		req := EvaluationRequest{
			Consumer:     ConsumerScheduledRecording,
			ResourceKind: ResourceTuner,
			Owner:        "timer-job-1",
			EvaluatedAt:  now,
		}
		snap := ResourceSnapshot{
			Kind:     ResourceTuner,
			Capacity: 2,
			Candidates: []ResourceCandidate{
				{Scope: "tuner:0", Compatible: true, Available: false},
				{Scope: "tuner:1", Compatible: true, Available: false},
			},
			Active: []ResourceAllocation{
				{AllocationID: "alloc-sacrosanct", Consumer: ConsumerScheduledRecording, Owner: "timer-job-0", Scope: "tuner:0", AcquiredAt: now.Add(-20 * time.Minute), IsSacrosanct: true},
				{AllocationID: "alloc-preemptible", Consumer: ConsumerChannelScan, Owner: "scan-service", Scope: "tuner:1", AcquiredAt: now.Add(-5 * time.Minute)},
			},
		}

		res, err := engine.Evaluate(req, snap)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Decision != DecisionPreemptionRequired {
			t.Fatalf("expected DecisionPreemptionRequired, got %s", res.Decision)
		}
		if res.TargetAllocationID != "alloc-preemptible" {
			t.Errorf("expected TargetAllocationID alloc-preemptible, got %s", res.TargetAllocationID)
		}
		if res.SelectedScope != "tuner:1" {
			t.Errorf("expected SelectedScope tuner:1, got %s", res.SelectedScope)
		}
		if len(res.BlockingAllocationIDs) != 1 || res.BlockingAllocationIDs[0] != "alloc-sacrosanct" {
			t.Errorf("expected BlockingAllocationIDs [alloc-sacrosanct], got %v", res.BlockingAllocationIDs)
		}
	})
}

// 3. Pure Function Determinism Test: Identical inputs MUST produce 100% identical byte-for-byte output.
func TestPolicyEngine_PureFunctionDeterminism(t *testing.T) {
	engine := NewPolicyEngine()
	now := time.Date(2026, 8, 2, 21, 30, 0, 0, time.UTC)

	req := EvaluationRequest{
		Consumer:     ConsumerLiveTV,
		ResourceKind: ResourceTuner,
		Owner:        "session-user-1",
		EvaluatedAt:  now,
	}

	snapshot := ResourceSnapshot{
		Kind:     ResourceTuner,
		Capacity: 1,
		Candidates: []ResourceCandidate{
			{Scope: "tuner:0", Compatible: true, Available: false},
		},
		Active: []ResourceAllocation{
			{
				AllocationID: "alloc-channel-scan",
				Consumer:     ConsumerChannelScan,
				Owner:        "scan-service",
				Scope:        "tuner:0",
				AcquiredAt:   now.Add(-5 * time.Minute),
			},
		},
	}

	res1, err1 := engine.Evaluate(req, snapshot)
	res2, err2 := engine.Evaluate(req, snapshot)

	if err1 != nil || err2 != nil {
		t.Fatalf("unexpected error: %v / %v", err1, err2)
	}

	if !reflect.DeepEqual(res1, res2) {
		t.Errorf("pure function violation: res1 != res2 (%+v vs %+v)", res1, res2)
	}
}

// 4. Resource Requirement Separation Test
func TestPolicyEngine_ResourceRequirements(t *testing.T) {
	engine := NewPolicyEngine()
	now := time.Date(2026, 8, 2, 21, 0, 0, 0, time.UTC)

	req := EvaluationRequest{
		Consumer:     ConsumerBackgroundTransfer,
		ResourceKind: ResourceTuner,
		Owner:        "transfer-job-1",
		EvaluatedAt:  now,
	}

	snapshot := ResourceSnapshot{
		Kind:     ResourceTuner,
		Capacity: 1,
		Candidates: []ResourceCandidate{
			{Scope: "tuner:0", Compatible: true, Available: true},
		},
		Active: nil,
	}

	res, err := engine.Evaluate(req, snapshot)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.Decision != DecisionReject {
		t.Errorf("expected DecisionReject for BackgroundTransfer on Tuner, got %s", res.Decision)
	}
	if res.ReasonCode != ReasonPolicyRejectedResourceNotRequired {
		t.Errorf("expected ReasonCode %s, got %s", ReasonPolicyRejectedResourceNotRequired, res.ReasonCode)
	}
}

// 5. Input & Snapshot Validation Tests
func TestPolicyEngine_ValidationErrors(t *testing.T) {
	engine := NewPolicyEngine()
	now := time.Date(2026, 8, 2, 21, 0, 0, 0, time.UTC)

	// a. Missing EvaluatedAt
	reqMissingTime := EvaluationRequest{Consumer: ConsumerLiveTV, ResourceKind: ResourceTuner, Owner: "owner-1"}
	_, err := engine.Evaluate(reqMissingTime, ResourceSnapshot{Kind: ResourceTuner, Capacity: 1})
	if !errors.Is(err, ErrEvaluationTimeRequired) {
		t.Errorf("expected ErrEvaluationTimeRequired, got %v", err)
	}

	// b. Invalid incoming consumer
	reqInvalidConsumer := EvaluationRequest{Consumer: ConsumerType("UNKNOWN"), ResourceKind: ResourceTuner, Owner: "owner-1", EvaluatedAt: now}
	_, err = engine.Evaluate(reqInvalidConsumer, ResourceSnapshot{Kind: ResourceTuner, Capacity: 1})
	if !errors.Is(err, ErrInvalidConsumerType) {
		t.Errorf("expected ErrInvalidConsumerType, got %v", err)
	}

	// c. ResourceKind mismatch
	reqKindMismatch := EvaluationRequest{Consumer: ConsumerLiveTV, ResourceKind: ResourceEncoderSlot, Owner: "owner-1", EvaluatedAt: now}
	_, err = engine.Evaluate(reqKindMismatch, ResourceSnapshot{Kind: ResourceTuner, Capacity: 1})
	if !errors.Is(err, ErrResourceKindMismatch) {
		t.Errorf("expected ErrResourceKindMismatch, got %v", err)
	}

	// d. Empty owner
	reqEmptyOwner := EvaluationRequest{Consumer: ConsumerLiveTV, ResourceKind: ResourceTuner, Owner: "", EvaluatedAt: now}
	_, err = engine.Evaluate(reqEmptyOwner, ResourceSnapshot{Kind: ResourceTuner, Capacity: 1})
	if !errors.Is(err, ErrInvalidOwner) {
		t.Errorf("expected ErrInvalidOwner, got %v", err)
	}

	// e. Zero AcquiredAt in Allocation
	reqValid := EvaluationRequest{Consumer: ConsumerLiveTV, ResourceKind: ResourceTuner, Owner: "owner-1", EvaluatedAt: now}
	snapZeroAcquired := ResourceSnapshot{
		Kind:     ResourceTuner,
		Capacity: 1,
		Candidates: []ResourceCandidate{
			{Scope: "tuner:0", Compatible: true, Available: false},
		},
		Active: []ResourceAllocation{
			{AllocationID: "alloc-1", Consumer: ConsumerLiveTV, Owner: "owner-2", Scope: "tuner:0", AcquiredAt: time.Time{}},
		},
	}
	_, err = engine.Evaluate(reqValid, snapZeroAcquired)
	if !errors.Is(err, ErrZeroAcquiredAtTimestamp) {
		t.Errorf("expected ErrZeroAcquiredAtTimestamp, got %v", err)
	}

	// f. Inconsistent Candidate State (Available == true despite active allocation on scope)
	snapInconsistent := ResourceSnapshot{
		Kind:     ResourceTuner,
		Capacity: 1,
		Candidates: []ResourceCandidate{
			{Scope: "tuner:0", Compatible: true, Available: true},
		},
		Active: []ResourceAllocation{
			{AllocationID: "alloc-1", Consumer: ConsumerLiveTV, Owner: "owner-2", Scope: "tuner:0", AcquiredAt: now.Add(-5 * time.Minute)},
		},
	}
	_, err = engine.Evaluate(reqValid, snapInconsistent)
	if !errors.Is(err, ErrInconsistentCandidateState) {
		t.Errorf("expected ErrInconsistentCandidateState, got %v", err)
	}
}

// 6. Deterministic 3-Tier Tie-Breaking Test
func TestPolicyEngine_DeterministicTieBreaking(t *testing.T) {
	engine := NewPolicyEngine()
	now := time.Date(2026, 8, 2, 21, 0, 0, 0, time.UTC)

	req := EvaluationRequest{
		Consumer:     ConsumerScheduledRecording,
		ResourceKind: ResourceTuner,
		Owner:        "timer-job-1",
		EvaluatedAt:  now,
	}

	// Tier 1 Test: Lowest LossClass candidate targeted first
	t.Run("Tier1_LowestLossClass", func(t *testing.T) {
		snap := ResourceSnapshot{
			Kind:     ResourceTuner,
			Capacity: 2,
			Candidates: []ResourceCandidate{
				{Scope: "tuner:0", Compatible: true, Available: false},
				{Scope: "tuner:1", Compatible: true, Available: false},
			},
			Active: []ResourceAllocation{
				{AllocationID: "alloc-livetv", Consumer: ConsumerLiveTV, Owner: "user-1", Scope: "tuner:0", AcquiredAt: now.Add(-10 * time.Minute)},
				{AllocationID: "alloc-scan", Consumer: ConsumerChannelScan, Owner: "scan-service", Scope: "tuner:1", AcquiredAt: now.Add(-5 * time.Minute)},
			},
		}

		res, err := engine.Evaluate(req, snap)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.TargetAllocationID != "alloc-scan" {
			t.Errorf("expected lowest LossClass 'alloc-scan' to be targeted, got %s", res.TargetAllocationID)
		}
	})

	// Tier 2 Test: Equal LossClass -> Oldest AcquiredAt targeted first
	t.Run("Tier2_OldestAcquiredAt", func(t *testing.T) {
		snap := ResourceSnapshot{
			Kind:     ResourceTuner,
			Capacity: 2,
			Candidates: []ResourceCandidate{
				{Scope: "tuner:0", Compatible: true, Available: false},
				{Scope: "tuner:1", Compatible: true, Available: false},
			},
			Active: []ResourceAllocation{
				{AllocationID: "alloc-scan-newer", Consumer: ConsumerChannelScan, Owner: "scan-1", Scope: "tuner:0", AcquiredAt: now.Add(-5 * time.Minute)},
				{AllocationID: "alloc-scan-older", Consumer: ConsumerChannelScan, Owner: "scan-2", Scope: "tuner:1", AcquiredAt: now.Add(-15 * time.Minute)},
			},
		}

		res, err := engine.Evaluate(req, snap)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.TargetAllocationID != "alloc-scan-older" {
			t.Errorf("expected oldest AcquiredAt 'alloc-scan-older' to be targeted, got %s", res.TargetAllocationID)
		}
	})

	// Tier 3 Test: Equal LossClass & Equal AcquiredAt -> Lexicographical AllocationID
	t.Run("Tier3_LexicographicalID", func(t *testing.T) {
		sameTime := now.Add(-10 * time.Minute)
		snap := ResourceSnapshot{
			Kind:     ResourceTuner,
			Capacity: 2,
			Candidates: []ResourceCandidate{
				{Scope: "tuner:0", Compatible: true, Available: false},
				{Scope: "tuner:1", Compatible: true, Available: false},
			},
			Active: []ResourceAllocation{
				{AllocationID: "alloc-z", Consumer: ConsumerChannelScan, Owner: "scan-1", Scope: "tuner:0", AcquiredAt: sameTime},
				{AllocationID: "alloc-a", Consumer: ConsumerChannelScan, Owner: "scan-2", Scope: "tuner:1", AcquiredAt: sameTime},
			},
		}

		res, err := engine.Evaluate(req, snap)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.TargetAllocationID != "alloc-a" {
			t.Errorf("expected lexicographically first 'alloc-a' to be targeted, got %s", res.TargetAllocationID)
		}
	})
}
