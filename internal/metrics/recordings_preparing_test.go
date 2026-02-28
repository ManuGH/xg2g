package metrics

import "testing"

func TestIncRecordingsPreparing_IncrementsCanonicalLabels(t *testing.T) {
	initial := getCounterVecValue(t, recordingsPreparingTotal, "blocked", "probe_disabled")
	IncRecordingsPreparing("blocked", "probe_disabled")
	actual := getCounterVecValue(t, recordingsPreparingTotal, "blocked", "probe_disabled")
	if actual != initial+1 {
		t.Fatalf("expected blocked/probe_disabled counter to increase by 1, got initial=%v actual=%v", initial, actual)
	}
}

func TestIncRecordingsPreparing_NonBlockedForcesNoneReason(t *testing.T) {
	initial := getCounterVecValue(t, recordingsPreparingTotal, "in_flight", "none")
	IncRecordingsPreparing("in_flight", "probe_disabled")
	actual := getCounterVecValue(t, recordingsPreparingTotal, "in_flight", "none")
	if actual != initial+1 {
		t.Fatalf("expected in_flight/none counter to increase by 1, got initial=%v actual=%v", initial, actual)
	}
}

func TestIncRecordingsPreparing_NormalizesUnknowns(t *testing.T) {
	initial := getCounterVecValue(t, recordingsPreparingTotal, "unknown", "none")
	IncRecordingsPreparing("custom_state", "custom_reason")
	actual := getCounterVecValue(t, recordingsPreparingTotal, "unknown", "none")
	if actual != initial+1 {
		t.Fatalf("expected unknown/none counter to increase by 1, got initial=%v actual=%v", initial, actual)
	}

	initialBlocked := getCounterVecValue(t, recordingsPreparingTotal, "blocked", "unknown")
	IncRecordingsPreparing("blocked", "custom_reason")
	actualBlocked := getCounterVecValue(t, recordingsPreparingTotal, "blocked", "unknown")
	if actualBlocked != initialBlocked+1 {
		t.Fatalf("expected blocked/unknown counter to increase by 1, got initial=%v actual=%v", initialBlocked, actualBlocked)
	}
}

func TestIncRecordingsPreparing_ProbeStateAllowlist(t *testing.T) {
	testCases := []struct {
		inputState string
		wantState  string
	}{
		{inputState: "queued", wantState: "queued"},
		{inputState: "in_flight", wantState: "in_flight"},
		{inputState: "blocked", wantState: "blocked"},
		{inputState: "unexpected_state", wantState: "unknown"},
	}

	for _, tc := range testCases {
		initial := getCounterVecValue(t, recordingsPreparingTotal, tc.wantState, "none")
		IncRecordingsPreparing(tc.inputState, "none")
		actual := getCounterVecValue(t, recordingsPreparingTotal, tc.wantState, "none")
		if actual != initial+1 {
			t.Fatalf("expected %s/none counter to increase by 1, got initial=%v actual=%v", tc.wantState, initial, actual)
		}
	}
}

func TestIncRecordingsPreparing_BlockedReasonAllowlist(t *testing.T) {
	testCases := []struct {
		inputReason string
		wantReason  string
	}{
		{inputReason: "probe_disabled", wantReason: "probe_disabled"},
		{inputReason: "probe_backoff", wantReason: "probe_backoff"},
		{inputReason: "remote_probe_failed", wantReason: "remote_probe_failed"},
		{inputReason: "unexpected_reason", wantReason: "unknown"},
	}

	for _, tc := range testCases {
		initial := getCounterVecValue(t, recordingsPreparingTotal, "blocked", tc.wantReason)
		IncRecordingsPreparing("blocked", tc.inputReason)
		actual := getCounterVecValue(t, recordingsPreparingTotal, "blocked", tc.wantReason)
		if actual != initial+1 {
			t.Fatalf("expected blocked/%s counter to increase by 1, got initial=%v actual=%v", tc.wantReason, initial, actual)
		}
	}
}
