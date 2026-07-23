// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	controlhttp "github.com/ManuGH/xg2g/internal/control/http"
	v3sessions "github.com/ManuGH/xg2g/internal/control/http/v3/sessions"
	"github.com/ManuGH/xg2g/internal/control/recordings/runtimepolicy"
	"github.com/ManuGH/xg2g/internal/domain/session/lifecycle"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteSessionStateResponse_WritesJSONAndTraceHeader(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v3/sessions/550e8400-e29b-41d4-a716-446655440001", nil)
	r = r.WithContext(log.ContextWithRequestID(r.Context(), "req-session-response"))

	writeSessionStateResponse(w, r, "", v3sessions.GetSessionResult{
		Session: &model.SessionRecord{
			SessionID:          "550e8400-e29b-41d4-a716-446655440001",
			ServiceRef:         "1:0:1:445D:453:1:C00000:0:0:0:",
			Profile:            model.ProfileSpec{Name: "compatible"},
			State:              model.SessionReady,
			CorrelationID:      "corr-123",
			UpdatedAtUnix:      1700000000,
			HeartbeatInterval:  30,
			LeaseExpiresAtUnix: 1700000030,
			ContextData:        map[string]string{model.CtxKeyClientPath: "hlsjs"},
			PlaybackTrace:      &model.PlaybackTrace{RequestProfile: "compatible"},
			LastAccessUnix:     1700000000,
		},
		Outcome: lifecycle.PublicOutcome{
			State:      model.SessionReady,
			Reason:     model.RNone,
			DetailCode: model.DNone,
		},
		PlaybackInfo: v3sessions.SessionPlaybackInfo{
			Mode:                 model.ModeRecording,
			WindowKind:           v3sessions.SessionWindowKindVOD,
			DurationSeconds:      float64Ptr(3600),
			SeekableStartSeconds: float64Ptr(0),
			SeekableEndSeconds:   float64Ptr(3600),
		},
	})

	resp := w.Result()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "req-session-response", resp.Header.Get(controlhttp.HeaderRequestID))
	assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")

	var body SessionResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "req-session-response", body.RequestId)
	require.NotNil(t, body.Trace)
	require.NotNil(t, body.Trace.SessionId)
	assert.Equal(t, "550e8400-e29b-41d4-a716-446655440001", *body.Trace.SessionId)
	assert.Nil(t, body.Trace.HlsDebug)
	require.NotNil(t, body.Mode)
	assert.Equal(t, RECORDING, *body.Mode)
	require.NotNil(t, body.WindowKind)
	assert.Equal(t, SessionResponseWindowKindVod, *body.WindowKind)
	require.NotNil(t, body.PlaybackUrl)
	assert.Equal(t, V3BaseURL+"/sessions/550e8400-e29b-41d4-a716-446655440001/hls/index.m3u8", *body.PlaybackUrl)
	require.NotNil(t, body.DurationSeconds)
	assert.Equal(t, float32(3600), *body.DurationSeconds)
	assert.Equal(t, int32(30), body.HeartbeatIntervalSeconds)
	assert.Equal(t, "2023-11-14T22:13:50Z", body.LeaseExpiresAt.Format(time.RFC3339))
}

func TestMapSessionStateResponse_IncludesProfileReasonForSafariTranscode(t *testing.T) {
	resp := mapSessionStateResponse("req-profile-reason", "", v3sessions.GetSessionResult{
		Session: &model.SessionRecord{
			SessionID:          "550e8400-e29b-41d4-a716-446655440002",
			ServiceRef:         "1:0:1:445D:453:1:C00000:0:0:0:",
			Profile:            model.ProfileSpec{Name: profiles.ProfileSafari, TranscodeVideo: true},
			State:              model.SessionPriming,
			CorrelationID:      "corr-safari",
			HeartbeatInterval:  30,
			LeaseExpiresAtUnix: 1700000030,
		},
		Outcome: lifecycle.PublicOutcome{
			State:      model.SessionPriming,
			Reason:     model.RNone,
			DetailCode: model.DNone,
		},
		PlaybackInfo: v3sessions.SessionPlaybackInfo{
			Mode:       model.ModeLive,
			WindowKind: v3sessions.SessionWindowKindLive,
		},
	})

	require.NotNil(t, resp.ProfileReason)
	assert.Equal(t, sessionProfileReasonSafariCompatTranscode, *resp.ProfileReason)
	require.NotNil(t, resp.WindowKind)
	assert.Equal(t, SessionResponseWindowKindLive, *resp.WindowKind)
}

