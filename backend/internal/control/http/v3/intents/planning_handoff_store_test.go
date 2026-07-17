package intents_test

import (
	"errors"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/control/http/v3/intents"
	"github.com/ManuGH/xg2g/internal/domain/playbackplanner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validIssueFixture() (intents.PlanningHandoffIssue, string) {
	evidence := playbackplanner.PlaybackEvidence{
		EvaluatedAt:    time.Now().UnixMilli(),
		Scope:          "live",
		SourceIdentity: "1:0:1:100:200:300:0:0:0:0:",
		Confidence:     "ok",
		PolicyVersion:  "policy-v1",
		SourceTruth: playbackplanner.SourceTruth{
			Container:  "mpegts",
			VideoCodec: "h264",
			AudioCodec: "aac",
		},
		ClientEvidence: playbackplanner.ClientEvidence{
			AllowTranscode:       true,
			SupportedContainers:  []string{"mpegts"},
			SupportedVideoCodecs: []string{"h264"},
			SupportedAudioCodecs: []string{"aac"},
			SupportedEngines:     []string{"hls"},
			SupportsHls:          true,
		},
		HostSnapshot: playbackplanner.HostSnapshot{AvailableEngines: []string{"hls"}},
	}
	evHash, _ := evidence.Hash()

	plan := playbackplanner.PlaybackPlan{
		Decision:       playbackplanner.DecisionAllow,
		Outcome:        playbackplanner.DecisionAllow,
		Mode:           "remux",
		DeliveryEngine: "hls",
		Video:          playbackplanner.TrackPlan{Mode: "copy", Codec: "h264"},
		Audio:          playbackplanner.TrackPlan{Mode: "copy", Codec: "aac"},
		Packaging:      playbackplanner.Packaging{Container: "mpegts"},
	}

	return intents.PlanningHandoffIssue{
		Evidence: evidence,
		Result: playbackplanner.PlanningResult{
			Plan: plan,
			Trace: playbackplanner.PlanTrace{
				PlannerVersion: playbackplanner.PlannerVersion,
				PolicyVersion:  "policy-v1",
				EvidenceHash:   evHash,
			},
		},
		PrincipalID:   "alice",
		ServiceRef:    "1:0:1:100:200:300:0:0:0:0:",
		Scope:         "live",
		PolicyVersion: "policy-v1",
	}, evHash
}

func TestIssuePlanned_Positive(t *testing.T) {
	store := intents.NewPlanningHandoffStore(intents.PlanningHandoffStoreConfig{TTL: time.Minute})
	req, _ := validIssueFixture()

	handoff, err := store.IssuePlanned(req)
	require.NoError(t, err)
	assert.NotEmpty(t, handoff.Receipt.ReceiptID)
	assert.Equal(t, playbackplanner.DecisionAllow, handoff.Plan.Decision)
}

func TestIssuePlanned_Negative_PlanDecisionNotAllow(t *testing.T) {
	store := intents.NewPlanningHandoffStore(intents.PlanningHandoffStoreConfig{TTL: time.Minute})
	req, _ := validIssueFixture()
	req.Result.Plan.Decision = playbackplanner.DecisionDeny

	_, err := store.IssuePlanned(req)
	require.Error(t, err)
	assert.True(t, errors.Is(err, intents.ErrInvalidPlanningHandoff))
}

func TestIssuePlanned_Negative_PlanOutcomeNotAllow(t *testing.T) {
	store := intents.NewPlanningHandoffStore(intents.PlanningHandoffStoreConfig{TTL: time.Minute})
	req, _ := validIssueFixture()
	req.Result.Plan.Outcome = playbackplanner.DecisionDeny

	_, err := store.IssuePlanned(req)
	require.Error(t, err)
	assert.True(t, errors.Is(err, intents.ErrInvalidPlanningHandoff))
}

func TestIssuePlanned_Negative_PlannerVersionMismatch(t *testing.T) {
	store := intents.NewPlanningHandoffStore(intents.PlanningHandoffStoreConfig{TTL: time.Minute})
	req, _ := validIssueFixture()
	req.Result.Trace.PlannerVersion = "v0.0.1-legacy"

	_, err := store.IssuePlanned(req)
	require.Error(t, err)
	assert.True(t, errors.Is(err, intents.ErrPlanningVersionMismatch))
}

func TestIssuePlanned_Negative_EvidenceHashMismatch(t *testing.T) {
	store := intents.NewPlanningHandoffStore(intents.PlanningHandoffStoreConfig{TTL: time.Minute})
	req, _ := validIssueFixture()
	req.Result.Trace.EvidenceHash = "deadbeef00000000000000000000000000000000000000000000000000000000"

	_, err := store.IssuePlanned(req)
	require.Error(t, err)
	assert.True(t, errors.Is(err, intents.ErrPlanningHashMismatch))
}

func TestIssuePlanned_Negative_PolicyVersionMismatch_IssueVsEvidence(t *testing.T) {
	store := intents.NewPlanningHandoffStore(intents.PlanningHandoffStoreConfig{TTL: time.Minute})
	req, _ := validIssueFixture()
	req.PolicyVersion = "policy-v2" // differs from evidence ("policy-v1")

	_, err := store.IssuePlanned(req)
	require.Error(t, err)
	assert.True(t, errors.Is(err, intents.ErrPlanningVersionMismatch))
}

func TestIssuePlanned_Negative_PolicyVersionMismatch_IssueVsTrace(t *testing.T) {
	store := intents.NewPlanningHandoffStore(intents.PlanningHandoffStoreConfig{TTL: time.Minute})
	req, _ := validIssueFixture()
	req.Result.Trace.PolicyVersion = "policy-v2" // differs from issue/evidence

	_, err := store.IssuePlanned(req)
	require.Error(t, err)
	assert.True(t, errors.Is(err, intents.ErrPlanningVersionMismatch))
}

func TestIssuePlanned_Negative_ServiceRefMismatch(t *testing.T) {
	store := intents.NewPlanningHandoffStore(intents.PlanningHandoffStoreConfig{TTL: time.Minute})
	req, _ := validIssueFixture()
	req.ServiceRef = "1:0:1:999:888:777:0:0:0:0:" // differs from evidence source identity

	_, err := store.IssuePlanned(req)
	require.Error(t, err)
	assert.True(t, errors.Is(err, intents.ErrPlanningBindingMismatch))
}

func TestIssuePlanned_Negative_ScopeMismatch(t *testing.T) {
	store := intents.NewPlanningHandoffStore(intents.PlanningHandoffStoreConfig{TTL: time.Minute})
	req, _ := validIssueFixture()
	req.Scope = "vod" // differs from evidence scope ("live")

	_, err := store.IssuePlanned(req)
	require.Error(t, err)
	assert.True(t, errors.Is(err, intents.ErrPlanningBindingMismatch))
}

func TestIssuePlanned_Negative_InvalidExecutionPlan(t *testing.T) {
	store := intents.NewPlanningHandoffStore(intents.PlanningHandoffStoreConfig{TTL: time.Minute})
	req, _ := validIssueFixture()
	req.Result.Plan.DeliveryEngine = "rtmp" // unsupported delivery engine

	_, err := store.IssuePlanned(req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid execution plan")
}
