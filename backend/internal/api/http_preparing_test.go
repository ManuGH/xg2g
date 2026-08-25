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
type MockPreparingServer struct {
	v3.Unimplemented
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
