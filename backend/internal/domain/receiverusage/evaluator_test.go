// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package receiverusage

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/receivertopology"
)

var fixedTime = time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)

// Test 1: UNKNOWN access class counted as RESTRICTED in conservative profile.
func TestReceiverUsage_UNKNOWN_CountedAsRestricted_Conservative(t *testing.T) {
	evaluator := NewEvaluator()
	policy := ConservativeSingleUsePolicy()

	req := UsageRequest{
		ReceiverID: "rec-1",
		Owner:      "owner-1",
		Intent:     IntentLive,
		Source:     SourceIdentity{ReceiverID: "rec-1", ServiceReference: "1:0:19:283D:3FB:1:C00000:0:0:0:"},
		Access: AccessClassification{
			Class:      AccessCapacityUnknown,
			Confidence: ConfidenceUnknown,
		},
		RequestedAt: fixedTime,
	}

	snapshot := SystemSnapshot{}

	decision, err := evaluator.Evaluate(context.Background(), policy, req, snapshot)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Kind != DecisionAllow {
		t.Fatalf("expected DecisionAllow, got %v", decision.Kind)
	}

	hasRestrictedReq := false
	for _, r := range decision.Requirements {
		if r.Kind == ReqRestrictedAccessSlot {
			hasRestrictedReq = true
		}
	}
	if !hasRestrictedReq {
		t.Fatalf("expected UNKNOWN in conservative profile to require RESTRICTED_ACCESS_SLOT")
	}
}

