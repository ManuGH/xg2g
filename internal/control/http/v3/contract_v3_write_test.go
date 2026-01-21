package v3

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Minimal Mocks for Phase 6
type Phase6MockStore struct {
	// Add fields as needed for specific tests
}

func (m *Phase6MockStore) ListSessions(ctx context.Context) ([]*model.SessionRecord, error) {
	return nil, nil
}

// Stub methods...
func (m *Phase6MockStore) PutSession(ctx context.Context, s *model.SessionRecord) error { return nil }
func (m *Phase6MockStore) PutSessionWithIdempotency(ctx context.Context, s *model.SessionRecord, k string, t time.Duration) (string, bool, error) {
	return "", false, nil
}
func (m *Phase6MockStore) GetSession(ctx context.Context, id string) (*model.SessionRecord, error) {
	return nil, nil
}
func (m *Phase6MockStore) QuerySessions(ctx context.Context, f store.SessionFilter) ([]*model.SessionRecord, error) {
	return nil, nil
}
func (m *Phase6MockStore) UpdateSession(ctx context.Context, id string, fn func(*model.SessionRecord) error) (*model.SessionRecord, error) {
	return nil, nil
}
func (m *Phase6MockStore) ScanSessions(ctx context.Context, fn func(*model.SessionRecord) error) error {
	return nil
}
func (m *Phase6MockStore) DeleteSession(ctx context.Context, id string) error { return nil }
func (m *Phase6MockStore) PutIdempotency(ctx context.Context, k, s string, t time.Duration) error {
	return nil
}
func (m *Phase6MockStore) GetIdempotency(ctx context.Context, k string) (string, bool, error) {
	return "", false, nil
}
func (m *Phase6MockStore) TryAcquireLease(ctx context.Context, k, o string, t time.Duration) (store.Lease, bool, error) {
	return nil, false, nil
}
func (m *Phase6MockStore) RenewLease(ctx context.Context, k, o string, t time.Duration) (store.Lease, bool, error) {
	return nil, false, nil
}
func (m *Phase6MockStore) ReleaseLease(ctx context.Context, k, o string) error { return nil }
func (m *Phase6MockStore) DeleteAllLeases(ctx context.Context) (int, error)    { return 0, nil }

// AssertHelp for ProblemDetails
func assertProblemDetails(t *testing.T, resp *http.Response, expectedStatus int, expectedType string) {
	t.Helper()
	require.Equal(t, expectedStatus, resp.StatusCode)

	ct := resp.Header.Get("Content-Type")
	assert.Contains(t, ct, "application/problem+json")

	var pd map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&pd); err != nil {
		t.Logf("Failed to decode JSON: %v", err)
	}
	t.Logf("Response: Status=%d Body=%v", resp.StatusCode, pd)

	assert.Equal(t, float64(expectedStatus), pd["status"]) // JSON numbers are float64 in generic map
	assert.Equal(t, expectedType, pd["type"])
	assert.NotEmpty(t, pd["title"])
}

