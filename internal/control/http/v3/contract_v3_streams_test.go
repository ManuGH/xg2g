package v3

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockStoreForStreams implements StateStore for GetStreams testing.
type MockStoreForStreams struct {
	Sessions []*model.SessionRecord
	Err      error
}

func (m *MockStoreForStreams) PutSession(ctx context.Context, s *model.SessionRecord) error {
	return nil
}
func (m *MockStoreForStreams) PutSessionWithIdempotency(ctx context.Context, s *model.SessionRecord, k string, t time.Duration) (string, bool, error) {
	return "", false, nil
}
func (m *MockStoreForStreams) GetSession(ctx context.Context, id string) (*model.SessionRecord, error) {
	return nil, nil
}
func (m *MockStoreForStreams) QuerySessions(ctx context.Context, f store.SessionFilter) ([]*model.SessionRecord, error) {
	return nil, nil
}
func (m *MockStoreForStreams) UpdateSession(ctx context.Context, id string, fn func(*model.SessionRecord) error) (*model.SessionRecord, error) {
	return nil, nil
}
func (m *MockStoreForStreams) ListSessions(ctx context.Context) ([]*model.SessionRecord, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	return m.Sessions, nil
}
func (m *MockStoreForStreams) ScanSessions(ctx context.Context, fn func(*model.SessionRecord) error) error {
	return nil
}
func (m *MockStoreForStreams) DeleteSession(ctx context.Context, id string) error { return nil }
func (m *MockStoreForStreams) PutIdempotency(ctx context.Context, k, s string, t time.Duration) error {
	return nil
}
func (m *MockStoreForStreams) GetIdempotency(ctx context.Context, k string) (string, bool, error) {
	return "", false, nil
}
func (m *MockStoreForStreams) TryAcquireLease(ctx context.Context, k, o string, t time.Duration) (store.Lease, bool, error) {
	return nil, false, nil
}
func (m *MockStoreForStreams) RenewLease(ctx context.Context, k, o string, t time.Duration) (store.Lease, bool, error) {
	return nil, false, nil
}
func (m *MockStoreForStreams) ReleaseLease(ctx context.Context, k, o string) error { return nil }
func (m *MockStoreForStreams) DeleteAllLeases(ctx context.Context) (int, error)    { return 0, nil }