// Test 2: NONE (FTA) requires 0 restricted access slots.
func TestReceiverUsage_FTA_RequiresZeroRestrictedSlots(t *testing.T) {
	evaluator := NewEvaluator()
	policy := ConservativeSingleUsePolicy()

	req := UsageRequest{
		ReceiverID: "rec-1",
		Owner:      "owner-1",
		Intent:     IntentLive,
		Source:     SourceIdentity{ReceiverID: "rec-1", ServiceReference: "1:0:19:283D:3FB:1:C00000:0:0:0:"},
		Access: AccessClassification{
			Class:      AccessCapacityNone,
			Confidence: ConfidenceVerified,
		},
		RequestedAt: fixedTime,
	}

	decision, err := evaluator.Evaluate(context.Background(), policy, req, SystemSnapshot{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Kind != DecisionAllow {
		t.Fatalf("expected DecisionAllow, got %v", decision.Kind)
	}

	for _, r := range decision.Requirements {
		if r.Kind == ReqRestrictedAccessSlot {
			t.Fatalf("FTA service must NOT require RESTRICTED_ACCESS_SLOT")
		}
	}
}

// Test 3: Second restricted session rejected when limit is 1.
func TestReceiverUsage_SecondRestrictedSession_RejectedAtLimitOne(t *testing.T) {
	evaluator := NewEvaluator()
	policy := ConservativeSingleUsePolicy()
	policy.MaxLiveSessions = 2 // Allow 2 live sessions, but only 1 restricted access slot

	snapshot := SystemSnapshot{
		ActiveSessions: []ActiveSessionSnapshot{
			{
				SessionID:        "sess-1",
				ReceiverID:       "rec-1",
				Owner:            "owner-1",
				Intent:           IntentLive,
				ServiceReference: "1:0:19:283D:3FB:1:C00000:0:0:0:",
				IsRestricted:     true,
				StartedAt:        fixedTime.Add(-1 * time.Minute),
			},
		},
	}

	req := UsageRequest{
		ReceiverID: "rec-1",
		Owner:      "owner-2",
		Intent:     IntentLive,
		Source:     SourceIdentity{ReceiverID: "rec-1", ServiceReference: "1:0:19:283E:3FB:1:C00000:0:0:0:"},
		Access: AccessClassification{
			Class:      AccessCapacityRestricted,
			Confidence: ConfidenceVerified,
		},
		RequestedAt: fixedTime,
	}

	decision, err := evaluator.Evaluate(context.Background(), policy, req, snapshot)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Kind != DecisionReject {
		t.Fatalf("expected DecisionReject, got %v", decision.Kind)
	}
	if decision.Reason != model.RReceiverUsageRestrictedAccessLimitExceeded {
		t.Fatalf("expected reason RReceiverUsageRestrictedAccessLimitExceeded, got %v", decision.Reason)
	}
}

// Test 4: Live during recording handled according to policy (AllowLiveWithRecording).
func TestReceiverUsage_LiveWhileRecording_RespectsPolicy(t *testing.T) {
	evaluator := NewEvaluator()
	policy := ConservativeSingleUsePolicy()
	policy.AllowLiveWithRecording = false

	snapshot := SystemSnapshot{
		ActiveSessions: []ActiveSessionSnapshot{
			{
				SessionID:        "rec-sess-1",
				ReceiverID:       "rec-1",
				Owner:            "dvr-job",
				Intent:           IntentRecording,
				ServiceReference: "1:0:19:1111:3FB:1:C00000:0:0:0:",
				IsRestricted:     false,
				StartedAt:        fixedTime.Add(-2 * time.Minute),
			},
		},
	}

	req := UsageRequest{
		ReceiverID: "rec-1",
		Owner:      "user-live",
		Intent:     IntentLive,
		Source:     SourceIdentity{ReceiverID: "rec-1", ServiceReference: "1:0:19:2222:3FB:1:C00000:0:0:0:"},
		Access: AccessClassification{
			Class:      AccessCapacityNone,
			Confidence: ConfidenceVerified,
		},
		RequestedAt: fixedTime,
	}

	decision, err := evaluator.Evaluate(context.Background(), policy, req, snapshot)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Kind != DecisionReject {
		t.Fatalf("expected DecisionReject, got %v", decision.Kind)
	}
	if decision.Reason != model.RReceiverUsageLiveWithRecordingForbidden {
		t.Fatalf("expected reason RReceiverUsageLiveWithRecordingForbidden, got %v", decision.Reason)
	}

	// Now allow live with recording
	policy.AllowLiveWithRecording = true
	decisionAllowed, err := evaluator.Evaluate(context.Background(), policy, req, snapshot)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decisionAllowed.Kind != DecisionAllow {
		t.Fatalf("expected DecisionAllow when AllowLiveWithRecording is true, got %v", decisionAllowed.Kind)
	}
}

// Test 5: Duplicate request from same owner returns DecisionReuseSession.
func TestReceiverUsage_DuplicateRequest_SameOwner_ReturnsReuseSession(t *testing.T) {
	evaluator := NewEvaluator()
	policy := ConservativeSingleUsePolicy()

	snapshot := SystemSnapshot{
		ActiveSessions: []ActiveSessionSnapshot{
			{
				SessionID:        "sess-active-1",
				ReceiverID:       "rec-1",
				Owner:            "owner-same",
				Intent:           IntentLive,
				ServiceReference: "1:0:19:283D:3FB:1:C00000:0:0:0:",
				IsRestricted:     true,
				StartedAt:        fixedTime.Add(-2 * time.Second),
			},
		},
	}

	req := UsageRequest{
		ReceiverID: "rec-1",
		Owner:      "owner-same",
		Intent:     IntentLive,
		Source:     SourceIdentity{ReceiverID: "rec-1", ServiceReference: "1:0:19:283D:3FB:1:C00000:0:0:0:"},
		Access: AccessClassification{
			Class:      AccessCapacityRestricted,
			Confidence: ConfidenceVerified,
		},
		RequestedAt: fixedTime,
	}

	decision, err := evaluator.Evaluate(context.Background(), policy, req, snapshot)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Kind != DecisionReuseSession {
		t.Fatalf("expected DecisionReuseSession, got %v", decision.Kind)
	}
	if decision.ReuseSessionID != "sess-active-1" {
		t.Fatalf("expected ReuseSessionID sess-active-1, got %s", decision.ReuseSessionID)
	}
}

// Test 6: Same request from different owner is NOT deduplicated.
func TestReceiverUsage_SameChannel_DifferentOwner_NotDeduplicated(t *testing.T) {
	evaluator := NewEvaluator()
	policy := ConservativeSingleUsePolicy()

	snapshot := SystemSnapshot{
		ActiveSessions: []ActiveSessionSnapshot{
			{
				SessionID:        "sess-active-1",
				ReceiverID:       "rec-1",
				Owner:            "owner-A",
				Intent:           IntentLive,
				ServiceReference: "1:0:19:283D:3FB:1:C00000:0:0:0:",
				IsRestricted:     false,
				StartedAt:        fixedTime.Add(-2 * time.Second),
			},
		},
	}

	req := UsageRequest{
		ReceiverID: "rec-1",
		Owner:      "owner-B", // Different owner
		Intent:     IntentLive,
		Source:     SourceIdentity{ReceiverID: "rec-1", ServiceReference: "1:0:19:283D:3FB:1:C00000:0:0:0:"},
		Access: AccessClassification{
			Class:      AccessCapacityNone,
			Confidence: ConfidenceVerified,
		},
		RequestedAt: fixedTime,
	}

	decision, err := evaluator.Evaluate(context.Background(), policy, req, snapshot)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Kind == DecisionReuseSession {
		t.Fatalf("different owner request MUST NOT be deduplicated")
	}
}

// Test 7: Retro-DVR from existing buffer (RetroSourceExistingBuffer) requires 0 new resources.
func TestReceiverUsage_RetroDVR_ExistingBuffer_RequiresZeroResources(t *testing.T) {
	evaluator := NewEvaluator()
	policy := ConservativeSingleUsePolicy()

	req := UsageRequest{
		ReceiverID: "rec-1",
		Owner:      "retro-job",
		Intent:     IntentRetroDVR,
		Source:     SourceIdentity{ReceiverID: "rec-1", ServiceReference: "1:0:19:283D:3FB:1:C00000:0:0:0:"},
		Access: AccessClassification{
			Class:      AccessCapacityRestricted,
			Confidence: ConfidenceVerified,
		},
		RetroSource: RetroSourceExistingBuffer,
		RequestedAt: fixedTime,
	}

	decision, err := evaluator.Evaluate(context.Background(), policy, req, SystemSnapshot{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Kind != DecisionAllow {
		t.Fatalf("expected DecisionAllow, got %v", decision.Kind)
	}
	if len(decision.Requirements) != 0 {
		t.Fatalf("RetroDVR from existing buffer must require 0 new resources, got %d", len(decision.Requirements))
	}
}

// Test 8: Retro-DVR requiring receiver fetch (RetroSourceReceiverFetch) is rejected in conservative profile.
func TestReceiverUsage_RetroDVR_ReceiverFetch_RejectedConservative(t *testing.T) {
	evaluator := NewEvaluator()
	policy := ConservativeSingleUsePolicy()
	policy.AllowRetroDVRRestricted = false

	req := UsageRequest{
		ReceiverID: "rec-1",
		Owner:      "retro-job",
		Intent:     IntentRetroDVR,
		Source:     SourceIdentity{ReceiverID: "rec-1", ServiceReference: "1:0:19:283D:3FB:1:C00000:0:0:0:"},
		Access: AccessClassification{
			Class:      AccessCapacityRestricted,
			Confidence: ConfidenceVerified,
		},
		RetroSource: RetroSourceReceiverFetch,
		RequestedAt: fixedTime,
	}

	decision, err := evaluator.Evaluate(context.Background(), policy, req, SystemSnapshot{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Kind != DecisionReject {
		t.Fatalf("expected DecisionReject for RetroDVR with receiver fetch, got %v", decision.Kind)
	}
	if decision.Reason != model.RReceiverUsageIntentNotAllowed {
		t.Fatalf("expected reason RReceiverUsageIntentNotAllowed, got %v", decision.Reason)
	}
}

// Test 9: Policy changes affect only new evaluation calls, active session records are untouched.
func TestReceiverUsage_PolicyMutation_DoesNotAffectActiveSessions(t *testing.T) {
	evaluator := NewEvaluator()
	policy := ConservativeSingleUsePolicy()

	snapshot := SystemSnapshot{
		ActiveSessions: []ActiveSessionSnapshot{
			{
				SessionID:        "active-1",
				ReceiverID:       "rec-1",
				Owner:            "owner-1",
				Intent:           IntentLive,
				ServiceReference: "1:0:19:283D:3FB:1:C00000:0:0:0:",
				IsRestricted:     true,
				StartedAt:        fixedTime.Add(-10 * time.Minute),
			},
		},
	}

	// Mutate policy to zero live sessions
	strictPolicy := policy
	strictPolicy.MaxLiveSessions = 0

	req := UsageRequest{
		ReceiverID: "rec-1",
		Owner:      "owner-2",
		Intent:     IntentLive,
		Source:     SourceIdentity{ReceiverID: "rec-1", ServiceReference: "1:0:19:283E:3FB:1:C00000:0:0:0:"},
		Access: AccessClassification{
			Class:      AccessCapacityNone,
			Confidence: ConfidenceVerified,
		},
		RequestedAt: fixedTime,
	}

	decision, err := evaluator.Evaluate(context.Background(), strictPolicy, req, snapshot)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Kind != DecisionReject {
		t.Fatalf("expected DecisionReject under strict policy, got %v", decision.Kind)
	}

	// Verify active session in snapshot remains untouched
	if len(snapshot.ActiveSessions) != 1 || snapshot.ActiveSessions[0].SessionID != "active-1" {
		t.Fatalf("active session snapshot mutated unexpectedly")
	}
}

// Test 10: Negative and unknown config values rejected during validation; valid zero values (MaxRecordingSessions: 0) remain allowed.
func TestReceiverUsage_ConfigValidation_NegativeRejected_ValidZeroAllowed(t *testing.T) {
	// Valid zero values (Recordings disabled)
	validZeroPolicy := ReceiverUsagePolicy{
		MaxLiveSessions:             1,
		MaxRecordingSessions:        0, // Valid 0
		MaxRestrictedAccessSessions: 0, // Valid 0
		UnknownAccessHandling:       UnknownAccessCountAsRestricted,
	}
	if err := validZeroPolicy.Validate(); err != nil {
		t.Fatalf("expected valid zero policy to pass validation, got err: %v", err)
	}

	// Negative value
	invalidNegativePolicy := validZeroPolicy
	invalidNegativePolicy.MaxLiveSessions = -1
	if err := invalidNegativePolicy.Validate(); err == nil || !errors.Is(err, ErrInvalidPolicyConfiguration) {
		t.Fatalf("expected ErrInvalidPolicyConfiguration for negative MaxLiveSessions, got %v", err)
	}

	// Unknown enum
	invalidEnumPolicy := validZeroPolicy
	invalidEnumPolicy.UnknownAccessHandling = UnknownAccessHandling("INVALID_ENUM")
	if err := invalidEnumPolicy.Validate(); err == nil || !errors.Is(err, ErrInvalidPolicyConfiguration) {
		t.Fatalf("expected ErrInvalidPolicyConfiguration for unknown enum, got %v", err)
	}
}

// Test 11: Deterministic evaluation output for identical request + snapshot.
func TestReceiverUsage_DeterministicEvaluation(t *testing.T) {
	evaluator := NewEvaluator()
	policy := ConservativeSingleUsePolicy()

	req := UsageRequest{
		ReceiverID: "rec-1",
		Owner:      "owner-det",
		Intent:     IntentLive,
		Source:     SourceIdentity{ReceiverID: "rec-1", ServiceReference: "1:0:19:283D:3FB:1:C00000:0:0:0:"},
		Access: AccessClassification{
			Class:      AccessCapacityRestricted,
			Confidence: ConfidenceVerified,
		},
		RequestedAt: fixedTime,
	}

	snapshot := SystemSnapshot{}

	decision1, _ := evaluator.Evaluate(context.Background(), policy, req, snapshot)
	decision2, _ := evaluator.Evaluate(context.Background(), policy, req, snapshot)

	if decision1.Kind != decision2.Kind || decision1.Reason != decision2.Reason || len(decision1.Requirements) != len(decision2.Requirements) {
		t.Fatalf("evaluator output is not deterministic between identical calls")
	}
}

// Test 12: Evaluator performs ZERO side effects / state mutations on snapshot or input records.
func TestReceiverUsage_ZeroSideEffects_PureEvaluation(t *testing.T) {
	evaluator := NewEvaluator()
	policy := ConservativeSingleUsePolicy()

	snapshot := SystemSnapshot{
		ActiveSessions: []ActiveSessionSnapshot{
			{
				SessionID:        "sess-pure-1",
				ReceiverID:       "rec-1",
				Owner:            "owner-pure",
				Intent:           IntentLive,
				ServiceReference: "1:0:19:283D:3FB:1:C00000:0:0:0:",
				IsRestricted:     true,
				StartedAt:        fixedTime.Add(-5 * time.Minute),
			},
		},
	}

	req := UsageRequest{
		ReceiverID: "rec-1",
		Owner:      "owner-pure-2",
		Intent:     IntentLive,
		Source:     SourceIdentity{ReceiverID: "rec-1", ServiceReference: "1:0:19:9999:3FB:1:C00000:0:0:0:"},
		Access: AccessClassification{
			Class:      AccessCapacityRestricted,
			Confidence: ConfidenceVerified,
		},
		RequestedAt: fixedTime,
	}

	origSnapLen := len(snapshot.ActiveSessions)
	origSessState := snapshot.ActiveSessions[0]

	_, err := evaluator.Evaluate(context.Background(), policy, req, snapshot)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(snapshot.ActiveSessions) != origSnapLen {
		t.Fatalf("evaluator mutated active sessions length")
	}
	if snapshot.ActiveSessions[0] != origSessState {
		t.Fatalf("evaluator mutated active session content")
	}
}

func TestReceiverUsage_TopologyAwareRejection(t *testing.T) {
	topo := receivertopology.ReceiverTopology{
		Model:      "Test FBC Receiver",
		Confidence: receivertopology.ConfidenceVerified,
		Inputs: []receivertopology.PhysicalInput{
			{ID: "input_a", Label: "Tuner A", DeliveryType: receivertopology.DeliveryLegacyUniversal},
		},
		Demodulators: []receivertopology.Demodulator{
			{ID: "demod_a", InputID: "input_a", DVBTypes: []receivertopology.DVBType{receivertopology.DVBTypeSat}},
		},
	}
	topoSvc, err := receivertopology.NewService(topo, receivertopology.EvaluationModeEnforce)
	if err != nil {
		t.Fatalf("failed to create topo service: %v", err)
	}

	// Occupy the only demodulator with stream 1 on High-H
	sRef1 := "1:0:19:283D:3FB:1:C00000:0:0:0:"
	_, _, err = topoSvc.ReserveStreamLeaseAtomic(sRef1, "sess-1", receivertopology.PriorityLive, time.Minute)
	if err != nil {
		t.Fatalf("failed to seed session 1: %v", err)
	}

	evaluator := NewEvaluatorWithTopology(topoSvc)
	policy := ReceiverUsagePolicy{
		Mode:            ReceiverUsageModeEnforce,
		MaxLiveSessions: 10, // High session limit, but hardware only has 1 demod!
	}

	// Stream 2 requests a different transponder -> hardware exhausted!
	req2 := UsageRequest{
		ReceiverID: "rec-1",
		Owner:      "user-2",
		Intent:     IntentLive,
		Source:     SourceIdentity{ReceiverID: "rec-1", ServiceReference: "1:0:19:9999:999:1:C00000:0:0:0:"},
		Access: AccessClassification{
			Class:      AccessCapacityNone,
			Confidence: ConfidenceVerified,
		},
		RequestedAt: fixedTime,
	}

	decision, err := evaluator.Evaluate(context.Background(), policy, req2, SystemSnapshot{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Kind != DecisionReject {
		t.Fatalf("expected DecisionReject due to demod exhaustion, got %v", decision.Kind)
	}
	if decision.Reason != model.RLeaseBusy {
		t.Fatalf("expected Reason RLeaseBusy, got %v", decision.Reason)
	}

	// Stream 3 requests the SAME multiplex as Stream 1 -> Multiplex Reuse -> ALLOW!
	req3 := UsageRequest{
		ReceiverID: "rec-1",
		Owner:      "user-3",
		Intent:     IntentLive,
		Source:     SourceIdentity{ReceiverID: "rec-1", ServiceReference: sRef1},
		Access: AccessClassification{
			Class:      AccessCapacityNone,
			Confidence: ConfidenceVerified,
		},
		RequestedAt: fixedTime,
	}

	decision3, err := evaluator.Evaluate(context.Background(), policy, req3, SystemSnapshot{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision3.Kind != DecisionAllow {
		t.Fatalf("expected DecisionAllow via multiplex reuse, got %v", decision3.Kind)
	}
}

func TestReceiverUsage_Topology_InvalidRef_EnforceRejects(t *testing.T) {
	topo := receivertopology.ReceiverTopology{
		Model:      "Test FBC Receiver",
		Confidence: receivertopology.ConfidenceVerified,
		Inputs: []receivertopology.PhysicalInput{
			{ID: "input_a", Label: "Tuner A", DeliveryType: receivertopology.DeliveryLegacyUniversal},
		},
		Demodulators: []receivertopology.Demodulator{
			{ID: "demod_a", InputID: "input_a", DVBTypes: []receivertopology.DVBType{receivertopology.DVBTypeSat}},
		},
	}
	topoSvc, err := receivertopology.NewService(topo, receivertopology.EvaluationModeEnforce)
	if err != nil {
		t.Fatalf("failed to create topo service: %v", err)
	}

	evaluator := NewEvaluatorWithTopology(topoSvc)
	policy := ReceiverUsagePolicy{
		Mode:            ReceiverUsageModeEnforce,
		MaxLiveSessions: 10,
	}

	reqInvalid := UsageRequest{
		ReceiverID: "rec-1",
		Owner:      "user-invalid",
		Intent:     IntentLive,
		Source:     SourceIdentity{ReceiverID: "rec-1", ServiceReference: "INVALID_REF"},
		Access: AccessClassification{
			Class:      AccessCapacityNone,
			Confidence: ConfidenceVerified,
		},
		RequestedAt: fixedTime,
	}

	decision, err := evaluator.Evaluate(context.Background(), policy, reqInvalid, SystemSnapshot{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Kind != DecisionReject {
		t.Fatalf("expected DecisionReject for invalid service reference under ENFORCE mode, got %v", decision.Kind)
	}
	if decision.Reason != model.RLeaseBusy {
		t.Fatalf("expected Reason RLeaseBusy, got %v", decision.Reason)
	}
}

func TestReceiverUsage_Topology_InvalidRef_AuditOnlyAllowsWithMessage(t *testing.T) {
	topo := receivertopology.ReceiverTopology{
		Model:      "Test FBC Receiver",
		Confidence: receivertopology.ConfidenceVerified,
		Inputs: []receivertopology.PhysicalInput{
			{ID: "input_a", Label: "Tuner A", DeliveryType: receivertopology.DeliveryLegacyUniversal},
		},
		Demodulators: []receivertopology.Demodulator{
			{ID: "demod_a", InputID: "input_a", DVBTypes: []receivertopology.DVBType{receivertopology.DVBTypeSat}},
		},
	}
	topoSvc, err := receivertopology.NewService(topo, receivertopology.EvaluationModeEnforce)
	if err != nil {
		t.Fatalf("failed to create topo service: %v", err)
	}

	evaluator := NewEvaluatorWithTopology(topoSvc)
	policy := ReceiverUsagePolicy{
		Mode:            ReceiverUsageModeAuditOnly,
		MaxLiveSessions: 10,
	}

	reqInvalid := UsageRequest{
		ReceiverID: "rec-1",
		Owner:      "user-invalid",
		Intent:     IntentLive,
		Source:     SourceIdentity{ReceiverID: "rec-1", ServiceReference: "INVALID_REF"},
		Access: AccessClassification{
			Class:      AccessCapacityNone,
			Confidence: ConfidenceVerified,
		},
		RequestedAt: fixedTime,
	}

	decision, err := evaluator.Evaluate(context.Background(), policy, reqInvalid, SystemSnapshot{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Kind != DecisionAllow {
		t.Fatalf("expected DecisionAllow for AUDIT_ONLY mode, got %v", decision.Kind)
	}
	expectedPrefix := "audit-only: receiver topology evaluation failed:"
	if len(decision.Message) < len(expectedPrefix) || decision.Message[:len(expectedPrefix)] != expectedPrefix {
		t.Fatalf("expected message to start with %q, got %q", expectedPrefix, decision.Message)
	}
}

func TestReceiverUsage_Topology_NilService_EvaluatesNormally(t *testing.T) {
	evaluator := NewEvaluatorWithTopology(nil)
	policy := ReceiverUsagePolicy{
		Mode:            ReceiverUsageModeEnforce,
		MaxLiveSessions: 2,
	}

	req := UsageRequest{
		ReceiverID: "rec-1",
		Owner:      "user-1",
		Intent:     IntentLive,
		Source:     SourceIdentity{ReceiverID: "rec-1", ServiceReference: "1:0:19:283D:3FB:1:C00000:0:0:0:"},
		Access: AccessClassification{
			Class:      AccessCapacityNone,
			Confidence: ConfidenceVerified,
		},
		RequestedAt: fixedTime,
	}

	decision, err := evaluator.Evaluate(context.Background(), policy, req, SystemSnapshot{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Kind != DecisionAllow {
		t.Fatalf("expected DecisionAllow when topologyService is nil, got %v", decision.Kind)
	}
	if len(decision.Requirements) != 1 || decision.Requirements[0].Kind != ReqTunerSlot {
		t.Fatalf("expected 1 ReqTunerSlot requirement, got %v", decision.Requirements)
	}
}