func TestMapSessionStateResponse_IncludesProfileReasonForSafariRuntimeHQTranscode(t *testing.T) {
	resp := mapSessionStateResponse("req-profile-reason-runtime-hq", "", v3sessions.GetSessionResult{
		Session: &model.SessionRecord{
			SessionID:          "550e8400-e29b-41d4-a716-446655440003",
			ServiceRef:         "1:0:1:445D:453:1:C00000:0:0:0:",
			Profile:            model.ProfileSpec{Name: profiles.ProfileSafariRuntimeHQ, TranscodeVideo: true},
			State:              model.SessionPriming,
			CorrelationID:      "corr-safari-runtime-hq",
			HeartbeatInterval:  30,
			LeaseExpiresAtUnix: 1700000030,
		},
		Outcome: lifecycle.PublicOutcome{
			State:      model.SessionPriming,
			Reason:     model.RNone,
			DetailCode: model.DNone,
		},
		PlaybackInfo: v3sessions.SessionPlaybackInfo{
			Mode:       model.ModeLive,
			WindowKind: v3sessions.SessionWindowKindLive,
		},
	})

	require.NotNil(t, resp.ProfileReason)
	assert.Equal(t, sessionProfileReasonSafariCompatTranscode, *resp.ProfileReason)
}

func TestMapSessionStateResponse_ExposesAutoCodecTrace(t *testing.T) {
	resp := mapSessionStateResponse("req-auto-codec", "", v3sessions.GetSessionResult{
		Session: &model.SessionRecord{
			SessionID:          "550e8400-e29b-41d4-a716-446655440099",
			ServiceRef:         "1:0:1:445D:453:1:C00000:0:0:0:",
			Profile:            model.ProfileSpec{Name: profiles.ProfileSafariHEVC},
			State:              model.SessionReady,
			CorrelationID:      "corr-auto-codec",
			HeartbeatInterval:  30,
			LeaseExpiresAtUnix: 1700000030,
			PlaybackTrace: &model.PlaybackTrace{
				AutoCodecPolicy:     "host_aware_bottleneck",
				AutoCodecRequested:  "av1,hevc,h264",
				AutoCodecSelected:   "hevc",
				AutoCodecHostClass:  "medium",
				AutoCodecBenchClass: "strong",
			},
		},
		Outcome: lifecycle.PublicOutcome{
			State:      model.SessionReady,
			Reason:     model.RNone,
			DetailCode: model.DNone,
		},
		PlaybackInfo: v3sessions.SessionPlaybackInfo{
			Mode:       model.ModeLive,
			WindowKind: v3sessions.SessionWindowKindLive,
		},
	})

	require.NotNil(t, resp.Trace)
	require.NotNil(t, resp.Trace.AutoCodecPolicy)
	assert.Equal(t, "host_aware_bottleneck", *resp.Trace.AutoCodecPolicy)
	require.NotNil(t, resp.Trace.AutoCodecRequestedCodecs)
	assert.Equal(t, "av1,hevc,h264", *resp.Trace.AutoCodecRequestedCodecs)
	require.NotNil(t, resp.Trace.AutoCodecSelectedCodec)
	assert.Equal(t, "hevc", *resp.Trace.AutoCodecSelectedCodec)
	require.NotNil(t, resp.Trace.AutoCodecPerformanceClass)
	assert.Equal(t, "medium", *resp.Trace.AutoCodecPerformanceClass)
	require.NotNil(t, resp.Trace.AutoCodecBenchmarkClass)
	assert.Equal(t, "strong", *resp.Trace.AutoCodecBenchmarkClass)
}

