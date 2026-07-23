package v3

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	controlauth "github.com/ManuGH/xg2g/internal/control/auth"
	v3auth "github.com/ManuGH/xg2g/internal/control/http/v3/auth"
	v3intents "github.com/ManuGH/xg2g/internal/control/http/v3/intents"
	v3recordings "github.com/ManuGH/xg2g/internal/control/http/v3/recordings"
	"github.com/ManuGH/xg2g/internal/control/recordings/decision"
	"github.com/ManuGH/xg2g/internal/domain/playbackplanner"
	"github.com/ManuGH/xg2g/internal/normalize"
	"github.com/ManuGH/xg2g/internal/problemcode"
)

func TestPlannerReceiptTokenRoundTripAndBinding(t *testing.T) {
	server, record := plannerReceiptServerFixture(t)
	eval := &v3recordings.PlannerEvaluation{
		Result: playbackplanner.PlanningResult{
			Plan: playbackplanner.PlaybackPlan{
				Decision:       playbackplanner.DecisionAllow,
				Outcome:        playbackplanner.DecisionAllow,
				DeliveryEngine: "hls",
			},
		},
	}

	token := server.playbackInfoProcessor().BuildLivePlannerDecisionToken(record.Receipt.ServiceRefBind, eval, nil, &record, "test-req-id")
	require.NotNil(t, token)
	claims, err := v3auth.VerifyStrict(*token, server.JWTSecret, "xg2g/v3/intents", "xg2g")
	require.NoError(t, err)
	require.Equal(t, record.Receipt.ReceiptID, claims.ReceiptID)
	require.Equal(t, record.Receipt.PlanHash, claims.PlanHash)
	require.Equal(t, record.Receipt.EvidenceHash, claims.EvidenceHash)
	require.Equal(t, record.Receipt.PlannerVersion, claims.PlannerVersion)
	require.Equal(t, record.Receipt.PolicyVersion, claims.PolicyVersion)
	require.LessOrEqual(t, claims.Exp, record.Receipt.ExpiresAt/1000)

	req := httptest.NewRequest("POST", "/api/v3/intents", nil)
	req = req.WithContext(controlauth.WithPrincipal(req.Context(), controlauth.NewPrincipal("token", "alice", nil)))
	response := httptest.NewRecorder()
	resolved, done := server.resolvePlannerReceipt(response, req, claims, record.Receipt.ServiceRefBind)
	require.False(t, done)
	require.NotNil(t, resolved)
	require.Equal(t, record.Receipt, resolved.Receipt)
}

func TestPlannerReceiptResolutionFailsClosed(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(claims *v3auth.TokenClaims) *v3auth.TokenClaims
		principal  string
		restart    bool
		wantStatus int
		wantCode   string
	}{
		{name: "missing", mutate: func(c *v3auth.TokenClaims) *v3auth.TokenClaims { return &v3auth.TokenClaims{} }, principal: "alice", wantStatus: 409, wantCode: problemcode.CodePlannerReceiptMissing},
		{name: "partial", mutate: func(c *v3auth.TokenClaims) *v3auth.TokenClaims { return &v3auth.TokenClaims{ReceiptID: c.ReceiptID} }, principal: "alice", wantStatus: 409, wantCode: problemcode.CodePlannerReceiptInvalid},
		{name: "wrong principal", mutate: func(c *v3auth.TokenClaims) *v3auth.TokenClaims { return c }, principal: "bob", wantStatus: 403, wantCode: problemcode.CodePlannerReceiptConflict},
		{name: "missing after restart", mutate: func(c *v3auth.TokenClaims) *v3auth.TokenClaims { return c }, principal: "alice", restart: true, wantStatus: 409, wantCode: problemcode.CodePlannerReceiptExpired},
		{name: "mismatched evidence hash", mutate: func(c *v3auth.TokenClaims) *v3auth.TokenClaims {
			c.EvidenceHash = "tampered-evidence-hash"
			return c
		}, principal: "alice", wantStatus: 403, wantCode: problemcode.CodePlannerReceiptConflict},
		{name: "mismatched plan hash", mutate: func(c *v3auth.TokenClaims) *v3auth.TokenClaims {
			c.PlanHash = "tampered-plan-hash"
			return c
		}, principal: "alice", wantStatus: 403, wantCode: problemcode.CodePlannerReceiptConflict},
		{name: "mismatched planner version", mutate: func(c *v3auth.TokenClaims) *v3auth.TokenClaims {
			c.PlannerVersion = "v999.999"
			return c
		}, principal: "alice", wantStatus: 403, wantCode: problemcode.CodePlannerReceiptConflict},
		{name: "mismatched policy version", mutate: func(c *v3auth.TokenClaims) *v3auth.TokenClaims {
			c.PolicyVersion = "policy-v999"
			return c
		}, principal: "alice", wantStatus: 403, wantCode: problemcode.CodePlannerReceiptConflict},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server, record := plannerReceiptServerFixture(t)
			validClaims := &v3auth.TokenClaims{
				ReceiptID:      record.Receipt.ReceiptID,
				PlanHash:       record.Receipt.PlanHash,
				EvidenceHash:   record.Receipt.EvidenceHash,
				PlannerVersion: record.Receipt.PlannerVersion,
				PolicyVersion:  record.Receipt.PolicyVersion,
			}
			if tc.restart {
				server.plannerReceiptStore = v3intents.NewPlanningHandoffStore(v3intents.PlanningHandoffStoreConfig{})
			}
			req := httptest.NewRequest("POST", "/api/v3/intents", nil)
			req = req.WithContext(controlauth.WithPrincipal(req.Context(), controlauth.NewPrincipal("token", tc.principal, nil)))
			response := httptest.NewRecorder()
			resolved, done := server.resolvePlannerReceipt(response, req, tc.mutate(validClaims), record.Receipt.ServiceRefBind)
			require.True(t, done)
			require.Nil(t, resolved)
			require.Equal(t, tc.wantStatus, response.Code)
			var problem struct {
				Code string `json:"code"`
			}
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &problem))
			require.Equal(t, tc.wantCode, problem.Code)
		})
	}
}

