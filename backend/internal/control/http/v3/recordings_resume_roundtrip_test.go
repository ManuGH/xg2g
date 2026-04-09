package v3

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/auth"
	"github.com/ManuGH/xg2g/internal/control/playback"
	recservice "github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/control/vod"
	"github.com/ManuGH/xg2g/internal/pipeline/resume"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
)

func TestHandleRecordingResume_PersistsFingerprintAcrossReopen(t *testing.T) {
	const serviceRef = "1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/test.ts"
	recordingID := recservice.EncodeRecordingID(serviceRef)
	dbPath := filepath.Join(t.TempDir(), "resume.sqlite")
	store, err := resume.NewSqliteStore(dbPath)
	require.NoError(t, err)

	server := NewServer(config.AppConfig{}, nil, nil)
	server.SetDependencies(Dependencies{ResumeStore: store})

	req := httptest.NewRequest(http.MethodPut, "/api/v3/recordings/"+recordingID+"/resume", bytes.NewBufferString(`{"position":123.9,"total":3600,"finished":true}`))
	req.Header.Set("Content-Type", "application/json")

	ctx := auth.WithPrincipal(req.Context(), auth.NewPrincipal("token", "viewer", []string{"v3:read"}))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("recordingId", recordingID)
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	server.HandleRecordingResume(rr, req)
	require.Equal(t, http.StatusNoContent, rr.Code)

	require.NoError(t, store.Close())

	reopened, err := resume.NewSqliteStore(dbPath)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, reopened.Close())
	}()

	stored, err := reopened.Get(context.Background(), "viewer", recordingID)
	require.NoError(t, err)
	require.NotNil(t, stored)
	require.Equal(t, int64(123), stored.PosSeconds)
	require.Equal(t, int64(3600), stored.DurationSeconds)
	require.True(t, stored.Finished)
	require.Equal(t, "id:"+recordingID, stored.Fingerprint)

	adapter := NewResumeAdapter(reopened)
	resumeData, ok, err := adapter.GetResume(context.Background(), "viewer", serviceRef)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, stored.PosSeconds, resumeData.PosSeconds)
	require.Equal(t, stored.DurationSeconds, resumeData.DurationSeconds)
	require.Equal(t, stored.Finished, resumeData.Finished)
	require.Equal(t, stored.UpdatedAt, resumeData.UpdatedAt)

	recordingsService := newResumeTruthRecordingsService(t, adapter)
	listing, err := recordingsService.List(context.Background(), recservice.ListInput{
		RootID:      "movies",
		PrincipalID: "viewer",
	})
	require.NoError(t, err)
	require.Len(t, listing.Recordings, 1)
	require.NotNil(t, listing.Recordings[0].Resume)
	require.Equal(t, stored.PosSeconds, listing.Recordings[0].Resume.PosSeconds)
	require.Equal(t, stored.DurationSeconds, listing.Recordings[0].Resume.DurationSeconds)
	require.Equal(t, stored.Finished, listing.Recordings[0].Resume.Finished)
	require.NotNil(t, listing.Recordings[0].Resume.UpdatedAt)
	require.Equal(t, stored.UpdatedAt, *listing.Recordings[0].Resume.UpdatedAt)
}

type resumeTruthOWIClient struct{}

func (resumeTruthOWIClient) GetLocations(ctx context.Context) ([]recservice.OWILocation, error) {
	return []recservice.OWILocation{{Name: "movies", Path: "/media/hdd/movie"}}, nil
}

func (resumeTruthOWIClient) GetRecordings(ctx context.Context, path string) (recservice.OWIRecordingsList, error) {
	return recservice.OWIRecordingsList{
		Result: true,
		Movies: []recservice.OWIMovie{
			{
				ServiceRef: "1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/test.ts",
				Title:      "Test Recording",
				Length:     "01:00:00",
				Filename:   "test.ts",
			},
		},
	}, nil
}

func (resumeTruthOWIClient) GetTimers(ctx context.Context) ([]recservice.OWITimer, error) {
	return []recservice.OWITimer{}, nil
}

func (resumeTruthOWIClient) DeleteRecording(ctx context.Context, serviceRef string) error {
	return nil
}

type resumeTruthResolver struct{}

func (resumeTruthResolver) Resolve(ctx context.Context, serviceRef string, intent recservice.PlaybackIntent, profile recservice.PlaybackProfile) (recservice.PlaybackInfoResult, error) {
	return recservice.PlaybackInfoResult{}, nil
}

func (resumeTruthResolver) GetMediaTruth(ctx context.Context, recordingID string) (playback.MediaTruth, error) {
	return playback.MediaTruth{}, nil
}

func newResumeTruthRecordingsService(t *testing.T, resumeAdapter *ResumeAdapter) recservice.Service {
	t.Helper()

	cfg := &config.AppConfig{
		RecordingRoots: map[string]string{
			"movies": "/media/hdd/movie",
		},
		HLS: config.HLSConfig{
			Root: t.TempDir(),
		},
	}

	vodManager, err := vod.NewManager(&successRunner{fsRoot: "/tmp"}, &noopProber{}, nil)
	require.NoError(t, err)

	svc, err := recservice.NewService(cfg, vodManager, resumeTruthResolver{}, resumeTruthOWIClient{}, resumeAdapter)
	require.NoError(t, err)
	return svc
}
