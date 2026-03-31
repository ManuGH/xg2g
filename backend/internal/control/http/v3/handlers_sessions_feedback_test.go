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
	require.Equal(t, "fmp4", updated.Profile.Container)
	require.NotNil(t, updated.PlaybackTrace)
	require.Len(t, updated.PlaybackTrace.Fallbacks, 1)
	require.Equal(t, "client_feedback", updated.PlaybackTrace.Fallbacks[0].Trigger)
	require.Equal(t, "client_report:code=3", updated.PlaybackTrace.Fallbacks[0].Reason)
	require.NotEmpty(t, updated.PlaybackTrace.TargetProfileHash)

	store.setState(model.SessionStopped)

	select {
	case evt := <-eventBus.ch:
		require.Equal(t, string(model.EventStartSession), evt.topic)
	case <-time.After(time.Second):
		t.Fatal("expected fallback restart event after terminal state")
	}
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
	require.Equal(t, profiles.ProfileSafariDirty, updated.Profile.Name)
	require.Equal(t, "fmp4", updated.Profile.Container)
	require.NotNil(t, updated.PlaybackTrace)
	require.Len(t, updated.PlaybackTrace.Fallbacks, 1)
	require.Equal(t, "client_feedback", updated.PlaybackTrace.Fallbacks[0].Trigger)
	require.Equal(t, "client_report:code=3", updated.PlaybackTrace.Fallbacks[0].Reason)
	require.NotEmpty(t, updated.PlaybackTrace.TargetProfileHash)

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