func TestMapSessionStateResponse_ExposesRuntimeDiagnosticsTrace(t *testing.T) {
	resp := mapSessionStateResponse("req-runtime-diagnostics", "", v3sessions.GetSessionResult{
		Session: &model.SessionRecord{
			SessionID:          "550e8400-e29b-41d4-a716-446655440100",
			ServiceRef:         "1:0:19:91:4:85:C00000:0:0:0:",
			Profile:            model.ProfileSpec{Name: profiles.ProfileAV1HW},
			State:              model.SessionReady,
			CorrelationID:      "corr-runtime-diagnostics",
			HeartbeatInterval:  30,
			LeaseExpiresAtUnix: 1700000030,
			PlaybackTrace: &model.PlaybackTrace{
				RuntimeDiagnostics: &ports.RuntimeDiagnostics{
					FrameCount:           6472,
					FPS:                  51.35,
					DropFrames:           0,
					DupFrames:            52,
					Speed:                1.03,
					CorruptDecodedFrames: 2,
					LastWarning:          "[mpegts @ 0x123] corrupt decoded frame in stream 0",
					UpdatedAtUnix:        1700000012,
				},
			},
		},
		Outcome: lifecycle.PublicOutcome{
			State:      model.SessionReady,
			Reason:     model.RNone,
			DetailCode: model.DNone,
		},
		PlaybackInfo: v3sessions.SessionPlaybackInfo{
			Mode:       model.ModeLive,
			WindowKind: v3sessions.SessionWindowKindLive,
		},
	})

	require.NotNil(t, resp.Trace)
	require.NotNil(t, resp.Trace.RuntimeDiagnostics)
	assert.Equal(t, 6472, *resp.Trace.RuntimeDiagnostics.FrameCount)
	assert.Equal(t, float32(51.35), *resp.Trace.RuntimeDiagnostics.Fps)
	assert.Equal(t, 0, *resp.Trace.RuntimeDiagnostics.DropFrames)
	assert.Equal(t, 52, *resp.Trace.RuntimeDiagnostics.DupFrames)
	assert.Equal(t, float32(1.03), *resp.Trace.RuntimeDiagnostics.Speed)
	assert.Equal(t, 2, *resp.Trace.RuntimeDiagnostics.CorruptDecodedFrames)
	require.NotNil(t, resp.Trace.RuntimeDiagnostics.LastWarning)
	assert.Contains(t, *resp.Trace.RuntimeDiagnostics.LastWarning, "corrupt decoded frame")
	assert.Equal(t, 1700000012, *resp.Trace.RuntimeDiagnostics.UpdatedAtUnix)
}

func TestMapSessionStateResponse_ExposesRuntimePolicyTimeline(t *testing.T) {
	statePayload, err := json.Marshal(runtimepolicy.SessionLoopState{
		CurrentStep:     runtimepolicy.PlaybackStepH264720p,
		TargetStep:      runtimepolicy.PlaybackStepDirectCopy,
		ProbeStep:       runtimepolicy.PlaybackStepH2641080p,
		ProbeState:      runtimepolicy.ProbeLifecycleScheduled,
		ConfidenceScore: 61,
		ConfidenceState: runtimepolicy.ConfidenceHigh,
		LastAction:      runtimepolicy.PolicyProbeUp,
		Reasons:         []string{runtimepolicy.ReasonHeadroomGood, runtimepolicy.ReasonProbeUpReady},
	})
	require.NoError(t, err)
	timelinePayload, err := json.Marshal([]runtimepolicy.TickTrace{{
		TickAt:             time.Unix(1700000000, 0).UTC(),
		ConfidenceScore:    61,
		ConfidenceState:    runtimepolicy.ConfidenceHigh,
		PolicyAction:       runtimepolicy.PolicyProbeUp,
		PlannedTransition:  runtimepolicy.SessionTransitionScheduleProbeUp,
		ExecutedTransition: runtimepolicy.SessionTransitionScheduleProbeUp,
		ActiveStep:         runtimepolicy.PlaybackStepH264720p,
		TargetStep:         runtimepolicy.PlaybackStepDirectCopy,
		ProbeStep:          runtimepolicy.PlaybackStepH2641080p,
		ProbeState:         runtimepolicy.ProbeLifecycleScheduled,
		Reasons:            []string{runtimepolicy.ReasonHeadroomGood},
	}})
	require.NoError(t, err)

	resp := mapSessionStateResponse("req-runtime-trace", "", v3sessions.GetSessionResult{
		Session: &model.SessionRecord{
			SessionID:          "550e8400-e29b-41d4-a716-446655440003",
			ServiceRef:         "1:0:1:445D:453:1:C00000:0:0:0:",
			Profile:            model.ProfileSpec{Name: profiles.ProfileLow},
			State:              model.SessionReady,
			CorrelationID:      "corr-runtime-trace",
			HeartbeatInterval:  30,
			LeaseExpiresAtUnix: 1700000030,
			ContextData: map[string]string{
				model.CtxKeyRuntimePolicyState:    string(statePayload),
				model.CtxKeyRuntimePolicyTimeline: string(timelinePayload),
			},
		},
		Outcome: lifecycle.PublicOutcome{
			State:      model.SessionReady,
			Reason:     model.RNone,
			DetailCode: model.DNone,
		},
		PlaybackInfo: v3sessions.SessionPlaybackInfo{
			Mode:       model.ModeLive,
			WindowKind: v3sessions.SessionWindowKindLive,
		},
	})

	require.NotNil(t, resp.Trace)
	require.NotNil(t, resp.Trace.Operator)
	require.NotNil(t, resp.Trace.Operator.RuntimePolicyAction)
	assert.Equal(t, "probe_up", *resp.Trace.Operator.RuntimePolicyAction)
	require.NotNil(t, resp.Trace.Operator.RuntimePolicyPhase)
	assert.Equal(t, "probing", *resp.Trace.Operator.RuntimePolicyPhase)
	require.NotNil(t, resp.Trace.Operator.RuntimePolicyTimeline)
	require.Len(t, *resp.Trace.Operator.RuntimePolicyTimeline, 1)
	tick := (*resp.Trace.Operator.RuntimePolicyTimeline)[0]
	require.NotNil(t, tick.PolicyAction)
	assert.Equal(t, "probe_up", *tick.PolicyAction)
	require.NotNil(t, tick.PlannedTransition)
	assert.Equal(t, "schedule_probe_up", *tick.PlannedTransition)
	require.NotNil(t, resp.Trace.Operator.RuntimePolicyReplay)
	require.NotNil(t, resp.Trace.Operator.RuntimePolicyReplay.Metadata)
	require.NotNil(t, resp.Trace.Operator.RuntimePolicyReplay.Metadata.SessionId)
	assert.Equal(t, "550e8400-e29b-41d4-a716-446655440003", *resp.Trace.Operator.RuntimePolicyReplay.Metadata.SessionId)
	require.NotNil(t, resp.Trace.Operator.RuntimePolicyReplay.Ticks)
	require.Len(t, *resp.Trace.Operator.RuntimePolicyReplay.Ticks, 1)
}

