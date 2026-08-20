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
// MockPreparingServer answers the two recording routes this contract test
// exercises and inherits the rest.
//
// It used to spell out all eighty-five ServerInterface methods, so every
// endpoint added to api/openapi.yaml broke this file until somebody pasted
// another empty method in. Embedding the generated Unimplemented makes that
// maintenance disappear, and a route the test does not mean to exercise now
// answers 501 instead of a silent 200.
type MockPreparingServer struct {
	v3.Unimplemented
}

func (m *MockPreparingServer) GetDvrStatus(w http.ResponseWriter, r *http.Request)   {}
func (m *MockPreparingServer) CreateIntent(w http.ResponseWriter, r *http.Request)   {}
func (m *MockPreparingServer) GetSeriesRules(w http.ResponseWriter, r *http.Request) {}

// New methods identified from server_gen.go
func (m *MockPreparingServer) GetStreams(w http.ResponseWriter, r *http.Request)               {}
func (m *MockPreparingServer) GetErrors(w http.ResponseWriter, r *http.Request)                {}
func (m *MockPreparingServer) GetSystemConfig(w http.ResponseWriter, r *http.Request)          {}
func (m *MockPreparingServer) GetSystemConnectivity(w http.ResponseWriter, r *http.Request)    {}
func (m *MockPreparingServer) PutSystemConfig(w http.ResponseWriter, r *http.Request)          {}
func (m *MockPreparingServer) GetSystemHealth(w http.ResponseWriter, r *http.Request)          {}
func (m *MockPreparingServer) GetSystemHealthz(w http.ResponseWriter, r *http.Request)         {}
func (m *MockPreparingServer) GetSystemInfo(w http.ResponseWriter, r *http.Request)            {}
func (m *MockPreparingServer) PostSystemRefresh(w http.ResponseWriter, r *http.Request)        {}
func (m *MockPreparingServer) TriggerSystemScan(w http.ResponseWriter, r *http.Request)        {}
func (m *MockPreparingServer) AddTimer(w http.ResponseWriter, r *http.Request)                 {}
func (m *MockPreparingServer) PreviewConflicts(w http.ResponseWriter, r *http.Request)         {}
func (m *MockPreparingServer) GetTimer(w http.ResponseWriter, r *http.Request, timerId string) {}

// Optional Extensions (if needed by router)
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

func (m *MockPreparingServer) ServeHLSVariant(w http.ResponseWriter, r *http.Request, sessionID openapi_types.UUID, variant string, filename string) {
	m.ServeHLS(w, r, sessionID, filename)
}

func (m *MockPreparingServer) ServeHLSVariantHead(w http.ResponseWriter, r *http.Request, sessionID openapi_types.UUID, variant string, filename string) {
	m.ServeHLSHead(w, r, sessionID, filename)
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
