package v3

import (
	"encoding/json"
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
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/normalize"
	"github.com/ManuGH/xg2g/internal/problemcode"
)

func TestPlannerReceiptTokenRoundTripAndBinding(t *testing.T) {
	server, record := plannerReceiptServerFixture(t)
	decisionResult := &decision.Decision{
		Mode:  decision.ModeDirectStream,
		Trace: decision.Trace{RequestID: "trace-1"},
	}

	token := server.buildLivePlaybackDecisionToken(record.Receipt.ServiceRefBind, decisionResult, "live", nil, &record)
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
	server, record := plannerReceiptServerFixture(t)
	validClaims := &v3auth.TokenClaims{
		ReceiptID:      record.Receipt.ReceiptID,
		PlanHash:       record.Receipt.PlanHash,
		EvidenceHash:   record.Receipt.EvidenceHash,
		PlannerVersion: record.Receipt.PlannerVersion,
		PolicyVersion:  record.Receipt.PolicyVersion,
	}

	tests := []struct {
		name       string
		claims     *v3auth.TokenClaims
		principal  string
		restart    bool
		wantStatus int
		wantCode   string
	}{
		{name: "missing", claims: &v3auth.TokenClaims{}, principal: "alice", wantStatus: 409, wantCode: problemcode.CodePlannerReceiptMissing},
		{name: "partial", claims: &v3auth.TokenClaims{ReceiptID: record.Receipt.ReceiptID}, principal: "alice", wantStatus: 409, wantCode: problemcode.CodePlannerReceiptInvalid},
		{name: "wrong principal", claims: validClaims, principal: "bob", wantStatus: 403, wantCode: problemcode.CodePlannerReceiptConflict},
		{name: "missing after restart", claims: validClaims, principal: "alice", restart: true, wantStatus: 409, wantCode: problemcode.CodePlannerReceiptExpired},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.restart {
				server.plannerReceiptStore = v3intents.NewPlanningHandoffStore(v3intents.PlanningHandoffStoreConfig{})
			}
			req := httptest.NewRequest("POST", "/api/v3/intents", nil)
			req = req.WithContext(controlauth.WithPrincipal(req.Context(), controlauth.NewPrincipal("token", tc.principal, nil)))
			response := httptest.NewRecorder()
			resolved, done := server.resolvePlannerReceipt(response, req, tc.claims, record.Receipt.ServiceRefBind)
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

func TestIssuePlannerReceiptRequiresStructuralEquivalence(t *testing.T) {
	server, _ := plannerReceiptServerFixture(t)
	evidence := plannerReceiptEvidenceFixture()
	legacy := &decision.Decision{
		Mode:               decision.ModeDirectStream,
		Selected:           decision.SelectedFormats{Container: "mpegts", VideoCodec: "h264", AudioCodec: "aac"},
		SelectedOutputKind: "hls",
		TargetProfile: &playbackprofile.TargetPlaybackProfile{
			Container: "mpegts",
			Packaging: playbackprofile.PackagingTS,
			Video:     playbackprofile.VideoTarget{Mode: playbackprofile.MediaModeCopy, Codec: "h264"},
			Audio:     playbackprofile.AudioTarget{Mode: playbackprofile.MediaModeCopy, Codec: "aac"},
		},
	}
	record, err := server.issuePlannerReceipt(v3recordings.PlaybackInfoResult{
		SourceRef:       evidence.SourceIdentity,
		Decision:        legacy,
		PlannerEvidence: &evidence,
	}, v3recordings.PlaybackInfoRequest{PrincipalID: "alice"}, "live")
	require.NoError(t, err)
	require.NotNil(t, record)

	legacy.TargetProfile.Video.Codec = "hevc"
	_, err = server.issuePlannerReceipt(v3recordings.PlaybackInfoResult{
		SourceRef:       evidence.SourceIdentity,
		Decision:        legacy,
		PlannerEvidence: &evidence,
	}, v3recordings.PlaybackInfoRequest{PrincipalID: "alice"}, "live")
	require.ErrorContains(t, err, "unexplained diffs")
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
	}, "live")

	require.NoError(t, err)
	require.Nil(t, record)
}

func plannerReceiptServerFixture(t *testing.T) (*Server, v3intents.PlanningHandoff) {
	t.Helper()
	store := v3intents.NewPlanningHandoffStore(v3intents.PlanningHandoffStoreConfig{TTL: time.Minute})
	evidence := plannerReceiptEvidenceFixture()
	plan := playbackplanner.PlaybackPlan{
		Decision:       playbackplanner.DecisionAllow,
		Outcome:        playbackplanner.DecisionAllow,
		Mode:           "remux",
		DeliveryEngine: "hls",
		Video:          playbackplanner.TrackPlan{Mode: "copy", Codec: "h264"},
		Audio:          playbackplanner.TrackPlan{Mode: "copy", Codec: "aac"},
		Packaging:      playbackplanner.Packaging{Container: "mpegts"},
	}
	record, err := store.Issue(v3intents.PlanningHandoffIssue{
		Evidence: evidence,
		Result: playbackplanner.PlanningResult{
			Plan:  plan,
			Trace: playbackplanner.PlanTrace{PlannerVersion: playbackplanner.PlannerVersion, PolicyVersion: "policy-v1"},
		},
		PrincipalID: "alice",
		ServiceRef:  normalize.ServiceRef(evidence.SourceIdentity),
		Scope:       "live",
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