func TestBuildSessionRuntimePolicyReplay_FromTimeline(t *testing.T) {
	timelinePayload, err := json.Marshal([]runtimepolicy.TickTrace{{
		TickAt:                time.Unix(1700000000, 0).UTC(),
		ObservedStep:          runtimepolicy.PlaybackStepH2641080p,
		ConfidenceScore:       -44,
		ConfidenceState:       runtimepolicy.ConfidenceLow,
		ConfidenceWindowCount: 1,
		PolicyAction:          runtimepolicy.PolicyStepDown,
		PlannedTransition:     runtimepolicy.SessionTransitionScheduleStepDown,
		ExecutedTransition:    runtimepolicy.SessionTransitionScheduleStepDown,
		ActiveStep:            runtimepolicy.PlaybackStepH264720p,
		TargetStep:            runtimepolicy.PlaybackStepH2641080p,
		RuntimePhase:          "degraded",
		Reasons:               []string{runtimepolicy.ReasonBufferingRecent},
	}})
	require.NoError(t, err)

	replay := v3sessions.BuildSessionRuntimePolicyReplay(&model.SessionRecord{
		SessionID:  "550e8400-e29b-41d4-a716-446655440004",
		ServiceRef: "1:0:1:445D:453:1:C00000:0:0:0:",
		ContextData: map[string]string{
			model.CtxKeyClientPath:            "hlsjs",
			model.CtxKeySourceType:            "tuner",
			model.CtxKeyRuntimePolicyTimeline: string(timelinePayload),
		},
	})

	require.NotNil(t, replay)
	assert.Equal(t, "hlsjs", replay.Metadata.ClientPath)
	assert.Equal(t, runtimepolicy.PlaybackStepH2641080p, replay.InitialState.CurrentStep)
	require.Len(t, replay.Ticks, 1)
	assert.Equal(t, runtimepolicy.PolicyStepDown, replay.Ticks[0].Expected.Action)
	assert.Equal(t, runtimepolicy.SessionTransitionScheduleStepDown, replay.Ticks[0].Expected.PlannedTransition)
}

func float64Ptr(v float64) *float64 { return &v }
