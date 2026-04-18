package v3

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/recordings/capreg"
	"github.com/ManuGH/xg2g/internal/control/recordings/runtimepolicy"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
	"github.com/stretchr/testify/require"
)

func TestSessionHeartbeat_RuntimeStepDownEnforcesRestart(t *testing.T) {
	registry := capreg.NewMemoryStore()
	store := &feedbackStore{}
	eventBus := newFeedbackBus()

	s := &Server{
		cfg: config.AppConfig{
			HLS: config.HLSConfig{Root: t.TempDir()},
		},
		v3Store:            store,
		v3Bus:              eventBus,
		capabilityRegistry: registry,
	}

	sessionID := "550e8400-e29b-41d4-a716-446655440101"
	now := time.Now().UTC()
	reqID := "req-runtime-enforce-001"

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

	store.session = &model.SessionRecord{
		SessionID:          sessionID,
		State:              model.SessionReady,
		PipelineState:      model.PipeServing,
		ServiceRef:         "1:0:1:445D:453:1:C00000:0:0:0:",
		CorrelationID:      "corr-runtime-enforce-001",
		HeartbeatInterval:  30,
		LeaseExpiresAtUnix: now.Add(30 * time.Second).Unix(),
		LastHeartbeatUnix:  now.Add(-31 * time.Second).Unix(),
		Profile: model.ProfileSpec{
			Name:                 profiles.ProfileHigh,
			PolicyModeHint:       ports.RuntimeModeHQ25,
			EffectiveRuntimeMode: ports.RuntimeModeHQ25,
			EffectiveModeSource:  ports.RuntimeModeSourceResolve,
			TranscodeVideo:       true,
			VideoCodec:           "libx264",
			VideoCRF:             20,
			VideoMaxWidth:        1920,
			AudioBitrateK:        192,
			Preset:               "slow",
			Container:            "fmp4",
		},
		ContextData: map[string]string{
			model.CtxKeyDecisionRequest:   reqID,
			model.CtxKeyRuntimeTargetStep: string(runtimepolicy.PlaybackStepH2641080p),
			model.CtxKeySourceType:        "tuner",
		},
		PlaybackTrace: &model.PlaybackTrace{
			RequestProfile:      "quality",
			RequestedIntent:     "quality",
			ResolvedIntent:      "quality",
			VideoQualityRung:    string(playbackprofile.RungQualityVideoH264CRF20),
			QualityRung:         string(playbackprofile.RungQualityVideoH264CRF20),
			EffectiveModeSource: ports.RuntimeModeSourceResolve,
			TargetProfile: &playbackprofile.TargetPlaybackProfile{
				Container: "fmp4",
				Packaging: playbackprofile.PackagingFMP4,
				Video: playbackprofile.VideoTarget{
					Mode:   playbackprofile.MediaModeTranscode,
					Codec:  "h264",
					Width:  1920,
					CRF:    20,
					Preset: "slow",
				},
				Audio: playbackprofile.AudioTarget{
					Mode:        playbackprofile.MediaModeTranscode,
					Codec:       "aac",
					BitrateKbps: 192,
				},
			},
		},
	}

	req := httptest.NewRequest(http.MethodPost, V3BaseURL+"/sessions/"+sessionID+"/heartbeat", nil)
	rr := httptest.NewRecorder()

	s.handleSessionHeartbeat(rr, req, sessionID)
	require.Equal(t, http.StatusOK, rr.Code)

	updated, err := store.GetSession(context.Background(), sessionID)
	require.NoError(t, err)
	require.NotNil(t, updated)
	require.Equal(t, model.SessionStarting, updated.State)
	require.Equal(t, model.PipeStopRequested, updated.PipelineState)
	require.Equal(t, profiles.ProfileLow, updated.Profile.Name)
	require.Equal(t, ports.RuntimeModeSourceRuntimeHardening, updated.Profile.EffectiveModeSource)
	require.Equal(t, string(runtimepolicy.PolicyStepDown), updated.ContextData[model.CtxKeyRuntimePolicyAction])
	require.Equal(t, string(runtimepolicy.PlaybackStepH264720p), updated.ContextData[model.CtxKeyRuntimeCurrentStep])
	require.NotNil(t, updated.PlaybackTrace)
	require.Len(t, updated.PlaybackTrace.Fallbacks, 1)
	require.Equal(t, "runtime_policy", updated.PlaybackTrace.Fallbacks[0].Trigger)
	require.Contains(t, updated.PlaybackTrace.Fallbacks[0].Reason, "schedule_step_down")
	require.NotEmpty(t, updated.PlaybackTrace.TargetProfileHash)

	stop := waitPublishedEvent(t, eventBus.ch)
	require.Equal(t, string(model.EventStopSession), stop.topic)

	store.mu.Lock()
	store.session.State = model.SessionStopped
	store.mu.Unlock()

	start := waitPublishedEvent(t, eventBus.ch)
	require.Equal(t, string(model.EventStartSession), start.topic)
	restart, ok := start.msg.(model.StartSessionEvent)
	require.True(t, ok)
	require.Equal(t, sessionID, restart.SessionID)
	require.Equal(t, profiles.ProfileLow, restart.ProfileID)
}

