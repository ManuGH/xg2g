package policy

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

// 1. Completeness Test: Proves exactly 36 rules exist for 6x6 matrix with zero missing entries.
func TestDefaultConflictMatrix_Completeness(t *testing.T) {
	if len(DefaultConflictMatrix) != 6 {
		t.Fatalf("expected 6 existing consumer rows in matrix, got %d", len(DefaultConflictMatrix))
	}

	totalEntries := 0
	for _, existing := range AllConsumerTypes {
		row, exists := DefaultConflictMatrix[existing]
		if !exists {
			t.Fatalf("missing row for existing consumer %s", existing)
		}
		if len(row) != 6 {
			t.Fatalf("expected 6 incoming columns for existing consumer %s, got %d", existing, len(row))
		}
		for _, incoming := range AllConsumerTypes {
			rule, ruleExists := row[incoming]
			if !ruleExists {
				t.Fatalf("missing rule for (%s, %s)", existing, incoming)
			}
			if rule.ReasonCode == "" {
				t.Fatalf("empty reason code for rule (%s, %s)", existing, incoming)
			}
			totalEntries++
		}
	}

	if totalEntries != 36 {
		t.Fatalf("expected exactly 36 matrix entries, got %d", totalEntries)
	}
}

// 2. Independent Exhaustive Table Test: Hardcoded expected values (NO code logic computing expectations).
func TestPolicyEngine_ExhaustiveConflictMatrix(t *testing.T) {
	engine := NewPolicyEngine()
	now := time.Date(2026, 8, 2, 21, 0, 0, 0, time.UTC)

	tests := []struct {
		existing       ConsumerType
		incoming       ConsumerType
		wantDecision   PreemptionDecision
		wantReasonCode ReasonCode
	}{
		// 1. Existing: SCHEDULED_RECORDING (Sacrosanct - Unconditionally Protects Recording)
		{ConsumerScheduledRecording, ConsumerScheduledRecording, DecisionReject, ReasonPolicyRejectedProtectedActivity},
		{ConsumerScheduledRecording, ConsumerManualRecording, DecisionReject, ReasonPolicyRejectedProtectedActivity},
		{ConsumerScheduledRecording, ConsumerLiveTV, DecisionReject, ReasonPolicyRejectedProtectedActivity},
		{ConsumerScheduledRecording, ConsumerRetroDVR, DecisionReject, ReasonPolicyRejectedProtectedActivity},
		{ConsumerScheduledRecording, ConsumerChannelScan, DecisionReject, ReasonPolicyRejectedProtectedActivity},
		{ConsumerScheduledRecording, ConsumerBackgroundTransfer, DecisionReject, ReasonPolicyRejectedResourceNotRequired},

		// 2. Existing: MANUAL_RECORDING
		{ConsumerManualRecording, ConsumerScheduledRecording, DecisionPreemptionRequired, ReasonPolicyPreemptionRequired},
		{ConsumerManualRecording, ConsumerManualRecording, DecisionReject, ReasonPolicyRejectedEqualOrLowerPriority},
		{ConsumerManualRecording, ConsumerLiveTV, DecisionReject, ReasonPolicyRejectedEqualOrLowerPriority},
		{ConsumerManualRecording, ConsumerRetroDVR, DecisionReject, ReasonPolicyRejectedEqualOrLowerPriority},
		{ConsumerManualRecording, ConsumerChannelScan, DecisionReject, ReasonPolicyRejectedEqualOrLowerPriority},
		{ConsumerManualRecording, ConsumerBackgroundTransfer, DecisionReject, ReasonPolicyRejectedResourceNotRequired},

		// 3. Existing: LIVE_TV
		{ConsumerLiveTV, ConsumerScheduledRecording, DecisionPreemptionRequired, ReasonPolicyPreemptionRequired},
		{ConsumerLiveTV, ConsumerManualRecording, DecisionPreemptionRequired, ReasonPolicyPreemptionRequired},
		{ConsumerLiveTV, ConsumerLiveTV, DecisionReject, ReasonPolicyRejectedEqualOrLowerPriority},
		{ConsumerLiveTV, ConsumerRetroDVR, DecisionReject, ReasonPolicyRejectedEqualOrLowerPriority},
		{ConsumerLiveTV, ConsumerChannelScan, DecisionReject, ReasonPolicyRejectedEqualOrLowerPriority},
		{ConsumerLiveTV, ConsumerBackgroundTransfer, DecisionReject, ReasonPolicyRejectedResourceNotRequired},

		// 4. Existing: RETRO_DVR
		{ConsumerRetroDVR, ConsumerScheduledRecording, DecisionPreemptionRequired, ReasonPolicyPreemptionRequired},
		{ConsumerRetroDVR, ConsumerManualRecording, DecisionPreemptionRequired, ReasonPolicyPreemptionRequired},
		{ConsumerRetroDVR, ConsumerLiveTV, DecisionPreemptionRequired, ReasonPolicyPreemptionRequired},
		{ConsumerRetroDVR, ConsumerRetroDVR, DecisionReject, ReasonPolicyRejectedEqualOrLowerPriority},
		{ConsumerRetroDVR, ConsumerChannelScan, DecisionReject, ReasonPolicyRejectedEqualOrLowerPriority},
		{ConsumerRetroDVR, ConsumerBackgroundTransfer, DecisionReject, ReasonPolicyRejectedResourceNotRequired},

		// 5. Existing: CHANNEL_SCAN
		{ConsumerChannelScan, ConsumerScheduledRecording, DecisionPreemptionRequired, ReasonPolicyPreemptionRequired},
		{ConsumerChannelScan, ConsumerManualRecording, DecisionPreemptionRequired, ReasonPolicyPreemptionRequired},
		{ConsumerChannelScan, ConsumerLiveTV, DecisionPreemptionRequired, ReasonPolicyPreemptionRequired},
		{ConsumerChannelScan, ConsumerRetroDVR, DecisionPreemptionRequired, ReasonPolicyPreemptionRequired},
		{ConsumerChannelScan, ConsumerChannelScan, DecisionReject, ReasonPolicyRejectedEqualOrLowerPriority},
		{ConsumerChannelScan, ConsumerBackgroundTransfer, DecisionReject, ReasonPolicyRejectedResourceNotRequired},

		// 6. Existing: BACKGROUND_TRANSFER (Wait: BackgroundTransfer is not required on Demuxer)
		{ConsumerBackgroundTransfer, ConsumerScheduledRecording, DecisionPreemptionRequired, ReasonPolicyPreemptionRequired},
		{ConsumerBackgroundTransfer, ConsumerManualRecording, DecisionPreemptionRequired, ReasonPolicyPreemptionRequired},
		{ConsumerBackgroundTransfer, ConsumerLiveTV, DecisionPreemptionRequired, ReasonPolicyPreemptionRequired},
		{ConsumerBackgroundTransfer, ConsumerRetroDVR, DecisionPreemptionRequired, ReasonPolicyPreemptionRequired},
		{ConsumerBackgroundTransfer, ConsumerChannelScan, DecisionPreemptionRequired, ReasonPolicyPreemptionRequired},
		{ConsumerBackgroundTransfer, ConsumerBackgroundTransfer, DecisionReject, ReasonPolicyRejectedResourceNotRequired},
	}

	for _, tt := range tests {
		t.Run(string(tt.existing)+"_vs_"+string(tt.incoming), func(t *testing.T) {
			req := EvaluationRequest{
				Consumer:     tt.incoming,
				ResourceKind: ResourceDemuxer, // Demuxer allows all consumers including BackgroundTransfer for matrix test
				Owner:        "incoming-owner",
				EvaluatedAt:  now,
			}

			snapshot := ResourceSnapshot{
				Kind:     ResourceDemuxer,
				Capacity: 1, // Forces conflict evaluation
				Active: []ResourceAllocation{
					{
						AllocationID: "alloc-active-1",
						Consumer:     tt.existing,
						Owner:        "existing-owner",
						AcquiredAt:   now.Add(-10 * time.Minute),
					},
				},
			}

			res, err := engine.Evaluate(req, snapshot)
			if err != nil {
				t.Fatalf("unexpected evaluation error: %v", err)
			}

			if res.Decision != tt.wantDecision {
				t.Errorf("expected Decision %s, got %s", tt.wantDecision, res.Decision)
			}
			if res.ReasonCode != tt.wantReasonCode {
				t.Errorf("expected ReasonCode %s, got %s", tt.wantReasonCode, res.ReasonCode)
			}
		})
	}
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
		Active: []ResourceAllocation{
			{
				AllocationID: "alloc-channel-scan",
				Consumer:     ConsumerChannelScan,
				Owner:        "scan-service",
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

// 4. Resource Requirement Separation Test: BackgroundTransfer on Tuner is rejected without preempting.
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
		Active:   nil, // Empty tuner pool
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

	// c. Empty owner
	reqEmptyOwner := EvaluationRequest{Consumer: ConsumerLiveTV, ResourceKind: ResourceTuner, Owner: "", EvaluatedAt: now}
	_, err = engine.Evaluate(reqEmptyOwner, ResourceSnapshot{Kind: ResourceTuner, Capacity: 1})
	if !errors.Is(err, ErrInvalidOwner) {
		t.Errorf("expected ErrInvalidOwner, got %v", err)
	}

	// d. Invalid ResourceKind
	reqInvalidResource := EvaluationRequest{Consumer: ConsumerLiveTV, ResourceKind: ResourceKind("UNKNOWN"), Owner: "owner-1", EvaluatedAt: now}
	_, err = engine.Evaluate(reqInvalidResource, ResourceSnapshot{Kind: ResourceKind("UNKNOWN"), Capacity: 1})
	if !errors.Is(err, ErrInvalidResourceKind) {
		t.Errorf("expected ErrInvalidResourceKind, got %v", err)
	}

	// e. Invalid Capacity
	reqValid := EvaluationRequest{Consumer: ConsumerLiveTV, ResourceKind: ResourceTuner, Owner: "owner-1", EvaluatedAt: now}
	_, err = engine.Evaluate(reqValid, ResourceSnapshot{Kind: ResourceTuner, Capacity: 0})
	if !errors.Is(err, ErrInvalidSnapshotCapacity) {
		t.Errorf("expected ErrInvalidSnapshotCapacity for 0 capacity, got %v", err)
	}

	// f. Duplicate Allocation ID
	snapDupAlloc := ResourceSnapshot{
		Kind:     ResourceTuner,
		Capacity: 2,
		Active: []ResourceAllocation{
			{AllocationID: "alloc-1", Consumer: ConsumerChannelScan},
			{AllocationID: "alloc-1", Consumer: ConsumerRetroDVR},
		},
	}
	_, err = engine.Evaluate(reqValid, snapDupAlloc)
	if !errors.Is(err, ErrInvalidAllocationID) {
		t.Errorf("expected ErrInvalidAllocationID for duplicate allocation ID, got %v", err)
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
			Active: []ResourceAllocation{
				{AllocationID: "alloc-livetv", Consumer: ConsumerLiveTV, AcquiredAt: now.Add(-10 * time.Minute)},
				{AllocationID: "alloc-scan", Consumer: ConsumerChannelScan, AcquiredAt: now.Add(-5 * time.Minute)},
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
			Active: []ResourceAllocation{
				{AllocationID: "alloc-scan-newer", Consumer: ConsumerChannelScan, AcquiredAt: now.Add(-5 * time.Minute)},
				{AllocationID: "alloc-scan-older", Consumer: ConsumerChannelScan, AcquiredAt: now.Add(-15 * time.Minute)},
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
			Active: []ResourceAllocation{
				{AllocationID: "alloc-z", Consumer: ConsumerChannelScan, AcquiredAt: sameTime},
				{AllocationID: "alloc-a", Consumer: ConsumerChannelScan, AcquiredAt: sameTime},
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
