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
			_, _ = w.Write([]byte(`{"result": false, "movies": [], "bookmarks": []}`))
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

	// 4. Assert Contract (treat result=false as empty directory)
	assert.Equal(t, http.StatusOK, w.Code, "Expected 200 OK for result=false with empty list")

	var resp RecordingResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err, "Response should be valid JSON")
	if resp.Recordings != nil {
		assert.Len(t, *resp.Recordings, 0, "Expected empty recordings list")
	}

	// Ensure no path leaks in the response
	assert.NotContains(t, strings.ToLower(w.Body.String()), "/media/", "Response body should not contain absolute paths")
	assert.NotContains(t, strings.ToLower(w.Body.String()), "/hdd/", "Response body should not contain absolute paths")
}