func TestSessionHeartbeat_RuntimeProbeUpConfirmsWithoutRestart(t *testing.T) {
	registry := capreg.NewMemoryStore()
	store := &feedbackStore{}
	eventBus := newFeedbackBus()

	s := &Server{
		cfg: config.AppConfig{
			HLS: config.HLSConfig{Root: t.TempDir()},
		},
		v3Store:            store,
		v3Bus:              eventBus,
		capabilityRegistry: registry,
	}

	sessionID := "550e8400-e29b-41d4-a716-446655440102"
	now := time.Now().UTC()
	reqID := "req-runtime-probe-confirm-001"

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
		MaxQualityRung:    playbackprofile.RungCompatibleVideoH264CRF23,
		Confidence: runtimepolicy.ConfidenceSnapshot{
			Score:       72,
			State:       runtimepolicy.ConfidenceHigh,
			StateSince:  now.Add(-20 * time.Second),
			WindowCount: 4,
			Reasons:     []string{runtimepolicy.ReasonHeadroomGood, runtimepolicy.ReasonCleanPlaybackWindow},
		},
		UpdatedAt: now,
	}))

	store.session = &model.SessionRecord{
		SessionID:          sessionID,
		State:              model.SessionReady,
		PipelineState:      model.PipeServing,
		ServiceRef:         "1:0:1:445D:453:1:C00000:0:0:0:",
		CorrelationID:      "corr-runtime-probe-confirm-001",
		HeartbeatInterval:  30,
		LeaseExpiresAtUnix: now.Add(30 * time.Second).Unix(),
		LastHeartbeatUnix:  now.Add(-31 * time.Second).Unix(),
		Profile: model.ProfileSpec{
			Name:                 profiles.ProfileLow,
			PolicyModeHint:       ports.RuntimeModeHQ25,
			EffectiveRuntimeMode: ports.RuntimeModeHQ25,
			EffectiveModeSource:  ports.RuntimeModeSourceResolve,
			TranscodeVideo:       true,
			VideoCodec:           "libx264",
			VideoCRF:             26,
			VideoMaxWidth:        1280,
			AudioBitrateK:        160,
			Preset:               "fast",
			Container:            "fmp4",
		},
		ContextData: map[string]string{
			model.CtxKeyDecisionRequest:   reqID,
			model.CtxKeyRuntimeTargetStep: string(runtimepolicy.PlaybackStepDirectCopy),
			model.CtxKeySourceType:        "tuner",
		},
		PlaybackTrace: &model.PlaybackTrace{
			RequestProfile:      "compatible",
			RequestedIntent:     "compatible",
			ResolvedIntent:      "compatible",
			VideoQualityRung:    string(playbackprofile.RungCompatibleVideoH264CRF23),
			QualityRung:         string(playbackprofile.RungCompatibleVideoH264CRF23),
			EffectiveModeSource: ports.RuntimeModeSourceResolve,
			TargetProfile: &playbackprofile.TargetPlaybackProfile{
				Container: "fmp4",
				Packaging: playbackprofile.PackagingFMP4,
				Video: playbackprofile.VideoTarget{
					Mode:   playbackprofile.MediaModeTranscode,
					Codec:  "h264",
					Width:  1280,
					CRF:    23,
					Preset: "fast",
				},
				Audio: playbackprofile.AudioTarget{
					Mode:        playbackprofile.MediaModeTranscode,
					Codec:       "aac",
					BitrateKbps: 160,
				},
			},
		},
	}

	req := httptest.NewRequest(http.MethodPost, V3BaseURL+"/sessions/"+sessionID+"/heartbeat", nil)
	rr := httptest.NewRecorder()
	s.handleSessionHeartbeat(rr, req, sessionID)
	require.Equal(t, http.StatusOK, rr.Code)

	stop := waitPublishedEvent(t, eventBus.ch)
	require.Equal(t, string(model.EventStopSession), stop.topic)

	store.mu.Lock()
	store.session.State = model.SessionStopped
	store.mu.Unlock()

	start := waitPublishedEvent(t, eventBus.ch)
	require.Equal(t, string(model.EventStartSession), start.topic)

	store.mu.Lock()
	store.session.State = model.SessionReady
	store.session.PipelineState = model.PipeServing
	store.session.LastHeartbeatUnix = time.Now().Add(-31 * time.Second).Unix()
	ageSessionRuntimeTick(store.session, 3*time.Second)
	store.mu.Unlock()

	require.NoError(t, registry.RememberPlaybackPolicyState(context.Background(), capreg.PlaybackPolicyState{
		SubjectKind:       "live",
		SourceFingerprint: "source-fp-1",
		DeviceFingerprint: "device-fp-1",
		HostFingerprint:   "host-fp-1",
		MaxQualityRung:    playbackprofile.RungCompatibleVideoH264CRF23,
		Confidence: runtimepolicy.ConfidenceSnapshot{
			Score:       78,
			State:       runtimepolicy.ConfidenceHigh,
			StateSince:  now.Add(-10 * time.Second),
			WindowCount: 5,
			Reasons:     []string{runtimepolicy.ReasonProbeWindowConfirmed},
		},
		UpdatedAt: time.Now().UTC(),
	}))

	rr = httptest.NewRecorder()
	s.handleSessionHeartbeat(rr, req, sessionID)
	require.Equal(t, http.StatusOK, rr.Code)

	updated, err := store.GetSession(context.Background(), sessionID)
	require.NoError(t, err)
	require.NotNil(t, updated)
	require.Equal(t, model.SessionReady, updated.State)
	require.Equal(t, string(runtimepolicy.PlaybackStepH2641080p), updated.ContextData[model.CtxKeyRuntimeCurrentStep])
	require.Equal(t, "", updated.ContextData[model.CtxKeyRuntimeProbeStep])
	require.Equal(t, string(runtimepolicy.ProbeLifecycleConfirmed), updated.ContextData[model.CtxKeyRuntimeProbeState])

	select {
	case evt := <-eventBus.ch:
		t.Fatalf("expected no additional restart event on probe confirm, got %#v", evt)
	default:
	}
}

