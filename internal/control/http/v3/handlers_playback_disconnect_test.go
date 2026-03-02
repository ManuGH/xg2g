package v3_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/ManuGH/xg2g/internal/control/playback"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type disconnectResponseWriter struct {
	header http.Header
	status int
}

func newDisconnectResponseWriter() *disconnectResponseWriter {
	return &disconnectResponseWriter{header: make(http.Header)}
}

func (w *disconnectResponseWriter) Header() http.Header {
	return w.header
}

func (w *disconnectResponseWriter) WriteHeader(statusCode int) {
	if w.status != 0 {
		return
	}
	w.status = statusCode
}

func (w *disconnectResponseWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	if len(p) == 0 {
		return 0, syscall.EPIPE
	}
	return 1, syscall.EPIPE
}

func TestRecordingSegment_ClientDisconnectSuppressesSuccessMetric(t *testing.T) {
	tmpDir := t.TempDir()
	segmentPath := filepath.Join(tmpDir, "seg_00001.ts")
	require.NoError(t, os.WriteFile(segmentPath, bytes.Repeat([]byte{0x47}, 64*1024), 0600))

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

	beforeOK := histogramCountForLabels(t, "xg2g_playback_ttff_seconds", map[string]string{
		"schema":  "recording",
		"outcome": "ok",
	})
	beforeAborted := histogramCountForLabels(t, "xg2g_playback_ttff_seconds", map[string]string{
		"schema":  "recording",
		"outcome": "aborted",
	})

	wInfo := httptest.NewRecorder()
	rInfo := httptest.NewRequest(http.MethodGet, "/api/v3/recordings/"+validRecordingID+"/stream-info", nil)
	srv.GetRecordingPlaybackInfo(wInfo, rInfo, validRecordingID)
	require.Equal(t, http.StatusOK, wInfo.Code)

	rSegment := httptest.NewRequest(http.MethodGet, "/api/v3/recordings/"+validRecordingID+"/seg_00001.ts", nil)
	disconnectWriter := newDisconnectResponseWriter()
	srv.GetRecordingHLSCustomSegment(disconnectWriter, rSegment, validRecordingID, "seg_00001.ts")

	afterOK := histogramCountForLabels(t, "xg2g_playback_ttff_seconds", map[string]string{
		"schema":  "recording",
		"outcome": "ok",
	})
	afterAborted := histogramCountForLabels(t, "xg2g_playback_ttff_seconds", map[string]string{
		"schema":  "recording",
		"outcome": "aborted",
	})

	assert.Equal(t, beforeOK, afterOK, "client disconnect must not emit success TTFF")
	assert.Equal(t, beforeAborted+1, afterAborted, "client disconnect must emit aborted TTFF outcome")
}
