package v3_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/control/http/v3/recordings/artifacts"
	"github.com/ManuGH/xg2g/internal/control/playback"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type staticArtifactsResolver struct {
	playlist []byte
	segment  string
}

func (s *staticArtifactsResolver) ResolvePlaylist(ctx context.Context, recordingID, profile string) (artifacts.ArtifactOK, *artifacts.ArtifactError) {
	return artifacts.ArtifactOK{
		Data:    s.playlist,
		ModTime: time.Now(),
		Kind:    artifacts.ArtifactKindPlaylist,
	}, nil
}

func (s *staticArtifactsResolver) ResolveTimeshift(ctx context.Context, recordingID, profile string) (artifacts.ArtifactOK, *artifacts.ArtifactError) {
	return s.ResolvePlaylist(ctx, recordingID, profile)
}

func (s *staticArtifactsResolver) ResolveSegment(ctx context.Context, recordingID string, segment string) (artifacts.ArtifactOK, *artifacts.ArtifactError) {
	return artifacts.ArtifactOK{
		AbsPath: s.segment,
		ModTime: time.Now(),
		Kind:    artifacts.ArtifactKindSegmentTS,
	}, nil
}

func histogramCountForLabels(t *testing.T, metricName string, labels map[string]string) uint64 {
	t.Helper()
	mfs, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	var total uint64
	for _, mf := range mfs {
		if mf.GetName() != metricName || mf.GetType() != dto.MetricType_HISTOGRAM {
			continue
		}
		for _, metric := range mf.GetMetric() {
			match := true
			for key, want := range labels {
				found := false
				for _, lp := range metric.GetLabel() {
					if lp.GetName() == key && lp.GetValue() == want {
						found = true
						break
					}
				}
				if !found {
					match = false
					break
				}
			}
			if match {
				total += metric.GetHistogram().GetSampleCount()
			}
		}
	}
	return total
}

func TestContract_PlaybackSLO_TTFFRecordedExactlyOnce_OnFirstMedia(t *testing.T) {
	tmpDir := t.TempDir()
	segmentPath := filepath.Join(tmpDir, "seg_00001.ts")
	require.NoError(t, os.WriteFile(segmentPath, []byte("segment-bytes"), 0600))

	svc := new(MockRecordingsService)
	svc.On("GetMediaTruth", mock.Anything, validRecordingID).Return(playback.MediaTruth{
		Container:  "ts",
		VideoCodec: "h264",
		AudioCodec: "aac",
		Duration:   120,
	}, nil)

	srv := createTestServer(svc)
	srv.SetArtifactsResolver(&staticArtifactsResolver{
		playlist: []byte("#EXTM3U\n#EXTINF:2,\nseg_00001.ts\n"),
		segment:  segmentPath,
	})

	beforeTTFF := histogramCountForLabels(t, "xg2g_playback_ttff_seconds", map[string]string{
		"schema":  "recording",
		"outcome": "ok",
	})

	wInfo := httptest.NewRecorder()
	rInfo := httptest.NewRequest(http.MethodGet, "/api/v3/recordings/"+validRecordingID+"/stream-info", nil)
	srv.GetRecordingPlaybackInfo(wInfo, rInfo, validRecordingID)
	require.Equal(t, http.StatusOK, wInfo.Code)

	var dto map[string]any
	require.NoError(t, json.Unmarshal(wInfo.Body.Bytes(), &dto))
	assert.NotEmpty(t, dto["sessionId"])

	wPlaylist := httptest.NewRecorder()
	rPlaylist := httptest.NewRequest(http.MethodGet, "/api/v3/recordings/"+validRecordingID+"/playlist.m3u8", nil)
	srv.GetRecordingHLSPlaylist(wPlaylist, rPlaylist, validRecordingID)
	require.Equal(t, http.StatusOK, wPlaylist.Code)

	afterPlaylistTTFF := histogramCountForLabels(t, "xg2g_playback_ttff_seconds", map[string]string{
		"schema":  "recording",
		"outcome": "ok",
	})
	assert.Equal(t, beforeTTFF+1, afterPlaylistTTFF)

	wSegment := httptest.NewRecorder()
	rSegment := httptest.NewRequest(http.MethodGet, "/api/v3/recordings/"+validRecordingID+"/seg_00001.ts", nil)
	srv.GetRecordingHLSCustomSegment(wSegment, rSegment, validRecordingID, "seg_00001.ts")
	require.Equal(t, http.StatusOK, wSegment.Code)

	afterSegmentTTFF := histogramCountForLabels(t, "xg2g_playback_ttff_seconds", map[string]string{
		"schema":  "recording",
		"outcome": "ok",
	})
	assert.Equal(t, afterPlaylistTTFF, afterSegmentTTFF)
}