func TestContract_Global_ProblemDetails(t *testing.T) {
	// Base Setup
	cfg := config.AppConfig{}
	snap := config.Snapshot{}
	store := &Phase6MockStore{}

	// Stub server with store, explicitly nil bus to test control plane check
	s := &Server{cfg: cfg, snap: snap, v3Store: store, v3Bus: nil}

	// Test 1: InvalidJSON_AddTimer
	t.Run("1_InvalidJSON_AddTimer", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v3/dvr/timers", strings.NewReader("{invalid-json"))
		w := httptest.NewRecorder()
		s.AddTimer(w, req)
		assertProblemDetails(t, w.Result(), 400, "dvr/invalid_input")
	})

	// Test 2: InvalidJSON_UpdateTimer
	t.Run("2_InvalidJSON_UpdateTimer", func(t *testing.T) {
		req := httptest.NewRequest("PATCH", "/api/v3/dvr/timers/123", strings.NewReader("{invalid"))
		w := httptest.NewRecorder()
		s.UpdateTimer(w, req, "123")
		assertProblemDetails(t, w.Result(), 400, "dvr/invalid_input")
	})

	// Test 3: InvalidBody_PreviewConflicts
	t.Run("3_InvalidBody_PreviewConflicts", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v3/dvr/timers/conflicts/preview", strings.NewReader("{boom"))
		w := httptest.NewRecorder()
		s.PreviewConflicts(w, req)
		assertProblemDetails(t, w.Result(), 400, "dvr/invalid_input")
	})

	// Test 4: InvalidID_DeleteStreams
	t.Run("4_InvalidID_DeleteStreams", func(t *testing.T) {
		// Sanity check: unsafe ID must be rejected by validation
		require.False(t, model.IsSafeSessionID("../unsafe"), "sanity: unsafe id must be rejected")

		req := httptest.NewRequest("DELETE", "/api/v3/streams/../unsafe", nil)
		w := httptest.NewRecorder()

		t.Logf("Server type: %T", s)
		t.Logf("isSafe=%v", model.IsSafeSessionID("../unsafe"))

		s.DeleteStreamsId(w, req, "../unsafe")

		// Note: Don't read body here - assertProblemDetails will do it
		t.Logf("status=%d ct=%q", w.Code, w.Header().Get("Content-Type"))

		assertProblemDetails(t, w.Result(), 400, "streams/invalid_id")
	})

	// Test 5: InvalidTimerID_DeleteTimer
	t.Run("5_InvalidTimerID_DeleteTimer", func(t *testing.T) {
		req := httptest.NewRequest("DELETE", "/api/v3/dvr/timers/bad-id", nil)
		w := httptest.NewRecorder()
		s.DeleteTimer(w, req, "bad-id")
		assertProblemDetails(t, w.Result(), 400, "dvr/invalid_id")
	})

	// Test 6: InvalidTimerID_UpdateTimer
	t.Run("6_InvalidTimerID_UpdateTimer", func(t *testing.T) {
		req := httptest.NewRequest("PATCH", "/api/v3/dvr/timers/bad-id", strings.NewReader("{}"))
		w := httptest.NewRecorder()
		s.UpdateTimer(w, req, "bad-id")
		assertProblemDetails(t, w.Result(), 400, "dvr/invalid_id")
	})

	// Test 7: ControlPlaneDisabled_DeleteStreams
	t.Run("7_ControlPlaneDisabled_DeleteStreams", func(t *testing.T) {
		sNoStore := &Server{cfg: cfg, snap: snap, v3Store: nil}
		req := httptest.NewRequest("DELETE", "/api/v3/streams/123", nil)
		w := httptest.NewRecorder()
		sNoStore.DeleteStreamsId(w, req, "123")
		assertProblemDetails(t, w.Result(), 503, "streams/unavailable")
	})

	// Test 8: ControlPlaneDisabled_GetStreams (Regression)
	t.Run("8_ControlPlaneDisabled_GetStreams", func(t *testing.T) {
		sNoStore := &Server{cfg: cfg, snap: snap, v3Store: nil}
		req := httptest.NewRequest("GET", "/api/v3/streams", nil)
		w := httptest.NewRecorder()
		sNoStore.GetStreams(w, req)
		assertProblemDetails(t, w.Result(), 503, "streams/unavailable")
	})

	// Note: Tests 9-12 require Receiver mocking.
	// We'll skip implementation details of receiver injection for now
	// until we implement Step 4/5/6/7 fully.
	// Or we can stub the receiver source if possible.
	// For Step 1, we focus on what's testable given current structure.

	// Test 12b: Nil Details Omission
	t.Run("12b_ProblemDetails_OmitsDetails_WhenNil", func(t *testing.T) {
		// This uses internal helper capability (we can expose a temporary handler or rely on one)
		// Or we can just test writeProblem directly if we assume it's unexported?
		// Unexported functions are hard to test from test package.
		// We can use a test handler that triggers it.
		// "Streams Store Nil" test uses it with nil details.

		sNoStore := &Server{cfg: cfg, snap: snap, v3Store: nil}
		req := httptest.NewRequest("GET", "/api/v3/streams", nil)
		w := httptest.NewRecorder()
		sNoStore.GetStreams(w, req)

		resp := w.Result()
		var pd map[string]any
		err := json.NewDecoder(resp.Body).Decode(&pd)
		require.NoError(t, err)

		_, exists := pd["details"]
		assert.False(t, exists, "details field should be absent when nil")
		_, existsConflicts := pd["conflicts"]
		assert.False(t, existsConflicts, "conflicts field should be absent")
	})
}

func (s *Phase6MockStore) GetLease(ctx context.Context, key string) (store.Lease, bool, error) {
return nil, false, nil
}