func TestPlannerReceiptLegacyFlagsCannotDisablePresentedReceiptValidation(t *testing.T) {
	server, record := plannerReceiptServerFixture(t)
	server.plannerReceiptEnabled = false
	server.plannerReceiptRequired = false

	req := httptest.NewRequest("POST", "/api/v3/intents", nil)
	req = req.WithContext(controlauth.WithPrincipal(req.Context(), controlauth.NewPrincipal("token", "alice", nil)))
	response := httptest.NewRecorder()

	resolved, done := server.resolvePlannerReceipt(response, req, &v3auth.TokenClaims{ReceiptID: record.Receipt.ReceiptID}, record.Receipt.ServiceRefBind)
	require.True(t, done)
	require.Nil(t, resolved)
	require.Equal(t, http.StatusConflict, response.Code)

	var problem struct {
		Code string `json:"code"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &problem))
	require.Equal(t, problemcode.CodePlannerReceiptInvalid, problem.Code)
}

func TestPlanningHandoffStore_NegativeContract(t *testing.T) {
	store := v3intents.NewPlanningHandoffStore(v3intents.PlanningHandoffStoreConfig{TTL: time.Minute})
	evidence := plannerReceiptEvidenceFixture()
	evHash, _ := evidence.Hash()

	// Deny decision must not be issued
	_, err := store.IssuePlanned(v3intents.PlanningHandoffIssue{
		Evidence: evidence,
		Result: playbackplanner.PlanningResult{
			Plan: playbackplanner.PlaybackPlan{
				Decision: playbackplanner.DecisionDeny,
				Outcome:  playbackplanner.DecisionDeny,
			},
			Trace: playbackplanner.PlanTrace{PlannerVersion: playbackplanner.PlannerVersion, PolicyVersion: "policy-v1", EvidenceHash: evHash},
		},
		PrincipalID:   "alice",
		ServiceRef:    normalize.ServiceRef(evidence.SourceIdentity),
		Scope:         "live",
		PolicyVersion: "policy-v1",
	})
	require.ErrorIs(t, err, v3intents.ErrInvalidPlanningHandoff)

	// Version mismatch must not be issued
	_, err = store.IssuePlanned(v3intents.PlanningHandoffIssue{
		Evidence: evidence,
		Result: playbackplanner.PlanningResult{
			Plan: playbackplanner.PlaybackPlan{
				Decision: playbackplanner.DecisionAllow,
				Outcome:  playbackplanner.DecisionAllow,
			},
			Trace: playbackplanner.PlanTrace{PlannerVersion: "v0.0-wrong", PolicyVersion: "policy-v1", EvidenceHash: evHash},
		},
		PrincipalID:   "alice",
		ServiceRef:    normalize.ServiceRef(evidence.SourceIdentity),
		Scope:         "live",
		PolicyVersion: "policy-v1",
	})
	require.ErrorIs(t, err, v3intents.ErrPlanningVersionMismatch)

	// Evidence hash mismatch must not be issued
	_, err = store.IssuePlanned(v3intents.PlanningHandoffIssue{
		Evidence: evidence,
		Result: playbackplanner.PlanningResult{
			Plan: playbackplanner.PlaybackPlan{
				Decision: playbackplanner.DecisionAllow,
				Outcome:  playbackplanner.DecisionAllow,
			},
			Trace: playbackplanner.PlanTrace{PlannerVersion: playbackplanner.PlannerVersion, PolicyVersion: "policy-v1", EvidenceHash: "wrong-evidence-hash"},
		},
		PrincipalID:   "alice",
		ServiceRef:    normalize.ServiceRef(evidence.SourceIdentity),
		Scope:         "live",
		PolicyVersion: "policy-v1",
	})
	require.ErrorIs(t, err, v3intents.ErrPlanningHashMismatch)
}

func TestIssuePlannerReceiptFromPlannerEvaluation(t *testing.T) {
	server, _ := plannerReceiptServerFixture(t)
	evidence := plannerReceiptEvidenceFixture()
	result, err := playbackplanner.Plan(evidence)
	require.NoError(t, err)

	record, err := server.issuePlannerReceipt(v3recordings.PlaybackInfoResult{
		SourceRef: evidence.SourceIdentity,
		PlannerEvaluation: &v3recordings.PlannerEvaluation{
			Evidence: evidence,
			Result:   result,
		},
	}, v3recordings.PlaybackInfoRequest{PrincipalID: "alice"}, "live", "test-req-id")
	require.NoError(t, err)
	require.NotNil(t, record)
	require.NotEmpty(t, record.Receipt.PlanHash)

	resultDeny := result
	resultDeny.Plan.Decision = playbackplanner.DecisionDeny
	recordDeny, err := server.issuePlannerReceipt(v3recordings.PlaybackInfoResult{
		SourceRef: evidence.SourceIdentity,
		PlannerEvaluation: &v3recordings.PlannerEvaluation{
			Evidence: evidence,
			Result:   resultDeny,
		},
	}, v3recordings.PlaybackInfoRequest{PrincipalID: "alice"}, "live", "test-req-id")
	require.NoError(t, err)
	require.Nil(t, recordDeny)
}

func TestIssuePlannerReceiptSkipsPassiveEpgBadge(t *testing.T) {
	server, _ := plannerReceiptServerFixture(t)

	record, err := server.issuePlannerReceipt(v3recordings.PlaybackInfoResult{
		SourceRef: "1:0:1:100:200:300:0:0:0:0:",
		Decision:  &decision.Decision{Mode: decision.ModeDirectStream},
		// Deliberately no PlannerEvidence: a required receipt must not turn a
		// passive EPG preview into an error.
	}, v3recordings.PlaybackInfoRequest{
		PrincipalID: "alice",
		Headers: map[string]string{
			v3recordings.PlaybackInfoContextHeader: v3recordings.PlaybackInfoContextEpgBadge,
		},
	}, "live", "test-req-id")

	require.NoError(t, err)
	require.Nil(t, record)
}

func plannerReceiptServerFixture(t *testing.T) (*Server, v3intents.PlanningHandoff) {
	t.Helper()
	store := v3intents.NewPlanningHandoffStore(v3intents.PlanningHandoffStoreConfig{TTL: time.Minute})
	evidence := plannerReceiptEvidenceFixture()
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
	record, err := store.IssuePlanned(v3intents.PlanningHandoffIssue{
		Evidence: evidence,
		Result: playbackplanner.PlanningResult{
			Plan:  plan,
			Trace: playbackplanner.PlanTrace{PlannerVersion: playbackplanner.PlannerVersion, PolicyVersion: "policy-v1", EvidenceHash: evHash},
		},
		PrincipalID:   "alice",
		ServiceRef:    normalize.ServiceRef(evidence.SourceIdentity),
		Scope:         "live",
		PolicyVersion: "policy-v1",
	})
	require.NoError(t, err)
	return &Server{
		JWTSecret:              v3auth.TestSecret(),
		plannerReceiptStore:    store,
		plannerReceiptEnabled:  true,
		plannerReceiptRequired: true,
	}, record
}

func plannerReceiptEvidenceFixture() playbackplanner.PlaybackEvidence {
	return playbackplanner.PlaybackEvidence{
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
}
