package policy

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// 1. Matrix Completeness & Rule Test: Tests all 36 entries directly against ConflictRuleFor.
func TestConflictMatrix_Rules(t *testing.T) {
	allConsumers := AllConsumerTypes()
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

	if len(allConsumers) != 6 {
		t.Fatalf("expected 6 consumer types in AllConsumerTypes(), got %d", len(allConsumers))
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

// 2. Exact TargetScope Semantics Suite: Proves exact target filtering and zero deflection.
func TestPolicyEngine_ExactTargetScopeSemantics(t *testing.T) {
	engine := NewPolicyEngine()
	now := time.Date(2026, 8, 2, 21, 0, 0, 0, time.UTC)

	// Test 1: TargetScope specified -> MUST NOT deflect to lower loss class on tuner:0
	t.Run("TargetScope_NoDeflectionToLowerLossScope", func(t *testing.T) {
		req := EvaluationRequest{
			Consumer:     ConsumerScheduledRecording,
			ResourceKind: ResourceTuner,
			Owner:        "timer-job-1",
			TargetScope:  "tuner:1", // Explicitly target tuner:1 (occupied by LiveTV)
			ScopeMode:    ScopeSelectionExact,
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
				{AllocationID: "alloc-scan-tuner0", Consumer: ConsumerChannelScan, Owner: "scan", Scope: "tuner:0", AcquiredAt: now.Add(-10 * time.Minute)}, // Lower LossClass (LossScan=20)
				{AllocationID: "alloc-livetv-tuner1", Consumer: ConsumerLiveTV, Owner: "user", Scope: "tuner:1", AcquiredAt: now.Add(-5 * time.Minute)},     // Higher LossClass (LossLiveTV=40)
			},
		}

		res, err := engine.Evaluate(req, snap)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if res.Decision != DecisionPreemptionRequired {
			t.Fatalf("expected DecisionPreemptionRequired, got %s", res.Decision)
		}
		if res.TargetAllocationID != "alloc-livetv-tuner1" {
			t.Errorf("expected preemption target alloc-livetv-tuner1 on exact targetScope tuner:1, got %s", res.TargetAllocationID)
		}
		if res.SelectedScope != "tuner:1" {
			t.Errorf("expected SelectedScope tuner:1, got %s", res.SelectedScope)
		}
	})

	// Test 2: TargetScope protected -> Must REJECT and NOT deflect to preempt tuner:0
	t.Run("TargetScope_ProtectedTargetRejectsWithoutDeflection", func(t *testing.T) {
		req := EvaluationRequest{
			Consumer:     ConsumerLiveTV,
			ResourceKind: ResourceTuner,
			Owner:        "user-2",
			TargetScope:  "tuner:1", // Target tuner:1 (occupied by sacrosanct ScheduledRecording)
			ScopeMode:    ScopeSelectionExact,
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
				{AllocationID: "alloc-scan-tuner0", Consumer: ConsumerChannelScan, Owner: "scan", Scope: "tuner:0", AcquiredAt: now.Add(-10 * time.Minute)},                                   // Preemptible scan on tuner:0
				{AllocationID: "alloc-scheduled-tuner1", Consumer: ConsumerScheduledRecording, Owner: "timer-1", Scope: "tuner:1", AcquiredAt: now.Add(-5 * time.Minute), IsSacrosanct: true}, // Protected on tuner:1
			},
		}

		res, err := engine.Evaluate(req, snap)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if res.Decision != DecisionReject {
			t.Fatalf("expected DecisionReject for protected targetScope tuner:1, got %s", res.Decision)
		}
		if res.ReasonCode != ReasonPolicyRejectedProtectedActivity {
			t.Errorf("expected ReasonCode %s, got %s", ReasonPolicyRejectedProtectedActivity, res.ReasonCode)
		}
		if res.TargetAllocationID != "" {
			t.Errorf("expected empty TargetAllocationID on reject, got %s", res.TargetAllocationID)
		}
		if len(res.BlockingAllocationIDs) != 1 || res.BlockingAllocationIDs[0] != "alloc-scheduled-tuner1" {
			t.Errorf("expected BlockingAllocationIDs [alloc-scheduled-tuner1], got %v", res.BlockingAllocationIDs)
		}
	})
}

