package playbackreceipt

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ManuGH/xg2g/internal/domain/playbackplanner"
)

func TestStoreIssueResolveConsumeLifecycle(t *testing.T) {
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	store := NewStore(Config{
		Capacity: 2,
		TTL:      time.Minute,
		Clock:    func() time.Time { return now },
		NewID:    func() string { return "receipt-1" },
	})
	record, err := store.Issue(testIssueRequest())
	require.NoError(t, err)
	require.Equal(t, StateIssued, record.Receipt.LifecycleState)
	require.Equal(t, now.UnixMilli(), record.Receipt.IssuedAt)

	binding := bindingFromRecord(record)
	loaded, err := store.Resolve(binding)
	require.NoError(t, err)
	require.Equal(t, record.Receipt, loaded.Receipt)

	consumed, err := store.Consume(record.Receipt.ReceiptID, "session-1")
	require.NoError(t, err)
	require.Equal(t, StateConsumed, consumed.Receipt.LifecycleState)
	require.Equal(t, "session-1", consumed.Receipt.ConsumedSessionID)

	_, err = store.Consume(record.Receipt.ReceiptID, "session-1")
	require.NoError(t, err)
	_, err = store.Consume(record.Receipt.ReceiptID, "session-2")
	require.ErrorIs(t, err, ErrAlreadyConsumed)
}

func TestStoreRejectsEveryBindingMismatch(t *testing.T) {
	store := NewStore(Config{NewID: func() string { return "receipt-1" }})
	record, err := store.Issue(testIssueRequest())
	require.NoError(t, err)

	tests := []struct {
		name string
		edit func(*Binding)
		want error
	}{
		{"evidence hash", func(b *Binding) { b.EvidenceHash = "other" }, ErrHashMismatch},
		{"plan hash", func(b *Binding) { b.PlanHash = "other" }, ErrHashMismatch},
		{"planner version", func(b *Binding) { b.PlannerVersion = "other" }, ErrVersionMismatch},
		{"policy version", func(b *Binding) { b.PolicyVersion = "other" }, ErrVersionMismatch},
		{"principal", func(b *Binding) { b.PrincipalID = "other" }, ErrBindingMismatch},
		{"service", func(b *Binding) { b.ServiceRef = "other" }, ErrBindingMismatch},
		{"scope", func(b *Binding) { b.Scope = "other" }, ErrBindingMismatch},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			binding := bindingFromRecord(record)
			tc.edit(&binding)
			_, err := store.Resolve(binding)
			require.ErrorIs(t, err, tc.want)
		})
	}
}

func TestStoreExpiresAndEvictsOldest(t *testing.T) {
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	nextID := 0
	store := NewStore(Config{
		Capacity: 2,
		TTL:      time.Second,
		Clock:    func() time.Time { return now },
		NewID: func() string {
			nextID++
			return []string{"one", "two", "three"}[nextID-1]
		},
	})
	one, err := store.Issue(testIssueRequest())
	require.NoError(t, err)
	now = now.Add(time.Millisecond)
	_, err = store.Issue(testIssueRequest())
	require.NoError(t, err)
	now = now.Add(time.Millisecond)
	_, err = store.Issue(testIssueRequest())
	require.NoError(t, err)
	require.Equal(t, 2, store.Len())
	_, err = store.Resolve(bindingFromRecord(one))
	require.True(t, errors.Is(err, ErrReceiptNotFound))

	now = now.Add(2 * time.Second)
	require.Equal(t, 0, store.Len())
}

func TestStoreCopiesMutablePlannerData(t *testing.T) {
	store := NewStore(Config{NewID: func() string { return "receipt-1" }})
	req := testIssueRequest()
	record, err := store.Issue(req)
	require.NoError(t, err)
	req.Evidence.ClientEvidence.SupportedVideoCodecs[0] = "mutated"
	req.Result.Plan.Guardrails.PermittedAlternativePlans[0] = "mutated"
	req.Result.Trace.Log[0].Rule = "mutated"

	loaded, err := store.Resolve(bindingFromRecord(record))
	require.NoError(t, err)
	require.Equal(t, "h264", loaded.Evidence.ClientEvidence.SupportedVideoCodecs[0])
	require.Equal(t, "repair", loaded.Plan.Guardrails.PermittedAlternativePlans[0])
	require.Equal(t, "mode", loaded.Trace.Log[0].Rule)
}

func testIssueRequest() IssueRequest {
	evidence := playbackplanner.PlaybackEvidence{
		EvaluatedAt:    time.Now().UnixMilli(),
		Scope:          "live",
		SourceIdentity: "service:1",
		PolicyVersion:  "policy-v1",
		SourceTruth: playbackplanner.SourceTruth{
			Container:  "mpegts",
			VideoCodec: "h264",
			AudioCodec: "aac",
		},
		ClientEvidence: playbackplanner.ClientEvidence{
			AllowTranscode:       true,
			SupportedVideoCodecs: []string{"h264"},
			SupportedAudioCodecs: []string{"aac"},
			SupportedContainers:  []string{"fmp4"},
			SupportedEngines:     []string{"hls"},
			SupportsHls:          true,
		},
		HostSnapshot: playbackplanner.HostSnapshot{AvailableEngines: []string{"hls"}},
	}
	plan := playbackplanner.PlaybackPlan{
		Decision:       playbackplanner.DecisionAllow,
		Outcome:        playbackplanner.DecisionAllow,
		Mode:           "remux",
		DeliveryEngine: "hls",
		Video:          playbackplanner.TrackPlan{Mode: "copy", Codec: "h264"},
		Audio:          playbackplanner.TrackPlan{Mode: "copy", Codec: "aac"},
		Packaging:      playbackplanner.Packaging{Container: "fmp4"},
		Guardrails:     playbackplanner.Guardrails{PermittedAlternativePlans: []string{"repair"}},
	}
	return IssueRequest{
		Evidence: evidence,
		Result: playbackplanner.PlanningResult{
			Plan: plan,
			Trace: playbackplanner.PlanTrace{
				PlannerVersion: "planner-v1",
				PolicyVersion:  "policy-v1",
				Log:            []playbackplanner.RuleHit{{Rule: "mode", Result: "allow"}},
			},
		},
		PrincipalID: "principal-1",
		ServiceRef:  "service:1",
		Scope:       "live",
	}
}

func bindingFromRecord(record Record) Binding {
	return Binding{
		ReceiptID:      record.Receipt.ReceiptID,
		EvidenceHash:   record.Receipt.EvidenceHash,
		PlanHash:       record.Receipt.PlanHash,
		PlannerVersion: record.Receipt.PlannerVersion,
		PolicyVersion:  record.Receipt.PolicyVersion,
		PrincipalID:    record.Receipt.PrincipalBind,
		ServiceRef:     record.Receipt.ServiceRefBind,
		Scope:          record.Receipt.ScopeBind,
	}
}
