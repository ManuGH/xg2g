package v3

import (
	"context"
	"encoding/json"
	"errors" // For errors.New
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5" // Import chi
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock" // Import mock
	"github.com/stretchr/testify/require"

	controlhttp "github.com/ManuGH/xg2g/internal/control/http"
	"github.com/ManuGH/xg2g/internal/control/playback"
	recservice "github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/ManuGH/xg2g/internal/log"
)

// -- Traceability Gates --
func assertTraceability(t *testing.T, w *httptest.ResponseRecorder, dtoReqID string, dtoSessionID string, isRecording bool) {
	t.Helper()
	assert.NotEmpty(t, dtoReqID, "Gate 1: DTO RequestId must not be empty")
	headerID := w.Header().Get(controlhttp.HeaderRequestID)
	assert.Equal(t, dtoReqID, headerID, "Gate 2: Response Header must match DTO RequestId")
	if dtoSessionID != "" {
		if isRecording {
			assert.True(t, strings.HasPrefix(dtoSessionID, "rec:"), "Gate 3: Recording SessionId must start with 'rec:'")
		} else {
			_, err := uuid.Parse(dtoSessionID)
			assert.NoError(t, err, "Gate 3: Live SessionId must be a valid UUID")
		}
	}
}

// -- Local Mocks with Expectations (Avoiding Shared Stubs) --

type MockTraceabilityService struct {
	mock.Mock
}

func (m *MockTraceabilityService) List(ctx context.Context, in recservice.ListInput) (recservice.ListResult, error) {
	args := m.Called(ctx, in)
	return args.Get(0).(recservice.ListResult), args.Error(1)
}

// Implement other methods as no-ops to satisfy interface
func (m *MockTraceabilityService) ResolvePlayback(ctx context.Context, recordingID, profile string) (recservice.PlaybackResolution, error) {
	return recservice.PlaybackResolution{}, nil
}
func (m *MockTraceabilityService) GetPlaybackInfo(ctx context.Context, in recservice.PlaybackInfoInput) (recservice.PlaybackInfoResult, error) {
	return recservice.PlaybackInfoResult{}, nil
}
func (m *MockTraceabilityService) GetStatus(ctx context.Context, in recservice.StatusInput) (recservice.StatusResult, error) {
	args := m.Called(ctx, in)
	// Add panic guard or return safety if not mocked
	if len(args) == 0 {
		return recservice.StatusResult{}, nil
	}
	return args.Get(0).(recservice.StatusResult), args.Error(1)
}
func (m *MockTraceabilityService) Stream(ctx context.Context, in recservice.StreamInput) (recservice.StreamResult, error) {
	return recservice.StreamResult{}, nil
}
func (m *MockTraceabilityService) Delete(ctx context.Context, in recservice.DeleteInput) (recservice.DeleteResult, error) {
	return recservice.DeleteResult{}, nil
}

func (m *MockTraceabilityService) GetMediaTruth(ctx context.Context, recordingID string) (playback.MediaTruth, error) {
	args := m.Called(ctx, recordingID)
	return args.Get(0).(playback.MediaTruth), args.Error(1)
}

type MockTraceabilityStore struct {
	Session             *model.SessionRecord
	MockStoreForStreams // Inherit stub methods
}

// Override GetSession
func (m *MockTraceabilityStore) GetSession(ctx context.Context, id string) (*model.SessionRecord, error) {
	if m.Session != nil && m.Session.SessionID == id {
		return m.Session, nil
	}
	return nil, nil // Not found
}

// -- Tests --

func TestTraceability_RecordingsList(t *testing.T) {
	svc := new(MockTraceabilityService)
	svc.On("List", mock.Anything, mock.Anything).Return(recservice.ListResult{
		Recordings: []recservice.RecordingItem{
			{RecordingID: "rec1", Title: "Test Rec"},
		},
	}, nil)

	s := &Server{recordingsService: svc}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/v3/recordings", nil)

	ctx := log.ContextWithRequestID(r.Context(), "req-test-rec-list")
	r = r.WithContext(ctx)

	s.GetRecordings(w, r, GetRecordingsParams{})

	require.Equal(t, http.StatusOK, w.Code)

	var resp RecordingResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	assertTraceability(t, w, resp.RequestId, "", false)
}

func TestTraceability_GetSession_Real(t *testing.T) {
	sessionID := uuid.New().String()
	store := &MockTraceabilityStore{
		Session: &model.SessionRecord{
			SessionID:     sessionID,
			State:         model.SessionReady,
			CreatedAtUnix: time.Now().Unix(),
		},
	}

	s := &Server{v3Store: store}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/v3/sessions/"+sessionID, nil)

	// Inject Context Request ID
	ctx := log.ContextWithRequestID(r.Context(), "req-test-sess-detail")

	// Inject Chi URL Param
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("sessionID", sessionID)
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)

	r = r.WithContext(ctx)

	s.handleV3SessionState(w, r)

	require.Equal(t, http.StatusOK, w.Code)

	var resp SessionResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	sid := uuid.UUID(resp.SessionId).String()
	assertTraceability(t, w, resp.RequestId, sid, false)
}

func TestTraceability_GetStreams(t *testing.T) {
	sessionID := uuid.New().String()
	store := &MockStoreForStreams{ // This one works fine for streams as it implements ListSessions via struct field
		Sessions: []*model.SessionRecord{
			{SessionID: sessionID, State: model.SessionReady, ServiceRef: "1:0:1..."},
		},
	}

	s := &Server{
		v3Store: store,
		// minimal dependencies for GetStreams
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/v3/streams", nil)

	ctx := log.ContextWithRequestID(r.Context(), "req-test-streams")
	r = r.WithContext(ctx)

	s.GetStreams(w, r)

	require.Equal(t, http.StatusOK, w.Code)

	var resp []StreamSession
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.Len(t, resp, 1)

	// Strict Gates (Per Item)
	item := resp[0]
	sid := uuid.UUID(item.SessionId).String()
	assertTraceability(t, w, item.RequestId, sid, false)
}

func TestTraceability_RFC7807_Error(t *testing.T) {
	svc := new(MockTraceabilityService)
	// We force an error
	// Note: We use errors.New to ensure it's not a domain error that gets classified as 200 or 404 in weird ways, although Classify handles generic error as 500
	svc.On("List", mock.Anything, mock.Anything).Return(recservice.ListResult{}, errors.New("simulated error"))

	s := &Server{recordingsService: svc}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/v3/recordings", nil)

	ctx := log.ContextWithRequestID(r.Context(), "req-test-error")
	r = r.WithContext(ctx)

	s.GetRecordings(w, r, GetRecordingsParams{})

	require.NotEqual(t, http.StatusOK, w.Code)
	// Expect 500
	require.Equal(t, http.StatusInternalServerError, w.Code)

	type Problem struct {
		Type      string `json:"type"`
		RequestId string `json:"requestId"`
	}
	var p Problem
	err := json.Unmarshal(w.Body.Bytes(), &p)
	require.NoError(t, err)

	assert.NotEmpty(t, p.RequestId, "RFC7807: requestId field must be present")
	assert.Equal(t, "req-test-error", p.RequestId, "RFC7807: requestId must match context")
	assert.Equal(t, "req-test-error", w.Header().Get(controlhttp.HeaderRequestID), "RFC7807: Response Header must match context")
}

func (s *MockTraceabilityStore) GetLease(ctx context.Context, key string) (store.Lease, bool, error) {
	return nil, false, nil
}
