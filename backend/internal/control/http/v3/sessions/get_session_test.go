package sessions

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/problemcode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeDeps struct {
	store SessionStore
}

func (d fakeDeps) SessionStore() SessionStore {
	return d.store
}

type fakeStore struct {
	session *model.SessionRecord
	err     error
}

func (s fakeStore) GetSession(ctx context.Context, id string) (*model.SessionRecord, error) {
	return s.session, s.err
}

func TestServiceGetSessionUnavailable(t *testing.T) {
	svc := NewService(fakeDeps{})

	_, err := svc.GetSession(context.Background(), GetSessionRequest{SessionID: "550e8400-e29b-41d4-a716-446655440001"})

	require.NotNil(t, err)
	assert.Equal(t, GetSessionErrorUnavailable, err.Kind)
	assert.Equal(t, "session store is not initialized", err.Message)
}

func TestServiceGetSessionInvalidInput(t *testing.T) {
	svc := NewService(fakeDeps{store: fakeStore{}})

	_, err := svc.GetSession(context.Background(), GetSessionRequest{SessionID: "../bad"})

	require.NotNil(t, err)
	assert.Equal(t, GetSessionErrorInvalidInput, err.Kind)
	assert.Equal(t, "invalid session id", err.Message)
}

func TestServiceGetSessionNotFound(t *testing.T) {
	svc := NewService(fakeDeps{store: fakeStore{err: errors.New("boom")}})

	_, err := svc.GetSession(context.Background(), GetSessionRequest{SessionID: "550e8400-e29b-41d4-a716-446655440001"})

	require.NotNil(t, err)
	assert.Equal(t, GetSessionErrorNotFound, err.Kind)
	assert.Equal(t, "session not found", err.Message)
	assert.Error(t, err.Cause)
}

func TestServiceGetSessionTerminal(t *testing.T) {
	svc := NewService(fakeDeps{store: fakeStore{session: &model.SessionRecord{
		SessionID:        "550e8400-e29b-41d4-a716-446655440001",
		State:            model.SessionFailed,
		Reason:           model.RProcessEnded,
		ReasonDetailCode: model.DTranscodeStalled,
	}}})

	_, err := svc.GetSession(context.Background(), GetSessionRequest{SessionID: "550e8400-e29b-41d4-a716-446655440001"})

	require.NotNil(t, err)
	assert.Equal(t, GetSessionErrorTerminal, err.Kind)
	require.NotNil(t, err.Terminal)
	assert.Equal(t, model.SessionFailed, err.Terminal.State)
	assert.Equal(t, model.RProcessEnded, err.Terminal.Reason)
	assert.Equal(t, "transcode stalled - no progress detected", err.Terminal.ReasonDetail)
	assert.Equal(t, problemcode.CodeTranscodeStalled, err.Terminal.Code)
	assert.Equal(t, "The session failed because the transcode process stopped producing progress.", err.Terminal.Detail)
}

func TestServiceGetSessionReadyRecordingPlaybackInfo(t *testing.T) {
	svc := NewService(fakeDeps{store: fakeStore{session: &model.SessionRecord{
		SessionID:  "550e8400-e29b-41d4-a716-446655440001",
		State:      model.SessionReady,
		ServiceRef: "1:0:1:445D:453:1:C00000:0:0:0:",
		Profile: model.ProfileSpec{
			Name: "compatible",
		},
		ContextData: map[string]string{
			model.CtxKeyMode:            model.ModeRecording,
			model.CtxKeyDurationSeconds: "3600",
		},
	}}})

	got, err := svc.GetSession(context.Background(), GetSessionRequest{
		SessionID: "550e8400-e29b-41d4-a716-446655440001",
		Now:       time.Unix(1700000000, 0),
	})

	require.Nil(t, err)
	require.NotNil(t, got.Session)
	assert.Equal(t, model.SessionReady, got.Outcome.State)
	assert.Equal(t, model.ModeRecording, got.PlaybackInfo.Mode)
	require.NotNil(t, got.PlaybackInfo.DurationSeconds)
	assert.Equal(t, 3600.0, *got.PlaybackInfo.DurationSeconds)
	require.NotNil(t, got.PlaybackInfo.SeekableStartSeconds)
	assert.Equal(t, 0.0, *got.PlaybackInfo.SeekableStartSeconds)
	require.NotNil(t, got.PlaybackInfo.SeekableEndSeconds)
	assert.Equal(t, 3600.0, *got.PlaybackInfo.SeekableEndSeconds)
	assert.Nil(t, got.PlaybackInfo.LiveEdgeSeconds)
}
