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

func writeFirstFrameMarker(t *testing.T, hlsRoot, sid string) {
	t.Helper()
	markerPath := model.SessionFirstFrameMarkerPath(hlsRoot, sid)
	require.NotEmpty(t, markerPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(markerPath), 0o755))
	require.NoError(t, os.WriteFile(markerPath, []byte("ready"), 0o600))
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
