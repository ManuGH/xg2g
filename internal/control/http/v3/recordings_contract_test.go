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
	"github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/control/vod"
	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	// Server struct definition usually usually has `cfg *config.AppConfig` or `cfg config.AppConfig`.
	// In recordings_contract_test.go original: `cfg: cfg` but `cfg` var was struct.
	// `s := &Server{cfg: cfg}`. If Server.cfg is pointer, this fails.
	// I'll check Server definition if I can. But let's assume original test compiled.
	// The problem was s.recordingsService was nil.

	// Wire service
	owiClient := openwebif.NewWithPort(mockServer.URL, 0, openwebif.Options{})
	s.owiClient = owiClient
	// Dependency injection with dummy mocks to satisfy strict invariants
	dummyMgr, err := vod.NewManager(&dummyRunner2{}, &dummyProber2{}, nil)
	require.NoError(t, err)
	dummyRes, err := recordings.NewResolver(&cfg, dummyMgr, recordings.ResolverOptions{})
	require.NoError(t, err)
	recSvc, err := recordings.NewService(&cfg, dummyMgr, dummyRes, NewOWIAdapter(owiClient), nil, dummyRes)
	require.NoError(t, err)
	s.recordingsService = recSvc

	// 3. Perform Request
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/v3/recordings?path=.", nil)

	s.GetRecordings(w, r, GetRecordingsParams{})

	// 4. Assert Contract (treat result=false as empty directory)
	assert.Equal(t, http.StatusOK, w.Code, "Expected 200 OK for result=false with empty list")

	var resp RecordingResponse
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err, "Response should be valid JSON")
	if resp.Recordings != nil {
		assert.Len(t, *resp.Recordings, 0, "Expected empty recordings list")
	}

	// Ensure no path leaks in the response
	assert.NotContains(t, strings.ToLower(w.Body.String()), "/media/", "Response body should not contain absolute paths")
	assert.NotContains(t, strings.ToLower(w.Body.String()), "/hdd/", "Response body should not contain absolute paths")
}

// Helpers for Invariant Satisfaction in this test file
type dummyRunner2 struct{}

func (r *dummyRunner2) Start(ctx context.Context, spec vod.Spec) (vod.Handle, error) {
	return &dummyHandle2{}, nil
}

type dummyHandle2 struct{}

func (h *dummyHandle2) Wait() error                          { return nil }
func (h *dummyHandle2) Stop(grace, kill time.Duration) error { return nil }
func (h *dummyHandle2) Progress() <-chan vod.ProgressEvent {
	return make(chan vod.ProgressEvent)
}
func (h *dummyHandle2) Diagnostics() []string { return nil }

type dummyProber2 struct{}

func (p *dummyProber2) Probe(ctx context.Context, path string) (*vod.StreamInfo, error) {
	return &vod.StreamInfo{
		Video: vod.VideoStreamInfo{CodecName: "h264"},
		Audio: vod.AudioStreamInfo{CodecName: "aac"},
	}, nil
}