// 3. Releasing Allocation Snapshot Semantics Suite
func TestPolicyEngine_ReleasingAllocationSemantics(t *testing.T) {
	engine := NewPolicyEngine()
	now := time.Date(2026, 8, 2, 21, 0, 0, 0, time.UTC)

	req := EvaluationRequest{
		Consumer:     ConsumerLiveTV,
		ResourceKind: ResourceTuner,
		Owner:        "user-1",
		EvaluatedAt:  now,
	}

	t.Run("ReleasingAllocationWithAvailableCandidate_InvalidSnapshot", func(t *testing.T) {
		snapInconsistent := ResourceSnapshot{
			Kind:     ResourceTuner,
			Capacity: 1,
			Candidates: []ResourceCandidate{
				{Scope: "tuner:0", Compatible: true, Available: true}, // Inconsistent! Allocation exists on scope
			},
			Active: []ResourceAllocation{
				{AllocationID: "alloc-1", Consumer: ConsumerLiveTV, Owner: "user-2", Scope: "tuner:0", AcquiredAt: now.Add(-5 * time.Minute), IsReleasing: true},
			},
		}

		_, err := engine.Evaluate(req, snapInconsistent)
		if !errors.Is(err, ErrInconsistentCandidateState) {
			t.Fatalf("expected ErrInconsistentCandidateState, got %v", err)
		}
	})

	t.Run("ReleasingAllocationWithUnavailableCandidate_ProtectedFromPreemption", func(t *testing.T) {
		snapValidReleasing := ResourceSnapshot{
			Kind:     ResourceTuner,
			Capacity: 1,
			Candidates: []ResourceCandidate{
				{Scope: "tuner:0", Compatible: true, Available: false},
			},
			Active: []ResourceAllocation{
				{AllocationID: "alloc-1", Consumer: ConsumerChannelScan, Owner: "scan", Scope: "tuner:0", AcquiredAt: now.Add(-5 * time.Minute), IsReleasing: true},
			},
		}

		res, err := engine.Evaluate(req, snapValidReleasing)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Decision != DecisionReject {
			t.Errorf("expected DecisionReject when sole allocation is releasing, got %s", res.Decision)
		}
	})
}

// 4. ResourceStorageIO Unsupported Test
func TestPolicyEngine_StorageIO_Unsupported(t *testing.T) {
	engine := NewPolicyEngine()
	now := time.Date(2026, 8, 2, 21, 0, 0, 0, time.UTC)

	req := EvaluationRequest{
		Consumer:     ConsumerScheduledRecording,
		ResourceKind: ResourceStorageIO,
		Owner:        "timer-job-1",
		EvaluatedAt:  now,
	}
	snap := ResourceSnapshot{
		Kind:     ResourceStorageIO,
		Capacity: 100,
	}

	_, err := engine.Evaluate(req, snap)
	if !errors.Is(err, ErrUnsupportedResourceModel) {
		t.Fatalf("expected ErrUnsupportedResourceModel for ResourceStorageIO, got %v", err)
	}
}

// 5. Systemwide Reason Code Governance Test
func TestPolicyReasonCodes_GovernanceRegistry(t *testing.T) {
	policyCodes := []ReasonCode{
		ReasonPolicyGrantedResourceAvailable,
		ReasonPolicyRejectedProtectedActivity,
		ReasonPolicyRejectedEqualOrLowerPriority,
		ReasonPolicyRejectedResourceNotRequired,
		ReasonPolicyRejectedNoCompatibleCandidate,
		ReasonPolicyPreemptionRequired,
		ReasonPolicyInvalidInput,
	}

	// Find docs directory by walking up from test directory
	currentDir, _ := os.Getwd()
	var rootDir string
	for dir := currentDir; dir != "/" && dir != "."; dir = filepath.Dir(dir) {
		candidate := filepath.Join(dir, "docs", "guides")
		if _, err := os.Stat(candidate); err == nil {
			rootDir = candidate
			break
		}
	}
	if rootDir == "" {
		t.Fatalf("could not find docs/guides directory starting from %s", currentDir)
	}

	reasonCodesPath := filepath.Join(rootDir, "REASON_CODES.md")
	policyReasonCodesPath := filepath.Join(rootDir, "POLICY_REASON_CODES.md")

	contentMaster, err := os.ReadFile(reasonCodesPath)
	if err != nil {
		t.Fatalf("failed to read master reason codes doc: %v", err)
	}

	contentPolicy, err := os.ReadFile(policyReasonCodesPath)
	if err != nil {
		t.Fatalf("failed to read policy reason codes doc: %v", err)
	}

	masterStr := string(contentMaster)
	policyStr := string(contentPolicy)

	for _, code := range policyCodes {
		if !strings.Contains(masterStr, string(code)) {
			t.Errorf("reason code %s missing from docs/guides/REASON_CODES.md", code)
		}
		if !strings.Contains(policyStr, string(code)) {
			t.Errorf("reason code %s missing from docs/guides/POLICY_REASON_CODES.md", code)
		}
	}
}

// 6. Pure Function Determinism Test: Identical inputs MUST produce 100% identical byte-for-byte output.
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
