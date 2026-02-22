package v3

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ManuGH/xg2g/internal/config"
	recservice "github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/control/vod"
)

func TestRecordingsStatusHTTP_IdleOmitsError(t *testing.T) {
	srv, _ := newStatusTestServer(t)
	serviceRef := "1:0:0:0:0:0:0:0:0:/media/test.ts"
	recordingID := recservice.EncodeRecordingID(serviceRef)

	req := httptest.NewRequest(http.MethodGet, "/api/v3/recordings/"+recordingID+"/status", nil)
	rr := httptest.NewRecorder()
	srv.GetRecordingsRecordingIdStatus(rr, req, recordingID)

	require.Equal(t, http.StatusOK, rr.Code)
	require.Contains(t, rr.Header().Get("Content-Type"), "application/json")

	var body map[string]any
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&body))
	require.Equal(t, "IDLE", body["state"])
	_, hasError := body["error"]
	require.False(t, hasError)
}

func TestRecordingsStatusHTTP_MetaFailedIncludesError(t *testing.T) {
	srv, vodMgr := newStatusTestServer(t)
	serviceRef := "1:0:0:0:0:0:0:0:0:/media/test.ts"
	recordingID := recservice.EncodeRecordingID(serviceRef)
	vodMgr.SeedMetadata(serviceRef, vod.Metadata{
		State: vod.ArtifactStateFailed,
		Error: "oops",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v3/recordings/"+recordingID+"/status", nil)
	rr := httptest.NewRecorder()
	srv.GetRecordingsRecordingIdStatus(rr, req, recordingID)

	require.Equal(t, http.StatusOK, rr.Code)
	require.Contains(t, rr.Header().Get("Content-Type"), "application/json")

	var body map[string]any
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&body))
	require.Equal(t, "FAILED", body["state"])
	require.Equal(t, "oops", body["error"])
}

func newStatusTestServer(t *testing.T) (*Server, *vod.Manager) {
	t.Helper()
	cfg := config.AppConfig{
		HLS: config.HLSConfig{
			Root: t.TempDir(),
		},
	}
	srv := NewServer(cfg, nil, nil)
	vodMgr, err := vod.NewManager(&successRunner{fsRoot: "/tmp"}, &noopProber{}, nil) // Wire dependencies
	require.NoError(t, err)
	dummyRes, err := recservice.NewResolver(&cfg, vodMgr, recservice.ResolverOptions{})
	require.NoError(t, err)
	recSvc, err := recservice.NewService(&cfg, vodMgr, dummyRes, nil, nil, dummyRes)
	require.NoError(t, err)

	srv.SetDependencies(Dependencies{
		VODManager:        vodMgr,
		RecordingsService: recSvc,
	})

	return srv, vodMgr
}