func TestSessionHeartbeat_RuntimeProbeUpRegressionRevertsRestart(t *testing.T) {
	registry := capreg.NewMemoryStore()
	store := &feedbackStore{}
	eventBus := newFeedbackBus()

	s := &Server{
		cfg: config.AppConfig{
			HLS: config.HLSConfig{Root: t.TempDir()},
		},
		v3Store:            store,
		v3Bus:              eventBus,
		capabilityRegistry: registry,
	}

	sessionID := "550e8400-e29b-41d4-a716-446655440103"
	now := time.Now().UTC()
	reqID := "req-runtime-probe-abort-001"

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
		MaxQualityRung:    playbackprofile.RungCompatibleVideoH264CRF23,
		Confidence: runtimepolicy.ConfidenceSnapshot{
			Score:       72,
			State:       runtimepolicy.ConfidenceHigh,
			StateSince:  now.Add(-20 * time.Second),
			WindowCount: 4,
			Reasons:     []string{runtimepolicy.ReasonHeadroomGood, runtimepolicy.ReasonCleanPlaybackWindow},
		},
		UpdatedAt: now,
	}))

	store.session = &model.SessionRecord{
		SessionID:          sessionID,
		State:              model.SessionReady,
		PipelineState:      model.PipeServing,
		ServiceRef:         "1:0:1:445D:453:1:C00000:0:0:0:",
		CorrelationID:      "corr-runtime-probe-abort-001",
		HeartbeatInterval:  30,
		LeaseExpiresAtUnix: now.Add(30 * time.Second).Unix(),
		LastHeartbeatUnix:  now.Add(-31 * time.Second).Unix(),
		Profile: model.ProfileSpec{
			Name:                 profiles.ProfileLow,
			PolicyModeHint:       ports.RuntimeModeHQ25,
			EffectiveRuntimeMode: ports.RuntimeModeHQ25,
			EffectiveModeSource:  ports.RuntimeModeSourceResolve,
			TranscodeVideo:       true,
			VideoCodec:           "libx264",
			VideoCRF:             26,
			VideoMaxWidth:        1280,
			AudioBitrateK:        160,
			Preset:               "fast",
			Container:            "fmp4",
		},
		ContextData: map[string]string{
			model.CtxKeyDecisionRequest:   reqID,
			model.CtxKeyRuntimeTargetStep: string(runtimepolicy.PlaybackStepDirectCopy),
			model.CtxKeySourceType:        "tuner",
		},
		PlaybackTrace: &model.PlaybackTrace{
			RequestProfile:      "compatible",
			RequestedIntent:     "compatible",
			ResolvedIntent:      "compatible",
			VideoQualityRung:    string(playbackprofile.RungCompatibleVideoH264CRF23),
			QualityRung:         string(playbackprofile.RungCompatibleVideoH264CRF23),
			EffectiveModeSource: ports.RuntimeModeSourceResolve,
			TargetProfile: &playbackprofile.TargetPlaybackProfile{
				Container: "fmp4",
				Packaging: playbackprofile.PackagingFMP4,
				Video: playbackprofile.VideoTarget{
					Mode:   playbackprofile.MediaModeTranscode,
					Codec:  "h264",
					Width:  1280,
					CRF:    23,
					Preset: "fast",
				},
				Audio: playbackprofile.AudioTarget{
					Mode:        playbackprofile.MediaModeTranscode,
					Codec:       "aac",
					BitrateKbps: 160,
				},
			},
		},
	}

	req := httptest.NewRequest(http.MethodPost, V3BaseURL+"/sessions/"+sessionID+"/heartbeat", nil)
	rr := httptest.NewRecorder()
	s.handleSessionHeartbeat(rr, req, sessionID)
	require.Equal(t, http.StatusOK, rr.Code)

	_ = waitPublishedEvent(t, eventBus.ch)
	store.mu.Lock()
	store.session.State = model.SessionStopped
	store.mu.Unlock()
	_ = waitPublishedEvent(t, eventBus.ch)

	store.mu.Lock()
	store.session.State = model.SessionReady
	store.session.PipelineState = model.PipeServing
	store.session.LastHeartbeatUnix = time.Now().Add(-31 * time.Second).Unix()
	ageSessionRuntimeTick(store.session, 3*time.Second)
	store.mu.Unlock()

	require.NoError(t, registry.RememberPlaybackPolicyState(context.Background(), capreg.PlaybackPolicyState{
		SubjectKind:       "live",
		SourceFingerprint: "source-fp-1",
		DeviceFingerprint: "device-fp-1",
		HostFingerprint:   "host-fp-1",
		MaxQualityRung:    playbackprofile.RungCompatibleVideoH264CRF23,
		Confidence: runtimepolicy.ConfidenceSnapshot{
			Score:       18,
			State:       runtimepolicy.ConfidenceRecovery,
			StateSince:  now.Add(-5 * time.Second),
			WindowCount: 5,
			Reasons:     []string{runtimepolicy.ReasonProbeWindowRegressed},
		},
		UpdatedAt: time.Now().UTC(),
	}))

	rr = httptest.NewRecorder()
	s.handleSessionHeartbeat(rr, req, sessionID)
	require.Equal(t, http.StatusOK, rr.Code)

	updated, err := store.GetSession(context.Background(), sessionID)
	require.NoError(t, err)
	require.NotNil(t, updated)
	require.Equal(t, model.SessionStarting, updated.State)
	require.Equal(t, model.PipeStopRequested, updated.PipelineState)
	require.Equal(t, profiles.ProfileLow, updated.Profile.Name)
	require.Equal(t, string(runtimepolicy.ProbeLifecycleAborted), updated.ContextData[model.CtxKeyRuntimeProbeState])
	require.Equal(t, "", updated.ContextData[model.CtxKeyRuntimeProbeStep])

	stop := waitPublishedEvent(t, eventBus.ch)
	require.Equal(t, string(model.EventStopSession), stop.topic)

	store.mu.Lock()
	store.session.State = model.SessionStopped
	store.mu.Unlock()

	start := waitPublishedEvent(t, eventBus.ch)
	require.Equal(t, string(model.EventStartSession), start.topic)
}

func waitPublishedEvent(t *testing.T, ch <-chan publishedEvent) publishedEvent {
	t.Helper()
	select {
	case evt := <-ch:
		return evt
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for published event")
		return publishedEvent{}
	}
}

func ageSessionRuntimeTick(session *model.SessionRecord, delta time.Duration) {
	if session == nil || session.ContextData == nil {
		return
	}
	raw := session.ContextData[model.CtxKeyRuntimePolicyState]
	if raw == "" {
		return
	}
	var state runtimepolicy.SessionLoopState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return
	}
	state.LastTickAt = time.Now().Add(-delta).UTC()
	payload, err := json.Marshal(state)
	if err != nil {
		return
	}
	session.ContextData[model.CtxKeyRuntimePolicyState] = string(payload)
}
