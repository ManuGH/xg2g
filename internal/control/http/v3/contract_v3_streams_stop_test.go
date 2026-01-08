package v3

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/ManuGH/xg2g/internal/pipeline/bus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockBus struct {
	publishedTopic string
	publishedEvent any
	publishErr     error
}

func (b *mockBus) Publish(ctx context.Context, topic string, event any) error {
	b.publishedTopic = topic
	b.publishedEvent = event
	return b.publishErr
}

func (b *mockBus) Subscribe(ctx context.Context, topic string) (bus.Subscriber, error) {
	return nil, nil // Stub - not used in DeleteStreamsId tests
}

// Minimal store mock for DeleteStreamsId (only what we need)
type mockStore struct {
	session *model.SessionRecord
	err     error
}

func (m *mockStore) GetSession(ctx context.Context, id string) (*model.SessionRecord, error) {
	return m.session, m.err
}

// Stub methods to satisfy store.StateStore interface (not used in DeleteStreamsId)
func (m *mockStore) PutSession(ctx context.Context, s *model.SessionRecord) error { return nil }
func (m *mockStore) PutSessionWithIdempotency(ctx context.Context, s *model.SessionRecord, k string, d time.Duration) (string, bool, error) {
	return "", false, nil
}
func (m *mockStore) DeleteSession(ctx context.Context, id string) error { return nil }
func (m *mockStore) UpdateSession(ctx context.Context, id string, fn func(*model.SessionRecord) error) (*model.SessionRecord, error) {
	return nil, nil
}
func (m *mockStore) QuerySessions(ctx context.Context, f store.SessionFilter) ([]*model.SessionRecord, error) {
	return nil, nil
}
func (m *mockStore) ListSessions(ctx context.Context) ([]*model.SessionRecord, error) {
	return nil, nil
}
func (m *mockStore) ScanSessions(ctx context.Context, fn func(*model.SessionRecord) error) error {
	return nil
}
func (m *mockStore) PutIdempotency(ctx context.Context, k, s string, d time.Duration) error {
	return nil
}
func (m *mockStore) GetIdempotency(ctx context.Context, k string) (string, bool, error) {
	return "", false, nil
}
func (m *mockStore) TryAcquireLease(ctx context.Context, k, o string, d time.Duration) (store.Lease, bool, error) {
	return nil, false, nil
}
func (m *mockStore) RenewLease(ctx context.Context, k, o string, d time.Duration) (store.Lease, bool, error) {
	return nil, false, nil
}
func (m *mockStore) ReleaseLease(ctx context.Context, k, o string) error { return nil }
func (m *mockStore) DeleteAllLeases(ctx context.Context) (int, error)    { return 0, nil }

// --- Tests 13-20 ---

func TestContract_StopStream_DeleteStreamsId(t *testing.T) {
	cfg := config.AppConfig{}
	snap := config.Snapshot{}

	t.Run("13_InvalidID_Empty", func(t *testing.T) {
		s := &Server{cfg: cfg, snap: snap, v3Store: &mockStore{}, v3Bus: &mockBus{}}
		req := httptest.NewRequest("DELETE", "/api/v3/streams/", nil)
		w := httptest.NewRecorder()

		s.DeleteStreamsId(w, req, "")

		assertProblemDetails(t, w.Result(), 400, "streams/invalid_id")
	})

	t.Run("14_InvalidID_Unsafe", func(t *testing.T) {
		require.False(t, model.IsSafeSessionID("../unsafe"))

		s := &Server{cfg: cfg, snap: snap, v3Store: &mockStore{}, v3Bus: &mockBus{}}
		req := httptest.NewRequest("DELETE", "/api/v3/streams/../unsafe", nil)
		w := httptest.NewRecorder()

		s.DeleteStreamsId(w, req, "../unsafe")

		assertProblemDetails(t, w.Result(), 400, "streams/invalid_id")
	})

	t.Run("15_ControlPlaneDisabled_NoStore", func(t *testing.T) {
		s := &Server{cfg: cfg, snap: snap, v3Store: nil, v3Bus: &mockBus{}}
		req := httptest.NewRequest("DELETE", "/api/v3/streams/abc", nil)
		w := httptest.NewRecorder()

		s.DeleteStreamsId(w, req, "abc")

		assertProblemDetails(t, w.Result(), 503, "streams/unavailable")
	})

	t.Run("16_ControlPlaneDisabled_NoBus", func(t *testing.T) {
		s := &Server{cfg: cfg, snap: snap, v3Store: &mockStore{}, v3Bus: nil}
		req := httptest.NewRequest("DELETE", "/api/v3/streams/abc", nil)
		w := httptest.NewRecorder()

		s.DeleteStreamsId(w, req, "abc")

		assertProblemDetails(t, w.Result(), 503, "streams/unavailable")
	})

	t.Run("17_SessionNotFound", func(t *testing.T) {
		s := &Server{
			cfg: cfg, snap: snap,
			v3Store: &mockStore{session: nil, err: nil},
			v3Bus:   &mockBus{},
		}
		req := httptest.NewRequest("DELETE", "/api/v3/streams/abc", nil)
		w := httptest.NewRecorder()

		s.DeleteStreamsId(w, req, "abc")

		assertProblemDetails(t, w.Result(), 404, "streams/not_found")
	})

	t.Run("18_StoreError", func(t *testing.T) {
		s := &Server{
			cfg: cfg, snap: snap,
			v3Store: &mockStore{session: nil, err: errors.New("db down")},
			v3Bus:   &mockBus{},
		}
		req := httptest.NewRequest("DELETE", "/api/v3/streams/abc", nil)
		w := httptest.NewRecorder()

		s.DeleteStreamsId(w, req, "abc")

		assertProblemDetails(t, w.Result(), 500, "streams/stop_failed")
	})

	t.Run("19_PublishFails", func(t *testing.T) {
		st := &model.SessionRecord{CorrelationID: "corr-123"}

		bus := &mockBus{publishErr: errors.New("bus fail")}
		s := &Server{
			cfg: cfg, snap: snap,
			v3Store: &mockStore{session: st, err: nil},
			v3Bus:   bus,
		}
		req := httptest.NewRequest("DELETE", "/api/v3/streams/abc", nil)
		w := httptest.NewRecorder()

		s.DeleteStreamsId(w, req, "abc")

		assertProblemDetails(t, w.Result(), 500, "streams/stop_failed")
	})

	t.Run("20_Success_204_PublishesCorrectEvent", func(t *testing.T) {
		st := &model.SessionRecord{CorrelationID: "corr-123"}

		bus := &mockBus{}
		s := &Server{
			cfg: cfg, snap: snap,
			v3Store: &mockStore{session: st, err: nil},
			v3Bus:   bus,
		}
		req := httptest.NewRequest("DELETE", "/api/v3/streams/abc", nil)
		w := httptest.NewRecorder()

		s.DeleteStreamsId(w, req, "abc")

		resp := w.Result()
		assert.Equal(t, 204, resp.StatusCode)

		// Publishing invariants (hard gate against drift)
		require.Equal(t, string(model.EventStopSession), bus.publishedTopic)

		ev, ok := bus.publishedEvent.(model.StopSessionEvent)
		require.True(t, ok, "published event must be model.StopSessionEvent")
		assert.Equal(t, model.EventStopSession, ev.Type)
		assert.Equal(t, "abc", ev.SessionID)
		assert.Equal(t, st.CorrelationID, ev.CorrelationID)
		assert.Equal(t, model.RClientStop, ev.Reason)
		assert.NotZero(t, ev.RequestedAtUN)
	})
}
