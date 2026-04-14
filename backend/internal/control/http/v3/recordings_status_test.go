package v3

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ManuGH/xg2g/internal/config"
	recservice "github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/control/vod"
)

func TestRecordingsStatusHTTP_IdleOmitsError(t *testing.T) {
	srv, _ := newStatusTestServer(t)
	serviceRef := "1:0:0:0:0:0:0:0:0:0:/media/test.ts"
	recordingID := recservice.EncodeRecordingID(serviceRef)

	req := httptest.NewRequest(http.MethodGet, "/api/v3/recordings/"+recordingID+"/status", nil)
	rr := httptest.NewRecorder()
	srv.GetRecordingsRecordingIdStatus(rr, req, recordingID)

	require.Equal(t, http.StatusOK, rr.Code)
	require.Contains(t, rr.Header().Get("Content-Type"), "application/json")

	var body map[string]any
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&body))
	require.Equal(t, "IDLE", body["state"])
	require.Equal(t, false, body["progressiveReady"])
	_, hasError := body["error"]
	require.False(t, hasError)
}

func TestRecordingsStatusHTTP_MetaFailedIncludesError(t *testing.T) {
	srv, vodMgr := newStatusTestServer(t)
	serviceRef := "1:0:0:0:0:0:0:0:0:0:/media/test.ts"
	recordingID := recservice.EncodeRecordingID(serviceRef)
	vodMgr.SeedMetadata(recservice.RecordingVariantMetadataKey(serviceRef, recservice.DefaultRecordingVariantHash()), vod.Metadata{
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
	require.Equal(t, false, body["progressiveReady"])
	require.Equal(t, "oops", body["error"])
}

func TestRecordingsStatusHTTP_RehydratesDefaultPlaylistFromDisk(t *testing.T) {
	srv, vodMgr := newStatusTestServer(t)
	serviceRef := "1:0:0:0:0:0:0:0:0:0:/media/test.ts"
	recordingID := recservice.EncodeRecordingID(serviceRef)

	cacheDir, err := recservice.RecordingVariantCacheDir(srv.cfg.HLS.Root, serviceRef, recservice.DefaultRecordingVariantHash())
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(cacheDir, 0750))
	playlistPath := filepath.Join(cacheDir, "index.m3u8")
	require.NoError(t, os.WriteFile(playlistPath, []byte("#EXTM3U\n#EXT-X-ENDLIST\n"), 0600))

	req := httptest.NewRequest(http.MethodGet, "/api/v3/recordings/"+recordingID+"/status", nil)
	rr := httptest.NewRecorder()
	srv.GetRecordingsRecordingIdStatus(rr, req, recordingID)

	require.Equal(t, http.StatusOK, rr.Code)
	var body map[string]any
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&body))
	require.Equal(t, "READY", body["state"])
	require.Equal(t, false, body["progressiveReady"])

	meta, ok := vodMgr.GetMetadata(recservice.RecordingVariantMetadataKey(serviceRef, recservice.DefaultRecordingVariantHash()))
	require.True(t, ok)
	require.Equal(t, vod.ArtifactStateReady, meta.State)
	require.Equal(t, playlistPath, meta.PlaylistPath)
}

func TestRecordingsStatusHTTP_ProgressiveReadyReflectsPlayableLivePlaylist(t *testing.T) {
	srv, _ := newStatusTestServer(t)
	serviceRef := "1:0:0:0:0:0:0:0:0:0:/media/test.ts"
	recordingID := recservice.EncodeRecordingID(serviceRef)

	cacheDir, err := recservice.RecordingVariantCacheDir(srv.cfg.HLS.Root, serviceRef, recservice.DefaultRecordingVariantHash())
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(cacheDir, 0750))
	require.NoError(t, os.WriteFile(filepath.Join(cacheDir, "seg_00001.ts"), []byte("segment"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(cacheDir, "index.live.m3u8"), []byte("#EXTM3U\n#EXTINF:6,\nseg_00001.ts\n"), 0600))

	req := httptest.NewRequest(http.MethodGet, "/api/v3/recordings/"+recordingID+"/status", nil)
	rr := httptest.NewRecorder()
	srv.GetRecordingsRecordingIdStatus(rr, req, recordingID)

	require.Equal(t, http.StatusOK, rr.Code)

	var body map[string]any
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&body))
	require.Equal(t, true, body["progressiveReady"])
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

	svc, err := recservice.NewService(
		&cfg,
		vodMgr,
		dummyRes,
		nil,
		nil,
	)
	require.NoError(t, err)

	srv.SetDependencies(Dependencies{
		VODManager:        vodMgr,
		RecordingsService: svc,
	})

	return srv, vodMgr
}
