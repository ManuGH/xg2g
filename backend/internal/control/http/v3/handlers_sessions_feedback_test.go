package v3

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
	"github.com/ManuGH/xg2g/internal/control/recordings/capreg"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	sessionstore "github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/ManuGH/xg2g/internal/pipeline/bus"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type feedbackStore struct {
	mu      sync.RWMutex
	session *model.SessionRecord
}

func (s *feedbackStore) ListSessions(ctx context.Context) ([]*model.SessionRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.session == nil {
		return nil, nil
	}
	cp := *s.session
	cp.PlaybackTrace = s.session.PlaybackTrace.Clone()
	return []*model.SessionRecord{&cp}, nil
}

func (s *feedbackStore) GetSession(ctx context.Context, id string) (*model.SessionRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.session == nil || s.session.SessionID != id {
		return nil, nil
	}
	cp := *s.session
	cp.PlaybackTrace = s.session.PlaybackTrace.Clone()
	return &cp, nil
}

func (s *feedbackStore) UpdateSession(ctx context.Context, id string, fn func(*model.SessionRecord) error) (*model.SessionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.session == nil || s.session.SessionID != id {
		return nil, sessionstore.ErrNotFound
	}
	cp := *s.session
	cp.PlaybackTrace = s.session.PlaybackTrace.Clone()
	if err := fn(&cp); err != nil {
		return nil, err
	}
	s.session = &cp
	return &cp, nil
}

func (s *feedbackStore) PutSessionWithIdempotency(ctx context.Context, rec *model.SessionRecord, idemKey string, ttl time.Duration) (string, bool, error) {
	return "", false, nil
}

func (s *feedbackStore) setState(state model.SessionState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.session != nil {
		s.session.State = state
	}
}

type publishedEvent struct {
	topic string
	msg   any
}

type feedbackBus struct {
	ch chan publishedEvent
}

func newFeedbackBus() *feedbackBus {
	return &feedbackBus{ch: make(chan publishedEvent, 4)}
}

func (b *feedbackBus) Publish(ctx context.Context, topic string, msg bus.Message) error {
	b.ch <- publishedEvent{topic: topic, msg: msg}
	return nil
}

func (b *feedbackBus) Subscribe(ctx context.Context, topic string) (bus.Subscriber, error) {
	return nil, nil
}

type feedbackRegistry struct {
	mu                  sync.Mutex
	decisionObservation capreg.PlaybackObservation
	recorded            []capreg.PlaybackObservation
}

func (r *feedbackRegistry) RememberHost(context.Context, capreg.HostSnapshot) error { return nil }

func (r *feedbackRegistry) RememberDevice(context.Context, capreg.DeviceSnapshot) error { return nil }

func (r *feedbackRegistry) RememberSource(context.Context, capreg.SourceSnapshot) error { return nil }

func (r *feedbackRegistry) LookupCapabilities(context.Context, capreg.DeviceIdentity) (capabilities.PlaybackCapabilities, bool, error) {
	return capabilities.PlaybackCapabilities{}, false, nil
}

func (r *feedbackRegistry) LookupDecisionObservation(_ context.Context, requestID string) (capreg.PlaybackObservation, bool, error) {
	if strings.TrimSpace(requestID) == strings.TrimSpace(r.decisionObservation.RequestID) {
		return r.decisionObservation, true, nil
	}
	return capreg.PlaybackObservation{}, false, nil
}

func (r *feedbackRegistry) RecordObservation(_ context.Context, observation capreg.PlaybackObservation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recorded = append(r.recorded, observation)
	return nil
}

func (r *feedbackRegistry) lastObservation() capreg.PlaybackObservation {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.recorded) == 0 {
		return capreg.PlaybackObservation{}
	}
	return r.recorded[len(r.recorded)-1]
}

func writeFirstFrameMarker(t *testing.T, hlsRoot, sid string) {
	t.Helper()
	markerPath := model.SessionFirstFrameMarkerPath(hlsRoot, sid)
	require.NotEmpty(t, markerPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(markerPath), 0o755))
	require.NoError(t, os.WriteFile(markerPath, []byte("ready"), 0o600))
}

