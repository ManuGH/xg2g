package v3

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/stretchr/testify/assert"
)

func TestGetRecordings_Contract_UpstreamFailure(t *testing.T) {
	// 1. Mock OpenWebIF to return result: false
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/movielist" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"result": false, "movies": [], "bookmarks": []}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockServer.Close()

	// 2. Setup xg2g Server with mock OWI
	cfg := config.AppConfig{
		Enigma2: config.Enigma2Settings{
			BaseURL: mockServer.URL,
		},
	}

	s := &Server{
		cfg: cfg,
	}
	// Initialize client using internal/api/v3/v3.go logic if possible, or just inject
	s.owiClient = openwebif.NewWithPort(mockServer.URL, 0, openwebif.Options{})

	// 3. Perform Request
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/v3/recordings?path=.", nil)

	s.GetRecordings(w, r, GetRecordingsParams{})

	// 4. Assert Contract (ADR-SEC-001 & Industrial Resilience)
	assert.Equal(t, http.StatusBadGateway, w.Code, "Expected 502 Bad Gateway for upstream failure")

	var apiErr APIError
	err := json.Unmarshal(w.Body.Bytes(), &apiErr)
	assert.NoError(t, err, "Response should be valid JSON")
	assert.Equal(t, "UPSTREAM_RESULT_FALSE", apiErr.Code, "Expected code UPSTREAM_RESULT_FALSE")

	// Ensure no path leaks in the error message or details
	assert.NotContains(t, strings.ToLower(w.Body.String()), "/media/", "Response body should not contain absolute paths")
	assert.NotContains(t, strings.ToLower(w.Body.String()), "/hdd/", "Response body should not contain absolute paths")
}
