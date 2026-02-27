package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	v3 "github.com/ManuGH/xg2g/internal/control/http/v3"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/stretchr/testify/assert"
)

// MockPreparingServer handles v3 API calls for the preparing test.
type MockPreparingServer struct{}

func (m *MockPreparingServer) CreateSession(w http.ResponseWriter, r *http.Request)      {}
func (m *MockPreparingServer) GetDvrCapabilities(w http.ResponseWriter, r *http.Request) {}
func (m *MockPreparingServer) GetDvrStatus(w http.ResponseWriter, r *http.Request)       {}
func (m *MockPreparingServer) GetEpg(w http.ResponseWriter, r *http.Request, params v3.GetEpgParams) {
}
func (m *MockPreparingServer) CreateIntent(w http.ResponseWriter, r *http.Request)         {}
func (m *MockPreparingServer) PostLivePlaybackInfo(w http.ResponseWriter, r *http.Request) {}
func (m *MockPreparingServer) GetLogs(w http.ResponseWriter, r *http.Request)              {}
func (m *MockPreparingServer) GetReceiverCurrent(w http.ResponseWriter, r *http.Request)   {}
func (m *MockPreparingServer) GetRecordings(w http.ResponseWriter, r *http.Request, params v3.GetRecordingsParams) {
}
func (m *MockPreparingServer) DeleteRecording(w http.ResponseWriter, r *http.Request, recordingId string) {
}
func (m *MockPreparingServer) GetRecordingsRecordingIdStatus(w http.ResponseWriter, r *http.Request, recordingId string) {
}
func (m *MockPreparingServer) GetRecordingPlaybackInfo(w http.ResponseWriter, r *http.Request, recordingId string) {
}
func (m *MockPreparingServer) GetRecordingHLSTimeshift(w http.ResponseWriter, r *http.Request, recordingId string) {
}
func (m *MockPreparingServer) GetRecordingHLSTimeshiftHead(w http.ResponseWriter, r *http.Request, recordingId string) {
}
func (m *MockPreparingServer) GetRecordingHLSCustomSegment(w http.ResponseWriter, r *http.Request, recordingId string, segment string) {
}
func (m *MockPreparingServer) GetRecordingHLSCustomSegmentHead(w http.ResponseWriter, r *http.Request, recordingId string, segment string) {
}
func (m *MockPreparingServer) GetSeriesRules(w http.ResponseWriter, r *http.Request)   {}
func (m *MockPreparingServer) CreateSeriesRule(w http.ResponseWriter, r *http.Request) {}
func (m *MockPreparingServer) RunAllSeriesRules(w http.ResponseWriter, r *http.Request, params v3.RunAllSeriesRulesParams) {
}
func (m *MockPreparingServer) DeleteSeriesRule(w http.ResponseWriter, r *http.Request, id string) {}
func (m *MockPreparingServer) UpdateSeriesRule(w http.ResponseWriter, r *http.Request, id string) {}
func (m *MockPreparingServer) RunSeriesRule(w http.ResponseWriter, r *http.Request, id string, params v3.RunSeriesRuleParams) {
}
func (m *MockPreparingServer) GetServices(w http.ResponseWriter, r *http.Request, params v3.GetServicesParams) {
}
func (m *MockPreparingServer) GetServicesBouquets(w http.ResponseWriter, r *http.Request) {}
func (m *MockPreparingServer) PostServicesNowNext(w http.ResponseWriter, r *http.Request) {}
func (m *MockPreparingServer) PostServicesIdToggle(w http.ResponseWriter, r *http.Request, id string) {
}
func (m *MockPreparingServer) ListSessions(w http.ResponseWriter, r *http.Request, params v3.ListSessionsParams) {
}
func (m *MockPreparingServer) GetSessionState(w http.ResponseWriter, r *http.Request, sessionID openapi_types.UUID) {
}
func (m *MockPreparingServer) ServeHLS(w http.ResponseWriter, r *http.Request, sessionID openapi_types.UUID, filename string) {
}
func (m *MockPreparingServer) ServeHLSHead(w http.ResponseWriter, r *http.Request, sessionID openapi_types.UUID, filename string) {
}
func (m *MockPreparingServer) ReportPlaybackFeedback(w http.ResponseWriter, r *http.Request, sessionId openapi_types.UUID) {
}