func TestGetStreams_Contract_Slice53(t *testing.T) {
	// Base Config
	cfg := config.AppConfig{}
	snap := config.Snapshot{Runtime: config.RuntimeSnapshot{
		PlaylistFilename: "", // No playlist -> no name resolution (names will be empty/ServiceRef)
	}}

	t.Run("Empty_Returns_EmptyList", func(t *testing.T) {
		mockStore := &MockStoreForStreams{Sessions: nil}
		s := &Server{
			cfg:     cfg,
			snap:    snap,
			v3Store: mockStore,
		}

		req := httptest.NewRequest("GET", "/api/v3/streams", nil)
		w := httptest.NewRecorder()

		s.GetStreams(w, req)

		resp := w.Result()
		require.Equal(t, http.StatusOK, resp.StatusCode)

		var list []StreamSession
		err := json.NewDecoder(resp.Body).Decode(&list)
		require.NoError(t, err)
		assert.NotNil(t, list, "Must return [] not null")
		assert.Equal(t, 0, len(list))
	})

	t.Run("Sorting_Deterministic", func(t *testing.T) {
		// Prepare data
		ts1 := int64(1000)
		ts2 := int64(2000)
		ts0 := int64(0)

		sessions := []*model.SessionRecord{
			{SessionID: "C", CreatedAtUnix: ts1, State: model.SessionNew}, // Mid
			{SessionID: "A", CreatedAtUnix: ts2, State: model.SessionNew}, // Latest (First)
			{SessionID: "D", CreatedAtUnix: ts0, State: model.SessionNew}, // Zero (Last)
			{SessionID: "B", CreatedAtUnix: ts2, State: model.SessionNew}, // Same TS (Tie-break ID: A then B)
		}
		mockStore := &MockStoreForStreams{Sessions: sessions}
		s := &Server{
			cfg:     cfg,
			snap:    snap,
			v3Store: mockStore,
		}

		req := httptest.NewRequest("GET", "/api/v3/streams", nil)
		w := httptest.NewRecorder()

		s.GetStreams(w, req)

		var list []StreamSession
		_ = json.NewDecoder(w.Body).Decode(&list)

		require.Len(t, list, 4)
		// Expected Order:
		// 1. A (TS=2000, ID=A)
		// 2. B (TS=2000, ID=B) -> Tie break on ID
		// 3. C (TS=1000)
		// 4. D (TS=0)

		assert.Equal(t, "A", *list[0].Id)
		assert.Equal(t, "B", *list[1].Id)
		assert.Equal(t, "C", *list[2].Id)
		assert.Equal(t, "D", *list[3].Id)
	})

	t.Run("IP_Gating", func(t *testing.T) {
		sessions := []*model.SessionRecord{
			{
				SessionID:   "1",
				State:       model.SessionNew,
				ContextData: map[string]string{"client_ip": "1.2.3.4"},
			},
		}
		mockStore := &MockStoreForStreams{Sessions: sessions}
		s := &Server{
			cfg:     cfg,
			snap:    snap,
			v3Store: mockStore,
		}

		// Case 1: Default (No Param) -> IP Empty/Cloned
		req1 := httptest.NewRequest("GET", "/api/v3/streams", nil)
		w1 := httptest.NewRecorder()
		s.GetStreams(w1, req1)

		var list1 []StreamSession
		_ = json.NewDecoder(w1.Body).Decode(&list1)
		require.Len(t, list1, 1)
		assert.Nil(t, list1[0].ClientIp, "IP must be omitted by default")

		// Case 2: Explicit True
		req2 := httptest.NewRequest("GET", "/api/v3/streams?include_client_ip=true", nil)
		w2 := httptest.NewRecorder()
		s.GetStreams(w2, req2)

		var list2 []StreamSession
		_ = json.NewDecoder(w2.Body).Decode(&list2)
		require.Len(t, list2, 1)
		require.NotNil(t, list2[0].ClientIp)
		assert.Equal(t, "1.2.3.4", *list2[0].ClientIp)

		// Case 3: Explicit False
		req3 := httptest.NewRequest("GET", "/api/v3/streams?include_client_ip=false", nil)
		w3 := httptest.NewRecorder()
		s.GetStreams(w3, req3)

		var list3 []StreamSession
		_ = json.NewDecoder(w3.Body).Decode(&list3)
		assert.Nil(t, list3[0].ClientIp)
	})

	t.Run("Terminal_States_Filtered", func(t *testing.T) {
		sessions := []*model.SessionRecord{
			{SessionID: "active", State: model.SessionReady},
			{SessionID: "stopped", State: model.SessionStopped},
			{SessionID: "failed", State: model.SessionFailed},
		}
		mockStore := &MockStoreForStreams{Sessions: sessions}
		s := &Server{cfg: cfg, snap: snap, v3Store: mockStore}

		req := httptest.NewRequest("GET", "/api/v3/streams", nil)
		w := httptest.NewRecorder()
		s.GetStreams(w, req)

		var list []StreamSession
		_ = json.NewDecoder(w.Body).Decode(&list)

		require.Len(t, list, 1)
		assert.Equal(t, "active", *list[0].Id)
	})

	t.Run("Error_Contract_ProblemDetails", func(t *testing.T) {
		// Test 24: StoreNil_Returns503_ProblemDetails_JSON
		t.Run("StoreNil", func(t *testing.T) {
			s := &Server{cfg: cfg, snap: snap, v3Store: nil} // Nil store
			req := httptest.NewRequest("GET", "/api/v3/streams", nil)
			w := httptest.NewRecorder()
			s.GetStreams(w, req)

			resp := w.Result()
			require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
			require.Equal(t, "application/problem+json", resp.Header.Get("Content-Type"))

			var pd ProblemDetails
			err := json.NewDecoder(resp.Body).Decode(&pd)
			require.NoError(t, err)
			assert.Equal(t, 503, pd.Status)
			assert.Equal(t, "streams/unavailable", pd.Type)
			assert.Equal(t, "V3 control plane not enabled", pd.Title)
		})

		// Test 25: StoreError_Returns500_ProblemDetails_JSON
		t.Run("StoreError", func(t *testing.T) {
			mockStore := &MockStoreForStreams{
				Sessions: nil,
				Err:      errors.New("db boom"),
			}
			s := &Server{cfg: cfg, snap: snap, v3Store: mockStore}

			req := httptest.NewRequest("GET", "/api/v3/streams", nil)
			w := httptest.NewRecorder()
			s.GetStreams(w, req)

			resp := w.Result()
			require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
			require.Equal(t, "application/problem+json", resp.Header.Get("Content-Type"))

			var pd ProblemDetails
			err := json.NewDecoder(resp.Body).Decode(&pd)
			require.NoError(t, err)
			assert.Equal(t, 500, pd.Status)
			assert.Equal(t, "streams/read_failed", pd.Type)
			assert.Equal(t, "Failed to get streams", pd.Title)
		})
	})

	t.Run("Mandatory_Traceability_Fields", func(t *testing.T) {
		id := "00000000-0000-0000-0000-000000000001"
		mockStore := &MockStoreForStreams{
			Sessions: []*model.SessionRecord{
				{
					SessionID:     id,
					CorrelationID: "req_test_123",
					State:         model.SessionReady,
					CreatedAtUnix: 1000,
					// PR-P3-2: Make it strictly Active
					PipelineState:        model.PipeServing,
					LatestSegmentAt:      time.Now(),
					LastPlaylistAccessAt: time.Now(),
				},
			},
		}
		s := &Server{
			cfg:     cfg,
			snap:    snap,
			v3Store: mockStore,
		}

		req := httptest.NewRequest("GET", "/api/v3/streams", nil)
		req = req.WithContext(log.ContextWithRequestID(req.Context(), "req_test_123"))

		w := httptest.NewRecorder()
		s.GetStreams(w, req)

		var list []StreamSession
		json.NewDecoder(w.Result().Body).Decode(&list)

		require.Len(t, list, 1)
		assert.Equal(t, id, *list[0].Id)
		assert.Equal(t, id, list[0].SessionId.String())
		assert.Equal(t, "req_test_123", list[0].RequestId)
		assert.Equal(t, StreamSessionStateActive, list[0].State)
	})
	t.Run("Lifecycle_Truth_Mapping", func(t *testing.T) {
		now := time.Now()
		sessions := []*model.SessionRecord{
			{
				SessionID: "buffering",
				State:     model.SessionPriming,
			},
			{
				SessionID:            "active",
				State:                model.SessionReady,
				PipelineState:        model.PipeServing,
				PlaylistPublishedAt:  now.Add(-1 * time.Minute),
				LatestSegmentAt:      now.Add(-2 * time.Second),
				LastPlaylistAccessAt: now.Add(-1 * time.Second),
			},
			{
				SessionID:            "stalled",
				State:                model.SessionReady,
				PipelineState:        model.PipeServing,
				PlaylistPublishedAt:  now.Add(-1 * time.Minute),
				LatestSegmentAt:      now.Add(-15 * time.Second), // > 12s
				LastPlaylistAccessAt: now.Add(-1 * time.Second),
			},
			{
				SessionID:            "idle",
				State:                model.SessionReady,
				PipelineState:        model.PipeServing,
				PlaylistPublishedAt:  now.Add(-1 * time.Minute),
				LatestSegmentAt:      now.Add(-2 * time.Second),
				LastPlaylistAccessAt: now.Add(-40 * time.Second), // > 30s
			},
		}

		mockStore := &MockStoreForStreams{Sessions: sessions}
		s := &Server{cfg: cfg, snap: snap, v3Store: mockStore}

		req := httptest.NewRequest("GET", "/api/v3/streams", nil)
		w := httptest.NewRecorder()
		s.GetStreams(w, req)

		var list []StreamSession
		err := json.NewDecoder(w.Result().Body).Decode(&list)
		require.NoError(t, err)
		require.Len(t, list, 4)

		stateMap := make(map[string]StreamSessionState)
		for _, sess := range list {
			if sess.Id != nil {
				stateMap[*sess.Id] = sess.State
			}
		}

		assert.Equal(t, StreamSessionStateBuffering, stateMap["buffering"])
		assert.Equal(t, StreamSessionStateActive, stateMap["active"])
		assert.Equal(t, StreamSessionStateStalled, stateMap["stalled"])
		assert.Equal(t, StreamSessionStateIdle, stateMap["idle"])
	})
}