func TestReportPlaybackFeedback_RecordsFeedbackObservation(t *testing.T) {
	sid := uuid.NewString()
	store := &feedbackStore{
		session: &model.SessionRecord{
			SessionID:     sid,
			ServiceRef:    "1:0:1:445D:453:1:C00000:0:0:0:",
			State:         model.SessionReady,
			CorrelationID: "corr-feedback-observation-001",
			Profile: model.ProfileSpec{
				Name:      "universal",
				Container: "ts",
			},
			ContextData: map[string]string{
				model.CtxKeyMode:            model.ModeLive,
				model.CtxKeyDecisionRequest: "decision-req-1",
			},
			PlaybackTrace: &model.PlaybackTrace{
				RequestedIntent: "quality",
				ResolvedIntent:  "compatible",
			},
		},
	}
	registry := &feedbackRegistry{
		decisionObservation: capreg.PlaybackObservation{
			RequestID:          "decision-req-1",
			ObservationKind:    "decision",
			Outcome:            "predicted",
			SourceRef:          "1:0:1:445D:453:1:C00000:0:0:0:",
			SourceFingerprint:  "source-fp-1",
			SubjectKind:        "live",
			RequestedIntent:    "quality",
			ResolvedIntent:     "compatible",
			Mode:               "direct_stream",
			SelectedContainer:  "ts",
			SelectedVideoCodec: "h264",
			SelectedAudioCodec: "ac3",
			SourceWidth:        1920,
			SourceHeight:       1080,
			SourceFPS:          50,
			HostFingerprint:    "host-fp-1",
			DeviceFingerprint:  "device-fp-1",
			ClientCapsHash:     "caps-hash-1",
		},
	}

	s := &Server{
		cfg:                config.AppConfig{HLS: config.HLSConfig{Root: t.TempDir()}},
		v3Store:            store,
		v3Bus:              newFeedbackBus(),
		capabilityRegistry: registry,
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v3/sessions/"+sid+"/feedback", strings.NewReader(`{"event":"info","code":200,"message":"playing"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	s.ReportPlaybackFeedback(rr, req, uuid.MustParse(sid))
	require.Equal(t, http.StatusAccepted, rr.Code)

	observed := registry.lastObservation()
	require.Equal(t, "decision-req-1", observed.RequestID)
	require.Equal(t, "feedback", observed.ObservationKind)
	require.Equal(t, "started", observed.Outcome)
	require.Equal(t, sid, observed.SessionID)
	require.Equal(t, "direct_stream", observed.Mode)
	require.Equal(t, "source-fp-1", observed.SourceFingerprint)
	require.Equal(t, "host-fp-1", observed.HostFingerprint)
	require.Equal(t, "device-fp-1", observed.DeviceFingerprint)
	require.Equal(t, "info", observed.FeedbackEvent)
	require.Equal(t, 200, observed.FeedbackCode)
	require.Equal(t, "playing", observed.FeedbackMessage)
}

func TestReportPlaybackFeedback_RecoveryInfoCountsAsStarted(t *testing.T) {
	sid := uuid.NewString()
	store := &feedbackStore{
		session: &model.SessionRecord{
			SessionID:     sid,
			ServiceRef:    "1:0:1:445D:453:1:C00000:0:0:0:",
			State:         model.SessionReady,
			CorrelationID: "corr-feedback-recovery-001",
			Profile: model.ProfileSpec{
				Name:      "universal",
				Container: "ts",
			},
			ContextData: map[string]string{
				model.CtxKeyMode:            model.ModeLive,
				model.CtxKeyDecisionRequest: "decision-req-recovery",
			},
			PlaybackTrace: &model.PlaybackTrace{
				RequestedIntent: "quality",
				ResolvedIntent:  "compatible",
			},
		},
	}
	registry := &feedbackRegistry{
		decisionObservation: capreg.PlaybackObservation{
			RequestID:          "decision-req-recovery",
			ObservationKind:    "decision",
			Outcome:            "predicted",
			SourceRef:          "1:0:1:445D:453:1:C00000:0:0:0:",
			SourceFingerprint:  "source-fp-recovery",
			SubjectKind:        "live",
			RequestedIntent:    "quality",
			ResolvedIntent:     "compatible",
			Mode:               "direct_stream",
			SelectedContainer:  "ts",
			SelectedVideoCodec: "h264",
			SelectedAudioCodec: "ac3",
			SourceWidth:        1920,
			SourceHeight:       1080,
			SourceFPS:          50,
			HostFingerprint:    "host-fp-recovery",
			DeviceFingerprint:  "device-fp-recovery",
			ClientCapsHash:     "caps-hash-recovery",
		},
	}

	s := &Server{
		cfg:                config.AppConfig{HLS: config.HLSConfig{Root: t.TempDir()}},
		v3Store:            store,
		v3Bus:              newFeedbackBus(),
		capabilityRegistry: registry,
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v3/sessions/"+sid+"/feedback", strings.NewReader(`{"event":"info","code":211,"message":"recovered_buffering"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	s.ReportPlaybackFeedback(rr, req, uuid.MustParse(sid))
	require.Equal(t, http.StatusAccepted, rr.Code)

	observed := registry.lastObservation()
	require.Equal(t, "started", observed.Outcome)
	require.Equal(t, "info", observed.FeedbackEvent)
	require.Equal(t, 211, observed.FeedbackCode)
	require.Equal(t, "recovered_buffering", observed.FeedbackMessage)
}

func TestReportPlaybackFeedback_ProbeInfoCountsAsStarted(t *testing.T) {
	sid := uuid.NewString()
	store := &feedbackStore{
		session: &model.SessionRecord{
			SessionID:     sid,
			ServiceRef:    "1:0:1:445D:453:1:C00000:0:0:0:",
			State:         model.SessionReady,
			CorrelationID: "corr-feedback-probe-001",
			Profile: model.ProfileSpec{
				Name:      "universal",
				Container: "ts",
			},
			ContextData: map[string]string{
				model.CtxKeyMode:            model.ModeLive,
				model.CtxKeyDecisionRequest: "decision-req-probe",
			},
			PlaybackTrace: &model.PlaybackTrace{
				RequestedIntent: "quality",
				ResolvedIntent:  "compatible",
			},
		},
	}
	registry := &feedbackRegistry{
		decisionObservation: capreg.PlaybackObservation{
			RequestID:          "decision-req-probe",
			ObservationKind:    "decision",
			Outcome:            "predicted",
			SourceRef:          "1:0:1:445D:453:1:C00000:0:0:0:",
			SourceFingerprint:  "source-fp-probe",
			SubjectKind:        "live",
			RequestedIntent:    "quality",
			ResolvedIntent:     "compatible",
			Mode:               "direct_stream",
			SelectedContainer:  "ts",
			SelectedVideoCodec: "h264",
			SelectedAudioCodec: "ac3",
			SourceWidth:        1920,
			SourceHeight:       1080,
			SourceFPS:          50,
			HostFingerprint:    "host-fp-probe",
			DeviceFingerprint:  "device-fp-probe",
			ClientCapsHash:     "caps-hash-probe",
		},
	}

	s := &Server{
		cfg:                config.AppConfig{HLS: config.HLSConfig{Root: t.TempDir()}},
		v3Store:            store,
		v3Bus:              newFeedbackBus(),
		capabilityRegistry: registry,
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v3/sessions/"+sid+"/feedback", strings.NewReader(`{"event":"info","code":220,"message":"probe_window_started"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	s.ReportPlaybackFeedback(rr, req, uuid.MustParse(sid))
	require.Equal(t, http.StatusAccepted, rr.Code)

	observed := registry.lastObservation()
	require.Equal(t, "started", observed.Outcome)
	require.Equal(t, "info", observed.FeedbackEvent)
	require.Equal(t, 220, observed.FeedbackCode)
	require.Equal(t, "probe_window_started", observed.FeedbackMessage)
}

func TestReportPlaybackFeedback_HLSJSBlackRenderInfoCountsAsWarning(t *testing.T) {
	sid := uuid.NewString()
	store := &feedbackStore{
		session: &model.SessionRecord{
			SessionID:     sid,
			ServiceRef:    "1:0:1:445D:453:1:C00000:0:0:0:",
			State:         model.SessionReady,
			CorrelationID: "corr-feedback-hls-black-001",
			Profile: model.ProfileSpec{
				Name:      "universal",
				Container: "ts",
			},
			ContextData: map[string]string{
				model.CtxKeyMode:            model.ModeLive,
				model.CtxKeyDecisionRequest: "decision-req-hls-black",
			},
			PlaybackTrace: &model.PlaybackTrace{
				RequestedIntent: "quality",
				ResolvedIntent:  "quality",
				ClientPath:      "hlsjs",
			},
		},
	}
	registry := &feedbackRegistry{
		decisionObservation: capreg.PlaybackObservation{
			RequestID:          "decision-req-hls-black",
			ObservationKind:    "decision",
			Outcome:            "predicted",
			SourceRef:          "1:0:1:445D:453:1:C00000:0:0:0:",
			SourceFingerprint:  "source-fp-hls-black",
			SubjectKind:        "live",
			RequestedIntent:    "quality",
			ResolvedIntent:     "quality",
			Mode:               "transcode",
			SelectedContainer:  "fmp4",
			SelectedVideoCodec: "av1",
			SelectedAudioCodec: "aac",
			HostFingerprint:    "host-fp-hls-black",
			DeviceFingerprint:  "device-fp-hls-black",
			ClientCapsHash:     "caps-hash-hls-black",
		},
	}

	s := &Server{
		cfg:                config.AppConfig{HLS: config.HLSConfig{Root: t.TempDir()}},
		v3Store:            store,
		v3Bus:              newFeedbackBus(),
		capabilityRegistry: registry,
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v3/sessions/"+sid+"/feedback", strings.NewReader(`{"event":"info","code":242,"message":"black_suspect"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	s.ReportPlaybackFeedback(rr, req, uuid.MustParse(sid))
	require.Equal(t, http.StatusAccepted, rr.Code)

	observed := registry.lastObservation()
	require.Equal(t, "feedback", observed.ObservationKind)
	require.Equal(t, "warning", observed.Outcome)
	require.Equal(t, "info", observed.FeedbackEvent)
	require.Equal(t, 242, observed.FeedbackCode)
	require.Equal(t, "black_suspect", observed.FeedbackMessage)
	require.Equal(t, "av1", observed.SelectedVideoCodec)
}

func TestReportPlaybackFeedback_IgnoresSoftStartupWarningDuringWarmup(t *testing.T) {
	sid := uuid.NewString()
	now := time.Now().UTC()
	store := &feedbackStore{
		session: &model.SessionRecord{
			SessionID:           sid,
			ServiceRef:          "1:0:1:445D:453:1:C00000:0:0:0:",
			State:               model.SessionReady,
			CreatedAtUnix:       now.Add(-30 * time.Second).Unix(),
			PlaylistPublishedAt: now.Add(-5 * time.Second),
			ContextData: map[string]string{
				model.CtxKeyMode:            model.ModeLive,
				model.CtxKeyDecisionRequest: "decision-req-startup-warning",
			},
			PlaybackTrace: &model.PlaybackTrace{
				RequestedIntent: "quality",
				ResolvedIntent:  "compatible",
			},
		},
	}
	registry := &feedbackRegistry{
		decisionObservation: capreg.PlaybackObservation{
			RequestID:         "decision-req-startup-warning",
			ObservationKind:   "decision",
			Outcome:           "predicted",
			SourceFingerprint: "source-fp-startup-warning",
			SubjectKind:       "live",
			HostFingerprint:   "host-fp-startup-warning",
			DeviceFingerprint: "device-fp-startup-warning",
		},
	}

	s := &Server{
		cfg:                config.AppConfig{HLS: config.HLSConfig{Root: t.TempDir()}},
		v3Store:            store,
		v3Bus:              newFeedbackBus(),
		capabilityRegistry: registry,
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v3/sessions/"+sid+"/feedback", strings.NewReader(`{"event":"warning","code":101,"message":"waiting"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	s.ReportPlaybackFeedback(rr, req, uuid.MustParse(sid))
	require.Equal(t, http.StatusAccepted, rr.Code)
	require.Len(t, registry.recorded, 0)
}

func TestReportPlaybackFeedback_WaitsForTerminalBeforeRestart(t *testing.T) {
	sid := uuid.NewString()
	hlsRoot := t.TempDir()
	store := &feedbackStore{
		session: &model.SessionRecord{
			SessionID:     sid,
			ServiceRef:    "1:0:1:445D:453:1:C00000:0:0:0:",
			State:         model.SessionReady,
			CorrelationID: "corr-feedback-001",
			Profile: model.ProfileSpec{
				Name:      "universal",
				Container: "ts",
			},
		},
	}
	eventBus := newFeedbackBus()

	writeFirstFrameMarker(t, hlsRoot, sid)

	s := &Server{cfg: config.AppConfig{HLS: config.HLSConfig{Root: hlsRoot}}, v3Store: store, v3Bus: eventBus}
	runtimeCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, s.SetRuntimeContext(runtimeCtx))

	req := httptest.NewRequest(http.MethodPost, "/api/v3/sessions/"+sid+"/feedback", strings.NewReader(`{"event":"error","code":3,"message":"bufferAppendError"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	s.ReportPlaybackFeedback(rr, req, uuid.MustParse(sid))
	require.Equal(t, http.StatusAccepted, rr.Code)

	select {
	case evt := <-eventBus.ch:
		require.Equal(t, string(model.EventStopSession), evt.topic)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected fallback stop event")
	}

	select {
	case evt := <-eventBus.ch:
		t.Fatalf("unexpected restart event before terminal state: %s", evt.topic)
	case <-time.After(150 * time.Millisecond):
	}

	updated, err := store.GetSession(context.Background(), sid)
	require.NoError(t, err)
	require.NotNil(t, updated)
	require.Equal(t, "repair", updated.Profile.Name)
	require.Equal(t, "mpegts", updated.Profile.Container)
	require.NotNil(t, updated.PlaybackTrace)
	require.Len(t, updated.PlaybackTrace.Fallbacks, 1)
	require.Equal(t, "client_feedback", updated.PlaybackTrace.Fallbacks[0].Trigger)
	require.Equal(t, "client_report:code=3", updated.PlaybackTrace.Fallbacks[0].Reason)
	require.Equal(t, "repair_fmp4", updated.PlaybackTrace.Fallbacks[0].PlanID)
	require.Equal(t, "default_repair_escalation", updated.PlaybackTrace.Fallbacks[0].PlanReason)
	require.NotEmpty(t, updated.PlaybackTrace.TargetProfileHash)

	store.setState(model.SessionStopped)

	select {
	case evt := <-eventBus.ch:
		require.Equal(t, string(model.EventStartSession), evt.topic)
	case <-time.After(time.Second):
		t.Fatal("expected fallback restart event after terminal state")
	}
}

func TestReportPlaybackFeedback_IOSAV1HlsStallFallsBackToRepair(t *testing.T) {
	sid := uuid.NewString()
	hlsRoot := t.TempDir()
	store := &feedbackStore{
		session: &model.SessionRecord{
			SessionID:     sid,
			ServiceRef:    "1:0:19:EF75:3F9:1:C00000:0:0:0:",
			State:         model.SessionReady,
			CorrelationID: "corr-feedback-ios-av1-stall-001",
			Profile: model.ProfileSpec{
				Name:       "av1_hw",
				Container:  "fmp4",
				VideoCodec: "av1",
			},
			ContextData: map[string]string{
				model.CtxKeyClientFamily: playbackprofile.ClientIOSSafariNative,
				model.CtxKeyClientPath:   "hlsjs",
			},
			PlaybackTrace: &model.PlaybackTrace{
				ClientPath: "hlsjs",
				Client: &model.PlaybackClientSnapshot{
					ClientFamily:       playbackprofile.ClientIOSSafariNative,
					PreferredHLSEngine: "hlsjs",
				},
				TargetProfile: &playbackprofile.TargetPlaybackProfile{
					Video: playbackprofile.VideoTarget{
						Codec: "av1",
					},
				},
				FFmpegPlan: &model.FFmpegPlanTrace{
					VideoCodec: "av1",
				},
				AutoCodecSelected: "av1",
			},
		},
	}
	eventBus := newFeedbackBus()

	writeFirstFrameMarker(t, hlsRoot, sid)

	s := &Server{cfg: config.AppConfig{HLS: config.HLSConfig{Root: hlsRoot}}, v3Store: store, v3Bus: eventBus}
	runtimeCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, s.SetRuntimeContext(runtimeCtx))

	req := httptest.NewRequest(http.MethodPost, "/api/v3/sessions/"+sid+"/feedback", strings.NewReader(`{"event":"error","code":4,"message":"hlsjs_stalled"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	s.ReportPlaybackFeedback(rr, req, uuid.MustParse(sid))
	require.Equal(t, http.StatusAccepted, rr.Code)

	select {
	case evt := <-eventBus.ch:
		require.Equal(t, string(model.EventStopSession), evt.topic)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected AV1 stall fallback stop event")
	}

	updated, err := store.GetSession(context.Background(), sid)
	require.NoError(t, err)
	require.NotNil(t, updated)
	require.Equal(t, model.SessionStarting, updated.State)
	require.Equal(t, profiles.ProfileRepair, updated.Profile.Name)
	require.Equal(t, "mpegts", updated.Profile.Container)
	require.Equal(t, "client_report:code=4", updated.FallbackReason)
	require.NotNil(t, updated.PlaybackTrace)
	require.Len(t, updated.PlaybackTrace.Fallbacks, 1)
	require.Equal(t, "client_feedback", updated.PlaybackTrace.Fallbacks[0].Trigger)
	require.Equal(t, "client_report:code=4", updated.PlaybackTrace.Fallbacks[0].Reason)
	require.Equal(t, "repair_fmp4", updated.PlaybackTrace.Fallbacks[0].PlanID)
	require.Equal(t, "default_repair_escalation", updated.PlaybackTrace.Fallbacks[0].PlanReason)

	store.setState(model.SessionStopped)

	select {
	case evt := <-eventBus.ch:
		require.Equal(t, string(model.EventStartSession), evt.topic)
	case <-time.After(time.Second):
		t.Fatal("expected AV1 stall fallback restart event after terminal state")
	}
}

func TestReportPlaybackFeedback_HlsStallIgnoredOutsideIOSAV1HlsjsPath(t *testing.T) {
	sid := uuid.NewString()
	hlsRoot := t.TempDir()
	store := &feedbackStore{
		session: &model.SessionRecord{
			SessionID:     sid,
			ServiceRef:    "1:0:19:EF75:3F9:1:C00000:0:0:0:",
			State:         model.SessionReady,
			CorrelationID: "corr-feedback-ios-av1-stall-002",
			Profile: model.ProfileSpec{
				Name:       "hevc_hw",
				Container:  "fmp4",
				VideoCodec: "hevc",
			},
			ContextData: map[string]string{
				model.CtxKeyClientFamily: playbackprofile.ClientIOSSafariNative,
				model.CtxKeyClientPath:   "hlsjs",
			},
			PlaybackTrace: &model.PlaybackTrace{
				ClientPath: "hlsjs",
				Client: &model.PlaybackClientSnapshot{
					ClientFamily: playbackprofile.ClientIOSSafariNative,
				},
				TargetProfile: &playbackprofile.TargetPlaybackProfile{
					Video: playbackprofile.VideoTarget{
						Codec: "hevc",
					},
				},
			},
		},
	}
	eventBus := newFeedbackBus()

	writeFirstFrameMarker(t, hlsRoot, sid)

	s := &Server{cfg: config.AppConfig{HLS: config.HLSConfig{Root: hlsRoot}}, v3Store: store, v3Bus: eventBus}

	req := httptest.NewRequest(http.MethodPost, "/api/v3/sessions/"+sid+"/feedback", strings.NewReader(`{"event":"error","code":4,"message":"hlsjs_stalled"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	s.ReportPlaybackFeedback(rr, req, uuid.MustParse(sid))
	require.Equal(t, http.StatusAccepted, rr.Code)

	select {
	case evt := <-eventBus.ch:
		t.Fatalf("unexpected fallback event for non-AV1 stall: %s", evt.topic)
	case <-time.After(150 * time.Millisecond):
	}

	updated, err := store.GetSession(context.Background(), sid)
	require.NoError(t, err)
	require.NotNil(t, updated)
	require.Equal(t, model.SessionReady, updated.State)
	require.Equal(t, "hevc_hw", updated.Profile.Name)
	require.Empty(t, updated.FallbackReason)
}

func TestReportPlaybackFeedback_SafariFallsBackToDirtyProfileBeforeRestart(t *testing.T) {
	sid := uuid.NewString()
	hlsRoot := t.TempDir()
	store := &feedbackStore{
		session: &model.SessionRecord{
			SessionID:     sid,
			ServiceRef:    "1:0:1:445D:453:1:C00000:0:0:0:",
			State:         model.SessionReady,
			CorrelationID: "corr-feedback-safari-001",
			Profile: model.ProfileSpec{
				Name:         profiles.ProfileSafari,
				Container:    "fmp4",
				DVRWindowSec: 2700,
			},
		},
	}
	eventBus := newFeedbackBus()

	writeFirstFrameMarker(t, hlsRoot, sid)

	s := &Server{cfg: config.AppConfig{HLS: config.HLSConfig{Root: hlsRoot}}, v3Store: store, v3Bus: eventBus}
	runtimeCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, s.SetRuntimeContext(runtimeCtx))

	req := httptest.NewRequest(http.MethodPost, "/api/v3/sessions/"+sid+"/feedback", strings.NewReader(`{"event":"error","code":3,"message":"mediaError"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	s.ReportPlaybackFeedback(rr, req, uuid.MustParse(sid))
	require.Equal(t, http.StatusAccepted, rr.Code)

	select {
	case evt := <-eventBus.ch:
		require.Equal(t, string(model.EventStopSession), evt.topic)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected safari fallback stop event")
	}

	select {
	case evt := <-eventBus.ch:
		t.Fatalf("unexpected restart event before terminal state: %s", evt.topic)
	case <-time.After(150 * time.Millisecond):
	}

	updated, err := store.GetSession(context.Background(), sid)
	require.NoError(t, err)
	require.NotNil(t, updated)
	require.Equal(t, model.SessionStarting, updated.State)
	require.Equal(t, model.PipeStopRequested, updated.PipelineState)
	require.Equal(t, profiles.ProfileSafariDirty, updated.Profile.Name)
	require.Equal(t, "fmp4", updated.Profile.Container)
	require.NotNil(t, updated.PlaybackTrace)
	require.Len(t, updated.PlaybackTrace.Fallbacks, 1)
	require.Equal(t, "client_feedback", updated.PlaybackTrace.Fallbacks[0].Trigger)
	require.Equal(t, "client_report:code=3", updated.PlaybackTrace.Fallbacks[0].Reason)
	require.Equal(t, "safari_dirty", updated.PlaybackTrace.Fallbacks[0].PlanID)
	require.Equal(t, "safari_general_first_failure", updated.PlaybackTrace.Fallbacks[0].PlanReason)
	require.NotEmpty(t, updated.PlaybackTrace.TargetProfileHash)

	store.setState(model.SessionStopped)

	select {
	case evt := <-eventBus.ch:
		require.Equal(t, string(model.EventStartSession), evt.topic)
	case <-time.After(time.Second):
		t.Fatal("expected safari fallback restart event after terminal state")
	}
}

func TestReportPlaybackFeedback_SafariForceCopyAllowlistFallsBackToBrowserTSBeforeRestart(t *testing.T) {
	t.Setenv("XG2G_SAFARI_FORCE_COPY_SERVICE_REFS", "1:0:19:11:6:85:C00000:0:0:0:")

	sid := uuid.NewString()
	hlsRoot := t.TempDir()
	store := &feedbackStore{
		session: &model.SessionRecord{
			SessionID:     sid,
			ServiceRef:    "1:0:19:11:6:85:C00000:0:0:0:",
			State:         model.SessionReady,
			CorrelationID: "corr-feedback-safari-force-copy-001",
			Profile: model.ProfileSpec{
				Name:         profiles.ProfileSafari,
				Container:    "fmp4",
				DVRWindowSec: 2700,
			},
		},
	}
	eventBus := newFeedbackBus()

	writeFirstFrameMarker(t, hlsRoot, sid)

	s := &Server{cfg: config.AppConfig{HLS: config.HLSConfig{Root: hlsRoot}}, v3Store: store, v3Bus: eventBus}
	runtimeCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, s.SetRuntimeContext(runtimeCtx))

	req := httptest.NewRequest(http.MethodPost, "/api/v3/sessions/"+sid+"/feedback", strings.NewReader(`{"event":"error","code":3,"message":"mediaError"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	s.ReportPlaybackFeedback(rr, req, uuid.MustParse(sid))
	require.Equal(t, http.StatusAccepted, rr.Code)

	select {
	case evt := <-eventBus.ch:
		require.Equal(t, string(model.EventStopSession), evt.topic)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected safari fallback stop event")
	}

	select {
	case evt := <-eventBus.ch:
		t.Fatalf("unexpected restart event before terminal state: %s", evt.topic)
	case <-time.After(150 * time.Millisecond):
	}

	updated, err := store.GetSession(context.Background(), sid)
	require.NoError(t, err)
	require.NotNil(t, updated)
	require.Equal(t, model.SessionStarting, updated.State)
	require.Equal(t, model.PipeStopRequested, updated.PipelineState)
	require.Equal(t, profiles.ProfileSafari, updated.Profile.Name)
	require.Equal(t, "mpegts", updated.Profile.Container)
	require.True(t, updated.Profile.DisableSafariForceCopy)
	require.True(t, updated.Profile.TranscodeVideo)
	require.True(t, updated.Profile.Deinterlace)
	require.NotNil(t, updated.PlaybackTrace)
	require.Len(t, updated.PlaybackTrace.Fallbacks, 1)
	require.Equal(t, "client_feedback", updated.PlaybackTrace.Fallbacks[0].Trigger)
	require.Equal(t, "client_report:code=3", updated.PlaybackTrace.Fallbacks[0].Reason)
	require.Equal(t, "safari_browser_ts", updated.PlaybackTrace.Fallbacks[0].PlanID)
	require.Equal(t, "safari_force_copy_allowlist_first_failure", updated.PlaybackTrace.Fallbacks[0].PlanReason)
	require.NotEmpty(t, updated.PlaybackTrace.TargetProfileHash)

	store.setState(model.SessionStopped)

	select {
	case evt := <-eventBus.ch:
		require.Equal(t, string(model.EventStartSession), evt.topic)
	case <-time.After(time.Second):
		t.Fatal("expected safari fallback restart event after terminal state")
	}
}

func TestReportPlaybackFeedback_SafariForceCopyAllowlistEscalatesToRepairAfterTSFallbackReFails(t *testing.T) {
	t.Setenv("XG2G_SAFARI_FORCE_COPY_SERVICE_REFS", "1:0:19:11:6:85:C00000:0:0:0:")

	sid := uuid.NewString()
	hlsRoot := t.TempDir()
	store := &feedbackStore{
		session: &model.SessionRecord{
			SessionID:     sid,
			ServiceRef:    "1:0:19:11:6:85:C00000:0:0:0:",
			State:         model.SessionReady,
			CorrelationID: "corr-feedback-safari-force-copy-002",
			Profile: model.ProfileSpec{
				Name:                   profiles.ProfileSafari,
				Container:              "mpegts",
				DVRWindowSec:           2700,
				DisableSafariForceCopy: true,
				TranscodeVideo:         true,
				Deinterlace:            true,
				VideoCodec:             "libx264",
				VideoCRF:               20,
				VideoMaxRateK:          8000,
				VideoBufSizeK:          16000,
				AudioBitrateK:          192,
				Preset:                 "veryfast",
			},
		},
	}
	eventBus := newFeedbackBus()

	writeFirstFrameMarker(t, hlsRoot, sid)

	s := &Server{cfg: config.AppConfig{HLS: config.HLSConfig{Root: hlsRoot}}, v3Store: store, v3Bus: eventBus}
	runtimeCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, s.SetRuntimeContext(runtimeCtx))

	req := httptest.NewRequest(http.MethodPost, "/api/v3/sessions/"+sid+"/feedback", strings.NewReader(`{"event":"error","code":3,"message":"mediaError"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	s.ReportPlaybackFeedback(rr, req, uuid.MustParse(sid))
	require.Equal(t, http.StatusAccepted, rr.Code)

	select {
	case evt := <-eventBus.ch:
		require.Equal(t, string(model.EventStopSession), evt.topic)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected safari fallback stop event")
	}

	select {
	case evt := <-eventBus.ch:
		t.Fatalf("unexpected restart event before terminal state: %s", evt.topic)
	case <-time.After(150 * time.Millisecond):
	}

	updated, err := store.GetSession(context.Background(), sid)
	require.NoError(t, err)
	require.NotNil(t, updated)
	require.Equal(t, model.SessionStarting, updated.State)
	require.Equal(t, model.PipeStopRequested, updated.PipelineState)
	require.Equal(t, profiles.ProfileRepair, updated.Profile.Name)
	require.Equal(t, "mpegts", updated.Profile.Container)
	require.True(t, updated.Profile.TranscodeVideo)
	require.True(t, updated.Profile.Deinterlace)
	require.Equal(t, "libx264", updated.Profile.VideoCodec)
	require.Equal(t, 24, updated.Profile.VideoCRF)
	require.Equal(t, 1280, updated.Profile.VideoMaxWidth)
	require.Equal(t, "veryfast", updated.Profile.Preset)
	require.NotNil(t, updated.PlaybackTrace)
	require.Len(t, updated.PlaybackTrace.Fallbacks, 1)
	require.Equal(t, "client_feedback", updated.PlaybackTrace.Fallbacks[0].Trigger)
	require.Equal(t, "client_report:code=3", updated.PlaybackTrace.Fallbacks[0].Reason)
	require.Equal(t, "safari_repair_ts", updated.PlaybackTrace.Fallbacks[0].PlanID)
	require.Equal(t, "safari_force_copy_allowlist_repeat_failure", updated.PlaybackTrace.Fallbacks[0].PlanReason)
	require.NotEmpty(t, updated.PlaybackTrace.TargetProfileHash)
	require.NotEqual(t, updated.PlaybackTrace.Fallbacks[0].FromProfileHash, updated.PlaybackTrace.Fallbacks[0].ToProfileHash)

	store.setState(model.SessionStopped)

	select {
	case evt := <-eventBus.ch:
		require.Equal(t, string(model.EventStartSession), evt.topic)
	case <-time.After(time.Second):
		t.Fatal("expected safari fallback restart event after terminal state")
	}
}

func TestReportPlaybackFeedback_IgnoresFallbackBeforeFirstFrame(t *testing.T) {
	sid := uuid.NewString()
	hlsRoot := t.TempDir()
	store := &feedbackStore{
		session: &model.SessionRecord{
			SessionID:     sid,
			ServiceRef:    "1:0:1:445D:453:1:C00000:0:0:0:",
			State:         model.SessionReady,
			CorrelationID: "corr-feedback-no-frame-001",
			Profile: model.ProfileSpec{
				Name:      "universal",
				Container: "ts",
			},
		},
	}
	eventBus := newFeedbackBus()

	s := &Server{cfg: config.AppConfig{HLS: config.HLSConfig{Root: hlsRoot}}, v3Store: store, v3Bus: eventBus}

	req := httptest.NewRequest(http.MethodPost, "/api/v3/sessions/"+sid+"/feedback", strings.NewReader(`{"event":"error","code":3,"message":"bufferAppendError"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	s.ReportPlaybackFeedback(rr, req, uuid.MustParse(sid))
	require.Equal(t, http.StatusAccepted, rr.Code)

	select {
	case evt := <-eventBus.ch:
		t.Fatalf("unexpected fallback event without first-frame marker: %s", evt.topic)
	case <-time.After(150 * time.Millisecond):
	}

	updated, err := store.GetSession(context.Background(), sid)
	require.NoError(t, err)
	require.NotNil(t, updated)
	require.Equal(t, "universal", updated.Profile.Name)
	require.Empty(t, updated.FallbackReason)
}

func TestReportPlaybackFeedback_DisableClientFallbackSkipsRestart(t *testing.T) {
	sid := uuid.NewString()
	hlsRoot := t.TempDir()
	store := &feedbackStore{
		session: &model.SessionRecord{
			SessionID:     sid,
			ServiceRef:    "1:0:1:445D:453:1:C00000:0:0:0:",
			State:         model.SessionReady,
			CorrelationID: "corr-feedback-disabled-001",
			Profile: model.ProfileSpec{
				Name:      profiles.ProfileSafari,
				Container: "fmp4",
			},
			PlaybackTrace: &model.PlaybackTrace{
				Operator: &model.PlaybackOperatorTrace{
					ForcedIntent:           "repair",
					MaxQualityRung:         "repair_audio_aac_192_stereo",
					ClientFallbackDisabled: true,
					OverrideApplied:        true,
				},
			},
		},
	}
	eventBus := newFeedbackBus()

	writeFirstFrameMarker(t, hlsRoot, sid)

	s := &Server{
		cfg:     config.AppConfig{HLS: config.HLSConfig{Root: hlsRoot}},
		v3Store: store,
		v3Bus:   eventBus,
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v3/sessions/"+sid+"/feedback", strings.NewReader(`{"event":"error","code":3,"message":"mediaError"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	s.ReportPlaybackFeedback(rr, req, uuid.MustParse(sid))
	require.Equal(t, http.StatusAccepted, rr.Code)

	select {
	case evt := <-eventBus.ch:
		t.Fatalf("unexpected fallback event while disabled: %s", evt.topic)
	case <-time.After(150 * time.Millisecond):
	}

	updated, err := store.GetSession(context.Background(), sid)
	require.NoError(t, err)
	require.NotNil(t, updated)
	require.Equal(t, profiles.ProfileSafari, updated.Profile.Name)
	require.NotNil(t, updated.PlaybackTrace)
	require.NotNil(t, updated.PlaybackTrace.Operator)
	require.True(t, updated.PlaybackTrace.Operator.ClientFallbackDisabled)
	require.True(t, updated.PlaybackTrace.Operator.OverrideApplied)
	require.Equal(t, "repair", updated.PlaybackTrace.Operator.ForcedIntent)
	require.Equal(t, "repair_audio_aac_192_stereo", updated.PlaybackTrace.Operator.MaxQualityRung)
}
