package policy

import (
	"errors"
	"testing"
	"time"
)

func TestPolicyEngine_AvailableSlot_Granted(t *testing.T) {
	engine := NewPolicyEngine()
	req := EvaluationRequest{
		Consumer:    ConsumerLiveTV,
		Owner:       "session-1",
		RequestedAt: time.Now(),
	}

	active := []ActiveLeaseState{
		{LeaseID: "lease-0", Consumer: ConsumerLiveTV, Owner: "session-0"},
	}

	res, err := engine.Evaluate(req, active, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.Decision != DecisionGrant {
		t.Errorf("expected DecisionGrant, got %s", res.Decision)
	}
	if res.ReasonCode != ReasonPolicyGrantedAvailable {
		t.Errorf("expected %s, got %s", ReasonPolicyGrantedAvailable, res.ReasonCode)
	}
}

func TestPolicyEngine_ExhaustiveConflictMatrix(t *testing.T) {
	engine := NewPolicyEngine()
	now := time.Now()

	consumers := []ConsumerType{
		ConsumerScheduledRecording,
		ConsumerManualRecording,
		ConsumerLiveTV,
		ConsumerRetroDVR,
		ConsumerChannelScan,
		ConsumerBackgroundTransfer,
	}

	for _, incoming := range consumers {
		for _, existing := range consumers {
			t.Run(string(incoming)+"_vs_"+string(existing), func(t *testing.T) {
				req := EvaluationRequest{
					Consumer:    incoming,
					Owner:       "incoming-owner",
					RequestedAt: now,
				}

				active := []ActiveLeaseState{
					{
						LeaseID:    "active-lease-1",
						Consumer:   existing,
						Owner:      "existing-owner",
						AcquiredAt: now.Add(-10 * time.Minute),
					},
				}

				res, err := engine.Evaluate(req, active, 1) // 1 slot capacity -> forces conflict evaluation
				if err != nil {
					t.Fatalf("unexpected error during evaluation: %v", err)
				}

				incomingWeight := incoming.PriorityWeight()
				existingWeight := existing.PriorityWeight()

				if existing == ConsumerScheduledRecording {
					// Invariant: ScheduledRecording is sacrosanct and NEVER preemptible by any consumer
					if res.Decision != DecisionReject {
						t.Errorf("expected DecisionReject when existing is ScheduledRecording, got %s", res.Decision)
					}
					return
				}

				if incomingWeight > existingWeight {
					if res.Decision != DecisionPreempt {
						t.Errorf("expected DecisionPreempt for higher priority incoming (%d vs %d), got %s", incomingWeight, existingWeight, res.Decision)
					}
					if res.ReasonCode != ReasonPolicyGrantedPreempted {
						t.Errorf("expected ReasonCode %s, got %s", ReasonPolicyGrantedPreempted, res.ReasonCode)
					}
					if res.TargetLeaseID != "active-lease-1" {
						t.Errorf("expected TargetLeaseID active-lease-1, got %s", res.TargetLeaseID)
					}
				} else {
					if res.Decision != DecisionReject {
						t.Errorf("expected DecisionReject for equal or lower priority incoming (%d vs %d), got %s", incomingWeight, existingWeight, res.Decision)
					}
				}
			})
		}
	}
}

func TestPolicyEngine_SacrosanctLease_NeverPreempted(t *testing.T) {
	engine := NewPolicyEngine()
	now := time.Now()

	req := EvaluationRequest{
		Consumer:    ConsumerScheduledRecording,
		Owner:       "recording-job-1",
		RequestedAt: now,
	}

	active := []ActiveLeaseState{
		{
			LeaseID:      "lease-sacrosanct",
			Consumer:     ConsumerBackgroundTransfer, // Lower priority, but marked sacrosanct
			Owner:        "transfer-job-1",
			IsSacrosanct: true,
		},
	}

	res, err := engine.Evaluate(req, active, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.Decision != DecisionReject {
		t.Fatalf("expected DecisionReject when all candidates are sacrosanct, got %s", res.Decision)
	}
}

func TestPolicyEngine_ReleasingLease_NeverPreempted(t *testing.T) {
	engine := NewPolicyEngine()
	now := time.Now()

	req := EvaluationRequest{
		Consumer:    ConsumerScheduledRecording,
		Owner:       "recording-job-1",
		RequestedAt: now,
	}

	active := []ActiveLeaseState{
		{
			LeaseID:     "lease-releasing",
			Consumer:    ConsumerLiveTV,
			Owner:       "session-1",
			IsReleasing: true,
		},
	}

	res, err := engine.Evaluate(req, active, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.Decision != DecisionReject {
		t.Fatalf("expected DecisionReject when candidate is releasing, got %s", res.Decision)
	}
}

func TestPolicyEngine_SelectsLowestPriorityCandidate(t *testing.T) {
	engine := NewPolicyEngine()
	now := time.Now()

	req := EvaluationRequest{
		Consumer:    ConsumerScheduledRecording,
		Owner:       "recording-job-1",
		RequestedAt: now,
	}

	active := []ActiveLeaseState{
		{LeaseID: "lease-livetv", Consumer: ConsumerLiveTV, AcquiredAt: now.Add(-5 * time.Minute)},
		{LeaseID: "lease-scan", Consumer: ConsumerChannelScan, AcquiredAt: now.Add(-10 * time.Minute)},
	}

	res, err := engine.Evaluate(req, active, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.Decision != DecisionPreempt {
		t.Fatalf("expected DecisionPreempt, got %s", res.Decision)
	}
	if res.TargetLeaseID != "lease-scan" {
		t.Errorf("expected lowest priority candidate 'lease-scan' to be targeted, got %s", res.TargetLeaseID)
	}
}

func TestPolicyEngine_InvalidConsumer_ReturnsError(t *testing.T) {
	engine := NewPolicyEngine()
	req := EvaluationRequest{
		Consumer: ConsumerType("INVALID_CONSUMER"),
	}

	_, err := engine.Evaluate(req, nil, 2)
	if !errors.Is(err, ErrInvalidConsumerType) {
		t.Fatalf("expected ErrInvalidConsumerType, got %v", err)
	}
}
