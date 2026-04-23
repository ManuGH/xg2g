package v3

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/control/recordings/capreg"
	"github.com/ManuGH/xg2g/internal/control/recordings/runtimepolicy"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/stretchr/testify/require"
)

func TestSessionHeartbeat_TicksRuntimePolicyLoop(t *testing.T) {
	s, st := newV3TestServer(t, t.TempDir())
	registry := capreg.NewMemoryStore()
	s.capabilityRegistry = registry

	sessionID := "550e8400-e29b-41d4-a716-446655440099"
	now := time.Now().UTC()
	reqID := "req-runtime-loop-001"

	require.NoError(t, registry.RecordObservation(context.Background(), capreg.PlaybackObservation{
		ObservedAt:        now.Add(-10 * time.Second),
		RequestID:         reqID,
		ObservationKind:   "decision",
		Outcome:           "predicted",
		SubjectKind:       "live",
		SourceFingerprint: "source-fp-1",
		DeviceFingerprint: "device-fp-1",
		HostFingerprint:   "host-fp-1",
	}))
	require.NoError(t, registry.RememberPlaybackPolicyState(context.Background(), capreg.PlaybackPolicyState{
		SubjectKind:       "live",
		SourceFingerprint: "source-fp-1",
		DeviceFingerprint: "device-fp-1",
		HostFingerprint:   "host-fp-1",
		MaxQualityRung:    playbackprofile.RungQualityVideoH264CRF20,
		Confidence: runtimepolicy.ConfidenceSnapshot{
			Score:   -44,
			State:   runtimepolicy.ConfidenceLow,
			Reasons: []string{runtimepolicy.ReasonBufferingRecent},
		},
		UpdatedAt: now,
	}))

	require.NoError(t, st.PutSession(context.Background(), &model.SessionRecord{
		SessionID:          sessionID,
		State:              model.SessionReady,
		ServiceRef:         "1:0:1:445D:453:1:C00000:0:0:0:",
		HeartbeatInterval:  30,
		LeaseExpiresAtUnix: now.Add(30 * time.Second).Unix(),
		LastHeartbeatUnix:  now.Add(-31 * time.Second).Unix(),
		ContextData: map[string]string{
			model.CtxKeyDecisionRequest:   reqID,
			model.CtxKeyRuntimeTargetStep: string(runtimepolicy.PlaybackStepH2641080p),
		},
		PlaybackTrace: &model.PlaybackTrace{
			VideoQualityRung: string(playbackprofile.RungQualityVideoH264CRF20),
			TargetProfile: &playbackprofile.TargetPlaybackProfile{
				Video: playbackprofile.VideoTarget{
					Mode:   playbackprofile.MediaModeTranscode,
					Codec:  "h264",
					Width:  1920,
					CRF:    20,
					Preset: "slow",
				},
				Audio: playbackprofile.AudioTarget{
					Mode:  playbackprofile.MediaModeTranscode,
					Codec: "aac",
				},
			},
		},
	}))

	req := httptest.NewRequest(http.MethodPost, V3BaseURL+"/sessions/"+sessionID+"/heartbeat", nil)
	rr := httptest.NewRecorder()

	s.handleSessionHeartbeat(rr, req, sessionID)
	require.Equal(t, http.StatusOK, rr.Code)

	updated, err := st.GetSession(context.Background(), sessionID)
	require.NoError(t, err)
	require.NotNil(t, updated)
	require.Equal(t, string(runtimepolicy.PolicyStepDown), updated.ContextData[model.CtxKeyRuntimePolicyAction])
	require.Equal(t, string(runtimepolicy.PlaybackStepH264720p), updated.ContextData[model.CtxKeyRuntimeCurrentStep])
	require.NotEmpty(t, updated.ContextData[model.CtxKeyRuntimePolicyState])
	require.NotEmpty(t, updated.ContextData[model.CtxKeyRuntimePolicyReplay])
	timeline := loadSessionRuntimeTimeline(updated)
	require.Len(t, timeline, 1)
	require.Equal(t, runtimepolicy.PolicyStepDown, timeline[0].PolicyAction)
	require.Equal(t, runtimepolicy.SessionTransitionScheduleStepDown, timeline[0].PlannedTransition)
	require.Equal(t, runtimepolicy.SessionTransitionScheduleStepDown, timeline[0].ExecutedTransition)
	require.Equal(t, runtimepolicy.PlaybackStepH264720p, timeline[0].ActiveStep)
	replay := loadSessionRuntimeReplay(updated)
	require.NotNil(t, replay)
	require.Len(t, replay.Ticks, 1)
	require.Equal(t, runtimepolicy.PolicyStepDown, replay.Ticks[0].Expected.Action)
}
