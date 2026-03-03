package decision

import (
	"context"
	"testing"
)

func baseDecisionInput() DecisionInput {
	return DecisionInput{
		Source: Source{
			Container:  "mp4",
			VideoCodec: "h264",
			AudioCodec: "aac",
		},
		Capabilities: Capabilities{
			Version:       1,
			Containers:    []string{"mp4"},
			VideoCodecs:   []string{"h264"},
			AudioCodecs:   []string{"aac"},
			SupportsHLS:   true,
			SupportsRange: boolPtr(true),
		},
		Policy: Policy{
			AllowTranscode: true,
		},
		APIVersion: "v3",
	}
}

func TestAPIVersionAllowlist(t *testing.T) {
	ctx := context.Background()
	valid := []string{" v3 ", "V3", "v3.0", "v3.1"}
	for _, v := range valid {
		input := baseDecisionInput()
		input.APIVersion = v
		status, _, prob := Decide(ctx, input, "test")
		if prob != nil {
			t.Fatalf("expected api version %q to be accepted, got status %d: %v", v, status, prob)
		}
	}

	invalid := []string{"v3.2", "v4", "legacy"}
	for _, v := range invalid {
		input := baseDecisionInput()
		input.APIVersion = v
		_, _, prob := Decide(ctx, input, "test")
		if prob == nil || prob.Status != 400 {
			t.Fatalf("expected api version %q to be rejected with 400, got %v", v, prob)
		}
	}
}

func TestCapabilitiesVersionStrict(t *testing.T) {
	ctx := context.Background()
	input := baseDecisionInput()
	input.Capabilities.Version = 0
	_, _, prob := Decide(ctx, input, "test")
	if prob == nil || prob.Status != 412 {
		t.Fatalf("expected 412 for missing capabilities_version, got %v", prob)
	}

	input = baseDecisionInput()
	input.Capabilities.Version = 2
	_, _, prob = Decide(ctx, input, "test")
	if prob == nil || prob.Status != 400 {
		t.Fatalf("expected 400 for invalid capabilities_version, got %v", prob)
	}

	input = baseDecisionInput()
	input.Capabilities.Version = 1
	_, _, prob = Decide(ctx, input, "test")
	if prob != nil {
		t.Fatalf("expected capabilities_version=1 to pass, got %v", prob)
	}
}

func TestReasonPriorityPolicyOverCapability(t *testing.T) {
	ctx := context.Background()
	input := baseDecisionInput()
	input.Capabilities.VideoCodecs = []string{"hevc"}
	input.Policy.AllowTranscode = false
	input.Capabilities.SupportsHLS = true

	_, dec, prob := Decide(ctx, input, "test")
	if prob != nil || dec == nil {
		t.Fatalf("expected decision, got %v", prob)
	}
	if dec.Mode != ModeDeny {
		t.Fatalf("expected deny, got %s", dec.Mode)
	}
	if len(dec.Reasons) == 0 || dec.Reasons[0] != ReasonPolicyDeniesTranscode {
		t.Fatalf("expected policy reason first, got %v", dec.Reasons)
	}
	if ReasonPrimaryFrom(dec, nil) != string(ReasonPolicyDeniesTranscode) {
		t.Fatalf("expected primary reason to be policy_denies_transcode, got %s", ReasonPrimaryFrom(dec, nil))
	}
}

func TestTranscodeRequiresHLSSupport(t *testing.T) {
	ctx := context.Background()
	input := baseDecisionInput()
	input.Capabilities.VideoCodecs = []string{"hevc"}
	input.Policy.AllowTranscode = true
	input.Capabilities.SupportsHLS = false

	_, dec, prob := Decide(ctx, input, "test")
	if prob != nil || dec == nil {
		t.Fatalf("expected decision, got %v", prob)
	}
	if dec.Mode != ModeDeny {
		t.Fatalf("expected deny without HLS support, got %s", dec.Mode)
	}
	if !hasReason(dec.Reasons, ReasonHLSNotSupported) {
		t.Fatalf("expected hls_not_supported_by_client in reasons, got %v", dec.Reasons)
	}

	input = baseDecisionInput()
	input.Capabilities.VideoCodecs = []string{"hevc"}
	input.Policy.AllowTranscode = true
	input.Capabilities.SupportsHLS = true

	_, dec, prob = Decide(ctx, input, "test")
	if prob != nil || dec == nil {
		t.Fatalf("expected decision, got %v", prob)
	}
	if dec.Mode != ModeTranscode {
		t.Fatalf("expected transcode with HLS support, got %s", dec.Mode)
	}
}

func hasReason(reasons []ReasonCode, target ReasonCode) bool {
	for _, r := range reasons {
		if r == target {
			return true
		}
	}
	return false
}