// New methods identified from server_gen.go
func (m *MockPreparingServer) GetStreams(w http.ResponseWriter, r *http.Request)                 {}
func (m *MockPreparingServer) DeleteStreamsId(w http.ResponseWriter, r *http.Request, id string) {}
func (m *MockPreparingServer) GetSystemConfig(w http.ResponseWriter, r *http.Request)            {}
func (m *MockPreparingServer) PutSystemConfig(w http.ResponseWriter, r *http.Request)            {}
func (m *MockPreparingServer) GetSystemHealth(w http.ResponseWriter, r *http.Request)            {}
func (m *MockPreparingServer) GetSystemHealthz(w http.ResponseWriter, r *http.Request)           {}
func (m *MockPreparingServer) GetSystemInfo(w http.ResponseWriter, r *http.Request)              {}
func (m *MockPreparingServer) PostSystemRefresh(w http.ResponseWriter, r *http.Request)          {}
func (m *MockPreparingServer) GetSystemScanStatus(w http.ResponseWriter, r *http.Request)        {}
func (m *MockPreparingServer) TriggerSystemScan(w http.ResponseWriter, r *http.Request)          {}
func (m *MockPreparingServer) GetTimers(w http.ResponseWriter, r *http.Request, params v3.GetTimersParams) {
}
func (m *MockPreparingServer) AddTimer(w http.ResponseWriter, r *http.Request)                    {}
func (m *MockPreparingServer) PreviewConflicts(w http.ResponseWriter, r *http.Request)            {}
func (m *MockPreparingServer) DeleteTimer(w http.ResponseWriter, r *http.Request, timerId string) {}
func (m *MockPreparingServer) GetTimer(w http.ResponseWriter, r *http.Request, timerId string)    {}
func (m *MockPreparingServer) UpdateTimer(w http.ResponseWriter, r *http.Request, timerId string) {}
func (m *MockPreparingServer) PostRecordingPlaybackInfo(w http.ResponseWriter, r *http.Request, recordingId string) {
}

// Optional Extensions (if needed by router)
func (m *MockPreparingServer) HandleRecordingResume(w http.ResponseWriter, r *http.Request, recordingId string) {
}
func (m *MockPreparingServer) HandleRecordingResumeOptions(w http.ResponseWriter, r *http.Request, recordingId string) {
}

func (m *MockPreparingServer) StreamRecordingDirect(w http.ResponseWriter, r *http.Request, recordingId string) {
	w.Header().Set("Retry-After", "5")
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(http.StatusServiceUnavailable)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"code":   "recordings/preparing",
		"status": 503,
		"title":  "Preparing",
	})
}

func (m *MockPreparingServer) ProbeRecordingMp4(w http.ResponseWriter, r *http.Request, recordingId string) {
	m.StreamRecordingDirect(w, r, recordingId)
}

func (m *MockPreparingServer) GetRecordingHLSPlaylist(w http.ResponseWriter, r *http.Request, recordingId string) {
	w.Header().Set("Retry-After", "5")
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(http.StatusServiceUnavailable)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"code":   "recordings/preparing",
		"status": 503,
		"title":  "Preparing",
	})
}

func (m *MockPreparingServer) GetRecordingHLSPlaylistHead(w http.ResponseWriter, r *http.Request, recordingId string) {
	m.GetRecordingHLSPlaylist(w, r, recordingId)
}

func TestPreparingContract(t *testing.T) {
	mockSvc := &MockPreparingServer{}

	h := v3.Handler(mockSvc)

	t.Run("MP4 503 Preparing", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/recordings/some-id/stream.mp4", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
		assert.Equal(t, "5", rec.Header().Get("Retry-After"))
		assert.Contains(t, rec.Body.String(), "recordings/preparing")
	})

	t.Run("HLS 503 Preparing", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/recordings/some-id/playlist.m3u8", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
		assert.Equal(t, "5", rec.Header().Get("Retry-After"))
		assert.Contains(t, rec.Body.String(), "recordings/preparing")
	})
}
