package v3

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ManuGH/xg2g/internal/config"
	recservice "github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/control/vod"
	internalrecordings "github.com/ManuGH/xg2g/internal/recordings"
)

func TestStreamRecordingDirect_RehydratesLocalSourceWithoutMetadata(t *testing.T) {
	localRoot := t.TempDir()
	localPath := filepath.Join(localRoot, "test.ts")
	require.NoError(t, os.WriteFile(localPath, []byte("ts-data"), 0644))
	resolvedPath, err := filepath.EvalSymlinks(localPath)
	require.NoError(t, err)

	cfg := config.AppConfig{
		HLS: config.HLSConfig{
			Root: t.TempDir(),
		},
		RecordingPathMappings: []config.RecordingPathMapping{
			{ReceiverRoot: "/media/hdd/movie", LocalRoot: localRoot},
		},
	}

	mapper := internalrecordings.NewPathMapper(cfg.RecordingPathMappings)
	vodMgr, err := vod.NewManager(&successRunner{fsRoot: t.TempDir()}, &noopProber{}, mapper)
	require.NoError(t, err)
	defer vodMgr.Shutdown()

	resolver, err := recservice.NewResolver(&cfg, vodMgr, recservice.ResolverOptions{})
	require.NoError(t, err)

	svc, err := recservice.NewService(&cfg, vodMgr, resolver, nil, nil)
	require.NoError(t, err)

	srv := NewServer(cfg, nil, nil)
	srv.SetDependencies(Dependencies{
		VODManager:        vodMgr,
		RecordingsService: svc,
		PathMapper:        mapper,
	})

	serviceRef := "1:0:1:0:0:0:0:0:0:0:/media/hdd/movie/test.ts"
	recordingID := recservice.EncodeRecordingID(serviceRef)

	req := httptest.NewRequest(http.MethodGet, "/api/v3/recordings/"+recordingID+"/stream.mp4", nil)
	rr := httptest.NewRecorder()
	srv.StreamRecordingDirect(rr, req, recordingID)

	require.Equal(t, http.StatusOK, rr.Code)
	require.Equal(t, "video/mp2t", rr.Header().Get("Content-Type"))
	require.Equal(t, "ts-data", rr.Body.String())

	meta, ok := vodMgr.GetMetadata(serviceRef)
	require.True(t, ok)
	require.Equal(t, vod.ArtifactStateReady, meta.State)
	require.Equal(t, resolvedPath, meta.ResolvedPath)
}
